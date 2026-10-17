//go:build integration

package controller_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/applemango/chain/internal/testutil"
)

func TestEncounterSummaryAPI(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)
	const me, peer = "did:plc:me", "did:plc:peer"
	env.CreateUser(t, me, "me.test")
	env.CreateUser(t, peer, "peer.test")
	token := env.Login(t, me)
	now := env.Clock.Now()
	a, b := pair(me, peer)

	execSQL(t, env, `INSERT INTO encounters (id, user_a, user_b, established_at, first_seen_at, last_seen_at, visibility)
		VALUES (?, ?, ?, ?, ?, ?, 'hidden')`,
		"enc-1", a, b, now.Add(-2*time.Hour).UnixMilli(), now.Add(-2*time.Hour).UnixMilli(), now.Add(-2*time.Hour).UnixMilli())
	execSQL(t, env, `INSERT INTO encounters (id, user_a, user_b, established_at, first_seen_at, last_seen_at, visibility)
		VALUES (?, ?, ?, ?, ?, ?, 'revealed')`,
		"enc-2", a, b, now.Add(-59*time.Minute).UnixMilli(), now.Add(-59*time.Minute).UnixMilli(), now.Add(-59*time.Minute).UnixMilli())

	status, body := request(t, app, http.MethodGet, "/api/v1/encounters/summary", "", token)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", status, body)
	}
	var got struct {
		Today        int `json:"today"`
		Week         int `json:"week"`
		Month        int `json:"month"`
		Total        int `json:"total"`
		UniquePeople int `json:"uniquePeople"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode: %v (body=%s)", err, body)
	}
	if got.Today != 1 || got.Total != 1 || got.UniquePeople != 1 {
		t.Fatalf("summary = %+v, want today/total/unique = 1 (59 分前は数えない)", got)
	}
}

func TestMeStatsAPI(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)
	const me, peer = "did:plc:me", "did:plc:peer"
	env.CreateUser(t, me, "me.test")
	env.CreateUser(t, peer, "peer.test")
	token := env.Login(t, me)
	now := env.Clock.Now()
	ms := now.UnixMilli()
	a, b := pair(me, peer)

	execSQL(t, env, `INSERT INTO friendships (user_a, user_b, created_at, via) VALUES (?, ?, ?, 'encounter')`, a, b, ms)
	execSQL(t, env, `INSERT INTO encounters (id, user_a, user_b, established_at, first_seen_at, last_seen_at, visibility)
		VALUES (?, ?, ?, ?, ?, ?, 'revealed')`, "enc-1", a, b, now.Add(-2*time.Hour).UnixMilli(), now.Add(-2*time.Hour).UnixMilli(), now.Add(-2*time.Hour).UnixMilli())

	execSQL(t, env, `INSERT INTO spaces (id, type, authority, ref, name, state, created_by, created_at)
		VALUES ('g1', 'jp.i32.chain.space.group', 'did:web:chain.test', 'at://x/g1', 'g1', 'active', ?, ?)`, me, ms)
	execSQL(t, env, `INSERT INTO groups (space_id, visibility, origin, center_lat, center_lon, radius_m)
		VALUES ('g1', 'open', 'explicit', 35.0, 139.0, 100)`)
	execSQL(t, env, `INSERT INTO space_members (space_id, did, can_read, can_write, role, joined_at, left_at, hidden_from_memory)
		VALUES ('g1', ?, 1, 1, 'member', ?, NULL, 0)`, me, ms)

	execSQL(t, env, `INSERT INTO spaces (id, type, authority, ref, name, state, created_by, created_at)
		VALUES ('p1', 'jp.i32.chain.space.party', 'did:web:chain.test', 'at://x/p1', 'p1', 'active', ?, ?)`, me, ms)
	execSQL(t, env, `INSERT INTO parties (space_id, phase, locked, tentative_at, last_activity_at)
		VALUES ('p1', 'confirmed', 0, ?, ?)`, ms, ms)
	execSQL(t, env, `INSERT INTO space_members (space_id, did, can_read, can_write, role, joined_at, left_at, hidden_from_memory)
		VALUES ('p1', ?, 1, 1, 'member', ?, NULL, 0)`, me, ms)

	execSQL(t, env, `INSERT INTO records (uri, space_id, author_did, collection, rkey, value_json, created_at, deleted_at)
		VALUES ('at://x/post1', 'g1', ?, 'jp.i32.chain.instance.post', 'r1', '{}', ?, NULL)`, me, ms)

	status, body := request(t, app, http.MethodGet, "/api/v1/me/stats", "", token)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", status, body)
	}
	var got struct {
		Encounters struct {
			Today        int `json:"today"`
			Total        int `json:"total"`
			UniquePeople int `json:"uniquePeople"`
		} `json:"encounters"`
		Friends int `json:"friends"`
		Parties struct {
			Total int `json:"total"`
		} `json:"parties"`
		Groups struct {
			Member int `json:"member"`
		} `json:"groups"`
		Posts struct {
			Month int `json:"month"`
		} `json:"posts"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode: %v (body=%s)", err, body)
	}
	if got.Encounters.Today != 1 || got.Encounters.Total != 1 || got.Encounters.UniquePeople != 1 {
		t.Fatalf("encounters = %+v", got.Encounters)
	}
	if got.Friends != 1 || got.Parties.Total != 1 || got.Groups.Member != 1 || got.Posts.Month != 1 {
		t.Fatalf("stats = %+v, want friends/parties/groups/posts = 1", got)
	}
}
