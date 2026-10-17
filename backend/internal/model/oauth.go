package model

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/applemango/chain/internal/clock"
	"github.com/applemango/chain/internal/db"
	"github.com/applemango/chain/internal/kv"
)

// ATProtoSession は atproto_oauth_sessions の行。中身（DataEnc）は暗号化されたまま扱う。
type ATProtoSession struct {
	ID        string
	DID       string
	DataEnc   []byte
	RevokedAt *time.Time
}

func GetATProtoSession(ctx context.Context, q db.Querier, id string) (*ATProtoSession, error) {
	return db.GetItem(ctx, q,
		"SELECT id, did, data_enc, revoked_at FROM atproto_oauth_sessions WHERE id = ?",
		[]any{id}, func(scan db.Scan, s *ATProtoSession) error {
			var revoked sql.NullInt64
			if err := scan(&s.ID, &s.DID, &s.DataEnc, &revoked); err != nil {
				return err
			}
			if revoked.Valid {
				t := clock.FromUnixMilli(revoked.Int64)
				s.RevokedAt = &t
			}
			return nil
		})
}

func SaveATProtoSession(ctx context.Context, q db.Querier, id, did string, dataEnc []byte, now time.Time) error {
	_, err := db.Execute(ctx, q, `
		INSERT INTO atproto_oauth_sessions (id, did, data_enc, created_at, updated_at) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET data_enc = excluded.data_enc, updated_at = excluded.updated_at`,
		id, did, dataEnc, clock.UnixMilli(now), clock.UnixMilli(now))
	return err
}

func RevokeATProtoSession(ctx context.Context, q db.Querier, id string, now time.Time) error {
	_, err := db.Execute(ctx, q,
		"UPDATE atproto_oauth_sessions SET data_enc = x'', revoked_at = ?, updated_at = ? WHERE id = ? AND revoked_at IS NULL",
		clock.UnixMilli(now), clock.UnixMilli(now), id)
	return err
}

// OAuth の認可要求（04 §3 oauth:req:{state}）。中身は呼び出し側で暗号化済みのバイト列。

func CreateOAuthRequest(ctx context.Context, r *redis.Client, state string, data []byte, ttl time.Duration) (bool, error) {
	return r.SetNX(ctx, kv.OAuthRequestKey(state), data, ttl).Result()
}

func GetOAuthRequest(ctx context.Context, r *redis.Client, state string) ([]byte, error) {
	b, err := r.Get(ctx, kv.OAuthRequestKey(state)).Bytes()
	if err == redis.Nil {
		return nil, sql.ErrNoRows
	}
	return b, err
}

func DeleteOAuthRequest(ctx context.Context, r *redis.Client, state string) error {
	return r.Del(ctx, kv.OAuthRequestKey(state)).Err()
}

// OneTimeCode はログイン完了からアプリのトークン交換までをつなぐ値（03 §2、04 §3 otc:{code}）。
type OneTimeCode struct {
	DID              string `json:"did"`
	ATProtoSessionID string `json:"atprotoSessionId"`
}

const OneTimeCodeTTL = 60 * time.Second

func SaveOneTimeCode(ctx context.Context, r *redis.Client, code string, v OneTimeCode) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return r.Set(ctx, kv.OneTimeCodeKey(code), b, OneTimeCodeTTL).Err()
}

// TakeOneTimeCode は 1 回だけ読める。なければ sql.ErrNoRows。
func TakeOneTimeCode(ctx context.Context, r *redis.Client, code string) (*OneTimeCode, error) {
	b, err := r.GetDel(ctx, kv.OneTimeCodeKey(code)).Bytes()
	if err == redis.Nil {
		return nil, sql.ErrNoRows
	}
	if err != nil {
		return nil, err
	}
	var v OneTimeCode
	if err := json.Unmarshal(b, &v); err != nil {
		return nil, err
	}
	return &v, nil
}
