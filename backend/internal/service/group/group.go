package group

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
	groupmodel "github.com/applemango/chain/internal/model/group"
	spacemodel "github.com/applemango/chain/internal/model/space"
	"github.com/applemango/chain/internal/realtime"
	"github.com/applemango/chain/internal/service/access"
	"github.com/applemango/chain/internal/service/content"
	"github.com/applemango/chain/internal/service/core"
	"github.com/applemango/chain/internal/service/moderation"
	"github.com/applemango/chain/internal/service/proximity"
	"github.com/applemango/chain/internal/service/schedule"
	"github.com/applemango/chain/internal/storage"
)

// GroupService はグループ（09）の作成・詳細・変更・一覧・参加・退出・メンバー一覧。
type GroupService struct {
	core.Deps
	Zones     proximity.ZonePresence
	Access    *access.AccessService
	Moderator moderation.Moderator
	Broker    realtime.Broker
	// Invites は closed グループの招待（09 §6）。メンバーの退出で必要承認数が下がったときの再判定に使う。
	Invites *GroupInviteService
	// Signer はアイコン画像の署名付き URL を作る（10 §4.4）。
	Signer storage.Signer
}

type CreateGroupInput struct {
	Name         string
	Description  string
	Visibility   string
	Persistent   bool
	Lat          float64
	Lon          float64
	RadiusM      int
	Schedule     *schedule.GroupSchedule
	InviteQuorum int
	Force        bool
	// IconMediaID は POST /media で上げた未添付の画像（09 §2）。空ならアイコンなし。
	IconMediaID string
}

type UpdateGroupInput struct {
	Name         *string
	Description  *string
	Schedule     *schedule.GroupSchedule
	InviteQuorum *int
	Visibility   *string
}

// GroupDetail は GET /groups/{id}。中心座標などは member のときだけ埋める（09 §4）。
type GroupDetail struct {
	ID            string                  `json:"id"`
	Name          string                  `json:"name"`
	Description   string                  `json:"description"`
	Visibility    string                  `json:"visibility"`
	MemberCount   int                     `json:"memberCount"`
	IsMember      bool                    `json:"isMember"`
	State         *string                 `json:"state,omitempty"`
	CreatedAt     *time.Time              `json:"createdAt,omitempty"`
	Role          *string                 `json:"role,omitempty"`
	Origin        *string                 `json:"origin,omitempty"`
	Persistent    *bool                   `json:"persistent,omitempty"`
	RadiusM       *int                    `json:"radiusM,omitempty"`
	CenterLat     *float64                `json:"centerLat,omitempty"`
	CenterLon     *float64                `json:"centerLon,omitempty"`
	Schedule      *schedule.GroupSchedule `json:"schedule,omitempty"`
	InviteQuorum  *int                    `json:"inviteQuorum,omitempty"`
	ParentSpaceID *string                 `json:"parentSpaceId,omitempty"`
	// IconURL はアイコン画像（サムネイル）の署名付き URL。無ければ省く（09 §2）。
	IconURL string `json:"iconUrl,omitempty"`
}

type NearbyInsideItem struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	MemberCount int    `json:"memberCount"`
	IsMember    bool   `json:"isMember"`
	Persistent  bool   `json:"persistent"`
	RadiusM     int    `json:"radiusM"`
	IconURL     string `json:"iconUrl,omitempty"`
}

type NearbyNearItem struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	MemberCount    int    `json:"memberCount"`
	DistanceBucket string `json:"distanceBucket"`
	IconURL        string `json:"iconUrl,omitempty"`
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
	inside, err := s.Zones.Inside(ctx, did, proximity.Zone{
		CenterLat: in.Lat, CenterLon: in.Lon, RadiusM: in.RadiusM,
	}, config.LocationFresh)
	if errors.Is(err, proximity.ErrStale) {
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
	if label == moderation.LabelBlock {
		s.auditModerationBlock(ctx, did)
		return nil, httpx.ErrModerationBlocked.With("categories", cats)
	}

	if in.IconMediaID != "" {
		if err := s.checkIcon(ctx, did, in.IconMediaID); err != nil {
			return nil, err
		}
	}

	all, err := groupmodel.ListActiveGroups(ctx, s.DB)
	if err != nil {
		return nil, err
	}
	center := GroupCircle{Lat: in.Lat, Lon: in.Lon, RadiusM: in.RadiusM}
	if similar := findSimilar(center, in.Schedule, all); len(similar) > 0 && !in.Force {
		return nil, httpx.ErrConflict.With("similar", similar)
	}

	now := s.Clock.Now()
	id := s.IDs.NewID()
	authority := core.AuthorityFor(s.Config.PublicURL)
	space := spacemodel.Space{
		ID: id, Type: core.SpaceTypeGroup, Authority: authority,
		Ref:        spacemodel.SpaceRef{Authority: authority, Type: core.SpaceTypeGroup, ID: id}.String(),
		ReadPolicy: "managing-app", WritePolicy: "managing-app",
		Name: in.Name, State: "active", CreatedBy: did, CreatedAt: now,
	}
	group := groupmodel.Group{
		SpaceID: id, Description: in.Description, Visibility: visibility, Origin: "explicit",
		Persistent: in.Persistent, CenterLat: in.Lat, CenterLon: in.Lon, RadiusM: in.RadiusM,
		ScheduleJSON: ScheduleJSON(in.Schedule), InviteQuorum: quorum, ModLabel: string(label),
	}
	member := spacemodel.SpaceMember{SpaceID: id, DID: did, CanRead: true, CanWrite: true, Role: "owner", JoinedAt: now}

	var parent *string
	if err := db.Transaction(ctx, s.DB, func(tx *sql.Tx) error {
		if err := spacemodel.InsertSpace(ctx, tx, space); err != nil {
			return err
		}
		if err := groupmodel.InsertGroup(ctx, tx, group); err != nil {
			return err
		}
		if err := spacemodel.InsertSpaceMember(ctx, tx, member); err != nil {
			return err
		}
		if in.IconMediaID != "" {
			// 確認から添付までの間に他で使われていたら、作成ごと取り消す
			attached, err := spacemodel.AttachMedia(ctx, tx, in.IconMediaID, did, id)
			if err != nil {
				return err
			}
			if !attached {
				return invalidIcon()
			}
			if err := groupmodel.SetGroupIcon(ctx, tx, id, in.IconMediaID); err != nil {
				return err
			}
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
	return s.withIcon(ctx, groupDetail(&space, &group, 1, "owner"))
}

// Get は 09 §4。メンバーには全項目、非メンバーには open のときだけ限られた項目を返す。
func (s *GroupService) Get(ctx context.Context, viewer, id string) (*GroupDetail, error) {
	sp, group, err := s.load(ctx, id)
	if err != nil {
		return nil, err
	}
	count, err := spacemodel.CountActiveMembers(ctx, s.DB, id)
	if err != nil {
		return nil, err
	}
	member, err := s.membership(ctx, id, viewer)
	if err != nil {
		return nil, err
	}
	if member != nil {
		return s.withIcon(ctx, groupDetail(sp, group, count, member.Role))
	}
	if sp.State != "active" || group.Visibility != "open" {
		return nil, httpx.ErrNotFound
	}
	return s.withIcon(ctx, limitedGroupDetail(sp, group, count))
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
	if in.Visibility != nil && *in.Visibility != "open" && *in.Visibility != "closed" {
		fields["visibility"] = "open / closed のいずれかです"
	}
	for k, v := range schedule.ValidateGroupSchedule(in.Schedule) {
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
		if l == moderation.LabelBlock {
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
		newSchedule = ScheduleJSON(in.Schedule)
	}
	quorum := group.InviteQuorum
	if in.InviteQuorum != nil {
		quorum = *in.InviteQuorum
	}
	newVisibility := group.Visibility
	if in.Visibility != nil {
		newVisibility = *in.Visibility
	}

	if err := db.Transaction(ctx, s.DB, func(tx *sql.Tx) error {
		if newName != sp.Name {
			if err := spacemodel.UpdateSpaceName(ctx, tx, id, newName); err != nil {
				return err
			}
		}
		if err := groupmodel.UpdateGroupAdmin(ctx, tx, id, newDesc, newSchedule, quorum, label); err != nil {
			return err
		}
		if newVisibility != group.Visibility {
			return groupmodel.SetGroupVisibility(ctx, tx, id, newVisibility)
		}
		return nil
	}); err != nil {
		return nil, err
	}

	if err := s.invalidateSpace(ctx, id); err != nil {
		return nil, err
	}
	s.publish(ctx, realtime.Event{Name: "group.updated", Space: id, Data: map[string]string{"id": id, "name": newName}})
	sp.Name = newName
	group.Description = newDesc
	group.ScheduleJSON = newSchedule
	group.InviteQuorum = quorum
	group.Visibility = newVisibility
	group.ModLabel = label
	count, err := spacemodel.CountActiveMembers(ctx, s.DB, id)
	if err != nil {
		return nil, err
	}
	return s.withIcon(ctx, groupDetail(sp, group, count, member.Role))
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
		if sp.State != "active" || group.Visibility != "open" || !schedule.ScheduleActive(group.ScheduleJSON, now) {
			continue
		}
		count, err := spacemodel.CountActiveMembers(ctx, s.DB, sp.ID)
		if err != nil {
			return nil, err
		}
		zone := proximity.Zone{ID: sp.ID, CenterLat: group.CenterLat, CenterLon: group.CenterLon, RadiusM: group.RadiusM}
		if proximity.InsideZone(zone, loc) {
			mem, err := s.membership(ctx, sp.ID, did)
			if err != nil {
				return nil, err
			}
			icon, err := s.iconURL(ctx, sp.ID)
			if err != nil {
				return nil, err
			}
			res.Inside = append(res.Inside, NearbyInsideItem{
				ID: sp.ID, Name: sp.Name, MemberCount: count, IsMember: mem != nil,
				Persistent: group.Persistent, RadiusM: group.RadiusM, IconURL: icon,
			})
			continue
		}
		if d := proximity.Haversine(loc.Lat, loc.Lon, group.CenterLat, group.CenterLon); d <= 500 {
			icon, err := s.iconURL(ctx, sp.ID)
			if err != nil {
				return nil, err
			}
			res.Near = append(res.Near, NearbyNearItem{
				ID: sp.ID, Name: sp.Name, MemberCount: count, DistanceBucket: distanceBucket(d),
				IconURL: icon,
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

	existing, gerr := spacemodel.GetSpaceMember(ctx, s.DB, id, did)
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
	inside, err := s.Zones.Inside(ctx, did, proximity.Zone{
		ID: id, CenterLat: group.CenterLat, CenterLon: group.CenterLon, RadiusM: group.RadiusM,
	}, config.LocationFresh)
	if errors.Is(err, proximity.ErrStale) {
		return httpx.ErrLocationStale
	}
	if err != nil {
		return err
	}
	if !inside {
		return httpx.ErrNotInGeofence
	}
	if !schedule.ScheduleActive(group.ScheduleJSON, s.Clock.Now()) {
		return httpx.ErrForbidden
	}

	now := s.Clock.Now()
	if hasLeft {
		if err := spacemodel.RejoinSpaceMember(ctx, s.DB, id, did, now); err != nil {
			return err
		}
	} else {
		if err := spacemodel.InsertSpaceMember(ctx, s.DB, spacemodel.SpaceMember{
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

	active, err := spacemodel.ListActiveSpaceMembers(ctx, s.DB, id)
	if err != nil {
		return err
	}
	now := s.Clock.Now()
	archive := false
	if err := db.Transaction(ctx, s.DB, func(tx *sql.Tx) error {
		if err := spacemodel.LeaveSpaceMember(ctx, tx, id, did, now); err != nil {
			return err
		}
		if next := oldestOther(active, did); next == nil {
			archive = true
			archivedAt := now
			if err := spacemodel.SetSpaceState(ctx, tx, id, "archived", &archivedAt); err != nil {
				return err
			}
		} else if member.Role == "owner" {
			if err := spacemodel.SetSpaceMemberRole(ctx, tx, id, next.DID, "owner"); err != nil {
				return err
			}
		}
		return spacemodel.SetSpaceMemberRole(ctx, tx, id, did, "member")
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
	// メンバーが減ると必要承認数が下がり、collecting の招待が offered になりうる（09 §6）。
	if !archive && s.Invites != nil {
		if err := s.Invites.ReconsiderOnLeave(ctx, id); err != nil {
			slog.WarnContext(ctx, "group invite reconsider failed", "err", err)
		}
	}
	return nil
}

// Members は 09 §4。read 権限がある人にだけ、メンバーのプロフィール要約を返す。
func (s *GroupService) Members(ctx context.Context, viewer, id string) ([]MemberItem, error) {
	if s.Access == nil {
		return nil, httpx.ErrForbidden
	}
	ok, err := s.Access.Check(ctx, id, syntax.DID(viewer), access.ModeRead)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, httpx.ErrForbidden
	}
	members, err := spacemodel.ListActiveSpaceMembers(ctx, s.DB, id)
	if err != nil {
		return nil, err
	}
	out := make([]MemberItem, 0, len(members))
	for _, m := range members {
		summary, err := core.AuthorSummaryFor(ctx, s.DB, m.DID)
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
func (s *GroupService) load(ctx context.Context, id string) (*spacemodel.Space, *groupmodel.Group, error) {
	sp, err := spacemodel.GetSpace(ctx, s.DB, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, httpx.ErrNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	if sp.State == "deleted" || sp.Type != core.SpaceTypeGroup {
		return nil, nil, httpx.ErrNotFound
	}
	group, err := groupmodel.GetGroup(ctx, s.DB, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, httpx.ErrNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	return sp, group, nil
}

// membership は read できる現役メンバーならその行を返す。退出済み・非メンバーは nil。
func (s *GroupService) membership(ctx context.Context, spaceID, did string) (*spacemodel.SpaceMember, error) {
	m, err := spacemodel.GetSpaceMember(ctx, s.DB, spaceID, did)
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

func (s *GroupService) moderateTexts(ctx context.Context, kind string, texts ...string) (moderation.Label, []string, error) {
	if s.Moderator == nil {
		return moderation.LabelOK, nil, nil
	}
	label := moderation.LabelOK
	var cats []string
	for _, text := range texts {
		if strings.TrimSpace(text) == "" {
			continue
		}
		v, err := s.Moderator.Judge(ctx, moderation.Content{Kind: kind, Text: text})
		if err != nil {
			slog.WarnContext(ctx, "moderation error; treating as ok", "err", err)
			continue
		}
		cats = append(cats, v.Categories...)
		if v.Label == moderation.LabelBlock {
			return moderation.LabelBlock, cats, nil
		}
		if v.Label == moderation.LabelWarn {
			label = moderation.LabelWarn
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
func (s *GroupService) assignParent(ctx context.Context, q db.Querier, newID string, newGroup groupmodel.Group, all []groupmodel.GroupWithSpace) (*string, error) {
	return AssignGroupParent(ctx, q, newID, newGroup, all)
}

// AssignGroupParent は assignParent の本体。自動グループの作成からも使う。
func AssignGroupParent(ctx context.Context, q db.Querier, newID string, newGroup groupmodel.Group, all []groupmodel.GroupWithSpace) (*string, error) {
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
	if err := groupmodel.UpdateGroupParent(ctx, q, newID, parent); err != nil {
		return nil, err
	}
	for id, p := range updates {
		if err := groupmodel.UpdateGroupParent(ctx, q, id, p); err != nil {
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

// RebuildGeoIndex は SQLite の active なグループから geo:groups を組み直す（09 §5）。
// seed で作ったグループは Redis に入らないので、起動時と db-reset でこれを呼ぶ。
func (s *GroupService) RebuildGeoIndex(ctx context.Context) error {
	groups, err := groupmodel.ListActiveGroups(ctx, s.DB)
	if err != nil {
		return err
	}
	if err := s.Redis.Del(ctx, kv.GeoGroupsKey()).Err(); err != nil {
		return err
	}
	if len(groups) == 0 {
		return nil
	}
	locations := make([]*redis.GeoLocation, 0, len(groups))
	for _, g := range groups {
		locations = append(locations, &redis.GeoLocation{Name: g.SpaceID, Longitude: g.CenterLon, Latitude: g.CenterLat})
	}
	return s.Redis.GeoAdd(ctx, kv.GeoGroupsKey(), locations...).Err()
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
	if in.RadiusM < 5 || in.RadiusM > 2000 {
		fields["geofence.radiusM"] = "5〜2000 で指定してください"
	}
	if in.InviteQuorum < 0 {
		fields["inviteQuorum"] = "1 以上で指定してください"
	}
	for k, v := range schedule.ValidateGroupSchedule(in.Schedule) {
		fields[k] = v
	}
	return fields
}

func groupDetail(sp *spacemodel.Space, g *groupmodel.Group, memberCount int, role string) *GroupDetail {
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

// invalidIcon はアイコンに使えない画像を指定されたときの 400（09 §2）。
func invalidIcon() error {
	return httpx.ErrInvalidRequest.With("fields", map[string]string{"iconMediaId": "使用できない画像です"})
}

// checkIcon はアイコンに使う画像が、作成者が上げたまだどこにも添付していないものか確かめる（09 §2）。
func (s *GroupService) checkIcon(ctx context.Context, did, mediaID string) error {
	m, err := spacemodel.GetMedia(ctx, s.DB, mediaID)
	if errors.Is(err, sql.ErrNoRows) {
		return invalidIcon()
	}
	if err != nil {
		return err
	}
	if m.OwnerDID != did || m.SpaceID != nil || m.DeletedAt != nil {
		return invalidIcon()
	}
	return nil
}

// iconURL はグループのアイコン（サムネイル）の署名付き URL。アイコンが無い・消えていれば空文字。
func (s *GroupService) iconURL(ctx context.Context, spaceID string) (string, error) {
	mediaID, err := groupmodel.GetGroupIconMediaID(ctx, s.DB, spaceID)
	if err != nil || mediaID == "" {
		return "", err
	}
	m, err := spacemodel.GetMedia(ctx, s.DB, mediaID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if m.DeletedAt != nil {
		return "", nil
	}
	exp := s.Clock.Now().Add(content.MediaURLTTL).Unix()
	return s.Signer.URL(s.Config.PublicURL, m.ID, storage.VariantThumb, exp), nil
}

// withIcon は詳細にアイコンの URL を足す。
func (s *GroupService) withIcon(ctx context.Context, d *GroupDetail) (*GroupDetail, error) {
	icon, err := s.iconURL(ctx, d.ID)
	if err != nil {
		return nil, err
	}
	d.IconURL = icon
	return d, nil
}

// limitedGroupDetail は非メンバーに見せる最小の項目だけを返す（09 §4）。
func limitedGroupDetail(sp *spacemodel.Space, g *groupmodel.Group, memberCount int) *GroupDetail {
	return &GroupDetail{
		ID: sp.ID, Name: sp.Name, Description: g.Description, Visibility: g.Visibility,
		MemberCount: memberCount,
	}
}

func parseSchedule(raw *string) *schedule.GroupSchedule {
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return nil
	}
	var s schedule.GroupSchedule
	if err := json.Unmarshal([]byte(*raw), &s); err != nil {
		return nil
	}
	return &s
}

func ScheduleJSON(s *schedule.GroupSchedule) *string {
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

func oldestOther(members []spacemodel.SpaceMember, did string) *spacemodel.SpaceMember {
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
