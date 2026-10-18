//go:build integration

package controller_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/applemango/chain/internal/auth"
	"github.com/applemango/chain/internal/testutil"
)

// 自分の PDS のホスト名（O1）と、データの置き場所（O4。19 §7.1）。

type dataMapBody struct {
	PDSURL  string         `json:"pdsUrl"`
	PDSHost string         `json:"pdsHost"`
	Scopes  []string       `json:"scopes"`
	Counts  map[string]int `json:"counts"`
	Rows    []struct {
		ID        string   `json:"id"`
		Place     string   `json:"place"`
		CountKeys []string `json:"countKeys"`
	} `json:"rows"`
}

func TestMeIncludesPDSHost(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)
	env.CreateUser(t, "did:plc:me", "me.test")
	token := env.Login(t, "did:plc:me")

	status, body := testutil.Request(t, app, http.MethodGet, "/api/v1/me", "", token)
	if status != http.StatusOK {
		t.Fatalf("status = %d (body=%s)", status, body)
	}
	var got struct {
		PDSHost string `json:"pdsHost"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.PDSHost != "pds.example" {
		t.Fatalf("pdsHost = %q, want pds.example（ホスト名だけ）", got.PDSHost)
	}
}

func TestDataMapAPI(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)
	const me, peer = "did:plc:me", "did:plc:peer"
	env.CreateUser(t, me, "me.test")
	env.CreateUser(t, peer, "peer.test")
	token := env.Login(t, me)
	now := env.Clock.Now().UnixMilli()

	testutil.Exec(t, env, `INSERT INTO public_posts (uri, cid, author_did, text, created_at, indexed_at) VALUES
		('at://did:plc:me/jp.i32.chain.feed.post/1', 'c', ?, 'a', ?, ?),
		('at://did:plc:me/jp.i32.chain.feed.post/2', 'c', ?, 'b', ?, ?)`, me, now, now, me, now, now)
	testutil.Exec(t, env, `UPDATE public_posts SET deleted_at = ? WHERE uri LIKE '%/2'`, now)
	a, b := testutil.Pair(me, peer)
	testutil.Exec(t, env, `INSERT INTO encounters (id, user_a, user_b, established_at, first_seen_at, last_seen_at, visibility)
		VALUES ('e1', ?, ?, ?, ?, ?, 'revealed')`, a, b, now, now, now)
	testutil.Exec(t, env, `INSERT INTO friendships (user_a, user_b, created_at, via) VALUES (?, ?, ?, 'invite')`, a, b, now)

	// 暗号化したセッションが無ければ（Login の既定は空）scopes は null。
	status, body := testutil.Request(t, app, http.MethodGet, "/api/v1/me/data-map", "", token)
	if status != http.StatusOK {
		t.Fatalf("status = %d (body=%s)", status, body)
	}
	var got dataMapBody
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode: %v (body=%s)", err, body)
	}
	if got.PDSURL != "https://pds.example" || got.PDSHost != "pds.example" {
		t.Fatalf("pds = %q / %q", got.PDSURL, got.PDSHost)
	}
	if got.Scopes != nil {
		t.Fatalf("scopes = %v, want null", got.Scopes)
	}
	if got.Counts["publicPosts"] != 1 || got.Counts["encounters"] != 1 || got.Counts["friends"] != 1 ||
		got.Counts["spacePosts"] != 0 || got.Counts["photos"] != 0 {
		t.Fatalf("counts = %v", got.Counts)
	}
	ids := map[string]string{}
	for _, r := range got.Rows {
		ids[r.ID] = r.Place
	}
	if len(got.Rows) != 7 {
		t.Fatalf("rows = %d, want 7", len(got.Rows))
	}
	if ids["public_posts"] != "あなたの PDS（https://pds.example）" {
		t.Fatalf("public_posts place = %q", ids["public_posts"])
	}
	if ids["friends_follow"] != "あなたの PDS（app.bsky.graph.follow）" || ids["encounters"] != "CHAIN のサーバー" {
		t.Fatalf("friend rows = %q / %q", ids["friends_follow"], ids["encounters"])
	}
}

func TestDataMapScopesFromSession(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)
	const me = "did:plc:me"
	env.CreateUser(t, me, "me.test")
	token := env.Login(t, me)

	raw, err := json.Marshal(oauth.ClientSessionData{AccountDID: syntax.DID(me), SessionID: "atp-" + me, Scopes: []string{"atproto", "repo:app.bsky.feed.post?action=create"}})
	if err != nil {
		t.Fatal(err)
	}
	enc, err := auth.Encrypt(env.Config.SecretKey, raw)
	if err != nil {
		t.Fatal(err)
	}
	testutil.Exec(t, env, `UPDATE atproto_oauth_sessions SET data_enc = ? WHERE id = ?`, enc, "atp-"+me)

	get := func() dataMapBody {
		status, body := testutil.Request(t, app, http.MethodGet, "/api/v1/me/data-map", "", token)
		if status != http.StatusOK {
			t.Fatalf("status = %d (body=%s)", status, body)
		}
		var got dataMapBody
		if err := json.Unmarshal(body, &got); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return got
	}
	got := get()
	if len(got.Scopes) != 2 || got.Scopes[0] != "atproto" {
		t.Fatalf("scopes = %v", got.Scopes)
	}

	// セッションが失効すると null。
	testutil.Exec(t, env, `UPDATE atproto_oauth_sessions SET data_enc = x'', revoked_at = ? WHERE id = ?`, env.Clock.Now().UnixMilli(), "atp-"+me)
	if got := get(); got.Scopes != nil {
		t.Fatalf("revoked scopes = %v, want null", got.Scopes)
	}
}

func TestDataMapRequiresAuth(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)
	if status, _ := testutil.Request(t, app, http.MethodGet, "/api/v1/me/data-map", "", ""); status != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", status)
	}
}
