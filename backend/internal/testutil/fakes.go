package testutil

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/applemango/chain/internal/external/atproto"
	"github.com/applemango/chain/internal/external/fcm"
	"github.com/applemango/chain/internal/realtime"
	authsvc "github.com/applemango/chain/internal/service/auth"
	"github.com/applemango/chain/internal/service/content"
	core "github.com/applemango/chain/internal/service/core"
	"github.com/applemango/chain/internal/service/moderation"
	"github.com/applemango/chain/internal/service/proximity"
)

var (
	_ authsvc.OAuthClient       = (*fakeOAuth)(nil)
	_ authsvc.IdentityDirectory = fakeDir{}
	_ authsvc.ProfileFetcher    = (*fakeProfiles)(nil)
)

// fakeOAuth は indigo の *oauth.ClientApp を置き換える。ProcessCallback は本物と同じく、
// 先にストアへセッションを保存してから返す。
type fakeOAuth struct {
	store   *atproto.Store
	session *oauth.ClientSessionData
	mu      sync.Mutex
	logouts []string
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

func (f *fakeOAuth) Logout(ctx context.Context, did syntax.DID, sessionID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.logouts = append(f.logouts, sessionID)
	return nil
}

func (f *fakeOAuth) logoutCalls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.logouts))
	copy(out, f.logouts)
	return out
}

type fakeDir struct{}

func (fakeDir) LookupDID(ctx context.Context, did syntax.DID) (*identity.Identity, error) {
	return &identity.Identity{DID: did, Handle: syntax.Handle("test.example")}, nil
}

type fakeProfiles struct{}

func (*fakeProfiles) FetchProfile(ctx context.Context, pdsURL, did string) (*atproto.Profile, error) {
	return &atproto.Profile{}, nil
}

// fakeFirstLoad は初回の読み込みで何もしない（17 §2.5）。NewApp の既定。
type fakeFirstLoad struct{}

var _ content.PublicPostBackfiller = fakeFirstLoad{}

func (fakeFirstLoad) BackfillPublicPosts(context.Context, string, string) error { return nil }

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

var _ proximity.ZonePresence = (*FakeZonePresence)(nil)

func (*FakeZonePresence) Kind() proximity.Kind { return proximity.KindGPS }

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

func (f *FakeZonePresence) Inside(_ context.Context, u string, _ proximity.Zone, _ time.Duration) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.stale {
		return false, proximity.ErrStale
	}
	return f.inside[u], nil
}

// FakePeerProximity は近接判定をテストから制御する（05 §3）。既定では隣接者はおらず、
// Set した相手だけが Neighbors に出る。Observe / Mutual は何もしない。
type FakePeerProximity struct {
	mu        sync.Mutex
	neighbors map[string][]string
}

func NewFakePeerProximity() *FakePeerProximity {
	return &FakePeerProximity{neighbors: map[string][]string{}}
}

var _ proximity.PeerProximity = (*FakePeerProximity)(nil)

func (*FakePeerProximity) Kind() proximity.Kind { return proximity.KindBLE }

// Set は user の隣接者（DID）を設定する。
func (f *FakePeerProximity) Set(user string, peers ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.neighbors[user] = append([]string(nil), peers...)
}

func (f *FakePeerProximity) Neighbors(_ context.Context, u string, _ time.Duration) ([]proximity.Neighbor, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	peers := f.neighbors[u]
	out := make([]proximity.Neighbor, 0, len(peers))
	for _, did := range peers {
		out = append(out, proximity.Neighbor{DID: did})
	}
	return out, nil
}

func (*FakePeerProximity) Observe(_ context.Context, _, _ string, _, _ time.Time, _ *int) error {
	return nil
}

func (*FakePeerProximity) Mutual(_ context.Context, _, _ string, _ time.Time, _ time.Duration) (bool, error) {
	return false, nil
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

// FakeRecordWriter は core.RecordWriter の fake。呼び出しの引数を記録し、結果を差し替えられる（03 §4）。
type FakeRecordWriter struct {
	mu          sync.Mutex
	calls       []PutRecordCall
	createCalls []CreateRecordCall
	deleteCalls []DeleteRecordCall
	records     map[string]fakeRecord
	uploads     []UploadedBlob
	URI         string
	CID         string
	Err         error
	GetErr      error
	UploadErr   error
	SwapErr     bool
	CreateErrs  map[string]error
	DeleteErrs  map[string]error
	seq         int
	blobSeq     int
	// repos は PDS の repo にある record（follow / block など）。ListRecords が返す。did → uri → 値。
	repos   map[string]map[string]json.RawMessage
	ListErr error
}

// fakeRecord は PDS にある record の現在の値と cid。
type fakeRecord struct {
	Value map[string]any
	CID   string
}

// UploadedBlob は UploadBlob が受け取った内容。
type UploadedBlob struct {
	Mime string
	Data []byte
}

// PutRecordCall は PutRecord の引数の記録。
type PutRecordCall struct {
	DID        string
	SessionID  string
	Collection string
	RKey       string
	Record     any
	SwapRecord string
}

// CreateRecordCall は CreateRecord の引数の記録。
type CreateRecordCall struct {
	DID        string
	SessionID  string
	Collection string
	Record     any
}

// DeleteRecordCall は DeleteRecord の引数の記録。
type DeleteRecordCall struct {
	DID        string
	SessionID  string
	Collection string
	RKey       string
}

func NewFakeRecordWriter() *FakeRecordWriter {
	return &FakeRecordWriter{
		URI:        "at://did:plc:fake/jp.i32.chain.actor.profile/self",
		CID:        "bafyfake",
		records:    map[string]fakeRecord{},
		repos:      map[string]map[string]json.RawMessage{},
		CreateErrs: map[string]error{},
		DeleteErrs: map[string]error{},
	}
}

var _ core.RecordWriter = (*FakeRecordWriter)(nil)

// SetRecord は PDS に既にある record を用意する（GetRecord が返す）。
func (f *FakeRecordWriter) SetRecord(collection, rkey string, value map[string]any, cid string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.records[collection+"/"+rkey] = fakeRecord{Value: value, CID: cid}
}

// Record は今の record と cid を返す。無ければ found=false。
func (f *FakeRecordWriter) Record(collection, rkey string) (map[string]any, string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	rec, ok := f.records[collection+"/"+rkey]
	if !ok {
		return nil, "", false
	}
	return rec.Value, rec.CID, true
}

// Uploads は UploadBlob が受け取った内容のコピーを返す。
func (f *FakeRecordWriter) Uploads() []UploadedBlob {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]UploadedBlob, len(f.uploads))
	copy(out, f.uploads)
	return out
}

// SetGetError は次の GetRecord が返すエラーを設定する。
func (f *FakeRecordWriter) SetGetError(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.GetErr = err
}

// SetUploadError は次の UploadBlob が返すエラーを設定する。
func (f *FakeRecordWriter) SetUploadError(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.UploadErr = err
}

// SetSwapConflict は swapRecord を渡した PutRecord を必ず ErrSwapConflict にする（13 §1.3）。
func (f *FakeRecordWriter) SetSwapConflict(conflict bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.SwapErr = conflict
}

func (f *FakeRecordWriter) PutRecord(_ context.Context, did, sessionID, collection, rkey string, record any, swapRecord string) (string, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, PutRecordCall{DID: did, SessionID: sessionID, Collection: collection, RKey: rkey, Record: record, SwapRecord: swapRecord})
	if f.Err != nil {
		return "", "", f.Err
	}
	if swapRecord != "" {
		cur, ok := f.records[collection+"/"+rkey]
		if f.SwapErr || !ok || cur.CID != swapRecord {
			return "", "", atproto.ErrSwapConflict
		}
	}
	cid := f.CID
	if value, ok := record.(map[string]any); ok {
		f.seq++
		cid = "cid-" + strconv.Itoa(f.seq)
		f.records[collection+"/"+rkey] = fakeRecord{Value: value, CID: cid}
	}
	return f.URI, cid, nil
}

func (f *FakeRecordWriter) GetRecord(_ context.Context, did, sessionID, collection, rkey string) (map[string]any, string, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.GetErr != nil {
		return nil, "", false, f.GetErr
	}
	rec, ok := f.records[collection+"/"+rkey]
	if !ok {
		return nil, "", false, nil
	}
	cp := make(map[string]any, len(rec.Value))
	for k, v := range rec.Value {
		cp[k] = v
	}
	return cp, rec.CID, true, nil
}

func (f *FakeRecordWriter) UploadBlob(_ context.Context, did, sessionID, mime string, data []byte) (*atproto.Blob, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.uploads = append(f.uploads, UploadedBlob{Mime: mime, Data: append([]byte(nil), data...)})
	if f.UploadErr != nil {
		return nil, f.UploadErr
	}
	f.blobSeq++
	return &atproto.Blob{
		Type:     "blob",
		Ref:      atproto.BlobLink{Link: "bafyblob" + strconv.Itoa(f.blobSeq)},
		MimeType: mime,
		Size:     len(data),
	}, nil
}

func (f *FakeRecordWriter) CreateRecord(_ context.Context, did, sessionID, collection string, record any) (string, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.createCalls = append(f.createCalls, CreateRecordCall{DID: did, SessionID: sessionID, Collection: collection, Record: record})
	if err := f.CreateErrs[collection]; err != nil {
		return "", "", err
	}
	f.seq++
	n := strconv.Itoa(f.seq)
	uri := "at://" + did + "/" + collection + "/fake" + n
	f.putRepoLocked(did, uri, record)
	return uri, "cid-fake" + n, nil
}

func (f *FakeRecordWriter) putRepoLocked(did, uri string, record any) {
	b, err := json.Marshal(record)
	if err != nil {
		return
	}
	if f.repos[did] == nil {
		f.repos[did] = map[string]json.RawMessage{}
	}
	f.repos[did][uri] = b
}

// SeedRepoRecord は PDS の repo に record を置く（ListRecords が返す。Bluesky のアプリで書いたフォローなど）。
func (f *FakeRecordWriter) SeedRepoRecord(did, collection, rkey string, record any) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	uri := "at://" + did + "/" + collection + "/" + rkey
	f.putRepoLocked(did, uri, record)
	return uri
}

// RemoveRepoRecord は repo から record を消す（Bluesky のアプリで外した場合）。
func (f *FakeRecordWriter) RemoveRepoRecord(did, uri string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.repos[did], uri)
}

// RepoURIs は did の repo にある collection の record の URI を返す。
func (f *FakeRecordWriter) RepoURIs(did, collection string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for uri := range f.repos[did] {
		if strings.HasPrefix(uri, "at://"+did+"/"+collection+"/") {
			out = append(out, uri)
		}
	}
	sort.Strings(out)
	return out
}

// ListRecords は repo にある collection の record を返す（friend.RecordLister）。
func (f *FakeRecordWriter) ListRecords(_ context.Context, _ string, did, collection string) ([]atproto.Record, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.ListErr != nil {
		return nil, f.ListErr
	}
	var out []atproto.Record
	for uri, v := range f.repos[did] {
		if strings.HasPrefix(uri, "at://"+did+"/"+collection+"/") {
			out = append(out, atproto.Record{URI: uri, CID: "cid", Value: v})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].URI < out[j].URI })
	return out, nil
}

func (f *FakeRecordWriter) DeleteRecord(_ context.Context, did, sessionID, collection, rkey string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleteCalls = append(f.deleteCalls, DeleteRecordCall{DID: did, SessionID: sessionID, Collection: collection, RKey: rkey})
	if err := f.DeleteErrs[collection]; err != nil {
		return err
	}
	delete(f.repos[did], "at://"+did+"/"+collection+"/"+rkey)
	return nil
}

// Calls は記録した PutRecord の呼び出しのコピーを返す。
func (f *FakeRecordWriter) Calls() []PutRecordCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]PutRecordCall, len(f.calls))
	copy(out, f.calls)
	return out
}

// CreateCalls は記録した CreateRecord の呼び出しのコピーを返す。
func (f *FakeRecordWriter) CreateCalls() []CreateRecordCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]CreateRecordCall, len(f.createCalls))
	copy(out, f.createCalls)
	return out
}

// DeleteCalls は記録した DeleteRecord の呼び出しのコピーを返す。
func (f *FakeRecordWriter) DeleteCalls() []DeleteRecordCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]DeleteRecordCall, len(f.deleteCalls))
	copy(out, f.deleteCalls)
	return out
}

// SetError は次の PutRecord が返すエラーを設定する。
func (f *FakeRecordWriter) SetError(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Err = err
}

// SetCreateError は指定した collection の CreateRecord が返すエラーを設定する。
func (f *FakeRecordWriter) SetCreateError(collection string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.CreateErrs[collection] = err
}

// SetDeleteError は指定した collection の DeleteRecord が返すエラーを設定する。
func (f *FakeRecordWriter) SetDeleteError(collection string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.DeleteErrs[collection] = err
}

// BlockingModerator は指定した語を含む本文を block にする（11 §3 のテスト用）。
type BlockingModerator struct{ Words []string }

var _ moderation.Moderator = (*BlockingModerator)(nil)

func (m *BlockingModerator) Judge(_ context.Context, c moderation.Content) (moderation.Verdict, error) {
	for _, w := range m.Words {
		if w != "" && strings.Contains(c.Text, w) {
			return moderation.Verdict{Label: moderation.LabelBlock, Categories: []string{"harassment"}}, nil
		}
	}
	return moderation.Verdict{Label: moderation.LabelOK}, nil
}

// FakeAssessor は投稿・グループ名の判定をテストから制御する（11 §3）。Moderator と
// PostAssessor の両方を満たす。含まれる語で block / warn / pending（エラー）を切り替える。
type FakeAssessor struct {
	BlockWords []string
	WarnWords  []string
	FailWords  []string
	Pin        bool
}

var (
	_ moderation.Moderator    = (*FakeAssessor)(nil)
	_ moderation.PostAssessor = (*FakeAssessor)(nil)
)

func (m *FakeAssessor) Judge(_ context.Context, c moderation.Content) (moderation.Verdict, error) {
	v, _, err := m.judge(c.Text)
	return v, err
}

func (m *FakeAssessor) Assess(_ context.Context, c moderation.Content) (moderation.Verdict, bool, error) {
	v, pin, err := m.judge(c.Text)
	return v, pin, err
}

func (m *FakeAssessor) judge(text string) (moderation.Verdict, bool, error) {
	for _, w := range m.FailWords {
		if w != "" && strings.Contains(text, w) {
			return moderation.Verdict{}, false, errors.New("fake moderation failure")
		}
	}
	for _, w := range m.BlockWords {
		if w != "" && strings.Contains(text, w) {
			return moderation.Verdict{Label: moderation.LabelBlock, Categories: []string{"harassment"}, Score: 0.9}, false, nil
		}
	}
	for _, w := range m.WarnWords {
		if w != "" && strings.Contains(text, w) {
			return moderation.Verdict{Label: moderation.LabelWarn, Categories: []string{"spam"}, Score: 0.65}, m.Pin, nil
		}
	}
	return moderation.Verdict{Label: moderation.LabelOK}, m.Pin, nil
}

// FakeFCM は fcm.Sender の fake。送ったメッセージを記録し、トークンごとに結果を差し替える（12 §4）。
type FakeFCM struct {
	mu      sync.Mutex
	sent    []fcm.Message
	errs    map[string]error
	failAll error
}

func NewFakeFCM() *FakeFCM { return &FakeFCM{errs: map[string]error{}} }

var _ fcm.Sender = (*FakeFCM)(nil)

func (f *FakeFCM) Send(_ context.Context, msg fcm.Message) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, msg)
	if err, ok := f.errs[msg.Token]; ok {
		return err
	}
	return f.failAll
}

// FailToken は特定のトークンへの送信を失敗させる。
func (f *FakeFCM) FailToken(token string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.errs[token] = err
}

// FailAll はすべての送信を失敗させる。
func (f *FakeFCM) FailAll(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failAll = err
}

// Sent は送ったメッセージのコピーを返す。
func (f *FakeFCM) Sent() []fcm.Message {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]fcm.Message, len(f.sent))
	copy(out, f.sent)
	return out
}
