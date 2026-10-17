//go:build integration

package service_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fasthttp/websocket"

	"github.com/applemango/chain/internal/external/atproto"
	"github.com/applemango/chain/internal/model"
	"github.com/applemango/chain/internal/service"
	"github.com/applemango/chain/internal/testutil"
	"github.com/applemango/chain/internal/worker"
)

// Jetstream の購読・初回の読み込み（03 §5、17 §2.5）。

// jetstreamServer は httptest の WebSocket サーバで Jetstream を偽装する。イベントを流した後は
// 接続を開いたままにし、クライアントが閉じるまで読む。
func jetstreamServer(t *testing.T, events []any) *httptest.Server {
	t.Helper()
	payloads := make([][]byte, len(events))
	for i, e := range events {
		b, err := json.Marshal(e)
		if err != nil {
			t.Fatalf("marshal event: %v", err)
		}
		payloads[i] = b
	}
	up := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		for _, p := range payloads {
			if err := c.WriteMessage(websocket.TextMessage, p); err != nil {
				return
			}
		}
		for {
			if _, _, err := c.ReadMessage(); err != nil {
				return
			}
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func wsURL(srv *httptest.Server) string { return "ws" + strings.TrimPrefix(srv.URL, "http") }

// feedJetstream はイベントを WebSocket で流し、すべて svc.Handle に通るまで待つ。
func feedJetstream(t *testing.T, svc *service.JetstreamService, events []any) {
	t.Helper()
	srv := jetstreamServer(t, events)
	handled := make(chan struct{}, len(events))
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	client := atproto.NewJetstream(atproto.JetstreamOptions{
		URL: wsURL(srv), Cursor: svc.Cursor,
		BackoffBase: 10 * time.Millisecond, BackoffMax: 50 * time.Millisecond,
	})
	go func() {
		_ = client.Run(ctx, func(c context.Context, ev atproto.JetstreamEvent) error {
			err := svc.Handle(c, ev)
			handled <- struct{}{}
			return err
		})
	}()
	for range events {
		select {
		case <-handled:
		case <-time.After(5 * time.Second):
			t.Fatal("jetstream のイベントを処理できなかった")
		}
	}
}

func commitEvent(did, op, collection, rkey, cid string, record map[string]any) map[string]any {
	c := map[string]any{"operation": op, "collection": collection, "rkey": rkey, "cid": cid}
	if record != nil {
		c["record"] = record
	}
	return map[string]any{"did": did, "time_us": 1, "kind": "commit", "commit": c}
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for !cond() {
		select {
		case <-deadline:
			t.Fatalf("timeout: %s", what)
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func TestJetstreamHandleIngestsEvents(t *testing.T) {
	ctx := context.Background()
	env := testutil.NewEnv(t)
	env.CreateUser(t, alice, "alice.test")
	svc := env.Jetstream(nil)

	feed := map[string]any{"text": "hello", "langs": []string{"ja"}, "createdAt": "2026-10-17T03:00:00.000Z"}
	update := map[string]any{
		"text": "hello2", "langs": []string{"ja"},
		"bsky":      map[string]any{"uri": "at://did:plc:alice/app.bsky.feed.post/x", "cid": "bcid"},
		"createdAt": "2026-10-17T03:00:01.000Z",
	}
	events := []any{
		commitEvent(alice, "create", "jp.i32.chain.feed.post", "rkey1", "c1", feed),
		commitEvent(alice, "update", "jp.i32.chain.feed.post", "rkey1", "c2", update),
		commitEvent(alice, "create", "jp.i32.chain.feed.post", "rkeybad", "cx", map[string]any{"langs": []string{"ja"}, "createdAt": "2026-10-17T03:00:00.000Z"}),
		commitEvent(alice, "create", "jp.i32.chain.feed.post", "rkey3", "c3", map[string]any{"text": "bye", "createdAt": "2026-10-17T03:00:02.000Z"}),
		commitEvent(alice, "delete", "jp.i32.chain.feed.post", "rkey3", "", nil),
		commitEvent(alice, "create", "app.bsky.actor.profile", "self", "p1", map[string]any{
			"displayName": "Alice", "description": "d",
			"avatar": map[string]any{"ref": map[string]any{"$link": "avcid"}},
			"banner": map[string]any{"ref": map[string]any{"$link": "bncid"}},
		}),
		commitEvent(alice, "create", "jp.i32.chain.actor.profile", "self", "p2", map[string]any{"status": "busy", "createdAt": "2026-10-17T03:00:00.000Z"}),
		map[string]any{"did": alice, "time_us": 5, "kind": "identity", "identity": map[string]any{"did": alice, "handle": "alice2.test"}},
	}
	feedJetstream(t, svc, events)

	p, err := model.GetPublicPost(ctx, env.DB, atproto.FeedPostURI(alice, "rkey1"))
	if err != nil {
		t.Fatalf("GetPublicPost(rkey1): %v", err)
	}
	if p.CID != "c2" || p.Text != "hello2" || p.DeletedAt != nil {
		t.Fatalf("rkey1 = %+v", p)
	}
	if p.BskyURI != "at://did:plc:alice/app.bsky.feed.post/x" || p.BskyCID != "bcid" {
		t.Fatalf("bsky = (%q,%q)", p.BskyURI, p.BskyCID)
	}

	if _, err := model.GetPublicPost(ctx, env.DB, atproto.FeedPostURI(alice, "rkeybad")); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("rkeybad err = %v, want sql.ErrNoRows", err)
	}

	p3, err := model.GetPublicPost(ctx, env.DB, atproto.FeedPostURI(alice, "rkey3"))
	if err != nil {
		t.Fatalf("GetPublicPost(rkey3): %v", err)
	}
	if p3.DeletedAt == nil {
		t.Fatal("rkey3 が削除されていない")
	}

	prof, err := model.GetProfile(ctx, env.DB, alice)
	if err != nil {
		t.Fatalf("GetProfile: %v", err)
	}
	if prof.DisplayName != "Alice" || prof.Description != "d" || prof.AvatarCID != "avcid" || prof.BannerCID != "bncid" || prof.Status != "busy" {
		t.Fatalf("profile = %+v", prof)
	}

	u, err := model.GetUser(ctx, env.DB, alice)
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if u.Handle != "alice2.test" {
		t.Fatalf("handle = %q, want alice2.test", u.Handle)
	}

	uris := map[string]bool{}
	for _, j := range modQueueJobs(t, env) {
		uris[j.URI] = true
	}
	if !uris[atproto.FeedPostURI(alice, "rkey1")] {
		t.Fatalf("modq = %v, want rkey1", uris)
	}
	if uris[atproto.FeedPostURI(alice, "rkeybad")] {
		t.Fatal("不正な record が modq に積まれた")
	}
}

func TestJetstreamDropsUnknownAndDeletedDIDs(t *testing.T) {
	ctx := context.Background()
	env := testutil.NewEnv(t)
	env.CreateUser(t, alice, "alice.test")
	env.CreateUser(t, bob, "bob.test")
	exec(t, env, "UPDATE users SET deleted_at = ? WHERE did = ?", env.Clock.Now().UnixMilli(), bob)
	svc := env.Jetstream(nil)

	rec := map[string]any{"text": "hi", "createdAt": "2026-10-17T03:00:00.000Z"}
	events := []any{
		commitEvent("did:plc:nobody", "create", "jp.i32.chain.feed.post", "r1", "c1", rec),
		commitEvent(bob, "create", "jp.i32.chain.feed.post", "r2", "c2", rec),
		commitEvent(alice, "create", "jp.i32.chain.feed.post", "r3", "c3", rec),
	}
	feedJetstream(t, svc, events)

	if _, err := model.GetPublicPost(ctx, env.DB, atproto.FeedPostURI("did:plc:nobody", "r1")); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("未登録の DID: err = %v", err)
	}
	if _, err := model.GetPublicPost(ctx, env.DB, atproto.FeedPostURI(bob, "r2")); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("削除済みの DID: err = %v", err)
	}
	if _, err := model.GetPublicPost(ctx, env.DB, atproto.FeedPostURI(alice, "r3")); err != nil {
		t.Fatalf("登録済みの DID が取り込まれていない: %v", err)
	}
}

func TestJetstreamSameURICIDIsNoOp(t *testing.T) {
	ctx := context.Background()
	env := testutil.NewEnv(t)
	env.CreateUser(t, alice, "alice.test")
	svc := env.Jetstream(nil)

	uri := atproto.FeedPostURI(alice, "r1")
	now := env.Clock.Now()
	if _, err := model.UpsertPublicPost(ctx, env.DB, model.PublicPost{
		URI: uri, CID: "c1", AuthorDID: alice, Text: "hi", CreatedAt: now, IndexedAt: now,
		ModLabel: "ok", ModState: "done",
	}); err != nil {
		t.Fatalf("read-after-write: %v", err)
	}
	if err := env.Redis.Del(ctx, "modq").Err(); err != nil {
		t.Fatalf("modq clear: %v", err)
	}

	rec := map[string]any{"text": "hi", "createdAt": "2026-10-17T03:00:00.000Z"}
	feedJetstream(t, svc, []any{commitEvent(alice, "create", "jp.i32.chain.feed.post", "r1", "c1", rec)})

	if jobs := modQueueJobs(t, env); len(jobs) != 0 {
		t.Fatalf("同じ uri/cid で modq に積まれた: %+v", jobs)
	}
	p, err := model.GetPublicPost(ctx, env.DB, uri)
	if err != nil {
		t.Fatalf("GetPublicPost: %v", err)
	}
	if p.ModState != "done" {
		t.Fatalf("mod_state = %q, want done（何も変えない）", p.ModState)
	}
}

func TestJetstreamAccountInactiveHidesPosts(t *testing.T) {
	ctx := context.Background()
	env := testutil.NewEnv(t)
	env.CreateUser(t, alice, "alice.test")
	svc := env.Jetstream(nil)

	events := []any{
		commitEvent(alice, "create", "jp.i32.chain.feed.post", "r1", "c1", map[string]any{"text": "hi", "createdAt": "2026-10-17T03:00:00.000Z"}),
		map[string]any{"did": alice, "time_us": 2, "kind": "account", "account": map[string]any{"did": alice, "active": false}},
	}
	feedJetstream(t, svc, events)

	p, err := model.GetPublicPost(ctx, env.DB, atproto.FeedPostURI(alice, "r1"))
	if err != nil {
		t.Fatalf("GetPublicPost: %v", err)
	}
	if p.DeletedAt == nil {
		t.Fatal("account(active=false) で非表示になっていない")
	}
}

func TestJetstreamWorkerSavesCursor(t *testing.T) {
	ctx := context.Background()
	env := testutil.NewEnv(t)
	env.CreateUser(t, alice, "alice.test")
	svc := env.Jetstream(nil)

	srv := jetstreamServer(t, []any{
		map[string]any{"did": alice, "time_us": 777000, "kind": "identity", "identity": map[string]any{"did": alice, "handle": "alice.test"}},
	})
	client := atproto.NewJetstream(atproto.JetstreamOptions{
		URL: wsURL(srv), Cursor: svc.Cursor,
		BackoffBase: 10 * time.Millisecond, BackoffMax: 50 * time.Millisecond,
	})
	w := &worker.Jetstream{Deps: env.Deps, Service: svc, Client: client, SaveInterval: time.Second}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- w.Run(runCtx) }()

	waitFor(t, "client がイベントを処理する", func() bool { return client.LastTimeUS() == 777000 })
	env.Clock.Advance(time.Second)
	waitFor(t, "カーソルが保存される", func() bool {
		us, err := model.GetJetstreamCursor(ctx, env.DB)
		return err == nil && us == 777000
	})
	cancel()
	<-done
}

// fakePDS は listRecords を返す偽の PDS。呼び出し回数を数える。
func fakePDS(t *testing.T, records []map[string]any, status int) (*httptest.Server, *int32) {
	t.Helper()
	var count int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&count, 1)
		if r.URL.Path != "/xrpc/com.atproto.repo.listRecords" {
			http.NotFound(w, r)
			return
		}
		if status != http.StatusOK {
			w.WriteHeader(status)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"records": records})
	}))
	t.Cleanup(srv.Close)
	return srv, &count
}

func listRecord(did, rkey, cid string, value map[string]any) map[string]any {
	return map[string]any{"uri": atproto.FeedPostURI(did, rkey), "cid": cid, "value": value}
}

// signalingBackfiller は初回の読み込みの呼び出しをチャネルで知らせる。
type signalingBackfiller struct {
	inner service.PublicPostBackfiller
	calls chan struct{}
}

func (s signalingBackfiller) BackfillPublicPosts(ctx context.Context, did, pdsURL string) error {
	defer func() { s.calls <- struct{}{} }()
	return s.inner.BackfillPublicPosts(ctx, did, pdsURL)
}

func TestCompleteLoginFirstLoad(t *testing.T) {
	ctx := context.Background()
	env, svc, oauthFake := newAuthEnv(t)
	pds, count := fakePDS(t, []map[string]any{
		listRecord(alice, "r1", "c1", map[string]any{"text": "old", "createdAt": "2026-10-17T03:00:00.000Z"}),
	}, http.StatusOK)
	oauthFake.session.HostURL = pds.URL

	calls := make(chan struct{}, 4)
	svc.FirstLoad = signalingBackfiller{inner: env.Jetstream(&atproto.FirstLoadFetcher{Client: pds.Client()}), calls: calls}

	completeLogin(t, svc)
	select {
	case <-calls:
	case <-time.After(5 * time.Second):
		t.Fatal("初回の読み込みが呼ばれなかった")
	}
	p, err := model.GetPublicPost(ctx, env.DB, atproto.FeedPostURI(alice, "r1"))
	if err != nil {
		t.Fatalf("初回の読み込みで取り込まれていない: %v", err)
	}
	if p.Text != "old" || p.CID != "c1" {
		t.Fatalf("public post = %+v", p)
	}

	// 2 回目のログインでは読まない。
	completeLogin(t, svc)
	select {
	case <-calls:
		t.Fatal("2 回目のログインで初回の読み込みが呼ばれた")
	case <-time.After(300 * time.Millisecond):
	}
	if n := atomic.LoadInt32(count); n != 1 {
		t.Fatalf("listRecords calls = %d, want 1", n)
	}
}

func TestCompleteLoginFirstLoadFailureDoesNotBlockLogin(t *testing.T) {
	ctx := context.Background()
	env, svc, oauthFake := newAuthEnv(t)
	pds, _ := fakePDS(t, nil, http.StatusInternalServerError)
	oauthFake.session.HostURL = pds.URL

	calls := make(chan struct{}, 4)
	svc.FirstLoad = signalingBackfiller{inner: env.Jetstream(&atproto.FirstLoadFetcher{Client: pds.Client()}), calls: calls}

	completeLogin(t, svc) // エラーにならない
	select {
	case <-calls:
	case <-time.After(5 * time.Second):
		t.Fatal("初回の読み込みが呼ばれなかった")
	}
	if _, err := model.GetUser(ctx, env.DB, alice); err != nil {
		t.Fatalf("ログインが失敗した: %v", err)
	}
}
