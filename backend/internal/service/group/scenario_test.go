//go:build integration

package group_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/applemango/chain/internal/httpx"
	"github.com/applemango/chain/internal/kv"
	groupsvc "github.com/applemango/chain/internal/service/group"
	"github.com/applemango/chain/internal/service/schedule"
	"github.com/applemango/chain/internal/testutil"
)

// グループ（09）のシナリオテスト。位置は Redis に直接入れ、時計は env.Clock で進める。

const (
	alice = "did:plc:alice"
	bob   = "did:plc:bob"
	carol = "did:plc:carol"
)

const (
	groupLat = 35.0
	groupLon = 135.0
)

func TestGroupScenarios(t *testing.T) {
	ctx := context.Background()

	t.Run("G1 geofence の外から参加できない", func(t *testing.T) {
		h := testutil.NewGroupHarness(t)
		h.User(alice)
		h.User(bob)
		h.SetLocation(alice, groupLat, groupLon)
		g := h.Create(alice, groupsvc.CreateGroupInput{Name: "G1", Lat: groupLat, Lon: groupLon, RadiusM: 40})

		h.SetLocation(bob, groupLat+0.01, groupLon)
		testutil.WantCode(t, h.Svc.Join(ctx, bob, g.ID), "not_in_geofence")
	})

	t.Run("G2 位置が古いと参加できない", func(t *testing.T) {
		h := testutil.NewGroupHarness(t)
		h.User(alice)
		h.User(bob)
		h.SetLocation(alice, groupLat, groupLon)
		g := h.Create(alice, groupsvc.CreateGroupInput{Name: "G2", Lat: groupLat, Lon: groupLon, RadiusM: 40})

		h.SetLocation(bob, groupLat, groupLon)
		h.Env.Clock.Advance(3 * time.Minute)
		testutil.WantCode(t, h.Svc.Join(ctx, bob, g.ID), "location_stale")
	})

	t.Run("G3 参加後 15 分更新がなければ閲覧できない", func(t *testing.T) {
		h := testutil.NewGroupHarness(t)
		h.User(alice)
		h.User(bob)
		h.SetLocation(alice, groupLat, groupLon)
		g := h.Create(alice, groupsvc.CreateGroupInput{Name: "G3", Lat: groupLat, Lon: groupLon, RadiusM: 40})

		h.SetLocation(bob, groupLat, groupLon)
		if err := h.Svc.Join(ctx, bob, g.ID); err != nil {
			t.Fatalf("Join: %v", err)
		}
		h.Env.Clock.Advance(15 * time.Minute)

		_, err := h.Svc.Members(ctx, bob, g.ID)
		testutil.WantCode(t, err, "location_stale")
	})

	t.Run("G4 persistent ならエリア外でも閲覧できる", func(t *testing.T) {
		h := testutil.NewGroupHarness(t)
		h.User(alice)
		h.User(bob)
		h.SetLocation(alice, groupLat, groupLon)
		g := h.Create(alice, groupsvc.CreateGroupInput{Name: "G4", Lat: groupLat, Lon: groupLon, RadiusM: 40, Persistent: true})

		h.SetLocation(bob, groupLat, groupLon)
		if err := h.Svc.Join(ctx, bob, g.ID); err != nil {
			t.Fatalf("Join: %v", err)
		}
		h.SetLocation(bob, groupLat+0.1, groupLon)

		items, err := h.Svc.Members(ctx, bob, g.ID)
		if err != nil {
			t.Fatalf("Members: %v", err)
		}
		if len(items) != 2 {
			t.Fatalf("members = %d, want 2", len(items))
		}
	})

	t.Run("G5 schedule の時間外は nearby に出ない", func(t *testing.T) {
		h := testutil.NewGroupHarness(t)
		h.User(alice)
		weekday := int(testutil.Epoch.Weekday())
		if weekday == 0 {
			weekday = 7
		}
		sched := &schedule.GroupSchedule{TZ: "UTC", Slots: []schedule.GroupScheduleSlot{
			{Weekday: weekday, Start: "10:00", End: "11:00"},
		}}

		h.SetLocation(alice, groupLat, groupLon)
		g := h.Create(alice, groupsvc.CreateGroupInput{Name: "G5", Lat: groupLat, Lon: groupLon, RadiusM: 40, Schedule: sched})

		// Epoch は 00:00 UTC なので時間外。300m 離れた schedule なしの対照群は出る。
		controlLat := groupLat + 0.002698
		h.SetLocation(alice, controlLat, groupLon)
		control := h.Create(alice, groupsvc.CreateGroupInput{Name: "G5-control", Lat: controlLat, Lon: groupLon, RadiusM: 40})

		h.SetLocation(alice, groupLat, groupLon)
		res, err := h.Svc.Nearby(ctx, alice)
		if err != nil {
			t.Fatalf("Nearby: %v", err)
		}
		if containsNearby(res, g.ID) {
			t.Fatalf("schedule 時間外のグループが出ている: %+v", res)
		}
		if !containsNearby(res, control.ID) {
			t.Fatalf("対照群が出ていない: %+v", res)
		}
	})

	t.Run("G6 ほぼ同じ円・同じ時間帯は 409、force で作成できる", func(t *testing.T) {
		h := testutil.NewGroupHarness(t)
		h.User(alice)
		h.SetLocation(alice, groupLat, groupLon)
		first := h.Create(alice, groupsvc.CreateGroupInput{Name: "既存", Lat: groupLat, Lon: groupLon, RadiusM: 40})

		h.SetLocation(alice, groupLat, groupLon)
		_, err := h.Svc.Create(ctx, alice, groupsvc.CreateGroupInput{Name: "重複", Lat: groupLat, Lon: groupLon, RadiusM: 40})
		var e *httpx.Error
		if !errors.As(err, &e) || e.Code != "conflict" {
			t.Fatalf("error = %v, want conflict", err)
		}
		similar, ok := e.Details["similar"].([]groupsvc.SimilarGroup)
		if !ok || len(similar) == 0 || similar[0].ID != first.ID || similar[0].Name != "既存" {
			t.Fatalf("details.similar = %#v, want first group", e.Details["similar"])
		}

		h.SetLocation(alice, groupLat, groupLon)
		forced := h.Create(alice, groupsvc.CreateGroupInput{Name: "強制", Lat: groupLat, Lon: groupLon, RadiusM: 40, Force: true})
		if forced.ID == first.ID {
			t.Fatal("force で作ったグループが既存と同じ id")
		}
	})

	t.Run("owner が退出すると owner が移り、最後の退出で archived", func(t *testing.T) {
		h := testutil.NewGroupHarness(t)
		h.User(alice)
		h.User(bob)
		h.SetLocation(alice, groupLat, groupLon)
		g := h.Create(alice, groupsvc.CreateGroupInput{Name: "transfer", Lat: groupLat, Lon: groupLon, RadiusM: 40})

		h.SetLocation(bob, groupLat, groupLon)
		if err := h.Svc.Join(ctx, bob, g.ID); err != nil {
			t.Fatalf("Join: %v", err)
		}
		if err := h.Svc.Leave(ctx, alice, g.ID); err != nil {
			t.Fatalf("Leave(owner): %v", err)
		}
		if role := h.MemberRole(g.ID, bob); role != "owner" {
			t.Fatalf("bob role = %q, want owner", role)
		}

		if err := h.Svc.Leave(ctx, bob, g.ID); err != nil {
			t.Fatalf("Leave(last): %v", err)
		}
		if state := h.SpaceState(g.ID); state != "archived" {
			t.Fatalf("state = %q, want archived", state)
		}
		if _, err := h.Env.Redis.ZScore(ctx, kv.GeoGroupsKey(), g.ID).Result(); !errors.Is(err, redis.Nil) {
			t.Fatalf("geo:groups に残っている (err=%v)", err)
		}

		h.User(carol)
		h.SetLocation(carol, groupLat, groupLon)
		res, err := h.Svc.Nearby(ctx, carol)
		if err != nil {
			t.Fatalf("Nearby: %v", err)
		}
		if containsNearby(res, g.ID) {
			t.Fatalf("archived のグループが nearby に出ている: %+v", res)
		}
	})

	t.Run("退出済みは再加入できる", func(t *testing.T) {
		h := testutil.NewGroupHarness(t)
		h.User(alice)
		h.User(bob)
		h.SetLocation(alice, groupLat, groupLon)
		g := h.Create(alice, groupsvc.CreateGroupInput{Name: "rejoin", Lat: groupLat, Lon: groupLon, RadiusM: 40})

		h.SetLocation(bob, groupLat, groupLon)
		if err := h.Svc.Join(ctx, bob, g.ID); err != nil {
			t.Fatalf("Join: %v", err)
		}
		if err := h.Svc.Leave(ctx, bob, g.ID); err != nil {
			t.Fatalf("Leave: %v", err)
		}
		if err := h.Svc.Join(ctx, bob, g.ID); err != nil {
			t.Fatalf("rejoin: %v", err)
		}
		if _, err := h.Svc.Members(ctx, alice, g.ID); err != nil {
			t.Fatalf("Members: %v", err)
		}
	})

	t.Run("owner 以外は変更できず、owner は変更できる", func(t *testing.T) {
		h := testutil.NewGroupHarness(t)
		h.User(alice)
		h.User(bob)
		h.SetLocation(alice, groupLat, groupLon)
		g := h.Create(alice, groupsvc.CreateGroupInput{Name: "patch", Lat: groupLat, Lon: groupLon, RadiusM: 40})

		h.SetLocation(bob, groupLat, groupLon)
		if err := h.Svc.Join(ctx, bob, g.ID); err != nil {
			t.Fatalf("Join: %v", err)
		}
		name := "changed"
		_, err := h.Svc.Update(ctx, bob, g.ID, groupsvc.UpdateGroupInput{Name: &name})
		testutil.WantCode(t, err, "forbidden")

		detail, err := h.Svc.Update(ctx, alice, g.ID, groupsvc.UpdateGroupInput{Name: &name})
		if err != nil {
			t.Fatalf("Update(owner): %v", err)
		}
		if detail.Name != "changed" {
			t.Fatalf("name = %q, want changed", detail.Name)
		}
	})

	t.Run("group.updated を Space 宛てに送る", func(t *testing.T) {
		h := testutil.NewGroupHarness(t)
		h.User(alice)
		h.SetLocation(alice, groupLat, groupLon)
		g := h.Create(alice, groupsvc.CreateGroupInput{Name: "before", Lat: groupLat, Lon: groupLon, RadiusM: 40})

		broker := testutil.NewFakeBroker()
		h.Svc.Broker = broker

		name := "after"
		if _, err := h.Svc.Update(ctx, alice, g.ID, groupsvc.UpdateGroupInput{Name: &name}); err != nil {
			t.Fatalf("Update: %v", err)
		}

		events := broker.EventsFor("group.updated", "")
		if len(events) != 1 {
			t.Fatalf("group.updated = %+v, want 1", events)
		}
		if events[0].Space != g.ID || events[0].User != "" {
			t.Fatalf("event = %+v, want Space %s", events[0], g.ID)
		}
		data, ok := events[0].Data.(map[string]string)
		if !ok || data["id"] != g.ID || data["name"] != "after" {
			t.Fatalf("data = %+v, want id=%s name=after", events[0].Data, g.ID)
		}
	})

	t.Run("moderation の block は 422", func(t *testing.T) {
		h := testutil.NewGroupHarness(t)
		h.User(alice)
		h.Svc.Moderator = &testutil.BlockingModerator{Words: []string{"ばか"}}
		h.SetLocation(alice, groupLat, groupLon)
		_, err := h.Svc.Create(ctx, alice, groupsvc.CreateGroupInput{Name: "ばか", Lat: groupLat, Lon: groupLon, RadiusM: 40})
		testutil.WantCode(t, err, "moderation_blocked")
	})

	t.Run("parent_space_id の入れ子を計算する", func(t *testing.T) {
		h := testutil.NewGroupHarness(t)
		h.User(alice)
		h.SetLocation(alice, groupLat, groupLon)
		a := h.Create(alice, groupsvc.CreateGroupInput{Name: "A", Lat: groupLat, Lon: groupLon, RadiusM: 800})
		b := h.Create(alice, groupsvc.CreateGroupInput{Name: "B", Lat: groupLat, Lon: groupLon, RadiusM: 200})
		c := h.Create(alice, groupsvc.CreateGroupInput{Name: "C", Lat: groupLat, Lon: groupLon, RadiusM: 40})

		if p := h.ParentOf(b.ID); p == nil || *p != a.ID {
			t.Fatalf("B parent = %v, want A", p)
		}
		if p := h.ParentOf(c.ID); p == nil || *p != b.ID {
			t.Fatalf("C parent = %v, want B", p)
		}

		// A を丸ごと含む D(1000) を足すと A の parent が D になる。
		d := h.Create(alice, groupsvc.CreateGroupInput{Name: "D", Lat: groupLat, Lon: groupLon, RadiusM: 1000})
		if p := h.ParentOf(a.ID); p == nil || *p != d.ID {
			t.Fatalf("A parent = %v, want D", p)
		}
		if p := h.ParentOf(b.ID); p == nil || *p != a.ID {
			t.Fatalf("B parent = %v, want A (unchanged)", p)
		}
		if p := h.ParentOf(c.ID); p == nil || *p != b.ID {
			t.Fatalf("C parent = %v, want B (unchanged)", p)
		}
	})

	t.Run("closed グループは非メンバーからは見えない", func(t *testing.T) {
		h := testutil.NewGroupHarness(t)
		h.User(alice)
		h.User(bob)
		h.SetLocation(alice, groupLat, groupLon)
		g := h.Create(alice, groupsvc.CreateGroupInput{Name: "closed", Visibility: "closed", Lat: groupLat, Lon: groupLon, RadiusM: 40})

		if _, err := h.Svc.Get(ctx, alice, g.ID); err != nil {
			t.Fatalf("member Get: %v", err)
		}
		_, err := h.Svc.Get(ctx, bob, g.ID)
		testutil.WantCode(t, err, "not_found")
	})
}

func containsNearby(res *groupsvc.NearbyResult, id string) bool {
	for _, it := range res.Inside {
		if it.ID == id {
			return true
		}
	}
	for _, it := range res.Near {
		if it.ID == id {
			return true
		}
	}
	return false
}
