package model

import (
	"context"
	"database/sql"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/applemango/chain/internal/clock"
	"github.com/applemango/chain/internal/kv"
)

// Location は loc:{did} の 1 点（05 §4）。履歴は持たない。
type Location struct {
	DID        string
	Lat        float64
	Lon        float64
	AccuracyM  float64
	RecordedAt time.Time
	Source     string
}

func SaveLocation(ctx context.Context, r *redis.Client, loc Location, ttl time.Duration) error {
	key := kv.LocationKey(loc.DID)
	if err := r.HSet(ctx, key, map[string]any{
		"lat": strconv.FormatFloat(loc.Lat, 'f', -1, 64),
		"lon": strconv.FormatFloat(loc.Lon, 'f', -1, 64),
		"acc": strconv.FormatFloat(loc.AccuracyM, 'f', -1, 64),
		"ts":  strconv.FormatInt(clock.UnixMilli(loc.RecordedAt), 10),
		"src": loc.Source,
	}).Err(); err != nil {
		return err
	}
	if err := r.Expire(ctx, key, ttl).Err(); err != nil {
		return err
	}
	return r.GeoAdd(ctx, kv.GeoUsersKey(), &redis.GeoLocation{
		Name: loc.DID, Longitude: loc.Lon, Latitude: loc.Lat,
	}).Err()
}

func GetLocation(ctx context.Context, r *redis.Client, did string) (*Location, error) {
	m, err := r.HGetAll(ctx, kv.LocationKey(did)).Result()
	if err != nil {
		return nil, err
	}
	if len(m) == 0 {
		return nil, sql.ErrNoRows
	}
	lat, err := strconv.ParseFloat(m["lat"], 64)
	if err != nil {
		return nil, err
	}
	lon, err := strconv.ParseFloat(m["lon"], 64)
	if err != nil {
		return nil, err
	}
	acc, err := strconv.ParseFloat(m["acc"], 64)
	if err != nil {
		return nil, err
	}
	ts, err := strconv.ParseInt(m["ts"], 10, 64)
	if err != nil {
		return nil, err
	}
	return &Location{DID: did, Lat: lat, Lon: lon, AccuracyM: acc, RecordedAt: clock.FromUnixMilli(ts), Source: m["src"]}, nil
}

func RemoveGeoUser(ctx context.Context, r *redis.Client, did string) error {
	return r.ZRem(ctx, kv.GeoUsersKey(), did).Err()
}
