//go:build integration

package content_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/applemango/chain/internal/external/atproto"
	contentsvc "github.com/applemango/chain/internal/service/content"
	"github.com/applemango/chain/internal/testutil"
)

// 公開投稿の API（17 §2.1・§2.2）と POST /posts の alsoPublic（10 §4.1）。

type feedPostCreated struct {
	URI  string `json:"uri"`
	CID  string `json:"cid"`
	Bsky *struct {
		URI string `json:"uri"`
	} `json:"bsky"`
}

type createdPostsResponse struct {
	Items []struct {
		URI string `json:"uri"`
	} `json:"items"`
	Public *struct {
		URI   string `json:"uri"`
		Error string `json:"error"`
	} `json:"public"`
}

func TestFeedPostCreate(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)
	env.CreateUser(t, "did:plc:alice", "alice.test")
	alice := env.Login(t, "did:plc:alice")

	status, body := testutil.Request(t, app, http.MethodPost, "/api/v1/feed/posts",
		`{"text":"今日はいい天気","langs":["ja"],"alsoBluesky":true}`, alice)
	if status != http.StatusCreated {
		t.Fatalf("create: status = %d, want 201 (body=%s)", status, body)
	}
	var res feedPostCreated
	testutil.MustJSONBody(t, body, &res)
	if !strings.Contains(res.URI, "/"+contentsvc.CollectionFeedPost+"/") || res.CID == "" {
		t.Fatalf("res = %+v", res)
	}
	if res.Bsky == nil || !strings.Contains(res.Bsky.URI, "/"+contentsvc.CollectionBskyPost+"/") {
		t.Fatalf("bsky = %+v, want a %s uri", res.Bsky, contentsvc.CollectionBskyPost)
	}

	calls := env.Writer.CreateCalls()
	if len(calls) != 2 || calls[0].Collection != contentsvc.CollectionBskyPost || calls[1].Collection != contentsvc.CollectionFeedPost {
		t.Fatalf("create calls = %+v", calls)
	}
}

func TestFeedPostCreateWithoutBluesky(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)
	env.CreateUser(t, "did:plc:alice", "alice.test")
	alice := env.Login(t, "did:plc:alice")

	status, body := testutil.Request(t, app, http.MethodPost, "/api/v1/feed/posts", `{"text":"ひとりごと"}`, alice)
	if status != http.StatusCreated {
		t.Fatalf("create: status = %d, want 201 (body=%s)", status, body)
	}
	var res feedPostCreated
	testutil.MustJSONBody(t, body, &res)
	if res.Bsky != nil {
		t.Fatalf("bsky = %+v, want none", res.Bsky)
	}
	if calls := env.Writer.CreateCalls(); len(calls) != 1 {
		t.Fatalf("create calls = %+v, want 1", calls)
	}
}

func TestFeedPostValidation(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)
	env.CreateUser(t, "did:plc:alice", "alice.test")
	alice := env.Login(t, "did:plc:alice")

	long := strings.Repeat("あ", 1001)
	cases := []struct {
		name  string
		body  string
		field string
	}{
		{"1001 graphemes", `{"text":"` + long + `"}`, "text"},
		{"empty text", `{"text":""}`, "text"},
		{"4 langs", `{"text":"hi","langs":["ja","en","fr","de"]}`, "langs"},
		{"bad lang", `{"text":"hi","langs":["!!"]}`, "langs.0"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, body := testutil.Request(t, app, http.MethodPost, "/api/v1/feed/posts", tc.body, alice)
			if status != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (body=%s)", status, body)
			}
			e := testutil.WantErrorCode(t, body, "invalid_request")
			if e.Error.Details.Fields[tc.field] == "" {
				t.Fatalf("details.fields[%q] is empty (body=%s)", tc.field, body)
			}
		})
	}

	// 1000 grapheme は通る。
	ok := strings.Repeat("あ", 1000)
	status, body := testutil.Request(t, app, http.MethodPost, "/api/v1/feed/posts", `{"text":"`+ok+`"}`, alice)
	if status != http.StatusCreated {
		t.Fatalf("1000 graphemes: status = %d, want 201 (body=%s)", status, body)
	}
}

func TestFeedPostModerationBlocked(t *testing.T) {
	env := testutil.NewEnv(t)
	env.Moderator = &testutil.FakeAssessor{BlockWords: []string{"誹謗"}}
	app := testutil.NewApp(env)
	env.CreateUser(t, "did:plc:alice", "alice.test")
	alice := env.Login(t, "did:plc:alice")

	status, body := testutil.Request(t, app, http.MethodPost, "/api/v1/feed/posts", `{"text":"これは誹謗です"}`, alice)
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422 (body=%s)", status, body)
	}
	testutil.WantErrorCode(t, body, "moderation_blocked")
	if len(env.Writer.CreateCalls()) != 0 {
		t.Fatalf("writer was called: %+v", env.Writer.CreateCalls())
	}
}

func TestFeedPostWriteErrors(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		wantStatus int
		code       string
	}{
		{"session expired", atproto.ErrSessionExpired, http.StatusUnauthorized, "atproto_session_expired"},
		{"reauth required", atproto.ErrReauthRequired, http.StatusForbidden, "atproto_reauth_required"},
		{"upstream", errors.New("pds down"), http.StatusBadGateway, "upstream_error"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := testutil.NewEnv(t)
			app := testutil.NewApp(env)
			env.CreateUser(t, "did:plc:alice", "alice.test")
			alice := env.Login(t, "did:plc:alice")
			env.Writer.SetCreateError(contentsvc.CollectionFeedPost, tc.err)

			status, body := testutil.Request(t, app, http.MethodPost, "/api/v1/feed/posts", `{"text":"hi"}`, alice)
			if status != tc.wantStatus {
				t.Fatalf("status = %d, want %d (body=%s)", status, tc.wantStatus, body)
			}
			testutil.WantErrorCode(t, body, tc.code)
		})
	}
}

func TestFeedPostDelete(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)
	env.CreateUser(t, "did:plc:alice", "alice.test")
	alice := env.Login(t, "did:plc:alice")
	env.CreateUser(t, "did:plc:bob", "bob.test")
	bob := env.Login(t, "did:plc:bob")

	status, body := testutil.Request(t, app, http.MethodPost, "/api/v1/feed/posts", `{"text":"消す投稿","alsoBluesky":true}`, alice)
	if status != http.StatusCreated {
		t.Fatalf("create: status = %d (body=%s)", status, body)
	}
	var res feedPostCreated
	testutil.MustJSONBody(t, body, &res)
	rkey := rkeyOf(t, res.URI)

	status, body = testutil.Request(t, app, http.MethodDelete, "/api/v1/feed/posts/"+rkey, "", alice)
	if status != http.StatusNoContent {
		t.Fatalf("delete: status = %d, want 204 (body=%s)", status, body)
	}
	deletes := env.Writer.DeleteCalls()
	if len(deletes) != 2 || deletes[0].Collection != contentsvc.CollectionFeedPost || deletes[1].Collection != contentsvc.CollectionBskyPost {
		t.Fatalf("delete calls = %+v", deletes)
	}

	// 他人の投稿は 404。
	status, body = testutil.Request(t, app, http.MethodDelete, "/api/v1/feed/posts/"+rkey, "", bob)
	if status != http.StatusNotFound {
		t.Fatalf("foreign delete: status = %d, want 404 (body=%s)", status, body)
	}
	testutil.WantErrorCode(t, body, "not_found")
}

func TestInstancePostAlsoPublic(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)
	env.CreateUser(t, "did:plc:alice", "alice.test")
	env.Consent(t, "did:plc:alice")
	alice := env.Login(t, "did:plc:alice")
	testutil.Space(t, env, "sp1", "jp.i32.chain.space.group", "active", "did:plc:alice")
	testutil.Group(t, env, "sp1", true)
	testutil.Member(t, env, "sp1", "did:plc:alice", true)

	status, body := testutil.Request(t, app, http.MethodPost, "/api/v1/posts",
		`{"spaceIds":["sp1"],"text":"公開もする","alsoPublic":true}`, alice)
	if status != http.StatusCreated {
		t.Fatalf("alsoPublic: status = %d, want 201 (body=%s)", status, body)
	}
	var res createdPostsResponse
	testutil.MustJSONBody(t, body, &res)
	if len(res.Items) != 1 || !strings.Contains(res.Items[0].URI, "/jp.i32.chain.instance.post/") {
		t.Fatalf("items = %+v", res.Items)
	}
	if res.Public == nil || res.Public.URI == "" || res.Public.Error != "" {
		t.Fatalf("public = %+v, want uri and no error", res.Public)
	}
}

func TestInstancePostAlsoPublicFailure(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)
	env.CreateUser(t, "did:plc:alice", "alice.test")
	env.Consent(t, "did:plc:alice")
	alice := env.Login(t, "did:plc:alice")
	testutil.Space(t, env, "sp1", "jp.i32.chain.space.group", "active", "did:plc:alice")
	testutil.Group(t, env, "sp1", true)
	testutil.Member(t, env, "sp1", "did:plc:alice", true)
	env.Writer.SetCreateError(contentsvc.CollectionFeedPost, errors.New("pds down"))

	status, body := testutil.Request(t, app, http.MethodPost, "/api/v1/posts",
		`{"spaceIds":["sp1"],"text":"公開だけ失敗","alsoPublic":true}`, alice)
	if status != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body=%s)", status, body)
	}
	var res createdPostsResponse
	testutil.MustJSONBody(t, body, &res)
	if len(res.Items) != 1 {
		t.Fatalf("items = %+v, want the instance post", res.Items)
	}
	if res.Public == nil || res.Public.Error != "upstream_error" {
		t.Fatalf("public = %+v, want error upstream_error", res.Public)
	}
}

// selfLabels は feed.post と app.bsky.feed.post の labels に書く（19 §2.15.3）。
func TestFeedPostSelfLabels(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)
	env.CreateUser(t, "did:plc:alice", "alice.test")
	alice := env.Login(t, "did:plc:alice")

	status, body := testutil.Request(t, app, http.MethodPost, "/api/v1/feed/posts",
		`{"text":"ネタバレあり","alsoBluesky":true,"selfLabels":["spoiler","violence","spoiler"]}`, alice)
	if status != http.StatusCreated {
		t.Fatalf("create: status = %d, want 201 (body=%s)", status, body)
	}
	calls := env.Writer.CreateCalls()
	if len(calls) != 2 {
		t.Fatalf("create calls = %d, want 2", len(calls))
	}
	for _, c := range calls {
		rec, _ := c.Record.(map[string]any)
		labels, _ := rec["labels"].(map[string]any)
		if labels["$type"] != "com.atproto.label.defs#selfLabels" {
			t.Fatalf("%s labels = %+v", c.Collection, rec["labels"])
		}
		raw, _ := json.Marshal(labels["values"])
		if string(raw) != `[{"val":"spoiler"},{"val":"violence"}]` {
			t.Fatalf("%s values = %s", c.Collection, raw)
		}
	}

	// ラベルなしの投稿には labels を書かない。
	status, _ = testutil.Request(t, app, http.MethodPost, "/api/v1/feed/posts", `{"text":"ふつう"}`, alice)
	if status != http.StatusCreated {
		t.Fatalf("plain create: status = %d", status)
	}
	all := env.Writer.CreateCalls()
	rec, _ := all[len(all)-1].Record.(map[string]any)
	if _, ok := rec["labels"]; ok {
		t.Fatalf("labels should be absent: %+v", rec)
	}
}

func TestFeedPostSelfLabelsInvalid(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)
	env.CreateUser(t, "did:plc:alice", "alice.test")
	env.Consent(t, "did:plc:alice")
	alice := env.Login(t, "did:plc:alice")
	testutil.Space(t, env, "sp1", "jp.i32.chain.space.group", "active", "did:plc:alice")
	testutil.Group(t, env, "sp1", true)
	testutil.Member(t, env, "sp1", "did:plc:alice", true)

	for _, tc := range []struct{ path, body string }{
		{"/api/v1/feed/posts", `{"text":"x","selfLabels":["porn"]}`},
		{"/api/v1/feed/posts", `{"text":"x","selfLabels":["spoiler",""]}`},
		{"/api/v1/posts", `{"spaceIds":["sp1"],"text":"x","alsoPublic":true,"selfLabels":["nudity"]}`},
	} {
		status, body := testutil.Request(t, app, http.MethodPost, tc.path, tc.body, alice)
		if status != http.StatusBadRequest {
			t.Fatalf("%s %s: status = %d, want 400 (body=%s)", tc.path, tc.body, status, body)
		}
		e := testutil.WantErrorCode(t, body, "invalid_request")
		if e.Error.Details.Fields["selfLabels"] == "" {
			t.Fatalf("details.fields[selfLabels] is empty (body=%s)", body)
		}
	}
	if n := len(env.Writer.CreateCalls()); n != 0 {
		t.Fatalf("create calls = %d, want 0", n)
	}
}

func TestInstancePostAlsoPublicSelfLabels(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)
	env.CreateUser(t, "did:plc:alice", "alice.test")
	env.Consent(t, "did:plc:alice")
	alice := env.Login(t, "did:plc:alice")
	testutil.Space(t, env, "sp1", "jp.i32.chain.space.group", "active", "did:plc:alice")
	testutil.Group(t, env, "sp1", true)
	testutil.Member(t, env, "sp1", "did:plc:alice", true)

	status, body := testutil.Request(t, app, http.MethodPost, "/api/v1/posts",
		`{"spaceIds":["sp1"],"text":"公開もする","alsoPublic":true,"selfLabels":["self_harm"]}`, alice)
	if status != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body=%s)", status, body)
	}
	var found bool
	for _, c := range env.Writer.CreateCalls() {
		if c.Collection != contentsvc.CollectionFeedPost {
			continue
		}
		found = true
		rec, _ := c.Record.(map[string]any)
		raw, _ := json.Marshal(rec["labels"])
		if string(raw) != `{"$type":"com.atproto.label.defs#selfLabels","values":[{"val":"self_harm"}]}` {
			t.Fatalf("labels = %s", raw)
		}
	}
	if !found {
		t.Fatal("feed.post was not written")
	}
}
