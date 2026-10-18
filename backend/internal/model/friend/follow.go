package friend

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/applemango/chain/internal/clock"
	"github.com/applemango/chain/internal/db"
	"github.com/applemango/chain/internal/model"
)

// Follow は follows の行（07 §5）。CHAIN の利用者が書いた app.bsky.graph.follow の索引。
type Follow struct {
	ID        string
	URI       string
	Author    string
	Subject   string
	CreatedAt time.Time
	IndexedAt time.Time
}

const followColumns = `id, uri, author_did, subject_did, created_at, indexed_at`

func scanFollow(scan db.Scan, f *Follow) error {
	var created, indexed int64
	if err := scan(&f.ID, &f.URI, &f.Author, &f.Subject, &created, &indexed); err != nil {
		return err
	}
	f.CreatedAt = clock.FromUnixMilli(created)
	f.IndexedAt = clock.FromUnixMilli(indexed)
	return nil
}

// InsertFollow は follows に入れる。同じ組が既にあれば（古い方を持つ。07 §5）何もせず false を返す。
func InsertFollow(ctx context.Context, q db.Querier, f Follow) (bool, error) {
	res, err := db.Execute(ctx, q, `
		INSERT INTO follows (id, uri, author_did, subject_did, created_at, indexed_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT DO NOTHING`,
		f.ID, f.URI, f.Author, f.Subject, clock.UnixMilli(f.CreatedAt), clock.UnixMilli(f.IndexedAt))
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

func GetFollowByID(ctx context.Context, q db.Querier, id string) (*Follow, error) {
	return db.GetItem(ctx, q, "SELECT "+followColumns+" FROM follows WHERE id = ?", []any{id}, scanFollow)
}

func GetFollowByURI(ctx context.Context, q db.Querier, uri string) (*Follow, error) {
	return db.GetItem(ctx, q, "SELECT "+followColumns+" FROM follows WHERE uri = ?", []any{uri}, scanFollow)
}

// GetFollow は author → subject のフォローを返す。なければ sql.ErrNoRows。
func GetFollow(ctx context.Context, q db.Querier, author, subject string) (*Follow, error) {
	return db.GetItem(ctx, q,
		"SELECT "+followColumns+" FROM follows WHERE author_did = ? AND subject_did = ?",
		[]any{author, subject}, scanFollow)
}

// HasFollow は author → subject のフォローがあるかを返す。
func HasFollow(ctx context.Context, q db.Querier, author, subject string) (bool, error) {
	var ok bool
	err := q.QueryRowContext(ctx,
		"SELECT EXISTS(SELECT 1 FROM follows WHERE author_did = ? AND subject_did = ?)", author, subject).Scan(&ok)
	return ok, err
}

// IsMutualFollow は 2 人がお互いにフォローしているかを返す。
func IsMutualFollow(ctx context.Context, q db.Querier, a, b string) (bool, error) {
	var ok bool
	err := q.QueryRowContext(ctx, `
		SELECT EXISTS(SELECT 1 FROM follows WHERE author_did = ? AND subject_did = ?)
		   AND EXISTS(SELECT 1 FROM follows WHERE author_did = ? AND subject_did = ?)`,
		a, b, b, a).Scan(&ok)
	return ok, err
}

func DeleteFollowByURI(ctx context.Context, q db.Querier, uri string) error {
	_, err := db.Execute(ctx, q, "DELETE FROM follows WHERE uri = ?", uri)
	return err
}

func DeleteFollow(ctx context.Context, q db.Querier, author, subject string) error {
	_, err := db.Execute(ctx, q, "DELETE FROM follows WHERE author_did = ? AND subject_did = ?", author, subject)
	return err
}

// ListFollowsByAuthor は author のフォローをすべて返す（ログイン時の読み直し用）。
func ListFollowsByAuthor(ctx context.Context, q db.Querier, author string) ([]Follow, error) {
	return db.GetItems(ctx, q, "SELECT "+followColumns+" FROM follows WHERE author_did = ?", []any{author}, scanFollow)
}

// MutualFriend は相互フォローの 1 組を自分から見た形で表す。
type MutualFriend struct {
	DID   string
	Since time.Time
}

// ListMutualFollows は did と相互フォローの CHAIN 利用者を、相互になった時刻（新しい方のフォローの createdAt）の新しい順に返す。
func ListMutualFollows(ctx context.Context, q db.Querier, did string) ([]MutualFriend, error) {
	return db.GetItems(ctx, q, `
		SELECT f1.subject_did, max(f1.created_at, f2.created_at)
		FROM follows f1
		JOIN follows f2 ON f2.author_did = f1.subject_did AND f2.subject_did = f1.author_did
		JOIN users u ON u.did = f1.subject_did AND u.deleted_at IS NULL
		WHERE f1.author_did = ?
		  AND NOT EXISTS (SELECT 1 FROM blocks b WHERE (b.blocker = f1.author_did AND b.blocked = f1.subject_did)
		                                           OR (b.blocker = f1.subject_did AND b.blocked = f1.author_did))
		ORDER BY max(f1.created_at, f2.created_at) DESC, f1.subject_did`,
		[]any{did}, func(scan db.Scan, m *MutualFriend) error {
			var since int64
			if err := scan(&m.DID, &since); err != nil {
				return err
			}
			m.Since = clock.FromUnixMilli(since)
			return nil
		})
}

// ListMutualFollowPairs は相互フォローの組（a < b）を最大 limit 組返す。friendships を持っているかも添える。
func ListMutualFollowPairs(ctx context.Context, q db.Querier, onlyUnmet bool, limit int) ([][2]string, error) {
	cond := ""
	if onlyUnmet {
		cond = "AND NOT EXISTS (SELECT 1 FROM friendships fr WHERE fr.user_a = f1.author_did AND fr.user_b = f1.subject_did)"
	}
	return db.GetItems(ctx, q, `
		SELECT f1.author_did, f1.subject_did
		FROM follows f1
		JOIN follows f2 ON f2.author_did = f1.subject_did AND f2.subject_did = f1.author_did
		JOIN users u ON u.did = f1.subject_did AND u.deleted_at IS NULL
		WHERE f1.author_did < f1.subject_did `+cond+`
		ORDER BY f1.author_did, f1.subject_did LIMIT ?`,
		[]any{limit}, func(scan db.Scan, p *[2]string) error { return scan(&p[0], &p[1]) })
}

// ListAllFriendshipPairs は friendships の組を最大 limit 組返す。
func ListAllFriendshipPairs(ctx context.Context, q db.Querier, limit int) ([][2]string, error) {
	return db.GetItems(ctx, q, "SELECT user_a, user_b FROM friendships ORDER BY user_a, user_b LIMIT ?",
		[]any{limit}, func(scan db.Scan, p *[2]string) error { return scan(&p[0], &p[1]) })
}

// FollowRequestRow は申請箱の 1 件（フォローと、相手の DID）。
type FollowRequestRow struct {
	Follow
	Peer string
}

// ListIncomingFollows は自分をフォローしていて、自分はフォローしていない CHAIN の利用者のフォローを新しい順に返す。
// 拒否済み（follow_dismissals）とブロック関係は除く。§2 の関係の有無は呼び出し側が確かめる。
func ListIncomingFollows(ctx context.Context, q db.Querier, did string) ([]Follow, error) {
	return db.GetItems(ctx, q, `
		SELECT f.id, f.uri, f.author_did, f.subject_did, f.created_at, f.indexed_at
		FROM follows f
		JOIN users u ON u.did = f.author_did AND u.deleted_at IS NULL
		WHERE f.subject_did = ?
		  AND NOT EXISTS (SELECT 1 FROM follows r WHERE r.author_did = f.subject_did AND r.subject_did = f.author_did)
		  AND NOT EXISTS (SELECT 1 FROM follow_dismissals d WHERE d.did = f.subject_did AND d.follower_did = f.author_did)
		  AND NOT EXISTS (SELECT 1 FROM blocks b WHERE (b.blocker = f.subject_did AND b.blocked = f.author_did)
		                                           OR (b.blocker = f.author_did AND b.blocked = f.subject_did))
		ORDER BY f.created_at DESC, f.id DESC`,
		[]any{did}, scanFollow)
}

// ListOutgoingFollows は自分がフォローしていて、相手はフォローしていない CHAIN の利用者のフォローを新しい順に返す。
func ListOutgoingFollows(ctx context.Context, q db.Querier, did string) ([]Follow, error) {
	return db.GetItems(ctx, q, `
		SELECT f.id, f.uri, f.author_did, f.subject_did, f.created_at, f.indexed_at
		FROM follows f
		JOIN users u ON u.did = f.subject_did AND u.deleted_at IS NULL
		WHERE f.author_did = ?
		  AND NOT EXISTS (SELECT 1 FROM follows r WHERE r.author_did = f.subject_did AND r.subject_did = f.author_did)
		  AND NOT EXISTS (SELECT 1 FROM blocks b WHERE (b.blocker = f.subject_did AND b.blocked = f.author_did)
		                                           OR (b.blocker = f.author_did AND b.blocked = f.subject_did))
		ORDER BY f.created_at DESC, f.id DESC`,
		[]any{did}, scanFollow)
}

// IsDismissed は did が follower のフォローを受信箱で拒否済みかを返す。
func IsDismissed(ctx context.Context, q db.Querier, did, follower string) (bool, error) {
	var ok bool
	err := q.QueryRowContext(ctx,
		"SELECT EXISTS(SELECT 1 FROM follow_dismissals WHERE did = ? AND follower_did = ?)", did, follower).Scan(&ok)
	return ok, err
}

func CreateDismissal(ctx context.Context, q db.Querier, did, follower string, now time.Time) error {
	_, err := db.Execute(ctx, q,
		"INSERT INTO follow_dismissals (did, follower_did, created_at) VALUES (?, ?, ?) ON CONFLICT DO NOTHING",
		did, follower, clock.UnixMilli(now))
	return err
}

func DeleteDismissal(ctx context.Context, q db.Querier, did, follower string) error {
	_, err := db.Execute(ctx, q, "DELETE FROM follow_dismissals WHERE did = ? AND follower_did = ?", did, follower)
	return err
}

// DeleteFriendship は friendships の行を消す。a, b は正規化済みであること。
func DeleteFriendship(ctx context.Context, q db.Querier, a, b string) (bool, error) {
	return model.DeleteFriendship(ctx, q, a, b)
}

// UserGraphSyncedAt は follow / block を最後に読み直した時刻を返す。未読み込みなら nil。
func UserGraphSyncedAt(ctx context.Context, q db.Querier, did string) (*time.Time, error) {
	var at sql.NullInt64
	err := q.QueryRowContext(ctx, "SELECT graph_synced_at FROM users WHERE did = ?", did).Scan(&at)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil || !at.Valid {
		return nil, err
	}
	t := clock.FromUnixMilli(at.Int64)
	return &t, nil
}

func SetUserGraphSyncedAt(ctx context.Context, q db.Querier, did string, at time.Time) error {
	_, err := db.Execute(ctx, q, "UPDATE users SET graph_synced_at = ? WHERE did = ?", clock.UnixMilli(at), did)
	return err
}

// Bluesky のブロックの索引（blocks の source = 'bsky'。07 §2.5）。

// UpsertBskyBlock は Bluesky のブロックを blocks に入れる。CHAIN のブロックが既にあれば何もしない。
func UpsertBskyBlock(ctx context.Context, q db.Querier, blocker, blocked, uri string, now time.Time) error {
	_, err := db.Execute(ctx, q, `
		INSERT INTO blocks (blocker, blocked, created_at, source, uri) VALUES (?, ?, ?, 'bsky', ?)
		ON CONFLICT (blocker, blocked) DO UPDATE SET uri = excluded.uri WHERE blocks.source = 'bsky'`,
		blocker, blocked, clock.UnixMilli(now), uri)
	return err
}

// BskyBlock は blocks の source = 'bsky' の行。
type BskyBlock struct {
	Blocked string
	URI     string
}

func ListBskyBlocks(ctx context.Context, q db.Querier, blocker string) ([]BskyBlock, error) {
	return db.GetItems(ctx, q, "SELECT blocked, uri FROM blocks WHERE blocker = ? AND source = 'bsky'", []any{blocker},
		func(scan db.Scan, b *BskyBlock) error {
			var uri sql.NullString
			if err := scan(&b.Blocked, &uri); err != nil {
				return err
			}
			b.URI = uri.String
			return nil
		})
}

// GetBskyBlockByURI は uri の Bluesky のブロックを返す。なければ sql.ErrNoRows。
func GetBskyBlockByURI(ctx context.Context, q db.Querier, uri string) (*model.Block, error) {
	return db.GetItem(ctx, q, "SELECT blocker, blocked, created_at, source FROM blocks WHERE uri = ? AND source = 'bsky'",
		[]any{uri}, func(scan db.Scan, b *model.Block) error {
			var created int64
			if err := scan(&b.Blocker, &b.Blocked, &created, &b.Source); err != nil {
				return err
			}
			b.CreatedAt = clock.FromUnixMilli(created)
			return nil
		})
}

func DeleteBskyBlock(ctx context.Context, q db.Querier, blocker, blocked string) error {
	_, err := db.Execute(ctx, q, "DELETE FROM blocks WHERE blocker = ? AND blocked = ? AND source = 'bsky'", blocker, blocked)
	return err
}

// LatestATProtoSessionID は did の有効な ATProto セッションのうち、最も新しいものの ID を返す。無ければ空。
// 招待の作成者のように、いま操作していない人のセッションでフォローを書くときに使う（07 §2.1）。
func LatestATProtoSessionID(ctx context.Context, q db.Querier, did string) (string, error) {
	var id string
	err := q.QueryRowContext(ctx,
		"SELECT id FROM atproto_oauth_sessions WHERE did = ? AND revoked_at IS NULL ORDER BY updated_at DESC LIMIT 1", did).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return id, err
}
