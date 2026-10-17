package model

import (
	"context"
	"time"

	"github.com/applemango/chain/internal/clock"
	"github.com/applemango/chain/internal/db"
)

// GroupInvite は group_invites の行（04 §2.4、09 §6）。
// collecting は承認を集めている状態、offered は必要数に達して対象者に提示された状態。
type GroupInvite struct {
	ID        string
	SpaceID   string
	TargetDID string
	CreatedBy string
	Status    string
	CreatedAt time.Time
	ExpiresAt time.Time
}

const groupInviteColumns = `id, space_id, target_did, created_by, status, created_at, expires_at`

func scanGroupInvite(scan db.Scan, i *GroupInvite) error {
	var created, expires int64
	if err := scan(&i.ID, &i.SpaceID, &i.TargetDID, &i.CreatedBy, &i.Status, &created, &expires); err != nil {
		return err
	}
	i.CreatedAt = clock.FromUnixMilli(created)
	i.ExpiresAt = clock.FromUnixMilli(expires)
	return nil
}

func InsertGroupInvite(ctx context.Context, q db.Querier, i GroupInvite) error {
	_, err := db.Execute(ctx, q, `
		INSERT INTO group_invites (id, space_id, target_did, created_by, status, created_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		i.ID, i.SpaceID, i.TargetDID, i.CreatedBy, i.Status,
		clock.UnixMilli(i.CreatedAt), clock.UnixMilli(i.ExpiresAt))
	return err
}

func GetGroupInvite(ctx context.Context, q db.Querier, id string) (*GroupInvite, error) {
	return db.GetItem(ctx, q,
		"SELECT "+groupInviteColumns+" FROM group_invites WHERE id = ?",
		[]any{id}, scanGroupInvite)
}

// GetOpenGroupInvite は collecting か offered の招待を返す（09 §6 の重複判定）。なければ sql.ErrNoRows。
func GetOpenGroupInvite(ctx context.Context, q db.Querier, spaceID, targetDID string) (*GroupInvite, error) {
	return db.GetItem(ctx, q, `
		SELECT `+groupInviteColumns+` FROM group_invites
		WHERE space_id = ? AND target_did = ? AND status IN ('collecting','offered')`,
		[]any{spaceID, targetDID}, scanGroupInvite)
}

// ListCollectingGroupInvites は collecting の招待を古い順に返す（09 §6 の一覧）。
func ListCollectingGroupInvites(ctx context.Context, q db.Querier, spaceID string) ([]GroupInvite, error) {
	return db.GetItems(ctx, q, `
		SELECT `+groupInviteColumns+` FROM group_invites
		WHERE space_id = ? AND status = 'collecting'
		ORDER BY created_at, id`,
		[]any{spaceID}, scanGroupInvite)
}

// ListOfferedGroupInvites は target 宛ての offered の招待を新しい順に返す（09 §6 の自分宛ての招待）。
func ListOfferedGroupInvites(ctx context.Context, q db.Querier, targetDID string) ([]GroupInvite, error) {
	return db.GetItems(ctx, q, `
		SELECT `+groupInviteColumns+` FROM group_invites
		WHERE target_did = ? AND status = 'offered'
		ORDER BY created_at DESC, id DESC`,
		[]any{targetDID}, scanGroupInvite)
}

// SetGroupInviteStatus は status を from から to へ変える。現在が from でなければ変えず false を返す。
func SetGroupInviteStatus(ctx context.Context, q db.Querier, id, from, to string) (bool, error) {
	res, err := db.Execute(ctx, q,
		"UPDATE group_invites SET status = ? WHERE id = ? AND status = ?", to, id, from)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// DeleteGroupInvite は招待と、CASCADE で承認の行を消す（09 §6 の取り消し）。
func DeleteGroupInvite(ctx context.Context, q db.Querier, id string) error {
	_, err := db.Execute(ctx, q, "DELETE FROM group_invites WHERE id = ?", id)
	return err
}

// MarkGroupInviteExpired は未処理の招待を期限切れにする（09 §6、04 §4）。
func MarkGroupInviteExpired(ctx context.Context, q db.Querier, id string) error {
	_, err := db.Execute(ctx, q,
		"UPDATE group_invites SET status = 'expired' WHERE id = ? AND status IN ('collecting','offered')", id)
	return err
}

// CreateGroupInviteApproval は承認の行を作る。既にあれば false（09 §6 の二重承認）。
func CreateGroupInviteApproval(ctx context.Context, q db.Querier, inviteID, did string, now time.Time) (bool, error) {
	res, err := db.Execute(ctx, q, `
		INSERT INTO group_invite_approvals (invite_id, did, created_at) VALUES (?, ?, ?)
		ON CONFLICT DO NOTHING`,
		inviteID, did, clock.UnixMilli(now))
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// CountGroupInviteApprovals は招待の承認数を返す。退出した承認者も 1 として数え続ける（09 §6）。
func CountGroupInviteApprovals(ctx context.Context, q db.Querier, inviteID string) (int, error) {
	var n int
	err := q.QueryRowContext(ctx,
		"SELECT count(*) FROM group_invite_approvals WHERE invite_id = ?", inviteID).Scan(&n)
	return n, err
}

// HasRevealedEncounterBetween は a と b の間に revealed な encounter があるかを返す（09 §6 の招待の対象条件）。
func HasRevealedEncounterBetween(ctx context.Context, q db.Querier, a, b string) (bool, error) {
	if a > b {
		a, b = b, a
	}
	var ok bool
	err := q.QueryRowContext(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM encounters WHERE user_a = ? AND user_b = ? AND visibility = 'revealed')`,
		a, b).Scan(&ok)
	return ok, err
}
