package proximity

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"time"

	"github.com/applemango/chain/internal/httpx"
	"github.com/applemango/chain/internal/model"
	"github.com/applemango/chain/internal/service/core"
)

// LocationService は PUT /me/location（05 §4）。
type LocationService struct {
	core.Deps
	Hooks LocationHooks
}

// LocationHooks は位置の保存後に非同期で呼ぶフック。09 §6.1 のエリア入場通知を
// GroupEntryService が実装する。位置共有（07）は要求時に読むのでフックを持たない。
type LocationHooks interface {
	LocationUpdated(ctx context.Context, did string, loc model.Location)
}

// NoopLocationHooks はフックを使わない配線のための空実装。
type NoopLocationHooks struct{}

func (NoopLocationHooks) LocationUpdated(context.Context, string, model.Location) {}

type LocationInput struct {
	Lat        float64
	Lon        float64
	AccuracyM  float64
	RecordedAt time.Time
	Source     string
	IsMock     bool
}

const locationHookTimeout = 5 * time.Second

// Update は検証を通った位置だけを loc:{did} と geo:users に保存する。SQLite には書かない（05 §4）。
func (s *LocationService) Update(ctx context.Context, did string, in LocationInput) (*model.Location, error) {
	now := s.Clock.Now()
	if fields := validateLocation(in, now); len(fields) > 0 {
		return nil, httpx.ErrInvalidRequest.With("fields", fields)
	}

	source := in.Source
	if source == "" {
		source = "gps"
	}
	loc := model.Location{
		DID: did, Lat: in.Lat, Lon: in.Lon, AccuracyM: in.AccuracyM,
		RecordedAt: in.RecordedAt, Source: source,
	}

	prev, err := model.GetLocation(ctx, s.Redis, did)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if prev != nil && tooFast(prev, &loc) {
		s.recordSpeedReject(ctx, did)
		return nil, httpx.ErrInvalidRequest.With("fields", map[string]string{
			"lat": "直前の位置からの移動速度が速すぎます",
		})
	}

	if err := model.SaveLocation(ctx, s.Redis, loc, core.LocationTTL); err != nil {
		return nil, err
	}
	s.notify(did, loc)
	return &loc, nil
}

func validateLocation(in LocationInput, now time.Time) map[string]string {
	fields := map[string]string{}
	if in.Lat < -90 || in.Lat > 90 {
		fields["lat"] = "-90 から 90 の範囲で指定してください"
	}
	if in.Lon < -180 || in.Lon > 180 {
		fields["lon"] = "-180 から 180 の範囲で指定してください"
	}
	if in.AccuracyM < 0 || in.AccuracyM > core.LocationMaxAccuracy {
		fields["accuracyM"] = "0 から 200 の範囲で指定してください"
	}
	if in.RecordedAt.Before(now.Add(-core.LocationFresh)) || in.RecordedAt.After(now.Add(core.LocationFutureSkew)) {
		fields["recordedAt"] = "現在時刻の前後（-120 秒〜+30 秒）で指定してください"
	}
	if in.IsMock {
		fields["isMock"] = "モック位置は受け付けません"
	}
	return fields
}

// speedCheckMinInterval より短い間隔の 2 点は、GPS の揺れだけで速度が跳ね上がるので判定しない。
// speedCheckSlackM は静止していても出る位置の揺れ (10〜20m) を無視するための余裕。
const speedCheckSlackM = 20.0

const speedCheckMinInterval = 10 * time.Second

func tooFast(prev, next *model.Location) bool {
	dt := next.RecordedAt.Sub(prev.RecordedAt)
	if dt < speedCheckMinInterval {
		return false
	}
	// 両方の精度円に収まる分は誤差として移動距離から引く。
	km := Haversine(prev.Lat, prev.Lon, next.Lat, next.Lon) - (prev.AccuracyM+next.AccuracyM+speedCheckSlackM)/1000
	if km <= 0 {
		return false
	}
	return km/dt.Hours() > core.MaxSpeedKmh
}

func (s *LocationService) recordSpeedReject(ctx context.Context, did string) {
	err := model.InsertAuditLog(ctx, s.DB, model.AuditEntry{
		ID:        s.IDs.NewID(),
		ActorDID:  did,
		Action:    "location.speed_rejected",
		CreatedAt: s.Clock.Now(),
	})
	if err != nil {
		slog.WarnContext(ctx, "audit log failed", "err", err)
	}
}

func (s *LocationService) notify(did string, loc model.Location) {
	if s.Hooks == nil {
		return
	}
	go func() {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("location hook panic", "err", r)
			}
		}()
		ctx, cancel := context.WithTimeout(context.Background(), locationHookTimeout)
		defer cancel()
		s.Hooks.LocationUpdated(ctx, did, loc)
	}()
}
