//go:build integration

package service_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/applemango/chain/internal/kv"
	"github.com/applemango/chain/internal/model"
	"github.com/applemango/chain/internal/service"
	"github.com/applemango/chain/internal/testutil"
)

// closed グループの招待（09 §6）のシナリオテスト。

const (
	inviteLat = 35.1
	inviteLon = 135.1
)

type inviteHarness struct {
	t       *testing.T
	env     *testutil.Env
	groups  *service.GroupService
	invites *service.GroupInviteService
	access  *service.AccessService
	broker  *testutil.FakeBroker
}

func newInviteHarness(t *testing.T) *inviteHarness {
	t.Helper()
	env := testutil.NewEnv(t)
	zones := &service.GPSZonePresence{Deps: env.Deps}
	access := &service.AccessService{Deps: env.Deps, Zones: zones}
	broker := testutil.NewFakeBroker()
	invites := &service.GroupInviteService{Deps: env.Deps, Zones: zones, Access: access, Broker: broker}
	groups := &service.GroupService{
		Deps: env.Deps, Zones: zones, Access: access, Moderator: service.ModerationNoop{}, Broker: broker, Invites: invites,
	}
	return &inviteHarness{t: t, env: env, groups: groups, invites: invites, access: access, broker: broker}
}

func (h *inviteHarness) user(did string) {
	h.t.Helper()
	h.env.CreateUser(h.t, did, did[8:]+".test")
}

func (h *inviteHarness) setLocation(did string, lat, lon float64) {
	h.t.Helper()
	loc := model.Location{DID: did, Lat: lat, Lon: lon, AccuracyM: 5, RecordedAt: h.env.Clock.Now(), Source: "gps"}
	if err := model.SaveLocation(context.Background(), h.env.Redis, loc, service.LocationTTL); err != nil {
		h.t.Fatalf("SaveLocation(%s): %v", did, err)
	}
}

func (h *inviteHarness) makeFriends(a, b string) {
	h.t.Helper()
	x, y := service.OrderDIDs(a, b)
	if err := model.CreateFriendship(context.Background(), h.env.DB, x, y, "invite", h.env.Clock.Now()); err != nil {
		h.t.Fatalf("CreateFriendship(%s,%s): %v", a, b, err)
	}
}

// closedGroup は owner が作り、members が参加した open グループを作って closed に切り替える。
func (h *inviteHarness) closedGroup(owner string, members []string, quorum int) string {
	h.t.Helper()
	ctx := context.Background()
	h.user(owner)
	h.setLocation(owner, inviteLat, inviteLon)
	d, err := h.groups.Create(ctx, owner, service.CreateGroupInput{
		Name: "G", Lat: inviteLat, Lon: inviteLon, RadiusM: 40, InviteQuorum: quorum,
	})
	if err != nil {
		h.t.Fatalf("Create(%s): %v", owner, err)
	}
	for _, m := range members {
		h.user(m)
		h.setLocation(m, inviteLat, inviteLon)
		if err := h.groups.Join(ctx, m, d.ID); err != nil {
			h.t.Fatalf("Join(%s): %v", m, err)
		}
	}
	closed := "closed"
	if _, err := h.groups.Update(ctx, owner, d.ID, service.UpdateGroupInput{Visibility: &closed}); err != nil {
		h.t.Fatalf("Update(closed): %v", err)
	}
	return d.ID
}

func (h *inviteHarness) createInvite(by, space, target string) *service.InviteItem {
	h.t.Helper()
	item, err := h.invites.Create(context.Background(), by, space, target)
	if err != nil {
		h.t.Fatalf("Create(%s -> %s): %v", by, target, err)
	}
	return item
}

func (h *inviteHarness) createErr(by, space, target string) error {
	h.t.Helper()
	_, err := h.invites.Create(context.Background(), by, space, target)
	return err
}

func (h *inviteHarness) status(inviteID string) string {
	h.t.Helper()
	inv, err := model.GetGroupInvite(context.Background(), h.env.DB, inviteID)
	if err != nil {
		h.t.Fatalf("GetGroupInvite(%s): %v", inviteID, err)
	}
	return inv.Status
}

func (h *inviteHarness) memberCount(spaceID string) int {
	h.t.Helper()
	n, err := model.CountActiveMembers(context.Background(), h.env.DB, spaceID)
	if err != nil {
		h.t.Fatalf("CountActiveMembers: %v", err)
	}
	return n
}

func (h *inviteHarness) notifications(typ string) []string {
	h.t.Helper()
	rows, err := h.env.DB.QueryContext(context.Background(),
		"SELECT did FROM notifications WHERE type = ? ORDER BY did", typ)
	if err != nil {
		h.t.Fatalf("notifications: %v", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var did string
		if err := rows.Scan(&did); err != nil {
			h.t.Fatalf("scan notification: %v", err)
		}
		out = append(out, did)
	}
	return out
}

func TestInviteQuorumFlow(t *testing.T) {
	ctx := context.Background()
	h := newInviteHarness(t)
	g := h.closedGroup("did:plc:alice", []string{"did:plc:bob", "did:plc:carol"}, 2)
	h.user("did:plc:dave")
	h.makeFriends("did:plc:alice", "did:plc:dave")

	// 招待者の作成は 1 票。必要数は min(2, 3) = 2 なので collecting。
	item := h.createInvite("did:plc:alice", g, "did:plc:dave")
	if item.Status != "collecting" || item.ApprovedCount != 1 || item.RequiredCount != 2 {
		t.Fatalf("item = %+v, want collecting 1/2", item)
	}

	// 2 人目の承認で offered。
	item, err := h.invites.Approve(ctx, "did:plc:bob", g, item.ID)
	if err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if item.Status != "offered" || item.ApprovedCount != 2 {
		t.Fatalf("item = %+v, want offered 2", item)
	}

	// 対象者が承諾してメンバーになる。
	h.setLocation("did:plc:dave", inviteLat, inviteLon)
	if err := h.invites.Accept(ctx, "did:plc:dave", item.ID); err != nil {
		t.Fatalf("Accept: %v", err)
	}
	if n := h.memberCount(g); n != 4 {
		t.Fatalf("members = %d, want 4", n)
	}
	if s := h.status(item.ID); s != "accepted" {
		t.Fatalf("status = %q, want accepted", s)
	}
}

func TestInviteRequiredIsMinOfQuorumAndMembers(t *testing.T) {
	ctx := context.Background()
	h := newInviteHarness(t)

	t.Run("quorum 3・メンバー 2 なら必要数は 2", func(t *testing.T) {
		g := h.closedGroup("did:plc:alice", []string{"did:plc:bob"}, 3)
		h.user("did:plc:dave")
		h.makeFriends("did:plc:alice", "did:plc:dave")

		item := h.createInvite("did:plc:alice", g, "did:plc:dave")
		if item.RequiredCount != 2 {
			t.Fatalf("required = %d, want 2", item.RequiredCount)
		}
		if item.Status != "collecting" {
			t.Fatalf("status = %q, want collecting", item.Status)
		}
		approved, err := h.invites.Approve(ctx, "did:plc:bob", g, item.ID)
		if err != nil {
			t.Fatalf("Approve: %v", err)
		}
		if approved.Status != "offered" {
			t.Fatalf("status = %q, want offered", approved.Status)
		}
	})

	t.Run("メンバーの退出で必要数が下がると offered になる", func(t *testing.T) {
		g := h.closedGroup("did:plc:alice", []string{"did:plc:bob", "did:plc:carol"}, 3)
		h.user("did:plc:erin")
		h.makeFriends("did:plc:alice", "did:plc:erin")

		item := h.createInvite("did:plc:alice", g, "did:plc:erin")
		if item.RequiredCount != 3 {
			t.Fatalf("required = %d, want 3", item.RequiredCount)
		}
		if _, err := h.invites.Approve(ctx, "did:plc:bob", g, item.ID); err != nil {
			t.Fatalf("Approve: %v", err)
		}
		if s := h.status(item.ID); s != "collecting" {
			t.Fatalf("status = %q, want collecting (2/3)", s)
		}

		if err := h.groups.Leave(ctx, "did:plc:carol", g); err != nil {
			t.Fatalf("Leave: %v", err)
		}
		if s := h.status(item.ID); s != "offered" {
			t.Fatalf("status = %q, want offered after leave (2/2)", s)
		}
	})
}

func TestInviteTargetConditions(t *testing.T) {
	ctx := context.Background()
	h := newInviteHarness(t)
	g := h.closedGroup("did:plc:alice", []string{"did:plc:bob"}, 2)

	h.user("did:plc:tom")
	wantCode(t, h.createErr("did:plc:alice", g, "did:plc:tom"), "forbidden")

	h.user("did:plc:dave")
	h.makeFriends("did:plc:alice", "did:plc:dave")
	if err := model.CreateBlock(ctx, h.env.DB, "did:plc:dave", "did:plc:alice", h.env.Clock.Now()); err != nil {
		t.Fatalf("CreateBlock: %v", err)
	}
	wantCode(t, h.createErr("did:plc:alice", g, "did:plc:dave"), "blocked")

	h.makeFriends("did:plc:alice", "did:plc:bob")
	wantCode(t, h.createErr("did:plc:alice", g, "did:plc:bob"), "conflict")

	h.user("did:plc:erin")
	h.makeFriends("did:plc:alice", "did:plc:erin")
	h.createInvite("did:plc:alice", g, "did:plc:erin")
	wantCode(t, h.createErr("did:plc:alice", g, "did:plc:erin"), "conflict")

	wantCode(t, h.createErr("did:plc:alice", g, "did:plc:alice"), "invalid_request")

	// revealed な encounter のある相手は招待できる。
	h.user("did:plc:frank")
	fa, fb := service.OrderDIDs("did:plc:alice", "did:plc:frank")
	now := h.env.Clock.Now()
	if err := model.CreateEncounter(ctx, h.env.DB, model.Encounter{
		ID: "enc-frank", UserA: fa, UserB: fb, EstablishedAt: now, FirstSeenAt: now, LastSeenAt: now,
		Visibility: "revealed",
	}); err != nil {
		t.Fatalf("CreateEncounter: %v", err)
	}
	if _, err := h.invites.Create(ctx, "did:plc:alice", g, "did:plc:frank"); err != nil {
		t.Fatalf("Create(encounter target): %v", err)
	}
}

func TestInviteApproveConditions(t *testing.T) {
	ctx := context.Background()
	h := newInviteHarness(t)
	g := h.closedGroup("did:plc:alice", []string{"did:plc:bob", "did:plc:carol"}, 3)
	h.user("did:plc:dave")
	h.makeFriends("did:plc:alice", "did:plc:dave")

	item := h.createInvite("did:plc:alice", g, "did:plc:dave")

	// 招待者自身は既に 1 票持っているので 409。
	_, err := h.invites.Approve(ctx, "did:plc:alice", g, item.ID)
	wantCode(t, err, "conflict")

	// 非メンバーは 403。
	_, err = h.invites.Approve(ctx, "did:plc:tom", g, item.ID)
	wantCode(t, err, "forbidden")

	// bob の承認 (2/3) は通り、二度目は 409。
	if _, err := h.invites.Approve(ctx, "did:plc:bob", g, item.ID); err != nil {
		t.Fatalf("Approve(bob): %v", err)
	}
	_, err = h.invites.Approve(ctx, "did:plc:bob", g, item.ID)
	wantCode(t, err, "conflict")
}

func TestInviteAcceptNeedsGeofence(t *testing.T) {
	ctx := context.Background()
	h := newInviteHarness(t)
	g := h.closedGroup("did:plc:alice", []string{"did:plc:bob"}, 1)
	h.user("did:plc:dave")
	h.makeFriends("did:plc:alice", "did:plc:dave")

	item := h.createInvite("did:plc:alice", g, "did:plc:dave")
	if item.Status != "offered" {
		t.Fatalf("status = %q, want offered (quorum 1)", item.Status)
	}

	h.setLocation("did:plc:dave", inviteLat+0.01, inviteLon)
	wantCode(t, h.invites.Accept(ctx, "did:plc:dave", item.ID), "not_in_geofence")

	h.setLocation("did:plc:dave", inviteLat, inviteLon)
	h.env.Clock.Advance(3 * time.Minute)
	wantCode(t, h.invites.Accept(ctx, "did:plc:dave", item.ID), "location_stale")

	h.setLocation("did:plc:dave", inviteLat, inviteLon)
	if err := h.invites.Accept(ctx, "did:plc:dave", item.ID); err != nil {
		t.Fatalf("Accept: %v", err)
	}
}

func TestInviteExpiry(t *testing.T) {
	ctx := context.Background()
	h := newInviteHarness(t)
	g := h.closedGroup("did:plc:alice", []string{"did:plc:bob"}, 1)
	h.user("did:plc:dave")
	h.makeFriends("did:plc:alice", "did:plc:dave")

	item := h.createInvite("did:plc:alice", g, "did:plc:dave")
	h.setLocation("did:plc:dave", inviteLat, inviteLon)
	h.env.Clock.Advance(7*24*time.Hour + time.Second)

	wantCode(t, h.invites.Accept(ctx, "did:plc:dave", item.ID), "gone")
	if s := h.status(item.ID); s != "expired" {
		t.Fatalf("status = %q, want expired", s)
	}
}

func TestInviteCancel(t *testing.T) {
	ctx := context.Background()
	h := newInviteHarness(t)
	g := h.closedGroup("did:plc:alice", []string{"did:plc:bob", "did:plc:carol"}, 3)
	h.user("did:plc:dave")
	h.makeFriends("did:plc:alice", "did:plc:dave")

	item := h.createInvite("did:plc:alice", g, "did:plc:dave")
	wantCode(t, h.invites.Cancel(ctx, "did:plc:bob", g, item.ID), "forbidden")
	if err := h.invites.Cancel(ctx, "did:plc:alice", g, item.ID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if _, err := model.GetGroupInvite(ctx, h.env.DB, item.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("invite still exists after cancel (err=%v)", err)
	}

	// offered になったら取り消せない。
	item = h.createInvite("did:plc:alice", g, "did:plc:dave")
	if _, err := h.invites.Approve(ctx, "did:plc:bob", g, item.ID); err != nil {
		t.Fatalf("Approve(bob): %v", err)
	}
	if _, err := h.invites.Approve(ctx, "did:plc:carol", g, item.ID); err != nil {
		t.Fatalf("Approve(carol): %v", err)
	}
	wantCode(t, h.invites.Cancel(ctx, "did:plc:alice", g, item.ID), "conflict")
}

func TestInviteSurvivesInviterLeaving(t *testing.T) {
	ctx := context.Background()
	h := newInviteHarness(t)
	g := h.closedGroup("did:plc:alice", []string{"did:plc:bob", "did:plc:carol"}, 2)
	h.user("did:plc:dave")
	h.makeFriends("did:plc:alice", "did:plc:dave")

	item := h.createInvite("did:plc:alice", g, "did:plc:dave")

	if err := h.groups.Leave(ctx, "did:plc:alice", g); err != nil {
		t.Fatalf("Leave(alice): %v", err)
	}
	if s := h.status(item.ID); s != "collecting" {
		t.Fatalf("status = %q, want collecting (inviter's vote still counts)", s)
	}
	offered, err := h.invites.Approve(ctx, "did:plc:bob", g, item.ID)
	if err != nil {
		t.Fatalf("Approve(bob): %v", err)
	}
	if offered.Status != "offered" {
		t.Fatalf("status = %q, want offered", offered.Status)
	}

	h.setLocation("did:plc:dave", inviteLat, inviteLon)
	if err := h.invites.Accept(ctx, "did:plc:dave", item.ID); err != nil {
		t.Fatalf("Accept: %v", err)
	}
	if n := h.memberCount(g); n != 3 {
		t.Fatalf("members = %d, want 3 (bob, carol, dave)", n)
	}
}

func TestInviteNotificationsAndEvents(t *testing.T) {
	ctx := context.Background()
	h := newInviteHarness(t)
	g := h.closedGroup("did:plc:alice", []string{"did:plc:bob", "did:plc:carol"}, 2)
	h.user("did:plc:dave")
	h.makeFriends("did:plc:alice", "did:plc:dave")

	item := h.createInvite("did:plc:alice", g, "did:plc:dave")
	dids := h.notifications(service.NotificationGroupInviteApproval)
	if len(dids) != 2 || dids[0] != "did:plc:bob" || dids[1] != "did:plc:carol" {
		t.Fatalf("invite_approval notifications = %v, want [bob carol]", dids)
	}
	if got := h.notifications(service.NotificationGroupInvited); len(got) != 0 {
		t.Fatalf("group.invited before offer = %v, want none", got)
	}

	if _, err := h.invites.Approve(ctx, "did:plc:bob", g, item.ID); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	invited := h.notifications(service.NotificationGroupInvited)
	if len(invited) != 1 || invited[0] != "did:plc:dave" {
		t.Fatalf("group.invited = %v, want [dave]", invited)
	}
	if evs := h.broker.EventsFor("notification", "did:plc:dave"); len(evs) == 0 {
		t.Fatal("notification event for dave not published")
	}

	h.broker.Reset()
	h.setLocation("did:plc:dave", inviteLat, inviteLon)
	if err := h.invites.Accept(ctx, "did:plc:dave", item.ID); err != nil {
		t.Fatalf("Accept: %v", err)
	}
	joined := h.broker.EventsFor("member.joined", "")
	if len(joined) != 1 || joined[0].Space != g {
		t.Fatalf("member.joined events = %+v, want 1 for %s", joined, g)
	}
}

func TestGroupVisibilityToggle(t *testing.T) {
	ctx := context.Background()
	h := newInviteHarness(t)
	h.user("did:plc:alice")
	h.user("did:plc:bob")
	h.setLocation("did:plc:alice", inviteLat, inviteLon)
	d, err := h.groups.Create(ctx, "did:plc:alice", service.CreateGroupInput{
		Name: "G", Lat: inviteLat, Lon: inviteLon, RadiusM: 40,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := h.groups.Get(ctx, "did:plc:bob", d.ID); err != nil {
		t.Fatalf("non-member Get(open): %v", err)
	}

	closed := "closed"
	_, err = h.groups.Update(ctx, "did:plc:bob", d.ID, service.UpdateGroupInput{Visibility: &closed})
	wantCode(t, err, "forbidden")

	cacheKey := kv.AccessKey(d.ID, "did:plc:bob", "read")
	if err := h.env.Redis.Set(ctx, cacheKey, "1", time.Minute).Err(); err != nil {
		t.Fatalf("set cache: %v", err)
	}
	detail, err := h.groups.Update(ctx, "did:plc:alice", d.ID, service.UpdateGroupInput{Visibility: &closed})
	if err != nil {
		t.Fatalf("Update(closed): %v", err)
	}
	if detail.Visibility != "closed" {
		t.Fatalf("visibility = %q, want closed", detail.Visibility)
	}
	if n, err := h.env.Redis.Exists(ctx, cacheKey).Result(); err != nil || n != 0 {
		t.Fatalf("access cache exists = %d (%v), want invalidated", n, err)
	}

	// closed にしたら非メンバーには詳細が出ない。
	wantCode(t, groupGetErr(h, "did:plc:bob", d.ID), "not_found")

	// nearby にも出ない。
	h.setLocation("did:plc:bob", inviteLat, inviteLon)
	res, err := h.groups.Nearby(ctx, "did:plc:bob")
	if err != nil {
		t.Fatalf("Nearby: %v", err)
	}
	if containsNearby(res, d.ID) {
		t.Fatalf("closed グループが nearby に出ている: %+v", res)
	}

	// 不正な visibility は 400。
	bad := "hidden"
	_, err = h.groups.Update(ctx, "did:plc:alice", d.ID, service.UpdateGroupInput{Visibility: &bad})
	wantCode(t, err, "invalid_request")
}

func TestGroupVisibilityOwnerless(t *testing.T) {
	ctx := context.Background()
	h := newInviteHarness(t)
	h.user("did:plc:alice")
	h.user("did:plc:bob")
	h.setLocation("did:plc:alice", inviteLat, inviteLon)
	d, err := h.groups.Create(ctx, "did:plc:alice", service.CreateGroupInput{
		Name: "G", Lat: inviteLat, Lon: inviteLon, RadiusM: 40,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	h.setLocation("did:plc:bob", inviteLat, inviteLon)
	if err := h.groups.Join(ctx, "did:plc:bob", d.ID); err != nil {
		t.Fatalf("Join: %v", err)
	}
	// 自動グループのように owner を置かない（全員 member）。
	if _, err := h.env.DB.ExecContext(ctx,
		"UPDATE space_members SET role = 'member' WHERE space_id = ?", d.ID); err != nil {
		t.Fatalf("strip owner: %v", err)
	}
	closed := "closed"
	_, err = h.groups.Update(ctx, "did:plc:alice", d.ID, service.UpdateGroupInput{Visibility: &closed})
	wantCode(t, err, "forbidden")
}

func groupGetErr(h *inviteHarness, viewer, spaceID string) error {
	h.t.Helper()
	_, err := h.groups.Get(context.Background(), viewer, spaceID)
	return err
}
