//go:build integration

package content_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

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
