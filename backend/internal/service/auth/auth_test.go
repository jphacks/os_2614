//go:build integration

package auth_test

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"testing"
	"time"

	"github.com/applemango/chain/internal/auth"
	"github.com/applemango/chain/internal/model"
	authsvc "github.com/applemango/chain/internal/service/auth"
	mesvc "github.com/applemango/chain/internal/service/me"
	"github.com/applemango/chain/internal/testutil"
)

func exchange(t *testing.T, env *testutil.Env, svc *authsvc.AuthService, code string) (*authsvc.Tokens, auth.Claims) {
	t.Helper()
	tok, err := svc.Exchange(context.Background(), code, "device-1")
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	claims, err := auth.ParseAccess(env.Config.JWTKey, tok.AccessToken, env.Clock.Now())
	if err != nil {
		t.Fatalf("ParseAccess: %v", err)
	}
	return tok, claims
}

func TestStartLogin(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"省略時は bsky.social", "", "https://bsky.social"},
		{"@ 付きの handle", "@alice.test", "alice.test"},
		{"handle", "alice.test", "alice.test"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, svc, oauthFake := testutil.NewAuthEnv(t)
			got, err := svc.StartLogin(ctx, tc.input)
			if err != nil {
				t.Fatalf("StartLogin: %v", err)
			}
			if got != "https://auth.example/authorize" {
				t.Fatalf("url = %q", got)
			}
			if oauthFake.StartIdentifier != tc.want {
				t.Fatalf("identifier = %q, want %q", oauthFake.StartIdentifier, tc.want)
			}
		})
	}
}

func TestStartLoginInvalidHandle(t *testing.T) {
	_, svc, oauthFake := testutil.NewAuthEnv(t)
	_, err := svc.StartLogin(context.Background(), "not a handle!")
	testutil.WantCode(t, err, "invalid_request")
	if oauthFake.StartCalls != 0 {
		t.Fatalf("StartAuthFlow が %d 回呼ばれた", oauthFake.StartCalls)
	}
}

func TestCompleteLogin(t *testing.T) {
	ctx := context.Background()
	env, svc, _ := testutil.NewAuthEnv(t)
	code := testutil.CompleteLogin(t, svc)
	if code == "" {
		t.Fatal("one-time code が空です")
	}

	u, err := model.GetUser(ctx, env.DB, "did:plc:alice")
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if u.Handle != "alice.test" {
		t.Fatalf("handle = %q, want %q", u.Handle, "alice.test")
	}

	p, err := model.GetProfile(ctx, env.DB, "did:plc:alice")
	if err != nil {
		t.Fatalf("GetProfile: %v", err)
	}
	if p.DisplayName != "Alice" {
		t.Fatalf("display_name = %q, want %q", p.DisplayName, "Alice")
	}

	if _, err := model.GetSettings(ctx, env.DB, "did:plc:alice"); err != nil {
		t.Fatalf("user_settings: %v", err)
	}

	row, err := model.GetATProtoSession(ctx, env.DB, "atp-sess-1")
	if err != nil {
		t.Fatalf("atproto_oauth_sessions: %v", err)
	}
	if row.DID != "did:plc:alice" || row.RevokedAt != nil {
		t.Fatalf("session = %+v", row)
	}
}

func TestCompleteLoginDenied(t *testing.T) {
	_, svc, oauthFake := testutil.NewAuthEnv(t)
	_, err := svc.CompleteLogin(context.Background(), url.Values{"error": {"access_denied"}})
	testutil.WantCode(t, err, "unauthenticated")
	if oauthFake.ProcessCalls != 0 {
		t.Fatalf("ProcessCallback が %d 回呼ばれた", oauthFake.ProcessCalls)
	}
}

func TestCompleteLoginProfileFailure(t *testing.T) {
	ctx := context.Background()
	env, svc, _ := testutil.NewAuthEnv(t)
	svc.Profiles = &testutil.FakeProfiles{Err: errors.New("pds down")}

	testutil.CompleteLogin(t, svc)

	if _, err := model.GetProfile(ctx, env.DB, "did:plc:alice"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("profile err = %v, want sql.ErrNoRows", err)
	}
}

func TestExchange(t *testing.T) {
	ctx := context.Background()
	env, svc, _ := testutil.NewAuthEnv(t)
	code := testutil.CompleteLogin(t, svc)

	tok, claims := exchange(t, env, svc, code)
	if claims.DID != "did:plc:alice" {
		t.Fatalf("did = %q, want %q", claims.DID, "did:plc:alice")
	}
	if tok.RefreshToken == "" {
		t.Fatal("refresh token が空です")
	}

	_, err := svc.Exchange(ctx, code, "device-1")
	testutil.WantCode(t, err, "gone")
}

func TestRefreshRotates(t *testing.T) {
	ctx := context.Background()
	env, svc, _ := testutil.NewAuthEnv(t)
	tok, oldClaims := exchange(t, env, svc, testutil.CompleteLogin(t, svc))

	tok2, err := svc.Refresh(ctx, tok.RefreshToken)
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if tok2.RefreshToken == tok.RefreshToken {
		t.Fatal("refresh token が変わっていない")
	}
	newClaims, err := auth.ParseAccess(env.Config.JWTKey, tok2.AccessToken, env.Clock.Now())
	if err != nil {
		t.Fatalf("ParseAccess: %v", err)
	}

	sessions := &authsvc.SessionService{Deps: env.Deps}
	oldActive, err := sessions.Active(ctx, oldClaims.SessionID)
	if err != nil {
		t.Fatalf("Active(old): %v", err)
	}
	if oldActive {
		t.Fatal("古い access の sid が有効なまま")
	}
	newActive, err := sessions.Active(ctx, newClaims.SessionID)
	if err != nil {
		t.Fatalf("Active(new): %v", err)
	}
	if !newActive {
		t.Fatal("新しい access の sid が無効")
	}
}

func TestRefreshReuseRevokesFamily(t *testing.T) {
	ctx := context.Background()
	env, svc, _ := testutil.NewAuthEnv(t)
	tok, _ := exchange(t, env, svc, testutil.CompleteLogin(t, svc))

	tok2, err := svc.Refresh(ctx, tok.RefreshToken)
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	// 古い refresh の再利用で系列ごと失効する。
	_, err = svc.Refresh(ctx, tok.RefreshToken)
	testutil.WantCode(t, err, "unauthenticated")

	// 新しい refresh も使えなくなっている。
	_, err = svc.Refresh(ctx, tok2.RefreshToken)
	testutil.WantCode(t, err, "unauthenticated")
}

func TestRefreshExpired(t *testing.T) {
	ctx := context.Background()
	env, svc, _ := testutil.NewAuthEnv(t)
	tok, _ := exchange(t, env, svc, testutil.CompleteLogin(t, svc))

	env.Clock.Advance(31 * 24 * time.Hour)

	_, err := svc.Refresh(ctx, tok.RefreshToken)
	testutil.WantCode(t, err, "unauthenticated")
}

func TestLogoutLastSessionRevokesATProto(t *testing.T) {
	ctx := context.Background()
	env, svc, oauthFake := testutil.NewAuthEnv(t)
	_, claims := exchange(t, env, svc, testutil.CompleteLogin(t, svc))

	if err := svc.Logout(ctx, "did:plc:alice", claims.SessionID); err != nil {
		t.Fatalf("Logout: %v", err)
	}
	if len(oauthFake.LogoutCalls) != 1 {
		t.Fatalf("OAuth.Logout calls = %d, want 1", len(oauthFake.LogoutCalls))
	}
	got := oauthFake.LogoutCalls[0]
	if got.DID != "did:plc:alice" || got.SessionID != "atp-sess-1" {
		t.Fatalf("OAuth.Logout(%q, %q), want (did:plc:alice, atp-sess-1)", got.DID, got.SessionID)
	}
}

func TestLogoutKeepsSharedATProtoSession(t *testing.T) {
	ctx := context.Background()
	env, svc, oauthFake := testutil.NewAuthEnv(t)
	_, claims1 := exchange(t, env, svc, testutil.CompleteLogin(t, svc))
	_, claims2 := exchange(t, env, svc, testutil.CompleteLogin(t, svc))
	if claims1.SessionID == claims2.SessionID {
		t.Fatal("2 つのアプリセッションの sid が同じ")
	}

	if err := svc.Logout(ctx, "did:plc:alice", claims1.SessionID); err != nil {
		t.Fatalf("Logout: %v", err)
	}
	if len(oauthFake.LogoutCalls) != 0 {
		t.Fatalf("OAuth.Logout が呼ばれた: %+v", oauthFake.LogoutCalls)
	}

	sessions := &authsvc.SessionService{Deps: env.Deps}
	active, err := sessions.Active(ctx, claims2.SessionID)
	if err != nil {
		t.Fatalf("Active: %v", err)
	}
	if !active {
		t.Fatal("2 つ目のセッションまで失効した")
	}
}

func TestMeGet(t *testing.T) {
	ctx := context.Background()
	env, svc, _ := testutil.NewAuthEnv(t)
	testutil.CompleteLogin(t, svc)

	me := &mesvc.MeService{Deps: env.Deps}
	got, err := me.Get(ctx, "did:plc:alice")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.User.Handle != "alice.test" {
		t.Fatalf("handle = %q, want %q", got.User.Handle, "alice.test")
	}
	if got.Profile.DisplayName != "Alice" {
		t.Fatalf("display_name = %q, want %q", got.Profile.DisplayName, "Alice")
	}
	if got.Settings.EncounterMode != "delayed" {
		t.Fatalf("encounter_mode = %q, want %q", got.Settings.EncounterMode, "delayed")
	}

	if _, err := me.Get(ctx, "did:plc:nobody"); err == nil {
		t.Fatal("存在しない DID でエラーにならない")
	} else {
		testutil.WantCode(t, err, "not_found")
	}
}
