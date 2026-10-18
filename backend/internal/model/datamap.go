package model

import (
	"context"

	"github.com/applemango/chain/internal/db"
)

// DataMapCounts は「データの置き場所」の画面（19 §7.1）に出す自分の件数。
type DataMapCounts struct {
	PublicPosts int
	SpacePosts  int
	Photos      int
	Encounters  int
}

// CountDataMap は did の公開投稿・Space の投稿・写真・すれ違いを数える（削除済みは除く）。
// フレンドの数は friend.CountFriends を使う。
func CountDataMap(ctx context.Context, q db.Querier, did string) (DataMapCounts, error) {
	var c DataMapCounts
	err := q.QueryRowContext(ctx, `
		SELECT
		  (SELECT count(*) FROM public_posts WHERE author_did = ? AND deleted_at IS NULL),
		  (SELECT count(*) FROM records WHERE author_did = ? AND collection = ? AND deleted_at IS NULL),
		  (SELECT count(*) FROM media WHERE owner_did = ? AND deleted_at IS NULL),
		  (SELECT count(*) FROM encounters WHERE user_a = ? OR user_b = ?)`,
		did, did, collectionPost, did, did, did).
		Scan(&c.PublicPosts, &c.SpacePosts, &c.Photos, &c.Encounters)
	return c, err
}
