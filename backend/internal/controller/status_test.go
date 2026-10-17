//go:build integration

package controller_test

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/applemango/chain/internal/external/atproto"
	"github.com/applemango/chain/internal/testutil"
)

// ステータスの API（13 §1.1）。

func TestMeStatus(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)

	env.CreateUser(t, "did:plc:alice", "alice.test")
	alice := env.Login(t, "did:plc:alice")

	status, body := request(t, app, http.MethodPut, "/api/v1/me/status", `{"status":"旅の途中"}`, alice)
	if status != http.StatusOK {
		t.Fatalf("put status: status = %d, want 200 (body=%s)", status, body)
	}

	calls := env.Writer.Calls()
	if len(calls) != 1 || calls[0].Collection != "jp.i32.chain.actor.profile" || calls[0].RKey != "self" {
		t.Fatalf("writer calls = %+v", calls)
	}

	status, body = request(t, app, http.MethodGet, "/api/v1/me", "", alice)
	if status != http.StatusOK {
		t.Fatalf("get me: status = %d (body=%s)", status, body)
	}
	var me struct {
		Status string `json:"status"`
	}
	mustJSONBody(t, body, &me)
	if me.Status != "旅の途中" {
		t.Fatalf("me.status = %q, want 旅の途中", me.Status)
	}

	status, body = request(t, app, http.MethodPut, "/api/v1/me/status", `{"status":""}`, alice)
	if status != http.StatusOK {
		t.Fatalf("clear status: status = %d (body=%s)", status, body)
	}
	status, body = request(t, app, http.MethodGet, "/api/v1/me", "", alice)
	if status != http.StatusOK {
		t.Fatalf("get me after clear: status = %d (body=%s)", status, body)
	}
	var cleared struct {
		Status string `json:"status"`
	}
	mustJSONBody(t, body, &cleared)
	if cleared.Status != "" {
		t.Fatalf("me.status = %q, want empty", cleared.Status)
	}
}

func TestMeStatusTooLong(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)
	env.CreateUser(t, "did:plc:alice", "alice.test")
	alice := env.Login(t, "did:plc:alice")

	long := strings.Repeat("あ", 41)
	status, body := request(t, app, http.MethodPut, "/api/v1/me/status", `{"status":"`+long+`"}`, alice)
	if status != http.StatusBadRequest {
		t.Fatalf("41 chars: status = %d, want 400 (body=%s)", status, body)
	}
	e := wantErrorCode(t, body, "invalid_request")
	if e.Error.Details.Fields["status"] == "" {
		t.Fatalf("details.fields.status is empty (body=%s)", body)
	}
}

func TestMeStatusModerationBlocked(t *testing.T) {
	env := testutil.NewEnv(t)
	env.Moderator = &testutil.FakeAssessor{BlockWords: []string{"誹謗"}}
	app := testutil.NewApp(env)
	env.CreateUser(t, "did:plc:alice", "alice.test")
	alice := env.Login(t, "did:plc:alice")

	status, body := request(t, app, http.MethodPut, "/api/v1/me/status", `{"status":"これは誹謗です"}`, alice)
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("blocked: status = %d, want 422 (body=%s)", status, body)
	}
	wantErrorCode(t, body, "moderation_blocked")
}

func TestMeStatusWriterErrors(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)
	env.CreateUser(t, "did:plc:alice", "alice.test")
	alice := env.Login(t, "did:plc:alice")

	env.Writer.SetError(atproto.ErrSessionExpired)
	status, body := request(t, app, http.MethodPut, "/api/v1/me/status", `{"status":"x"}`, alice)
	if status != http.StatusUnauthorized {
		t.Fatalf("expired: status = %d, want 401 (body=%s)", status, body)
	}
	wantErrorCode(t, body, "atproto_session_expired")

	env.Writer.SetError(errors.New("pds down"))
	status, body = request(t, app, http.MethodPut, "/api/v1/me/status", `{"status":"x"}`, alice)
	if status != http.StatusBadGateway {
		t.Fatalf("upstream: status = %d, want 502 (body=%s)", status, body)
	}
	wantErrorCode(t, body, "upstream_error")
}
