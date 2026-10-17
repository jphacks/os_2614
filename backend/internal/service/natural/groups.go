package natural

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/applemango/chain/internal/db"
	"github.com/applemango/chain/internal/kv"
	"github.com/applemango/chain/internal/model"
	groupmodel "github.com/applemango/chain/internal/model/group"
	schedulemodel "github.com/applemango/chain/internal/model/schedule"
	spacemodel "github.com/applemango/chain/internal/model/space"
	"github.com/applemango/chain/internal/realtime"
	"github.com/applemango/chain/internal/service/core"
	groupsvc "github.com/applemango/chain/internal/service/group"
	"github.com/applemango/chain/internal/service/moderation"
	"github.com/applemango/chain/internal/service/notify"
	"github.com/applemango/chain/internal/service/proximity"
	"github.com/applemango/chain/internal/service/schedule"
)

// naturalGroupFallbackName は名前が moderation で block になったときの名前（09 §8.5）。
const naturalGroupFallbackName = "近くのグループ"

// NaturalGroupService は自動グループ（09 §8）の照合・作成・自動参加・終了。
type NaturalGroupService struct {
	core.Deps
	Moderator moderation.Moderator
	Broker    realtime.Broker
}

// naturalMember は照合の対象になる 1 人と、その人の「今の予定」。
type naturalMember struct {
	DID      string
	Lat, Lon float64
	Entry    schedulemodel.MatchEntry
}

// naturalComponent は 3 人以上の連結成分（09 §8.4）。
type naturalComponent struct {
	Members          []naturalMember
	Lat, Lon         float64
	Rep              schedulemodel.MatchEntry
	StartMin, EndMin int
	Date             string
}

// naturalCandidate は ng:cand:* の値（09 §8.4）。続けて見つかった回数を持つ。
type naturalCandidate struct {
	Count     int   `json:"count"`
	UpdatedAt int64 `json:"updatedAt"`
}

// autoJoinedPayload は group.auto_joined の payload（12 §3）。遷移先は Space。
type autoJoinedPayload struct {
	Text    string `json:"text"`
	SpaceID string `json:"spaceId"`
}

// Tick は 5 分ごとの照合を 1 回行う（09 §8.4〜§8.5）。時刻は Deps.Clock から取る。
func (s *NaturalGroupService) Tick(ctx context.Context) error {
	now := s.Clock.Now()
	t := now.In(core.Jst)
	date := t.Format(schedule.DateLayout)
	nowMin := t.Hour()*60 + t.Minute()
	leadMin := int(core.NaturalLeadTime / time.Minute)

	if err := s.archiveEnded(ctx, now); err != nil {
		return err
	}

	entries, err := schedulemodel.ListNaturalMatchCandidates(ctx, s.DB, date, nowMin, leadMin)
	if err != nil {
		return err
	}
	members, err := s.freshMembers(ctx, entries, now)
	if err != nil {
		return err
	}

	existing, err := s.candidateKeys(ctx)
	if err != nil {
		return err
	}
	touched := map[string]bool{}

	points := make([]geoPoint, len(members))
	for i, m := range members {
		points[i] = geoPoint{Lat: m.Lat, Lon: m.Lon}
	}
	for _, cluster := range dbscan(points, core.NaturalClusterEps, core.NaturalMinSize) {
		similar := func(i, j int) bool {
			return entriesSimilar(members[cluster[i]].Entry, members[cluster[j]].Entry)
		}
		for _, group := range naturalComponents(len(cluster), similar) {
			if len(group) < core.NaturalMinSize {
				continue
			}
			idxs := make([]int, len(group))
			for k, c := range group {
				idxs[k] = cluster[c]
			}
			comp := buildComponent(idxs, members, date)
			merged, err := s.tryMerge(ctx, comp, now)
			if err != nil {
				return err
			}
			if merged {
				continue
			}
			key := candidateKey(comp)
			touched[key] = true
			count, err := s.bumpCandidate(ctx, key, now)
			if err != nil {
				return err
			}
			if count >= core.NaturalConfirmTicks {
				if err := s.createGroup(ctx, comp, now); err != nil {
					return err
				}
				if err := s.Redis.Del(ctx, key).Err(); err != nil {
					return err
				}
			}
		}
	}

	if err := s.joinArrivals(ctx, members, date, now); err != nil {
		return err
	}

	for key := range existing {
		if touched[key] {
			continue
		}
		if err := s.Redis.Del(ctx, key).Err(); err != nil {
			return err
		}
	}
	return nil
}

// freshMembers は予定を持つ人の最新位置を引き、鮮度が core.NaturalLocationFresh 以内のものだけ返す（09 §8.4）。
func (s *NaturalGroupService) freshMembers(ctx context.Context, entries []schedulemodel.MatchEntry, now time.Time) ([]naturalMember, error) {
	byDID := map[string][]schedulemodel.MatchEntry{}
	order := make([]string, 0, len(entries))
	for _, e := range entries {
		if _, ok := byDID[e.DID]; !ok {
			order = append(order, e.DID)
		}
		byDID[e.DID] = append(byDID[e.DID], e)
	}
	out := make([]naturalMember, 0, len(order))
	for _, did := range order {
		loc, err := model.GetLocation(ctx, s.Redis, did)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if now.Sub(loc.RecordedAt) > core.NaturalLocationFresh {
			continue
		}
		out = append(out, naturalMember{DID: did, Lat: loc.Lat, Lon: loc.Lon, Entry: representativeEntry(byDID[did])})
	}
	return out, nil
}

// buildComponent は塊の添字から成分（重心・代表・時間帯）を作る。
func buildComponent(idxs []int, members []naturalMember, date string) naturalComponent {
	comp := naturalComponent{Date: date}
	var latSum, lonSum float64
	for i, idx := range idxs {
		m := members[idx]
		comp.Members = append(comp.Members, m)
		latSum += m.Lat
		lonSum += m.Lon
		if i == 0 || m.Entry.StartMin < comp.StartMin {
			comp.StartMin = m.Entry.StartMin
		}
		if i == 0 || m.Entry.EndMin > comp.EndMin {
			comp.EndMin = m.Entry.EndMin
		}
	}
	n := float64(len(comp.Members))
	comp.Lat, comp.Lon = latSum/n, lonSum/n
	vecs := make([][]float32, len(comp.Members))
	for i, m := range comp.Members {
		vecs[i] = m.Entry.Embedding
	}
	if mi := medoidIndex(vecs); mi >= 0 {
		comp.Rep = comp.Members[mi].Entry
	}
	return comp
}

// tryMerge は成分を既存の active な open グループへ合流させる（09 §8.5）。合流できたら true。
func (s *NaturalGroupService) tryMerge(ctx context.Context, comp naturalComponent, now time.Time) (bool, error) {
	if len(comp.Rep.Embedding) == 0 {
		return false, nil
	}
	groups, err := groupmodel.ListActiveGroups(ctx, s.DB)
	if err != nil {
		return false, err
	}
	for _, g := range groups {
		if g.Visibility != "open" {
			continue
		}
		if proximity.Haversine(comp.Lat, comp.Lon, g.CenterLat, g.CenterLon) > float64(g.RadiusM) {
			continue
		}
		if !schedule.ScheduleOverlapsRange(g.ScheduleJSON, comp.Date, comp.StartMin, comp.EndMin) {
			continue
		}
		rep, ok, err := s.groupRepresentative(ctx, g.SpaceID, comp.Date)
		if err != nil {
			return false, err
		}
		if !ok || !entriesSimilar(comp.Rep, rep) {
			continue
		}
		return true, s.joinMembers(ctx, g.SpaceID, comp.Members, now)
	}
	return false, nil
}

// joinArrivals は既存の自動グループの geofence の中にいて予定が合う人を自動参加させる（09 §8.5）。
func (s *NaturalGroupService) joinArrivals(ctx context.Context, members []naturalMember, date string, now time.Time) error {
	groups, err := groupmodel.ListActiveGroups(ctx, s.DB)
	if err != nil {
		return err
	}
	for _, g := range groups {
		if g.Origin != "natural" || g.Visibility != "open" {
			continue
		}
		rep, ok, err := s.groupRepresentative(ctx, g.SpaceID, date)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		var join []naturalMember
		for _, m := range members {
			if proximity.Haversine(m.Lat, m.Lon, g.CenterLat, g.CenterLon) > float64(g.RadiusM) {
				continue
			}
			if entriesSimilar(m.Entry, rep) {
				join = append(join, m)
			}
		}
		if len(join) == 0 {
			continue
		}
		if err := s.joinMembers(ctx, g.SpaceID, join, now); err != nil {
			return err
		}
	}
	return nil
}

// joinMembers は members のうち、まだメンバーでなく自分から退出していない人を member にする（09 §8.5）。
func (s *NaturalGroupService) joinMembers(ctx context.Context, spaceID string, members []naturalMember, now time.Time) error {
	var notes []notify.NotificationToSend
	var joined []string
	err := db.Transaction(ctx, s.DB, func(tx *sql.Tx) error {
		var name string
		if err := tx.QueryRowContext(ctx, "SELECT name FROM spaces WHERE id = ?", spaceID).Scan(&name); err != nil {
			return err
		}
		for _, m := range members {
			ok, err := canAutoJoin(ctx, tx, spaceID, m.DID)
			if err != nil {
				return err
			}
			if !ok {
				continue
			}
			if err := spacemodel.InsertSpaceMember(ctx, tx, spacemodel.SpaceMember{
				SpaceID: spaceID, DID: m.DID, CanRead: true, CanWrite: true, Role: "member", JoinedAt: now,
			}); err != nil {
				return err
			}
			item, err := notify.EnqueueNotification(ctx, tx, s.Deps, m.DID, notify.NotificationGroupAutoJoined, autoJoinedPayload{
				Text: "『" + name + "』のグループに参加しました", SpaceID: spaceID,
			}, now)
			if err != nil {
				return err
			}
			if item != nil {
				notes = append(notes, notify.NotificationToSend{DID: m.DID, Item: *item})
			}
			joined = append(joined, m.DID)
		}
		return nil
	})
	if err != nil {
		return err
	}
	for _, did := range joined {
		s.publish(ctx, realtime.Event{Name: "member.joined", Space: spaceID, Data: map[string]string{"did": did}})
	}
	s.publishNotes(ctx, notes)
	return nil
}

// canAutoJoin は自動参加してよいかを返す。メンバーの行があれば（現役でも退出済みでも）参加させない（09 §8.5）。
func canAutoJoin(ctx context.Context, q db.Querier, spaceID, did string) (bool, error) {
	_, err := spacemodel.GetSpaceMember(ctx, q, spaceID, did)
	if errors.Is(err, sql.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return false, nil
}

// createGroup は成分から新しい自動グループを作り、全員を member にする（09 §8.5）。
func (s *NaturalGroupService) createGroup(ctx context.Context, comp naturalComponent, now time.Time) error {
	if len(comp.Members) < core.NaturalMinSize {
		return nil
	}
	sorted := append([]naturalMember{}, comp.Members...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].DID < sorted[j].DID })

	name, label := s.naturalName(ctx, comp.Rep.Title)
	points := make([]geoPoint, len(sorted))
	for i, m := range sorted {
		points[i] = geoPoint{Lat: m.Lat, Lon: m.Lon}
	}
	lat, lon, radius := naturalGeofence(points)
	schedule := &schedule.GroupSchedule{TZ: "Asia/Tokyo", Dates: []schedule.GroupScheduleDate{{
		Date: comp.Date, Start: clockString(comp.StartMin), End: clockString(comp.EndMin),
	}}}

	id := s.IDs.NewID()
	authority := core.AuthorityFor(s.Config.PublicURL)
	space := spacemodel.Space{
		ID: id, Type: core.SpaceTypeGroup, Authority: authority,
		Ref:        spacemodel.SpaceRef{Authority: authority, Type: core.SpaceTypeGroup, ID: id}.String(),
		ReadPolicy: "managing-app", WritePolicy: "managing-app",
		Name: name, State: "active", CreatedBy: sorted[0].DID, CreatedAt: now,
	}
	group := groupmodel.Group{
		SpaceID: id, Visibility: "open", Origin: "natural",
		CenterLat: lat, CenterLon: lon, RadiusM: radius,
		ScheduleJSON: groupsvc.ScheduleJSON(schedule), InviteQuorum: 1, ModLabel: label,
	}

	all, err := groupmodel.ListActiveGroups(ctx, s.DB)
	if err != nil {
		return err
	}

	var notes []notify.NotificationToSend
	if err := db.Transaction(ctx, s.DB, func(tx *sql.Tx) error {
		if err := spacemodel.InsertSpace(ctx, tx, space); err != nil {
			return err
		}
		if err := groupmodel.InsertGroup(ctx, tx, group); err != nil {
			return err
		}
		for _, m := range sorted {
			if err := spacemodel.InsertSpaceMember(ctx, tx, spacemodel.SpaceMember{
				SpaceID: id, DID: m.DID, CanRead: true, CanWrite: true, Role: "member", JoinedAt: now,
			}); err != nil {
				return err
			}
			item, err := notify.EnqueueNotification(ctx, tx, s.Deps, m.DID, notify.NotificationGroupAutoJoined, autoJoinedPayload{
				Text: "『" + name + "』のグループに参加しました", SpaceID: id,
			}, now)
			if err != nil {
				return err
			}
			if item != nil {
				notes = append(notes, notify.NotificationToSend{DID: m.DID, Item: *item})
			}
		}
		_, err := groupsvc.AssignGroupParent(ctx, tx, id, group, all)
		return err
	}); err != nil {
		return err
	}

	if err := s.Redis.GeoAdd(ctx, kv.GeoGroupsKey(), &redis.GeoLocation{Name: id, Longitude: lon, Latitude: lat}).Err(); err != nil {
		return err
	}
	for _, m := range sorted {
		s.publish(ctx, realtime.Event{Name: "member.joined", Space: id, Data: map[string]string{"did": m.DID}})
	}
	s.publishNotes(ctx, notes)
	return nil
}

// naturalName は medoid のタイトルを 40 文字に切り詰め、moderation にかける（09 §8.5）。
// block なら「近くのグループ」にする。
func (s *NaturalGroupService) naturalName(ctx context.Context, title string) (string, string) {
	name := truncateRunes(strings.TrimSpace(title), 40)
	if name == "" {
		name = naturalGroupFallbackName
	}
	if s.Moderator == nil {
		return name, string(moderation.LabelOK)
	}
	v, err := s.Moderator.Judge(ctx, moderation.Content{Kind: "group_name", Text: name})
	if err != nil {
		slog.WarnContext(ctx, "moderation error; treating as ok", "err", err)
		return name, string(moderation.LabelOK)
	}
	if v.Label == moderation.LabelBlock {
		return naturalGroupFallbackName, string(moderation.LabelBlock)
	}
	return name, string(v.Label)
}

// groupRepresentative はグループの代表の予定を、現役メンバーの今の予定の medoid として求める（09 §8.5）。
func (s *NaturalGroupService) groupRepresentative(ctx context.Context, spaceID, date string) (schedulemodel.MatchEntry, bool, error) {
	members, err := spacemodel.ListActiveSpaceMembers(ctx, s.DB, spaceID)
	if err != nil {
		return schedulemodel.MatchEntry{}, false, err
	}
	dids := make([]string, 0, len(members))
	for _, m := range members {
		dids = append(dids, m.DID)
	}
	entries, err := schedulemodel.ListNaturalMatchEntriesForUsers(ctx, s.DB, dids, date)
	if err != nil {
		return schedulemodel.MatchEntry{}, false, err
	}
	if len(entries) == 0 {
		return schedulemodel.MatchEntry{}, false, nil
	}
	byDID := map[string][]schedulemodel.MatchEntry{}
	order := make([]string, 0, len(entries))
	for _, e := range entries {
		if _, ok := byDID[e.DID]; !ok {
			order = append(order, e.DID)
		}
		byDID[e.DID] = append(byDID[e.DID], e)
	}
	reps := make([]schedulemodel.MatchEntry, 0, len(order))
	for _, did := range order {
		reps = append(reps, representativeEntry(byDID[did]))
	}
	vecs := make([][]float32, len(reps))
	for i := range reps {
		vecs[i] = reps[i].Embedding
	}
	mi := medoidIndex(vecs)
	if mi < 0 {
		return schedulemodel.MatchEntry{}, false, nil
	}
	return reps[mi], true, nil
}

// archiveEnded は時間帯の終了から core.NaturalEndGrace たって空になった自動グループを archived にする（09 §8.5）。
func (s *NaturalGroupService) archiveEnded(ctx context.Context, now time.Time) error {
	groups, err := groupmodel.ListActiveGroups(ctx, s.DB)
	if err != nil {
		return err
	}
	for _, g := range groups {
		if g.Origin != "natural" || g.Persistent {
			continue
		}
		end, ok := schedule.ScheduleLatestEnd(g.ScheduleJSON, now)
		if !ok || now.Before(end.Add(core.NaturalEndGrace)) {
			continue
		}
		occupied, err := s.hasMemberInside(ctx, g, now)
		if err != nil {
			return err
		}
		if occupied {
			continue
		}
		archivedAt := now
		if err := spacemodel.SetSpaceState(ctx, s.DB, g.SpaceID, "archived", &archivedAt); err != nil {
			return err
		}
		if err := s.Redis.ZRem(ctx, kv.GeoGroupsKey(), g.SpaceID).Err(); err != nil {
			return err
		}
		s.publish(ctx, realtime.Event{Name: "space.archived", Space: g.SpaceID})
	}
	return nil
}

// hasMemberInside は geofence の中に新しい位置を持つメンバーがいるかを返す（09 §8.5）。
func (s *NaturalGroupService) hasMemberInside(ctx context.Context, g groupmodel.GroupWithSpace, now time.Time) (bool, error) {
	members, err := spacemodel.ListActiveSpaceMembers(ctx, s.DB, g.SpaceID)
	if err != nil {
		return false, err
	}
	for _, m := range members {
		loc, err := model.GetLocation(ctx, s.Redis, m.DID)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return false, err
		}
		if now.Sub(loc.RecordedAt) > core.NaturalLocationFresh {
			continue
		}
		if proximity.Haversine(g.CenterLat, g.CenterLon, loc.Lat, loc.Lon) <= float64(g.RadiusM) {
			return true, nil
		}
	}
	return false, nil
}

// bumpCandidate は候補の回数を 1 増やして保存し、新しい回数を返す（09 §8.4）。
func (s *NaturalGroupService) bumpCandidate(ctx context.Context, key string, now time.Time) (int, error) {
	count := 0
	if v, err := s.Redis.Get(ctx, key).Result(); err == nil {
		var st naturalCandidate
		if json.Unmarshal([]byte(v), &st) == nil {
			count = st.Count
		}
	} else if !errors.Is(err, redis.Nil) {
		return 0, err
	}
	count++
	b, err := json.Marshal(naturalCandidate{Count: count, UpdatedAt: now.UnixMilli()})
	if err != nil {
		return 0, err
	}
	if err := s.Redis.Set(ctx, key, b, core.NaturalCandidateTTL).Err(); err != nil {
		return 0, err
	}
	return count, nil
}

// candidateKeys は今ある ng:cand:* を全部返す（見つからなかった候補を消すため。09 §8.4）。
func (s *NaturalGroupService) candidateKeys(ctx context.Context) (map[string]bool, error) {
	out := map[string]bool{}
	var cursor uint64
	for {
		keys, next, err := s.Redis.Scan(ctx, cursor, kv.NaturalCandidatePrefix()+"*", 100).Result()
		if err != nil {
			return nil, err
		}
		for _, k := range keys {
			out[k] = true
		}
		cursor = next
		if cursor == 0 {
			break
		}
	}
	return out, nil
}

func (s *NaturalGroupService) publish(ctx context.Context, e realtime.Event) {
	if s.Broker != nil {
		s.Broker.Publish(ctx, e)
	}
}

func (s *NaturalGroupService) publishNotes(ctx context.Context, notes []notify.NotificationToSend) {
	if s.Broker == nil {
		return
	}
	for _, n := range notes {
		s.Broker.Publish(ctx, realtime.Event{Name: "notification", User: n.DID, Data: n.Item})
	}
}

// representativeEntry は 1 人の「今の予定」を 1 件選ぶ。開始が最も早いもの、同じなら ID 順。
func representativeEntry(entries []schedulemodel.MatchEntry) schedulemodel.MatchEntry {
	best := entries[0]
	for _, e := range entries[1:] {
		if e.StartMin < best.StartMin || (e.StartMin == best.StartMin && e.ID < best.ID) {
			best = e
		}
	}
	return best
}

// entriesSimilar は 2 つの予定が cos ≥ core.NaturalSimilarity かつ重なり ≥ core.NaturalOverlap かを返す（09 §8.4）。
func entriesSimilar(a, b schedulemodel.MatchEntry) bool {
	if len(a.Embedding) == 0 || len(b.Embedding) == 0 {
		return false
	}
	if cosineSimilarity(a.Embedding, b.Embedding) < core.NaturalSimilarity {
		return false
	}
	return overlapRatio(a.StartMin, a.EndMin, b.StartMin, b.EndMin) >= core.NaturalOverlap
}

// candidateKey は成分の重心の geohash 7 桁と時間帯から ng:cand:* のキーを作る（09 §8.4）。
func candidateKey(c naturalComponent) string {
	slot := fmt.Sprintf("%sT%s-%s", c.Date, clockString(c.StartMin), clockString(c.EndMin))
	return kv.NaturalCandidateKey(core.EncodeGeohash(c.Lat, c.Lon, 7), slot)
}

func clockString(minutes int) string {
	return fmt.Sprintf("%02d:%02d", minutes/60, minutes%60)
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
