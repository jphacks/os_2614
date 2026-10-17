package model

import (
	"context"

	"github.com/applemango/chain/internal/db"
)

// SetUserHandle は Jetstream の identity イベントで handle を更新する（03 §5）。
// deleted_at は変えない（削除済みの DID のイベントは呼び出し側で捨てる）。
func SetUserHandle(ctx context.Context, q db.Querier, did, handle string) error {
	_, err := db.Execute(ctx, q, "UPDATE users SET handle = ? WHERE did = ?", handle, did)
	return err
}
