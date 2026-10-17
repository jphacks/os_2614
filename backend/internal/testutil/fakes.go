package testutil

import (
	"context"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/applemango/chain/internal/external/atproto"
	"github.com/applemango/chain/internal/realtime"
	"github.com/applemango/chain/internal/service"
)

var (
	_ service.OAuthClient       = (*fakeOAuth)(nil)
	_ service.IdentityDirectory = fakeDir{}
	_ service.ProfileFetcher    = (*fakeProfiles)(nil)
)

// fakeOAuth は indigo の *oauth.ClientApp を置き換える。ProcessCallback は本物と同じく、
// 先にストアへセッションを保存してから返す。
type fakeOAuth struct {
	store   *atproto.Store
	session *oauth.ClientSessionData
}

func (f *fakeOAuth) StartAuthFlow(ctx context.Context, identifier string) (string, error) {
	return "https://auth.example/authorize", nil
}

func (f *fakeOAuth) ProcessCallback(ctx context.Context, params url.Values) (*oauth.ClientSessionData, error) {
	if err := f.store.SaveSession(ctx, *f.session); err != nil {
		return nil, err
	}
	return f.session, nil
}

func (f *fakeOAuth) Logout(ctx context.Context, did syntax.DID, sessionID string) error { return nil }

type fakeDir struct{}

func (fakeDir) LookupDID(ctx context.Context, did syntax.DID) (*identity.Identity, error) {
	return &identity.Identity{DID: did, Handle: syntax.Handle("test.example")}, nil
}

type fakeProfiles struct{}

func (*fakeProfiles) FetchProfile(ctx context.Context, pdsURL, did string) (*atproto.Profile, error) {
	return &atproto.Profile{}, nil
}

// FakeZonePresence はグループの位置判定をテストから制御する（09 §4）。
// 既定では「位置が古い」（stale）を返し、Set した DID だけ inside / outside を返す。
type FakeZonePresence struct {
	mu     sync.Mutex
	inside map[string]bool
	stale  bool
}

func NewFakeZonePresence() *FakeZonePresence {
	return &FakeZonePresence{inside: map[string]bool{}, stale: true}
}

var _ service.ZonePresence = (*FakeZonePresence)(nil)

func (*FakeZonePresence) Kind() service.Kind { return service.KindGPS }

// Set は DID の在席を設定する。stale が false のときだけ使われる。
func (f *FakeZonePresence) Set(did string, inside bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.inside[did] = inside
}

// SetStale は「位置がない」状態を切り替える。true なら ErrStale を返す。
func (f *FakeZonePresence) SetStale(stale bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stale = stale
}

func (f *FakeZonePresence) Inside(_ context.Context, u string, _ service.Zone, _ time.Duration) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.stale {
		return false, service.ErrStale
	}
	return f.inside[u], nil
}

// FakeBroker は realtime.Broker の fake。送られたイベントを記録し、テストが
// 「どのイベントが誰に送られたか」を確かめられるようにする（10 §7.3）。
type FakeBroker struct {
	mu     sync.Mutex
	events []realtime.Event
}

func NewFakeBroker() *FakeBroker { return &FakeBroker{} }

var _ realtime.Broker = (*FakeBroker)(nil)

func (f *FakeBroker) Publish(_ context.Context, e realtime.Event) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, e)
}

// Events は記録したイベントのコピーを返す。
func (f *FakeBroker) Events() []realtime.Event {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]realtime.Event, len(f.events))
	copy(out, f.events)
	return out
}

// EventsFor は name のイベントで、宛先が user のものを返す。user が空なら全宛先。
func (f *FakeBroker) EventsFor(name, user string) []realtime.Event {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []realtime.Event{}
	for _, e := range f.events {
		if e.Name != name {
			continue
		}
		if user != "" && e.User != user {
			continue
		}
		out = append(out, e)
	}
	return out
}

func (f *FakeBroker) Reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = nil
}

// BlockingModerator は指定した語を含む本文を block にする（11 §3 のテスト用）。
type BlockingModerator struct{ Words []string }

var _ service.Moderator = (*BlockingModerator)(nil)

func (m *BlockingModerator) Judge(_ context.Context, c service.Content) (service.Verdict, error) {
	for _, w := range m.Words {
		if w != "" && strings.Contains(c.Text, w) {
			return service.Verdict{Label: service.LabelBlock, Categories: []string{"harassment"}}, nil
		}
	}
	return service.Verdict{Label: service.LabelOK}, nil
}
