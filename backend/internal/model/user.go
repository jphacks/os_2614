package model

import (
	"context"
	"database/sql"
	"time"

	"github.com/applemango/chain/internal/clock"
	"github.com/applemango/chain/internal/db"
)

type User struct {
	DID       string
	Handle    string
	PDSURL    string
	CreatedAt time.Time
	DeletedAt *time.Time
}

func ScanUser(scan db.Scan, u *User) error {
	var created int64
	var deleted sql.NullInt64
	if err := scan(&u.DID, &u.Handle, &u.PDSURL, &created, &deleted); err != nil {
		return err
	}
	u.CreatedAt = clock.FromUnixMilli(created)
	if deleted.Valid {
		t := clock.FromUnixMilli(deleted.Int64)
		u.DeletedAt = &t
	}
	return nil
}

func GetUser(ctx context.Context, q db.Querier, did string) (*User, error) {
	return db.GetItem(ctx, q,
		"SELECT did, handle, pds_url, created_at, deleted_at FROM users WHERE did = ?",
		[]any{did}, ScanUser)
}

// UpsertUser はログインのたびに handle と PDS を最新にする。
// 削除済み（13 §5）のユーザーが再びログインした場合は、新しいユーザーとして扱い deleted_at を消す。
func UpsertUser(ctx context.Context, q db.Querier, did, handle, pdsURL string, now time.Time) error {
	_, err := db.Execute(ctx, q, `
		INSERT INTO users (did, handle, pds_url, created_at) VALUES (?, ?, ?, ?)
		ON CONFLICT (did) DO UPDATE SET handle = excluded.handle, pds_url = excluded.pds_url, deleted_at = NULL`,
		did, handle, pdsURL, clock.UnixMilli(now))
	return err
}

// CreateUserIfMissing は行がなければ handle を DID にして作る（OAuth の保存が users より先に来るため）。
func CreateUserIfMissing(ctx context.Context, q db.Querier, did, pdsURL string, now time.Time) error {
	_, err := db.Execute(ctx, q,
		"INSERT INTO users (did, handle, pds_url, created_at) VALUES (?, ?, ?, ?) ON CONFLICT (did) DO NOTHING",
		did, did, pdsURL, clock.UnixMilli(now))
	return err
}
