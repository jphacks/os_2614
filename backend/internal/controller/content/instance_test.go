//go:build integration

package content_test

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/applemango/chain/internal/testutil"
)

// instance の投稿（10 §4.1〜§4.3）の API テスト。

func listPosts(t *testing.T, app *fiber.App, token, spaceID string) testutil.PostPage {
	t.Helper()
	status, body := testutil.Request(t, app, http.MethodGet, "/api/v1/instances/"+spaceID+"/posts", "", token)
	if status != http.StatusOK {
		t.Fatalf("list posts: status = %d, want 200 (body=%s)", status, body)
	}
	var page testutil.PostPage
	testutil.MustJSONBody(t, body, &page)
	return page
}

func rkeyOf(t *testing.T, uri string) string {
	t.Helper()
	parts := strings.Split(uri, "/")
	if len(parts) == 0 {
		t.Fatalf("uri %q has no rkey", uri)
	}
	return parts[len(parts)-1]
}

func TestCreatePost(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)

	env.CreateUser(t, "did:plc:alice", "alice.test")
	env.Consent(t, "did:plc:alice")
	token := env.Login(t, "did:plc:alice")
	testutil.Space(t, env, "sp1", "jp.i32.chain.space.group", "active", "did:plc:alice")
	testutil.Group(t, env, "sp1", true)
	testutil.Member(t, env, "sp1", "did:plc:alice", true)

	status, body := testutil.Request(t, app, http.MethodPost, "/api/v1/posts",
		`{"spaceIds":["sp1"],"text":"明日の集合は 9:00 正門","format":"plain"}`, token)
	if status != http.StatusCreated {
		t.Fatalf("create post: status = %d, want 201 (body=%s)", status, body)
	}
	var created struct {
		Items []struct {
			URI     string `json:"uri"`
			SpaceID string `json:"spaceId"`
		} `json:"items"`
	}
	testutil.MustJSONBody(t, body, &created)
	if len(created.Items) != 1 || created.Items[0].SpaceID != "sp1" {
		t.Fatalf("created = %+v, want 1 item in sp1", created)
	}
	if !strings.Contains(created.Items[0].URI, "/jp.i32.chain.instance.post/") {
		t.Fatalf("uri = %q, want instance.post URI", created.Items[0].URI)
	}

	page := listPosts(t, app, token, "sp1")
	if len(page.Items) != 1 || page.Items[0].Text != "明日の集合は 9:00 正門" {
		t.Fatalf("posts = %+v, want the created post", page.Items)
	}

	// GET /instances に space が出る（13 §2）。
	status, body = testutil.Request(t, app, http.MethodGet, "/api/v1/instances", "", token)
	if status != http.StatusOK {
		t.Fatalf("instances: status = %d, want 200 (body=%s)", status, body)
	}
	var instances struct {
		Items []struct {
			SpaceID string `json:"spaceId"`
			State   string `json:"state"`
			Type    string `json:"type"`
		} `json:"items"`
	}
	testutil.MustJSONBody(t, body, &instances)
	if len(instances.Items) != 1 || instances.Items[0].SpaceID != "sp1" || instances.Items[0].Type != "group" {
		t.Fatalf("instances = %+v, want sp1 group", instances.Items)
	}
}

func TestListInstancesCanWrite(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)
	env.CreateUser(t, "did:plc:alice", "alice.test")
	alice := env.Login(t, "did:plc:alice")
	now := env.Clock.Now().UnixMilli()
	addParty := func(id, state, phase string, left bool) {
		testutil.Space(t, env, id, "jp.i32.chain.space.party", state, "did:plc:alice")
		testutil.Exec(t, env, `INSERT INTO parties (space_id, phase, tentative_at, confirmed_at, last_activity_at)
			VALUES (?, ?, ?, ?, ?)`, id, phase, now, now, now)
		if left {
			testutil.Exec(t, env, `INSERT INTO space_members (space_id, did, can_write, joined_at, left_at)
				VALUES (?, ?, 0, ?, ?)`, id, "did:plc:alice", now, now)
		} else {
			testutil.Member(t, env, id, "did:plc:alice", true)
		}
	}
	addParty("party-tentative", "active", "tentative", false)
	addParty("party-confirmed", "active", "confirmed", false)
	addParty("party-left", "active", "confirmed", true)
	addParty("party-archived", "archived", "ended", true)

	status, body := testutil.Request(t, app, http.MethodGet, "/api/v1/instances", "", alice)
	if status != http.StatusOK {
		t.Fatalf("instances: status = %d, want 200 (body=%s)", status, body)
	}
	var response struct {
		Items []struct {
			SpaceID  string `json:"spaceId"`
			CanWrite bool   `json:"canWrite"`
		} `json:"items"`
	}
	testutil.MustJSONBody(t, body, &response)
	got := map[string]bool{}
	for _, item := range response.Items {
		got[item.SpaceID] = item.CanWrite
	}
	want := map[string]bool{
		"party-tentative": false,
		"party-confirmed": true,
		"party-left":      false,
		"party-archived":  false,
	}
	for id, canWrite := range want {
		if got[id] != canWrite {
			t.Fatalf("canWrite[%s] = %v, want %v (items=%+v)", id, got[id], canWrite, got)
		}
	}
}

func TestCreatePostErrors(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)

	env.CreateUser(t, "did:plc:alice", "alice.test")
	env.Consent(t, "did:plc:alice")
	alice := env.Login(t, "did:plc:alice")
	testutil.Space(t, env, "sp1", "jp.i32.chain.space.group", "active", "did:plc:alice")
	testutil.Group(t, env, "sp1", true)
	testutil.Member(t, env, "sp1", "did:plc:alice", true)

	// 同意なし（02 §8）。
	env.CreateUser(t, "did:plc:noconsent", "nc.test")
	nc := env.Login(t, "did:plc:noconsent")
	testutil.Member(t, env, "sp1", "did:plc:noconsent", true)
	status, body := testutil.Request(t, app, http.MethodPost, "/api/v1/posts", `{"spaceIds":["sp1"],"text":"hi"}`, nc)
	if status != http.StatusForbidden {
		t.Fatalf("no consent: status = %d, want 403 (body=%s)", status, body)
	}
	testutil.WantErrorCode(t, body, "consent_required")

	// write 権限なし。
	env.CreateUser(t, "did:plc:outsider", "out.test")
	env.Consent(t, "did:plc:outsider")
	outsider := env.Login(t, "did:plc:outsider")
	status, body = testutil.Request(t, app, http.MethodPost, "/api/v1/posts", `{"spaceIds":["sp1"],"text":"hi"}`, outsider)
	if status != http.StatusForbidden {
		t.Fatalf("outsider: status = %d, want 403 (body=%s)", status, body)
	}
	testutil.WantErrorCode(t, body, "forbidden")

	// archived の Space へは 410。
	testutil.Space(t, env, "sp-arch", "jp.i32.chain.space.group", "archived", "did:plc:alice")
	testutil.Member(t, env, "sp-arch", "did:plc:alice", false)
	status, body = testutil.Request(t, app, http.MethodPost, "/api/v1/posts", `{"spaceIds":["sp-arch"],"text":"hi"}`, alice)
	if status != http.StatusGone {
		t.Fatalf("archived: status = %d, want 410 (body=%s)", status, body)
	}
	testutil.WantErrorCode(t, body, "gone")

	// spaceIds が 6 件 → 400。
	many := `{"spaceIds":["1","2","3","4","5","6"],"text":"hi"}`
	status, body = testutil.Request(t, app, http.MethodPost, "/api/v1/posts", many, alice)
	if status != http.StatusBadRequest {
		t.Fatalf("6 spaces: status = %d, want 400 (body=%s)", status, body)
	}
	e := testutil.WantErrorCode(t, body, "invalid_request")
	if e.Error.Details.Fields["spaceIds"] == "" {
		t.Fatalf("details.fields.spaceIds is empty (body=%s)", body)
	}

	// media 付きで spaceIds が 2 件 → 400。
	status, body = testutil.Request(t, app, http.MethodPost, "/api/v1/posts",
		`{"spaceIds":["sp1","sp-arch"],"text":"hi","media":[{"id":"m1"}]}`, alice)
	if status != http.StatusBadRequest {
		t.Fatalf("media with 2 spaces: status = %d, want 400 (body=%s)", status, body)
	}
	e = testutil.WantErrorCode(t, body, "invalid_request")
	if e.Error.Details.Fields["media"] == "" {
		t.Fatalf("details.fields.media is empty (body=%s)", body)
	}

	// alsoPublic は公開投稿も作る（10 §4.1）。既定の fake Writer では成功する。
	status, body = testutil.Request(t, app, http.MethodPost, "/api/v1/posts",
		`{"spaceIds":["sp1"],"text":"hi","alsoPublic":true}`, alice)
	if status != http.StatusCreated {
		t.Fatalf("alsoPublic: status = %d, want 201 (body=%s)", status, body)
	}
	var withPublic struct {
		Public *struct {
			URI   string `json:"uri"`
			Error string `json:"error"`
		} `json:"public"`
	}
	testutil.MustJSONBody(t, body, &withPublic)
	if withPublic.Public == nil || withPublic.Public.URI == "" || withPublic.Public.Error != "" {
		t.Fatalf("public = %+v, want uri and no error", withPublic.Public)
	}
}

func TestCreatePostWithForeignMedia(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)

	env.CreateUser(t, "did:plc:alice", "alice.test")
	env.Consent(t, "did:plc:alice")
	alice := env.Login(t, "did:plc:alice")

	env.CreateUser(t, "did:plc:bob", "bob.test")
	env.Consent(t, "did:plc:bob")
	bob := env.Login(t, "did:plc:bob")

	testutil.Space(t, env, "sp-bob", "jp.i32.chain.space.group", "active", "did:plc:bob")
	testutil.Group(t, env, "sp-bob", true)
	testutil.Member(t, env, "sp-bob", "did:plc:bob", true)

	mediaID := uploadJPEG(t, app, alice)

	status, body := testutil.Request(t, app, http.MethodPost, "/api/v1/posts",
		fmt.Sprintf(`{"spaceIds":["sp-bob"],"text":"hi","media":[{"id":%q}]}`, mediaID), bob)
	if status != http.StatusBadRequest {
		t.Fatalf("foreign media: status = %d, want 400 (body=%s)", status, body)
	}
	e := testutil.WantErrorCode(t, body, "invalid_request")
	if e.Error.Details.Fields["media"] == "" {
		t.Fatalf("details.fields.media is empty (body=%s)", body)
	}
}

func TestBlockedPostsHiddenAndDelete(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)

	env.CreateUser(t, "did:plc:alice", "alice.test")
	env.Consent(t, "did:plc:alice")
	alice := env.Login(t, "did:plc:alice")
	env.CreateUser(t, "did:plc:bob", "bob.test")
	env.Consent(t, "did:plc:bob")
	bob := env.Login(t, "did:plc:bob")

	testutil.Space(t, env, "sp1", "jp.i32.chain.space.group", "active", "did:plc:alice")
	testutil.Group(t, env, "sp1", true)
	testutil.Member(t, env, "sp1", "did:plc:alice", true)
	testutil.Member(t, env, "sp1", "did:plc:bob", true)

	status, body := testutil.Request(t, app, http.MethodPost, "/api/v1/posts", `{"spaceIds":["sp1"],"text":"alice post"}`, alice)
	if status != http.StatusCreated {
		t.Fatalf("alice post: status = %d (body=%s)", status, body)
	}
	var aliceCreated struct {
		Items []struct {
			URI string `json:"uri"`
		} `json:"items"`
	}
	testutil.MustJSONBody(t, body, &aliceCreated)

	status, body = testutil.Request(t, app, http.MethodPost, "/api/v1/posts", `{"spaceIds":["sp1"],"text":"bob post"}`, bob)
	if status != http.StatusCreated {
		t.Fatalf("bob post: status = %d (body=%s)", status, body)
	}

	// ブロック前は 2 件。
	if page := listPosts(t, app, alice, "sp1"); len(page.Items) != 2 {
		t.Fatalf("before block: %d posts, want 2", len(page.Items))
	}

	testutil.Exec(t, env, `INSERT INTO blocks (blocker, blocked, created_at) VALUES (?, ?, ?)`,
		"did:plc:alice", "did:plc:bob", env.Clock.Now().UnixMilli())

	page := listPosts(t, app, alice, "sp1")
	if len(page.Items) != 1 || page.Items[0].Text != "alice post" {
		t.Fatalf("after block: %+v, want only alice post", page.Items)
	}

	// 自分の投稿を削除すると一覧から消える（10 §4.3）。
	rkey := rkeyOf(t, aliceCreated.Items[0].URI)
	status, body = testutil.Request(t, app, http.MethodDelete, "/api/v1/instances/sp1/posts/"+rkey, "", alice)
	if status != http.StatusNoContent {
		t.Fatalf("delete: status = %d, want 204 (body=%s)", status, body)
	}
	if page := listPosts(t, app, alice, "sp1"); len(page.Items) != 0 {
		t.Fatalf("after delete: %+v, want empty", page.Items)
	}
}

type replyPostItem struct {
	URI   string `json:"uri"`
	Text  string `json:"text"`
	Reply *struct {
		Root   string `json:"root"`
		Parent string `json:"parent"`
	} `json:"reply"`
}

type instancePostItem struct {
	URI        string `json:"uri"`
	Text       string `json:"text"`
	ReplyCount int    `json:"replyCount"`
	Reply      *struct {
		Root string `json:"root"`
	} `json:"reply"`
}

type instancePostPage struct {
	Items  []instancePostItem `json:"items"`
	Cursor string             `json:"cursor"`
}

func listPostsWithQuery(t *testing.T, app *fiber.App, token, spaceID, query string) instancePostPage {
	t.Helper()
	status, body := testutil.Request(t, app, http.MethodGet, "/api/v1/instances/"+spaceID+"/posts?"+query, "", token)
	if status != http.StatusOK {
		t.Fatalf("list posts: status = %d, want 200 (body=%s)", status, body)
	}
	var page instancePostPage
	testutil.MustJSONBody(t, body, &page)
	return page
}

func insertReply(t *testing.T, env *testutil.Env, spaceID, typ, author, rkey, text, root string, at time.Time, modLabel string) string {
	t.Helper()
	uri := testutil.InsertInstancePost(t, env, spaceID, typ, author, rkey, text, at, modLabel)
	value := fmt.Sprintf(`{"text":%q,"format":"plain","reply":{"root":%q,"parent":%q},"createdAt":%q}`,
		text, root, root, at.UTC().Format(time.RFC3339))
	testutil.Exec(t, env, `UPDATE records SET value_json = ? WHERE uri = ?`, value, uri)
	return uri
}

func createPost(t *testing.T, app *fiber.App, token, spaceID, text string) string {
	t.Helper()
	status, body := testutil.Request(t, app, http.MethodPost, "/api/v1/posts",
		fmt.Sprintf(`{"spaceIds":[%q],"text":%q}`, spaceID, text), token)
	if status != http.StatusCreated {
		t.Fatalf("create post %q: status = %d (body=%s)", text, status, body)
	}
	var created struct {
		Items []struct {
			URI string `json:"uri"`
		} `json:"items"`
	}
	testutil.MustJSONBody(t, body, &created)
	return created.Items[0].URI
}

func replyPosts(t *testing.T, app *fiber.App, token, spaceID string) []replyPostItem {
	t.Helper()
	status, body := testutil.Request(t, app, http.MethodGet, "/api/v1/instances/"+spaceID+"/posts", "", token)
	if status != http.StatusOK {
		t.Fatalf("list posts: status = %d (body=%s)", status, body)
	}
	var page struct {
		Items []replyPostItem `json:"items"`
	}
	testutil.MustJSONBody(t, body, &page)
	return page.Items
}

// 返信（10 §3）の API。
func TestCreatePostReply(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)

	env.CreateUser(t, "did:plc:alice", "alice.test")
	env.Consent(t, "did:plc:alice")
	alice := env.Login(t, "did:plc:alice")
	testutil.Space(t, env, "sp1", "jp.i32.chain.space.group", "active", "did:plc:alice")
	testutil.Group(t, env, "sp1", true)
	testutil.Member(t, env, "sp1", "did:plc:alice", true)

	root := createPost(t, app, alice, "sp1", "根の投稿")

	status, body := testutil.Request(t, app, http.MethodPost, "/api/v1/posts",
		fmt.Sprintf(`{"spaceIds":["sp1"],"text":"返信","reply":{"root":%q,"parent":%q}}`, root, root), alice)
	if status != http.StatusCreated {
		t.Fatalf("reply: status = %d, want 201 (body=%s)", status, body)
	}

	items := replyPosts(t, app, alice, "sp1")
	var found bool
	for _, it := range items {
		if it.Text == "返信" {
			found = true
			if it.Reply == nil || it.Reply.Root != root || it.Reply.Parent != root {
				t.Fatalf("reply = %+v, want root/parent %s", it.Reply, root)
			}
		}
	}
	if !found {
		t.Fatalf("reply が一覧にない: %+v", items)
	}
}

func TestCreatePostReplyErrors(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)

	env.CreateUser(t, "did:plc:alice", "alice.test")
	env.Consent(t, "did:plc:alice")
	alice := env.Login(t, "did:plc:alice")
	testutil.Space(t, env, "sp1", "jp.i32.chain.space.group", "active", "did:plc:alice")
	testutil.Group(t, env, "sp1", true)
	testutil.Member(t, env, "sp1", "did:plc:alice", true)
	testutil.Space(t, env, "sp2", "jp.i32.chain.space.group", "active", "did:plc:alice")
	testutil.Group(t, env, "sp2", true)
	testutil.Member(t, env, "sp2", "did:plc:alice", true)

	root1 := createPost(t, app, alice, "sp1", "根1")
	root2 := createPost(t, app, alice, "sp1", "根2")
	foreign := createPost(t, app, alice, "sp2", "別 Space の根")

	// parent の root と指定 root が不一致 → 400。
	status, body := testutil.Request(t, app, http.MethodPost, "/api/v1/posts",
		fmt.Sprintf(`{"spaceIds":["sp1"],"text":"x","reply":{"root":%q,"parent":%q}}`, root1, root2), alice)
	if status != http.StatusBadRequest {
		t.Fatalf("mismatch: status = %d, want 400 (body=%s)", status, body)
	}
	testutil.WantErrorCode(t, body, "invalid_request")

	// 別 Space の root → 400。
	status, body = testutil.Request(t, app, http.MethodPost, "/api/v1/posts",
		fmt.Sprintf(`{"spaceIds":["sp1"],"text":"x","reply":{"root":%q,"parent":%q}}`, foreign, foreign), alice)
	if status != http.StatusBadRequest {
		t.Fatalf("foreign root: status = %d, want 400 (body=%s)", status, body)
	}

	// spaceIds 2 件で reply → 400。
	status, body = testutil.Request(t, app, http.MethodPost, "/api/v1/posts",
		fmt.Sprintf(`{"spaceIds":["sp1","sp2"],"text":"x","reply":{"root":%q,"parent":%q}}`, root1, root1), alice)
	if status != http.StatusBadRequest {
		t.Fatalf("two spaces: status = %d, want 400 (body=%s)", status, body)
	}
	e := testutil.WantErrorCode(t, body, "invalid_request")
	if e.Error.Details.Fields["reply"] == "" {
		t.Fatalf("details.fields.reply is empty (body=%s)", body)
	}
}

// 10 §4.2 の roots / replyCount と、返信の可視性を確認する。
func TestListPostsRootsAndReplyCount(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)
	const space = "sp-replies"
	const typ = "jp.i32.chain.space.group"
	users := []string{"did:plc:root", "did:plc:visible", "did:plc:deleted", "did:plc:moderated", "did:plc:blocked"}
	for _, did := range users {
		env.CreateUser(t, did, strings.TrimPrefix(did, "did:plc:")+".test")
		env.Consent(t, did)
	}
	alice := env.Login(t, users[0])
	testutil.Space(t, env, space, typ, "active", users[0])
	testutil.Group(t, env, space, true)
	for _, did := range users {
		testutil.Member(t, env, space, did, true)
	}
	now := env.Clock.Now()
	root := testutil.InsertInstancePost(t, env, space, typ, users[0], "root", "root", now, "ok")
	insertReply(t, env, space, typ, users[1], "visible", "visible", root, now.Add(time.Minute), "ok")
	deleted := insertReply(t, env, space, typ, users[2], "deleted", "deleted", root, now.Add(2*time.Minute), "ok")
	testutil.Exec(t, env, `UPDATE records SET deleted_at = ? WHERE uri = ?`, now.UnixMilli(), deleted)
	insertReply(t, env, space, typ, users[3], "moderated", "moderated", root, now.Add(3*time.Minute), "block")
	insertReply(t, env, space, typ, users[4], "blocked", "blocked", root, now.Add(4*time.Minute), "ok")
	// Alice -> blocked と blocked -> Alice のどちら向きも replyCount から除外する。
	testutil.Exec(t, env, `INSERT INTO blocks (blocker, blocked, created_at) VALUES (?, ?, ?)`, users[0], users[4], now.UnixMilli())
	testutil.Exec(t, env, `INSERT INTO blocks (blocker, blocked, created_at) VALUES (?, ?, ?)`, users[4], users[0], now.UnixMilli())

	all := listPostsWithQuery(t, app, alice, space, "")
	if len(all.Items) != 2 {
		t.Fatalf("roots なしの件数 = %d, want visible root and reply (items=%+v)", len(all.Items), all.Items)
	}
	for _, item := range all.Items {
		if item.URI == root && item.ReplyCount != 1 {
			t.Fatalf("replyCount = %d, want 1 (item=%+v)", item.ReplyCount, item)
		}
	}
	roots := listPostsWithQuery(t, app, alice, space, "roots=1")
	if len(roots.Items) != 1 || roots.Items[0].URI != root || roots.Items[0].Reply != nil {
		t.Fatalf("roots=1 = %+v, want root only", roots.Items)
	}
}

// 10 §4.2.1 の順序・cursor・root 解決・404 と read 判定を確認する。
func TestThreadAPI(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)
	const typ = "jp.i32.chain.space.group"
	const space = "sp-thread"
	aliceDID, bobDID := "did:plc:thread-alice", "did:plc:thread-bob"
	env.CreateUser(t, aliceDID, "thread-alice.test")
	env.CreateUser(t, bobDID, "thread-bob.test")
	env.Consent(t, aliceDID)
	alice := env.Login(t, aliceDID)
	testutil.Space(t, env, space, typ, "active", aliceDID)
	testutil.Group(t, env, space, true)
	testutil.Member(t, env, space, aliceDID, true)
	testutil.Member(t, env, space, bobDID, true)
	now := env.Clock.Now()
	root := testutil.InsertInstancePost(t, env, space, typ, aliceDID, "thread-root", "root", now, "ok")
	old := insertReply(t, env, space, typ, aliceDID, "old", "old", root, now.Add(time.Minute), "ok")
	blockedReply := insertReply(t, env, space, typ, bobDID, "blocked", "blocked", root, now.Add(90*time.Second), "ok")
	newReply := insertReply(t, env, space, typ, aliceDID, "new", "new", root, now.Add(2*time.Minute), "ok")
	testutil.Exec(t, env, `INSERT INTO blocks (blocker, blocked, created_at) VALUES (?, ?, ?)`, aliceDID, bobDID, now.UnixMilli())
	thread := func(rootURI, limit string) (int, string, []instancePostItem) {
		query := "root=" + url.QueryEscape(rootURI)
		if limit != "" {
			query += "&limit=" + limit
		}
		status, body := testutil.Request(t, app, http.MethodGet, "/api/v1/instances/"+space+"/thread?"+query, "", alice)
		if status != http.StatusOK {
			t.Fatalf("thread %s: status = %d (body=%s)", query, status, body)
		}
		var page struct {
			Root    instancePostItem   `json:"root"`
			Replies []instancePostItem `json:"replies"`
			Cursor  string             `json:"cursor"`
		}
		testutil.MustJSONBody(t, body, &page)
		return status, page.Cursor, page.Replies
	}
	_, cursor, replies := thread(root, "1")
	if len(replies) != 1 || replies[0].URI != old || cursor == "" {
		t.Fatalf("thread first page = replies=%+v cursor=%q", replies, cursor)
	}
	status, body := testutil.Request(t, app, http.MethodGet, "/api/v1/instances/"+space+"/thread?root="+url.QueryEscape(root)+"&limit=1&cursor="+url.QueryEscape(cursor), "", alice)
	if status != http.StatusOK {
		t.Fatalf("thread second page: status = %d (body=%s)", status, body)
	}
	var second struct {
		Replies []instancePostItem `json:"replies"`
		Cursor  string             `json:"cursor"`
	}
	testutil.MustJSONBody(t, body, &second)
	if len(second.Replies) != 0 {
		t.Fatalf("thread second page = %+v", second.Replies)
	}
	status, body = testutil.Request(t, app, http.MethodGet, "/api/v1/instances/"+space+"/thread?root="+url.QueryEscape(root)+"&limit=1&cursor="+url.QueryEscape(second.Cursor), "", alice)
	if status != http.StatusOK {
		t.Fatalf("thread third page: status = %d (body=%s)", status, body)
	}
	var third struct {
		Replies []instancePostItem `json:"replies"`
	}
	testutil.MustJSONBody(t, body, &third)
	if len(third.Replies) != 1 || third.Replies[0].URI != newReply || third.Replies[0].URI == blockedReply {
		t.Fatalf("thread third page = %+v", third.Replies)
	}
	_, _, byReply := thread(old, "")
	if len(byReply) != 2 || byReply[0].URI != old || byReply[1].URI != newReply {
		t.Fatalf("reply URI as root = %+v, want root thread", byReply)
	}

	foreignSpace := "sp-thread-foreign"
	testutil.Space(t, env, foreignSpace, typ, "active", aliceDID)
	testutil.Group(t, env, foreignSpace, true)
	foreignRoot := testutil.InsertInstancePost(t, env, foreignSpace, typ, aliceDID, "foreign-root", "foreign", now, "ok")
	for _, bad := range []string{"at://missing", foreignRoot} {
		status, _ := testutil.Request(t, app, http.MethodGet, "/api/v1/instances/"+space+"/thread?root="+url.QueryEscape(bad), "", alice)
		if status != http.StatusNotFound {
			t.Fatalf("missing root %s: status = %d, want 404", bad, status)
		}
	}
	testutil.Exec(t, env, `UPDATE records SET deleted_at = ? WHERE uri = ?`, now.UnixMilli(), root)
	status, _ = testutil.Request(t, app, http.MethodGet, "/api/v1/instances/"+space+"/thread?root="+url.QueryEscape(root), "", alice)
	if status != http.StatusNotFound {
		t.Fatalf("deleted root: status = %d, want 404", status)
	}

	// 仮 party、離脱済みメンバー、archived は read 可。メンバー外は 403。
	for _, tc := range []struct {
		id, state, phase string
		left             bool
	}{
		{"party-tentative", "active", "tentative", false},
		{"party-left", "active", "confirmed", true},
		{"party-archived", "archived", "ended", true},
	} {
		testutil.Space(t, env, tc.id, "jp.i32.chain.space.party", tc.state, aliceDID)
		testutil.Exec(t, env, `INSERT INTO parties (space_id, phase, tentative_at, confirmed_at, last_activity_at) VALUES (?, ?, ?, ?, ?)`, tc.id, tc.phase, now.UnixMilli(), now.UnixMilli(), now.UnixMilli())
		if tc.left {
			testutil.Exec(t, env, `INSERT INTO space_members (space_id, did, can_read, can_write, joined_at, left_at) VALUES (?, ?, 1, 0, ?, ?)`, tc.id, aliceDID, now.UnixMilli(), now.UnixMilli())
		} else {
			testutil.Member(t, env, tc.id, aliceDID, false)
		}
		partyRoot := testutil.InsertInstancePost(t, env, tc.id, "jp.i32.chain.space.party", aliceDID, tc.id+"-root", "party root", now, "ok")
		status, _ = testutil.Request(t, app, http.MethodGet, "/api/v1/instances/"+tc.id+"/thread?root="+url.QueryEscape(partyRoot), "", alice)
		if status != http.StatusOK {
			t.Fatalf("%s read: status = %d, want 200", tc.id, status)
		}
	}
	testutil.Space(t, env, "sp-outsider", typ, "active", aliceDID)
	testutil.Group(t, env, "sp-outsider", true)
	outsiderRoot := testutil.InsertInstancePost(t, env, "sp-outsider", typ, aliceDID, "outsider-root", "root", now, "ok")
	status, _ = testutil.Request(t, app, http.MethodGet, "/api/v1/instances/sp-outsider/thread?root="+url.QueryEscape(outsiderRoot), "", alice)
	if status != http.StatusForbidden {
		t.Fatalf("non-member read: status = %d, want 403", status)
	}
}
