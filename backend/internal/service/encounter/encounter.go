package encounter

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/applemango/chain/internal/db"
	"github.com/applemango/chain/internal/httpx"
	"github.com/applemango/chain/internal/model"
	encountermodel "github.com/applemango/chain/internal/model/encounter"
	"github.com/applemango/chain/internal/realtime"
	"github.com/applemango/chain/internal/service/core"
	"github.com/applemango/chain/internal/service/proximity"
)

// EncounterService はすれ違い（06）。
type EncounterService struct {
	core.Deps
	Proximity proximity.PeerProximity
	Broker    realtime.Broker
}

type SightingInput struct {
	Token       string
	FirstSeenAt time.Time
	LastSeenAt  time.Time
	MaxRssi     *int
	Count       int
}

type SightingResult struct {
	Accepted int `json:"accepted"`
	Dropped  int `json:"dropped"`
}

const (
	encounterLockTTL = 5 * time.Second
	revealBatch      = 500
)

type sightingBounds struct {
	first time.Time
	last  time.Time
	rssi  *int
}

// RecordSightings は sighting を受け取り、token を解決して観測を記録し、双方向が揃えば encounter を作る（06 §2・§3）。
// 返すのは accepted / dropped だけで、誰と成立したかは返さない（06 §2）。
func (s *EncounterService) RecordSightings(ctx context.Context, me string, items []SightingInput) (SightingResult, error) {
	if len(items) > core.SightingMax {
		return SightingResult{}, httpx.ErrInvalidRequest.With("fields", map[string]string{
			"items": "1 リクエストは 500 件までです",
		})
	}
	now := s.Clock.Now()
	groups := map[string]*sightingBounds{}
	accepted, dropped := 0, 0

	for _, it := range items {
		if it.LastSeenAt.Before(now.Add(-core.SightingMaxAge)) {
			dropped++
			continue
		}
		peer, ok, err := proximity.ResolveBeaconToken(ctx, s.Deps, it.Token, it.FirstSeenAt, it.LastSeenAt)
		if err != nil {
			return SightingResult{}, err
		}
		if !ok || peer == me {
			dropped++
			continue
		}
		if it.MaxRssi == nil || *it.MaxRssi < core.RssiMin {
			dropped++
			continue
		}
		allowed, err := s.peerAllowed(ctx, me, peer)
		if err != nil {
			return SightingResult{}, err
		}
		if !allowed {
			dropped++
			continue
		}

		g := groups[peer]
		if g == nil {
			g = &sightingBounds{first: it.FirstSeenAt, last: it.LastSeenAt}
			groups[peer] = g
		}
		if it.FirstSeenAt.Before(g.first) {
			g.first = it.FirstSeenAt
		}
		if it.LastSeenAt.After(g.last) {
			g.last = it.LastSeenAt
		}
		if g.rssi == nil || *it.MaxRssi > *g.rssi {
			v := *it.MaxRssi
			g.rssi = &v
		}
		accepted++
	}

	peers := make([]string, 0, len(groups))
	for peer := range groups {
		peers = append(peers, peer)
	}
	sort.Strings(peers)
	for _, peer := range peers {
		if err := s.processPeer(ctx, me, peer, groups[peer]); err != nil {
			return SightingResult{}, err
		}
	}
	return SightingResult{Accepted: accepted, Dropped: dropped}, nil
}

func (s *EncounterService) processPeer(ctx context.Context, me, peer string, g *sightingBounds) error {
	if err := s.Proximity.Observe(ctx, me, peer, g.first, g.last, g.rssi); err != nil {
		return err
	}
	mutual, err := s.Proximity.Mutual(ctx, me, peer, g.last, core.MatchWindow)
	if err != nil {
		return err
	}
	if !mutual {
		return nil
	}
	return s.establish(ctx, me, peer, g.first, g.last, g.rssi)
}

func (s *EncounterService) peerAllowed(ctx context.Context, me, peer string) (bool, error) {
	u, err := model.GetUser(ctx, s.DB, peer)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if u.DeletedAt != nil {
		return false, nil
	}
	blocked, err := model.IsBlocked(ctx, s.DB, me, peer)
	if err != nil {
		return false, err
	}
	return !blocked, nil
}

func (s *EncounterService) establish(ctx context.Context, me, peer string, first, last time.Time, rssi *int) error {
	a, b := core.OrderDIDs(me, peer)
	lockName := "enc:" + a + ":" + b
	got, err := model.AcquireLock(ctx, s.Redis, lockName, encounterLockTTL)
	if err != nil {
		return err
	}
	if !got {
		return nil
	}
	defer func() { _ = model.ReleaseLock(context.WithoutCancel(ctx), s.Redis, lockName) }()

	now := s.Clock.Now()
	existing, err := encountermodel.GetEncounterForPair(ctx, s.DB, a, b, first, last, core.MergeGap)
	switch {
	case err == nil:
		return s.extend(ctx, existing, first, last, rssi, now)
	case !errors.Is(err, sql.ErrNoRows):
		return err
	}

	from, to := core.JstDayBounds(now)
	n, err := encountermodel.CountEncountersForUserBetween(ctx, s.DB, me, from, to)
	if err != nil {
		return err
	}
	if n >= core.DailyEncounterCap {
		return nil
	}
	return s.create(ctx, me, a, b, first, last, rssi, now)
}

func (s *EncounterService) create(ctx context.Context, reporter, a, b string, first, last time.Time, rssi *int, now time.Time) error {
	sa, err := s.getSettings(ctx, a, now)
	if err != nil {
		return err
	}
	sb, err := s.getSettings(ctx, b, now)
	if err != nil {
		return err
	}
	visibility, revealAt := resolveReveal(sa.EncounterMode, sa.EncounterDelaySec, sb.EncounterMode, sb.EncounterDelaySec, now, last)
	var geohash string
	if sa.RecordEncounterPlace && sb.RecordEncounterPlace {
		geohash = s.encounterPlace(ctx, reporter, now)
	}
	return encountermodel.CreateEncounter(ctx, s.DB, encountermodel.Encounter{
		ID: s.IDs.NewID(), UserA: a, UserB: b,
		EstablishedAt: now, FirstSeenAt: first, LastSeenAt: last,
		MaxRSSI: rssi, Visibility: visibility, RevealAt: revealAt, Geohash6: geohash,
	})
}

// encounterPlace は報告側の最新位置を geohash 6 桁に丸めて返す（06 §7）。
// 位置がない・古い場合は空文字（場所なしで成立させる）。
func (s *EncounterService) encounterPlace(ctx context.Context, did string, now time.Time) string {
	loc, err := model.GetLocation(ctx, s.Redis, did)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			slog.WarnContext(ctx, "encounter: load location failed", "did", did, "err", err)
		}
		return ""
	}
	if now.Sub(loc.RecordedAt) > core.EncounterPlaceFresh {
		return ""
	}
	return core.EncodeGeohash(loc.Lat, loc.Lon, core.EncounterPlaceLength)
}

func (s *EncounterService) extend(ctx context.Context, e *encountermodel.Encounter, first, last time.Time, rssi *int, now time.Time) error {
	firstSeen, lastSeen := e.FirstSeenAt, e.LastSeenAt
	if first.Before(firstSeen) {
		firstSeen = first
	}
	if last.After(lastSeen) {
		lastSeen = last
	}
	maxRSSI := e.MaxRSSI
	if rssi != nil && (maxRSSI == nil || *rssi > *maxRSSI) {
		v := *rssi
		maxRSSI = &v
	}
	sa, err := s.getSettings(ctx, e.UserA, now)
	if err != nil {
		return err
	}
	sb, err := s.getSettings(ctx, e.UserB, now)
	if err != nil {
		return err
	}
	visibility, revealAt := extendReveal(e.Visibility, e.RevealAt, sa.EncounterMode, sa.EncounterDelaySec, sb.EncounterMode, sb.EncounterDelaySec, lastSeen)
	return encountermodel.UpdateEncounterSeen(ctx, s.DB, e.ID, firstSeen, lastSeen, maxRSSI, visibility, revealAt)
}

func (s *EncounterService) getSettings(ctx context.Context, did string, now time.Time) (*model.UserSettings, error) {
	if err := model.EnsureSettings(ctx, s.DB, did, now); err != nil {
		return nil, err
	}
	return model.GetSettings(ctx, s.DB, did)
}

// RevealPending は公開時刻に達した pending の encounter を revealed にする（06 §5.4）。
func (s *EncounterService) RevealPending(ctx context.Context) (int, error) {
	now := s.Clock.Now()
	var ids []string
	err := db.Transaction(ctx, s.DB, func(tx *sql.Tx) error {
		due, err := encountermodel.ListDueRevealIDs(ctx, tx, now, revealBatch)
		if err != nil {
			return err
		}
		for _, id := range due {
			if err := encountermodel.MarkEncounterRevealed(ctx, tx, id); err != nil {
				return err
			}
		}
		ids = due
		return nil
	})
	if err != nil {
		return 0, err
	}
	// コミットの後に、両者それぞれに「その人から見た」1 件を送る（06 §5.4、10 §7.3）。
	for _, id := range ids {
		e, err := encountermodel.GetEncounter(ctx, s.DB, id)
		if err != nil {
			slog.ErrorContext(ctx, "encounter.revealed: load encounter failed", "id", id, "err", err)
			continue
		}
		for _, viewer := range []string{e.UserA, e.UserB} {
			item, err := s.item(ctx, e, viewer)
			if err != nil {
				slog.ErrorContext(ctx, "encounter.revealed: build item failed", "id", id, "did", viewer, "err", err)
				continue
			}
			s.publish(ctx, realtime.Event{Name: "encounter.revealed", User: viewer, Data: item})
		}
	}
	return len(ids), nil
}

// EncounterItem は GET /encounters の 1 件（06 §6）。
// WS の encounter.revealed も同じ形を data に載せる（10 §7.3）。
type EncounterItem struct {
	ID            string           `json:"id"`
	Peer          core.UserSummary `json:"peer"`
	SeenFrom      time.Time        `json:"seenFrom"`
	SeenUntil     time.Time        `json:"seenUntil"`
	TimesWithPeer int              `json:"timesWithPeer"`
	Relation      string           `json:"relation"`
}

type EncounterPage struct {
	Items  []EncounterItem `json:"items"`
	Cursor string          `json:"cursor,omitempty"`
}

func (s *EncounterService) List(ctx context.Context, me, cursor string, limit int) (*EncounterPage, error) {
	if limit <= 0 {
		limit = core.DefaultPageLimit
	}
	if limit > core.MaxPageLimit {
		limit = core.MaxPageLimit
	}
	cursorAt, cursorID, err := decodeCursor(cursor)
	if err != nil {
		return nil, httpx.ErrInvalidRequest.With("fields", map[string]string{"cursor": "カーソルが不正です"})
	}

	rows, err := encountermodel.ListRevealedEncounters(ctx, s.DB, me, cursorAt, cursorID, limit+1)
	if err != nil {
		return nil, err
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}

	page := &EncounterPage{Items: make([]EncounterItem, 0, len(rows))}
	for i := range rows {
		item, err := s.item(ctx, &rows[i], me)
		if err != nil {
			return nil, err
		}
		page.Items = append(page.Items, *item)
	}
	if hasMore {
		last := rows[len(rows)-1]
		page.Cursor = encodeCursor(last.EstablishedAt, last.ID)
	}
	return page, nil
}

// item は 1 件を viewer から見た形に組み立てる。一覧と encounter.revealed で同じ処理を使う。
func (s *EncounterService) item(ctx context.Context, e *encountermodel.Encounter, viewer string) (*EncounterItem, error) {
	peerDID := e.UserA
	if peerDID == viewer {
		peerDID = e.UserB
	}
	rel, err := model.GetFriendRelation(ctx, s.DB, viewer, peerDID)
	if err != nil {
		return nil, err
	}
	times, err := encountermodel.CountRevealedWithPeer(ctx, s.DB, e.UserA, e.UserB)
	if err != nil {
		return nil, err
	}
	peer, err := core.FetchUserSummary(ctx, s.DB, peerDID)
	if err != nil {
		return nil, err
	}
	return &EncounterItem{
		ID:            e.ID,
		Peer:          *peer,
		SeenFrom:      core.RoundDown(e.FirstSeenAt),
		SeenUntil:     core.RoundUp(e.LastSeenAt),
		TimesWithPeer: times,
		Relation:      relationString(rel),
	}, nil
}

func (s *EncounterService) publish(ctx context.Context, e realtime.Event) {
	if s.Broker == nil {
		return
	}
	s.Broker.Publish(ctx, e)
}

// ApplyStrictEncounterSettings は厳しくする設定変更を、まだ pending の encounter に即時反映する（06 §5.3）。
func ApplyStrictEncounterSettings(ctx context.Context, deps core.Deps, did string, st *model.UserSettings) error {
	rows, err := encountermodel.ListPendingEncountersForUser(ctx, deps.DB, did)
	if err != nil {
		return err
	}
	now := deps.Clock.Now()
	for _, e := range rows {
		other := e.UserA
		if other == did {
			other = e.UserB
		}
		if err := model.EnsureSettings(ctx, deps.DB, other, now); err != nil {
			return err
		}
		ost, err := model.GetSettings(ctx, deps.DB, other)
		if err != nil {
			return err
		}
		visibility, revealAt := extendReveal("pending", e.RevealAt, st.EncounterMode, st.EncounterDelaySec, ost.EncounterMode, ost.EncounterDelaySec, e.LastSeenAt)
		if visibility == e.Visibility && core.SameTime(revealAt, e.RevealAt) {
			continue
		}
		if err := encountermodel.UpdateEncounterVisibility(ctx, deps.DB, e.ID, visibility, revealAt); err != nil {
			return err
		}
	}
	return nil
}

// IsStricterSettings は公開を遅らせる / private にする変更かを返す（06 §5.3）。
func IsStricterSettings(old, next *model.UserSettings) bool {
	if next.EncounterMode == "private" && old.EncounterMode != "private" {
		return true
	}
	if old.EncounterMode == "private" || next.EncounterMode == "private" {
		return false
	}
	return delayOf(next.EncounterMode, next.EncounterDelaySec) > delayOf(old.EncounterMode, old.EncounterDelaySec)
}

func delayOf(mode string, sec int) time.Duration {
	if mode == "immediate" || mode == "private" {
		return 0
	}
	return time.Duration(sec) * time.Second
}

// resolveReveal は新しい encounter の公開設定を決める（06 §5.2）。
func resolveReveal(aMode string, aDelay int, bMode string, bDelay int, established, lastSeen time.Time) (string, *time.Time) {
	if aMode == "private" || bMode == "private" {
		return "hidden", nil
	}
	delay := delayOf(aMode, aDelay)
	if d := delayOf(bMode, bDelay); d > delay {
		delay = d
	}
	if delay <= 0 {
		t := established
		return "pending", &t
	}
	t := lastSeen.Add(delay)
	return "pending", &t
}

// extendReveal は既存 encounter を延長するときの公開設定を再計算する。hidden と revealed は戻さない（06 §5.2・§5.3）。
func extendReveal(existing string, currentReveal *time.Time, aMode string, aDelay int, bMode string, bDelay int, lastSeen time.Time) (string, *time.Time) {
	switch existing {
	case "hidden":
		return "hidden", nil
	case "revealed":
		return "revealed", currentReveal
	}
	if aMode == "private" || bMode == "private" {
		return "hidden", nil
	}
	delay := delayOf(aMode, aDelay)
	if d := delayOf(bMode, bDelay); d > delay {
		delay = d
	}
	if delay <= 0 {
		return "pending", currentReveal
	}
	t := lastSeen.Add(delay)
	if currentReveal != nil && currentReveal.After(t) {
		t = *currentReveal
	}
	return "pending", &t
}

func relationString(r *model.FriendRelation) string {
	switch {
	case r.Friend:
		return "friend"
	case r.Requested:
		return "requested"
	case r.Incoming:
		return "incoming"
	default:
		return "none"
	}
}

func encodeCursor(at time.Time, id string) string {
	raw := strconv.FormatInt(at.UnixMilli(), 10) + ":" + id
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func decodeCursor(cursor string) (time.Time, string, error) {
	if cursor == "" {
		return time.Time{}, "", nil
	}
	b, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return time.Time{}, "", err
	}
	i := strings.LastIndexByte(string(b), ':')
	if i < 0 {
		return time.Time{}, "", errors.New("cursor: missing separator")
	}
	ms, err := strconv.ParseInt(string(b[:i]), 10, 64)
	if err != nil {
		return time.Time{}, "", err
	}
	return time.UnixMilli(ms).UTC(), string(b[i+1:]), nil
}
