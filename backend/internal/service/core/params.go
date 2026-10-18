package core

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

// nearby オーディエンスのパラメータ（17 §2）。今近くにいる人だけに届ける限定投稿に使う。
const (
	// NearbyAudienceWindow は「今近くにいる」とみなす近接の時間窓。
	NearbyAudienceWindow = 15 * time.Minute
	// NearbyPostTTL は nearby 投稿の寿命（expires_at = created_at + NearbyPostTTL）。
	NearbyPostTTL = time.Hour
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
	// EncounterPlaceFresh は場所記録に使う位置の鮮度（06 §7）。
	EncounterPlaceFresh  = 10 * time.Minute
	EncounterPlaceLength = 6
)

// SightingMax は 1 リクエストで受け付ける sighting の上限（06 §2）。
const SightingMax = 500

// 09 §8.6 のパラメータ。
const (
	GroupVoteDuration   = 48 * time.Hour
	GroupVoteListWindow = 7 * 24 * time.Hour
)

// 09 §6 のパラメータ。招待は作成から 7 日で期限切れになる。
const GroupInviteDuration = 7 * 24 * time.Hour

// 08 §7 のパラメータ。
const (
	PresenceInterval    = 15 * time.Second
	EdgeAlive           = 45 * time.Second
	EdgeGapReset        = 60 * time.Second
	TentativeAfter      = 30 * time.Second
	ConfirmAfter        = 5 * time.Minute
	TentativeDissolve   = 60 * time.Second
	LeaveAfter          = 10 * time.Minute
	LockedIdleEnd       = 72 * time.Hour
	MaxDuration         = 7 * 24 * time.Hour
	MaxMembers          = 50
	FriendBeaconHorizon = 6 * time.Hour
)

// 09 §8.7 のパラメータ（自動グループの照合）。
const (
	// NaturalSimilarity は同じ予定とみなす cos 類似度。
	NaturalSimilarity = 0.80
	// NaturalOverlap は時間の重なり（短い方に対する割合）。
	NaturalOverlap = 0.5
	// NaturalClusterEps は位置で塊にする距離（m）。
	NaturalClusterEps = 50.0
	// NaturalMinSize は作成に要る人数。
	NaturalMinSize = 3
	// NaturalConfirmTicks は作成までに続けて見つかる回数（5 分ごとなので 10 分）。
	NaturalConfirmTicks = 3
	// NaturalLocationFresh は照合に使う位置の鮮度。
	NaturalLocationFresh = 5 * time.Minute
	// NaturalLeadTime は予定の開始の何分前から対象にするか。
	NaturalLeadTime = 10 * time.Minute
	// NaturalEndGrace は時間帯の終了から archived までの猶予。
	NaturalEndGrace = 30 * time.Minute
	// NaturalRadiusMin, NaturalRadiusMax は自動グループの半径の範囲（m）。
	NaturalRadiusMin = 30
	NaturalRadiusMax = 300
	// NaturalRadiusMargin は外接円に足す余裕（m）。
	NaturalRadiusMargin = 20.0
	// NaturalCandidateTTL は候補（ng:cand:*）の保持時間。
	NaturalCandidateTTL = 30 * time.Minute
)

var Jst = time.FixedZone("JST", 9*60*60)

// WindowStart は t を含む beacon の窓の開始（unix 秒、900 の倍数）を返す（05 §2.1）。
func WindowStart(t time.Time) int64 {
	return t.Truncate(BeaconWindow).Unix()
}

// RoundDown は TimeRounding の境界まで切り下げる（06 §6）。
func RoundDown(t time.Time) time.Time { return t.Truncate(TimeRounding) }

// RoundUp は TimeRounding の境界まで切り上げる（06 §6）。
func RoundUp(t time.Time) time.Time {
	down := t.Truncate(TimeRounding)
	if down.Equal(t) {
		return down
	}
	return down.Add(TimeRounding)
}

// JstDayBounds は t が属する Asia/Tokyo の暦日の [開始, 翌日開始) を返す。
func JstDayBounds(t time.Time) (time.Time, time.Time) {
	l := t.In(Jst)
	start := time.Date(l.Year(), l.Month(), l.Day(), 0, 0, 0, 0, Jst)
	return start, start.Add(24 * time.Hour)
}

// JstWeekStart は t が属する週の月曜 0:00（Asia/Tokyo）を返す（06 §6、12 §5）。
func JstWeekStart(t time.Time) time.Time {
	l := t.In(Jst)
	offset := (int(l.Weekday()) + 6) % 7 // Monday=0, Sunday=6
	return time.Date(l.Year(), l.Month(), l.Day(), 0, 0, 0, 0, Jst).AddDate(0, 0, -offset)
}

// JstMonthStart は t が属する月の 1 日 0:00（Asia/Tokyo）を返す（13 §3、12 §5）。
func JstMonthStart(t time.Time) time.Time {
	l := t.In(Jst)
	return time.Date(l.Year(), l.Month(), 1, 0, 0, 0, 0, Jst)
}

// OrderDIDs は 2 人の組を user_a < user_b に正規化する（02 §2）。
func OrderDIDs(a, b string) (string, string) {
	if a < b {
		return a, b
	}
	return b, a
}

// SameTime は pointer の時刻が等しいかを返す（nil 同士は等しい）。
func SameTime(a, b *time.Time) bool {
	switch {
	case a == nil && b == nil:
		return true
	case a == nil || b == nil:
		return false
	default:
		return a.Equal(*b)
	}
}
