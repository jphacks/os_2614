package model

import (
	"context"
	"database/sql"
	"time"

	"github.com/applemango/chain/internal/clock"
	"github.com/applemango/chain/internal/db"
)

// Encounter は encounters の行（06）。user_a < user_b に正規化されている。
type Encounter struct {
	ID            string
	UserA         string
	UserB         string
	EstablishedAt time.Time
	FirstSeenAt   time.Time
	LastSeenAt    time.Time
	MaxRSSI       *int
	Visibility    string
	RevealAt      *time.Time
	Geohash6      string
}

const encounterColumns = `id, user_a, user_b, established_at, first_seen_at, last_seen_at,
	max_rssi, visibility, reveal_at, geohash6`

func scanEncounter(scan db.Scan, e *Encounter) error {
	var established, first, last int64
	var maxRSSI, revealAt sql.NullInt64
	var geohash sql.NullString
	if err := scan(&e.ID, &e.UserA, &e.UserB, &established, &first, &last,
		&maxRSSI, &e.Visibility, &revealAt, &geohash); err != nil {
		return err
	}
	e.EstablishedAt = clock.FromUnixMilli(established)
	e.FirstSeenAt = clock.FromUnixMilli(first)
	e.LastSeenAt = clock.FromUnixMilli(last)
	if maxRSSI.Valid {
		v := int(maxRSSI.Int64)
		e.MaxRSSI = &v
	}
	if revealAt.Valid {
		t := clock.FromUnixMilli(revealAt.Int64)
		e.RevealAt = &t
	}
	e.Geohash6 = geohash.String
	return nil
}

// GetEncounterForPair は pair の行のうち、観測区間が MergeGap 以内で重なるものを返す（06 §3 step 6）。
// 重なるものが複数あれば最後の観測が新しいものを返す。なければ sql.ErrNoRows。
func GetEncounterForPair(ctx context.Context, q db.Querier, a, b string, first, last time.Time, gap time.Duration) (*Encounter, error) {
	return db.GetItem(ctx, q, `
		SELECT `+encounterColumns+` FROM encounters
		WHERE user_a = ? AND user_b = ?
		  AND last_seen_at >= ? AND first_seen_at <= ?
		ORDER BY last_seen_at DESC LIMIT 1`,
		[]any{a, b, clock.UnixMilli(first.Add(-gap)), clock.UnixMilli(last.Add(gap))}, scanEncounter)
}

func GetEncounter(ctx context.Context, q db.Querier, id string) (*Encounter, error) {
	return db.GetItem(ctx, q,
		"SELECT "+encounterColumns+" FROM encounters WHERE id = ?",
		[]any{id}, scanEncounter)
}

func CreateEncounter(ctx context.Context, q db.Querier, e Encounter) error {
	_, err := db.Execute(ctx, q, `
		INSERT INTO encounters (id, user_a, user_b, established_at, first_seen_at, last_seen_at,
		                        max_rssi, visibility, reveal_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.ID, e.UserA, e.UserB, clock.UnixMilli(e.EstablishedAt), clock.UnixMilli(e.FirstSeenAt),
		clock.UnixMilli(e.LastSeenAt), nullableInt(e.MaxRSSI), e.Visibility, nullableTime(e.RevealAt))
	return err
}

func UpdateEncounterSeen(ctx context.Context, q db.Querier, id string, first, last time.Time, maxRSSI *int, visibility string, revealAt *time.Time) error {
	_, err := db.Execute(ctx, q, `
		UPDATE encounters
		SET first_seen_at = ?, last_seen_at = ?, max_rssi = ?, visibility = ?, reveal_at = ?
		WHERE id = ?`,
		clock.UnixMilli(first), clock.UnixMilli(last), nullableInt(maxRSSI), visibility, nullableTime(revealAt), id)
	return err
}

func UpdateEncounterVisibility(ctx context.Context, q db.Querier, id, visibility string, revealAt *time.Time) error {
	_, err := db.Execute(ctx, q,
		"UPDATE encounters SET visibility = ?, reveal_at = ? WHERE id = ?",
		visibility, nullableTime(revealAt), id)
	return err
}

func CountEncountersForUserBetween(ctx context.Context, q db.Querier, did string, from, to time.Time) (int, error) {
	var n int
	err := q.QueryRowContext(ctx, `
		SELECT count(*) FROM encounters
		WHERE (user_a = ? OR user_b = ?) AND established_at >= ? AND established_at < ?`,
		did, did, clock.UnixMilli(from), clock.UnixMilli(to)).Scan(&n)
	return n, err
}

// EncounterSummaryCounts は summary の集計値（06 §6）。
type EncounterSummaryCounts struct {
	Today        int
	Week         int
	Month        int
	Total        int
	UniquePeople int
}

// CountEncounterSummary は did の encounter を数える（06 §6）。revealed に加えて
// pending / hidden も数え、established_at が cutoff より後のものは数えない。
// todayStart / weekStart / monthStart は Asia/Tokyo の区切りの開始。
func CountEncounterSummary(ctx context.Context, q db.Querier, did string, todayStart, weekStart, monthStart, cutoff time.Time) (EncounterSummaryCounts, error) {
	var c EncounterSummaryCounts
	err := q.QueryRowContext(ctx, `
		SELECT
		  count(*),
		  coalesce(sum(CASE WHEN established_at >= ? THEN 1 ELSE 0 END), 0),
		  coalesce(sum(CASE WHEN established_at >= ? THEN 1 ELSE 0 END), 0),
		  coalesce(sum(CASE WHEN established_at >= ? THEN 1 ELSE 0 END), 0),
		  count(DISTINCT CASE WHEN user_a = ? THEN user_b ELSE user_a END)
		FROM encounters
		WHERE (user_a = ? OR user_b = ?) AND established_at <= ?`,
		clock.UnixMilli(todayStart), clock.UnixMilli(weekStart), clock.UnixMilli(monthStart),
		did, did, did, clock.UnixMilli(cutoff)).
		Scan(&c.Total, &c.Today, &c.Week, &c.Month, &c.UniquePeople)
	return c, err
}

func CountRevealedWithPeer(ctx context.Context, q db.Querier, a, b string) (int, error) {
	var n int
	err := q.QueryRowContext(ctx,
		"SELECT count(*) FROM encounters WHERE user_a = ? AND user_b = ? AND visibility = 'revealed'",
		a, b).Scan(&n)
	return n, err
}

// ListDueRevealIDs は公開時刻に達した pending の encounter を古い順に返す（06 §5.4）。
func ListDueRevealIDs(ctx context.Context, q db.Querier, now time.Time, limit int) ([]string, error) {
	return db.GetItems(ctx, q, `
		SELECT id FROM encounters
		WHERE visibility = 'pending' AND reveal_at IS NOT NULL AND reveal_at <= ?
		ORDER BY reveal_at LIMIT ?`,
		[]any{clock.UnixMilli(now), limit},
		func(scan db.Scan, id *string) error { return scan(id) })
}

func MarkEncounterRevealed(ctx context.Context, q db.Querier, id string) error {
	_, err := db.Execute(ctx, q,
		"UPDATE encounters SET visibility = 'revealed' WHERE id = ? AND visibility = 'pending'", id)
	return err
}

func ListPendingEncountersForUser(ctx context.Context, q db.Querier, did string) ([]Encounter, error) {
	return db.GetItems(ctx, q, `
		SELECT `+encounterColumns+` FROM encounters
		WHERE (user_a = ? OR user_b = ?) AND visibility = 'pending'`,
		[]any{did, did}, scanEncounter)
}

// ListRevealedEncounters は did が関わる revealed の encounter を新しい順に返す。
// cursorID が空なら先頭から。ブロック関係（どちら向きでも）の相手は除く。
func ListRevealedEncounters(ctx context.Context, q db.Querier, did string, cursorAt time.Time, cursorID string, limit int) ([]Encounter, error) {
	query := `
		SELECT ` + encounterColumns + ` FROM encounters
		WHERE (user_a = ? OR user_b = ?) AND visibility = 'revealed'
		  AND NOT EXISTS (
		    SELECT 1 FROM blocks bl
		    WHERE (bl.blocker = encounters.user_a AND bl.blocked = encounters.user_b)
		       OR (bl.blocker = encounters.user_b AND bl.blocked = encounters.user_a))`
	args := []any{did, did}
	if cursorID != "" {
		query += " AND (established_at < ? OR (established_at = ? AND id < ?))"
		ms := clock.UnixMilli(cursorAt)
		args = append(args, ms, ms, cursorID)
	}
	query += " ORDER BY established_at DESC, id DESC LIMIT ?"
	args = append(args, limit)
	return db.GetItems(ctx, q, query, args, scanEncounter)
}

func nullableInt(v *int) any {
	if v == nil {
		return nil
	}
	return *v
}

func nullableTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return clock.UnixMilli(*t)
}
