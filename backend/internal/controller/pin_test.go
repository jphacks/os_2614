//go:build integration

package controller_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/applemango/chain/internal/testutil"
)

// Pin の API（10 §6）。

func TestPinAPI(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)

	env.CreateUser(t, "did:plc:alice", "alice.test")
	env.Consent(t, "did:plc:alice")
	alice := env.Login(t, "did:plc:alice")
	space(t, env, "sp1", "jp.i32.chain.space.group", "active", "did:plc:alice")
	group(t, env, "sp1", true)
	member(t, env, "sp1", "did:plc:alice", true)

	status, body := request(t, app, http.MethodPost, "/api/v1/posts", `{"spaceIds":["sp1"],"text":"固定する投稿"}`, alice)
	if status != http.StatusCreated {
		t.Fatalf("create post: status = %d (body=%s)", status, body)
	}
	var created struct {
		Items []struct {
			URI string `json:"uri"`
		} `json:"items"`
	}
	mustJSONBody(t, body, &created)
	uri := created.Items[0].URI

	// 手動 Pin。
	status, body = request(t, app, http.MethodPost, "/api/v1/instances/sp1/pins", `{"subject":"`+uri+`"}`, alice)
	if status != http.StatusCreated {
		t.Fatalf("create pin: status = %d, want 201 (body=%s)", status, body)
	}
	var pin struct {
		URI     string `json:"uri"`
		Subject string `json:"subject"`
		Reason  string `json:"reason"`
	}
	mustJSONBody(t, body, &pin)
	if pin.Subject != uri || pin.Reason != "manual" {
		t.Fatalf("pin = %+v", pin)
	}

	// 一覧。
	status, body = request(t, app, http.MethodGet, "/api/v1/instances/sp1/pins", "", alice)
	if status != http.StatusOK {
		t.Fatalf("list pins: status = %d (body=%s)", status, body)
	}
	var list struct {
		Items []struct {
			URI     string `json:"uri"`
			Subject string `json:"subject"`
			Reason  string `json:"reason"`
		} `json:"items"`
	}
	mustJSONBody(t, body, &list)
	if len(list.Items) != 1 || list.Items[0].Subject != uri {
		t.Fatalf("pins = %+v, want 1 for %s", list.Items, uri)
	}

	// 解除。
	rkey := rkeyOf(t, pin.URI)
	status, body = request(t, app, http.MethodDelete, "/api/v1/instances/sp1/pins/"+rkey, "", alice)
	if status != http.StatusNoContent {
		t.Fatalf("delete pin: status = %d, want 204 (body=%s)", status, body)
	}
	status, body = request(t, app, http.MethodGet, "/api/v1/instances/sp1/pins", "", alice)
	if status != http.StatusOK {
		t.Fatalf("list after delete: status = %d (body=%s)", status, body)
	}
	mustJSONBody(t, body, &list)
	if len(list.Items) != 0 {
		t.Fatalf("pins after delete = %+v, want empty", list.Items)
	}
}

func TestPinAPIErrors(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)

	env.CreateUser(t, "did:plc:alice", "alice.test")
	env.Consent(t, "did:plc:alice")
	alice := env.Login(t, "did:plc:alice")
	space(t, env, "sp1", "jp.i32.chain.space.group", "active", "did:plc:alice")
	group(t, env, "sp1", true)
	member(t, env, "sp1", "did:plc:alice", true)

	env.CreateUser(t, "did:plc:bob", "bob.test")
	bob := env.Login(t, "did:plc:bob")

	status, body := request(t, app, http.MethodPost, "/api/v1/posts", `{"spaceIds":["sp1"],"text":"x"}`, alice)
	if status != http.StatusCreated {
		t.Fatalf("create post: status = %d (body=%s)", status, body)
	}
	var created struct {
		Items []struct {
			URI string `json:"uri"`
		} `json:"items"`
	}
	mustJSONBody(t, body, &created)
	uri := created.Items[0].URI

	// メンバー以外は write 不可 → 403。
	status, body = request(t, app, http.MethodPost, "/api/v1/instances/sp1/pins", `{"subject":"`+uri+`"}`, bob)
	if status != http.StatusForbidden {
		t.Fatalf("outsider: status = %d, want 403 (body=%s)", status, body)
	}

	// 二重 Pin → 409。
	if status, body = request(t, app, http.MethodPost, "/api/v1/instances/sp1/pins", `{"subject":"`+uri+`"}`, alice); status != http.StatusCreated {
		t.Fatalf("first pin: status = %d (body=%s)", status, body)
	}
	status, body = request(t, app, http.MethodPost, "/api/v1/instances/sp1/pins", `{"subject":"`+uri+`"}`, alice)
	if status != http.StatusConflict {
		t.Fatalf("double pin: status = %d, want 409 (body=%s)", status, body)
	}

	// 別 Space の投稿 → 400。
	space(t, env, "sp2", "jp.i32.chain.space.group", "active", "did:plc:alice")
	group(t, env, "sp2", true)
	member(t, env, "sp2", "did:plc:alice", true)
	foreign := "at://did:web:chain.test/space/jp.i32.chain.space.group/sp2/did:plc:alice/jp.i32.chain.instance.post/abc"
	status, body = request(t, app, http.MethodPost, "/api/v1/instances/sp1/pins", `{"subject":"`+foreign+`"}`, alice)
	if status != http.StatusBadRequest {
		t.Fatalf("foreign subject: status = %d, want 400 (body=%s)", status, body)
	}
	wantErrorCode(t, body, "invalid_request")

	// subject なし → 400。
	status, body = request(t, app, http.MethodPost, "/api/v1/instances/sp1/pins", `{}`, alice)
	if status != http.StatusBadRequest {
		t.Fatalf("no subject: status = %d, want 400 (body=%s)", status, body)
	}
	var e apiError
	_ = json.Unmarshal(body, &e)
	if e.Error.Details.Fields["subject"] == "" {
		t.Fatalf("details.fields.subject is empty (body=%s)", body)
	}
}
