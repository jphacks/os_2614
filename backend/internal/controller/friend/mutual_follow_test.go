//go:build integration

package friend_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/applemango/chain/internal/testutil"
)

type apiFriendItem struct {
	DID       string  `json:"did"`
	Precision string  `json:"precision"`
	Met       bool    `json:"met"`
	Via       *string `json:"via"`
}

func seedFollow(t *testing.T, env *testutil.Env, author, subject string) {
	t.Helper()
	now := env.Clock.Now().UnixMilli()
	testutil.Exec(t, env, `INSERT INTO follows (id, uri, author_did, subject_did, created_at, indexed_at) VALUES (?, ?, ?, ?, ?, ?)`,
		env.IDs.NewID(), "at://"+author+"/app.bsky.graph.follow/"+env.IDs.NewID(), author, subject, now, now)
}

// 会ったフレンドでない相互フォローには、位置も beacon も渡らない（07 §1、§4.2、08）。
func TestMutualFollowWithoutEvidenceGetsNoLocationOrBeacon(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)
	const alice, bob = "did:plc:alice", "did:plc:bob"
	env.CreateUser(t, alice, "alice.test")
	env.CreateUser(t, bob, "bob.test")
	aliceToken := env.Login(t, alice)
	bobToken := env.Login(t, bob)
	seedFollow(t, env, alice, bob)
	seedFollow(t, env, bob, alice)
	ctx := context.Background()

	friends := func(token string) []apiFriendItem {
		t.Helper()
		status, body := testutil.Request(t, app, http.MethodGet, "/api/v1/friends", "", token)
		if status != http.StatusOK {
			t.Fatalf("GET /friends: status = %d (body=%s)", status, body)
		}
		var out struct {
			Items []apiFriendItem `json:"items"`
		}
		if err := json.Unmarshal(body, &out); err != nil {
			t.Fatalf("decode: %v (body=%s)", err, body)
		}
		return out.Items
	}
	beacons := func(token string) int {
		t.Helper()
		status, body := testutil.Request(t, app, http.MethodGet, "/api/v1/party/friend-beacons", "", token)
		if status != http.StatusOK {
			t.Fatalf("friend-beacons: status = %d (body=%s)", status, body)
		}
		var out struct {
			Friends []json.RawMessage `json:"friends"`
		}
		if err := json.Unmarshal(body, &out); err != nil {
			t.Fatalf("decode: %v (body=%s)", err, body)
		}
		return len(out.Friends)
	}

	// 一覧には出るが met=false・via=null・precision=off。
	items := friends(aliceToken)
	if len(items) != 1 || items[0].DID != bob || items[0].Met || items[0].Via != nil || items[0].Precision != "off" {
		t.Fatalf("friends = %+v", items)
	}
	// 位置の設定は 403。位置を送っても相手には渡らない。beacon も配られない。
	status, body := setPrecision(t, app, aliceToken, bob, "exact", false)
	if status != http.StatusForbidden {
		t.Fatalf("set precision: status = %d, want 403 (body=%s)", status, body)
	}
	putLocation(t, app, bobToken, 35.0, 135.0, 10, env.Clock.Now())
	if locs := getFriendLocations(t, app, aliceToken); len(locs.Items) != 0 {
		t.Fatalf("locations = %+v, want empty", locs.Items)
	}
	if n := beacons(aliceToken); n != 0 {
		t.Fatalf("friend-beacons = %d, want 0", n)
	}

	// 会った裏付け（revealed な encounter）ができて Reconciler が走ると、会ったフレンドになる。
	a, b := testutil.Pair(alice, bob)
	now := env.Clock.Now().UnixMilli()
	testutil.Exec(t, env, `INSERT INTO encounters (id, user_a, user_b, established_at, first_seen_at, last_seen_at, visibility)
		VALUES ('enc-ab', ?, ?, ?, ?, ?, 'revealed')`, a, b, now, now, now)
	if _, err := env.Reconciler().ReconcilePair(ctx, alice, bob); err != nil {
		t.Fatalf("ReconcilePair: %v", err)
	}
	items = friends(aliceToken)
	if len(items) != 1 || !items[0].Met || items[0].Via == nil || *items[0].Via != "encounter" || items[0].Precision != "area" {
		t.Fatalf("friends after evidence = %+v", items)
	}
	if n := beacons(aliceToken); n != 1 {
		t.Fatalf("friend-beacons after evidence = %d, want 1", n)
	}
	if locs := getFriendLocations(t, app, aliceToken); len(locs.Items) != 1 {
		t.Fatalf("locations after evidence = %+v, want bob", locs.Items)
	}
	env.Clock.Advance(time.Second)

	// Bluesky のブロックが入ると一覧から消え、位置・beacon も止まる。
	testutil.Exec(t, env, `INSERT INTO blocks (blocker, blocked, created_at, source, uri) VALUES (?, ?, ?, 'bsky', ?)`,
		bob, alice, now, "at://"+bob+"/app.bsky.graph.block/x")
	if _, err := env.Reconciler().ReconcilePair(ctx, alice, bob); err != nil {
		t.Fatalf("ReconcilePair: %v", err)
	}
	if items = friends(aliceToken); len(items) != 0 {
		t.Fatalf("friends after bsky block = %+v, want empty", items)
	}
	if n := beacons(aliceToken); n != 0 {
		t.Fatalf("friend-beacons after bsky block = %d, want 0", n)
	}
	status, body = testutil.Request(t, app, http.MethodDelete, "/api/v1/blocks/"+alice, "", bobToken)
	if status != http.StatusConflict {
		t.Fatalf("unblock a bsky block: status = %d, want 409 (body=%s)", status, body)
	}
}
