package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/applemango/chain/internal/httpx"
	"github.com/applemango/chain/internal/model"
)

// 07 §4.1 の共有精度。direction は P2 なので扱わない。
const (
	PrecisionExact  = "exact"
	PrecisionApprox = "approx"
	PrecisionArea   = "area"
	PrecisionOff    = "off"
)

// 07 §4 のパラメータ。
const (
	LocationStaleWindow = 30 * time.Minute
	approxGeohashLen    = 6
	exactDecimals       = 5
)

// 07 §4.3 のパラメータ。
const (
	MaxAreas        = 10
	MinAreaRadiusM  = 50
	MaxAreaRadiusM  = 5000
	MaxAreaLabelLen = 20
)

// LocationShareService は相手ごとの位置共有の精度・一時停止・エリア（07 §4）。
type LocationShareService struct {
	Deps
}

// SetPrecision は owner が viewer に見せる精度を保存する（07 §4.2）。
// viewer とフレンドでなければ 404。direction と areasOnly は P2 なので 400。
func (s *LocationShareService) SetPrecision(ctx context.Context, owner, viewer, precision string, areasOnly bool) error {
	if viewer == owner {
		return httpx.ErrNotFound
	}
	fields := map[string]string{}
	if areasOnly {
		fields["areasOnly"] = "P2 の項目です"
	}
	if !validPrecision(precision) {
		fields["precision"] = "exact / approx / area / off のいずれかを指定してください"
	}
	if len(fields) > 0 {
		return httpx.ErrInvalidRequest.With("fields", fields)
	}
	if _, err := model.GetFriendship(ctx, s.DB, owner, viewer); errors.Is(err, sql.ErrNoRows) {
		return httpx.ErrNotFound
	} else if err != nil {
		return err
	}
	return model.UpsertLocationShare(ctx, s.DB, owner, viewer, precision, false, s.Clock.Now())
}

// SetPaused は全員への共有を一時停止する（07 §4.1、PUT /me/location-sharing）。
func (s *LocationShareService) SetPaused(ctx context.Context, did string, paused bool) error {
	return model.SetLocationPaused(ctx, s.DB, did, paused, s.Clock.Now())
}

func validPrecision(p string) bool {
	switch p {
	case PrecisionExact, PrecisionApprox, PrecisionArea, PrecisionOff:
		return true
	default:
		return false
	}
}

// AreaLabel は area 精度で見せる、今いるエリアの label。
type AreaLabel struct {
	Label string
}

// FriendLocation は GET /friends/locations の 1 件（07 §4.2）。
type FriendLocation struct {
	DID       string
	Precision string
	Lat       float64
	Lon       float64
	AccuracyM float64
	Area      *AreaLabel
	Outside   bool
	UpdatedAt *time.Time
	Stale     bool
}

// ListLocations は自分のフレンド全員について、相手が自分に設定した精度で位置を計算して返す（07 §4.2）。
// 保存はしない。ブロック関係の相手は出さない。
func (s *LocationShareService) ListLocations(ctx context.Context, me string) ([]FriendLocation, error) {
	friends, err := model.ListFriendships(ctx, s.DB, me)
	if err != nil {
		return nil, err
	}
	now := s.Clock.Now()
	items := make([]FriendLocation, 0, len(friends))
	for _, f := range friends {
		peer := f.UserA
		if peer == me {
			peer = f.UserB
		}
		blocked, err := model.IsBlocked(ctx, s.DB, me, peer)
		if err != nil {
			return nil, err
		}
		if blocked {
			continue
		}
		item, err := s.locationFor(ctx, me, peer, now)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, nil
}

func (s *LocationShareService) locationFor(ctx context.Context, me, peer string, now time.Time) (*FriendLocation, error) {
	item := &FriendLocation{DID: peer}

	paused, err := s.isPaused(ctx, peer)
	if err != nil {
		return nil, err
	}
	if paused {
		item.Precision = PrecisionOff
		return item, nil
	}

	precision, err := model.GetLocationPrecision(ctx, s.DB, peer, me)
	if err != nil {
		return nil, err
	}
	item.Precision = precision
	if precision == PrecisionOff {
		return item, nil
	}

	loc, err := model.GetLocation(ctx, s.Redis, peer)
	if errors.Is(err, sql.ErrNoRows) {
		item.Stale = true
		return item, nil
	}
	if err != nil {
		return nil, err
	}
	if now.Sub(loc.RecordedAt) > LocationStaleWindow {
		item.Stale = true
		return item, nil
	}

	at := loc.RecordedAt.Truncate(time.Minute)
	item.UpdatedAt = &at

	switch precision {
	case PrecisionExact:
		item.Lat = roundCoord(loc.Lat)
		item.Lon = roundCoord(loc.Lon)
		item.AccuracyM = loc.AccuracyM
	case PrecisionApprox:
		item.Lat, item.Lon = GeohashCellCenter(loc.Lat, loc.Lon, approxGeohashLen)
	case PrecisionArea:
		label, ok, err := s.areaLabelAt(ctx, peer, loc)
		if err != nil {
			return nil, err
		}
		if ok {
			item.Area = &AreaLabel{Label: label}
		} else {
			item.Outside = true
		}
	}
	return item, nil
}

func (s *LocationShareService) isPaused(ctx context.Context, did string) (bool, error) {
	st, err := model.GetSettings(ctx, s.DB, did)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return st.LocationPaused, nil
}

// areaLabelAt は loc を含むエリアのうち最も小さいものの label を返す（07 §4.3）。
func (s *LocationShareService) areaLabelAt(ctx context.Context, did string, loc *model.Location) (string, bool, error) {
	areas, err := model.ListAreas(ctx, s.DB, did)
	if err != nil {
		return "", false, err
	}
	best := -1
	for i := range areas {
		if !InsideZone(Zone{ID: areas[i].ID, CenterLat: areas[i].Lat, CenterLon: areas[i].Lon, RadiusM: areas[i].RadiusM}, loc) {
			continue
		}
		if best < 0 || areas[i].RadiusM < areas[best].RadiusM {
			best = i
		}
	}
	if best < 0 {
		return "", false, nil
	}
	return areas[best].Label, true, nil
}

// roundCoord は exact 精度の丸め（小数 5 桁 ≒ 1 m。07 §4.1）。
func roundCoord(v float64) float64 {
	scale := math.Pow(10, exactDecimals)
	return math.Round(v*scale) / scale
}

// AreaInput は POST /me/areas の入力（07 §4.3）。
type AreaInput struct {
	Label   string
	Lat     float64
	Lon     float64
	RadiusM int
}

// AreaUpdate は PATCH /me/areas/{id} の入力。nil の項目は変更しない。
type AreaUpdate struct {
	Label   *string
	Lat     *float64
	Lon     *float64
	RadiusM *int
}

func (s *LocationShareService) ListAreas(ctx context.Context, did string) ([]model.Area, error) {
	return model.ListAreas(ctx, s.DB, did)
}

// CreateArea は自分のエリアを作る。上限は 10 個（07 §4.3）。
func (s *LocationShareService) CreateArea(ctx context.Context, did string, in AreaInput) (*model.Area, error) {
	if fields := validateArea(in.Label, in.Lat, in.Lon, in.RadiusM); len(fields) > 0 {
		return nil, httpx.ErrInvalidRequest.With("fields", fields)
	}
	n, err := model.CountAreas(ctx, s.DB, did)
	if err != nil {
		return nil, err
	}
	if n >= MaxAreas {
		return nil, httpx.ErrConflict.With("reason", fmt.Sprintf("エリアの上限（%d 個）に達しています", MaxAreas))
	}
	a := model.Area{ID: s.IDs.NewID(), DID: did, Label: in.Label, Lat: in.Lat, Lon: in.Lon, RadiusM: in.RadiusM, CreatedAt: s.Clock.Now()}
	if err := model.CreateArea(ctx, s.DB, a); err != nil {
		return nil, err
	}
	return &a, nil
}

// UpdateArea は自分のエリアを変更する。他人のエリアは 404（07 §4.3）。
func (s *LocationShareService) UpdateArea(ctx context.Context, did, id string, in AreaUpdate) (*model.Area, error) {
	a, err := s.ownedArea(ctx, did, id)
	if err != nil {
		return nil, err
	}
	next := *a
	if in.Label != nil {
		next.Label = *in.Label
	}
	if in.Lat != nil {
		next.Lat = *in.Lat
	}
	if in.Lon != nil {
		next.Lon = *in.Lon
	}
	if in.RadiusM != nil {
		next.RadiusM = *in.RadiusM
	}
	if fields := validateArea(next.Label, next.Lat, next.Lon, next.RadiusM); len(fields) > 0 {
		return nil, httpx.ErrInvalidRequest.With("fields", fields)
	}
	if err := model.UpdateArea(ctx, s.DB, id, model.AreaPatch{
		Label: in.Label, Lat: in.Lat, Lon: in.Lon, RadiusM: in.RadiusM,
	}); err != nil {
		return nil, err
	}
	return &next, nil
}

// DeleteArea は自分のエリアを消す。他人のエリアは 404（07 §4.3）。
func (s *LocationShareService) DeleteArea(ctx context.Context, did, id string) error {
	if _, err := s.ownedArea(ctx, did, id); err != nil {
		return err
	}
	_, err := model.DeleteArea(ctx, s.DB, id)
	return err
}

func (s *LocationShareService) ownedArea(ctx context.Context, did, id string) (*model.Area, error) {
	a, err := model.GetArea(ctx, s.DB, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, httpx.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if a.DID != did {
		return nil, httpx.ErrNotFound
	}
	return a, nil
}

func validateArea(label string, lat, lon float64, radiusM int) map[string]string {
	fields := map[string]string{}
	switch {
	case strings.TrimSpace(label) == "":
		fields["label"] = "必須です"
	case utf8.RuneCountInString(label) > MaxAreaLabelLen:
		fields["label"] = fmt.Sprintf("%d 文字までです", MaxAreaLabelLen)
	}
	if lat < -90 || lat > 90 {
		fields["lat"] = "-90 から 90 の範囲で指定してください"
	}
	if lon < -180 || lon > 180 {
		fields["lon"] = "-180 から 180 の範囲で指定してください"
	}
	if radiusM < MinAreaRadiusM || radiusM > MaxAreaRadiusM {
		fields["radiusM"] = fmt.Sprintf("%d から %d の範囲で指定してください", MinAreaRadiusM, MaxAreaRadiusM)
	}
	return fields
}
