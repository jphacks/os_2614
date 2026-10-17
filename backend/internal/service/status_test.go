//go:build integration

package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/applemango/chain/internal/external/atproto"
	"github.com/applemango/chain/internal/model"
	"github.com/applemango/chain/internal/service"
	"github.com/applemango/chain/internal/testutil"
)

// ステータスの PDS 書き込みと profiles 更新（13 §1.1）。

const statusDID = "did:plc:status"

func sidFor(t *testing.T, env *testutil.Env, did string) string {
	t.Helper()
	var sid string
	if err := env.DB.QueryRowContext(context.Background(),
		"SELECT id FROM app_sessions WHERE did = ? ORDER BY created_at DESC, id DESC LIMIT 1", did).Scan(&sid); err != nil {
		t.Fatalf("select sid: %v", err)
	}
	return sid
}

func newStatusHarness(t *testing.T, moderator service.Moderator) (*testutil.Env, *service.MeService, string) {
	t.Helper()
	env := testutil.NewEnv(t)
	env.CreateUser(t, statusDID, "status.test")
	env.Login(t, statusDID)
	me := &service.MeService{Deps: env.Deps, Moderator: moderator, Writer: env.Writer}
	return env, me, sidFor(t, env, statusDID)
}

func TestUpdateStatusWritesPDSAndProfile(t *testing.T) {
	env, me, sid := newStatusHarness(t, service.ModerationNoop{})

	p, err := me.UpdateStatus(context.Background(), statusDID, sid, "旅の途中")
	if err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}
	if p.Status != "旅の途中" {
		t.Fatalf("profile status = %q, want 旅の途中", p.Status)
	}

	calls := env.Writer.Calls()
	if len(calls) != 1 {
		t.Fatalf("writer calls = %d, want 1", len(calls))
	}
	c := calls[0]
	if c.DID != statusDID || c.Collection != "jp.i32.chain.actor.profile" || c.RKey != "self" {
		t.Fatalf("call = %+v", c)
	}
	rec, ok := c.Record.(map[string]any)
	if !ok {
		t.Fatalf("record type = %T", c.Record)
	}
	if rec["$type"] != "jp.i32.chain.actor.profile" || rec["status"] != "旅の途中" {
		t.Fatalf("record = %+v", rec)
	}

	prof, err := model.GetProfile(context.Background(), env.DB, statusDID)
	if err != nil {
		t.Fatalf("GetProfile: %v", err)
	}
	if prof.Status != "旅の途中" {
		t.Fatalf("stored status = %q, want 旅の途中", prof.Status)
	}
}

func TestUpdateStatusEmptyClearsStatus(t *testing.T) {
	env, me, sid := newStatusHarness(t, service.ModerationNoop{})
	if _, err := me.UpdateStatus(context.Background(), statusDID, sid, "いま忙しい"); err != nil {
		t.Fatalf("set: %v", err)
	}
	if _, err := me.UpdateStatus(context.Background(), statusDID, sid, ""); err != nil {
		t.Fatalf("clear: %v", err)
	}
	prof, err := model.GetProfile(context.Background(), env.DB, statusDID)
	if err != nil {
		t.Fatalf("GetProfile: %v", err)
	}
	if prof.Status != "" {
		t.Fatalf("status = %q, want empty", prof.Status)
	}
}

func TestUpdateStatusModerationBlocked(t *testing.T) {
	_, me, sid := newStatusHarness(t, &testutil.FakeAssessor{BlockWords: []string{"誹謗"}})
	_, err := me.UpdateStatus(context.Background(), statusDID, sid, "これは誹謗です")
	wantHTTPError(t, err, "moderation_blocked")
}

func TestUpdateStatusSessionExpired(t *testing.T) {
	env, me, sid := newStatusHarness(t, service.ModerationNoop{})
	env.Writer.SetError(atproto.ErrSessionExpired)
	_, err := me.UpdateStatus(context.Background(), statusDID, sid, "こんにちは")
	wantHTTPError(t, err, "atproto_session_expired")
}

func TestUpdateStatusUpstreamError(t *testing.T) {
	env, me, sid := newStatusHarness(t, service.ModerationNoop{})
	env.Writer.SetError(errors.New("pds down"))
	_, err := me.UpdateStatus(context.Background(), statusDID, sid, "こんにちは")
	wantHTTPError(t, err, "upstream_error")
}
