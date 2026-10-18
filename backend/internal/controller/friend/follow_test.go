//go:build integration

package friend_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/applemango/chain/internal/external/atproto"
	"github.com/applemango/chain/internal/testutil"
)

// countFollowRows は follows にある author → subject の行数を返す。
func countFollowRows(t *testing.T, env *testutil.Env, author, subject string) int {
	t.Helper()
	var n int
	if err := env.DB.QueryRowContext(context.Background(),
		"SELECT count(*) FROM follows WHERE author_did = ? AND subject_did = ?", author, subject).Scan(&n); err != nil {
		t.Fatalf("count follows: %v", err)
	}
	return n
}

// revealEncounter は revealed な encounter を 1 つ入れる（接点を作る）。
func revealEncounter(t *testing.T, env *testutil.Env, id, a, b string) {
	t.Helper()
	x, y := testutil.Pair(a, b)
	now := env.Clock.Now().UnixMilli()
	testutil.Exec(t, env, `INSERT INTO encounters (id, user_a, user_b, established_at, first_seen_at, last_seen_at, visibility)
		VALUES (?, ?, ?, ?, ?, ?, 'revealed')`, id, x, y, now, now, now)
}

// POST /api/v1/users/{did}/follow は PDS に app.bsky.graph.follow を書き、follows に入れる（13 §1.2）。
func TestFollowAPI(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)
	const alice, bob, carol, dave = "did:plc:alice", "did:plc:bob", "did:plc:carol", "did:plc:dave"
	for _, u := range []struct{ did, handle string }{
		{alice, "alice.test"}, {bob, "bob.test"}, {carol, "carol.test"}, {dave, "dave.test"},
	} {
		env.CreateUser(t, u.did, u.handle)
	}
	aliceToken := env.Login(t, alice)
	env.Login(t, bob)
	env.Login(t, carol)

	// alice と bob はすれ違い済み（接点は revealed な encounter）。
	revealEncounter(t, env, "enc-ab", alice, bob)
	// alice と carol も接点があるが、carol は alice をブロックしている。
	revealEncounter(t, env, "enc-ac", alice, carol)
	testutil.Exec(t, env, "INSERT INTO blocks (blocker, blocked, created_at) VALUES (?, ?, ?)",
		carol, alice, env.Clock.Now().UnixMilli())

	// 自分自身は 400。
	status, body := testutil.Request(t, app, http.MethodPost, "/api/v1/users/"+alice+"/follow", "", aliceToken)
	if status != http.StatusBadRequest {
		t.Fatalf("self follow: status = %d, want 400 (body=%s)", status, body)
	}
	if e := testutil.WantErrorCode(t, body, "invalid_request"); e.Error.Details.Fields["did"] == "" {
		t.Fatalf("details.fields.did is empty (body=%s)", body)
	}

	// 接点の無い相手は 404。
	status, body = testutil.Request(t, app, http.MethodPost, "/api/v1/users/"+dave+"/follow", "", aliceToken)
	if status != http.StatusNotFound {
		t.Fatalf("unrelated follow: status = %d, want 404 (body=%s)", status, body)
	}
	testutil.WantErrorCode(t, body, "not_found")

	// ブロック関係は 403。
	status, body = testutil.Request(t, app, http.MethodPost, "/api/v1/users/"+carol+"/follow", "", aliceToken)
	if status != http.StatusForbidden {
		t.Fatalf("blocked follow: status = %d, want 403 (body=%s)", status, body)
	}
	testutil.WantErrorCode(t, body, "blocked")

	// 正常系: PDS に書き、follows に入る。
	before := len(env.Writer.CreateCalls())
	status, body = testutil.Request(t, app, http.MethodPost, "/api/v1/users/"+bob+"/follow", "", aliceToken)
	if status != http.StatusNoContent {
		t.Fatalf("follow: status = %d, want 204 (body=%s)", status, body)
	}
	if n := countFollowRows(t, env, alice, bob); n != 1 {
		t.Fatalf("follows rows = %d, want 1", n)
	}
	calls := env.Writer.CreateCalls()
	if len(calls) != before+1 {
		t.Fatalf("CreateRecord calls = %d, want %d", len(calls), before+1)
	}
	last := calls[len(calls)-1]
	if last.DID != alice || last.Collection != atproto.CollectionBskyFollow {
		t.Fatalf("create call = %+v", last)
	}
	rec, ok := last.Record.(map[string]any)
	if !ok || rec["subject"] != bob {
		t.Fatalf("record = %#v, want subject %s", last.Record, bob)
	}

	// 既にフォロー済みなら 204 で何も書かない。
	status, body = testutil.Request(t, app, http.MethodPost, "/api/v1/users/"+bob+"/follow", "", aliceToken)
	if status != http.StatusNoContent {
		t.Fatalf("repeat follow: status = %d, want 204 (body=%s)", status, body)
	}
	if n := len(env.Writer.CreateCalls()); n != before+1 {
		t.Fatalf("CreateRecord calls after repeat = %d, want %d", n, before+1)
	}
	if n := countFollowRows(t, env, alice, bob); n != 1 {
		t.Fatalf("follows rows after repeat = %d, want 1", n)
	}
}

// DELETE /api/v1/users/{did}/follow は DELETE /friends/{did} と同じハンドラ（13 §1.2、07 §2.3）。
func TestUnfollowAPI(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)
	const alice, bob, carol = "did:plc:alice", "did:plc:bob", "did:plc:carol"
	for _, u := range []struct{ did, handle string }{
		{alice, "alice.test"}, {bob, "bob.test"}, {carol, "carol.test"},
	} {
		env.CreateUser(t, u.did, u.handle)
	}
	aliceToken := env.Login(t, alice)

	now := env.Clock.Now().UnixMilli()
	testutil.Exec(t, env, `INSERT INTO follows (id, uri, author_did, subject_did, created_at, indexed_at) VALUES (?, ?, ?, ?, ?, ?)`,
		env.IDs.NewID(), "at://"+alice+"/app.bsky.graph.follow/rkey-1", alice, bob, now, now)

	// フォローしていない相手は 404。
	status, body := testutil.Request(t, app, http.MethodDelete, "/api/v1/users/"+carol+"/follow", "", aliceToken)
	if status != http.StatusNotFound {
		t.Fatalf("unfollow non-followed: status = %d, want 404 (body=%s)", status, body)
	}
	testutil.WantErrorCode(t, body, "not_found")

	// 自分自身は 404。
	status, body = testutil.Request(t, app, http.MethodDelete, "/api/v1/users/"+alice+"/follow", "", aliceToken)
	if status != http.StatusNotFound {
		t.Fatalf("unfollow self: status = %d, want 404 (body=%s)", status, body)
	}

	// フォローしていれば 204。PDS からも消す。
	status, body = testutil.Request(t, app, http.MethodDelete, "/api/v1/users/"+bob+"/follow", "", aliceToken)
	if status != http.StatusNoContent {
		t.Fatalf("unfollow: status = %d, want 204 (body=%s)", status, body)
	}
	if n := countFollowRows(t, env, alice, bob); n != 0 {
		t.Fatalf("follows rows after unfollow = %d, want 0", n)
	}
	if calls := env.Writer.DeleteCalls(); len(calls) != 1 || calls[0].DID != alice || calls[0].Collection != atproto.CollectionBskyFollow {
		t.Fatalf("DeleteRecord calls = %+v", calls)
	}

	// 2 度目は 404。
	status, body = testutil.Request(t, app, http.MethodDelete, "/api/v1/users/"+bob+"/follow", "", aliceToken)
	if status != http.StatusNotFound {
		t.Fatalf("second unfollow: status = %d, want 404 (body=%s)", status, body)
	}
}

// GET /api/v1/users/{did} は following / followedBy を follows の索引から添える（13 §1.2）。
func TestUserProfileFollowRelations(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)
	const viewer, out, in, mutual, none = "did:plc:viewer", "did:plc:out", "did:plc:in", "did:plc:mutual", "did:plc:none"
	for _, u := range []struct{ did, handle string }{
		{viewer, "viewer.test"}, {out, "out.test"}, {in, "in.test"}, {mutual, "mutual.test"}, {none, "none.test"},
	} {
		env.CreateUser(t, u.did, u.handle)
	}
	token := env.Login(t, viewer)

	now := env.Clock.Now().UnixMilli()
	follow := func(author, subject string) {
		testutil.Exec(t, env, `INSERT INTO follows (id, uri, author_did, subject_did, created_at, indexed_at) VALUES (?, ?, ?, ?, ?, ?)`,
			env.IDs.NewID(), "at://"+author+"/app.bsky.graph.follow/"+env.IDs.NewID(), author, subject, now, now)
	}
	// 片方向のフォローだけでは接点にならないので、プロフィールが見えるよう encounter を付ける。
	revealEncounter(t, env, "enc-out", viewer, out)
	revealEncounter(t, env, "enc-in", viewer, in)
	revealEncounter(t, env, "enc-none", viewer, none)

	follow(viewer, out) // viewer → out
	follow(in, viewer)  // in → viewer
	follow(viewer, mutual)
	follow(mutual, viewer)

	get := func(did string) (following, followedBy bool) {
		t.Helper()
		status, body := testutil.Request(t, app, http.MethodGet, "/api/v1/users/"+did, "", token)
		if status != http.StatusOK {
			t.Fatalf("GET /users/%s: status = %d, want 200 (body=%s)", did, status, body)
		}
		var u struct {
			DID        string `json:"did"`
			Following  bool   `json:"following"`
			FollowedBy bool   `json:"followedBy"`
		}
		if err := json.Unmarshal(body, &u); err != nil {
			t.Fatalf("decode: %v (body=%s)", err, body)
		}
		if u.DID != did {
			t.Fatalf("did = %q, want %q", u.DID, did)
		}
		return u.Following, u.FollowedBy
	}

	// 自分自身は両方 false。
	if f, fb := get(viewer); f || fb {
		t.Fatalf("self following=%v followedBy=%v, want false/false", f, fb)
	}
	// 自分から相手への片方向。
	if f, fb := get(out); !f || fb {
		t.Fatalf("out following=%v followedBy=%v, want true/false", f, fb)
	}
	// 相手から自分への片方向。
	if f, fb := get(in); f || !fb {
		t.Fatalf("in following=%v followedBy=%v, want false/true", f, fb)
	}
	// 相互。
	if f, fb := get(mutual); !f || !fb {
		t.Fatalf("mutual following=%v followedBy=%v, want true/true", f, fb)
	}
	// フォローは無い（接点だけ）。
	if f, fb := get(none); f || fb {
		t.Fatalf("none following=%v followedBy=%v, want false/false", f, fb)
	}
}
