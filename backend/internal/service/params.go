package service

import "time"

// 05 §5 のパラメータ。
const (
	BeaconWindow        = 15 * time.Minute
	BeaconClockSkew     = 120 * time.Second
	BeaconIssueMax      = 24 * time.Hour
	BeaconIssueHoursMax = 24
	BeaconWindowSec     = int(BeaconWindow / time.Second)
	SightingBucket      = 10 * time.Minute
	LocationMaxAccuracy = 200.0
	LocationTTL         = 30 * time.Minute
	LocationFresh       = 120 * time.Second
	LocationFutureSkew  = 30 * time.Second
	PresenceGrace       = 10 * time.Minute
	MaxSpeedKmh         = 300.0
)

// 06 §8 のパラメータ。
const (
	MatchWindow       = 10 * time.Minute
	MergeGap          = 30 * time.Minute
	RssiMin           = -100
	SightingMaxAge    = 24 * time.Hour
	DailyEncounterCap = 2000
	SummaryHoldback   = time.Hour
	TimeRounding      = 15 * time.Minute
)

// SightingMax は 1 リクエストで受け付ける sighting の上限（06 §2）。
const SightingMax = 500

var jst = time.FixedZone("JST", 9*60*60)

// windowStart は t を含む beacon の窓の開始（unix 秒、900 の倍数）を返す（05 §2.1）。
func windowStart(t time.Time) int64 {
	return t.Truncate(BeaconWindow).Unix()
}

// roundDown は TimeRounding の境界まで切り下げる（06 §6）。
func roundDown(t time.Time) time.Time { return t.Truncate(TimeRounding) }

// roundUp は TimeRounding の境界まで切り上げる（06 §6）。
func roundUp(t time.Time) time.Time {
	down := t.Truncate(TimeRounding)
	if down.Equal(t) {
		return down
	}
	return down.Add(TimeRounding)
}

// jstDayBounds は t が属する Asia/Tokyo の暦日の [開始, 翌日開始) を返す。
func jstDayBounds(t time.Time) (time.Time, time.Time) {
	l := t.In(jst)
	start := time.Date(l.Year(), l.Month(), l.Day(), 0, 0, 0, 0, jst)
	return start, start.Add(24 * time.Hour)
}

// OrderDIDs は 2 人の組を user_a < user_b に正規化する（02 §2）。
func OrderDIDs(a, b string) (string, string) {
	if a < b {
		return a, b
	}
	return b, a
}

// sameTime は pointer の時刻が等しいかを返す（nil 同士は等しい）。
func sameTime(a, b *time.Time) bool {
	switch {
	case a == nil && b == nil:
		return true
	case a == nil || b == nil:
		return false
	default:
		return a.Equal(*b)
	}
}
