package model

import (
	"context"
	"time"

	"github.com/applemango/chain/internal/clock"
	"github.com/applemango/chain/internal/db"
)

// AuditEntry は audit_log の行（02 §7）。detail_json には座標・token を入れない。
type AuditEntry struct {
	ID         string
	ActorDID   string
	Action     string
	Target     string
	DetailJSON string
	CreatedAt  time.Time
}

func InsertAuditLog(ctx context.Context, q db.Querier, e AuditEntry) error {
	_, err := db.Execute(ctx, q, `
		INSERT INTO audit_log (id, actor_did, action, target, detail_json, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		e.ID, Nullable(e.ActorDID), e.Action, Nullable(e.Target), Nullable(e.DetailJSON), clock.UnixMilli(e.CreatedAt))
	return err
}
