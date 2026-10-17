package testutil

import (
	"context"
	"net/url"
	"testing"

	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/applemango/chain/internal/external/atproto"
	authsvc "github.com/applemango/chain/internal/service/auth"
)

// 認証（07）のテストの土台。呼び出し回数や identifier を記録する fake を組む。
// content の初回読み込みのテスト（jetstream）もここを使う。

var (
	_ authsvc.OAuthClient       = (*FakeOAuth)(nil)
	_ authsvc.IdentityDirectory = FakeDir{}
	_ authsvc.ProfileFetcher    = (*FakeProfiles)(nil)
)

// FakeOAuth は indigo の *oauth.ClientApp を置き換える。
type FakeOAuth struct {
	Store   *atproto.Store
	Session *oauth.ClientSessionData

	StartIdentifier string
	StartCalls      int
	ProcessCalls    int
	LogoutCalls     []LogoutCall
}

// LogoutCall は Logout の 1 回分の引数。
type LogoutCall struct {
	DID       string
	SessionID string
}

// StartAuthFlow は呼び出しを記録して固定の URL を返す。
func (f *FakeOAuth) StartAuthFlow(ctx context.Context, identifier string) (string, error) {
	f.StartCalls++
	f.StartIdentifier = identifier
	return "https://auth.example/authorize", nil
}

// ProcessCallback は本物の indigo と同じく、先にストアへセッションを保存してから返す。
func (f *FakeOAuth) ProcessCallback(ctx context.Context, params url.Values) (*oauth.ClientSessionData, error) {
	f.ProcessCalls++
	if err := f.Store.SaveSession(ctx, *f.Session); err != nil {
		return nil, err
	}
	return f.Session, nil
}

// Logout は呼び出しを記録する。
func (f *FakeOAuth) Logout(ctx context.Context, did syntax.DID, sessionID string) error {
	f.LogoutCalls = append(f.LogoutCalls, LogoutCall{DID: did.String(), SessionID: sessionID})
	return nil
}

// FakeDir は DID の解決を固定の handle で返す。
type FakeDir struct{}

// LookupDID は固定の identity を返す。
func (FakeDir) LookupDID(ctx context.Context, did syntax.DID) (*identity.Identity, error) {
	return &identity.Identity{DID: did, Handle: syntax.Handle("alice.test")}, nil
}

// FakeProfiles はプロフィールの取得を制御する。
type FakeProfiles struct {
	DisplayName string
	Err         error
	Calls       int
}

// FetchProfile は設定された DisplayName を返す。Err があればそれを返す。
func (f *FakeProfiles) FetchProfile(ctx context.Context, pdsURL, did string) (*atproto.Profile, error) {
	f.Calls++
	if f.Err != nil {
		return nil, f.Err
	}
	return &atproto.Profile{DisplayName: f.DisplayName}, nil
}

// NewAuthEnv は auth のテスト環境を作る。
func NewAuthEnv(t *testing.T) (*Env, *authsvc.AuthService, *FakeOAuth) {
	t.Helper()
	env := NewEnv(t)
	store := &atproto.Store{DB: env.DB, Redis: env.Redis, SecretKey: env.Config.SecretKey, Clock: env.Clock}
	oauthFake := &FakeOAuth{
		Store: store,
		Session: &oauth.ClientSessionData{
			SessionID:  "atp-sess-1",
			AccountDID: syntax.DID("did:plc:alice"),
			HostURL:    "https://pds.example",
		},
	}
	svc := &authsvc.AuthService{
		Deps:     env.Deps,
		OAuth:    oauthFake,
		Dir:      FakeDir{},
		Profiles: &FakeProfiles{DisplayName: "Alice"},
	}
	return env, svc, oauthFake
}

// CompleteLogin はログインを完了して one-time code を返す。
func CompleteLogin(t *testing.T, svc *authsvc.AuthService) string {
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
