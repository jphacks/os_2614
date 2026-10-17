//go:build integration

package controller_test

import (
	"context"
	"errors"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/applemango/chain/internal/kv"
	"github.com/applemango/chain/internal/model"
	"github.com/applemango/chain/internal/storage"
	"github.com/applemango/chain/internal/testutil"
)

const (
	aliceDID = "did:plc:alice"
	bobDID   = "did:plc:bob"
)

func countRows(t *testing.T, env *testutil.Env, query string, args ...any) int {
	t.Helper()
	var n int
	if err := env.DB.QueryRowContext(context.Background(), query, args...).Scan(&n); err != nil {
		t.Fatalf("count %q: %v", query, err)
	}
	return n
}

func argsFor(did string, n int) []any {
	out := make([]any, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, did)
	}
	return out
}

// aliceRows は削除後に 0 件になるべき、alice が関わるテーブルと条件。
var aliceRows = []struct {
	name    string
	query   string
	didArgs int
}{
	{"beacon_tokens", "SELECT count(*) FROM beacon_tokens WHERE did = ?", 1},
	{"encounters", "SELECT count(*) FROM encounters WHERE user_a = ? OR user_b = ?", 2},
	{"friendships", "SELECT count(*) FROM friendships WHERE user_a = ? OR user_b = ?", 2},
	{"friend_requests", "SELECT count(*) FROM friend_requests WHERE from_did = ? OR to_did = ?", 2},
	{"friend_invites", "SELECT count(*) FROM friend_invites WHERE did = ?", 1},
	{"location_shares", "SELECT count(*) FROM location_shares WHERE owner = ? OR viewer = ?", 2},
	{"areas", "SELECT count(*) FROM areas WHERE did = ?", 1},
	{"devices", "SELECT count(*) FROM devices WHERE did = ?", 1},
	{"notifications", "SELECT count(*) FROM notifications WHERE did = ?", 1},
	{"notification_outbox", "SELECT count(*) FROM notification_outbox o JOIN notifications n ON n.id = o.notification_id WHERE n.did = ?", 1},
	{"blocks", "SELECT count(*) FROM blocks WHERE blocker = ? OR blocked = ?", 2},
	{"user_settings", "SELECT count(*) FROM user_settings WHERE did = ?", 1},
	{"records", "SELECT count(*) FROM records WHERE author_did = ?", 1},
	{"media", "SELECT count(*) FROM media WHERE owner_did = ?", 1},
	{"space_members", "SELECT count(*) FROM space_members WHERE did = ?", 1},
	{"group_invites", "SELECT count(*) FROM group_invites WHERE target_did = ? OR created_by = ?", 2},
}

func seedAccountData(t *testing.T, env *testutil.Env) {
	t.Helper()
	ms := env.Clock.Now().UnixMilli()
	exec := func(query string, args ...any) { execSQL(t, env, query, args...) }

	// ユーザーが関わる物理削除の対象。
	exec(`INSERT INTO beacon_tokens(token_hash, token_enc, did, window_start) VALUES(?,?,?,?)`,
		[]byte("hash-alice"), []byte("enc"), aliceDID, env.Clock.Now().Unix())
	exec(`INSERT INTO encounters(id, user_a, user_b, established_at, first_seen_at, last_seen_at, max_rssi, visibility, reveal_at) VALUES(?,?,?,?,?,?,?,?,?)`,
		"enc-1", aliceDID, bobDID, ms, ms, ms, nil, "revealed", ms)
	exec(`INSERT INTO friendships(user_a, user_b, created_at, via) VALUES(?,?,?,?)`, aliceDID, bobDID, ms, "encounter")
	exec(`INSERT INTO friend_requests(id, from_did, to_did, via, status, created_at) VALUES(?,?,?,?,?,?)`,
		"req-1", aliceDID, bobDID, "encounter", "accepted", ms)
	exec(`INSERT INTO friend_invites(code_hash, did, created_at, expires_at) VALUES(?,?,?,?)`,
		"invite-alice", aliceDID, ms, ms)
	exec(`INSERT INTO location_shares(owner, viewer, precision, areas_only, updated_at) VALUES(?,?,?,?,?)`,
		aliceDID, bobDID, "exact", 0, ms)
	exec(`INSERT INTO location_shares(owner, viewer, precision, areas_only, updated_at) VALUES(?,?,?,?,?)`,
		bobDID, aliceDID, "approx", 0, ms)
	exec(`INSERT INTO areas(id, did, label, lat, lon, radius_m, created_at) VALUES(?,?,?,?,?,?,?)`,
		"area-alice", aliceDID, "home", 1.0, 2.0, 100, ms)
	exec(`INSERT INTO areas(id, did, label, lat, lon, radius_m, created_at) VALUES(?,?,?,?,?,?,?)`,
		"area-bob", bobDID, "home", 3.0, 4.0, 100, ms)
	exec(`INSERT INTO devices(id, did, session_id, fcm_token, platform, updated_at) VALUES(?,?,?,?,?,?)`,
		"dev-alice", aliceDID, nil, "token-alice", "android", ms)
	exec(`INSERT INTO devices(id, did, session_id, fcm_token, platform, updated_at) VALUES(?,?,?,?,?,?)`,
		"dev-bob", bobDID, nil, "token-bob", "android", ms)
	exec(`INSERT INTO notifications(id, did, type, payload_json, created_at) VALUES(?,?,?,?,?)`,
		"notif-alice", aliceDID, "friend.request", "{}", ms)
	exec(`INSERT INTO notifications(id, did, type, payload_json, created_at) VALUES(?,?,?,?,?)`,
		"notif-bob", bobDID, "friend.request", "{}", ms)
	exec(`INSERT INTO notification_outbox(id, notification_id, status, attempts, next_attempt_at) VALUES(?,?,?,?,?)`,
		"out-alice", "notif-alice", "queued", 0, ms)
	exec(`INSERT INTO blocks(blocker, blocked, created_at) VALUES(?,?,?)`, aliceDID, bobDID, ms)
	exec(`INSERT INTO blocks(blocker, blocked, created_at) VALUES(?,?,?)`, bobDID, aliceDID, ms)

	// Space（グループと Party）と、alice の投稿・メディア。
	exec(`INSERT INTO spaces(id,type,authority,ref,read_policy,write_policy,name,state,created_by,created_at) VALUES(?,?,?,?,?,?,?,?,?,?)`,
		"space-g1", "jp.i32.chain.space.group", "did:web:chain.test", "at://a/space/group/g1", "managing-app", "managing-app", "G1", "active", aliceDID, ms)
	exec(`INSERT INTO spaces(id,type,authority,ref,read_policy,write_policy,name,state,created_by,created_at) VALUES(?,?,?,?,?,?,?,?,?,?)`,
		"space-g2", "jp.i32.chain.space.group", "did:web:chain.test", "at://a/space/group/g2", "managing-app", "managing-app", "G2", "active", aliceDID, ms)
	exec(`INSERT INTO spaces(id,type,authority,ref,read_policy,write_policy,name,state,created_by,created_at) VALUES(?,?,?,?,?,?,?,?,?,?)`,
		"space-p1", "jp.i32.chain.space.party", "did:web:chain.test", "at://a/space/party/p1", "managing-app", "managing-app", "P1", "active", aliceDID, ms)
	exec(`INSERT INTO spaces(id,type,authority,ref,read_policy,write_policy,name,state,created_by,created_at) VALUES(?,?,?,?,?,?,?,?,?,?)`,
		"space-p2", "jp.i32.chain.space.party", "did:web:chain.test", "at://a/space/party/p2", "managing-app", "managing-app", "P2", "active", aliceDID, ms)
	exec(`INSERT INTO groups(space_id, visibility, origin, center_lat, center_lon, radius_m) VALUES(?,?,?,?,?,?)`,
		"space-g1", "open", "explicit", 0.0, 0.0, 100)
	exec(`INSERT INTO groups(space_id, visibility, origin, center_lat, center_lon, radius_m) VALUES(?,?,?,?,?,?)`,
		"space-g2", "open", "explicit", 0.0, 0.0, 100)
	exec(`INSERT INTO parties(space_id, phase, tentative_at, last_activity_at) VALUES(?,?,?,?)`,
		"space-p1", "confirmed", ms, ms)
	exec(`INSERT INTO parties(space_id, phase, tentative_at, last_activity_at) VALUES(?,?,?,?)`,
		"space-p2", "confirmed", ms, ms)
	exec(`INSERT INTO space_members(space_id, did, role, joined_at) VALUES(?,?,?,?)`, "space-g1", aliceDID, "owner", ms-2000)
	exec(`INSERT INTO space_members(space_id, did, role, joined_at) VALUES(?,?,?,?)`, "space-g1", bobDID, "member", ms-1000)
	exec(`INSERT INTO space_members(space_id, did, role, joined_at) VALUES(?,?,?,?)`, "space-g2", aliceDID, "owner", ms-2000)
	exec(`INSERT INTO space_members(space_id, did, role, joined_at) VALUES(?,?,?,?)`, "space-p1", aliceDID, "owner", ms-2000)
	exec(`INSERT INTO space_members(space_id, did, role, joined_at) VALUES(?,?,?,?)`, "space-p1", bobDID, "member", ms-1000)
	exec(`INSERT INTO space_members(space_id, did, role, joined_at) VALUES(?,?,?,?)`, "space-p2", aliceDID, "owner", ms-2000)
	exec(`INSERT INTO records(uri, space_id, author_did, collection, rkey, value_json, created_at, mod_label, mod_state) VALUES(?,?,?,?,?,?,?,?,?)`,
		"at://a/space/group/g1/alice/post/1", "space-g1", aliceDID, "jp.i32.chain.instance.post", "1", "{}", ms, "ok", "done")
	exec(`INSERT INTO records(uri, space_id, author_did, collection, rkey, value_json, created_at, mod_label, mod_state) VALUES(?,?,?,?,?,?,?,?,?)`,
		"at://a/space/group/g1/bob/post/1", "space-g1", bobDID, "jp.i32.chain.instance.post", "1", "{}", ms, "ok", "done")
	exec(`INSERT INTO records(uri, space_id, author_did, collection, rkey, value_json, created_at, mod_label, mod_state) VALUES(?,?,?,?,?,?,?,?,?)`,
		"at://a/space/group/g2/alice/post/1", "space-g2", aliceDID, "jp.i32.chain.instance.post", "1", "{}", ms, "ok", "done")

	store := env.Storage()
	for _, m := range []struct {
		id, owner string
		space     any
	}{
		{"media-alice-1", aliceDID, nil},
		{"media-alice-2", aliceDID, "space-g1"},
		{"media-bob-1", bobDID, "space-g1"},
	} {
		if err := store.Save(m.id, env.Clock.Now(), []byte("full"), []byte("thumb")); err != nil {
			t.Fatalf("media のファイルを作れません (%s): %v", m.id, err)
		}
		exec(`INSERT INTO media(id, owner_did, space_id, mime, width, height, bytes, sha256, taken_at, created_at) VALUES(?,?,?,?,?,?,?,?,?,?)`,
			m.id, m.owner, m.space, "image/jpeg", 10, 10, 4, "sha", nil, ms)
	}

	exec(`INSERT INTO group_invites(id, space_id, target_did, created_by, status, created_at, expires_at) VALUES(?,?,?,?,?,?,?)`,
		"gi-1", "space-g1", aliceDID, bobDID, "offered", ms, ms+1000)
	exec(`INSERT INTO group_invites(id, space_id, target_did, created_by, status, created_at, expires_at) VALUES(?,?,?,?,?,?,?)`,
		"gi-2", "space-g1", bobDID, aliceDID, "offered", ms, ms+1000)

	exec(`INSERT INTO audit_log(id, actor_did, action, target, created_at) VALUES(?,?,?,?,?)`,
		"audit-1", aliceDID, "block.create", bobDID, ms)

	// Redis（位置・近接・所属）。
	ctx := context.Background()
	for _, did := range []string{aliceDID, bobDID} {
		if err := model.SaveLocation(ctx, env.Redis, model.Location{
			DID: did, Lat: 1, Lon: 2, AccuracyM: 10, RecordedAt: env.Clock.Now(), Source: "gps",
		}, 30*time.Minute); err != nil {
			t.Fatalf("SaveLocation(%s): %v", did, err)
		}
	}
	env.Redis.ZAdd(ctx, kv.BleRecentKey(aliceDID), redis.Z{Score: float64(ms), Member: bobDID})
	env.Redis.Set(ctx, kv.SightingKey(aliceDID, bobDID, 100), "x", time.Hour)
	env.Redis.Set(ctx, kv.SightingKey(bobDID, aliceDID, 100), "x", time.Hour)
	env.Redis.HSet(ctx, kv.PeerEdgeKey(aliceDID, bobDID), "last", "1")
	env.Redis.HSet(ctx, kv.PeerEdgeKey(bobDID, aliceDID), "last", "1")
	env.Redis.ZAdd(ctx, kv.PeerActiveKey(), redis.Z{Score: float64(ms), Member: aliceDID + "|" + bobDID})
	env.Redis.ZAdd(ctx, kv.PeerActiveKey(), redis.Z{Score: float64(ms), Member: bobDID + "|" + aliceDID})
	if err := model.SetPartyOf(ctx, env.Redis, aliceDID, "space-p1"); err != nil {
		t.Fatalf("SetPartyOf(alice): %v", err)
	}
	if err := model.SetPartyOf(ctx, env.Redis, bobDID, "space-p1"); err != nil {
		t.Fatalf("SetPartyOf(bob): %v", err)
	}
	env.Redis.Set(ctx, kv.AccessKey("space-p1", aliceDID, "read"), "1", 10*time.Minute)
}

func TestAccountDelete(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)
	env.CreateUser(t, aliceDID, "alice.test")
	env.CreateUser(t, bobDID, "bob.test")
	seedAccountData(t, env)

	aliceToken := env.Login(t, aliceDID)
	bobToken := env.Login(t, bobDID)

	status, body := request(t, app, http.MethodDelete, "/api/v1/me?deletePdsRecords=false", "", aliceToken)
	if status != http.StatusNoContent {
		t.Fatalf("DELETE /me: status = %d, want 204 (body=%s)", status, body)
	}

	// 表ごとに alice のデータが消えていることを確かめる。
	for _, c := range aliceRows {
		t.Run(c.name, func(t *testing.T) {
			if n := countRows(t, env, c.query, argsFor(aliceDID, c.didArgs)...); n != 0 {
				t.Fatalf("%s に alice の行が %d 件残っています", c.name, n)
			}
		})
	}

	// bob の（alice と共有していない）データは残る。
	bobKeeps := []struct {
		name  string
		query string
	}{
		{"profiles", "SELECT count(*) FROM profiles WHERE did = ?"},
		{"user_settings", "SELECT count(*) FROM user_settings WHERE did = ?"},
		{"areas", "SELECT count(*) FROM areas WHERE did = ?"},
		{"devices", "SELECT count(*) FROM devices WHERE did = ?"},
		{"notifications", "SELECT count(*) FROM notifications WHERE did = ?"},
		{"records", "SELECT count(*) FROM records WHERE author_did = ?"},
		{"media", "SELECT count(*) FROM media WHERE owner_did = ?"},
		{"space_members", "SELECT count(*) FROM space_members WHERE did = ?"},
	}
	for _, c := range bobKeeps {
		t.Run("keep_"+c.name, func(t *testing.T) {
			if n := countRows(t, env, c.query, bobDID); n == 0 {
				t.Fatalf("%s の bob の行が消えています", c.name)
			}
		})
	}

	// users.deleted_at を立て、profiles は残す。
	if n := countRows(t, env, "SELECT count(*) FROM users WHERE did = ? AND deleted_at IS NULL", aliceDID); n != 0 {
		t.Fatalf("users.deleted_at が立っていません")
	}
	if n := countRows(t, env, "SELECT count(*) FROM profiles WHERE did = ?", aliceDID); n != 1 {
		t.Fatalf("profiles は残すはずが %d 件です", n)
	}

	// Space: 残るメンバーがいれば owner と created_by を引き継ぎ、いなければ Space ごと消す。
	if n := countRows(t, env, "SELECT count(*) FROM spaces WHERE id = 'space-g1'"); n != 1 {
		t.Fatal("space-g1 は残るはずです")
	}
	if n := countRows(t, env, "SELECT count(*) FROM space_members WHERE space_id = 'space-g1' AND did = ? AND role = 'owner'", bobDID); n != 1 {
		t.Fatal("space-g1 の owner が bob に引き継がれていません")
	}
	if n := countRows(t, env, "SELECT count(*) FROM spaces WHERE id = 'space-g1' AND created_by = ?", bobDID); n != 1 {
		t.Fatal("space-g1 の created_by が bob に引き継がれていません")
	}
	if n := countRows(t, env, "SELECT count(*) FROM space_members WHERE space_id = 'space-p1' AND did = ?", bobDID); n != 1 {
		t.Fatal("space-p1 の bob が残っていません")
	}
	if n := countRows(t, env, "SELECT count(*) FROM spaces WHERE id = 'space-g2'"); n != 0 {
		t.Fatal("最後のメンバーが消えた space-g2 は Space ごと消えるはずです")
	}
	if n := countRows(t, env, "SELECT count(*) FROM spaces WHERE id = 'space-p2'"); n != 0 {
		t.Fatal("最後のメンバーが消えた space-p2 は Space ごと消えるはずです")
	}

	// audit_log: 自分の操作の actor_did を NULL にし、account.delete を残す。
	if n := countRows(t, env, "SELECT count(*) FROM audit_log WHERE action = 'block.create' AND actor_did IS NULL"); n != 1 {
		t.Fatal("audit_log.actor_did が NULL になっていません")
	}
	if n := countRows(t, env, "SELECT count(*) FROM audit_log WHERE action = 'account.delete' AND actor_did IS NULL AND target IS NULL"); n != 1 {
		t.Fatal("account.delete が actor/target なしで記録されていません")
	}

	// Redis の掃除。
	ctx := context.Background()
	if n := env.Redis.Exists(ctx, kv.LocationKey(aliceDID)).Val(); n != 0 {
		t.Fatal("loc:alice が残っています")
	}
	if _, err := env.Redis.ZScore(ctx, kv.GeoUsersKey(), aliceDID).Result(); !errors.Is(err, redis.Nil) {
		t.Fatal("geo:users から alice が消えていません")
	}
	for _, key := range []string{
		kv.BleRecentKey(aliceDID),
		kv.SightingKey(aliceDID, bobDID, 100),
		kv.SightingKey(bobDID, aliceDID, 100),
		kv.PeerEdgeKey(aliceDID, bobDID),
		kv.PeerEdgeKey(bobDID, aliceDID),
		kv.PartyOfKey(aliceDID),
		kv.AccessKey("space-p1", aliceDID, "read"),
	} {
		if n := env.Redis.Exists(ctx, key).Val(); n != 0 {
			t.Fatalf("%s が残っています", key)
		}
	}
	if _, err := env.Redis.ZScore(ctx, kv.PeerActiveKey(), aliceDID+"|"+bobDID).Result(); !errors.Is(err, redis.Nil) {
		t.Fatal("pe:active の alice|bob が残っています")
	}
	if _, err := env.Redis.ZScore(ctx, kv.PeerActiveKey(), bobDID+"|"+aliceDID).Result(); !errors.Is(err, redis.Nil) {
		t.Fatal("pe:active の bob|alice が残っています")
	}
	// bob の位置と所属は残る。
	if n := env.Redis.Exists(ctx, kv.LocationKey(bobDID)).Val(); n != 1 {
		t.Fatal("loc:bob が消えています")
	}
	if _, err := env.Redis.ZScore(ctx, kv.GeoUsersKey(), bobDID).Result(); err != nil {
		t.Fatalf("geo:users から bob が消えています: %v", err)
	}
	if n, _ := env.Redis.Exists(ctx, kv.PartyOfKey(bobDID)).Result(); n != 1 {
		t.Fatal("party:of:bob が消えています")
	}

	// メディアのファイル。
	store := env.Storage()
	for _, id := range []string{"media-alice-1", "media-alice-2"} {
		if _, err := os.Stat(store.Path(id, env.Clock.Now(), storage.VariantFull)); !os.IsNotExist(err) {
			t.Fatalf("alice のメディア %s のファイルが残っています (err=%v)", id, err)
		}
	}
	if _, err := os.Stat(store.Path("media-bob-1", env.Clock.Now(), storage.VariantFull)); err != nil {
		t.Fatalf("bob のメディアのファイルが消えています: %v", err)
	}

	// ATProto セッションの取り消し。
	if calls := env.OAuthLogoutCalls(); len(calls) != 1 || calls[0] != "atp-"+aliceDID {
		t.Fatalf("OAuthClient.Logout の呼び出し = %v, want [atp-%s]", calls, aliceDID)
	}

	// alice のトークンは 401、bob は有効なまま。
	status, body = request(t, app, http.MethodGet, "/api/v1/me", "", aliceToken)
	if status != http.StatusUnauthorized {
		t.Fatalf("削除後の alice のトークン: status = %d, want 401 (body=%s)", status, body)
	}
	wantErrorCode(t, body, "unauthenticated")
	status, body = request(t, app, http.MethodGet, "/api/v1/me", "", bobToken)
	if status != http.StatusOK {
		t.Fatalf("bob のトークン: status = %d, want 200 (body=%s)", status, body)
	}

	// 二重削除は 404。
	status, body = request(t, app, http.MethodDelete, "/api/v1/me?deletePdsRecords=false", "", bobToken)
	if status != http.StatusNoContent {
		t.Fatalf("bob の削除: status = %d, want 204 (body=%s)", status, body)
	}
}

func TestAccountDeletePdsUnsupported(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)
	env.CreateUser(t, aliceDID, "alice.test")
	token := env.Login(t, aliceDID)

	status, body := request(t, app, http.MethodDelete, "/api/v1/me?deletePdsRecords=true", "", token)
	if status != http.StatusBadRequest {
		t.Fatalf("deletePdsRecords=true: status = %d, want 400 (body=%s)", status, body)
	}
	e := wantErrorCode(t, body, "invalid_request")
	if e.Error.Details.Reason == "" {
		t.Fatalf("deletePdsRecords=true の理由がありません (body=%s)", body)
	}

	status, body = request(t, app, http.MethodDelete, "/api/v1/me?deletePdsRecords=maybe", "", token)
	if status != http.StatusBadRequest {
		t.Fatalf("deletePdsRecords=maybe: status = %d, want 400 (body=%s)", status, body)
	}

	// 未対応で拒否しただけなので、アカウントは消えていない。
	status, _ = request(t, app, http.MethodGet, "/api/v1/me", "", token)
	if status != http.StatusOK {
		t.Fatalf("アカウントが消えています: status = %d", status)
	}
}

func TestAccountDeleteRelogin(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)
	env.CreateUser(t, aliceDID, "alice.test")
	oldToken := env.Login(t, aliceDID)

	if status, body := request(t, app, http.MethodDelete, "/api/v1/me?deletePdsRecords=false", "", oldToken); status != http.StatusNoContent {
		t.Fatalf("DELETE /me: status = %d, want 204 (body=%s)", status, body)
	}

	// 再ログイン（UpsertUser が deleted_at を消す）で新しいユーザーとして扱われる。
	env.CreateUser(t, aliceDID, "alice.test")
	newToken := env.Login(t, aliceDID)
	status, body := request(t, app, http.MethodGet, "/api/v1/me", "", newToken)
	if status != http.StatusOK {
		t.Fatalf("再ログイン後: status = %d, want 200 (body=%s)", status, body)
	}
	if n := countRows(t, env, "SELECT count(*) FROM users WHERE did = ? AND deleted_at IS NULL", aliceDID); n != 1 {
		t.Fatal("再ログイン後に deleted_at が残っています")
	}

	// 古いトークンは失効したまま。
	status, _ = request(t, app, http.MethodGet, "/api/v1/me", "", oldToken)
	if status != http.StatusUnauthorized {
		t.Fatalf("古いトークン: status = %d, want 401", status)
	}
}
