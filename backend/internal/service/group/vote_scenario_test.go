//go:build integration

package group_test

import (
	"context"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/applemango/chain/internal/kv"
	"github.com/applemango/chain/internal/model"
	groupmodel "github.com/applemango/chain/internal/model/group"
	"github.com/applemango/chain/internal/service/access"
	core "github.com/applemango/chain/internal/service/core"
	groupsvc "github.com/applemango/chain/internal/service/group"
	"github.com/applemango/chain/internal/service/moderation"
	"github.com/applemango/chain/internal/service/notify"
	"github.com/applemango/chain/internal/service/proximity"
	"github.com/applemango/chain/internal/testutil"
	"github.com/applemango/chain/internal/worker"
)

const (
	voteLat = 35.0
	voteLon = 135.0
)

type voteHarness struct {
	t      *testing.T
	env    *testutil.Env
	groups *groupsvc.GroupService
	votes  *groupsvc.GroupVoteService
	access *access.AccessService
	broker *testutil.FakeBroker
}

func newVoteHarness(t *testing.T) *voteHarness {
	t.Helper()
	env := testutil.NewEnv(t)
	zones := &proximity.GPSZonePresence{Deps: env.Deps}
	access := &access.AccessService{Deps: env.Deps, Zones: zones}
	broker := testutil.NewFakeBroker()
	return &voteHarness{
		t: t, env: env, access: access, broker: broker,
		groups: &groupsvc.GroupService{Deps: env.Deps, Zones: zones, Access: access, Moderator: moderation.ModerationNoop{}, Broker: broker},
		votes:  &groupsvc.GroupVoteService{Deps: env.Deps, Access: access, Broker: broker},
	}
}

func (h *voteHarness) user(did string) {
	h.t.Helper()
	h.env.CreateUser(h.t, did, did[8:]+".test")
}

func (h *voteHarness) setLocation(did string) {
	h.t.Helper()
	loc := model.Location{DID: did, Lat: voteLat, Lon: voteLon, AccuracyM: 5, RecordedAt: h.env.Clock.Now(), Source: "gps"}
	if err := model.SaveLocation(context.Background(), h.env.Redis, loc, core.LocationTTL); err != nil {
		h.t.Fatalf("SaveLocation(%s): %v", did, err)
	}
}

func (h *voteHarness) create(owner string, in groupsvc.CreateGroupInput) *groupsvc.GroupDetail {
	h.t.Helper()
	h.setLocation(owner)
	d, err := h.groups.Create(context.Background(), owner, in)
	if err != nil {
		h.t.Fatalf("Create(%s): %v", owner, err)
	}
	return d
}

func (h *voteHarness) join(did, id string) {
	h.t.Helper()
	h.setLocation(did)
	if err := h.groups.Join(context.Background(), did, id); err != nil {
		h.t.Fatalf("Join(%s): %v", did, err)
	}
}

func (h *voteHarness) voteStatus(voteID string) string {
	h.t.Helper()
	v, err := groupmodel.GetGroupVote(context.Background(), h.env.DB, voteID)
	if err != nil {
		h.t.Fatalf("GetGroupVote(%s): %v", voteID, err)
	}
	return v.Status
}

func (h *voteHarness) visibility(id string) string {
	h.t.Helper()
	g, err := groupmodel.GetGroup(context.Background(), h.env.DB, id)
	if err != nil {
		h.t.Fatalf("GetGroup(%s): %v", id, err)
	}
	return g.Visibility
}

func (h *voteHarness) persistent(id string) bool {
	h.t.Helper()
	g, err := groupmodel.GetGroup(context.Background(), h.env.DB, id)
	if err != nil {
		h.t.Fatalf("GetGroup(%s): %v", id, err)
	}
	return g.Persistent
}

func (h *voteHarness) notifications(typ string) []string {
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

func TestGroupVotePropose(t *testing.T) {
	ctx := context.Background()

	t.Run("非メンバーは発議できない", func(t *testing.T) {
		h := newVoteHarness(t)
		h.user("did:plc:alice")
		h.user("did:plc:tom")
		g := h.create("did:plc:alice", groupsvc.CreateGroupInput{Name: "G", Lat: voteLat, Lon: voteLon, RadiusM: 40})

		_, err := h.votes.Propose(ctx, "did:plc:tom", g.ID, "make_closed")
		testutil.WantCode(t, err, "forbidden")
	})

	t.Run("同じ種類の二重発議は 409", func(t *testing.T) {
		h := newVoteHarness(t)
		h.user("did:plc:alice")
		g := h.create("did:plc:alice", groupsvc.CreateGroupInput{Name: "G", Lat: voteLat, Lon: voteLon, RadiusM: 40})

		if _, err := h.votes.Propose(ctx, "did:plc:alice", g.ID, "make_closed"); err != nil {
			t.Fatalf("Propose: %v", err)
		}
		_, err := h.votes.Propose(ctx, "did:plc:alice", g.ID, "make_closed")
		testutil.WantCode(t, err, "conflict")

		// 種類が違えば開いていても発議できる。
		if _, err := h.votes.Propose(ctx, "did:plc:alice", g.ID, "make_persistent"); err != nil {
			t.Fatalf("Propose(make_persistent): %v", err)
		}
	})

	t.Run("既に closed なら make_closed は 409", func(t *testing.T) {
		h := newVoteHarness(t)
		h.user("did:plc:alice")
		g := h.create("did:plc:alice", groupsvc.CreateGroupInput{
			Name: "G", Visibility: "closed", Lat: voteLat, Lon: voteLon, RadiusM: 40,
		})

		_, err := h.votes.Propose(ctx, "did:plc:alice", g.ID, "make_closed")
		testutil.WantCode(t, err, "conflict")
	})

	t.Run("不正な kind は 400", func(t *testing.T) {
		h := newVoteHarness(t)
		h.user("did:plc:alice")
		g := h.create("did:plc:alice", groupsvc.CreateGroupInput{Name: "G", Lat: voteLat, Lon: voteLon, RadiusM: 40})

		_, err := h.votes.Propose(ctx, "did:plc:alice", g.ID, "make_tea")
		testutil.WantCode(t, err, "invalid_request")
	})

	t.Run("発議者の自動賛成と group.vote_opened の配信", func(t *testing.T) {
		h := newVoteHarness(t)
		h.user("did:plc:alice")
		h.user("did:plc:bob")
		h.user("did:plc:carol")
		g := h.create("did:plc:alice", groupsvc.CreateGroupInput{Name: "G", Lat: voteLat, Lon: voteLon, RadiusM: 40})
		h.join("did:plc:bob", g.ID)
		h.join("did:plc:carol", g.ID)

		item, err := h.votes.Propose(ctx, "did:plc:alice", g.ID, "make_closed")
		if err != nil {
			t.Fatalf("Propose: %v", err)
		}
		if item.Status != "open" || item.YesCount != 1 || item.NoCount != 0 {
			t.Fatalf("item = %+v, want open 1/0", item)
		}
		if item.RequiredCount != 2 {
			t.Fatalf("RequiredCount = %d, want 2", item.RequiredCount)
		}
		if item.MyBallot == nil || !*item.MyBallot {
			t.Fatalf("MyBallot = %v, want true", item.MyBallot)
		}

		var yes int
		if err := h.env.DB.QueryRowContext(ctx,
			"SELECT yes FROM group_vote_ballots WHERE vote_id = ? AND did = ?", item.ID, "did:plc:alice").Scan(&yes); err != nil {
			t.Fatalf("ballot: %v", err)
		}
		if yes != 1 {
			t.Fatalf("ballot yes = %d, want 1", yes)
		}

		events := h.broker.EventsFor("group.vote_opened", "")
		if len(events) != 1 || events[0].Space != g.ID {
			t.Fatalf("vote_opened events = %+v, want 1 for %s", events, g.ID)
		}
	})
}

func TestGroupVotePassClosed(t *testing.T) {
	ctx := context.Background()
	h := newVoteHarness(t)
	h.user("did:plc:alice")
	h.user("did:plc:bob")
	h.user("did:plc:carol")
	g := h.create("did:plc:alice", groupsvc.CreateGroupInput{Name: "教室", Lat: voteLat, Lon: voteLon, RadiusM: 40})
	h.join("did:plc:bob", g.ID)
	h.join("did:plc:carol", g.ID)

	// 可決の前にアクセス判定のキャッシュを作っておく。
	if _, err := h.access.Check(ctx, g.ID, syntax.DID("did:plc:alice"), access.ModeRead); err != nil {
		t.Fatalf("Access.Check: %v", err)
	}
	cacheKey := kv.AccessKey(g.ID, "did:plc:alice", "read")
	if n, err := h.env.Redis.Exists(ctx, cacheKey).Result(); err != nil || n != 1 {
		t.Fatalf("cache exists = %d (%v), want 1", n, err)
	}

	item, err := h.votes.Propose(ctx, "did:plc:alice", g.ID, "make_closed")
	if err != nil {
		t.Fatalf("Propose: %v", err)
	}
	item, err = h.votes.CastBallot(ctx, "did:plc:bob", g.ID, item.ID, true)
	if err != nil {
		t.Fatalf("CastBallot: %v", err)
	}
	if item.Status != "passed" || item.YesCount != 2 {
		t.Fatalf("item = %+v, want passed 2 yes", item)
	}
	if v := h.visibility(g.ID); v != "closed" {
		t.Fatalf("visibility = %q, want closed", v)
	}
	if n, err := h.env.Redis.Exists(ctx, cacheKey).Result(); err != nil || n != 0 {
		t.Fatalf("cache exists = %d (%v), want 0 (invalidated)", n, err)
	}

	closed := h.broker.EventsFor("group.vote_closed", "")
	if len(closed) != 1 || closed[0].Space != g.ID {
		t.Fatalf("vote_closed events = %+v, want 1 for %s", closed, g.ID)
	}
	dids := h.notifications(notify.NotificationGroupVotePassed)
	if len(dids) != 3 {
		t.Fatalf("group.vote_passed notifications = %v, want 3", dids)
	}
}

func TestGroupVotePassPersistent(t *testing.T) {
	ctx := context.Background()
	h := newVoteHarness(t)
	h.user("did:plc:alice")
	h.user("did:plc:bob")
	h.user("did:plc:carol")
	g := h.create("did:plc:alice", groupsvc.CreateGroupInput{Name: "G", Lat: voteLat, Lon: voteLon, RadiusM: 40})
	h.join("did:plc:bob", g.ID)
	h.join("did:plc:carol", g.ID)

	item, err := h.votes.Propose(ctx, "did:plc:alice", g.ID, "make_persistent")
	if err != nil {
		t.Fatalf("Propose: %v", err)
	}
	if item, err = h.votes.CastBallot(ctx, "did:plc:bob", g.ID, item.ID, true); err != nil {
		t.Fatalf("CastBallot: %v", err)
	}
	if item.Status != "passed" {
		t.Fatalf("status = %q, want passed", item.Status)
	}
	if !h.persistent(g.ID) {
		t.Fatal("persistent = false, want true")
	}
	if v := h.visibility(g.ID); v != "open" {
		t.Fatalf("visibility = %q, want open (unchanged)", v)
	}
}

func TestGroupVoteMajorityAfterMemberLeaves(t *testing.T) {
	ctx := context.Background()
	h := newVoteHarness(t)
	for _, did := range []string{"did:plc:alice", "did:plc:bob", "did:plc:carol", "did:plc:dave"} {
		h.user(did)
	}
	g := h.create("did:plc:alice", groupsvc.CreateGroupInput{Name: "G", Lat: voteLat, Lon: voteLon, RadiusM: 40})
	h.join("did:plc:bob", g.ID)
	h.join("did:plc:carol", g.ID)
	h.join("did:plc:dave", g.ID)

	item, err := h.votes.Propose(ctx, "did:plc:alice", g.ID, "make_closed")
	if err != nil {
		t.Fatalf("Propose: %v", err)
	}
	item, err = h.votes.CastBallot(ctx, "did:plc:bob", g.ID, item.ID, true)
	if err != nil {
		t.Fatalf("CastBallot(bob): %v", err)
	}
	if item.Status != "open" {
		t.Fatalf("status = %q, want open (2/4 は過半数でない)", item.Status)
	}

	// dave が退出して 3 人になる。判定は次の票で行う。
	if err := h.groups.Leave(ctx, "did:plc:dave", g.ID); err != nil {
		t.Fatalf("Leave: %v", err)
	}
	item, err = h.votes.CastBallot(ctx, "did:plc:carol", g.ID, item.ID, false)
	if err != nil {
		t.Fatalf("CastBallot(carol): %v", err)
	}
	if item.Status != "passed" {
		t.Fatalf("status = %q, want passed (2/3)", item.Status)
	}
	if v := h.visibility(g.ID); v != "closed" {
		t.Fatalf("visibility = %q, want closed", v)
	}
}

func TestGroupVoteRevote(t *testing.T) {
	ctx := context.Background()
	h := newVoteHarness(t)
	for _, did := range []string{"did:plc:alice", "did:plc:bob", "did:plc:carol", "did:plc:dave"} {
		h.user(did)
	}
	g := h.create("did:plc:alice", groupsvc.CreateGroupInput{Name: "G", Lat: voteLat, Lon: voteLon, RadiusM: 40})
	h.join("did:plc:bob", g.ID)
	h.join("did:plc:carol", g.ID)
	h.join("did:plc:dave", g.ID)

	item, err := h.votes.Propose(ctx, "did:plc:alice", g.ID, "make_closed")
	if err != nil {
		t.Fatalf("Propose: %v", err)
	}
	if item, err = h.votes.CastBallot(ctx, "did:plc:bob", g.ID, item.ID, false); err != nil {
		t.Fatalf("CastBallot(no): %v", err)
	}
	if item.YesCount != 1 || item.NoCount != 1 {
		t.Fatalf("after no: yes=%d no=%d, want 1/1", item.YesCount, item.NoCount)
	}
	item, err = h.votes.CastBallot(ctx, "did:plc:bob", g.ID, item.ID, true)
	if err != nil {
		t.Fatalf("CastBallot(yes): %v", err)
	}
	if item.YesCount != 2 || item.NoCount != 0 || item.Status != "open" {
		t.Fatalf("after re-vote: %+v, want 2/0 open", item)
	}
	if item.MyBallot == nil || !*item.MyBallot {
		t.Fatalf("MyBallot = %v, want true", item.MyBallot)
	}
}

func TestGroupVoteFailedByWorker(t *testing.T) {
	ctx := context.Background()
	h := newVoteHarness(t)
	h.user("did:plc:alice")
	h.user("did:plc:bob")
	h.user("did:plc:carol")
	g := h.create("did:plc:alice", groupsvc.CreateGroupInput{Name: "G", Lat: voteLat, Lon: voteLon, RadiusM: 40})
	h.join("did:plc:bob", g.ID)
	h.join("did:plc:carol", g.ID)

	item, err := h.votes.Propose(ctx, "did:plc:alice", g.ID, "make_closed")
	if err != nil {
		t.Fatalf("Propose: %v", err)
	}

	h.env.Clock.Advance(core.GroupVoteDuration + time.Minute)
	w := &worker.GroupVotes{Deps: h.env.Deps, Votes: h.votes}
	if err := w.Tick(ctx); err != nil {
		t.Fatalf("worker Tick: %v", err)
	}

	if s := h.voteStatus(item.ID); s != "failed" {
		t.Fatalf("status = %q, want failed", s)
	}
	if v := h.visibility(g.ID); v != "open" {
		t.Fatalf("visibility = %q, want open (unchanged)", v)
	}
	closed := h.broker.EventsFor("group.vote_closed", "")
	if len(closed) != 1 {
		t.Fatalf("vote_closed events = %+v, want 1", closed)
	}
	if dids := h.notifications(notify.NotificationGroupVotePassed); len(dids) != 0 {
		t.Fatalf("notifications = %v, want none for failed vote", dids)
	}

	_, err = h.votes.CastBallot(ctx, "did:plc:bob", g.ID, item.ID, true)
	testutil.WantCode(t, err, "gone")
}

func TestGroupVoteBallotOnClosedIsGone(t *testing.T) {
	ctx := context.Background()
	h := newVoteHarness(t)
	h.user("did:plc:alice")
	h.user("did:plc:bob")
	h.user("did:plc:carol")
	g := h.create("did:plc:alice", groupsvc.CreateGroupInput{Name: "G", Lat: voteLat, Lon: voteLon, RadiusM: 40})
	h.join("did:plc:bob", g.ID)
	h.join("did:plc:carol", g.ID)

	item, err := h.votes.Propose(ctx, "did:plc:alice", g.ID, "make_closed")
	if err != nil {
		t.Fatalf("Propose: %v", err)
	}
	if _, err := h.votes.CastBallot(ctx, "did:plc:bob", g.ID, item.ID, true); err != nil {
		t.Fatalf("CastBallot: %v", err)
	}
	_, err = h.votes.CastBallot(ctx, "did:plc:carol", g.ID, item.ID, true)
	testutil.WantCode(t, err, "gone")
}

func TestGroupVoteList(t *testing.T) {
	ctx := context.Background()
	h := newVoteHarness(t)
	h.user("did:plc:alice")
	h.user("did:plc:bob")
	h.user("did:plc:carol")
	g := h.create("did:plc:alice", groupsvc.CreateGroupInput{Name: "G", Lat: voteLat, Lon: voteLon, RadiusM: 40})
	h.join("did:plc:bob", g.ID)
	h.join("did:plc:carol", g.ID)

	item, err := h.votes.Propose(ctx, "did:plc:alice", g.ID, "make_closed")
	if err != nil {
		t.Fatalf("Propose: %v", err)
	}
	list, err := h.votes.List(ctx, "did:plc:bob", g.ID)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list.Items) != 1 {
		t.Fatalf("items = %+v, want 1", list.Items)
	}
	if list.Items[0].ID != item.ID || list.Items[0].YesCount != 1 || list.Items[0].RequiredCount != 2 {
		t.Fatalf("item = %+v", list.Items[0])
	}
	if list.Items[0].MyBallot != nil {
		t.Fatalf("bob の票 = %v, want nil", list.Items[0].MyBallot)
	}

	_, err = h.votes.List(ctx, "did:plc:tom", g.ID)
	testutil.WantCode(t, err, "forbidden")
}

func TestGroupVoteNotificationOptOut(t *testing.T) {
	ctx := context.Background()
	h := newVoteHarness(t)
	h.user("did:plc:alice")
	h.user("did:plc:bob")
	h.user("did:plc:carol")
	g := h.create("did:plc:alice", groupsvc.CreateGroupInput{Name: "G", Lat: voteLat, Lon: voteLon, RadiusM: 40})
	h.join("did:plc:bob", g.ID)
	h.join("did:plc:carol", g.ID)

	if _, err := h.env.DB.ExecContext(ctx,
		"UPDATE user_settings SET notify_json = ? WHERE did = ?",
		`{"group.vote_passed":false}`, "did:plc:bob"); err != nil {
		t.Fatalf("set notify_json: %v", err)
	}

	item, err := h.votes.Propose(ctx, "did:plc:alice", g.ID, "make_closed")
	if err != nil {
		t.Fatalf("Propose: %v", err)
	}
	if _, err := h.votes.CastBallot(ctx, "did:plc:carol", g.ID, item.ID, true); err != nil {
		t.Fatalf("CastBallot: %v", err)
	}
	dids := h.notifications(notify.NotificationGroupVotePassed)
	want := map[string]bool{"did:plc:alice": true, "did:plc:carol": true}
	if len(dids) != 2 {
		t.Fatalf("notifications = %v, want alice and carol", dids)
	}
	for _, did := range dids {
		if !want[did] {
			t.Fatalf("想定外の宛先 %q (dids=%v)", did, dids)
		}
	}
}
