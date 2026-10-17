package model

import (
	"context"
	"time"

	"github.com/applemango/chain/internal/clock"
	"github.com/applemango/chain/internal/db"
)

// Block は blocks の行（13 §4）。
type Block struct {
	Blocker   string
	Blocked   string
	CreatedAt time.Time
}

func CreateBlock(ctx context.Context, q db.Querier, blocker, blocked string, now time.Time) error {
	_, err := db.Execute(ctx, q,
		"INSERT INTO blocks (blocker, blocked, created_at) VALUES (?, ?, ?) ON CONFLICT DO NOTHING",
		blocker, blocked, clock.UnixMilli(now))
	return err
}

func DeleteBlock(ctx context.Context, q db.Querier, blocker, blocked string) (bool, error) {
	res, err := db.Execute(ctx, q, "DELETE FROM blocks WHERE blocker = ? AND blocked = ?", blocker, blocked)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

func ListBlocks(ctx context.Context, q db.Querier, blocker string) ([]Block, error) {
	return db.GetItems(ctx, q,
		"SELECT blocker, blocked, created_at FROM blocks WHERE blocker = ? ORDER BY created_at DESC",
		[]any{blocker}, func(scan db.Scan, b *Block) error {
			var created int64
			if err := scan(&b.Blocker, &b.Blocked, &created); err != nil {
				return err
			}
			b.CreatedAt = clock.FromUnixMilli(created)
			return nil
		})
}

// ブロックした時点で、フレンド関係・申請・位置共有を双方向で消す（13 §4）。

func DeleteFriendship(ctx context.Context, q db.Querier, a, b string) (bool, error) {
	res, err := db.Execute(ctx, q, "DELETE FROM friendships WHERE user_a = ? AND user_b = ?", a, b)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

func DeleteFriendRequestsBetween(ctx context.Context, q db.Querier, a, b string) (int64, error) {
	res, err := db.Execute(ctx, q, `
		DELETE FROM friend_requests
		WHERE (from_did = ? AND to_did = ?) OR (from_did = ? AND to_did = ?)`,
		a, b, b, a)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func DeleteLocationSharesBetween(ctx context.Context, q db.Querier, a, b string) (int64, error) {
	res, err := db.Execute(ctx, q, `
		DELETE FROM location_shares
		WHERE (owner = ? AND viewer = ?) OR (owner = ? AND viewer = ?)`,
		a, b, b, a)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
