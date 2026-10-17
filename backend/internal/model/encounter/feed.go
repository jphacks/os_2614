package encounter

import (
	"context"
	"time"

	"github.com/applemango/chain/internal/clock"
	"github.com/applemango/chain/internal/db"
)

// ListRevealedEncounterPeers は did と revealed な encounter があり、established_at が since 以降の
// 相手の DID を返す（17 §3.1 の home。「直近 30 日に revealed な encounter がある相手」）。
func ListRevealedEncounterPeers(ctx context.Context, q db.Querier, did string, since time.Time) ([]string, error) {
	return db.GetItems(ctx, q, `
		SELECT CASE WHEN user_a = ? THEN user_b ELSE user_a END
		FROM encounters
		WHERE (user_a = ? OR user_b = ?) AND visibility = 'revealed' AND established_at >= ?`,
		[]any{did, did, did, clock.UnixMilli(since)},
		func(scan db.Scan, out *string) error { return scan(out) })
}
