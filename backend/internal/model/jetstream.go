package model

import (
	"context"
	"database/sql"
	"errors"

	"github.com/applemango/chain/internal/db"
)

// jetstream_cursor は 1 行だけの表（04 §2.5）。Jetstream の取り込み位置をマイクロ秒で持つ（03 §5）。

// GetJetstreamCursor は保存済みの cursor_us を返す。まだ無ければ 0。
func GetJetstreamCursor(ctx context.Context, q db.Querier) (int64, error) {
	var us int64
	err := q.QueryRowContext(ctx, "SELECT cursor_us FROM jetstream_cursor WHERE id = 1").Scan(&us)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return us, nil
}

// SetJetstreamCursor は cursor_us を保存する。行が無ければ作る。
func SetJetstreamCursor(ctx context.Context, q db.Querier, cursorUS int64) error {
	_, err := db.Execute(ctx, q, `
		INSERT INTO jetstream_cursor (id, cursor_us) VALUES (1, ?)
		ON CONFLICT (id) DO UPDATE SET cursor_us = excluded.cursor_us`, cursorUS)
	return err
}
