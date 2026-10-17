//go:build integration

package service_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/applemango/chain/internal/kv"
	"github.com/applemango/chain/internal/model"
	"github.com/applemango/chain/internal/service"
	"github.com/applemango/chain/internal/storage"
	"github.com/applemango/chain/internal/testutil"
)

// Janitor（04 §4、13 §5 の 6）の掃除を、保持期間の前後で確認する。
// 基準時刻は testutil.Epoch。時計は env.Clock.Advance で進める。

// janitorExec は生 SQL を 1 文実行する。テストでの直接の INSERT / UPDATE に使う。
func janitorExec(t *testing.T, env *testutil.Env, query string, args ...any) {
	t.Helper()
	if _, err := env.DB.ExecContext(context.Background(), query, args...); err != nil {
		t.Fatalf("exec %q: %v", query, err)
	}
}

// janitorCount は COUNT(*) を返す。
func janitorCount(t *testing.T, env *testutil.Env, query string, args ...any) int {
	t.Helper()
	var n int
	if err := env.DB.QueryRowContext(context.Background(), query, args...).Scan(&n); err != nil {
		t.Fatalf("count %q: %v", query, err)
	}
	return n
}

// janitorString は 1 列を string で読む。
func janitorString(t *testing.T, env *testutil.Env, query string, args ...any) string {
	t.Helper()
	var s string
	if err := env.DB.QueryRowContext(context.Background(), query, args...).Scan(&s); err != nil {
		t.Fatalf("query %q: %v", query, err)
	}
	return s
}

// janitorSweep は Sweep を 1 回呼び、エラーなら落とす。
func janitorSweep(t *testing.T, ctx context.Context, svc *service.Janitor) service.JanitorResult {
	t.Helper()
	res, err := svc.Sweep(ctx)
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	return res
}

// janitorCreateSpace は records / group_invites が参照する Space を 1 つ作る。
func janitorCreateSpace(t *testing.T, env *testutil.Env, id, createdBy string) {
	t.Helper()
	janitorExec(t, env, `
		INSERT INTO spaces (id, type, authority, ref, read_policy, write_policy, name, state, created_by, created_at, archived_at)
		VALUES (?, 'jp.i32.chain.space.group', 'did:web:chain.test', ?, 'managing-app', 'managing-app', 'test space', 'active', ?, ?, NULL)`,
		id, "at://did:web:chain.test/space/jp.i32.chain.space.group/"+id, createdBy, env.Clock.Now().UnixMilli())
}

// janitorInsertMedia は media の行を 1 つ入れる。spaceID / deletedAt が nil なら NULL。
func janitorInsertMedia(t *testing.T, env *testutil.Env, id, owner string, spaceID *string, createdAt time.Time, deletedAt *time.Time) {
	t.Helper()
	var space any
	if spaceID != nil {
		space = *spaceID
	}
	var deleted any
	if deletedAt != nil {
		deleted = deletedAt.UnixMilli()
	}
	janitorExec(t, env, `
		INSERT INTO media (id, owner_did, space_id, mime, width, height, bytes, sha256, taken_at, created_at, deleted_at)
		VALUES (?, ?, ?, 'image/jpeg', 8, 8, 4, 'sha', NULL, ?, ?)`,
		id, owner, space, createdAt.UnixMilli(), deleted)
}

// janitorFileExists はストア上のファイルの有無を返す。Stat の予期しないエラーは落とす。
func janitorFileExists(t *testing.T, env *testutil.Env, id string, createdAt time.Time, v storage.Variant) bool {
	t.Helper()
	_, err := os.Stat(env.Storage().Path(id, createdAt, v))
	switch {
	case err == nil:
		return true
	case os.IsNotExist(err):
		return false
	default:
		t.Fatalf("stat media %s (%s): %v", id, v, err)
		return false
	}
}

func TestJanitorRetention(t *testing.T) {
	t.Run("beacon_tokens は window_start から 48 時間", func(t *testing.T) {
		ctx := context.Background()
		env := testutil.NewEnv(t)
		svc := env.Janitor()
		env.CreateUser(t, alice, "alice.test")

		janitorExec(t, env, `INSERT INTO beacon_tokens (token_hash, token_enc, did, window_start) VALUES (?, ?, ?, ?)`,
			bytes.Repeat([]byte{0xAB}, 32), []byte("enc"), alice, testutil.Epoch.Unix())

		env.Clock.Advance(47*time.Hour + 59*time.Minute)
		res := janitorSweep(t, ctx, svc)
		if res.BeaconTokens != 0 {
			t.Fatalf("47h59m の BeaconTokens = %d, want 0 (保持期内)", res.BeaconTokens)
		}
		if n := janitorCount(t, env, "SELECT COUNT(*) FROM beacon_tokens"); n != 1 {
			t.Fatalf("前の beacon_tokens = %d, want 1", n)
		}

		env.Clock.Advance(2 * time.Minute) // 48h01m
		res = janitorSweep(t, ctx, svc)
		if res.BeaconTokens != 1 {
			t.Fatalf("48h01m の BeaconTokens = %d, want 1", res.BeaconTokens)
		}
		if n := janitorCount(t, env, "SELECT COUNT(*) FROM beacon_tokens"); n != 0 {
			t.Fatalf("後の beacon_tokens = %d, want 0", n)
		}
	})

	t.Run("friend_invites は期限から 1 日", func(t *testing.T) {
		ctx := context.Background()
		env := testutil.NewEnv(t)
		svc := env.Janitor()
		env.CreateUser(t, alice, "alice.test")

		ms := testutil.Epoch.UnixMilli()
		janitorExec(t, env, `INSERT INTO friend_invites (code_hash, did, created_at, expires_at) VALUES (?, ?, ?, ?)`,
			"hash-friend-1", alice, ms, ms)

		env.Clock.Advance(23*time.Hour + 59*time.Minute)
		res := janitorSweep(t, ctx, svc)
		if res.FriendInvites != 0 {
			t.Fatalf("23h59m の FriendInvites = %d, want 0 (保持期内)", res.FriendInvites)
		}
		if n := janitorCount(t, env, "SELECT COUNT(*) FROM friend_invites"); n != 1 {
			t.Fatalf("前の friend_invites = %d, want 1", n)
		}

		env.Clock.Advance(2 * time.Minute) // 24h01m
		res = janitorSweep(t, ctx, svc)
		if res.FriendInvites != 1 {
			t.Fatalf("24h01m の FriendInvites = %d, want 1", res.FriendInvites)
		}
		if n := janitorCount(t, env, "SELECT COUNT(*) FROM friend_invites"); n != 0 {
			t.Fatalf("後の friend_invites = %d, want 0", n)
		}
	})

	t.Run("app_sessions は失効・期限切れから 7 日", func(t *testing.T) {
		ctx := context.Background()
		env := testutil.NewEnv(t)
		svc := env.Janitor()
		env.CreateUser(t, alice, "alice.test")

		ms := testutil.Epoch.UnixMilli()
		future := testutil.Epoch.Add(30 * 24 * time.Hour).UnixMilli()
		janitorExec(t, env, `INSERT INTO atproto_oauth_sessions (id, did, data_enc, created_at, updated_at) VALUES (?, ?, x'', ?, ?)`,
			"atp-jan", alice, ms, ms)

		// 失効済み（revoked_at が cutoff より前）。
		janitorExec(t, env, `INSERT INTO app_sessions (id, did, atproto_session, family_id, refresh_hash, device_name, created_at, last_used_at, expires_at, revoked_at)
			VALUES (?, ?, 'atp-jan', ?, ?, 'test', ?, ?, ?, ?)`,
			"sid-revoked", alice, "fam-revoked", "hash-revoked", ms, ms, future, ms)
		// 未失効で期限切れ（expires_at が cutoff より前）。
		janitorExec(t, env, `INSERT INTO app_sessions (id, did, atproto_session, family_id, refresh_hash, device_name, created_at, last_used_at, expires_at, revoked_at)
			VALUES (?, ?, 'atp-jan', ?, ?, 'test', ?, ?, ?, NULL)`,
			"sid-expired", alice, "fam-expired", "hash-expired", ms, ms, ms)

		env.Clock.Advance(6*24*time.Hour + 23*time.Hour + 59*time.Minute)
		res := janitorSweep(t, ctx, svc)
		if res.AppSessions != 0 {
			t.Fatalf("6d23h59m の AppSessions = %d, want 0 (保持期内)", res.AppSessions)
		}
		if n := janitorCount(t, env, "SELECT COUNT(*) FROM app_sessions"); n != 2 {
			t.Fatalf("前の app_sessions = %d, want 2", n)
		}

		env.Clock.Advance(2 * time.Minute) // 7d01m
		res = janitorSweep(t, ctx, svc)
		if res.AppSessions != 2 {
			t.Fatalf("7d01m の AppSessions = %d, want 2 (失効・期限切れの両方)", res.AppSessions)
		}
		if n := janitorCount(t, env, "SELECT COUNT(*) FROM app_sessions"); n != 0 {
			t.Fatalf("後の app_sessions = %d, want 0", n)
		}
	})

	t.Run("未添付 media は作成から 24 時間で行とファイルを削除", func(t *testing.T) {
		ctx := context.Background()
		env := testutil.NewEnv(t)
		svc := env.Janitor()
		env.CreateUser(t, alice, "alice.test")

		id := "media-unattached-1"
		createdAt := testutil.Epoch
		if err := env.Storage().Save(id, createdAt, []byte("full"), []byte("thumb")); err != nil {
			t.Fatalf("Save(%s): %v", id, err)
		}
		janitorInsertMedia(t, env, id, alice, nil, createdAt, nil)

		env.Clock.Advance(23*time.Hour + 59*time.Minute)
		res := janitorSweep(t, ctx, svc)
		if res.UnattachedMedia != 0 {
			t.Fatalf("23h59m の UnattachedMedia = %d, want 0 (保持期内)", res.UnattachedMedia)
		}
		if n := janitorCount(t, env, "SELECT COUNT(*) FROM media WHERE id = ?", id); n != 1 {
			t.Fatalf("前の media = %d, want 1", n)
		}
		if !janitorFileExists(t, env, id, createdAt, storage.VariantFull) {
			t.Fatalf("前の full ファイル %s が消えている", id)
		}

		env.Clock.Advance(2 * time.Minute) // 24h01m
		res = janitorSweep(t, ctx, svc)
		if res.UnattachedMedia != 1 {
			t.Fatalf("24h01m の UnattachedMedia = %d, want 1", res.UnattachedMedia)
		}
		if n := janitorCount(t, env, "SELECT COUNT(*) FROM media WHERE id = ?", id); n != 0 {
			t.Fatalf("後の media = %d, want 0", n)
		}
		if janitorFileExists(t, env, id, createdAt, storage.VariantFull) {
			t.Fatalf("後の full ファイル %s が残っている", id)
		}
		if janitorFileExists(t, env, id, createdAt, storage.VariantThumb) {
			t.Fatalf("後の thumb ファイル %s が残っている", id)
		}
	})

	t.Run("論理削除した records は 30 日で物理削除", func(t *testing.T) {
		ctx := context.Background()
		env := testutil.NewEnv(t)
		svc := env.Janitor()
		env.CreateUser(t, alice, "alice.test")

		spaceID := "space-records-1"
		janitorCreateSpace(t, env, spaceID, alice)
		uri := "at://did:web:chain.test/space/jp.i32.chain.space.group/" + spaceID + "/" + alice + "/jp.i32.chain.instance.post/rkey1"
		ms := testutil.Epoch.UnixMilli()
		janitorExec(t, env, `INSERT INTO records (uri, space_id, author_did, collection, rkey, value_json, created_at, deleted_at, deleted_by, mod_label, mod_state)
			VALUES (?, ?, ?, 'jp.i32.chain.instance.post', 'rkey1', '{}', ?, ?, ?, 'ok', 'done')`,
			uri, spaceID, alice, ms, ms, alice)

		env.Clock.Advance(29*24*time.Hour + 23*time.Hour + 59*time.Minute)
		res := janitorSweep(t, ctx, svc)
		if res.Records != 0 {
			t.Fatalf("29d23h59m の Records = %d, want 0 (保持期内)", res.Records)
		}
		if n := janitorCount(t, env, "SELECT COUNT(*) FROM records WHERE uri = ?", uri); n != 1 {
			t.Fatalf("前の records = %d, want 1", n)
		}

		env.Clock.Advance(2 * time.Minute) // 30d01m
		res = janitorSweep(t, ctx, svc)
		if res.Records != 1 {
			t.Fatalf("30d01m の Records = %d, want 1", res.Records)
		}
		if n := janitorCount(t, env, "SELECT COUNT(*) FROM records WHERE uri = ?", uri); n != 0 {
			t.Fatalf("後の records = %d, want 0", n)
		}
	})

	t.Run("論理削除した media は 30 日で行とファイルを削除", func(t *testing.T) {
		ctx := context.Background()
		env := testutil.NewEnv(t)
		svc := env.Janitor()
		env.CreateUser(t, alice, "alice.test")

		id := "media-deleted-1"
		createdAt := testutil.Epoch
		deletedAt := testutil.Epoch
		if err := env.Storage().Save(id, createdAt, []byte("full"), []byte("thumb")); err != nil {
			t.Fatalf("Save(%s): %v", id, err)
		}
		janitorInsertMedia(t, env, id, alice, nil, createdAt, &deletedAt)

		env.Clock.Advance(29*24*time.Hour + 23*time.Hour + 59*time.Minute)
		res := janitorSweep(t, ctx, svc)
		if res.DeletedMedia != 0 {
			t.Fatalf("29d23h59m の DeletedMedia = %d, want 0 (保持期内)", res.DeletedMedia)
		}
		if n := janitorCount(t, env, "SELECT COUNT(*) FROM media WHERE id = ?", id); n != 1 {
			t.Fatalf("前の media = %d, want 1", n)
		}
		if !janitorFileExists(t, env, id, createdAt, storage.VariantFull) {
			t.Fatalf("前の full ファイル %s が消えている", id)
		}

		env.Clock.Advance(2 * time.Minute) // 30d01m
		res = janitorSweep(t, ctx, svc)
		if res.DeletedMedia != 1 {
			t.Fatalf("30d01m の DeletedMedia = %d, want 1", res.DeletedMedia)
		}
		if n := janitorCount(t, env, "SELECT COUNT(*) FROM media WHERE id = ?", id); n != 0 {
			t.Fatalf("後の media = %d, want 0", n)
		}
		if janitorFileExists(t, env, id, createdAt, storage.VariantFull) {
			t.Fatalf("後の full ファイル %s が残っている", id)
		}
	})

	t.Run("notifications は 90 日で削除し outbox も連鎖削除", func(t *testing.T) {
		ctx := context.Background()
		env := testutil.NewEnv(t)
		svc := env.Janitor()
		env.CreateUser(t, alice, "alice.test")

		nid := "notif-1"
		ms := testutil.Epoch.UnixMilli()
		janitorExec(t, env, `INSERT INTO notifications (id, did, type, payload_json, created_at, read_at) VALUES (?, ?, 'test', '{}', ?, NULL)`,
			nid, alice, ms)
		janitorExec(t, env, `INSERT INTO notification_outbox (id, notification_id, status, attempts, next_attempt_at) VALUES (?, ?, 'queued', 0, ?)`,
			"outbox-1", nid, ms)

		env.Clock.Advance(89*24*time.Hour + 23*time.Hour + 59*time.Minute)
		res := janitorSweep(t, ctx, svc)
		if res.Notifications != 0 {
			t.Fatalf("89d23h59m の Notifications = %d, want 0 (保持期内)", res.Notifications)
		}
		if n := janitorCount(t, env, "SELECT COUNT(*) FROM notifications"); n != 1 {
			t.Fatalf("前の notifications = %d, want 1", n)
		}
		if n := janitorCount(t, env, "SELECT COUNT(*) FROM notification_outbox"); n != 1 {
			t.Fatalf("前の notification_outbox = %d, want 1", n)
		}

		env.Clock.Advance(2 * time.Minute) // 90d01m
		res = janitorSweep(t, ctx, svc)
		if res.Notifications != 1 {
			t.Fatalf("90d01m の Notifications = %d, want 1", res.Notifications)
		}
		if n := janitorCount(t, env, "SELECT COUNT(*) FROM notifications"); n != 0 {
			t.Fatalf("後の notifications = %d, want 0", n)
		}
		if n := janitorCount(t, env, "SELECT COUNT(*) FROM notification_outbox"); n != 0 {
			t.Fatalf("後の notification_outbox = %d, want 0 (FK CASCADE)", n)
		}
	})

	t.Run("group_invites は期限切れで expired、30 日後に削除", func(t *testing.T) {
		ctx := context.Background()
		env := testutil.NewEnv(t)
		svc := env.Janitor()
		env.CreateUser(t, alice, "alice.test")
		env.CreateUser(t, bob, "bob.test")

		spaceID := "space-invites-1"
		janitorCreateSpace(t, env, spaceID, alice)
		ms := testutil.Epoch.UnixMilli()
		janitorExec(t, env, `INSERT INTO group_invites (id, space_id, target_did, created_by, status, created_at, expires_at)
			VALUES (?, ?, ?, ?, 'collecting', ?, ?)`,
			"invite-1", spaceID, bob, alice, ms, ms)

		env.Clock.Advance(2 * time.Minute)
		res := janitorSweep(t, ctx, svc)
		if res.InvitesExpired != 1 {
			t.Fatalf("期限切れの InvitesExpired = %d, want 1", res.InvitesExpired)
		}
		if res.InvitesDeleted != 0 {
			t.Fatalf("期限直後の InvitesDeleted = %d, want 0", res.InvitesDeleted)
		}
		if status := janitorString(t, env, "SELECT status FROM group_invites WHERE id = ?", "invite-1"); status != "expired" {
			t.Fatalf("group_invites.status = %q, want expired", status)
		}
		if n := janitorCount(t, env, "SELECT COUNT(*) FROM group_invites WHERE id = ?", "invite-1"); n != 1 {
			t.Fatalf("30 日前の group_invites = %d, want 1", n)
		}

		env.Clock.Advance(29*24*time.Hour + 23*time.Hour + 59*time.Minute) // 合計 30d01m
		res = janitorSweep(t, ctx, svc)
		if res.InvitesDeleted != 1 {
			t.Fatalf("30d01m の InvitesDeleted = %d, want 1", res.InvitesDeleted)
		}
		if n := janitorCount(t, env, "SELECT COUNT(*) FROM group_invites WHERE id = ?", "invite-1"); n != 0 {
			t.Fatalf("後の group_invites = %d, want 0", n)
		}
	})

	t.Run("geo:users は loc が消えたメンバーだけ削除", func(t *testing.T) {
		ctx := context.Background()
		env := testutil.NewEnv(t)
		svc := env.Janitor()

		stale := "did:plc:stale"
		active := "did:plc:active"
		if err := env.Redis.ZAdd(ctx, kv.GeoUsersKey(), redis.Z{Score: 0, Member: stale}).Err(); err != nil {
			t.Fatalf("ZAdd(%s): %v", stale, err)
		}
		loc := model.Location{DID: active, Lat: 35, Lon: 135, AccuracyM: 5, RecordedAt: env.Clock.Now(), Source: "gps"}
		if err := model.SaveLocation(ctx, env.Redis, loc, 30*time.Minute); err != nil {
			t.Fatalf("SaveLocation(%s): %v", active, err)
		}

		res := janitorSweep(t, ctx, svc)
		if res.GeoUsers != 1 {
			t.Fatalf("GeoUsers = %d, want 1", res.GeoUsers)
		}
		if err := env.Redis.ZScore(ctx, kv.GeoUsersKey(), stale).Err(); !errors.Is(err, redis.Nil) {
			t.Fatalf("stale が geo:users に残っている: %v", err)
		}
		if _, err := env.Redis.ZScore(ctx, kv.GeoUsersKey(), active).Result(); err != nil {
			t.Fatalf("active が geo:users から消えている: %v", err)
		}
	})

	t.Run("users は deleted_at から 30 日で users と profiles を削除", func(t *testing.T) {
		ctx := context.Background()
		env := testutil.NewEnv(t)
		svc := env.Janitor()
		env.CreateUser(t, alice, "alice.test")
		janitorExec(t, env, "UPDATE users SET deleted_at = ? WHERE did = ?", testutil.Epoch.UnixMilli(), alice)

		env.Clock.Advance(29 * 24 * time.Hour)
		res := janitorSweep(t, ctx, svc)
		if res.Users != 0 {
			t.Fatalf("29d の Users = %d, want 0 (保持期内)", res.Users)
		}
		if n := janitorCount(t, env, "SELECT COUNT(*) FROM users WHERE did = ?", alice); n != 1 {
			t.Fatalf("前の users = %d, want 1", n)
		}
		if n := janitorCount(t, env, "SELECT COUNT(*) FROM profiles WHERE did = ?", alice); n != 1 {
			t.Fatalf("前の profiles = %d, want 1", n)
		}

		env.Clock.Advance(24*time.Hour + time.Minute) // 30d01m
		res = janitorSweep(t, ctx, svc)
		if res.Users != 1 {
			t.Fatalf("30d01m の Users = %d, want 1", res.Users)
		}
		if n := janitorCount(t, env, "SELECT COUNT(*) FROM users WHERE did = ?", alice); n != 0 {
			t.Fatalf("後の users = %d, want 0", n)
		}
		if n := janitorCount(t, env, "SELECT COUNT(*) FROM profiles WHERE did = ?", alice); n != 0 {
			t.Fatalf("後の profiles = %d, want 0 (FK CASCADE)", n)
		}
	})
}

func TestJanitorBatchCap(t *testing.T) {
	ctx := context.Background()
	env := testutil.NewEnv(t)
	svc := env.Janitor()
	env.CreateUser(t, alice, "alice.test")

	const total = 1005
	old := testutil.Epoch.Add(-91 * 24 * time.Hour).UnixMilli()
	for i := 0; i < total; i++ {
		janitorExec(t, env, `INSERT INTO notifications (id, did, type, payload_json, created_at, read_at) VALUES (?, ?, 'test', '{}', ?, NULL)`,
			fmt.Sprintf("notif-%04d", i), alice, old)
	}
	if n := janitorCount(t, env, "SELECT COUNT(*) FROM notifications"); n != total {
		t.Fatalf("用意した notifications = %d, want %d", n, total)
	}

	res := janitorSweep(t, ctx, svc)
	if res.Notifications != service.JanitorBatch {
		t.Fatalf("1 回目の Notifications = %d, want %d", res.Notifications, service.JanitorBatch)
	}
	if n := janitorCount(t, env, "SELECT COUNT(*) FROM notifications"); n != total-service.JanitorBatch {
		t.Fatalf("1 回目の残り = %d, want %d", n, total-service.JanitorBatch)
	}

	res = janitorSweep(t, ctx, svc)
	if res.Notifications != total-service.JanitorBatch {
		t.Fatalf("2 回目の Notifications = %d, want %d", res.Notifications, total-service.JanitorBatch)
	}
	if n := janitorCount(t, env, "SELECT COUNT(*) FROM notifications"); n != 0 {
		t.Fatalf("2 回目の残り = %d, want 0", n)
	}
}
