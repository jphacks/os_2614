package db

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "github.com/mattn/go-sqlite3"
)

// Querier は *sql.DB と *sql.Tx の両方を受けるための interface。
type Querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func Open(path string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	// PRAGMA は接続ごとの設定なので DSN で渡し、プールの全接続に効かせる。
	dsn := fmt.Sprintf("file:%s?_journal_mode=WAL&_foreign_keys=on&_busy_timeout=5000&_synchronous=NORMAL", path)
	conn, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, err
	}
	if err := conn.Ping(); err != nil {
		conn.Close()
		return nil, err
	}
	return conn, nil
}

func Execute(ctx context.Context, q Querier, query string, args ...any) (sql.Result, error) {
	return q.ExecContext(ctx, query, args...)
}

// Transaction は fn を 1 つのトランザクションで実行する。fn がエラーを返せばロールバックする。
func Transaction(ctx context.Context, conn *sql.DB, fn func(tx *sql.Tx) error) error {
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

type Scan = func(dest ...any) error
type scanHandler[T any] = func(Scan, *T) error

func GetItems[T any](ctx context.Context, q Querier, query string, args []any, scan scanHandler[T]) ([]T, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []T{}
	for rows.Next() {
		var item T
		if err := scan(rows.Scan, &item); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// GetItem は行がなければ sql.ErrNoRows をそのまま返す。
func GetItem[T any](ctx context.Context, q Querier, query string, args []any, scan scanHandler[T]) (*T, error) {
	var item T
	row := q.QueryRowContext(ctx, query, args...)
	if err := scan(row.Scan, &item); err != nil {
		return nil, err
	}
	return &item, nil
}
