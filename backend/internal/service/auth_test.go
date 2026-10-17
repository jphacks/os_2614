//go:build integration

package service_test

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/applemango/chain/internal/auth"
	"github.com/applemango/chain/internal/external/atproto"
	"github.com/applemango/chain/internal/httpx"
	"github.com/applemango/chain/internal/model"
	"github.com/applemango/chain/internal/service"
	"github.com/applemango/chain/internal/testutil"
)

var (
	_ service.OAuthClient       = (*fakeOAuth)(nil)
	_ service.IdentityDirectory = fakeDir{}
	_ service.ProfileFetcher    = (*fakeProfiles)(nil)
)

// fakeOAuth は indigo の *oauth.ClientApp を置き換える。
type fakeOAuth struct {
	store   *atproto.Store
	session *oauth.ClientSessionData

	startIdentifier string
	startCalls      int
	processCalls    int
	logoutCalls     []logoutCall
}

type logoutCall struct {
	did       string
	sessionID string
}

func (f *fakeOAuth) StartAuthFlow(ctx context.Context, identifier string) (string, error) {
	f.startCalls++
	f.startIdentifier = identifier
	return "https://auth.example/authorize", nil
}

// ProcessCallback は本物の indigo と同じく、先にストアへセッションを保存してから返す。
func (f *fakeOAuth) ProcessCallback(ctx context.Context, params url.Values) (*oauth.ClientSessionData, error) {
	f.processCalls++
	if err := f.store.SaveSession(ctx, *f.session); err != nil {
		return nil, err
	}
	return f.session, nil
}

func (f *fakeOAuth) Logout(ctx context.Context, did syntax.DID, sessionID string) error {
	f.logoutCalls = append(f.logoutCalls, logoutCall{did: did.String(), sessionID: sessionID})
	return nil
}

type fakeDir struct{}

func (fakeDir) LookupDID(ctx context.Context, did syntax.DID) (*identity.Identity, error) {
	return &identity.Identity{DID: did, Handle: syntax.Handle("alice.test")}, nil
}

type fakeProfiles struct {
	displayName string
	err         error
	calls       int
}

func (f *fakeProfiles) FetchProfile(ctx context.Context, pdsURL, did string) (*atproto.Profile, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return &atproto.Profile{DisplayName: f.displayName}, nil
}

func newAuthEnv(t *testing.T) (*testutil.Env, *service.AuthService, *fakeOAuth) {
	t.Helper()
	env := testutil.NewEnv(t)
	store := &atproto.Store{DB: env.DB, Redis: env.Redis, SecretKey: env.Config.SecretKey, Clock: env.Clock}
	oauthFake := &fakeOAuth{
		store: store,
		session: &oauth.ClientSessionData{
			SessionID:  "atp-sess-1",
			AccountDID: syntax.DID("did:plc:alice"),
			HostURL:    "https://pds.example",
		},
	}
	svc := &service.AuthService{
		Deps:     env.Deps,
		OAuth:    oauthFake,
		Dir:      fakeDir{},
		Profiles: &fakeProfiles{displayName: "Alice"},
	}
	return env, svc, oauthFake
}

func completeLogin(t *testing.T, svc *service.AuthService) string {
	t.Helper()
	code, err := svc.CompleteLogin(context.Background(), url.Values{"code": {"oauth-code"}, "state": {"state-1"}})
	if err != nil {
		t.Fatalf("CompleteLogin: %v", err)
	}
	if code == "" {
		t.Fatal("one-time code が空です")
	}
	return code
}

func exchange(t *testing.T, env *testutil.Env, svc *service.AuthService, code string) (*service.Tokens, auth.Claims) {
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

func wantCode(t *testing.T, err error, code string) {
	t.Helper()
	var e *httpx.Error
	if !errors.As(err, &e) {
		t.Fatalf("err = %v, want *httpx.Error code %q", err, code)
	}
	if e.Code != code {
		t.Fatalf("code = %q, want %q", e.Code, code)
	}
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
			_, svc, oauthFake := newAuthEnv(t)
			got, err := svc.StartLogin(ctx, tc.input)
			if err != nil {
				t.Fatalf("StartLogin: %v", err)
			}
			if got != "https://auth.example/authorize" {
				t.Fatalf("url = %q", got)
			}
			if oauthFake.startIdentifier != tc.want {
				t.Fatalf("identifier = %q, want %q", oauthFake.startIdentifier, tc.want)
			}
		})
	}
}

func TestStartLoginInvalidHandle(t *testing.T) {
	_, svc, oauthFake := newAuthEnv(t)
	_, err := svc.StartLogin(context.Background(), "not a handle!")
	wantCode(t, err, "invalid_request")
	if oauthFake.startCalls != 0 {
		t.Fatalf("StartAuthFlow が %d 回呼ばれた", oauthFake.startCalls)
	}
}

func TestCompleteLogin(t *testing.T) {
	ctx := context.Background()
	env, svc, _ := newAuthEnv(t)
	code := completeLogin(t, svc)
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
	_, svc, oauthFake := newAuthEnv(t)
	_, err := svc.CompleteLogin(context.Background(), url.Values{"error": {"access_denied"}})
	wantCode(t, err, "unauthenticated")
	if oauthFake.processCalls != 0 {
		t.Fatalf("ProcessCallback が %d 回呼ばれた", oauthFake.processCalls)
	}
}

func TestCompleteLoginProfileFailure(t *testing.T) {
	ctx := context.Background()
	env, svc, _ := newAuthEnv(t)
	svc.Profiles = &fakeProfiles{err: errors.New("pds down")}

	completeLogin(t, svc)

	if _, err := model.GetProfile(ctx, env.DB, "did:plc:alice"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("profile err = %v, want sql.ErrNoRows", err)
	}
}

func TestExchange(t *testing.T) {
	ctx := context.Background()
	env, svc, _ := newAuthEnv(t)
	code := completeLogin(t, svc)

	tok, claims := exchange(t, env, svc, code)
	if claims.DID != "did:plc:alice" {
		t.Fatalf("did = %q, want %q", claims.DID, "did:plc:alice")
	}
	if tok.RefreshToken == "" {
		t.Fatal("refresh token が空です")
	}

	_, err := svc.Exchange(ctx, code, "device-1")
	wantCode(t, err, "gone")
}

func TestRefreshRotates(t *testing.T) {
	ctx := context.Background()
	env, svc, _ := newAuthEnv(t)
	tok, oldClaims := exchange(t, env, svc, completeLogin(t, svc))

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

	sessions := &service.SessionService{Deps: env.Deps}
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
	env, svc, _ := newAuthEnv(t)
	tok, _ := exchange(t, env, svc, completeLogin(t, svc))

	tok2, err := svc.Refresh(ctx, tok.RefreshToken)
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	// 古い refresh の再利用で系列ごと失効する。
	_, err = svc.Refresh(ctx, tok.RefreshToken)
	wantCode(t, err, "unauthenticated")

	// 新しい refresh も使えなくなっている。
	_, err = svc.Refresh(ctx, tok2.RefreshToken)
	wantCode(t, err, "unauthenticated")
}

func TestRefreshExpired(t *testing.T) {
	ctx := context.Background()
	env, svc, _ := newAuthEnv(t)
	tok, _ := exchange(t, env, svc, completeLogin(t, svc))

	env.Clock.Advance(31 * 24 * time.Hour)

	_, err := svc.Refresh(ctx, tok.RefreshToken)
	wantCode(t, err, "unauthenticated")
}

func TestLogoutLastSessionRevokesATProto(t *testing.T) {
	ctx := context.Background()
	env, svc, oauthFake := newAuthEnv(t)
	_, claims := exchange(t, env, svc, completeLogin(t, svc))

	if err := svc.Logout(ctx, "did:plc:alice", claims.SessionID); err != nil {
		t.Fatalf("Logout: %v", err)
	}
	if len(oauthFake.logoutCalls) != 1 {
		t.Fatalf("OAuth.Logout calls = %d, want 1", len(oauthFake.logoutCalls))
	}
	got := oauthFake.logoutCalls[0]
	if got.did != "did:plc:alice" || got.sessionID != "atp-sess-1" {
		t.Fatalf("OAuth.Logout(%q, %q), want (did:plc:alice, atp-sess-1)", got.did, got.sessionID)
	}
}

func TestLogoutKeepsSharedATProtoSession(t *testing.T) {
	ctx := context.Background()
	env, svc, oauthFake := newAuthEnv(t)
	_, claims1 := exchange(t, env, svc, completeLogin(t, svc))
	_, claims2 := exchange(t, env, svc, completeLogin(t, svc))
	if claims1.SessionID == claims2.SessionID {
		t.Fatal("2 つのアプリセッションの sid が同じ")
	}

	if err := svc.Logout(ctx, "did:plc:alice", claims1.SessionID); err != nil {
		t.Fatalf("Logout: %v", err)
	}
	if len(oauthFake.logoutCalls) != 0 {
		t.Fatalf("OAuth.Logout が呼ばれた: %+v", oauthFake.logoutCalls)
	}

	sessions := &service.SessionService{Deps: env.Deps}
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
	env, svc, _ := newAuthEnv(t)
	completeLogin(t, svc)

	me := &service.MeService{Deps: env.Deps}
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
		wantCode(t, err, "not_found")
	}
}
