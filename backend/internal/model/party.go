package model

import (
	"context"
	"database/sql"
	"time"

	"github.com/applemango/chain/internal/clock"
	"github.com/applemango/chain/internal/db"
)

// Party は parties の行（04 §2.4）。phase でアクセス判定が変わる（10 §5）。
type Party struct {
	SpaceID        string
	Phase          string
	Locked         bool
	TentativeAt    time.Time
	ConfirmedAt    *time.Time
	EndedAt        *time.Time
	LastActivityAt time.Time
}

func GetParty(ctx context.Context, q db.Querier, spaceID string) (*Party, error) {
	return db.GetItem(ctx, q, `
		SELECT space_id, phase, locked, tentative_at, confirmed_at, ended_at, last_activity_at
		FROM parties WHERE space_id = ?`,
		[]any{spaceID}, func(scan db.Scan, p *Party) error {
			var tentative, last int64
			var confirmed, ended sql.NullInt64
			if err := scan(&p.SpaceID, &p.Phase, &p.Locked, &tentative, &confirmed, &ended, &last); err != nil {
				return err
			}
			p.TentativeAt = clock.FromUnixMilli(tentative)
			p.LastActivityAt = clock.FromUnixMilli(last)
			if confirmed.Valid {
				t := clock.FromUnixMilli(confirmed.Int64)
				p.ConfirmedAt = &t
			}
			if ended.Valid {
				t := clock.FromUnixMilli(ended.Int64)
				p.EndedAt = &t
			}
			return nil
		})
}
