//go:build integration

package content_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"

	contentsvc "github.com/applemango/chain/internal/service/content"
	"github.com/applemango/chain/internal/service/core"
	"github.com/applemango/chain/internal/testutil"
)

// nearby 投稿（17 §2）と source=nearby（17 §3）の API テスト。
// nearby は PDS に書かず、今近くにいる人だけに届く限定投稿。

type nearbyPostCreated struct {
	URI string `json:"uri"`
}

// postNearby は nearby 投稿を 1 件作り、その合成 at-uri を返す。
func postNearby(t *testing.T, app *fiber.App, token, text string) string {
	t.Helper()
	status, body := testutil.Request(t, app, http.MethodPost, "/api/v1/nearby/posts", `{"text":"`+text+`"}`, token)
	if status != http.StatusCreated {
		t.Fatalf("nearby create: status = %d, want 201 (body=%s)", status, body)
	}
	var res nearbyPostCreated
	testutil.MustJSONBody(t, body, &res)
	if res.URI == "" {
		t.Fatalf("uri is empty (body=%s)", body)
	}
	return res.URI
}

// findURI は uri に一致する要素を返す。無ければ nil。
func findURI(items []feedItem, uri string) *feedItem {
	for i := range items {
		if items[i].URI == uri {
			return &items[i]
		}
	}
	return nil
}

// (a) 同意がなければ 403 consent_required。
func TestNearbyPostCreateWithoutConsent(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)
	env.CreateUser(t, "did:plc:alice", "alice.test")
	alice := env.Login(t, "did:plc:alice")

	status, body := testutil.Request(t, app, http.MethodPost, "/api/v1/nearby/posts", `{"text":"同意なし"}`, alice)
	if status != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (body=%s)", status, body)
	}
	testutil.WantErrorCode(t, body, "consent_required")
	if calls := env.Writer.CreateCalls(); len(calls) != 0 {
		t.Fatalf("writer was called: %+v", calls)
	}
}

// (b) moderation が block なら 422 moderation_blocked。
func TestNearbyPostCreateBlocked(t *testing.T) {
	env := testutil.NewEnv(t)
	env.Moderator = &testutil.BlockingModerator{Words: []string{"誹謗"}}
	app := testutil.NewApp(env)
	env.CreateUser(t, "did:plc:alice", "alice.test")
	env.Consent(t, "did:plc:alice")
	alice := env.Login(t, "did:plc:alice")

	status, body := testutil.Request(t, app, http.MethodPost, "/api/v1/nearby/posts", `{"text":"これは誹謗です"}`, alice)
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422 (body=%s)", status, body)
	}
	testutil.WantErrorCode(t, body, "moderation_blocked")
	if calls := env.Writer.CreateCalls(); len(calls) != 0 {
		t.Fatalf("writer was called: %+v", calls)
	}
}

// (c) 同意済みの投稿者は 201。source=nearby に著者本人の投稿が出る。PDS には書かない。
func TestNearbyPostAppearsInNearbyFeed(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)
	env.CreateUser(t, "did:plc:alice", "alice.test")
	env.Consent(t, "did:plc:alice")
	alice := env.Login(t, "did:plc:alice")

	uri := postNearby(t, app, alice, "今ここにいます")
	if !strings.Contains(uri, "/"+contentsvc.CollectionNearbyPost+"/") {
		t.Fatalf("uri = %q, want %s collection", uri, contentsvc.CollectionNearbyPost)
	}
	// nearby 投稿は PDS に書かない（限定データは SQLite のみ）。
	if calls := env.Writer.CreateCalls(); len(calls) != 0 {
		t.Fatalf("PDS に書いた: %+v", calls)
	}

	item := findURI(feedQuery(t, app, alice, "source=nearby").Items, uri)
	if item == nil {
		t.Fatalf("nearby フィードに %q がない", uri)
	}
	if item.Kind != "nearby_post" {
		t.Fatalf("kind = %q, want nearby_post", item.Kind)
	}
}

// (d) 近くにいない閲覧者には出ず、隣接すると出る（近くにいる人だけの不変条件）。
func TestNearbyPostHiddenFromNonNeighbor(t *testing.T) {
	env := testutil.NewEnv(t)
	env.Proximity = testutil.NewFakePeerProximity()
	app := testutil.NewApp(env)
	env.CreateUser(t, "did:plc:author", "author.test")
	env.Consent(t, "did:plc:author")
	author := env.Login(t, "did:plc:author")
	env.CreateUser(t, "did:plc:viewer", "viewer.test")
	viewer := env.Login(t, "did:plc:viewer")

	uri := postNearby(t, app, author, "近くだけの投稿")

	if item := findURI(feedQuery(t, app, viewer, "source=nearby").Items, uri); item != nil {
		t.Fatalf("隣接していない viewer に nearby が出た: %+v", item)
	}

	env.Proximity.(*testutil.FakePeerProximity).Set("did:plc:viewer", "did:plc:author")
	item := findURI(feedQuery(t, app, viewer, "source=nearby").Items, uri)
	if item == nil {
		t.Fatalf("隣接する viewer に nearby が出ない (uri=%q)", uri)
	}
	if item.Kind != "nearby_post" {
		t.Fatalf("kind = %q, want nearby_post", item.Kind)
	}
}

// (e) expires_at を過ぎた nearby は出ない。
func TestNearbyPostExpires(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)
	env.CreateUser(t, "did:plc:alice", "alice.test")
	env.Consent(t, "did:plc:alice")
	alice := env.Login(t, "did:plc:alice")

	uri := postNearby(t, app, alice, "すぐ消える投稿")
	if findURI(feedQuery(t, app, alice, "source=nearby").Items, uri) == nil {
		t.Fatalf("作成直後に nearby に出ない (uri=%q)", uri)
	}

	env.Clock.Advance(core.NearbyPostTTL + time.Minute)
	// access token は AccessTTL(15 分) で切れるので、進めた時刻で取り直す。
	alice = env.Login(t, "did:plc:alice")
	if item := findURI(feedQuery(t, app, alice, "source=nearby").Items, uri); item != nil {
		t.Fatalf("期限切れの nearby が出た: %+v", item)
	}
}

// (f) home フィード（audience='public' のみ）に nearby は混ざらない。
func TestNearbyPostNotInHomeFeed(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)
	env.CreateUser(t, "did:plc:alice", "alice.test")
	env.Consent(t, "did:plc:alice")
	alice := env.Login(t, "did:plc:alice")

	uri := postNearby(t, app, alice, "home には出ない")
	if item := findURI(feedQuery(t, app, alice, "source=home").Items, uri); item != nil {
		t.Fatalf("home に nearby が混ざった: %+v", item)
	}
}
