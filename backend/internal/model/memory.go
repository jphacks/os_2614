package model

import (
	"context"
	"database/sql"
	"time"

	"github.com/applemango/chain/internal/clock"
	"github.com/applemango/chain/internal/db"
)

// MemoryItem は GET /memories の 1 件（13 §2）。P0 は archived の Party だけを返す。
type MemoryItem struct {
	SpaceID      string
	Type         string
	Name         string
	From         time.Time
	Until        time.Time
	ArchivedAt   time.Time
	MemberCount  int
	MediaCount   int
	CoverMediaID *string
}

// ListMemoryParties は archived の Party のうち、did が hidden_from_memory = 0 のものを
// archived_at の新しい順で返す。before が非ゼロなら、その (archived_at, id) より前を返す（13 §2、02 §4）。
func ListMemoryParties(ctx context.Context, q db.Querier, did string, beforeAt int64, beforeID string, limit int) ([]MemoryItem, error) {
	query := `
		SELECT s.id, s.type, s.name, p.tentative_at, COALESCE(p.ended_at, s.archived_at, p.tentative_at), s.archived_at,
		  (SELECT count(*) FROM space_members m2 WHERE m2.space_id = s.id),
		  (SELECT count(*) FROM media md WHERE md.space_id = s.id AND md.deleted_at IS NULL),
		  (SELECT md2.id FROM media md2 WHERE md2.space_id = s.id AND md2.deleted_at IS NULL
		     ORDER BY md2.created_at, md2.id LIMIT 1)
		FROM spaces s
		JOIN parties p ON p.space_id = s.id
		JOIN space_members m ON m.space_id = s.id AND m.did = ? AND m.hidden_from_memory = 0
		WHERE s.type = 'jp.i32.chain.space.party' AND s.state = 'archived'`
	args := []any{did}
	if beforeAt > 0 {
		query += " AND (s.archived_at < ? OR (s.archived_at = ? AND s.id < ?))"
		args = append(args, beforeAt, beforeAt, beforeID)
	}
	query += " ORDER BY s.archived_at DESC, s.id DESC LIMIT ?"
	args = append(args, limit)

	return db.GetItems(ctx, q, query, args, func(scan db.Scan, m *MemoryItem) error {
		var from, until, archived int64
		var cover sql.NullString
		if err := scan(&m.SpaceID, &m.Type, &m.Name, &from, &until, &archived,
			&m.MemberCount, &m.MediaCount, &cover); err != nil {
			return err
		}
		m.From = clock.FromUnixMilli(from)
		m.Until = clock.FromUnixMilli(until)
		m.ArchivedAt = clock.FromUnixMilli(archived)
		if cover.Valid {
			v := cover.String
			m.CoverMediaID = &v
		}
		return nil
	})
}
