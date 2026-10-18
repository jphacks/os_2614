//go:build integration

package content_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/applemango/chain/internal/model"
	"github.com/applemango/chain/internal/testutil"
)

// 共通のフィード（17 §3）の API テスト。

type feedItem struct {
	Kind   string `json:"kind"`
	URI    string `json:"uri"`
	Text   string `json:"text"`
	Hidden string `json:"hidden"`
	Space  *struct {
		ID   string `json:"id"`
		Type string `json:"type"`
		Name string `json:"name"`
	} `json:"space"`
	SelfLabels []string `json:"selfLabels"`
	Moderation *struct {
		Label   string `json:"label"`
		Display string `json:"display"`
	} `json:"moderation"`
}

type feedPage struct {
	Items  []feedItem `json:"items"`
	Cursor string     `json:"cursor"`
}

func feedQuery(t *testing.T, app *fiber.App, token, query string) feedPage {
	t.Helper()
	status, body := testutil.Request(t, app, http.MethodGet, "/api/v1/feed?"+query, "", token)
	if status != http.StatusOK {
		t.Fatalf("feed?%s: status = %d, want 200 (body=%s)", query, status, body)
	}
	var page feedPage
	testutil.MustJSONBody(t, body, &page)
	return page
}

func textsOf(items []feedItem) map[string]bool {
	set := map[string]bool{}
	for _, it := range items {
		set[it.Text] = true
	}
	return set
}

func publishPost(t *testing.T, env *testutil.Env, author, rkey, text string, at time.Time, modLabel string) string {
	t.Helper()
	uri := "at://" + author + "/jp.i32.chain.feed.post/" + rkey
	if _, err := model.UpsertPublicPost(context.Background(), env.DB, model.PublicPost{
		URI: uri, CID: "cid-" + rkey, AuthorDID: author, Text: text,
		CreatedAt: at, IndexedAt: at, ModLabel: modLabel, ModState: "done",
	}); err != nil {
		t.Fatalf("upsert public post: %v", err)
	}
	return uri
}

func insertEncounter(t *testing.T, env *testutil.Env, id, a, b string, at time.Time, visibility string) {
	t.Helper()
	x, y := testutil.Pair(a, b)
	testutil.Exec(t, env, `INSERT INTO encounters (id, user_a, user_b, established_at, first_seen_at, last_seen_at, visibility)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, x, y, at.UnixMilli(), at.UnixMilli(), at.UnixMilli(), visibility)
}

func insertFriendship(t *testing.T, env *testutil.Env, a, b string, at time.Time) {
	t.Helper()
	x, y := testutil.Pair(a, b)
	testutil.Exec(t, env, `INSERT INTO friendships (user_a, user_b, created_at, via) VALUES (?, ?, ?, 'invite')`,
		x, y, at.UnixMilli())
}

func TestFeedHomeSource(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)

	const (
		me       = "did:plc:home-me"
		friend   = "did:plc:home-friend"
		encNew   = "did:plc:home-enc-new"
		encOld   = "did:plc:home-enc-old"
		stranger = "did:plc:home-stranger"
	)
	for _, did := range []string{me, friend, encNew, encOld, stranger} {
		env.CreateUser(t, did, did+".test")
	}
	token := env.Login(t, me)
	now := env.Clock.Now()

	insertFriendship(t, env, me, friend, now)
	insertEncounter(t, env, "e-new", me, encNew, now.Add(-24*time.Hour), "revealed")
	insertEncounter(t, env, "e-old", me, encOld, now.Add(-31*24*time.Hour), "revealed")

	publishPost(t, env, me, "p1", "自分", now.Add(-1*time.Hour), "ok")
	publishPost(t, env, friend, "p1", "フレンド", now.Add(-2*time.Hour), "ok")
	publishPost(t, env, encNew, "p1", "近いすれ違い", now.Add(-3*time.Hour), "ok")
	publishPost(t, env, encOld, "p1", "古いすれ違い", now.Add(-4*time.Hour), "ok")
	publishPost(t, env, stranger, "p1", "無関係", now.Add(-5*time.Hour), "ok")

	page := feedQuery(t, app, token, "source=home")
	texts := textsOf(page.Items)
	for _, want := range []string{"自分", "フレンド", "近いすれ違い"} {
		if !texts[want] {
			t.Fatalf("home に %q がない (items=%+v)", want, page.Items)
		}
	}
	for _, ng := range []string{"古いすれ違い", "無関係"} {
		if texts[ng] {
			t.Fatalf("home に %q が出てはいけない (items=%+v)", ng, page.Items)
		}
	}
	for _, it := range page.Items {
		if it.Kind != "public_post" || it.Space != nil {
			t.Fatalf("home の要素 = %+v, want public_post で space=null", it)
		}
	}
}

func TestFeedGroupsSource(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)

	const (
		me    = "did:plc:groups-me"
		other = "did:plc:groups-other"
	)
	env.CreateUser(t, me, "me.test")
	env.CreateUser(t, other, "other.test")
	token := env.Login(t, me)
	now := env.Clock.Now()

	// read できる active なグループ。
	testutil.Space(t, env, "sp-g", "jp.i32.chain.space.group", "active", me)
	testutil.Group(t, env, "sp-g", true)
	testutil.Member(t, env, "sp-g", me, true)
	testutil.InsertInstancePost(t, env, "sp-g", "jp.i32.chain.space.group", me, "g1", "g post", now.Add(-1*time.Minute), "ok")

	// Party は対象外。
	testutil.Space(t, env, "sp-p", "jp.i32.chain.space.party", "active", me)
	testutil.Member(t, env, "sp-p", me, true)
	testutil.InsertInstancePost(t, env, "sp-p", "jp.i32.chain.space.party", me, "p1", "party post", now.Add(-2*time.Minute), "ok")

	// 退出済みのグループは対象外。
	testutil.Space(t, env, "sp-left", "jp.i32.chain.space.group", "active", me)
	testutil.Group(t, env, "sp-left", true)
	testutil.Exec(t, env, `INSERT INTO space_members (space_id, did, can_read, can_write, role, joined_at, left_at)
		VALUES (?, ?, 1, 1, 'member', ?, ?)`, "sp-left", me, now.Add(-time.Hour).UnixMilli(), now.UnixMilli())
	testutil.InsertInstancePost(t, env, "sp-left", "jp.i32.chain.space.group", me, "l1", "left post", now.Add(-3*time.Minute), "ok")

	// 自分がメンバーでないグループは対象外。
	testutil.Space(t, env, "sp-other", "jp.i32.chain.space.group", "active", other)
	testutil.Group(t, env, "sp-other", true)
	testutil.Member(t, env, "sp-other", other, true)
	testutil.InsertInstancePost(t, env, "sp-other", "jp.i32.chain.space.group", other, "o1", "other post", now.Add(-4*time.Minute), "ok")

	// archived のグループは対象外。
	testutil.Space(t, env, "sp-arch", "jp.i32.chain.space.group", "archived", me)
	testutil.Group(t, env, "sp-arch", true)
	testutil.Member(t, env, "sp-arch", me, false)
	testutil.InsertInstancePost(t, env, "sp-arch", "jp.i32.chain.space.group", me, "a1", "arch post", now.Add(-5*time.Minute), "ok")

	page := feedQuery(t, app, token, "source=groups")
	if len(page.Items) != 1 {
		t.Fatalf("groups = %+v, want only the accessible group post", page.Items)
	}
	it := page.Items[0]
	if it.Text != "g post" || it.Kind != "instance_post" {
		t.Fatalf("groups item = %+v, want g post / instance_post", it)
	}
	if it.Space == nil || it.Space.ID != "sp-g" || it.Space.Type != "group" {
		t.Fatalf("groups space = %+v, want sp-g group", it.Space)
	}
}

func TestFeedSpaceMatchesInstancePosts(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)

	const me = "did:plc:space-me"
	env.CreateUser(t, me, "me.test")
	env.Consent(t, me)
	token := env.Login(t, me)

	testutil.Space(t, env, "sp1", "jp.i32.chain.space.group", "active", me)
	testutil.Group(t, env, "sp1", true)
	testutil.Member(t, env, "sp1", me, true)
	createPost(t, app, token, "sp1", "同じ JSON になる投稿")

	status1, body1 := testutil.Request(t, app, http.MethodGet, "/api/v1/instances/sp1/posts", "", token)
	status2, body2 := testutil.Request(t, app, http.MethodGet, "/api/v1/feed?source=space:sp1", "", token)
	if status1 != http.StatusOK || status2 != http.StatusOK {
		t.Fatalf("status = %d / %d, want 200 (bodies=%s / %s)", status1, status2, body1, body2)
	}
	if string(body1) != string(body2) {
		t.Fatalf("instance posts と space source が違う:\n%s\n%s", body1, body2)
	}
}

func TestFeedSpaceAccessErrors(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)

	const (
		me    = "did:plc:space-err-me"
		other = "did:plc:space-err-other"
	)
	env.CreateUser(t, me, "me.test")
	env.CreateUser(t, other, "other.test")
	token := env.Login(t, me)

	testutil.Space(t, env, "sp-other", "jp.i32.chain.space.group", "active", other)
	testutil.Group(t, env, "sp-other", true)
	testutil.Member(t, env, "sp-other", other, true)

	// read できない → 403。
	status, body := testutil.Request(t, app, http.MethodGet, "/api/v1/feed?source=space:sp-other", "", token)
	if status != http.StatusForbidden {
		t.Fatalf("forbidden space: status = %d, want 403 (body=%s)", status, body)
	}
	testutil.WantErrorCode(t, body, "forbidden")

	// 存在しない → 404。
	status, body = testutil.Request(t, app, http.MethodGet, "/api/v1/feed?source=space:nope", "", token)
	if status != http.StatusNotFound {
		t.Fatalf("missing space: status = %d, want 404 (body=%s)", status, body)
	}
	testutil.WantErrorCode(t, body, "not_found")
}

func TestFeedUserSource(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)

	const (
		me       = "did:plc:user-me"
		friend   = "did:plc:user-friend"
		stranger = "did:plc:user-stranger"
	)
	for _, did := range []string{me, friend, stranger} {
		env.CreateUser(t, did, did+".test")
	}
	token := env.Login(t, me)
	now := env.Clock.Now()

	insertFriendship(t, env, me, friend, now)
	publishPost(t, env, friend, "p1", "フレンドの投稿", now.Add(-1*time.Hour), "ok")
	publishPost(t, env, stranger, "p1", "無関係の投稿", now.Add(-1*time.Hour), "ok")

	page := feedQuery(t, app, token, "source=user:"+friend)
	if len(page.Items) != 1 || page.Items[0].Text != "フレンドの投稿" {
		t.Fatalf("user:friend = %+v, want friend post", page.Items)
	}

	// 自分自身は見られる（空でも 200）。
	feedQuery(t, app, token, "source=user:"+me)

	// 関係のない相手 → 404。
	status, body := testutil.Request(t, app, http.MethodGet, "/api/v1/feed?source=user:"+stranger, "", token)
	if status != http.StatusNotFound {
		t.Fatalf("stranger: status = %d, want 404 (body=%s)", status, body)
	}
	testutil.WantErrorCode(t, body, "not_found")

	// 存在しない相手 → 404。
	status, body = testutil.Request(t, app, http.MethodGet, "/api/v1/feed?source=user:did:plc:nope", "", token)
	if status != http.StatusNotFound {
		t.Fatalf("missing user: status = %d, want 404 (body=%s)", status, body)
	}
}

func TestFeedBlockRelation(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)

	const (
		alice = "did:plc:block-alice"
		bob   = "did:plc:block-bob"
	)
	env.CreateUser(t, alice, "alice.test")
	env.CreateUser(t, bob, "bob.test")
	aliceTok := env.Login(t, alice)
	bobTok := env.Login(t, bob)
	now := env.Clock.Now()

	testutil.Space(t, env, "sp1", "jp.i32.chain.space.group", "active", alice)
	testutil.Group(t, env, "sp1", true)
	testutil.Member(t, env, "sp1", alice, true)
	testutil.Member(t, env, "sp1", bob, true)
	testutil.InsertInstancePost(t, env, "sp1", "jp.i32.chain.space.group", alice, "a1", "alice post", now.Add(-1*time.Minute), "ok")
	testutil.InsertInstancePost(t, env, "sp1", "jp.i32.chain.space.group", bob, "b1", "bob post", now.Add(-2*time.Minute), "ok")

	if page := feedQuery(t, app, aliceTok, "source=space:sp1"); len(page.Items) != 2 {
		t.Fatalf("before block = %+v, want 2", page.Items)
	}

	testutil.Exec(t, env, `INSERT INTO blocks (blocker, blocked, created_at) VALUES (?, ?, ?)`,
		alice, bob, now.UnixMilli())

	page := feedQuery(t, app, aliceTok, "source=space:sp1")
	if len(page.Items) != 1 || page.Items[0].Text != "alice post" {
		t.Fatalf("alice after block = %+v, want only alice post", page.Items)
	}
	// どちら向きのブロックでも同じ（bob からも alice が見えない）。
	page = feedQuery(t, app, bobTok, "source=space:sp1")
	if len(page.Items) != 1 || page.Items[0].Text != "bob post" {
		t.Fatalf("bob after block = %+v, want only bob post", page.Items)
	}
}

func TestFeedModerationDisplay(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)

	const (
		alice = "did:plc:mod-alice"
		carol = "did:plc:mod-carol"
		dave  = "did:plc:mod-dave"
	)
	for _, did := range []string{alice, carol, dave} {
		env.CreateUser(t, did, did+".test")
	}
	aliceTok := env.Login(t, alice)
	carolTok := env.Login(t, carol)
	now := env.Clock.Now()

	testutil.Space(t, env, "sp1", "jp.i32.chain.space.group", "active", alice)
	testutil.Group(t, env, "sp1", true)
	for _, did := range []string{alice, carol, dave} {
		testutil.Member(t, env, "sp1", did, true)
	}
	testutil.InsertInstancePost(t, env, "sp1", "jp.i32.chain.space.group", carol, "c1", "block された投稿", now.Add(-1*time.Minute), "block")
	testutil.InsertInstancePost(t, env, "sp1", "jp.i32.chain.space.group", dave, "d1", "warn の投稿", now.Add(-2*time.Minute), "warn")

	// block は投稿者本人にだけ hidden:"moderation" として返る。
	page := feedQuery(t, app, aliceTok, "source=space:sp1")
	if textsOf(page.Items)["block された投稿"] {
		t.Fatalf("alice に block された投稿が出た: %+v", page.Items)
	}
	page = feedQuery(t, app, carolTok, "source=space:sp1")
	found := false
	for _, it := range page.Items {
		if it.Text == "block された投稿" {
			found = true
			if it.Hidden != "moderation" {
				t.Fatalf("carol の block 投稿 hidden = %q, want moderation", it.Hidden)
			}
		}
	}
	if !found {
		t.Fatalf("carol に自分の block 投稿が出ない: %+v", page.Items)
	}

	// warn は warn_display に従う。既定は blur（表示する）。
	assertDave := func(wantDisplay string) {
		t.Helper()
		page := feedQuery(t, app, aliceTok, "source=space:sp1")
		for _, it := range page.Items {
			if it.Text == "warn の投稿" {
				if it.Moderation == nil || it.Moderation.Label != "warn" || it.Moderation.Display != wantDisplay {
					t.Fatalf("warn moderation = %+v, want label=warn display=%s", it.Moderation, wantDisplay)
				}
				return
			}
		}
		t.Fatalf("warn の投稿が出ない (want display=%s, items=%+v)", wantDisplay, page.Items)
	}
	assertDave("blur")

	testutil.Exec(t, env, "UPDATE user_settings SET warn_display = 'show' WHERE did = ?", alice)
	assertDave("show")

	testutil.Exec(t, env, "UPDATE user_settings SET warn_display = 'hide' WHERE did = ?", alice)
	if page := feedQuery(t, app, aliceTok, "source=space:sp1"); textsOf(page.Items)["warn の投稿"] {
		t.Fatalf("warn_display=hide なのに warn の投稿が出た: %+v", page.Items)
	}
}

func TestFeedWarnDisplayPublic(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)

	const (
		me   = "did:plc:warn-me"
		peer = "did:plc:warn-peer"
	)
	env.CreateUser(t, me, "me.test")
	env.CreateUser(t, peer, "peer.test")
	token := env.Login(t, me)
	now := env.Clock.Now()

	insertFriendship(t, env, me, peer, now)
	publishPost(t, env, peer, "p1", "warn された公開投稿", now.Add(-1*time.Hour), "warn")

	page := feedQuery(t, app, token, "source=home")
	if !textsOf(page.Items)["warn された公開投稿"] {
		t.Fatalf("既定 blur で warn の公開投稿が出ない: %+v", page.Items)
	}

	testutil.Exec(t, env, "UPDATE user_settings SET warn_display = 'hide' WHERE did = ?", me)
	page = feedQuery(t, app, token, "source=home")
	if textsOf(page.Items)["warn された公開投稿"] {
		t.Fatalf("warn_display=hide なのに warn の公開投稿が出た: %+v", page.Items)
	}
}

// 投稿者が付けた self-label は warn と同じに扱う（19 §2.15.3、D2）。判定が block のものは緩めない。
func TestFeedPublicPostSelfLabelTreatedAsWarn(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)

	const (
		me   = "did:plc:sl-me"
		peer = "did:plc:sl-peer"
	)
	env.CreateUser(t, me, "me.test")
	env.CreateUser(t, peer, "peer.test")
	token := env.Login(t, me)
	now := env.Clock.Now()
	insertFriendship(t, env, me, peer, now)

	at := now.Add(-1 * time.Hour)
	if _, err := model.UpsertPublicPost(context.Background(), env.DB, model.PublicPost{
		URI: "at://" + peer + "/jp.i32.chain.feed.post/sl", CID: "cid-sl", AuthorDID: peer, Text: "ネタバレ",
		SelfLabels: []string{"spoiler"}, CreatedAt: at, IndexedAt: at, ModLabel: "ok", ModState: "done",
	}); err != nil {
		t.Fatal(err)
	}

	page := feedQuery(t, app, token, "source=home")
	var got *feedItem
	for i := range page.Items {
		if page.Items[i].Text == "ネタバレ" {
			got = &page.Items[i]
		}
	}
	if got == nil || got.Moderation == nil || got.Moderation.Label != "warn" || got.Moderation.Display != "blur" {
		t.Fatalf("self-label の投稿 = %+v, want warn/blur", got)
	}
	if len(got.SelfLabels) != 1 || got.SelfLabels[0] != "spoiler" {
		t.Fatalf("selfLabels = %v", got.SelfLabels)
	}

	testutil.Exec(t, env, "UPDATE user_settings SET warn_display = 'hide' WHERE did = ?", me)
	for _, it := range feedQuery(t, app, token, "source=home").Items {
		if it.Text == "ネタバレ" {
			t.Fatalf("warn_display=hide なのに self-label の投稿が出た")
		}
	}
}

// 公開投稿の block は SQL では除かず、service 側で投稿者本人にだけ hidden:"moderation"
// として返す（10 §4.2、11 §4）。home / user source の両方で確かめる。
func TestFeedPublicPostBlockedVisibleOnlyToAuthor(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)

	const (
		alice = "did:plc:pp-block-alice"
		bob   = "did:plc:pp-block-bob"
	)
	env.CreateUser(t, alice, "alice.test")
	env.CreateUser(t, bob, "bob.test")
	aliceTok := env.Login(t, alice)
	bobTok := env.Login(t, bob)
	now := env.Clock.Now()

	insertFriendship(t, env, alice, bob, now)
	publishPost(t, env, alice, "ok1", "alice の ok 投稿", now.Add(-3*time.Hour), "ok")
	publishPost(t, env, alice, "blocked1", "alice の block 投稿", now.Add(-2*time.Hour), "block")
	// ページングで 2 ページ目を作るための bob 自身の投稿（home には自分も入る）。
	publishPost(t, env, bob, "ok1", "bob の ok 投稿", now.Add(-1*time.Hour), "ok")

	itemByText := func(items []feedItem, text string) (feedItem, bool) {
		for _, it := range items {
			if it.Text == text {
				return it, true
			}
		}
		return feedItem{}, false
	}

	// bob（他人）からは block の公開投稿が見えない。ok は見える。
	for _, query := range []string{"source=home", "source=user:" + alice} {
		page := feedQuery(t, app, bobTok, query)
		if _, found := itemByText(page.Items, "alice の block 投稿"); found {
			t.Fatalf("bob に block の公開投稿が出た (%s): %+v", query, page.Items)
		}
		if _, found := itemByText(page.Items, "alice の ok 投稿"); !found {
			t.Fatalf("bob に ok の公開投稿が出ない (%s): %+v", query, page.Items)
		}
	}

	// alice 本人には block の公開投稿が hidden:"moderation" として見える。
	for _, query := range []string{"source=home", "source=user:" + alice} {
		page := feedQuery(t, app, aliceTok, query)
		it, found := itemByText(page.Items, "alice の block 投稿")
		if !found {
			t.Fatalf("alice に自分の block 投稿が出ない (%s): %+v", query, page.Items)
		}
		if it.Hidden != "moderation" {
			t.Fatalf("alice の block 投稿 hidden = %q, want moderation (%s)", it.Hidden, query)
		}
		if it.Moderation == nil || it.Moderation.Label != "block" {
			t.Fatalf("alice の block 投稿 moderation = %+v, want label=block (%s)", it.Moderation, query)
		}
		if _, found := itemByText(page.Items, "alice の ok 投稿"); !found {
			t.Fatalf("alice に ok の公開投稿が出ない (%s): %+v", query, page.Items)
		}
	}

	// block を 1 件挟んでも、limit=1 の 2 ページで ok が抜けも重複もなく出る。
	var got []string
	seen := map[string]bool{}
	cursor := ""
	for page := 0; page < 10; page++ {
		query := "source=home&limit=1"
		if cursor != "" {
			query += "&cursor=" + cursor
		}
		res := feedQuery(t, app, bobTok, query)
		for _, it := range res.Items {
			if seen[it.URI] {
				t.Fatalf("重複した要素: %s", it.URI)
			}
			seen[it.URI] = true
			got = append(got, it.Text)
		}
		cursor = res.Cursor
		if cursor == "" {
			break
		}
	}
	want := []string{"bob の ok 投稿", "alice の ok 投稿"}
	if len(got) != len(want) {
		t.Fatalf("paged = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("paged[%d] = %q, want %q (all=%v)", i, got[i], want[i], got)
		}
	}
}

func TestFeedCursorPagination(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)

	const me = "did:plc:cursor-me"
	env.CreateUser(t, me, "me.test")
	token := env.Login(t, me)
	base := env.Clock.Now().Truncate(time.Second)

	testutil.Space(t, env, "sp1", "jp.i32.chain.space.group", "active", me)
	testutil.Group(t, env, "sp1", true)
	testutil.Member(t, env, "sp1", me, true)

	// 同じ createdAt を 3 件含める。uri は rkey の降順になる。
	testutil.InsertInstancePost(t, env, "sp1", "jp.i32.chain.space.group", me, "r1", "r1", base, "ok")
	testutil.InsertInstancePost(t, env, "sp1", "jp.i32.chain.space.group", me, "r2", "r2", base, "ok")
	testutil.InsertInstancePost(t, env, "sp1", "jp.i32.chain.space.group", me, "r3", "r3", base, "ok")
	testutil.InsertInstancePost(t, env, "sp1", "jp.i32.chain.space.group", me, "p2", "p2", base.Add(2*time.Minute), "ok")
	testutil.InsertInstancePost(t, env, "sp1", "jp.i32.chain.space.group", me, "p4", "p4", base.Add(4*time.Minute), "ok")
	testutil.InsertInstancePost(t, env, "sp1", "jp.i32.chain.space.group", me, "p6", "p6", base.Add(6*time.Minute), "ok")

	want := []string{"p6", "p4", "p2", "r3", "r2", "r1"}

	var got []string
	seen := map[string]bool{}
	cursor := ""
	for page := 0; page < 20; page++ {
		query := "source=space:sp1&limit=2"
		if cursor != "" {
			query += "&cursor=" + cursor
		}
		res := feedQuery(t, app, token, query)
		for _, it := range res.Items {
			if seen[it.URI] {
				t.Fatalf("重複した要素: %s", it.URI)
			}
			seen[it.URI] = true
			got = append(got, it.Text)
		}
		cursor = res.Cursor
		if cursor == "" {
			break
		}
	}

	if len(got) != len(want) {
		t.Fatalf("paged = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("paged[%d] = %q, want %q (all=%v)", i, got[i], want[i], got)
		}
	}
}

func TestFeedInvalidSource(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)

	const me = "did:plc:invalid-me"
	env.CreateUser(t, me, "me.test")
	token := env.Login(t, me)

	for _, query := range []string{"", "source=", "source=bogus", "source=space:", "source=user:", "source=home,"} {
		status, body := testutil.Request(t, app, http.MethodGet, "/api/v1/feed?"+query, "", token)
		if status != http.StatusBadRequest {
			t.Fatalf("feed?%s: status = %d, want 400 (body=%s)", query, status, body)
		}
		testutil.WantErrorCode(t, body, "invalid_request")
	}
}
