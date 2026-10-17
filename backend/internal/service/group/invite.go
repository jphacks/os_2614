package group

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/applemango/chain/internal/config"
	"github.com/applemango/chain/internal/db"
	"github.com/applemango/chain/internal/httpx"
	"github.com/applemango/chain/internal/model"
	friendmodel "github.com/applemango/chain/internal/model/friend"
	groupmodel "github.com/applemango/chain/internal/model/group"
	spacemodel "github.com/applemango/chain/internal/model/space"
	"github.com/applemango/chain/internal/realtime"
	"github.com/applemango/chain/internal/service/access"
	"github.com/applemango/chain/internal/service/core"
	"github.com/applemango/chain/internal/service/notify"
	"github.com/applemango/chain/internal/service/proximity"
)

// GroupInviteService は closed グループの招待（09 §6）。招待を作って承認を集め、
// 必要数に達したら対象者に提示し、対象者の承諾でメンバーにする。
type GroupInviteService struct {
	core.Deps
	Zones  proximity.ZonePresence
	Access *access.AccessService
	Broker realtime.Broker
}

// InviteItem は招待 1 件（09 §6）。RequiredCount は「その時点のメンバー数」で計算した必要承認数。
type InviteItem struct {
	ID            string    `json:"id"`
	TargetDID     string    `json:"targetDid"`
	CreatedBy     string    `json:"createdBy"`
	Status        string    `json:"status"`
	ApprovedCount int       `json:"approvedCount"`
	RequiredCount int       `json:"requiredCount"`
	CreatedAt     time.Time `json:"createdAt"`
	ExpiresAt     time.Time `json:"expiresAt"`
}

type InviteList struct {
	Items []InviteItem `json:"items"`
}

// InviteInboxItem は GET /group-invites の要素（09 §6）。グループ名・招待者・期限。
type InviteInboxItem struct {
	ID        string              `json:"id"`
	SpaceID   string              `json:"spaceId"`
	GroupName string              `json:"groupName"`
	Inviter   *core.AuthorSummary `json:"inviter,omitempty"`
	ExpiresAt time.Time           `json:"expiresAt"`
}

type InboxList struct {
	Items []InviteInboxItem `json:"items"`
}

type groupInvitedPayload struct {
	Text     string `json:"text"`
	SpaceID  string `json:"spaceId"`
	InviteID string `json:"inviteId"`
}

type groupInviteApprovalPayload struct {
	Text      string `json:"text"`
	SpaceID   string `json:"spaceId"`
	InviteID  string `json:"inviteId"`
	TargetDID string `json:"targetDid"`
}

// Create は 09 §6。未退出のメンバーが作る。closed のグループだけ。
// 対象は招待者のフレンドか、revealed な encounter のある相手。招待者の承認を 1 として数える。
func (s *GroupInviteService) Create(ctx context.Context, did, spaceID, targetDID string) (*InviteItem, error) {
	if targetDID == "" {
		return nil, httpx.ErrInvalidRequest.With("fields", map[string]string{"did": "必須です"})
	}
	if targetDID == did {
		return nil, httpx.ErrInvalidRequest.With("fields", map[string]string{"did": "自分は招待できません"})
	}
	sp, group, err := loadGroupSpace(ctx, s.DB, spaceID)
	if err != nil {
		return nil, err
	}
	if sp.State == "archived" {
		return nil, httpx.ErrGone
	}
	if err := s.requireActiveMember(ctx, spaceID, did); err != nil {
		return nil, err
	}
	if group.Visibility != "closed" {
		return nil, httpx.ErrConflict.With("fields", map[string]string{
			"visibility": "open のグループでは招待を作れません",
		})
	}
	blocked, err := model.IsBlocked(ctx, s.DB, did, targetDID)
	if err != nil {
		return nil, err
	}
	if blocked {
		return nil, httpx.ErrBlocked
	}
	if target, err := s.activeMember(ctx, s.DB, spaceID, targetDID); err != nil {
		return nil, err
	} else if target != nil {
		return nil, httpx.ErrConflict.With("fields", map[string]string{"did": "既にメンバーです"})
	}
	invitable, err := s.isInvitable(ctx, did, targetDID)
	if err != nil {
		return nil, err
	}
	if !invitable {
		return nil, httpx.ErrForbidden
	}
	if _, err := groupmodel.GetOpenGroupInvite(ctx, s.DB, spaceID, targetDID); err == nil {
		return nil, httpx.ErrConflict.With("fields", map[string]string{"did": "既に招待しています"})
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}

	now := s.Clock.Now()
	invite := groupmodel.GroupInvite{
		ID: s.IDs.NewID(), SpaceID: spaceID, TargetDID: targetDID, CreatedBy: did,
		Status: "collecting", CreatedAt: now, ExpiresAt: now.Add(core.GroupInviteDuration),
	}
	approval := groupInviteApprovalPayload{
		Text:    s.inviteApprovalText(ctx, sp.Name, did, targetDID),
		SpaceID: spaceID, InviteID: invite.ID, TargetDID: targetDID,
	}
	var notes []notify.NotificationToSend
	if err := db.Transaction(ctx, s.DB, func(tx *sql.Tx) error {
		if err := groupmodel.InsertGroupInvite(ctx, tx, invite); err != nil {
			return err
		}
		if _, err := groupmodel.CreateGroupInviteApproval(ctx, tx, invite.ID, did, now); err != nil {
			return err
		}
		members, err := spacemodel.ListActiveSpaceMembers(ctx, tx, spaceID)
		if err != nil {
			return err
		}
		for _, m := range members {
			if m.DID == did {
				continue
			}
			item, err := notify.EnqueueNotification(ctx, tx, s.Deps, m.DID, notify.NotificationGroupInviteApproval, approval, now)
			if err != nil {
				return err
			}
			if item != nil {
				notes = append(notes, notify.NotificationToSend{DID: m.DID, Item: *item})
			}
		}
		_, err = s.tryOffer(ctx, tx, invite, sp.Name, group.InviteQuorum, now, &notes)
		return err
	}); err != nil {
		return nil, err
	}
	s.publishNotes(ctx, notes)
	return s.item(ctx, invite.ID)
}

// Approve は 09 §6。招待者以外の未退出のメンバーが承認する。二重の承認は 409。
func (s *GroupInviteService) Approve(ctx context.Context, did, spaceID, inviteID string) (*InviteItem, error) {
	sp, group, err := loadGroupSpace(ctx, s.DB, spaceID)
	if err != nil {
		return nil, err
	}
	if sp.State == "archived" {
		return nil, httpx.ErrGone
	}
	if err := s.requireActiveMember(ctx, spaceID, did); err != nil {
		return nil, err
	}
	inv, err := s.inviteInGroup(ctx, spaceID, inviteID)
	if err != nil {
		return nil, err
	}
	now := s.Clock.Now()
	if !now.Before(inv.ExpiresAt) {
		_ = groupmodel.MarkGroupInviteExpired(ctx, s.DB, inv.ID)
		return nil, httpx.ErrGone
	}
	if inv.Status != "collecting" {
		return nil, httpx.ErrConflict
	}

	var notes []notify.NotificationToSend
	if err := db.Transaction(ctx, s.DB, func(tx *sql.Tx) error {
		created, err := groupmodel.CreateGroupInviteApproval(ctx, tx, inv.ID, did, now)
		if err != nil {
			return err
		}
		if !created {
			return httpx.ErrConflict
		}
		_, err = s.tryOffer(ctx, tx, *inv, sp.Name, group.InviteQuorum, now, &notes)
		return err
	}); err != nil {
		return nil, err
	}
	s.publishNotes(ctx, notes)
	return s.item(ctx, inv.ID)
}

// List は 09 §6。メンバーに、collecting の招待と承認済み・必要の人数を返す。
func (s *GroupInviteService) List(ctx context.Context, did, spaceID string) (*InviteList, error) {
	if _, _, err := loadGroupSpace(ctx, s.DB, spaceID); err != nil {
		return nil, err
	}
	if err := s.requireActiveMember(ctx, spaceID, did); err != nil {
		return nil, err
	}
	invs, err := groupmodel.ListCollectingGroupInvites(ctx, s.DB, spaceID)
	if err != nil {
		return nil, err
	}
	items := make([]InviteItem, 0, len(invs))
	for _, inv := range invs {
		item, err := s.itemFor(ctx, s.DB, inv, inv.Status)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return &InviteList{Items: items}, nil
}

// Inbox は 09 §6。自分が対象の offered の招待を返す。
func (s *GroupInviteService) Inbox(ctx context.Context, did string) (*InboxList, error) {
	invs, err := groupmodel.ListOfferedGroupInvites(ctx, s.DB, did)
	if err != nil {
		return nil, err
	}
	now := s.Clock.Now()
	items := make([]InviteInboxItem, 0, len(invs))
	for _, inv := range invs {
		if !now.Before(inv.ExpiresAt) {
			continue
		}
		sp, err := spacemodel.GetSpace(ctx, s.DB, inv.SpaceID)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if sp.State == "deleted" {
			continue
		}
		inviter, err := core.AuthorSummaryFor(ctx, s.DB, inv.CreatedBy)
		if err != nil {
			return nil, err
		}
		items = append(items, InviteInboxItem{
			ID: inv.ID, SpaceID: inv.SpaceID, GroupName: sp.Name, Inviter: inviter, ExpiresAt: inv.ExpiresAt,
		})
	}
	return &InboxList{Items: items}, nil
}

// Accept は 09 §6。対象者が承諾してメンバーになる（退出済みなら再加入）。
// persistent でないグループでは、承諾時に geofence の中にいる必要がある（鮮度 120 秒）。
func (s *GroupInviteService) Accept(ctx context.Context, did, inviteID string) error {
	inv, err := s.inviteByID(ctx, inviteID)
	if err != nil {
		return err
	}
	if inv.TargetDID != did {
		return httpx.ErrForbidden
	}
	sp, group, err := loadGroupSpace(ctx, s.DB, inv.SpaceID)
	if err != nil {
		return err
	}
	if sp.State == "archived" {
		return httpx.ErrGone
	}
	now := s.Clock.Now()
	if !now.Before(inv.ExpiresAt) {
		_ = groupmodel.MarkGroupInviteExpired(ctx, s.DB, inv.ID)
		return httpx.ErrGone
	}
	if inv.Status != "offered" {
		if inv.Status == "collecting" {
			return httpx.ErrConflict
		}
		return httpx.ErrGone
	}
	if !group.Persistent {
		if s.Zones == nil {
			return httpx.ErrLocationStale
		}
		inside, err := s.Zones.Inside(ctx, did, proximity.Zone{
			ID: sp.ID, CenterLat: group.CenterLat, CenterLon: group.CenterLon, RadiusM: group.RadiusM,
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
	}

	existing, gerr := spacemodel.GetSpaceMember(ctx, s.DB, sp.ID, did)
	active := gerr == nil && existing.LeftAt == nil
	hasLeft := gerr == nil && existing.LeftAt != nil
	if gerr != nil && !errors.Is(gerr, sql.ErrNoRows) {
		return gerr
	}
	if err := db.Transaction(ctx, s.DB, func(tx *sql.Tx) error {
		changed, err := groupmodel.SetGroupInviteStatus(ctx, tx, inv.ID, "offered", "accepted")
		if err != nil {
			return err
		}
		if !changed {
			return httpx.ErrGone
		}
		switch {
		case active:
			return nil
		case hasLeft:
			return spacemodel.RejoinSpaceMember(ctx, tx, sp.ID, did, now)
		default:
			return spacemodel.InsertSpaceMember(ctx, tx, spacemodel.SpaceMember{
				SpaceID: sp.ID, DID: did, CanRead: true, CanWrite: true, Role: "member", JoinedAt: now,
			})
		}
	}); err != nil {
		return err
	}

	if err := s.invalidate(ctx, sp.ID, did); err != nil {
		return err
	}
	s.publish(ctx, realtime.Event{Name: "member.joined", Space: sp.ID, Data: map[string]string{"did": did}})
	return nil
}

// Decline は 09 §6。対象者が断る。
func (s *GroupInviteService) Decline(ctx context.Context, did, inviteID string) error {
	inv, err := s.inviteByID(ctx, inviteID)
	if err != nil {
		return err
	}
	if inv.TargetDID != did {
		return httpx.ErrForbidden
	}
	now := s.Clock.Now()
	if !now.Before(inv.ExpiresAt) {
		_ = groupmodel.MarkGroupInviteExpired(ctx, s.DB, inv.ID)
		return httpx.ErrGone
	}
	if inv.Status != "offered" {
		if inv.Status == "collecting" {
			return httpx.ErrConflict
		}
		return httpx.ErrGone
	}
	changed, err := groupmodel.SetGroupInviteStatus(ctx, s.DB, inv.ID, "offered", "declined")
	if err != nil {
		return err
	}
	if !changed {
		return httpx.ErrGone
	}
	return nil
}

// Cancel は 09 §6 の取り消し。招待者だけが、collecting の間だけ取り消せる。
func (s *GroupInviteService) Cancel(ctx context.Context, did, spaceID, inviteID string) error {
	if _, _, err := loadGroupSpace(ctx, s.DB, spaceID); err != nil {
		return err
	}
	inv, err := s.inviteInGroup(ctx, spaceID, inviteID)
	if err != nil {
		return err
	}
	if inv.CreatedBy != did {
		return httpx.ErrForbidden
	}
	if inv.Status != "collecting" {
		return httpx.ErrConflict
	}
	return groupmodel.DeleteGroupInvite(ctx, s.DB, inv.ID)
}

// ReconsiderOnLeave はメンバーの退出で必要承認数が下がった collecting の招待を offered にする（09 §6）。
// 退出の処理の後に呼ぶ。退出の承認も 1 として数え続けるので、承認数は変わらない。
func (s *GroupInviteService) ReconsiderOnLeave(ctx context.Context, spaceID string) error {
	sp, err := spacemodel.GetSpace(ctx, s.DB, spaceID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if sp.State != "active" {
		return nil
	}
	group, err := groupmodel.GetGroup(ctx, s.DB, spaceID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	invs, err := groupmodel.ListCollectingGroupInvites(ctx, s.DB, spaceID)
	if err != nil {
		return err
	}
	for _, inv := range invs {
		now := s.Clock.Now()
		var notes []notify.NotificationToSend
		offered := false
		if err := db.Transaction(ctx, s.DB, func(tx *sql.Tx) error {
			var err error
			offered, err = s.tryOffer(ctx, tx, inv, sp.Name, group.InviteQuorum, now, &notes)
			return err
		}); err != nil {
			return err
		}
		if offered {
			s.publishNotes(ctx, notes)
		}
	}
	return nil
}

// tryOffer は承認数を数え直し、必要数に達していれば offered にして対象者に通知を積む。
func (s *GroupInviteService) tryOffer(ctx context.Context, tx *sql.Tx, invite groupmodel.GroupInvite, groupName string, quorum int, now time.Time, notes *[]notify.NotificationToSend) (bool, error) {
	approved, err := groupmodel.CountGroupInviteApprovals(ctx, tx, invite.ID)
	if err != nil {
		return false, err
	}
	members, err := spacemodel.CountActiveMembers(ctx, tx, invite.SpaceID)
	if err != nil {
		return false, err
	}
	if approved < requiredApprovals(quorum, members) {
		return false, nil
	}
	changed, err := groupmodel.SetGroupInviteStatus(ctx, tx, invite.ID, "collecting", "offered")
	if err != nil {
		return false, err
	}
	if !changed {
		return false, nil
	}
	item, err := notify.EnqueueNotification(ctx, tx, s.Deps, invite.TargetDID, notify.NotificationGroupInvited,
		groupInvitedPayload{Text: invitedText(groupName), SpaceID: invite.SpaceID, InviteID: invite.ID}, now)
	if err != nil {
		return false, err
	}
	if item != nil {
		*notes = append(*notes, notify.NotificationToSend{DID: invite.TargetDID, Item: *item})
	}
	return true, nil
}

func (s *GroupInviteService) item(ctx context.Context, inviteID string) (*InviteItem, error) {
	inv, err := s.inviteByID(ctx, inviteID)
	if err != nil {
		return nil, err
	}
	return s.itemFor(ctx, s.DB, *inv, inv.Status)
}

func (s *GroupInviteService) itemFor(ctx context.Context, q db.Querier, inv groupmodel.GroupInvite, status string) (*InviteItem, error) {
	approved, err := groupmodel.CountGroupInviteApprovals(ctx, q, inv.ID)
	if err != nil {
		return nil, err
	}
	members, err := spacemodel.CountActiveMembers(ctx, q, inv.SpaceID)
	if err != nil {
		return nil, err
	}
	group, err := groupmodel.GetGroup(ctx, q, inv.SpaceID)
	if err != nil {
		return nil, err
	}
	return &InviteItem{
		ID: inv.ID, TargetDID: inv.TargetDID, CreatedBy: inv.CreatedBy, Status: status,
		ApprovedCount: approved, RequiredCount: requiredApprovals(group.InviteQuorum, members),
		CreatedAt: inv.CreatedAt, ExpiresAt: inv.ExpiresAt,
	}, nil
}

func (s *GroupInviteService) inviteByID(ctx context.Context, id string) (*groupmodel.GroupInvite, error) {
	inv, err := groupmodel.GetGroupInvite(ctx, s.DB, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, httpx.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return inv, nil
}

func (s *GroupInviteService) inviteInGroup(ctx context.Context, spaceID, inviteID string) (*groupmodel.GroupInvite, error) {
	inv, err := s.inviteByID(ctx, inviteID)
	if err != nil {
		return nil, err
	}
	if inv.SpaceID != spaceID {
		return nil, httpx.ErrNotFound
	}
	return inv, nil
}

func (s *GroupInviteService) requireActiveMember(ctx context.Context, spaceID, did string) error {
	m, err := s.activeMember(ctx, s.DB, spaceID, did)
	if err != nil {
		return err
	}
	if m == nil {
		return httpx.ErrForbidden
	}
	return nil
}

func (s *GroupInviteService) activeMember(ctx context.Context, q db.Querier, spaceID, did string) (*spacemodel.SpaceMember, error) {
	m, err := spacemodel.GetSpaceMember(ctx, q, spaceID, did)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if m.LeftAt != nil {
		return nil, nil
	}
	return m, nil
}

// isInvitable は対象が招待者のフレンドか、revealed な encounter のある相手かを返す（09 §6）。
func (s *GroupInviteService) isInvitable(ctx context.Context, inviter, target string) (bool, error) {
	if _, err := friendmodel.GetFriendship(ctx, s.DB, inviter, target); err == nil {
		return true, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	return groupmodel.HasRevealedEncounterBetween(ctx, s.DB, inviter, target)
}

func (s *GroupInviteService) inviteApprovalText(ctx context.Context, groupName, inviter, target string) string {
	return displayNameFor(ctx, s.DB, inviter) + " さんが『" + groupName + "』に " +
		displayNameFor(ctx, s.DB, target) + " さんを招待しようとしています"
}

func (s *GroupInviteService) invalidate(ctx context.Context, spaceID, did string) error {
	if s.Access == nil {
		return nil
	}
	return s.Access.Invalidate(ctx, spaceID, did)
}

func (s *GroupInviteService) publish(ctx context.Context, e realtime.Event) {
	if s.Broker != nil {
		s.Broker.Publish(ctx, e)
	}
}

func (s *GroupInviteService) publishNotes(ctx context.Context, notes []notify.NotificationToSend) {
	if s.Broker == nil {
		return
	}
	for _, n := range notes {
		s.Broker.Publish(ctx, realtime.Event{Name: "notification", User: n.DID, Data: n.Item})
	}
}

// loadGroupSpace は Space と group を読み、グループでなければ 404 にする。
func loadGroupSpace(ctx context.Context, q db.Querier, id string) (*spacemodel.Space, *groupmodel.Group, error) {
	sp, err := spacemodel.GetSpace(ctx, q, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, httpx.ErrNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	if sp.State == "deleted" || sp.Type != core.SpaceTypeGroup {
		return nil, nil, httpx.ErrNotFound
	}
	group, err := groupmodel.GetGroup(ctx, q, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, httpx.ErrNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	return sp, group, nil
}

// requiredApprovals は min(invite_quorum, その時点のメンバー数) を返す（09 §6）。
func requiredApprovals(quorum, memberCount int) int {
	if memberCount > 0 && quorum > memberCount {
		return memberCount
	}
	return quorum
}

func invitedText(groupName string) string {
	return "『" + groupName + "』に招待されました"
}

func displayNameFor(ctx context.Context, q *sql.DB, did string) string {
	sum, err := core.AuthorSummaryFor(ctx, q, did)
	if err != nil || sum == nil {
		return did
	}
	if sum.DisplayName != "" {
		return sum.DisplayName
	}
	if sum.Handle != "" {
		return sum.Handle
	}
	return did
}
