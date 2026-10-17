package service

import (
	"encoding/json"
	"math"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/applemango/chain/internal/model"
)

// GroupCircle は geofence の円（09 §1）。
type GroupCircle struct {
	Lat     float64
	Lon     float64
	RadiusM int
}

// SimilarGroup は重複の抑制（09 §2）で 409 の details.similar に返す候補。
type SimilarGroup struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// similarIoU は「ほぼ同じ場所」とみなす面積の IoU のしきい値（09 §2）。
const similarIoU = 0.8

// circleIoU は 2 つの円の面積の IoU（共通部分 / 和集合）を解析的に計算する（09 §2）。
func circleIoU(a, b GroupCircle) float64 {
	r1, r2 := float64(a.RadiusM), float64(b.RadiusM)
	d := Haversine(a.Lat, a.Lon, b.Lat, b.Lon)
	inter := circleIntersectionArea(r1, r2, d)
	union := math.Pi*r1*r1 + math.Pi*r2*r2 - inter
	if union <= 0 {
		return 0
	}
	return inter / union
}

// circleIntersectionArea は半径 r1, r2、中心間距離 d の 2 円の共通部分の面積を返す。
func circleIntersectionArea(r1, r2, d float64) float64 {
	if d >= r1+r2 {
		return 0
	}
	if d <= math.Abs(r1-r2) {
		r := math.Min(r1, r2)
		return math.Pi * r * r
	}
	cos1 := clampUnit((d*d + r1*r1 - r2*r2) / (2 * d * r1))
	cos2 := clampUnit((d*d + r2*r2 - r1*r1) / (2 * d * r2))
	term := 0.5 * math.Sqrt(math.Max(0, (-d+r1+r2))*math.Max(0, d+r1-r2)*
		math.Max(0, d-r1+r2)*math.Max(0, d+r1+r2))
	return r1*r1*math.Acos(cos1) + r2*r2*math.Acos(cos2) - term
}

func clampUnit(v float64) float64 {
	if v < -1 {
		return -1
	}
	if v > 1 {
		return 1
	}
	return v
}

// containsCircle は outer が inner を完全に含むか（09 §7 の contains: distance + r_child <= r_parent）。
func containsCircle(outer, inner GroupCircle) bool {
	return Haversine(outer.Lat, outer.Lon, inner.Lat, inner.Lon)+float64(inner.RadiusM) <= float64(outer.RadiusM)
}

type namedCircle struct {
	ID string
	GroupCircle
}

// smallestContainer は target を完全に含む最小（半径が最小）の円の ID を返す。なければ nil。
func smallestContainer(target namedCircle, nodes []namedCircle) *string {
	var best *namedCircle
	for i := range nodes {
		n := &nodes[i]
		if n.ID == target.ID || !containsCircle(n.GroupCircle, target.GroupCircle) {
			continue
		}
		if best == nil || n.RadiusM < best.RadiusM || (n.RadiusM == best.RadiusM && n.ID < best.ID) {
			best = n
		}
	}
	if best == nil {
		return nil
	}
	id := best.ID
	return &id
}

// planParents は target を加えたときの parent の割り当てを決める（09 §2）。
// target 自身の parent と、target が含むために parent が変わる既存グループの更新を返す。
func planParents(target namedCircle, existing []namedCircle, existingParents map[string]*string) (*string, map[string]*string) {
	nodes := make([]namedCircle, 0, len(existing)+1)
	nodes = append(nodes, existing...)
	nodes = append(nodes, target)

	parent := smallestContainer(target, nodes)
	updates := map[string]*string{}
	for _, g := range existing {
		if parent != nil && g.ID == *parent {
			continue
		}
		if !containsCircle(target.GroupCircle, g.GroupCircle) {
			continue
		}
		np := smallestContainer(g, nodes)
		if sameStringPtr(np, existingParents[g.ID]) {
			continue
		}
		updates[g.ID] = np
	}
	return parent, updates
}

// schedulesOverlap は 2 つの schedule の時間帯が重なるかを返す（09 §2）。
// schedule がない（常時有効）ものは何とでも重なる。
func schedulesOverlap(a *GroupSchedule, bRaw *string) bool {
	aSlots, aAlways := scheduleSlots(a)
	bSlots, bAlways := rawScheduleSlots(bRaw)
	if aAlways || bAlways {
		return true
	}
	for _, x := range aSlots {
		xs, xe, okx := slotBounds(x)
		if !okx {
			continue
		}
		for _, y := range bSlots {
			if x.Weekday != y.Weekday {
				continue
			}
			ys, ye, oky := slotBounds(y)
			if !oky {
				continue
			}
			if xs < ye && ys < xe {
				return true
			}
		}
	}
	return false
}

func slotBounds(s GroupScheduleSlot) (int, int, bool) {
	start, ok1 := parseClock(s.Start)
	end, ok2 := parseClock(s.End)
	return start, end, ok1 && ok2
}

// scheduleSlots は入力 schedule の slot を返す。指定がなければ (nil, true)（常時有効）。
func scheduleSlots(a *GroupSchedule) ([]GroupScheduleSlot, bool) {
	if a == nil || len(a.Slots) == 0 {
		return nil, true
	}
	return a.Slots, false
}

// rawScheduleSlots は保存済みの schedule_json の slot を返す。指定がなければ (nil, true)。
func rawScheduleSlots(raw *string) ([]GroupScheduleSlot, bool) {
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return nil, true
	}
	var s GroupSchedule
	if err := json.Unmarshal([]byte(*raw), &s); err != nil || len(s.Slots) == 0 {
		return nil, true
	}
	return s.Slots, false
}

// validateGroupSchedule は 09 §3 の規則を確かめる。
func validateGroupSchedule(s *GroupSchedule) map[string]string {
	fields := map[string]string{}
	if s == nil {
		return fields
	}
	if len(s.Slots) == 0 {
		fields["schedule.slots"] = "1 件以上必要です"
	}
	if len(s.Slots) > 14 {
		fields["schedule.slots"] = "14 件までです"
	}
	if s.TZ == "" {
		fields["schedule.tz"] = "IANA のタイムゾーン名が必要です"
	} else if _, err := time.LoadLocation(s.TZ); err != nil {
		fields["schedule.tz"] = "IANA のタイムゾーン名で指定してください"
	}
	for i, slot := range s.Slots {
		key := "schedule.slots." + strconv.Itoa(i)
		if slot.Weekday < 1 || slot.Weekday > 7 {
			fields[key+".weekday"] = "1〜7 で指定してください"
		}
		start, ok1 := parseClock(slot.Start)
		end, ok2 := parseClock(slot.End)
		if !ok1 {
			fields[key+".start"] = "HH:MM で指定してください"
		}
		if !ok2 {
			fields[key+".end"] = "HH:MM で指定してください"
		}
		if ok1 && ok2 && start >= end {
			fields[key+".start"] = "開始は終了より前にしてください"
		}
	}
	return fields
}

// authorityFor は did:web:{host}（03 §7、seed と同じ形）を作る。
func authorityFor(publicURL string) string {
	if u, err := url.Parse(publicURL); err == nil && u.Host != "" {
		return "did:web:" + u.Host
	}
	return "did:web:" + strings.TrimPrefix(strings.TrimPrefix(publicURL, "https://"), "http://")
}

// findSimilar は active な open グループのうち、IoU が 0.8 以上かつ schedule が重なるものを返す（09 §2）。
func findSimilar(center GroupCircle, schedule *GroupSchedule, groups []model.GroupWithSpace) []SimilarGroup {
	var out []SimilarGroup
	for _, g := range groups {
		if g.Visibility != "open" {
			continue
		}
		candidate := GroupCircle{Lat: g.CenterLat, Lon: g.CenterLon, RadiusM: g.RadiusM}
		if circleIoU(center, candidate) < similarIoU {
			continue
		}
		if !schedulesOverlap(schedule, g.ScheduleJSON) {
			continue
		}
		out = append(out, SimilarGroup{ID: g.SpaceID, Name: g.Name})
	}
	return out
}
