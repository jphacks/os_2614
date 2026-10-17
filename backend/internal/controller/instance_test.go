//go:build integration

package controller_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"

	"github.com/applemango/chain/internal/testutil"
)

// instance の投稿（10 §4.1〜§4.3）の API テスト。

func space(t *testing.T, env *testutil.Env, id, typ, state, createdBy string) {
	t.Helper()
	now := env.Clock.Now().UnixMilli()
	execSQL(t, env, `INSERT INTO spaces (id, type, authority, ref, name, state, created_by, created_at)
		VALUES (?, ?, 'did:web:chain.test', ?, ?, ?, ?, ?)`,
		id, typ, "at://did:web:chain.test/space/"+typ+"/"+id, "space "+id, state, createdBy, now)
}

func group(t *testing.T, env *testutil.Env, id string, persistent bool) {
	t.Helper()
	execSQL(t, env, `INSERT INTO groups (space_id, description, visibility, origin, persistent, center_lat, center_lon, radius_m)
		VALUES (?, '', 'open', 'explicit', ?, 35.0, 135.0, 40)`, id, persistent)
}

func member(t *testing.T, env *testutil.Env, id, did string, canWrite bool) {
	t.Helper()
	execSQL(t, env, `INSERT INTO space_members (space_id, did, can_read, can_write, role, joined_at)
		VALUES (?, ?, 1, ?, 'member', ?)`, id, did, canWrite, env.Clock.Now().UnixMilli())
}

func mustJSONBody(t *testing.T, body []byte, v any) {
	t.Helper()
	if err := json.Unmarshal(body, v); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}
}

type postItem struct {
	Kind   string `json:"kind"`
	URI    string `json:"uri"`
	Text   string `json:"text"`
	Hidden string `json:"hidden"`
}

type postPage struct {
	Items  []postItem `json:"items"`
	Cursor string     `json:"cursor"`
}

func listPosts(t *testing.T, app *fiber.App, token, spaceID string) postPage {
	t.Helper()
	status, body := request(t, app, http.MethodGet, "/api/v1/instances/"+spaceID+"/posts", "", token)
	if status != http.StatusOK {
		t.Fatalf("list posts: status = %d, want 200 (body=%s)", status, body)
	}
	var page postPage
	mustJSONBody(t, body, &page)
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
	space(t, env, "sp1", "jp.i32.chain.space.group", "active", "did:plc:alice")
	group(t, env, "sp1", true)
	member(t, env, "sp1", "did:plc:alice", true)

	status, body := request(t, app, http.MethodPost, "/api/v1/posts",
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
	mustJSONBody(t, body, &created)
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
	status, body = request(t, app, http.MethodGet, "/api/v1/instances", "", token)
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
	mustJSONBody(t, body, &instances)
	if len(instances.Items) != 1 || instances.Items[0].SpaceID != "sp1" || instances.Items[0].Type != "group" {
		t.Fatalf("instances = %+v, want sp1 group", instances.Items)
	}
}

func TestCreatePostErrors(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)

	env.CreateUser(t, "did:plc:alice", "alice.test")
	env.Consent(t, "did:plc:alice")
	alice := env.Login(t, "did:plc:alice")
	space(t, env, "sp1", "jp.i32.chain.space.group", "active", "did:plc:alice")
	group(t, env, "sp1", true)
	member(t, env, "sp1", "did:plc:alice", true)

	// 同意なし（02 §8）。
	env.CreateUser(t, "did:plc:noconsent", "nc.test")
	nc := env.Login(t, "did:plc:noconsent")
	member(t, env, "sp1", "did:plc:noconsent", true)
	status, body := request(t, app, http.MethodPost, "/api/v1/posts", `{"spaceIds":["sp1"],"text":"hi"}`, nc)
	if status != http.StatusForbidden {
		t.Fatalf("no consent: status = %d, want 403 (body=%s)", status, body)
	}
	wantErrorCode(t, body, "consent_required")

	// write 権限なし。
	env.CreateUser(t, "did:plc:outsider", "out.test")
	env.Consent(t, "did:plc:outsider")
	outsider := env.Login(t, "did:plc:outsider")
	status, body = request(t, app, http.MethodPost, "/api/v1/posts", `{"spaceIds":["sp1"],"text":"hi"}`, outsider)
	if status != http.StatusForbidden {
		t.Fatalf("outsider: status = %d, want 403 (body=%s)", status, body)
	}
	wantErrorCode(t, body, "forbidden")

	// archived の Space へは 410。
	space(t, env, "sp-arch", "jp.i32.chain.space.group", "archived", "did:plc:alice")
	member(t, env, "sp-arch", "did:plc:alice", false)
	status, body = request(t, app, http.MethodPost, "/api/v1/posts", `{"spaceIds":["sp-arch"],"text":"hi"}`, alice)
	if status != http.StatusGone {
		t.Fatalf("archived: status = %d, want 410 (body=%s)", status, body)
	}
	wantErrorCode(t, body, "gone")

	// spaceIds が 6 件 → 400。
	many := `{"spaceIds":["1","2","3","4","5","6"],"text":"hi"}`
	status, body = request(t, app, http.MethodPost, "/api/v1/posts", many, alice)
	if status != http.StatusBadRequest {
		t.Fatalf("6 spaces: status = %d, want 400 (body=%s)", status, body)
	}
	e := wantErrorCode(t, body, "invalid_request")
	if e.Error.Details.Fields["spaceIds"] == "" {
		t.Fatalf("details.fields.spaceIds is empty (body=%s)", body)
	}

	// media 付きで spaceIds が 2 件 → 400。
	status, body = request(t, app, http.MethodPost, "/api/v1/posts",
		`{"spaceIds":["sp1","sp-arch"],"text":"hi","media":[{"id":"m1"}]}`, alice)
	if status != http.StatusBadRequest {
		t.Fatalf("media with 2 spaces: status = %d, want 400 (body=%s)", status, body)
	}
	e = wantErrorCode(t, body, "invalid_request")
	if e.Error.Details.Fields["media"] == "" {
		t.Fatalf("details.fields.media is empty (body=%s)", body)
	}

	// alsoPublic は未対応（10 §4.1）。
	status, body = request(t, app, http.MethodPost, "/api/v1/posts",
		`{"spaceIds":["sp1"],"text":"hi","alsoPublic":true}`, alice)
	if status != http.StatusBadRequest {
		t.Fatalf("alsoPublic: status = %d, want 400 (body=%s)", status, body)
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

	space(t, env, "sp-bob", "jp.i32.chain.space.group", "active", "did:plc:bob")
	group(t, env, "sp-bob", true)
	member(t, env, "sp-bob", "did:plc:bob", true)

	mediaID := uploadJPEG(t, app, alice)

	status, body := request(t, app, http.MethodPost, "/api/v1/posts",
		fmt.Sprintf(`{"spaceIds":["sp-bob"],"text":"hi","media":[{"id":%q}]}`, mediaID), bob)
	if status != http.StatusBadRequest {
		t.Fatalf("foreign media: status = %d, want 400 (body=%s)", status, body)
	}
	e := wantErrorCode(t, body, "invalid_request")
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

	space(t, env, "sp1", "jp.i32.chain.space.group", "active", "did:plc:alice")
	group(t, env, "sp1", true)
	member(t, env, "sp1", "did:plc:alice", true)
	member(t, env, "sp1", "did:plc:bob", true)

	status, body := request(t, app, http.MethodPost, "/api/v1/posts", `{"spaceIds":["sp1"],"text":"alice post"}`, alice)
	if status != http.StatusCreated {
		t.Fatalf("alice post: status = %d (body=%s)", status, body)
	}
	var aliceCreated struct {
		Items []struct {
			URI string `json:"uri"`
		} `json:"items"`
	}
	mustJSONBody(t, body, &aliceCreated)

	status, body = request(t, app, http.MethodPost, "/api/v1/posts", `{"spaceIds":["sp1"],"text":"bob post"}`, bob)
	if status != http.StatusCreated {
		t.Fatalf("bob post: status = %d (body=%s)", status, body)
	}

	// ブロック前は 2 件。
	if page := listPosts(t, app, alice, "sp1"); len(page.Items) != 2 {
		t.Fatalf("before block: %d posts, want 2", len(page.Items))
	}

	execSQL(t, env, `INSERT INTO blocks (blocker, blocked, created_at) VALUES (?, ?, ?)`,
		"did:plc:alice", "did:plc:bob", env.Clock.Now().UnixMilli())

	page := listPosts(t, app, alice, "sp1")
	if len(page.Items) != 1 || page.Items[0].Text != "alice post" {
		t.Fatalf("after block: %+v, want only alice post", page.Items)
	}

	// 自分の投稿を削除すると一覧から消える（10 §4.3）。
	rkey := rkeyOf(t, aliceCreated.Items[0].URI)
	status, body = request(t, app, http.MethodDelete, "/api/v1/instances/sp1/posts/"+rkey, "", alice)
	if status != http.StatusNoContent {
		t.Fatalf("delete: status = %d, want 204 (body=%s)", status, body)
	}
	if page := listPosts(t, app, alice, "sp1"); len(page.Items) != 0 {
		t.Fatalf("after delete: %+v, want empty", page.Items)
	}
}
