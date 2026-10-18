package maintenance

import (
	"context"
	"time"

	"github.com/applemango/chain/internal/clock"
	"github.com/applemango/chain/internal/db"
	"github.com/applemango/chain/internal/model"
	"github.com/applemango/chain/internal/model/space"
)

// アカウント削除（13 §5）で使う SQL。
// 業務ルール（owner の引き継ぎ、Space の整理）は service が決め、ここは行の読み書きだけを持つ。

// --- 物理削除（13 §5 の 3、4、13 §4 のブロック） ---

func DeleteBeaconTokensByDID(ctx context.Context, q db.Querier, did string) (int64, error) {
	return model.Affected(db.Execute(ctx, q, "DELETE FROM beacon_tokens WHERE did = ?", did))
}

func DeleteEncountersByDID(ctx context.Context, q db.Querier, did string) (int64, error) {
	return model.Affected(db.Execute(ctx, q, "DELETE FROM encounters WHERE user_a = ? OR user_b = ?", did, did))
}

func DeleteFriendshipsByDID(ctx context.Context, q db.Querier, did string) (int64, error) {
	return model.Affected(db.Execute(ctx, q, "DELETE FROM friendships WHERE user_a = ? OR user_b = ?", did, did))
}

// DeleteFollowsByDID は did が書いた・向けられたフォローの索引と、受信箱の拒否を消す（PDS の record は消さない）。
func DeleteFollowsByDID(ctx context.Context, q db.Querier, did string) (int64, error) {
	if _, err := db.Execute(ctx, q, "DELETE FROM follow_dismissals WHERE did = ? OR follower_did = ?", did, did); err != nil {
		return 0, err
	}
	return model.Affected(db.Execute(ctx, q, "DELETE FROM follows WHERE author_did = ? OR subject_did = ?", did, did))
}

func DeleteFriendInvitesByDID(ctx context.Context, q db.Querier, did string) (int64, error) {
	return model.Affected(db.Execute(ctx, q, "DELETE FROM friend_invites WHERE did = ?", did))
}

func DeleteLocationSharesByDID(ctx context.Context, q db.Querier, did string) (int64, error) {
	return model.Affected(db.Execute(ctx, q, "DELETE FROM location_shares WHERE owner = ? OR viewer = ?", did, did))
}

func DeleteAreasByDID(ctx context.Context, q db.Querier, did string) (int64, error) {
	return model.Affected(db.Execute(ctx, q, "DELETE FROM areas WHERE did = ?", did))
}

func DeleteDevicesByDID(ctx context.Context, q db.Querier, did string) (int64, error) {
	return model.Affected(db.Execute(ctx, q, "DELETE FROM devices WHERE did = ?", did))
}

// DeleteNotificationsByDID は通知を物理削除する。notification_outbox は FK の CASCADE で消える。
func DeleteNotificationsByDID(ctx context.Context, q db.Querier, did string) (int64, error) {
	return model.Affected(db.Execute(ctx, q, "DELETE FROM notifications WHERE did = ?", did))
}

func DeleteBlocksByDID(ctx context.Context, q db.Querier, did string) (int64, error) {
	return model.Affected(db.Execute(ctx, q, "DELETE FROM blocks WHERE blocker = ? OR blocked = ?", did, did))
}

func DeleteUserSettings(ctx context.Context, q db.Querier, did string) (int64, error) {
	return model.Affected(db.Execute(ctx, q, "DELETE FROM user_settings WHERE did = ?", did))
}

// DeleteRecordsByAuthor は自分が投稿した record を物理削除する（論理削除を経ない。13 §5 の 4）。
func DeleteRecordsByAuthor(ctx context.Context, q db.Querier, did string) (int64, error) {
	return model.Affected(db.Execute(ctx, q, "DELETE FROM records WHERE author_did = ?", did))
}

// DeletePublicPostsByAuthor は P1 の公開投稿も残さない（13 §5 の 4 と同じ扱い）。
func DeletePublicPostsByAuthor(ctx context.Context, q db.Querier, did string) (int64, error) {
	return model.Affected(db.Execute(ctx, q, "DELETE FROM public_posts WHERE author_did = ?", did))
}

func DeleteGroupInvitesByDID(ctx context.Context, q db.Querier, did string) (int64, error) {
	return model.Affected(db.Execute(ctx, q,
		"DELETE FROM group_invites WHERE target_did = ? OR created_by = ?", did, did))
}

func DeleteGroupInviteApprovalsByDID(ctx context.Context, q db.Querier, did string) (int64, error) {
	return model.Affected(db.Execute(ctx, q, "DELETE FROM group_invite_approvals WHERE did = ?", did))
}

func DeleteGroupVoteBallotsByDID(ctx context.Context, q db.Querier, did string) (int64, error) {
	return model.Affected(db.Execute(ctx, q, "DELETE FROM group_vote_ballots WHERE did = ?", did))
}

// DeleteGroupVotesByCreator は group_votes.created_by が users を参照していて CASCADE しないため使う。
func DeleteGroupVotesByCreator(ctx context.Context, q db.Querier, did string) (int64, error) {
	return model.Affected(db.Execute(ctx, q, "DELETE FROM group_votes WHERE created_by = ?", did))
}

func DeleteSpaceMembersByDID(ctx context.Context, q db.Querier, did string) (int64, error) {
	return model.Affected(db.Execute(ctx, q, "DELETE FROM space_members WHERE did = ?", did))
}

// --- media（行とファイル。13 §5 の 4） ---

func ListMediaByOwner(ctx context.Context, q db.Querier, did string) ([]space.Media, error) {
	return db.GetItems(ctx, q,
		"SELECT "+space.MediaColumns+" FROM media WHERE owner_did = ?", []any{did}, space.ScanMedia)
}

// ListMediaInSpaces は指定した Space の media を返す。Space を消す前にファイルの場所を控えるために使う。
func ListMediaInSpaces(ctx context.Context, q db.Querier, spaceIDs []string) ([]space.Media, error) {
	if len(spaceIDs) == 0 {
		return nil, nil
	}
	args := make([]any, 0, len(spaceIDs))
	for _, id := range spaceIDs {
		args = append(args, id)
	}
	return db.GetItems(ctx, q,
		"SELECT "+space.MediaColumns+" FROM media WHERE space_id IN ("+model.Placeholders(len(spaceIDs))+")",
		args, space.ScanMedia)
}

func DeleteMediaByOwner(ctx context.Context, q db.Querier, did string) (int64, error) {
	return model.Affected(db.Execute(ctx, q, "DELETE FROM media WHERE owner_did = ?", did))
}

// --- セッション（03 §3、13 §5 の 2） ---

func ListAppSessionIDsByDID(ctx context.Context, q db.Querier, did string) ([]string, error) {
	return db.GetItems(ctx, q, "SELECT id FROM app_sessions WHERE did = ?", []any{did},
		func(scan db.Scan, id *string) error { return scan(id) })
}

func RevokeAllAppSessionsByDID(ctx context.Context, q db.Querier, did string, now time.Time) (int64, error) {
	return model.Affected(db.Execute(ctx, q,
		"UPDATE app_sessions SET revoked_at = ? WHERE did = ? AND revoked_at IS NULL",
		clock.UnixMilli(now), did))
}

func ListATProtoSessionIDsByDID(ctx context.Context, q db.Querier, did string) ([]string, error) {
	return db.GetItems(ctx, q, "SELECT id FROM atproto_oauth_sessions WHERE did = ?", []any{did},
		func(scan db.Scan, id *string) error { return scan(id) })
}

// RevokeAllATProtoSessionsByDID はトークンを消し、revoked_at を立てる（03 §3）。
func RevokeAllATProtoSessionsByDID(ctx context.Context, q db.Querier, did string, now time.Time) (int64, error) {
	return model.Affected(db.Execute(ctx, q, `
		UPDATE atproto_oauth_sessions SET data_enc = x'', revoked_at = ?, updated_at = ?
		WHERE did = ? AND revoked_at IS NULL`,
		clock.UnixMilli(now), clock.UnixMilli(now), did))
}

// --- users.deleted_at と audit_log（13 §5 の 5・6） ---

func MarkUserDeleted(ctx context.Context, q db.Querier, did string, now time.Time) (int64, error) {
	return model.Affected(db.Execute(ctx, q,
		"UPDATE users SET deleted_at = ? WHERE did = ? AND deleted_at IS NULL", clock.UnixMilli(now), did))
}

// NullAuditActor は自分の操作の actor_did を NULL にする（04 §2.5）。
func NullAuditActor(ctx context.Context, q db.Querier, did string) (int64, error) {
	return model.Affected(db.Execute(ctx, q, "UPDATE audit_log SET actor_did = NULL WHERE actor_did = ?", did))
}

// --- Space の整理（13 §5 の 4、09 §5） ---

// ListMembershipsByDID は自分が行を持つ space_members をすべて返す（退出済みも含む）。
func ListMembershipsByDID(ctx context.Context, q db.Querier, did string) ([]space.SpaceMember, error) {
	return db.GetItems(ctx, q,
		"SELECT "+space.SpaceMemberColumns+" FROM space_members WHERE did = ?", []any{did}, space.ScanSpaceMember)
}

// ListSpacesCreatedBy は created_by が自分な Space を返す。spaces.created_by は CASCADE しないため、
// users 行を消す前に始末しておく必要がある。
func ListSpacesCreatedBy(ctx context.Context, q db.Querier, did string) ([]space.Space, error) {
	return db.GetItems(ctx, q,
		"SELECT "+space.SpaceColumns+" FROM spaces WHERE created_by = ?", []any{did}, space.ScanSpace)
}

func SetSpaceCreatedBy(ctx context.Context, q db.Querier, spaceID, did string) error {
	_, err := db.Execute(ctx, q, "UPDATE spaces SET created_by = ? WHERE id = ?", did, spaceID)
	return err
}

// DeleteSpacesByIDs は Space をまとめて物理削除する。records / media / members は FK の CASCADE で消える。
func DeleteSpacesByIDs(ctx context.Context, q db.Querier, ids []string) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	args := make([]any, 0, len(ids))
	for _, id := range ids {
		args = append(args, id)
	}
	return model.Affected(db.Execute(ctx, q, "DELETE FROM spaces WHERE id IN ("+model.Placeholders(len(ids))+")", args...))
}
