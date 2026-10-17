package model

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/applemango/chain/internal/db"
	"github.com/applemango/chain/internal/kv"
)

// BeaconToken は beacon_tokens の行（05 §2.1）。
type BeaconToken struct {
	TokenHash   []byte
	TokenEnc    []byte
	DID         string
	WindowStart int64 // unix 秒、900 の倍数
}

func scanBeaconToken(scan db.Scan, t *BeaconToken) error {
	return scan(&t.TokenHash, &t.TokenEnc, &t.DID, &t.WindowStart)
}

const beaconTokenColumns = "token_hash, token_enc, did, window_start"

func GetBeaconTokenByWindow(ctx context.Context, q db.Querier, did string, windowStart int64) (*BeaconToken, error) {
	return db.GetItem(ctx, q,
		"SELECT "+beaconTokenColumns+" FROM beacon_tokens WHERE did = ? AND window_start = ?",
		[]any{did, windowStart}, scanBeaconToken)
}

func GetBeaconTokenByHash(ctx context.Context, q db.Querier, hash []byte) (*BeaconToken, error) {
	return db.GetItem(ctx, q,
		"SELECT "+beaconTokenColumns+" FROM beacon_tokens WHERE token_hash = ?",
		[]any{hash}, scanBeaconToken)
}

func CreateBeaconToken(ctx context.Context, q db.Querier, t BeaconToken) error {
	_, err := db.Execute(ctx, q,
		"INSERT INTO beacon_tokens (token_hash, token_enc, did, window_start) VALUES (?, ?, ?, ?)",
		t.TokenHash, t.TokenEnc, t.DID, t.WindowStart)
	return err
}

// 逆引きキャッシュ（04 §3 bt:{hex(token_hash)}）の値は {did}|{window_start}。

func BeaconLookupValue(did string, windowStart int64) string {
	return did + "|" + strconv.FormatInt(windowStart, 10)
}

func ParseBeaconLookup(v string) (did string, windowStart int64, ok bool) {
	i := strings.LastIndexByte(v, '|')
	if i <= 0 {
		return "", 0, false
	}
	ws, err := strconv.ParseInt(v[i+1:], 10, 64)
	if err != nil {
		return "", 0, false
	}
	return v[:i], ws, true
}

func SetBeaconLookup(ctx context.Context, r *redis.Client, hashHex, did string, windowStart int64, ttl time.Duration) error {
	return r.Set(ctx, kv.BeaconTokenKey(hashHex), BeaconLookupValue(did, windowStart), ttl).Err()
}

func GetBeaconLookup(ctx context.Context, r *redis.Client, hashHex string) (did string, windowStart int64, found bool, err error) {
	v, err := r.Get(ctx, kv.BeaconTokenKey(hashHex)).Result()
	if err == redis.Nil {
		return "", 0, false, nil
	}
	if err != nil {
		return "", 0, false, err
	}
	did, windowStart, ok := ParseBeaconLookup(v)
	return did, windowStart, ok, nil
}
