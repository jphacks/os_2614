package model

import (
	"context"
	"database/sql"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/applemango/chain/internal/clock"
	"github.com/applemango/chain/internal/db"
	"github.com/applemango/chain/internal/kv"
)

// AppSession は app_sessions の行（03 §3）。refresh のローテーションごとに新しい行を作る。
type AppSession struct {
	ID               string
	DID              string
	ATProtoSessionID string
	FamilyID         string
	RefreshHash      string
	DeviceName       string
	CreatedAt        time.Time
	LastUsedAt       time.Time
	ExpiresAt        time.Time
	RevokedAt        *time.Time
}

const appSessionColumns = `id, did, atproto_session, family_id, refresh_hash, device_name,
	created_at, last_used_at, expires_at, revoked_at`

func scanAppSession(scan db.Scan, s *AppSession) error {
	var device sql.NullString
	var created, used, expires int64
	var revoked sql.NullInt64
	if err := scan(&s.ID, &s.DID, &s.ATProtoSessionID, &s.FamilyID, &s.RefreshHash, &device,
		&created, &used, &expires, &revoked); err != nil {
		return err
	}
	s.DeviceName = device.String
	s.CreatedAt = clock.FromUnixMilli(created)
	s.LastUsedAt = clock.FromUnixMilli(used)
	s.ExpiresAt = clock.FromUnixMilli(expires)
	if revoked.Valid {
		t := clock.FromUnixMilli(revoked.Int64)
		s.RevokedAt = &t
	}
	return nil
}

func GetAppSession(ctx context.Context, q db.Querier, id string) (*AppSession, error) {
	return db.GetItem(ctx, q, "SELECT "+appSessionColumns+" FROM app_sessions WHERE id = ?", []any{id}, scanAppSession)
}

func GetAppSessionByRefreshHash(ctx context.Context, q db.Querier, hash string) (*AppSession, error) {
	return db.GetItem(ctx, q, "SELECT "+appSessionColumns+" FROM app_sessions WHERE refresh_hash = ?", []any{hash}, scanAppSession)
}

func CreateAppSession(ctx context.Context, q db.Querier, s AppSession) error {
	_, err := db.Execute(ctx, q, `
		INSERT INTO app_sessions (id, did, atproto_session, family_id, refresh_hash, device_name,
		                          created_at, last_used_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		s.ID, s.DID, s.ATProtoSessionID, s.FamilyID, s.RefreshHash, Nullable(s.DeviceName),
		clock.UnixMilli(s.CreatedAt), clock.UnixMilli(s.LastUsedAt), clock.UnixMilli(s.ExpiresAt))
	return err
}

// RevokeAppSession は 1 行を失効させる。既に失効していれば false。
func RevokeAppSession(ctx context.Context, q db.Querier, id string, now time.Time) (bool, error) {
	res, err := db.Execute(ctx, q,
		"UPDATE app_sessions SET revoked_at = ? WHERE id = ? AND revoked_at IS NULL", clock.UnixMilli(now), id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// RevokeAppSessionFamily は系列のすべてを失効させ、失効させた行の ID を返す（refresh の再利用検知）。
func RevokeAppSessionFamily(ctx context.Context, q db.Querier, familyID string, now time.Time) ([]string, error) {
	ids, err := db.GetItems(ctx, q,
		"SELECT id FROM app_sessions WHERE family_id = ? AND revoked_at IS NULL", []any{familyID},
		func(scan db.Scan, id *string) error { return scan(id) })
	if err != nil {
		return nil, err
	}
	_, err = db.Execute(ctx, q,
		"UPDATE app_sessions SET revoked_at = ? WHERE family_id = ? AND revoked_at IS NULL", clock.UnixMilli(now), familyID)
	return ids, err
}

// CountActiveAppSessions は ATProto セッションを共有している、有効なアプリセッションの数。
func CountActiveAppSessions(ctx context.Context, q db.Querier, atprotoSessionID string, now time.Time) (int, error) {
	var n int
	err := q.QueryRowContext(ctx,
		"SELECT count(*) FROM app_sessions WHERE atproto_session = ? AND revoked_at IS NULL AND expires_at > ?",
		atprotoSessionID, clock.UnixMilli(now)).Scan(&n)
	return n, err
}

// セッションの有効性のキャッシュ（04 §3 sess:{sid}）。値は "1"（有効）/ "0"（失効）。

const SessionCacheTTL = 15 * time.Minute

func GetSessionCache(ctx context.Context, r *redis.Client, sid string) (active, found bool, err error) {
	v, err := r.Get(ctx, kv.SessionKey(sid)).Result()
	if err == redis.Nil {
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}
	return v == "1", true, nil
}

func SetSessionCache(ctx context.Context, r *redis.Client, sid string, active bool) error {
	v := "0"
	if active {
		v = "1"
	}
	return r.Set(ctx, kv.SessionKey(sid), v, SessionCacheTTL).Err()
}
