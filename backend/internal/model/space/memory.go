package space

import (
	"context"
	"database/sql"
	"time"

	"github.com/applemango/chain/internal/clock"
	"github.com/applemango/chain/internal/db"
	"github.com/applemango/chain/internal/model"
)

// MemoryItem は GET /memories の 1 件（13 §2）。archived の Party とグループの両方を含む。
type MemoryItem struct {
	SpaceID      string
	Type         string
	Name         string
	From         time.Time
	Until        time.Time
	MemberCount  int
	MediaCount   int
	CoverMediaID *string
}

// memoryUntilSQL は一覧の並びとカーソルに使う「終わりの時刻」。Party は ended_at（無ければ
// archived_at）、グループは archived_at（13 §2）。
const memoryUntilSQL = `CASE WHEN s.type = '` + model.SpaceTypeParty + `'
	THEN COALESCE(p.ended_at, s.archived_at, p.tentative_at)
	ELSE COALESCE(s.archived_at, s.created_at) END`

// memoryFromSQL は「始まりの時刻」。Party は tentative_at、グループは created_at（13 §2）。
const memoryFromSQL = `CASE WHEN s.type = '` + model.SpaceTypeParty + `'
	THEN p.tentative_at ELSE s.created_at END`

// ListMemories は archived の Space（Party / グループ）のうち、did が hidden_from_memory = 0 のものを
// until の新しい順で返す。退出済みのメンバーも含む。before が非ゼロなら、その (until, id) より前を
// 返す（13 §2、02 §4）。
func ListMemories(ctx context.Context, q db.Querier, did string, beforeAt int64, beforeID string, limit int) ([]MemoryItem, error) {
	query := `
		SELECT s.id, s.type, s.name, ` + memoryFromSQL + `, ` + memoryUntilSQL + `,
		  (SELECT count(*) FROM space_members m2 WHERE m2.space_id = s.id),
		  (SELECT count(*) FROM media md WHERE md.space_id = s.id AND md.deleted_at IS NULL),
		  (SELECT md2.id FROM media md2 WHERE md2.space_id = s.id AND md2.deleted_at IS NULL
		     ORDER BY md2.created_at, md2.id LIMIT 1)
		FROM spaces s
		LEFT JOIN parties p ON p.space_id = s.id
		JOIN space_members m ON m.space_id = s.id AND m.did = ? AND m.hidden_from_memory = 0
		WHERE s.state = 'archived' AND s.type IN ('` + model.SpaceTypeParty + `', '` + model.SpaceTypeGroup + `')`
	args := []any{did}
	if beforeAt > 0 {
		query += " AND (" + memoryUntilSQL + " < ? OR (" + memoryUntilSQL + " = ? AND s.id < ?))"
		args = append(args, beforeAt, beforeAt, beforeID)
	}
	query += " ORDER BY " + memoryUntilSQL + " DESC, s.id DESC LIMIT ?"
	args = append(args, limit)

	return db.GetItems(ctx, q, query, args, func(scan db.Scan, m *MemoryItem) error {
		var from, until int64
		var cover sql.NullString
		if err := scan(&m.SpaceID, &m.Type, &m.Name, &from, &until,
			&m.MemberCount, &m.MediaCount, &cover); err != nil {
			return err
		}
		m.From = clock.FromUnixMilli(from)
		m.Until = clock.FromUnixMilli(until)
		if cover.Valid {
			v := cover.String
			m.CoverMediaID = &v
		}
		return nil
	})
}
