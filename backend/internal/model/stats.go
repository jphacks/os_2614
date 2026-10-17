package model

import (
	"context"
	"time"

	"github.com/applemango/chain/internal/clock"
	"github.com/applemango/chain/internal/db"
)

const (
	spaceTypeGroup = "jp.i32.chain.space.group"
	spaceTypeParty = "jp.i32.chain.space.party"
	collectionPost = "jp.i32.chain.instance.post"
)

// CountPartiesForUser は did がメンバーだった Party の数を返す（13 §3）。
// 破棄された仮 party は物理削除されているので数に入らない。ended の party は archived として残るので入る。
func CountPartiesForUser(ctx context.Context, q db.Querier, did string) (int, error) {
	var n int
	err := q.QueryRowContext(ctx, `
		SELECT count(*) FROM space_members m
		JOIN spaces s ON s.id = m.space_id
		WHERE m.did = ? AND s.type = ? AND s.state != 'deleted'`,
		did, spaceTypeParty).Scan(&n)
	return n, err
}

// CountActiveGroupsForUser は did が今メンバーの active なグループの数を返す（13 §3）。
func CountActiveGroupsForUser(ctx context.Context, q db.Querier, did string) (int, error) {
	var n int
	err := q.QueryRowContext(ctx, `
		SELECT count(*) FROM space_members m
		JOIN spaces s ON s.id = m.space_id
		WHERE m.did = ? AND m.left_at IS NULL AND s.type = ? AND s.state = 'active'`,
		did, spaceTypeGroup).Scan(&n)
	return n, err
}

// CountPostsForUserBetween は did が作った instance の投稿を [from, to) で数える（13 §3）。
func CountPostsForUserBetween(ctx context.Context, q db.Querier, did string, from, to time.Time) (int, error) {
	var n int
	err := q.QueryRowContext(ctx, `
		SELECT count(*) FROM records
		WHERE author_did = ? AND collection = ? AND deleted_at IS NULL
		  AND created_at >= ? AND created_at < ?`,
		did, collectionPost, clock.UnixMilli(from), clock.UnixMilli(to)).Scan(&n)
	return n, err
}

// ListUserDIDs は未削除のユーザーの DID を did の昇順で返す（12 §5 のダイジェストの対象）。
// after より大きい did から limit 件を返す。
func ListUserDIDs(ctx context.Context, q db.Querier, after string, limit int) ([]string, error) {
	return db.GetItems(ctx, q, `
		SELECT did FROM users
		WHERE deleted_at IS NULL AND did > ?
		ORDER BY did LIMIT ?`,
		[]any{after, limit},
		func(scan db.Scan, s *string) error { return scan(s) })
}

// HasDigestNotification は (did, type, period) の通知が既にあるかを返す（12 §5）。
// period は payload_json の "period" に入れる。
func HasDigestNotification(ctx context.Context, q db.Querier, did, typ, period string) (bool, error) {
	var n int
	err := q.QueryRowContext(ctx, `
		SELECT count(*) FROM notifications
		WHERE did = ? AND type = ? AND payload_json LIKE ?`,
		did, typ, `%"period":"`+period+`"%`).Scan(&n)
	return n > 0, err
}
