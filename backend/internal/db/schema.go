package db

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"os"
)

// migration は使わず、変更のたびに作り直す（specification/backend/04-data-model.md §5）。

//go:embed schema.sql
var schemaSQL string

//go:embed seed.sql
var seedSQL string

// EnsureSchema は、スキーマがまだなければ作る。既存の DB には触らない。
func EnsureSchema(ctx context.Context, conn *sql.DB) error {
	var n int
	err := conn.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'users'").Scan(&n)
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	return ApplySchema(ctx, conn)
}

func ApplySchema(ctx context.Context, conn *sql.DB) error {
	_, err := conn.ExecContext(ctx, schemaSQL)
	return err
}

func ApplySeed(ctx context.Context, conn *sql.DB) error {
	_, err := conn.ExecContext(ctx, seedSQL)
	return err
}

// RemoveFiles は DB ファイルと WAL のファイルを消す。
func RemoveFiles(path string) error {
	for _, p := range []string{path, path + "-wal", path + "-shm"} {
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}
