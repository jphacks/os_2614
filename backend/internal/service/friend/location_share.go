package friend

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
	friendmodel "github.com/applemango/chain/internal/model/friend"
	"github.com/applemango/chain/internal/service/core"
	"github.com/applemango/chain/internal/service/proximity"
)

// 07 §4.1 の共有精度。
const (
	PrecisionExact     = "exact"
	PrecisionApprox    = "approx"
	PrecisionArea      = "area"
	PrecisionDirection = "direction"
	PrecisionOff       = "off"
)

// 07 §4 のパラメータ。
const (
	LocationStaleWindow = 30 * time.Minute
	approxGeohashLen    = 6
	exactDecimals       = 5
	// directionViewerFresh は direction で見る側の位置を新しいとみなす窓（07 §4.1「見る側の位置も 2 分以内」）。
	directionViewerFresh = 2 * time.Minute
)

// directionBuckets は距離帯の上限（m、この値未満）。順に評価する（07 §4.1）。
var directionBuckets = []struct {
	limitM float64
	label  string
}{
	{100, "<100m"},
	{500, "<500m"},
	{2000, "<2km"},
	{10000, "<10km"},
}

// compassPoints は 8 方位。各方位は 45° の帯の中央（N=0°、NE=45°…）に置く。
var compassPoints = [8]string{"N", "NE", "E", "SE", "S", "SW", "W", "NW"}

// 07 §4.3 のパラメータ。
const (
	MaxAreas        = 10
	MinAreaRadiusM  = 50
	MaxAreaRadiusM  = 5000
	MaxAreaLabelLen = 20
)

// LocationShareService は相手ごとの位置共有の精度・一時停止・エリア（07 §4）。
type LocationShareService struct {
	core.Deps
}

// SetPrecision は owner が viewer に見せる精度と areasOnly を保存する（07 §4.1、§4.2）。
// viewer とフレンドでなければ 404。
func (s *LocationShareService) SetPrecision(ctx context.Context, owner, viewer, precision string, areasOnly bool) error {
	if viewer == owner {
		return httpx.ErrNotFound
	}
	if !validPrecision(precision) {
		return httpx.ErrInvalidRequest.With("fields", map[string]string{
			"precision": "exact / approx / area / direction / off のいずれかを指定してください",
		})
	}
	if _, err := friendmodel.GetFriendship(ctx, s.DB, owner, viewer); errors.Is(err, sql.ErrNoRows) {
		return httpx.ErrNotFound
	} else if err != nil {
		return err
	}
	return friendmodel.UpsertLocationShare(ctx, s.DB, owner, viewer, precision, areasOnly, s.Clock.Now())
}

// SetPaused は全員への共有を一時停止する（07 §4.1、PUT /me/location-sharing）。
func (s *LocationShareService) SetPaused(ctx context.Context, did string, paused bool) error {
	return friendmodel.SetLocationPaused(ctx, s.DB, did, paused, s.Clock.Now())
}

func validPrecision(p string) bool {
	switch p {
	case PrecisionExact, PrecisionApprox, PrecisionArea, PrecisionDirection, PrecisionOff:
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
// direction のときは Lat/Lon ではなく Direction・DistanceBucket・ViewerStale を使う。
type FriendLocation struct {
	DID            string
	Precision      string
	Lat            float64
	Lon            float64
	AccuracyM      float64
	Area           *AreaLabel
	Outside        bool
	Direction      string
	DistanceBucket string
	ViewerStale    bool
	UpdatedAt      *time.Time
	Stale          bool
}

// ListLocations は自分のフレンド全員について、相手が自分に設定した精度で位置を計算して返す（07 §4.2）。
// 保存はしない。ブロック関係の相手は出さない。
func (s *LocationShareService) ListLocations(ctx context.Context, me string) ([]FriendLocation, error) {
	friends, err := friendmodel.ListFriendships(ctx, s.DB, me)
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

	share, err := friendmodel.GetLocationShareOrDefault(ctx, s.DB, peer, me)
	if err != nil {
		return nil, err
	}
	item.Precision = share.Precision
	if share.Precision == PrecisionOff {
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

	// areasOnly は登録エリアのどれかの中にいるときだけ見せる。外は off と同じ（07 §4.1）。
	if share.AreasOnly {
		_, inside, err := s.areaLabelAt(ctx, peer, loc)
		if err != nil {
			return nil, err
		}
		if !inside {
			item.Precision = PrecisionOff
			return item, nil
		}
	}

	at := loc.RecordedAt.Truncate(time.Minute)
	item.UpdatedAt = &at

	switch share.Precision {
	case PrecisionExact:
		item.Lat = roundCoord(loc.Lat)
		item.Lon = roundCoord(loc.Lon)
		item.AccuracyM = loc.AccuracyM
	case PrecisionApprox:
		item.Lat, item.Lon = core.GeohashCellCenter(loc.Lat, loc.Lon, approxGeohashLen)
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
	case PrecisionDirection:
		if err := s.setDirection(ctx, item, me, loc, now); err != nil {
			return nil, err
		}
	}
	return item, nil
}

// setDirection は見る側（me）から相手（peerLoc）への真方位と距離帯を item に埋める（07 §4.1）。
// 見る側の位置がなければ、または directionViewerFresh より古ければ ViewerStale を立て、
// 方角と距離は返さない。
func (s *LocationShareService) setDirection(ctx context.Context, item *FriendLocation, me string, peerLoc *model.Location, now time.Time) error {
	viewerLoc, err := model.GetLocation(ctx, s.Redis, me)
	if errors.Is(err, sql.ErrNoRows) {
		item.ViewerStale = true
		return nil
	}
	if err != nil {
		return err
	}
	if now.Sub(viewerLoc.RecordedAt) > directionViewerFresh {
		item.ViewerStale = true
		return nil
	}
	item.Direction = CompassDirection(InitialBearing(viewerLoc.Lat, viewerLoc.Lon, peerLoc.Lat, peerLoc.Lon))
	item.DistanceBucket = DistanceBucket(proximity.Haversine(viewerLoc.Lat, viewerLoc.Lon, peerLoc.Lat, peerLoc.Lon))
	return nil
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
	areas, err := friendmodel.ListAreas(ctx, s.DB, did)
	if err != nil {
		return "", false, err
	}
	best := -1
	for i := range areas {
		if !proximity.InsideZone(proximity.Zone{ID: areas[i].ID, CenterLat: areas[i].Lat, CenterLon: areas[i].Lon, RadiusM: areas[i].RadiusM}, loc) {
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

// InitialBearing は (lat1,lon1) から (lat2,lon2) への真方位を北 0°・東回りの度（[0,360)）で返す（07 §4.1）。
// 2 点が同じでも 0（北）を返す。
func InitialBearing(lat1, lon1, lat2, lon2 float64) float64 {
	const rad = math.Pi / 180
	phi1, phi2 := lat1*rad, lat2*rad
	dLambda := (lon2 - lon1) * rad
	y := math.Sin(dLambda) * math.Cos(phi2)
	x := math.Cos(phi1)*math.Sin(phi2) - math.Sin(phi1)*math.Cos(phi2)*math.Cos(dLambda)
	return math.Mod(math.Atan2(y, x)/rad+360, 360)
}

// CompassDirection は真方位（度）を 8 方位に丸める。各方位は 45° の帯の中央に置き、帯は
// 中央 ±22.5°（22.5° は NE、337.5° は N のように上の帯に入る）（07 §4.1）。
func CompassDirection(bearingDeg float64) string {
	b := math.Mod(math.Mod(bearingDeg, 360)+360, 360)
	return compassPoints[int(math.Floor((b+22.5)/45))%8]
}

// DistanceBucket は距離（m）を 07 §4.1 の距離帯に分ける。境界値は上の帯に入る（100 m は <500m）。
func DistanceBucket(distM float64) string {
	for _, b := range directionBuckets {
		if distM < b.limitM {
			return b.label
		}
	}
	return "far"
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

func (s *LocationShareService) ListAreas(ctx context.Context, did string) ([]friendmodel.Area, error) {
	return friendmodel.ListAreas(ctx, s.DB, did)
}

// CreateArea は自分のエリアを作る。上限は 10 個（07 §4.3）。
func (s *LocationShareService) CreateArea(ctx context.Context, did string, in AreaInput) (*friendmodel.Area, error) {
	if fields := validateArea(in.Label, in.Lat, in.Lon, in.RadiusM); len(fields) > 0 {
		return nil, httpx.ErrInvalidRequest.With("fields", fields)
	}
	n, err := friendmodel.CountAreas(ctx, s.DB, did)
	if err != nil {
		return nil, err
	}
	if n >= MaxAreas {
		return nil, httpx.ErrConflict.With("reason", fmt.Sprintf("エリアの上限（%d 個）に達しています", MaxAreas))
	}
	a := friendmodel.Area{ID: s.IDs.NewID(), DID: did, Label: in.Label, Lat: in.Lat, Lon: in.Lon, RadiusM: in.RadiusM, CreatedAt: s.Clock.Now()}
	if err := friendmodel.CreateArea(ctx, s.DB, a); err != nil {
		return nil, err
	}
	return &a, nil
}

// UpdateArea は自分のエリアを変更する。他人のエリアは 404（07 §4.3）。
func (s *LocationShareService) UpdateArea(ctx context.Context, did, id string, in AreaUpdate) (*friendmodel.Area, error) {
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
	if err := friendmodel.UpdateArea(ctx, s.DB, id, friendmodel.AreaPatch{
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
	_, err := friendmodel.DeleteArea(ctx, s.DB, id)
	return err
}

func (s *LocationShareService) ownedArea(ctx context.Context, did, id string) (*friendmodel.Area, error) {
	a, err := friendmodel.GetArea(ctx, s.DB, id)
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
