package model

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/applemango/chain/internal/clock"
	"github.com/applemango/chain/internal/db"
)

// Friendship は friendships の行（04 §2.3）。user_a < user_b に正規化されている。
type Friendship struct {
	UserA     string
	UserB     string
	CreatedAt time.Time
	Via       string
}

const friendshipColumns = `user_a, user_b, created_at, via`

func scanFriendship(scan db.Scan, f *Friendship) error {
	var created int64
	if err := scan(&f.UserA, &f.UserB, &created, &f.Via); err != nil {
		return err
	}
	f.CreatedAt = clock.FromUnixMilli(created)
	return nil
}

// GetFriendship は 2 人のフレンド関係を返す。引数の順序は問わない。なければ sql.ErrNoRows。
func GetFriendship(ctx context.Context, q db.Querier, a, b string) (*Friendship, error) {
	if a > b {
		a, b = b, a
	}
	return db.GetItem(ctx, q,
		"SELECT "+friendshipColumns+" FROM friendships WHERE user_a = ? AND user_b = ?",
		[]any{a, b}, scanFriendship)
}

// ListFriendships は did が関わるフレンド関係を新しい順に返す。
func ListFriendships(ctx context.Context, q db.Querier, did string) ([]Friendship, error) {
	return db.GetItems(ctx, q, `
		SELECT `+friendshipColumns+` FROM friendships
		WHERE user_a = ? OR user_b = ?
		ORDER BY created_at DESC, user_a, user_b`,
		[]any{did, did}, scanFriendship)
}

func CountFriends(ctx context.Context, q db.Querier, did string) (int, error) {
	var n int
	err := q.QueryRowContext(ctx,
		"SELECT count(*) FROM friendships WHERE user_a = ? OR user_b = ?", did, did).Scan(&n)
	return n, err
}

// CreateFriendship は行を作る。a, b は正規化済み（user_a < user_b）であること。
func CreateFriendship(ctx context.Context, q db.Querier, a, b, via string, now time.Time) error {
	_, err := db.Execute(ctx, q,
		"INSERT INTO friendships (user_a, user_b, created_at, via) VALUES (?, ?, ?, ?)",
		a, b, clock.UnixMilli(now), via)
	return err
}

// FriendRequest は friend_requests の行（04 §2.3、07 §2.2）。
type FriendRequest struct {
	ID        string
	FromDID   string
	ToDID     string
	Via       string
	ViaRef    string
	Status    string
	CreatedAt time.Time
	DecidedAt *time.Time
}

const friendRequestColumns = `id, from_did, to_did, via, via_ref, status, created_at, decided_at`

func scanFriendRequest(scan db.Scan, r *FriendRequest) error {
	var viaRef sql.NullString
	var created int64
	var decided sql.NullInt64
	if err := scan(&r.ID, &r.FromDID, &r.ToDID, &r.Via, &viaRef, &r.Status, &created, &decided); err != nil {
		return err
	}
	r.ViaRef = viaRef.String
	r.CreatedAt = clock.FromUnixMilli(created)
	if decided.Valid {
		t := clock.FromUnixMilli(decided.Int64)
		r.DecidedAt = &t
	}
	return nil
}

func CreateFriendRequest(ctx context.Context, q db.Querier, r FriendRequest) error {
	_, err := db.Execute(ctx, q, `
		INSERT INTO friend_requests (id, from_did, to_did, via, via_ref, status, created_at, decided_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		r.ID, r.FromDID, r.ToDID, r.Via, nullable(r.ViaRef), r.Status,
		clock.UnixMilli(r.CreatedAt), nullableTime(r.DecidedAt))
	return err
}

func GetFriendRequest(ctx context.Context, q db.Querier, id string) (*FriendRequest, error) {
	return db.GetItem(ctx, q,
		"SELECT "+friendRequestColumns+" FROM friend_requests WHERE id = ?",
		[]any{id}, scanFriendRequest)
}

// GetPendingFriendRequest は from → to の pending な申請を返す。なければ sql.ErrNoRows。
func GetPendingFriendRequest(ctx context.Context, q db.Querier, from, to string) (*FriendRequest, error) {
	return db.GetItem(ctx, q, `
		SELECT `+friendRequestColumns+` FROM friend_requests
		WHERE from_did = ? AND to_did = ? AND status = 'pending'`,
		[]any{from, to}, scanFriendRequest)
}

// ListPendingFriendRequests は自分が受けた（incoming）か送った（outgoing）pending な申請を新しい順に返す。
func ListPendingFriendRequests(ctx context.Context, q db.Querier, did string, incoming bool) ([]FriendRequest, error) {
	column := "from_did"
	if incoming {
		column = "to_did"
	}
	return db.GetItems(ctx, q, `
		SELECT `+friendRequestColumns+` FROM friend_requests
		WHERE `+column+` = ? AND status = 'pending'
		ORDER BY created_at DESC, id DESC`,
		[]any{did}, scanFriendRequest)
}

// DecideFriendRequest は pending の申請を status に遷移させる。既に pending でなければ false。
func DecideFriendRequest(ctx context.Context, q db.Querier, id, status string, now time.Time) (bool, error) {
	res, err := db.Execute(ctx, q,
		"UPDATE friend_requests SET status = ?, decided_at = ? WHERE id = ? AND status = 'pending'",
		status, clock.UnixMilli(now), id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// AcceptPendingFriendRequestsBetween は 2 人の間の pending な申請をすべて accepted にする。
// フレンドが成立したときに、反対向きの申請を残さないために使う。
func AcceptPendingFriendRequestsBetween(ctx context.Context, q db.Querier, a, b string, now time.Time) error {
	_, err := db.Execute(ctx, q, `
		UPDATE friend_requests SET status = 'accepted', decided_at = ?
		WHERE status = 'pending'
		  AND ((from_did = ? AND to_did = ?) OR (from_did = ? AND to_did = ?))`,
		clock.UnixMilli(now), a, b, b, a)
	return err
}

// HasRecentDecline は from が to に declined にされた申請が since 以降にあるかを返す（07 §2.2）。
func HasRecentDecline(ctx context.Context, q db.Querier, from, to string, since time.Time) (bool, error) {
	var ok bool
	err := q.QueryRowContext(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM friend_requests
			WHERE from_did = ? AND to_did = ? AND status = 'declined'
			  AND decided_at IS NOT NULL AND decided_at >= ?)`,
		from, to, clock.UnixMilli(since)).Scan(&ok)
	return ok, err
}

// HasRevealedEncounter は id の encounter が revealed で、ちょうど 2 人の組であるかを返す（07 §2.2）。
func HasRevealedEncounter(ctx context.Context, q db.Querier, id, a, b string) (bool, error) {
	if a > b {
		a, b = b, a
	}
	var ok bool
	err := q.QueryRowContext(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM encounters
			WHERE id = ? AND user_a = ? AND user_b = ? AND visibility = 'revealed')`,
		id, a, b).Scan(&ok)
	return ok, err
}

// SharedSpace は 2 人が同じ Space のメンバーだったか（削除済みを除く）を返す（07 §2.2）。
func SharedSpace(ctx context.Context, q db.Querier, spaceID, a, b string) (bool, error) {
	var ok bool
	err := q.QueryRowContext(ctx, `
		SELECT EXISTS(
			SELECT 1
			FROM spaces s
			JOIN space_members m1 ON m1.space_id = s.id AND m1.did = ?
			JOIN space_members m2 ON m2.space_id = s.id AND m2.did = ?
			WHERE s.id = ? AND s.state != 'deleted')`,
		a, b, spaceID).Scan(&ok)
	return ok, err
}

// FriendInvite は friend_invites の行（04 §2.3）。code_hash は SHA-256 の hex。
type FriendInvite struct {
	CodeHash  string
	DID       string
	CreatedAt time.Time
	ExpiresAt time.Time
	UsedBy    string
	UsedAt    *time.Time
}

const friendInviteColumns = `code_hash, did, created_at, expires_at, used_by, used_at`

func scanFriendInvite(scan db.Scan, i *FriendInvite) error {
	var created, expires int64
	var usedBy sql.NullString
	var usedAt sql.NullInt64
	if err := scan(&i.CodeHash, &i.DID, &created, &expires, &usedBy, &usedAt); err != nil {
		return err
	}
	i.CreatedAt = clock.FromUnixMilli(created)
	i.ExpiresAt = clock.FromUnixMilli(expires)
	i.UsedBy = usedBy.String
	if usedAt.Valid {
		t := clock.FromUnixMilli(usedAt.Int64)
		i.UsedAt = &t
	}
	return nil
}

// CreateFriendInvite は行を作る。code_hash が既にあれば false。
func CreateFriendInvite(ctx context.Context, q db.Querier, i FriendInvite) (bool, error) {
	res, err := db.Execute(ctx, q, `
		INSERT INTO friend_invites (code_hash, did, created_at, expires_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT (code_hash) DO NOTHING`,
		i.CodeHash, i.DID, clock.UnixMilli(i.CreatedAt), clock.UnixMilli(i.ExpiresAt))
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

func GetFriendInvite(ctx context.Context, q db.Querier, codeHash string) (*FriendInvite, error) {
	return db.GetItem(ctx, q,
		"SELECT "+friendInviteColumns+" FROM friend_invites WHERE code_hash = ?",
		[]any{codeHash}, scanFriendInvite)
}

// DeleteUnusedFriendInvites は did の未使用の招待を消す（新しいコードを作ると古いコードは失効する。07 §2.1）。
func DeleteUnusedFriendInvites(ctx context.Context, q db.Querier, did string) error {
	_, err := db.Execute(ctx, q,
		"DELETE FROM friend_invites WHERE did = ? AND used_by IS NULL", did)
	return err
}

// UseFriendInvite は未使用かつ期限内の招待を使用済みにする。条件を満たさなければ false。
func UseFriendInvite(ctx context.Context, q db.Querier, codeHash, usedBy string, now time.Time) (bool, error) {
	res, err := db.Execute(ctx, q, `
		UPDATE friend_invites SET used_by = ?, used_at = ?
		WHERE code_hash = ? AND used_by IS NULL AND expires_at > ?`,
		usedBy, clock.UnixMilli(now), codeHash, clock.UnixMilli(now))
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// DefaultLocationPrecision は新しくフレンドになった相手への既定の共有精度（07 §4.1）。
const DefaultLocationPrecision = "area"

// GetLocationPrecision は owner が viewer に設定している共有精度を返す。
// 行がなければ既定（area）を返す。位置共有そのものは P1（07 §4）だが、一覧の形は P0 から返す。
func GetLocationPrecision(ctx context.Context, q db.Querier, owner, viewer string) (string, error) {
	var precision string
	err := q.QueryRowContext(ctx,
		"SELECT precision FROM location_shares WHERE owner = ? AND viewer = ?",
		owner, viewer).Scan(&precision)
	if errors.Is(err, sql.ErrNoRows) {
		return DefaultLocationPrecision, nil
	}
	return precision, err
}
