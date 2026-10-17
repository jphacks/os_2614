package model

import (
	"context"

	"github.com/applemango/chain/internal/db"
)

// HasRelationship は a と b の間に CHAIN 上の接点があるかを返す（13 §1.2）。
// 自分自身は service が判断する。SQL は friendships / friend_requests / encounters / 同じ Space のメンバーを見る。
func HasRelationship(ctx context.Context, q db.Querier, a, b string) (bool, error) {
	if a > b {
		a, b = b, a
	}
	var ok bool
	err := q.QueryRowContext(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM friendships WHERE user_a = ? AND user_b = ?
			UNION ALL
			SELECT 1 FROM friend_requests
			  WHERE status = 'pending'
			    AND ((from_did = ? AND to_did = ?) OR (from_did = ? AND to_did = ?))
			UNION ALL
			SELECT 1 FROM encounters
			  WHERE user_a = ? AND user_b = ? AND visibility = 'revealed'
			UNION ALL
			SELECT 1
			  FROM space_members m1
			  JOIN space_members m2 ON m1.space_id = m2.space_id
			  JOIN spaces s ON s.id = m1.space_id
			  WHERE m1.did = ? AND m2.did = ? AND s.state != 'deleted'
		)`,
		a, b,
		a, b, b, a,
		a, b,
		a, b).Scan(&ok)
	return ok, err
}

// FriendRelation は GET /encounters の relation を決める材料（06 §6）。
type FriendRelation struct {
	Friend    bool // friendships の行がある
	Requested bool // 自分から相手への pending 申請がある
	Incoming  bool // 相手から自分への pending 申請がある
}

func GetFriendRelation(ctx context.Context, q db.Querier, me, peer string) (*FriendRelation, error) {
	a, b := me, peer
	if a > b {
		a, b = b, a
	}
	r := &FriendRelation{}
	err := q.QueryRowContext(ctx, `
		SELECT
		  EXISTS(SELECT 1 FROM friendships WHERE user_a = ? AND user_b = ?),
		  EXISTS(SELECT 1 FROM friend_requests WHERE from_did = ? AND to_did = ? AND status = 'pending'),
		  EXISTS(SELECT 1 FROM friend_requests WHERE from_did = ? AND to_did = ? AND status = 'pending')`,
		a, b, me, peer, peer, me).Scan(&r.Friend, &r.Requested, &r.Incoming)
	return r, err
}

// IsBlocked はどちら向きでも blocks の行があるかを返す。
func IsBlocked(ctx context.Context, q db.Querier, a, b string) (bool, error) {
	var ok bool
	err := q.QueryRowContext(ctx,
		"SELECT EXISTS(SELECT 1 FROM blocks WHERE (blocker = ? AND blocked = ?) OR (blocker = ? AND blocked = ?))",
		a, b, b, a).Scan(&ok)
	return ok, err
}

// BlockedDIDs は did と、どちら向きかに関わらずブロック関係にある相手の集合を返す（10 §4.2）。
func BlockedDIDs(ctx context.Context, q db.Querier, did string) (map[string]bool, error) {
	items, err := db.GetItems(ctx, q,
		"SELECT CASE WHEN blocker = ? THEN blocked ELSE blocker END FROM blocks WHERE blocker = ? OR blocked = ?",
		[]any{did, did, did},
		func(scan db.Scan, s *string) error { return scan(s) })
	if err != nil {
		return nil, err
	}
	set := make(map[string]bool, len(items))
	for _, d := range items {
		set[d] = true
	}
	return set, nil
}
