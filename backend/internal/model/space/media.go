package space

import (
	"context"
	"database/sql"
	"time"

	"github.com/applemango/chain/internal/clock"
	"github.com/applemango/chain/internal/db"
	"github.com/applemango/chain/internal/model"
)

// Media は media の行（04 §2.4）。保存は再エンコード後の JPEG で、sha256 はそのファイルのもの。
type Media struct {
	ID        string
	OwnerDID  string
	SpaceID   *string
	Mime      string
	Width     int
	Height    int
	Bytes     int
	SHA256    string
	TakenAt   *time.Time
	CreatedAt time.Time
	DeletedAt *time.Time
}

const MediaColumns = `id, owner_did, space_id, mime, width, height, bytes, sha256, taken_at, created_at, deleted_at`

func ScanMedia(scan db.Scan, m *Media) error {
	var spaceStr sql.NullString
	var taken, deletedAt sql.NullInt64
	var created int64
	if err := scan(&m.ID, &m.OwnerDID, &spaceStr, &m.Mime, &m.Width, &m.Height, &m.Bytes,
		&m.SHA256, &taken, &created, &deletedAt); err != nil {
		return err
	}
	if spaceStr.Valid {
		v := spaceStr.String
		m.SpaceID = &v
	}
	if taken.Valid {
		t := clock.FromUnixMilli(taken.Int64)
		m.TakenAt = &t
	}
	m.CreatedAt = clock.FromUnixMilli(created)
	if deletedAt.Valid {
		t := clock.FromUnixMilli(deletedAt.Int64)
		m.DeletedAt = &t
	}
	return nil
}

func GetMedia(ctx context.Context, q db.Querier, id string) (*Media, error) {
	return db.GetItem(ctx, q, "SELECT "+MediaColumns+" FROM media WHERE id = ?", []any{id}, ScanMedia)
}

func InsertMedia(ctx context.Context, q db.Querier, m Media) error {
	_, err := db.Execute(ctx, q, `
		INSERT INTO media (id, owner_did, space_id, mime, width, height, bytes, sha256, taken_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		m.ID, m.OwnerDID, model.NullableString(m.SpaceID), m.Mime, m.Width, m.Height, m.Bytes, m.SHA256,
		model.NullableTime(m.TakenAt), clock.UnixMilli(m.CreatedAt))
	return err
}

// AttachMedia は未添付のメディアを 1 つの Space に添付する。所有者と未添付を条件にする。
func AttachMedia(ctx context.Context, q db.Querier, id, ownerDID, spaceID string) (bool, error) {
	res, err := db.Execute(ctx, q, `
		UPDATE media SET space_id = ?
		WHERE id = ? AND owner_did = ? AND space_id IS NULL AND deleted_at IS NULL`,
		spaceID, id, ownerDID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}
