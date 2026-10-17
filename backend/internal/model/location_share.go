package model

import (
	"context"
	"strings"
	"time"

	"github.com/applemango/chain/internal/clock"
	"github.com/applemango/chain/internal/db"
)

// LocationShare は location_shares の行（04 §2.3、07 §4）。
type LocationShare struct {
	Owner     string
	Viewer    string
	Precision string
	AreasOnly bool
	UpdatedAt time.Time
}

const locationShareColumns = `owner, viewer, precision, areas_only, updated_at`

func scanLocationShare(scan db.Scan, s *LocationShare) error {
	var updated int64
	if err := scan(&s.Owner, &s.Viewer, &s.Precision, &s.AreasOnly, &updated); err != nil {
		return err
	}
	s.UpdatedAt = clock.FromUnixMilli(updated)
	return nil
}

// UpsertLocationShare は owner→viewer の共有精度を作る/更新する。
func UpsertLocationShare(ctx context.Context, q db.Querier, owner, viewer, precision string, areasOnly bool, now time.Time) error {
	_, err := db.Execute(ctx, q, `
		INSERT INTO location_shares (owner, viewer, precision, areas_only, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (owner, viewer) DO UPDATE SET
			precision = excluded.precision,
			areas_only = excluded.areas_only,
			updated_at = excluded.updated_at`,
		owner, viewer, precision, areasOnly, clock.UnixMilli(now))
	return err
}

// GetLocationShare は行を返す。なければ sql.ErrNoRows。
func GetLocationShare(ctx context.Context, q db.Querier, owner, viewer string) (*LocationShare, error) {
	return db.GetItem(ctx, q,
		"SELECT "+locationShareColumns+" FROM location_shares WHERE owner = ? AND viewer = ?",
		[]any{owner, viewer}, scanLocationShare)
}

// CreateDefaultLocationShares は a と b の双方向に既定 precision（DefaultLocationPrecision）かつ
// areas_only=0 の行を作る。既に同じ (owner,viewer) の行があれば触らない（ON CONFLICT DO NOTHING）。
func CreateDefaultLocationShares(ctx context.Context, q db.Querier, a, b string, now time.Time) error {
	_, err := db.Execute(ctx, q, `
		INSERT INTO location_shares (owner, viewer, precision, areas_only, updated_at)
		VALUES (?, ?, ?, 0, ?), (?, ?, ?, 0, ?)
		ON CONFLICT (owner, viewer) DO NOTHING`,
		a, b, DefaultLocationPrecision, clock.UnixMilli(now),
		b, a, DefaultLocationPrecision, clock.UnixMilli(now))
	return err
}

// SetLocationPaused は user_settings.location_paused を更新する。行が無ければ既定値で作ってから更新する。
func SetLocationPaused(ctx context.Context, q db.Querier, did string, paused bool, now time.Time) error {
	if err := EnsureSettings(ctx, q, did, now); err != nil {
		return err
	}
	_, err := db.Execute(ctx, q,
		"UPDATE user_settings SET location_paused = ?, updated_at = ? WHERE did = ?",
		paused, clock.UnixMilli(now), did)
	return err
}

// Area は areas の行（04 §2.3、07 §4.3）。
type Area struct {
	ID        string
	DID       string
	Label     string
	Lat       float64
	Lon       float64
	RadiusM   int
	CreatedAt time.Time
}

const areaColumns = `id, did, label, lat, lon, radius_m, created_at`

func scanArea(scan db.Scan, a *Area) error {
	var created int64
	if err := scan(&a.ID, &a.DID, &a.Label, &a.Lat, &a.Lon, &a.RadiusM, &created); err != nil {
		return err
	}
	a.CreatedAt = clock.FromUnixMilli(created)
	return nil
}

func CreateArea(ctx context.Context, q db.Querier, a Area) error {
	_, err := db.Execute(ctx, q, `
		INSERT INTO areas (id, did, label, lat, lon, radius_m, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		a.ID, a.DID, a.Label, a.Lat, a.Lon, a.RadiusM, clock.UnixMilli(a.CreatedAt))
	return err
}

func GetArea(ctx context.Context, q db.Querier, id string) (*Area, error) {
	return db.GetItem(ctx, q,
		"SELECT "+areaColumns+" FROM areas WHERE id = ?",
		[]any{id}, scanArea)
}

// ListAreas は did のエリアを作成順に返す。
func ListAreas(ctx context.Context, q db.Querier, did string) ([]Area, error) {
	return db.GetItems(ctx, q, `
		SELECT `+areaColumns+` FROM areas
		WHERE did = ?
		ORDER BY created_at ASC, id ASC`,
		[]any{did}, scanArea)
}

func CountAreas(ctx context.Context, q db.Querier, did string) (int, error) {
	var n int
	err := q.QueryRowContext(ctx,
		"SELECT count(*) FROM areas WHERE did = ?", did).Scan(&n)
	return n, err
}

// AreaPatch は PATCH で来た項目だけを持つ。nil の項目は更新しない。
type AreaPatch struct {
	Label   *string
	Lat     *float64
	Lon     *float64
	RadiusM *int
}

// UpdateArea は patch に含まれる列を更新する。
func UpdateArea(ctx context.Context, q db.Querier, id string, patch AreaPatch) error {
	sets := make([]string, 0, 4)
	args := make([]any, 0, 5)
	if patch.Label != nil {
		sets = append(sets, "label = ?")
		args = append(args, *patch.Label)
	}
	if patch.Lat != nil {
		sets = append(sets, "lat = ?")
		args = append(args, *patch.Lat)
	}
	if patch.Lon != nil {
		sets = append(sets, "lon = ?")
		args = append(args, *patch.Lon)
	}
	if patch.RadiusM != nil {
		sets = append(sets, "radius_m = ?")
		args = append(args, *patch.RadiusM)
	}
	if len(sets) == 0 {
		return nil
	}
	args = append(args, id)

	_, err := db.Execute(ctx, q,
		"UPDATE areas SET "+strings.Join(sets, ", ")+" WHERE id = ?", args...)
	return err
}

// DeleteArea は行を消す。存在しなければ false。
func DeleteArea(ctx context.Context, q db.Querier, id string) (bool, error) {
	res, err := db.Execute(ctx, q, "DELETE FROM areas WHERE id = ?", id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}
