package group

import (
	"math"

	groupmodel "github.com/applemango/chain/internal/model/group"
	"github.com/applemango/chain/internal/service/proximity"
	"github.com/applemango/chain/internal/service/schedule"
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
	d := proximity.Haversine(a.Lat, a.Lon, b.Lat, b.Lon)
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
	return proximity.Haversine(outer.Lat, outer.Lon, inner.Lat, inner.Lon)+float64(inner.RadiusM) <= float64(outer.RadiusM)
}

// overlapsCircle は 2 つの円が重なるか（09 §7 の overlaps: distance < r_a + r_b）。
func overlapsCircle(a, b GroupCircle) bool {
	return proximity.Haversine(a.Lat, a.Lon, b.Lat, b.Lon) < float64(a.RadiusM)+float64(b.RadiusM)
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

// findSimilar は active な open グループのうち、IoU が 0.8 以上かつ schedule が重なるものを返す（09 §2）。
func findSimilar(center GroupCircle, sched *schedule.GroupSchedule, groups []groupmodel.GroupWithSpace) []SimilarGroup {
	var out []SimilarGroup
	for _, g := range groups {
		if g.Visibility != "open" {
			continue
		}
		candidate := GroupCircle{Lat: g.CenterLat, Lon: g.CenterLon, RadiusM: g.RadiusM}
		if circleIoU(center, candidate) < similarIoU {
			continue
		}
		if !schedule.SchedulesOverlap(sched, g.ScheduleJSON) {
			continue
		}
		out = append(out, SimilarGroup{ID: g.SpaceID, Name: g.Name})
	}
	return out
}
