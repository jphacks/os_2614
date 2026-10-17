package group

import (
	"context"
	"database/sql"
	"errors"
	"sort"

	"github.com/redis/go-redis/v9"

	"github.com/applemango/chain/internal/db"
	"github.com/applemango/chain/internal/httpx"
	"github.com/applemango/chain/internal/kv"
	"github.com/applemango/chain/internal/model"
	groupmodel "github.com/applemango/chain/internal/model/group"
	spacemodel "github.com/applemango/chain/internal/model/space"
	"github.com/applemango/chain/internal/service/core"
	"github.com/applemango/chain/internal/service/proximity"
)

// groupSearchRadiusM は nearby（09 §5）とグラフ（09 §7）で候補を集める半径。geofence の
// 最大半径（2000 m）と同じにする。中心が半径ぶん離れていても点を含み得るため。
const groupSearchRadiusM = 2000.0

// GraphNode はグラフの頂点（09 §7）。中心座標は返さない。
type GraphNode struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	RadiusM     int    `json:"radiusM"`
	MemberCount int    `json:"memberCount"`
	IsMember    bool   `json:"isMember"`
	Inside      bool   `json:"inside"`
	Origin      string `json:"origin"`
}

// GraphEdge はグラフの辺（09 §7）。weight は shared_members の人数のときだけ入る。
type GraphEdge struct {
	From   string `json:"from"`
	To     string `json:"to"`
	Kind   string `json:"kind"`
	Weight int    `json:"weight,omitempty"`
}

type GraphResult struct {
	Nodes []GraphNode `json:"nodes"`
	Edges []GraphEdge `json:"edges"`
}

// Graph は 09 §7。呼び出し元の最新位置（鮮度 120 秒）から 2 km 以内の、呼び出し元に
// 見える active なグループを頂点にし、包含・重なり・共通メンバーの辺を返す。
func (s *GroupService) Graph(ctx context.Context, did string) (*GraphResult, error) {
	loc, err := model.GetLocation(ctx, s.Redis, did)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, httpx.ErrLocationStale
	}
	if err != nil {
		return nil, err
	}
	if s.Clock.Now().Sub(loc.RecordedAt) > core.LocationFresh {
		return nil, httpx.ErrLocationStale
	}

	candidates, err := searchGroupsNear(ctx, s.Redis, loc.Lat, loc.Lon, groupSearchRadiusM)
	if err != nil {
		return nil, err
	}

	res := &GraphResult{Nodes: []GraphNode{}, Edges: []GraphEdge{}}
	circles := make([]namedCircle, 0, len(candidates))
	members := make(map[string]map[string]bool, len(candidates))
	for _, c := range candidates {
		sp, group, ok, err := lookupGroup(ctx, s.DB, c.Name)
		if err != nil {
			return nil, err
		}
		if !ok || sp.State != "active" {
			continue
		}
		member, err := s.membership(ctx, sp.ID, did)
		if err != nil {
			return nil, err
		}
		if group.Visibility != "open" && member == nil {
			continue
		}
		count, err := spacemodel.CountActiveMembers(ctx, s.DB, sp.ID)
		if err != nil {
			return nil, err
		}
		zone := proximity.Zone{ID: sp.ID, CenterLat: group.CenterLat, CenterLon: group.CenterLon, RadiusM: group.RadiusM}
		res.Nodes = append(res.Nodes, GraphNode{
			ID: sp.ID, Name: sp.Name, RadiusM: group.RadiusM, MemberCount: count,
			IsMember: member != nil, Inside: proximity.InsideZone(zone, loc), Origin: group.Origin,
		})
		circles = append(circles, namedCircle{ID: sp.ID, GroupCircle: GroupCircle{
			Lat: group.CenterLat, Lon: group.CenterLon, RadiusM: group.RadiusM,
		}})
		members[sp.ID], err = activeMemberSet(ctx, s.DB, sp.ID)
		if err != nil {
			return nil, err
		}
	}

	res.Edges = append(res.Edges, graphGeometricEdges(circles)...)
	res.Edges = append(res.Edges, graphSharedEdges(members)...)
	sortGraphEdges(res.Edges)
	return res, nil
}

// searchGroupsNear は geo:groups から点の周囲 radiusM のグループを近い順で返す（09 §5、§7）。
func searchGroupsNear(ctx context.Context, r *redis.Client, lat, lon, radiusM float64) ([]redis.GeoLocation, error) {
	res, err := r.GeoSearchLocation(ctx, kv.GeoGroupsKey(), &redis.GeoSearchLocationQuery{
		GeoSearchQuery: redis.GeoSearchQuery{
			Longitude: lon, Latitude: lat, Radius: radiusM, RadiusUnit: "m", Sort: "ASC",
		},
		WithDist: true,
	}).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return nil, err
	}
	return res, nil
}

// lookupGroup は Space と group を読む。グループでない・削除済み・行がなければ ok=false。
func lookupGroup(ctx context.Context, q db.Querier, id string) (*spacemodel.Space, *groupmodel.Group, bool, error) {
	sp, err := spacemodel.GetSpace(ctx, q, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, false, nil
	}
	if err != nil {
		return nil, nil, false, err
	}
	if sp.Type != core.SpaceTypeGroup || sp.State == "deleted" {
		return nil, nil, false, nil
	}
	group, err := groupmodel.GetGroup(ctx, q, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, false, nil
	}
	if err != nil {
		return nil, nil, false, err
	}
	return sp, group, true, nil
}

// activeMemberSet は left_at IS NULL のメンバーの DID 集合を返す（09 §7 の shared_members）。
func activeMemberSet(ctx context.Context, q db.Querier, spaceID string) (map[string]bool, error) {
	members, err := spacemodel.ListActiveSpaceMembers(ctx, q, spaceID)
	if err != nil {
		return nil, err
	}
	set := make(map[string]bool, len(members))
	for _, m := range members {
		set[m.DID] = true
	}
	return set, nil
}

// graphGeometricEdges は各頂点の組について contains か overlaps の辺を返す（09 §7）。
// contains は distance + r_child <= r_parent。半径も中心も同じ 2 円は「等しい」ので overlaps にする。
func graphGeometricEdges(nodes []namedCircle) []GraphEdge {
	out := []GraphEdge{}
	for i := 0; i < len(nodes); i++ {
		for j := i + 1; j < len(nodes); j++ {
			a, b := nodes[i], nodes[j]
			ab := containsCircle(a.GroupCircle, b.GroupCircle)
			ba := containsCircle(b.GroupCircle, a.GroupCircle)
			switch {
			case ab && !ba:
				out = append(out, GraphEdge{From: a.ID, To: b.ID, Kind: "contains"})
			case ba && !ab:
				out = append(out, GraphEdge{From: b.ID, To: a.ID, Kind: "contains"})
			case overlapsCircle(a.GroupCircle, b.GroupCircle):
				from, to := a.ID, b.ID
				if from > to {
					from, to = to, from
				}
				out = append(out, GraphEdge{From: from, To: to, Kind: "overlaps"})
			}
		}
	}
	return out
}

// graphSharedEdges は共通の未退出メンバーが 2 人以上いる組にだけ shared_members の辺を返す（09 §7）。
// 1 人だと、自分以外の誰が両方にいるかを推測できてしまうため出さない。
func graphSharedEdges(members map[string]map[string]bool) []GraphEdge {
	ids := make([]string, 0, len(members))
	for id := range members {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	out := []GraphEdge{}
	for i := 0; i < len(ids); i++ {
		for j := i + 1; j < len(ids); j++ {
			if w := sharedMembers(members[ids[i]], members[ids[j]]); w >= 2 {
				out = append(out, GraphEdge{From: ids[i], To: ids[j], Kind: "shared_members", Weight: w})
			}
		}
	}
	return out
}

func sharedMembers(a, b map[string]bool) int {
	n := 0
	for did := range a {
		if b[did] {
			n++
		}
	}
	return n
}

// sortGraphEdges は辺の順序を安定させる。API のレスポンスが実行ごとに変わらないようにする。
func sortGraphEdges(edges []GraphEdge) {
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].From != edges[j].From {
			return edges[i].From < edges[j].From
		}
		if edges[i].To != edges[j].To {
			return edges[i].To < edges[j].To
		}
		return edges[i].Kind < edges[j].Kind
	})
}
