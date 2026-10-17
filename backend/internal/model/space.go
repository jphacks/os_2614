package model

import (
	"context"
	"database/sql"
	"time"

	"github.com/applemango/chain/internal/clock"
	"github.com/applemango/chain/internal/db"
)

// Space は spaces の行（04 §2.4）。グループと Party の共通部分（03 §7.1）。
type Space struct {
	ID          string
	Type        string
	Authority   string
	Ref         string
	ReadPolicy  string
	WritePolicy string
	Name        string
	State       string
	CreatedBy   string
	CreatedAt   time.Time
	ArchivedAt  *time.Time
}

const spaceColumns = `id, type, authority, ref, read_policy, write_policy, name, state, created_by, created_at, archived_at`

func scanSpace(scan db.Scan, s *Space) error {
	var archived sql.NullInt64
	var created int64
	if err := scan(&s.ID, &s.Type, &s.Authority, &s.Ref, &s.ReadPolicy, &s.WritePolicy,
		&s.Name, &s.State, &s.CreatedBy, &created, &archived); err != nil {
		return err
	}
	s.CreatedAt = clock.FromUnixMilli(created)
	if archived.Valid {
		t := clock.FromUnixMilli(archived.Int64)
		s.ArchivedAt = &t
	}
	return nil
}

func GetSpace(ctx context.Context, q db.Querier, id string) (*Space, error) {
	return db.GetItem(ctx, q, "SELECT "+spaceColumns+" FROM spaces WHERE id = ?", []any{id}, scanSpace)
}

func InsertSpace(ctx context.Context, q db.Querier, s Space) error {
	_, err := db.Execute(ctx, q, `
		INSERT INTO spaces (id, type, authority, ref, read_policy, write_policy, name, state, created_by, created_at, archived_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		s.ID, s.Type, s.Authority, s.Ref, s.ReadPolicy, s.WritePolicy, s.Name, s.State,
		s.CreatedBy, clock.UnixMilli(s.CreatedAt), nullableTime(s.ArchivedAt))
	return err
}

func UpdateSpaceName(ctx context.Context, q db.Querier, id, name string) error {
	_, err := db.Execute(ctx, q, "UPDATE spaces SET name = ? WHERE id = ?", name, id)
	return err
}

// SetSpaceState は state と archived_at を更新する。archived にするときに使う（09 §5）。
func SetSpaceState(ctx context.Context, q db.Querier, id, state string, archivedAt *time.Time) error {
	_, err := db.Execute(ctx, q, "UPDATE spaces SET state = ?, archived_at = ? WHERE id = ?",
		state, nullableTime(archivedAt), id)
	return err
}

// SpaceMember は space_members の行（04 §2.4）。Spaces の read / write の真偽値を持つ。
type SpaceMember struct {
	SpaceID          string
	DID              string
	CanRead          bool
	CanWrite         bool
	Role             string
	JoinedAt         time.Time
	LeftAt           *time.Time
	HiddenFromMemory bool
}

const spaceMemberColumns = `space_id, did, can_read, can_write, role, joined_at, left_at, hidden_from_memory`

func scanSpaceMember(scan db.Scan, m *SpaceMember) error {
	var joined int64
	var left sql.NullInt64
	if err := scan(&m.SpaceID, &m.DID, &m.CanRead, &m.CanWrite, &m.Role, &joined, &left, &m.HiddenFromMemory); err != nil {
		return err
	}
	m.JoinedAt = clock.FromUnixMilli(joined)
	if left.Valid {
		t := clock.FromUnixMilli(left.Int64)
		m.LeftAt = &t
	}
	return nil
}

func GetSpaceMember(ctx context.Context, q db.Querier, spaceID, did string) (*SpaceMember, error) {
	return db.GetItem(ctx, q,
		"SELECT "+spaceMemberColumns+" FROM space_members WHERE space_id = ? AND did = ?",
		[]any{spaceID, did}, scanSpaceMember)
}

func ListSpaceMembers(ctx context.Context, q db.Querier, spaceID string) ([]SpaceMember, error) {
	return db.GetItems(ctx, q,
		"SELECT "+spaceMemberColumns+" FROM space_members WHERE space_id = ? ORDER BY joined_at",
		[]any{spaceID}, scanSpaceMember)
}

func InsertSpaceMember(ctx context.Context, q db.Querier, m SpaceMember) error {
	_, err := db.Execute(ctx, q, `
		INSERT INTO space_members (space_id, did, can_read, can_write, role, joined_at, left_at, hidden_from_memory)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		m.SpaceID, m.DID, m.CanRead, m.CanWrite, m.Role,
		clock.UnixMilli(m.JoinedAt), nullableTime(m.LeftAt), m.HiddenFromMemory)
	return err
}

// ListActiveSpaceMembers は left_at IS NULL のメンバーを joined_at の古い順で返す。
func ListActiveSpaceMembers(ctx context.Context, q db.Querier, spaceID string) ([]SpaceMember, error) {
	return db.GetItems(ctx, q,
		"SELECT "+spaceMemberColumns+" FROM space_members WHERE space_id = ? AND left_at IS NULL ORDER BY joined_at",
		[]any{spaceID}, scanSpaceMember)
}

func CountActiveMembers(ctx context.Context, q db.Querier, spaceID string) (int, error) {
	var n int
	if err := q.QueryRowContext(ctx,
		"SELECT count(*) FROM space_members WHERE space_id = ? AND left_at IS NULL", spaceID).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

// RejoinSpaceMember は退出済みのメンバーを再加入させる（09 §5）。left_at を消し、joined_at を入れ直す。
func RejoinSpaceMember(ctx context.Context, q db.Querier, spaceID, did string, joinedAt time.Time) error {
	_, err := db.Execute(ctx, q, `
		UPDATE space_members SET left_at = NULL, joined_at = ?, can_read = 1, can_write = 1, role = 'member'
		WHERE space_id = ? AND did = ?`,
		clock.UnixMilli(joinedAt), spaceID, did)
	return err
}

func LeaveSpaceMember(ctx context.Context, q db.Querier, spaceID, did string, leftAt time.Time) error {
	_, err := db.Execute(ctx, q, "UPDATE space_members SET left_at = ? WHERE space_id = ? AND did = ?",
		clock.UnixMilli(leftAt), spaceID, did)
	return err
}

// HideSpaceMemberFromMemory は自分の memory から Space を隠す（hidden_from_memory = 1、13 §2）。
func HideSpaceMemberFromMemory(ctx context.Context, q db.Querier, spaceID, did string) error {
	_, err := db.Execute(ctx, q,
		"UPDATE space_members SET hidden_from_memory = 1 WHERE space_id = ? AND did = ?", spaceID, did)
	return err
}

func SetSpaceMemberRole(ctx context.Context, q db.Querier, spaceID, did, role string) error {
	_, err := db.Execute(ctx, q, "UPDATE space_members SET role = ? WHERE space_id = ? AND did = ?",
		role, spaceID, did)
	return err
}

// DeleteSpace は Space の行を物理削除する。members / parties / records / media は FK の CASCADE で消える（08 §4.2）。
func DeleteSpace(ctx context.Context, q db.Querier, id string) error {
	_, err := db.Execute(ctx, q, "DELETE FROM spaces WHERE id = ?", id)
	return err
}

// SpaceListItem は GET /instances の要素（13 §2）。最終投稿の新しい順に並べる。
type SpaceListItem struct {
	Space       Space
	LastPostAt  *time.Time
	MemberCount int
}

// ListUserSpaces は自分がメンバーである Space を、最終投稿の新しい順で返す。
// archived も含み、deleted は除く。投稿がなければ作成時刻で並べる。
func ListUserSpaces(ctx context.Context, q db.Querier, did string) ([]SpaceListItem, error) {
	return db.GetItems(ctx, q, `
		SELECT `+spaceColumns+`,
		  (SELECT MAX(r.created_at) FROM records r WHERE r.space_id = s.id AND r.deleted_at IS NULL),
		  (SELECT count(*) FROM space_members m2 WHERE m2.space_id = s.id AND m2.left_at IS NULL)
		FROM spaces s
		JOIN space_members m ON m.space_id = s.id AND m.did = ?
		WHERE s.state != 'deleted'
		ORDER BY COALESCE((SELECT MAX(r.created_at) FROM records r WHERE r.space_id = s.id AND r.deleted_at IS NULL), s.created_at) DESC, s.id DESC`,
		[]any{did}, func(scan db.Scan, it *SpaceListItem) error {
			var last sql.NullInt64
			var created, archived sql.NullInt64
			if err := scan(&it.Space.ID, &it.Space.Type, &it.Space.Authority, &it.Space.Ref,
				&it.Space.ReadPolicy, &it.Space.WritePolicy, &it.Space.Name, &it.Space.State,
				&it.Space.CreatedBy, &created, &archived, &last, &it.MemberCount); err != nil {
				return err
			}
			it.Space.CreatedAt = clock.FromUnixMilli(created.Int64)
			if archived.Valid {
				t := clock.FromUnixMilli(archived.Int64)
				it.Space.ArchivedAt = &t
			}
			if last.Valid {
				t := clock.FromUnixMilli(last.Int64)
				it.LastPostAt = &t
			}
			return nil
		})
}
