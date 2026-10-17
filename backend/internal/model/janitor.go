package model

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/applemango/chain/internal/clock"
	"github.com/applemango/chain/internal/db"
	"github.com/applemango/chain/internal/kv"
)

// Janitor（04 §4）で使う SQL と Redis の読み書き。
// 保持期間の判定は service が決め、ここは行・メンバーの読み書きだけを持つ。

// DeleteOldBeaconTokens は window_start が before より古い token を最大 limit 件消す（04 §4）。
func DeleteOldBeaconTokens(ctx context.Context, q db.Querier, beforeUnixSec int64, limit int) (int64, error) {
	return affected(db.Execute(ctx, q, `
		DELETE FROM beacon_tokens WHERE token_hash IN (
			SELECT token_hash FROM beacon_tokens WHERE window_start < ? ORDER BY window_start LIMIT ?)`,
		beforeUnixSec, limit))
}

func DeleteExpiredFriendInvites(ctx context.Context, q db.Querier, before time.Time, limit int) (int64, error) {
	return affected(db.Execute(ctx, q, `
		DELETE FROM friend_invites WHERE code_hash IN (
			SELECT code_hash FROM friend_invites WHERE expires_at < ? ORDER BY expires_at LIMIT ?)`,
		clock.UnixMilli(before), limit))
}

// DeleteOldAppSessions は失効・期限切れから cutoff より前に過ぎたセッションを消す（04 §4）。
func DeleteOldAppSessions(ctx context.Context, q db.Querier, cutoff time.Time, limit int) (int64, error) {
	ms := clock.UnixMilli(cutoff)
	return affected(db.Execute(ctx, q, `
		DELETE FROM app_sessions WHERE id IN (
			SELECT id FROM app_sessions
			WHERE (revoked_at IS NOT NULL AND revoked_at < ?) OR (revoked_at IS NULL AND expires_at < ?)
			ORDER BY id LIMIT ?)`,
		ms, ms, limit))
}

// ListUnattachedMedia は未添付で before より前に作られた media を古い順に返す（04 §4）。
func ListUnattachedMedia(ctx context.Context, q db.Querier, before time.Time, limit int) ([]Media, error) {
	return db.GetItems(ctx, q, `
		SELECT `+mediaColumns+` FROM media
		WHERE space_id IS NULL AND deleted_at IS NULL AND created_at < ?
		ORDER BY created_at, id LIMIT ?`,
		[]any{clock.UnixMilli(before), limit}, scanMedia)
}

// ListSoftDeletedRecords は論理削除から before より前に過ぎた record を返す（04 §4）。
func ListSoftDeletedRecords(ctx context.Context, q db.Querier, before time.Time, limit int) ([]Record, error) {
	return db.GetItems(ctx, q, `
		SELECT `+recordColumns+` FROM records
		WHERE deleted_at IS NOT NULL AND deleted_at < ?
		ORDER BY deleted_at, uri LIMIT ?`,
		[]any{clock.UnixMilli(before), limit}, scanRecord)
}

// ListSoftDeletedMedia は論理削除から before より前に過ぎた media を返す（04 §4）。
func ListSoftDeletedMedia(ctx context.Context, q db.Querier, before time.Time, limit int) ([]Media, error) {
	return db.GetItems(ctx, q, `
		SELECT `+mediaColumns+` FROM media
		WHERE deleted_at IS NOT NULL AND deleted_at < ?
		ORDER BY deleted_at, id LIMIT ?`,
		[]any{clock.UnixMilli(before), limit}, scanMedia)
}

func DeleteMediaByIDs(ctx context.Context, q db.Querier, ids []string) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	args := make([]any, 0, len(ids))
	for _, id := range ids {
		args = append(args, id)
	}
	return affected(db.Execute(ctx, q, "DELETE FROM media WHERE id IN ("+placeholders(len(ids))+")", args...))
}

func DeleteRecordsByURIs(ctx context.Context, q db.Querier, uris []string) (int64, error) {
	if len(uris) == 0 {
		return 0, nil
	}
	args := make([]any, 0, len(uris))
	for _, u := range uris {
		args = append(args, u)
	}
	return affected(db.Execute(ctx, q, "DELETE FROM records WHERE uri IN ("+placeholders(len(uris))+")", args...))
}

func DeleteOldNotifications(ctx context.Context, q db.Querier, before time.Time, limit int) (int64, error) {
	return affected(db.Execute(ctx, q, `
		DELETE FROM notifications WHERE id IN (
			SELECT id FROM notifications WHERE created_at < ? ORDER BY created_at LIMIT ?)`,
		clock.UnixMilli(before), limit))
}

// ExpireGroupInvites は期限切れの未処理の招待を expired にする（04 §4）。
func ExpireGroupInvites(ctx context.Context, q db.Querier, now time.Time) (int64, error) {
	return affected(db.Execute(ctx, q, `
		UPDATE group_invites SET status = 'expired'
		WHERE expires_at < ? AND status IN ('collecting','offered')`,
		clock.UnixMilli(now)))
}

// DeleteExpiredGroupInvites は期限切れから before より前に過ぎた招待を消す（04 §4）。
func DeleteExpiredGroupInvites(ctx context.Context, q db.Querier, before time.Time, limit int) (int64, error) {
	return affected(db.Execute(ctx, q, `
		DELETE FROM group_invites WHERE id IN (
			SELECT id FROM group_invites WHERE expires_at < ? ORDER BY expires_at LIMIT ?)`,
		clock.UnixMilli(before), limit))
}

// ListDeletedUsers は deleted_at が before より前の users を返す（13 §5 の 6）。
func ListDeletedUsers(ctx context.Context, q db.Querier, before time.Time, limit int) ([]User, error) {
	return db.GetItems(ctx, q, `
		SELECT did, handle, pds_url, created_at, deleted_at FROM users
		WHERE deleted_at IS NOT NULL AND deleted_at < ?
		ORDER BY deleted_at LIMIT ?`,
		[]any{clock.UnixMilli(before), limit}, scanUser)
}

// DeleteUserRow は users の行を消す。profiles・user_settings など FK の CASCADE が及ぶものも消える。
func DeleteUserRow(ctx context.Context, q db.Querier, did string) error {
	_, err := db.Execute(ctx, q, "DELETE FROM users WHERE did = ?", did)
	return err
}

// ListStaleGeoUsers は geo:users のうち loc:* が消えているメンバーを先頭から最大 limit 件返す（04 §4）。
func ListStaleGeoUsers(ctx context.Context, r *redis.Client, limit int) ([]string, error) {
	if limit <= 0 {
		limit = 1000
	}
	members, err := r.ZRange(ctx, kv.GeoUsersKey(), 0, int64(limit-1)).Result()
	if err != nil {
		return nil, err
	}
	stale := make([]string, 0)
	for _, m := range members {
		n, err := r.Exists(ctx, kv.LocationKey(m)).Result()
		if err != nil {
			return nil, err
		}
		if n == 0 {
			stale = append(stale, m)
		}
	}
	return stale, nil
}

func RemoveGeoUsers(ctx context.Context, r *redis.Client, dids []string) error {
	if len(dids) == 0 {
		return nil
	}
	args := make([]any, 0, len(dids))
	for _, d := range dids {
		args = append(args, d)
	}
	return r.ZRem(ctx, kv.GeoUsersKey(), args...).Err()
}
