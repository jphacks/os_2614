package db

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
)

func openTest(t *testing.T, seed bool) *sql.DB {
	t.Helper()
	conn, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	ctx := context.Background()
	if err := ApplySchema(ctx, conn); err != nil {
		t.Fatalf("schema: %v", err)
	}
	if seed {
		if err := ApplySeed(ctx, conn); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	return conn
}

func TestSeedSatisfiesConstraints(t *testing.T) {
	conn := openTest(t, true)
	rows, err := conn.Query("PRAGMA foreign_key_check")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if rows.Next() {
		t.Fatal("seed violates foreign keys")
	}
}

func TestGetItems(t *testing.T) {
	conn := openTest(t, true)
	ctx := context.Background()

	handles, err := GetItems(ctx, conn, "SELECT handle FROM users ORDER BY did", nil,
		func(scan Scan, h *string) error { return scan(h) })
	if err != nil {
		t.Fatal(err)
	}
	if len(handles) != 4 || handles[0] != "seed1.chain.i32.jp" {
		t.Fatalf("handles = %v", handles)
	}

	// Scan のエラーは握りつぶさずに返す。
	_, err = GetItems(ctx, conn, "SELECT handle FROM users", nil,
		func(scan Scan, n *int) error { return scan(n) })
	if err == nil {
		t.Fatal("scan error was swallowed")
	}

	empty, err := GetItems(ctx, conn, "SELECT handle FROM users WHERE did = 'none'", nil,
		func(scan Scan, h *string) error { return scan(h) })
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("empty = %#v, err = %v", empty, err)
	}
}

func TestGetItemNoRows(t *testing.T) {
	conn := openTest(t, false)
	_, err := GetItem(context.Background(), conn, "SELECT handle FROM users WHERE did = ?", []any{"x"},
		func(scan Scan, h *string) error { return scan(h) })
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("err = %v, want sql.ErrNoRows", err)
	}
}

func TestTransactionRollback(t *testing.T) {
	conn := openTest(t, false)
	ctx := context.Background()
	boom := errors.New("boom")
	err := Transaction(ctx, conn, func(tx *sql.Tx) error {
		if _, err := Execute(ctx, tx, "INSERT INTO users (did, handle, pds_url, created_at) VALUES ('d', 'h', 'p', 0)"); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
	var n int
	conn.QueryRow("SELECT count(*) FROM users").Scan(&n)
	if n != 0 {
		t.Fatal("transaction was not rolled back")
	}
}
