package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/redis/go-redis/v9"

	"github.com/applemango/chain/internal/config"
	"github.com/applemango/chain/internal/db"
	"github.com/applemango/chain/internal/httpx"
	"github.com/applemango/chain/internal/kv"
	"github.com/applemango/chain/internal/model"
	"github.com/applemango/chain/internal/realtime"
)

// GroupService はグループ（09）の作成・詳細・変更・一覧・参加・退出・メンバー一覧。
type GroupService struct {
	Deps
	Zones     ZonePresence
	Access    *AccessService
	Moderator Moderator
	Broker    realtime.Broker
}

// GroupSchedule は 09 §3 の有効な時間帯。
type GroupSchedule struct {
	TZ    string              `json:"tz"`
	Slots []GroupScheduleSlot `json:"slots"`
}

type GroupScheduleSlot struct {
	Weekday int    `json:"weekday"`
	Start   string `json:"start"`
	End     string `json:"end"`
}

type CreateGroupInput struct {
	Name         string
	Description  string
	Visibility   string
	Persistent   bool
	Lat          float64
	Lon          float64
	RadiusM      int
	Schedule     *GroupSchedule
	InviteQuorum int
	Force        bool
}

type UpdateGroupInput struct {
	Name         *string
	Description  *string
	Schedule     *GroupSchedule
	InviteQuorum *int
}

// GroupDetail は GET /groups/{id}。中心座標などは member のときだけ埋める（09 §4）。
type GroupDetail struct {
	ID            string         `json:"id"`
	Name          string         `json:"name"`
	Description   string         `json:"description"`
	Visibility    string         `json:"visibility"`
	MemberCount   int            `json:"memberCount"`
	IsMember      bool           `json:"isMember"`
	State         *string        `json:"state,omitempty"`
	CreatedAt     *time.Time     `json:"createdAt,omitempty"`
	Role          *string        `json:"role,omitempty"`
	Origin        *string        `json:"origin,omitempty"`
	Persistent    *bool          `json:"persistent,omitempty"`
	RadiusM       *int           `json:"radiusM,omitempty"`
	CenterLat     *float64       `json:"centerLat,omitempty"`
	CenterLon     *float64       `json:"centerLon,omitempty"`
	Schedule      *GroupSchedule `json:"schedule,omitempty"`
	InviteQuorum  *int           `json:"inviteQuorum,omitempty"`
	ParentSpaceID *string        `json:"parentSpaceId,omitempty"`
}

type NearbyInsideItem struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	MemberCount int    `json:"memberCount"`
	IsMember    bool   `json:"isMember"`
	Persistent  bool   `json:"persistent"`
	RadiusM     int    `json:"radiusM"`
}

type NearbyNearItem struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	MemberCount    int    `json:"memberCount"`
	DistanceBucket string `json:"distanceBucket"`
}

type NearbyResult struct {
	Inside []NearbyInsideItem `json:"inside"`
	Near   []NearbyNearItem   `json:"near"`
}

// MemberItem は GET /groups/{id}/members の要素（09 §4）。プロフィールの要約だけを返す。
type MemberItem struct {
	DID         string    `json:"did"`
	Handle      string    `json:"handle"`
	DisplayName string    `json:"displayName,omitempty"`
	AvatarURL   string    `json:"avatarUrl,omitempty"`
	Role        string    `json:"role"`
	JoinedAt    time.Time `json:"joinedAt"`
}

// Create は 09 §2。geofence の中にいることを確かめ、重複を抑止してから 1 トランザクションで作る。
func (s *GroupService) Create(ctx context.Context, did string, in CreateGroupInput) (*GroupDetail, error) {
	if fields := validateCreateGroup(in); len(fields) > 0 {
		return nil, httpx.ErrInvalidRequest.With("fields", fields)
	}
	visibility := in.Visibility
	if visibility == "" {
		visibility = "open"
	}
	quorum := in.InviteQuorum
	if quorum < 1 {
		quorum = 1
	}

	if s.Zones == nil {
		return nil, httpx.ErrLocationStale
	}
	inside, err := s.Zones.Inside(ctx, did, Zone{
		CenterLat: in.Lat, CenterLon: in.Lon, RadiusM: in.RadiusM,
	}, config.LocationFresh)
	if errors.Is(err, ErrStale) {
		return nil, httpx.ErrLocationStale
	}
	if err != nil {
		return nil, err
	}
	if !inside {
		return nil, httpx.ErrNotInGeofence
	}

	label, cats, err := s.moderateTexts(ctx, "group_name", in.Name, in.Description)
	if err != nil {
		return nil, err
	}
	if label == LabelBlock {
		s.auditModerationBlock(ctx, did)
		return nil, httpx.ErrModerationBlocked.With("categories", cats)
	}

	all, err := model.ListActiveGroups(ctx, s.DB)
	if err != nil {
		return nil, err
	}
	center := GroupCircle{Lat: in.Lat, Lon: in.Lon, RadiusM: in.RadiusM}
	if similar := findSimilar(center, in.Schedule, all); len(similar) > 0 && !in.Force {
		return nil, httpx.ErrConflict.With("similar", similar)
	}

	now := s.Clock.Now()
	id := s.IDs.NewID()
	authority := authorityFor(s.Config.PublicURL)
	space := model.Space{
		ID: id, Type: spaceTypeGroup, Authority: authority,
		Ref:        model.SpaceRef{Authority: authority, Type: spaceTypeGroup, ID: id}.String(),
		ReadPolicy: "managing-app", WritePolicy: "managing-app",
		Name: in.Name, State: "active", CreatedBy: did, CreatedAt: now,
	}
	group := model.Group{
		SpaceID: id, Description: in.Description, Visibility: visibility, Origin: "explicit",
		Persistent: in.Persistent, CenterLat: in.Lat, CenterLon: in.Lon, RadiusM: in.RadiusM,
		ScheduleJSON: scheduleJSON(in.Schedule), InviteQuorum: quorum, ModLabel: string(label),
	}
	member := model.SpaceMember{SpaceID: id, DID: did, CanRead: true, CanWrite: true, Role: "owner", JoinedAt: now}

	var parent *string
	if err := db.Transaction(ctx, s.DB, func(tx *sql.Tx) error {
		if err := model.InsertSpace(ctx, tx, space); err != nil {
			return err
		}
		if err := model.InsertGroup(ctx, tx, group); err != nil {
			return err
		}
		if err := model.InsertSpaceMember(ctx, tx, member); err != nil {
			return err
		}
		var err error
		parent, err = s.assignParent(ctx, tx, id, group, all)
		return err
	}); err != nil {
		return nil, err
	}

	if err := s.geoAdd(ctx, id, in.Lat, in.Lon); err != nil {
		return nil, err
	}
	group.ParentSpaceID = parent
	return groupDetail(&space, &group, 1, "owner"), nil
}

// Get は 09 §4。メンバーには全項目、非メンバーには open のときだけ限られた項目を返す。
func (s *GroupService) Get(ctx context.Context, viewer, id string) (*GroupDetail, error) {
	sp, group, err := s.load(ctx, id)
	if err != nil {
		return nil, err
	}
	count, err := model.CountActiveMembers(ctx, s.DB, id)
	if err != nil {
		return nil, err
	}
	member, err := s.membership(ctx, id, viewer)
	if err != nil {
		return nil, err
	}
	if member != nil {
		return groupDetail(sp, group, count, member.Role), nil
	}
	if sp.State != "active" || group.Visibility != "open" {
		return nil, httpx.ErrNotFound
	}
	return limitedGroupDetail(sp, group, count), nil
}

// Update は 09 §9。owner だけが name / description / schedule / inviteQuorum を変えられる。
func (s *GroupService) Update(ctx context.Context, did, id string, in UpdateGroupInput) (*GroupDetail, error) {
	sp, group, err := s.load(ctx, id)
	if err != nil {
		return nil, err
	}
	member, err := s.membership(ctx, id, did)
	if err != nil {
		return nil, err
	}
	if member == nil || member.Role != "owner" {
		return nil, httpx.ErrForbidden
	}

	fields := map[string]string{}
	if in.Name != nil {
		n := strings.TrimSpace(*in.Name)
		if n == "" {
			fields["name"] = "必須です"
		} else if utf8.RuneCountInString(n) > 40 {
			fields["name"] = "40 文字までです"
		}
	}
	if in.Description != nil && utf8.RuneCountInString(*in.Description) > 500 {
		fields["description"] = "500 文字までです"
	}
	if in.InviteQuorum != nil && *in.InviteQuorum < 1 {
		fields["inviteQuorum"] = "1 以上で指定してください"
	}
	for k, v := range validateGroupSchedule(in.Schedule) {
		fields[k] = v
	}
	if len(fields) > 0 {
		return nil, httpx.ErrInvalidRequest.With("fields", fields)
	}

	label := group.ModLabel
	if in.Name != nil || in.Description != nil {
		name := sp.Name
		if in.Name != nil {
			name = strings.TrimSpace(*in.Name)
		}
		desc := group.Description
		if in.Description != nil {
			desc = *in.Description
		}
		l, cats, err := s.moderateTexts(ctx, "group_name", name, desc)
		if err != nil {
			return nil, err
		}
		if l == LabelBlock {
			s.auditModerationBlock(ctx, did)
			return nil, httpx.ErrModerationBlocked.With("categories", cats)
		}
		label = string(l)
	}

	newName := sp.Name
	if in.Name != nil {
		newName = strings.TrimSpace(*in.Name)
	}
	newDesc := group.Description
	if in.Description != nil {
		newDesc = *in.Description
	}
	newSchedule := group.ScheduleJSON
	if in.Schedule != nil {
		newSchedule = scheduleJSON(in.Schedule)
	}
	quorum := group.InviteQuorum
	if in.InviteQuorum != nil {
		quorum = *in.InviteQuorum
	}

	if err := db.Transaction(ctx, s.DB, func(tx *sql.Tx) error {
		if newName != sp.Name {
			if err := model.UpdateSpaceName(ctx, tx, id, newName); err != nil {
				return err
			}
		}
		return model.UpdateGroupAdmin(ctx, tx, id, newDesc, newSchedule, quorum, label)
	}); err != nil {
		return nil, err
	}

	if err := s.invalidateSpace(ctx, id); err != nil {
		return nil, err
	}
	sp.Name = newName
	group.Description = newDesc
	group.ScheduleJSON = newSchedule
	group.InviteQuorum = quorum
	group.ModLabel = label
	count, err := model.CountActiveMembers(ctx, s.DB, id)
	if err != nil {
		return nil, err
	}
	return groupDetail(sp, group, count, member.Role), nil
}

// Nearby は 09 §5。呼び出し元の最新位置から 2 km 以内の active・open・schedule 有効なグループを返す。
func (s *GroupService) Nearby(ctx context.Context, did string) (*NearbyResult, error) {
	loc, err := model.GetLocation(ctx, s.Redis, did)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, httpx.ErrLocationStale
	}
	if err != nil {
		return nil, err
	}
	now := s.Clock.Now()
	if now.Sub(loc.RecordedAt) > config.LocationFresh {
		return nil, httpx.ErrLocationStale
	}

	candidates, err := s.Redis.GeoSearchLocation(ctx, kv.GeoGroupsKey(), &redis.GeoSearchLocationQuery{
		GeoSearchQuery: redis.GeoSearchQuery{
			Longitude: loc.Lon, Latitude: loc.Lat, Radius: 2000, RadiusUnit: "m", Sort: "ASC",
		},
		WithDist: true,
	}).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return nil, err
	}

	res := &NearbyResult{Inside: []NearbyInsideItem{}, Near: []NearbyNearItem{}}
	for _, c := range candidates {
		sp, group, err := s.load(ctx, c.Name)
		if errors.Is(err, httpx.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if sp.State != "active" || group.Visibility != "open" || !scheduleActive(group.ScheduleJSON, now) {
			continue
		}
		count, err := model.CountActiveMembers(ctx, s.DB, sp.ID)
		if err != nil {
			return nil, err
		}
		zone := Zone{ID: sp.ID, CenterLat: group.CenterLat, CenterLon: group.CenterLon, RadiusM: group.RadiusM}
		if InsideZone(zone, loc) {
			mem, err := s.membership(ctx, sp.ID, did)
			if err != nil {
				return nil, err
			}
			res.Inside = append(res.Inside, NearbyInsideItem{
				ID: sp.ID, Name: sp.Name, MemberCount: count, IsMember: mem != nil,
				Persistent: group.Persistent, RadiusM: group.RadiusM,
			})
			continue
		}
		if d := Haversine(loc.Lat, loc.Lon, group.CenterLat, group.CenterLon); d <= 500 {
			res.Near = append(res.Near, NearbyNearItem{
				ID: sp.ID, Name: sp.Name, MemberCount: count, DistanceBucket: distanceBucket(d),
			})
		}
	}
	return res, nil
}

// Join は 09 §5。open で geofence の中（鮮度 120 秒）にいるときだけ参加できる。退出済みなら再加入させる。
func (s *GroupService) Join(ctx context.Context, did, id string) error {
	sp, group, err := s.load(ctx, id)
	if err != nil {
		return err
	}
	if sp.State == "archived" {
		return httpx.ErrGone
	}
	if sp.State != "active" || group.Visibility != "open" {
		return httpx.ErrForbidden
	}

	existing, gerr := model.GetSpaceMember(ctx, s.DB, id, did)
	hasLeft := false
	switch {
	case gerr == nil && existing.LeftAt == nil:
		return httpx.ErrConflict
	case gerr == nil:
		hasLeft = true
	case !errors.Is(gerr, sql.ErrNoRows):
		return gerr
	}

	if s.Zones == nil {
		return httpx.ErrLocationStale
	}
	inside, err := s.Zones.Inside(ctx, did, Zone{
		ID: id, CenterLat: group.CenterLat, CenterLon: group.CenterLon, RadiusM: group.RadiusM,
	}, config.LocationFresh)
	if errors.Is(err, ErrStale) {
		return httpx.ErrLocationStale
	}
	if err != nil {
		return err
	}
	if !inside {
		return httpx.ErrNotInGeofence
	}
	if !scheduleActive(group.ScheduleJSON, s.Clock.Now()) {
		return httpx.ErrForbidden
	}

	now := s.Clock.Now()
	if hasLeft {
		if err := model.RejoinSpaceMember(ctx, s.DB, id, did, now); err != nil {
			return err
		}
	} else {
		if err := model.InsertSpaceMember(ctx, s.DB, model.SpaceMember{
			SpaceID: id, DID: did, CanRead: true, CanWrite: true, Role: "member", JoinedAt: now,
		}); err != nil {
			return err
		}
	}
	if err := s.invalidate(ctx, id, did); err != nil {
		return err
	}
	s.publish(ctx, realtime.Event{Name: "member.joined", Space: id, Data: map[string]string{"did": did}})
	return nil
}

// Leave は 09 §5。owner が退出したら joined_at が最も古いメンバーへ owner を移し、最後の 1 人なら archived にする。
func (s *GroupService) Leave(ctx context.Context, did, id string) error {
	sp, _, err := s.load(ctx, id)
	if err != nil {
		return err
	}
	member, err := s.membership(ctx, id, did)
	if err != nil {
		return err
	}
	if member == nil || sp.State != "active" {
		return httpx.ErrForbidden
	}

	active, err := model.ListActiveSpaceMembers(ctx, s.DB, id)
	if err != nil {
		return err
	}
	now := s.Clock.Now()
	archive := false
	if err := db.Transaction(ctx, s.DB, func(tx *sql.Tx) error {
		if err := model.LeaveSpaceMember(ctx, tx, id, did, now); err != nil {
			return err
		}
		if next := oldestOther(active, did); next == nil {
			archive = true
			archivedAt := now
			if err := model.SetSpaceState(ctx, tx, id, "archived", &archivedAt); err != nil {
				return err
			}
		} else if member.Role == "owner" {
			if err := model.SetSpaceMemberRole(ctx, tx, id, next.DID, "owner"); err != nil {
				return err
			}
		}
		return model.SetSpaceMemberRole(ctx, tx, id, did, "member")
	}); err != nil {
		return err
	}

	if archive {
		if err := s.geoRemove(ctx, id); err != nil {
			return err
		}
	}
	if err := s.invalidateSpace(ctx, id); err != nil {
		return err
	}
	s.publish(ctx, realtime.Event{Name: "member.left", Space: id, Data: map[string]string{"did": did}})
	if archive {
		s.publish(ctx, realtime.Event{Name: "space.archived", Space: id})
	}
	return nil
}

// Members は 09 §4。read 権限がある人にだけ、メンバーのプロフィール要約を返す。
func (s *GroupService) Members(ctx context.Context, viewer, id string) ([]MemberItem, error) {
	if s.Access == nil {
		return nil, httpx.ErrForbidden
	}
	ok, err := s.Access.Check(ctx, id, syntax.DID(viewer), ModeRead)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, httpx.ErrForbidden
	}
	members, err := model.ListActiveSpaceMembers(ctx, s.DB, id)
	if err != nil {
		return nil, err
	}
	out := make([]MemberItem, 0, len(members))
	for _, m := range members {
		summary, err := authorSummaryFor(ctx, s.DB, m.DID)
		if err != nil {
			return nil, err
		}
		item := MemberItem{DID: m.DID, Role: m.Role, JoinedAt: m.JoinedAt}
		if summary != nil {
			item.Handle = summary.Handle
			item.DisplayName = summary.DisplayName
			item.AvatarURL = summary.AvatarURL
		}
		out = append(out, item)
	}
	return out, nil
}

// load は Space と group を読み、グループでなければ 404 にする。
func (s *GroupService) load(ctx context.Context, id string) (*model.Space, *model.Group, error) {
	sp, err := model.GetSpace(ctx, s.DB, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, httpx.ErrNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	if sp.State == "deleted" || sp.Type != spaceTypeGroup {
		return nil, nil, httpx.ErrNotFound
	}
	group, err := model.GetGroup(ctx, s.DB, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, httpx.ErrNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	return sp, group, nil
}

// membership は read できる現役メンバーならその行を返す。退出済み・非メンバーは nil。
func (s *GroupService) membership(ctx context.Context, spaceID, did string) (*model.SpaceMember, error) {
	m, err := model.GetSpaceMember(ctx, s.DB, spaceID, did)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if m.LeftAt != nil || !m.CanRead {
		return nil, nil
	}
	return m, nil
}

func (s *GroupService) moderateTexts(ctx context.Context, kind string, texts ...string) (Label, []string, error) {
	if s.Moderator == nil {
		return LabelOK, nil, nil
	}
	label := LabelOK
	var cats []string
	for _, text := range texts {
		if strings.TrimSpace(text) == "" {
			continue
		}
		v, err := s.Moderator.Judge(ctx, Content{Kind: kind, Text: text})
		if err != nil {
			slog.WarnContext(ctx, "moderation error; treating as ok", "err", err)
			continue
		}
		cats = append(cats, v.Categories...)
		if v.Label == LabelBlock {
			return LabelBlock, cats, nil
		}
		if v.Label == LabelWarn {
			label = LabelWarn
		}
	}
	return label, cats, nil
}

func (s *GroupService) auditModerationBlock(ctx context.Context, did string) {
	err := model.InsertAuditLog(ctx, s.DB, model.AuditEntry{
		ID: s.IDs.NewID(), ActorDID: did, Action: "moderation.block", Target: "group_name", CreatedAt: s.Clock.Now(),
	})
	if err != nil {
		slog.WarnContext(ctx, "audit log failed", "err", err)
	}
}

// assignParent は新しいグループの parent_space_id と、自分が含むグループ群の parent_space_id を再計算する（09 §2）。
func (s *GroupService) assignParent(ctx context.Context, q db.Querier, newID string, newGroup model.Group, all []model.GroupWithSpace) (*string, error) {
	target := namedCircle{ID: newID, GroupCircle: GroupCircle{
		Lat: newGroup.CenterLat, Lon: newGroup.CenterLon, RadiusM: newGroup.RadiusM,
	}}
	existing := make([]namedCircle, 0, len(all))
	parents := make(map[string]*string, len(all))
	for _, g := range all {
		existing = append(existing, namedCircle{ID: g.SpaceID, GroupCircle: GroupCircle{
			Lat: g.CenterLat, Lon: g.CenterLon, RadiusM: g.RadiusM,
		}})
		parents[g.SpaceID] = g.ParentSpaceID
	}

	parent, updates := planParents(target, existing, parents)
	if err := model.UpdateGroupParent(ctx, q, newID, parent); err != nil {
		return nil, err
	}
	for id, p := range updates {
		if err := model.UpdateGroupParent(ctx, q, id, p); err != nil {
			return nil, err
		}
	}
	return parent, nil
}

func (s *GroupService) geoAdd(ctx context.Context, spaceID string, lat, lon float64) error {
	return s.Redis.GeoAdd(ctx, kv.GeoGroupsKey(), &redis.GeoLocation{
		Name: spaceID, Longitude: lon, Latitude: lat,
	}).Err()
}

func (s *GroupService) geoRemove(ctx context.Context, spaceID string) error {
	return s.Redis.ZRem(ctx, kv.GeoGroupsKey(), spaceID).Err()
}

func (s *GroupService) invalidate(ctx context.Context, spaceID, did string) error {
	if s.Access == nil {
		return nil
	}
	return s.Access.Invalidate(ctx, spaceID, did)
}

func (s *GroupService) invalidateSpace(ctx context.Context, spaceID string) error {
	if s.Access == nil {
		return nil
	}
	return s.Access.InvalidateSpace(ctx, spaceID)
}

func (s *GroupService) publish(ctx context.Context, e realtime.Event) {
	if s.Broker != nil {
		s.Broker.Publish(ctx, e)
	}
}

func validateCreateGroup(in CreateGroupInput) map[string]string {
	fields := map[string]string{}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		fields["name"] = "必須です"
	} else if utf8.RuneCountInString(name) > 40 {
		fields["name"] = "40 文字までです"
	}
	if utf8.RuneCountInString(in.Description) > 500 {
		fields["description"] = "500 文字までです"
	}
	if in.Visibility != "" && in.Visibility != "open" && in.Visibility != "closed" {
		fields["visibility"] = "open / closed のいずれかです"
	}
	if in.Lat < -90 || in.Lat > 90 {
		fields["geofence.lat"] = "-90 から 90 の範囲で指定してください"
	}
	if in.Lon < -180 || in.Lon > 180 {
		fields["geofence.lon"] = "-180 から 180 の範囲で指定してください"
	}
	if in.RadiusM < 20 || in.RadiusM > 2000 {
		fields["geofence.radiusM"] = "20〜2000 で指定してください"
	}
	if in.InviteQuorum < 0 {
		fields["inviteQuorum"] = "1 以上で指定してください"
	}
	for k, v := range validateGroupSchedule(in.Schedule) {
		fields[k] = v
	}
	return fields
}

func groupDetail(sp *model.Space, g *model.Group, memberCount int, role string) *GroupDetail {
	d := limitedGroupDetail(sp, g, memberCount)
	state := sp.State
	created := sp.CreatedAt
	origin := g.Origin
	persistent := g.Persistent
	radius := g.RadiusM
	lat, lon := g.CenterLat, g.CenterLon
	quorum := g.InviteQuorum
	d.IsMember = true
	d.State = &state
	d.CreatedAt = &created
	d.Role = &role
	d.Origin = &origin
	d.Persistent = &persistent
	d.RadiusM = &radius
	d.CenterLat = &lat
	d.CenterLon = &lon
	d.InviteQuorum = &quorum
	d.ParentSpaceID = g.ParentSpaceID
	d.Schedule = parseSchedule(g.ScheduleJSON)
	return d
}

// limitedGroupDetail は非メンバーに見せる最小の項目だけを返す（09 §4）。
func limitedGroupDetail(sp *model.Space, g *model.Group, memberCount int) *GroupDetail {
	return &GroupDetail{
		ID: sp.ID, Name: sp.Name, Description: g.Description, Visibility: g.Visibility,
		MemberCount: memberCount,
	}
}

func parseSchedule(raw *string) *GroupSchedule {
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return nil
	}
	var s GroupSchedule
	if err := json.Unmarshal([]byte(*raw), &s); err != nil {
		return nil
	}
	return &s
}

func scheduleJSON(s *GroupSchedule) *string {
	if s == nil {
		return nil
	}
	b, err := json.Marshal(s)
	if err != nil {
		return nil
	}
	v := string(b)
	return &v
}

func distanceBucket(d float64) string {
	if d < 100 {
		return "<100m"
	}
	return "<500m"
}

func oldestOther(members []model.SpaceMember, did string) *model.SpaceMember {
	for i := range members {
		if members[i].DID != did {
			return &members[i]
		}
	}
	return nil
}

func sameStringPtr(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}
