//go:build integration

package atproto_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/applemango/chain/internal/external/atproto"
	"github.com/applemango/chain/internal/model"
	"github.com/applemango/chain/internal/testutil"
)

const (
	testDID       = "did:plc:alice"
	testSessionID = "atp-sess-1"
	testAccess    = "plaintext-access-token-xyz"
	testRefresh   = "plaintext-refresh-token-xyz"
)

func newStore(t *testing.T) (*testutil.Env, *atproto.Store) {
	t.Helper()
	env := testutil.NewEnv(t)
	store := &atproto.Store{DB: env.DB, Redis: env.Redis, SecretKey: env.Config.SecretKey, Clock: env.Clock}
	return env, store
}

func testSession() oauth.ClientSessionData {
	return oauth.ClientSessionData{
		SessionID:               testSessionID,
		AccountDID:              syntax.DID(testDID),
		HostURL:                 "https://pds.example",
		AccessToken:             testAccess,
		RefreshToken:            testRefresh,
		DPoPPrivateKeyMultibase: "zQ3shokFTS3brHcDQrn82RUDfCZESWL1ZdCEJwekUDPQiYBme",
	}
}

func TestSaveGetSession(t *testing.T) {
	ctx := context.Background()
	env, store := newStore(t)

	// 保存前は users の行がない（外部キーは SaveSession が仮の行で満たす）。
	if _, err := model.GetUser(ctx, env.DB, testDID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("GetUser err = %v, want sql.ErrNoRows", err)
	}

	if err := store.SaveSession(ctx, testSession()); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}
	got, err := store.GetSession(ctx, syntax.DID(testDID), testSessionID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got.SessionID != testSessionID || got.AccountDID.String() != testDID {
		t.Fatalf("session = %+v", got)
	}
	if got.HostURL != "https://pds.example" || got.AccessToken != testAccess || got.RefreshToken != testRefresh {
		t.Fatalf("session = %+v", got)
	}
}

func TestDeleteSession(t *testing.T) {
	ctx := context.Background()
	_, store := newStore(t)
	if err := store.SaveSession(ctx, testSession()); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}
	if err := store.DeleteSession(ctx, syntax.DID(testDID), testSessionID); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	if _, err := store.GetSession(ctx, syntax.DID(testDID), testSessionID); err == nil {
		t.Fatal("削除後も GetSession が成功した")
	}
}

func TestGetSessionDifferentDID(t *testing.T) {
	ctx := context.Background()
	_, store := newStore(t)
	if err := store.SaveSession(ctx, testSession()); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}
	_, err := store.GetSession(ctx, syntax.DID("did:plc:bob"), testSessionID)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("err = %v, want sql.ErrNoRows", err)
	}
}

func TestAuthRequestInfo(t *testing.T) {
	ctx := context.Background()
	_, store := newStore(t)
	info := oauth.AuthRequestData{
		State:         "state-1",
		AuthServerURL: "https://bsky.social",
		PKCEVerifier:  "verifier-1",
	}

	if err := store.SaveAuthRequestInfo(ctx, info); err != nil {
		t.Fatalf("SaveAuthRequestInfo: %v", err)
	}
	got, err := store.GetAuthRequestInfo(ctx, "state-1")
	if err != nil {
		t.Fatalf("GetAuthRequestInfo: %v", err)
	}
	if got.State != "state-1" || got.AuthServerURL != "https://bsky.social" || got.PKCEVerifier != "verifier-1" {
		t.Fatalf("info = %+v", got)
	}

	// SaveAuthRequestInfo は create-only。
	if err := store.SaveAuthRequestInfo(ctx, info); err == nil {
		t.Fatal("同じ state の 2 回目の保存が成功した")
	}

	if err := store.DeleteAuthRequestInfo(ctx, "state-1"); err != nil {
		t.Fatalf("DeleteAuthRequestInfo: %v", err)
	}
	if _, err := store.GetAuthRequestInfo(ctx, "state-1"); err == nil {
		t.Fatal("削除後も Get が成功した")
	}
}

func TestSessionDataIsEncrypted(t *testing.T) {
	ctx := context.Background()
	env, store := newStore(t)
	if err := store.SaveSession(ctx, testSession()); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}

	var enc []byte
	if err := env.DB.QueryRowContext(ctx,
		"SELECT data_enc FROM atproto_oauth_sessions WHERE id = ?", testSessionID).Scan(&enc); err != nil {
		t.Fatalf("select data_enc: %v", err)
	}
	if len(enc) == 0 {
		t.Fatal("data_enc が空")
	}
	for _, secret := range []string{testAccess, testRefresh} {
		if bytes.Contains(enc, []byte(secret)) {
			t.Fatalf("data_enc に平文 %q が含まれている", secret)
		}
	}
}
