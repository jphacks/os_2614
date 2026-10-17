//go:build integration

package controller_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/applemango/chain/internal/kv"
	"github.com/applemango/chain/internal/model"
	"github.com/applemango/chain/internal/testutil"
)

const (
	apiAlice = "did:plc:alice"
	apiBob   = "did:plc:bob"
	apiCarol = "did:plc:carol"
)

func seedFriendship(t *testing.T, env *testutil.Env, a, b string) {
	t.Helper()
	x, y := pair(a, b)
	execSQL(t, env, `INSERT INTO friendships (user_a, user_b, created_at, via) VALUES (?, ?, ?, 'encounter')`,
		x, y, env.Clock.Now().UnixMilli())
}

// seedParty はエンジンを回さずに party の行を作り、party:of を張る。
func seedParty(t *testing.T, env *testutil.Env, spaceID, phase string, members ...string) {
	t.Helper()
	ctx := context.Background()
	now := env.Clock.Now().UnixMilli()
	authority := "did:web:chain.test"
	ref := "at://" + authority + "/space/jp.i32.chain.space.party/" + spaceID
	execSQL(t, env, `INSERT INTO spaces (id, type, authority, ref, name, state, created_by, created_at)
		VALUES (?, 'jp.i32.chain.space.party', ?, ?, '10/17 09:00 の party', 'active', ?, ?)`,
		spaceID, authority, ref, members[0], now)

	var confirmed any
	if phase == "confirmed" {
		confirmed = now
	}
	execSQL(t, env, `INSERT INTO parties (space_id, phase, tentative_at, confirmed_at, last_activity_at)
		VALUES (?, ?, ?, ?, ?)`, spaceID, phase, now, confirmed, now)
	for _, did := range members {
		execSQL(t, env, `INSERT INTO space_members (space_id, did, joined_at) VALUES (?, ?, ?)`, spaceID, did, now)
		if err := model.SetPartyOf(ctx, env.Redis, did, spaceID); err != nil {
			t.Fatalf("SetPartyOf(%s): %v", did, err)
		}
	}
}

func TestPartyFriendBeacons(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)
	env.CreateUser(t, apiAlice, "alice.test")
	alice := env.Login(t, apiAlice)
	env.CreateUser(t, apiBob, "bob.test")
	seedFriendship(t, env, apiAlice, apiBob)
	env.CreateUser(t, apiCarol, "carol.test")

	status, body := request(t, app, http.MethodGet, "/api/v1/party/friend-beacons", "", alice)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", status, body)
	}
	var res struct {
		ValidUntil string `json:"validUntil"`
		Friends    []struct {
			DID    string `json:"did"`
			Tokens []struct {
				WindowStart string `json:"windowStart"`
				Token       string `json:"token"`
			} `json:"tokens"`
		} `json:"friends"`
	}
	mustJSONBody(t, body, &res)
	if res.ValidUntil != "2026-10-17T06:00:00Z" {
		t.Fatalf("validUntil = %q, want 2026-10-17T06:00:00Z", res.ValidUntil)
	}
	if len(res.Friends) != 1 || res.Friends[0].DID != apiBob {
		t.Fatalf("friends = %+v, want only bob", res.Friends)
	}
	if len(res.Friends[0].Tokens) != 24 {
		t.Fatalf("tokens = %d, want 24", len(res.Friends[0].Tokens))
	}
	if res.Friends[0].Tokens[0].WindowStart != "2026-10-17T00:00:00Z" || res.Friends[0].Tokens[0].Token == "" {
		t.Fatalf("first token = %+v", res.Friends[0].Tokens[0])
	}

	// friend_beacon_share = 0 の相手は配られない。
	status, body = request(t, app, http.MethodPatch, "/api/v1/me/settings", `{"friendBeaconShare":false}`, env.Login(t, apiBob))
	if status != http.StatusOK {
		t.Fatalf("bob settings: status = %d (body=%s)", status, body)
	}
	_, body = request(t, app, http.MethodGet, "/api/v1/party/friend-beacons", "", alice)
	mustJSONBody(t, body, &res)
	if len(res.Friends) != 0 {
		t.Fatalf("friends = %+v, want empty after opt-out", res.Friends)
	}
}

func TestPartyPresence(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)
	env.CreateUser(t, apiAlice, "alice.test")
	alice := env.Login(t, apiAlice)
	env.CreateUser(t, apiBob, "bob.test")
	seedFriendship(t, env, apiAlice, apiBob)

	body := `{"observations":[{"did":"` + apiBob + `","lastSeenAt":"2026-10-17T00:00:00Z","rssi":-60},` +
		`{"did":"did:plc:stranger","lastSeenAt":"2026-10-17T00:00:00Z"}]}`
	status, respBody := request(t, app, http.MethodPost, "/api/v1/party/presence", body, alice)
	if status != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 (body=%s)", status, respBody)
	}

	ctx := context.Background()
	exists, err := env.Redis.Exists(ctx, kv.PeerEdgeKey(pair(apiAlice, apiBob))).Result()
	if err != nil || exists != 1 {
		t.Fatalf("friend edge missing (exists=%d err=%v)", exists, err)
	}
	exists, err = env.Redis.Exists(ctx, kv.PeerEdgeKey(pair(apiAlice, "did:plc:stranger"))).Result()
	if err != nil {
		t.Fatalf("redis: %v", err)
	}
	if exists != 0 {
		t.Fatal("non-friend observation must be ignored")
	}
}

func TestPartyCurrentAndLeave(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)
	env.CreateUser(t, apiAlice, "alice.test")
	alice := env.Login(t, apiAlice)
	env.CreateUser(t, apiBob, "bob.test")

	// 所属なしなら null。
	status, body := request(t, app, http.MethodGet, "/api/v1/party/current", "", alice)
	if status != http.StatusOK || string(body) != "null" {
		t.Fatalf("current without party: status=%d body=%s, want 200 null", status, body)
	}

	seedParty(t, env, "party-1", "tentative", apiAlice, apiBob)
	status, body = request(t, app, http.MethodGet, "/api/v1/party/current", "", alice)
	if status != http.StatusOK {
		t.Fatalf("current: status = %d (body=%s)", status, body)
	}
	var detail struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		Phase       string `json:"phase"`
		Locked      bool   `json:"locked"`
		TentativeAt string `json:"tentativeAt"`
		Members     []struct {
			DID  string `json:"did"`
			Left bool   `json:"left"`
		} `json:"members"`
	}
	mustJSONBody(t, body, &detail)
	if detail.ID != "party-1" || detail.Phase != "tentative" || detail.Locked || len(detail.Members) != 2 {
		t.Fatalf("detail = %+v", detail)
	}

	// 退出すると 204、current は null に戻る。
	status, body = request(t, app, http.MethodPost, "/api/v1/parties/party-1/leave", "", alice)
	if status != http.StatusNoContent {
		t.Fatalf("leave: status = %d (body=%s)", status, body)
	}
	status, body = request(t, app, http.MethodGet, "/api/v1/party/current", "", alice)
	if status != http.StatusOK || string(body) != "null" {
		t.Fatalf("current after leave: status=%d body=%s, want 200 null", status, body)
	}
}

func TestPartyRename(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)
	env.CreateUser(t, apiAlice, "alice.test")
	alice := env.Login(t, apiAlice)
	env.CreateUser(t, apiBob, "bob.test")
	bob := env.Login(t, apiBob)
	seedParty(t, env, "party-2", "confirmed", apiAlice)

	status, body := request(t, app, http.MethodPatch, "/api/v1/parties/party-2", `{"name":"新 party 名"}`, alice)
	if status != http.StatusOK {
		t.Fatalf("rename: status = %d (body=%s)", status, body)
	}
	var detail struct {
		Name string `json:"name"`
	}
	mustJSONBody(t, body, &detail)
	if detail.Name != "新 party 名" {
		t.Fatalf("name = %q", detail.Name)
	}

	// 非メンバーは 403。
	status, body = request(t, app, http.MethodPatch, "/api/v1/parties/party-2", `{"name":"x"}`, bob)
	if status != http.StatusForbidden {
		t.Fatalf("non-member rename: status = %d (body=%s)", status, body)
	}
	wantErrorCode(t, body, "forbidden")

	// 空の名前は 400。
	status, body = request(t, app, http.MethodPatch, "/api/v1/parties/party-2", `{"name":"  "}`, alice)
	if status != http.StatusBadRequest {
		t.Fatalf("empty name: status = %d (body=%s)", status, body)
	}
}

func TestPartyRenameModerationBlocked(t *testing.T) {
	env := testutil.NewEnv(t)
	env.Moderator = &testutil.BlockingModerator{Words: []string{"禁止"}}
	app := testutil.NewApp(env)
	env.CreateUser(t, apiAlice, "alice.test")
	alice := env.Login(t, apiAlice)
	seedParty(t, env, "party-3", "confirmed", apiAlice)

	status, body := request(t, app, http.MethodPatch, "/api/v1/parties/party-3", `{"name":"禁止ワード"}`, alice)
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422 (body=%s)", status, body)
	}
	wantErrorCode(t, body, "moderation_blocked")
}

func TestMemoriesListsArchivedParty(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)
	env.CreateUser(t, apiAlice, "alice.test")
	alice := env.Login(t, apiAlice)
	env.CreateUser(t, apiBob, "bob.test")

	now := env.Clock.Now().UnixMilli()
	authority := "did:web:chain.test"
	execSQL(t, env, `INSERT INTO spaces (id, type, authority, ref, name, state, created_by, created_at, archived_at)
		VALUES ('party-old', 'jp.i32.chain.space.party', ?, ?, '10/14 13:00 の party', 'archived', ?, ?, ?)`,
		authority, "at://"+authority+"/space/jp.i32.chain.space.party/party-old", apiAlice, now, now)
	execSQL(t, env, `INSERT INTO parties (space_id, phase, tentative_at, confirmed_at, ended_at, last_activity_at)
		VALUES ('party-old', 'ended', ?, ?, ?, ?)`, now, now, now, now)
	execSQL(t, env, `INSERT INTO space_members (space_id, did, can_write, joined_at, left_at) VALUES ('party-old', ?, 0, ?, ?)`,
		apiAlice, now, now)
	execSQL(t, env, `INSERT INTO space_members (space_id, did, can_write, joined_at, left_at) VALUES ('party-old', ?, 0, ?, ?)`,
		apiBob, now, now)
	execSQL(t, env, `INSERT INTO media (id, owner_did, space_id, mime, width, height, bytes, sha256, created_at)
		VALUES ('m-1', ?, 'party-old', 'image/jpeg', 8, 8, 100, 'abc', ?)`, apiAlice, now)

	status, body := request(t, app, http.MethodGet, "/api/v1/memories", "", alice)
	if status != http.StatusOK {
		t.Fatalf("status = %d (body=%s)", status, body)
	}
	var page struct {
		Items []struct {
			SpaceID     string `json:"spaceId"`
			Type        string `json:"type"`
			Name        string `json:"name"`
			From        string `json:"from"`
			Until       string `json:"until"`
			MemberCount int    `json:"memberCount"`
			MediaCount  int    `json:"mediaCount"`
			Cover       *struct {
				ThumbURL string `json:"thumbUrl"`
			} `json:"cover"`
		} `json:"items"`
	}
	mustJSONBody(t, body, &page)
	if len(page.Items) != 1 {
		t.Fatalf("items = %+v, want 1", page.Items)
	}
	item := page.Items[0]
	if item.SpaceID != "party-old" || item.Type != "party" || item.MemberCount != 2 || item.MediaCount != 1 {
		t.Fatalf("item = %+v", item)
	}
	if item.Cover == nil || item.Cover.ThumbURL == "" {
		t.Fatalf("cover = %+v, want a signed thumb url", item.Cover)
	}
}

func TestPartyLockAPI(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)
	env.CreateUser(t, apiAlice, "alice.test")
	alice := env.Login(t, apiAlice)
	env.CreateUser(t, apiBob, "bob.test")
	bob := env.Login(t, apiBob)
	env.CreateUser(t, apiCarol, "carol.test")
	carol := env.Login(t, apiCarol)

	seedParty(t, env, "party-1", "confirmed", apiAlice, apiBob)

	status, body := request(t, app, http.MethodPost, "/api/v1/parties/party-1/lock", "", alice)
	if status != http.StatusOK {
		t.Fatalf("lock: status = %d, want 200 (body=%s)", status, body)
	}
	var detail struct {
		ID     string `json:"id"`
		Phase  string `json:"phase"`
		Locked bool   `json:"locked"`
	}
	mustJSONBody(t, body, &detail)
	if !detail.Locked || detail.Phase != "confirmed" {
		t.Fatalf("detail = %+v, want locked confirmed", detail)
	}
	ctx := context.Background()
	var n int
	if err := env.DB.QueryRowContext(ctx,
		`SELECT count(*) FROM records WHERE space_id = 'party-1' AND value_json LIKE '%"system":true%'`).Scan(&n); err != nil {
		t.Fatalf("count system posts: %v", err)
	}
	if n != 1 {
		t.Fatalf("system posts = %d, want 1", n)
	}

	// 他のメンバーも解除できる。
	status, body = request(t, app, http.MethodPost, "/api/v1/parties/party-1/unlock", "", bob)
	if status != http.StatusOK {
		t.Fatalf("unlock: status = %d, want 200 (body=%s)", status, body)
	}
	mustJSONBody(t, body, &detail)
	if detail.Locked {
		t.Fatalf("detail = %+v, want unlocked", detail)
	}

	// メンバー以外は 403。
	status, body = request(t, app, http.MethodPost, "/api/v1/parties/party-1/lock", "", carol)
	if status != http.StatusForbidden {
		t.Fatalf("non-member: status = %d, want 403 (body=%s)", status, body)
	}

	// 仮 party は 403。
	seedParty(t, env, "party-2", "tentative", apiAlice)
	status, body = request(t, app, http.MethodPost, "/api/v1/parties/party-2/lock", "", alice)
	if status != http.StatusForbidden {
		t.Fatalf("tentative: status = %d, want 403 (body=%s)", status, body)
	}
}
