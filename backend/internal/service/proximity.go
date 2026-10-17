package service

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"time"

	"github.com/applemango/chain/internal/model"
)

// 05 §1 の共通 interface。実装は main.go で組み立てる。

type Kind string

const (
	KindBLE Kind = "ble"
	KindGPS Kind = "gps"
)

type Neighbor struct {
	DID      string
	LastSeen time.Time
	RSSI     *int
	DistM    *float64
}

type PeerProximity interface {
	Kind() Kind
	Observe(ctx context.Context, a, b string, first, last time.Time, rssi *int) error
	Mutual(ctx context.Context, a, b string, at time.Time, within time.Duration) (bool, error)
	Neighbors(ctx context.Context, u string, within time.Duration) ([]Neighbor, error)
}

type Zone struct {
	ID        string
	CenterLat float64
	CenterLon float64
	RadiusM   int
}

type ZonePresence interface {
	Kind() Kind
	Inside(ctx context.Context, u string, z Zone, maxAge time.Duration) (bool, error)
}

// ErrStale は判定に使える新しい位置がないことを表す（05 §4.1）。呼び出し側で 403 location_stale にする。
var ErrStale = errors.New("proximity: location is stale")

// BLEPeerProximity は Redis の sgt: / ble:recent: を使う P0 実装（05 §3）。
type BLEPeerProximity struct{ Deps }

func (p *BLEPeerProximity) Kind() Kind { return KindBLE }

func (p *BLEPeerProximity) Observe(ctx context.Context, a, b string, first, last time.Time, rssi *int) error {
	bucket := last.Truncate(SightingBucket).Unix()
	return model.ObserveSighting(ctx, p.Redis, a, b, bucket, first, last, rssi, p.Clock.Now())
}

func (p *BLEPeerProximity) Mutual(ctx context.Context, a, b string, at time.Time, within time.Duration) (bool, error) {
	return model.MutualSighting(ctx, p.Redis, a, b, at, within, SightingBucket)
}

func (p *BLEPeerProximity) Neighbors(ctx context.Context, u string, within time.Duration) ([]Neighbor, error) {
	recent, err := model.RecentNeighbors(ctx, p.Redis, u, within, p.Clock.Now())
	if err != nil {
		return nil, err
	}
	out := make([]Neighbor, 0, len(recent))
	for _, n := range recent {
		out = append(out, Neighbor{DID: n.DID, LastSeen: n.LastSeen})
	}
	return out, nil
}

// GPSZonePresence は loc:{did} と geofence を使う P0 実装（05 §4.1）。
type GPSZonePresence struct{ Deps }

func (p *GPSZonePresence) Kind() Kind { return KindGPS }

func (p *GPSZonePresence) Inside(ctx context.Context, u string, z Zone, maxAge time.Duration) (bool, error) {
	loc, err := model.GetLocation(ctx, p.Redis, u)
	if errors.Is(err, sql.ErrNoRows) {
		return false, ErrStale
	}
	if err != nil {
		return false, err
	}
	if p.Clock.Now().Sub(loc.RecordedAt) > maxAge {
		return false, ErrStale
	}
	return InsideZone(z, loc), nil
}

// InsideZone は z の円の内側かを、位置の精度の半分（半径の 1/2 を上限）の許容幅を足して判定する（05 §4.1）。
func InsideZone(z Zone, loc *model.Location) bool {
	tolerance := loc.AccuracyM
	if half := float64(z.RadiusM) * 0.5; tolerance > half {
		tolerance = half
	}
	return Haversine(z.CenterLat, z.CenterLon, loc.Lat, loc.Lon) <= float64(z.RadiusM)+tolerance
}

const earthRadiusM = 6371000.0

// Haversine は 2 点間の距離（m）を返す。
func Haversine(lat1, lon1, lat2, lon2 float64) float64 {
	const rad = math.Pi / 180
	phi1, phi2 := lat1*rad, lat2*rad
	dPhi := (lat2 - lat1) * rad
	dLambda := (lon2 - lon1) * rad
	a := math.Sin(dPhi/2)*math.Sin(dPhi/2) +
		math.Cos(phi1)*math.Cos(phi2)*math.Sin(dLambda/2)*math.Sin(dLambda/2)
	return earthRadiusM * 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}
