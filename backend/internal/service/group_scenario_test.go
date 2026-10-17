//go:build integration

package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/applemango/chain/internal/httpx"
	"github.com/applemango/chain/internal/kv"
	"github.com/applemango/chain/internal/model"
	"github.com/applemango/chain/internal/service"
	"github.com/applemango/chain/internal/testutil"
)

// グループ（09）のシナリオテスト。位置は Redis に直接入れ、時計は env.Clock で進める。

const (
	groupLat = 35.0
	groupLon = 135.0
)

type groupHarness struct {
	t      *testing.T
	env    *testutil.Env
	svc    *service.GroupService
	access *service.AccessService
}

func newGroupHarness(t *testing.T) *groupHarness {
	t.Helper()
	env := testutil.NewEnv(t)
	zones := &service.GPSZonePresence{Deps: env.Deps}
	access := &service.AccessService{Deps: env.Deps, Zones: zones}
	return &groupHarness{
		t:      t,
		env:    env,
		access: access,
		svc:    &service.GroupService{Deps: env.Deps, Zones: zones, Access: access, Moderator: service.ModerationNoop{}},
	}
}

func (h *groupHarness) user(did string) {
	h.t.Helper()
	h.env.CreateUser(h.t, did, did[8:]+".test")
}

func (h *groupHarness) setLocation(did string, lat, lon float64) {
	h.t.Helper()
	loc := model.Location{DID: did, Lat: lat, Lon: lon, AccuracyM: 5, RecordedAt: h.env.Clock.Now(), Source: "gps"}
	if err := model.SaveLocation(context.Background(), h.env.Redis, loc, service.LocationTTL); err != nil {
		h.t.Fatalf("SaveLocation(%s): %v", did, err)
	}
}

func (h *groupHarness) create(did string, in service.CreateGroupInput) *service.GroupDetail {
	h.t.Helper()
	d, err := h.svc.Create(context.Background(), did, in)
	if err != nil {
		h.t.Fatalf("Create(%s): %v", did, err)
	}
	return d
}

func (h *groupHarness) parentOf(spaceID string) *string {
	h.t.Helper()
	var parent *string
	if err := h.env.DB.QueryRowContext(context.Background(),
		"SELECT parent_space_id FROM groups WHERE space_id = ?", spaceID).Scan(&parent); err != nil {
		h.t.Fatalf("parent of %s: %v", spaceID, err)
	}
	return parent
}

func (h *groupHarness) memberRole(spaceID, did string) string {
	h.t.Helper()
	var role string
	if err := h.env.DB.QueryRowContext(context.Background(),
		"SELECT role FROM space_members WHERE space_id = ? AND did = ?", spaceID, did).Scan(&role); err != nil {
		h.t.Fatalf("role of %s in %s: %v", did, spaceID, err)
	}
	return role
}

func (h *groupHarness) spaceState(spaceID string) string {
	h.t.Helper()
	var state string
	if err := h.env.DB.QueryRowContext(context.Background(),
		"SELECT state FROM spaces WHERE id = ?", spaceID).Scan(&state); err != nil {
		h.t.Fatalf("state of %s: %v", spaceID, err)
	}
	return state
}

func TestGroupScenarios(t *testing.T) {
	ctx := context.Background()

	t.Run("G1 geofence の外から参加できない", func(t *testing.T) {
		h := newGroupHarness(t)
		h.user(alice)
		h.user(bob)
		h.setLocation(alice, groupLat, groupLon)
		g := h.create(alice, service.CreateGroupInput{Name: "G1", Lat: groupLat, Lon: groupLon, RadiusM: 40})

		h.setLocation(bob, groupLat+0.01, groupLon)
		wantCode(t, h.svc.Join(ctx, bob, g.ID), "not_in_geofence")
	})

	t.Run("G2 位置が古いと参加できない", func(t *testing.T) {
		h := newGroupHarness(t)
		h.user(alice)
		h.user(bob)
		h.setLocation(alice, groupLat, groupLon)
		g := h.create(alice, service.CreateGroupInput{Name: "G2", Lat: groupLat, Lon: groupLon, RadiusM: 40})

		h.setLocation(bob, groupLat, groupLon)
		h.env.Clock.Advance(3 * time.Minute)
		wantCode(t, h.svc.Join(ctx, bob, g.ID), "location_stale")
	})

	t.Run("G3 参加後 15 分更新がなければ閲覧できない", func(t *testing.T) {
		h := newGroupHarness(t)
		h.user(alice)
		h.user(bob)
		h.setLocation(alice, groupLat, groupLon)
		g := h.create(alice, service.CreateGroupInput{Name: "G3", Lat: groupLat, Lon: groupLon, RadiusM: 40})

		h.setLocation(bob, groupLat, groupLon)
		if err := h.svc.Join(ctx, bob, g.ID); err != nil {
			t.Fatalf("Join: %v", err)
		}
		h.env.Clock.Advance(15 * time.Minute)

		_, err := h.svc.Members(ctx, bob, g.ID)
		wantCode(t, err, "location_stale")
	})

	t.Run("G4 persistent ならエリア外でも閲覧できる", func(t *testing.T) {
		h := newGroupHarness(t)
		h.user(alice)
		h.user(bob)
		h.setLocation(alice, groupLat, groupLon)
		g := h.create(alice, service.CreateGroupInput{Name: "G4", Lat: groupLat, Lon: groupLon, RadiusM: 40, Persistent: true})

		h.setLocation(bob, groupLat, groupLon)
		if err := h.svc.Join(ctx, bob, g.ID); err != nil {
			t.Fatalf("Join: %v", err)
		}
		h.setLocation(bob, groupLat+0.1, groupLon)

		items, err := h.svc.Members(ctx, bob, g.ID)
		if err != nil {
			t.Fatalf("Members: %v", err)
		}
		if len(items) != 2 {
			t.Fatalf("members = %d, want 2", len(items))
		}
	})

	t.Run("G5 schedule の時間外は nearby に出ない", func(t *testing.T) {
		h := newGroupHarness(t)
		h.user(alice)
		weekday := int(testutil.Epoch.Weekday())
		if weekday == 0 {
			weekday = 7
		}
		sched := &service.GroupSchedule{TZ: "UTC", Slots: []service.GroupScheduleSlot{
			{Weekday: weekday, Start: "10:00", End: "11:00"},
		}}

		h.setLocation(alice, groupLat, groupLon)
		g := h.create(alice, service.CreateGroupInput{Name: "G5", Lat: groupLat, Lon: groupLon, RadiusM: 40, Schedule: sched})

		// Epoch は 00:00 UTC なので時間外。300m 離れた schedule なしの対照群は出る。
		controlLat := groupLat + 0.002698
		h.setLocation(alice, controlLat, groupLon)
		control := h.create(alice, service.CreateGroupInput{Name: "G5-control", Lat: controlLat, Lon: groupLon, RadiusM: 40})

		h.setLocation(alice, groupLat, groupLon)
		res, err := h.svc.Nearby(ctx, alice)
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
		h := newGroupHarness(t)
		h.user(alice)
		h.setLocation(alice, groupLat, groupLon)
		first := h.create(alice, service.CreateGroupInput{Name: "既存", Lat: groupLat, Lon: groupLon, RadiusM: 40})

		h.setLocation(alice, groupLat, groupLon)
		_, err := h.svc.Create(ctx, alice, service.CreateGroupInput{Name: "重複", Lat: groupLat, Lon: groupLon, RadiusM: 40})
		var e *httpx.Error
		if !errors.As(err, &e) || e.Code != "conflict" {
			t.Fatalf("error = %v, want conflict", err)
		}
		similar, ok := e.Details["similar"].([]service.SimilarGroup)
		if !ok || len(similar) == 0 || similar[0].ID != first.ID || similar[0].Name != "既存" {
			t.Fatalf("details.similar = %#v, want first group", e.Details["similar"])
		}

		h.setLocation(alice, groupLat, groupLon)
		forced := h.create(alice, service.CreateGroupInput{Name: "強制", Lat: groupLat, Lon: groupLon, RadiusM: 40, Force: true})
		if forced.ID == first.ID {
			t.Fatal("force で作ったグループが既存と同じ id")
		}
	})

	t.Run("owner が退出すると owner が移り、最後の退出で archived", func(t *testing.T) {
		h := newGroupHarness(t)
		h.user(alice)
		h.user(bob)
		h.setLocation(alice, groupLat, groupLon)
		g := h.create(alice, service.CreateGroupInput{Name: "transfer", Lat: groupLat, Lon: groupLon, RadiusM: 40})

		h.setLocation(bob, groupLat, groupLon)
		if err := h.svc.Join(ctx, bob, g.ID); err != nil {
			t.Fatalf("Join: %v", err)
		}
		if err := h.svc.Leave(ctx, alice, g.ID); err != nil {
			t.Fatalf("Leave(owner): %v", err)
		}
		if role := h.memberRole(g.ID, bob); role != "owner" {
			t.Fatalf("bob role = %q, want owner", role)
		}

		if err := h.svc.Leave(ctx, bob, g.ID); err != nil {
			t.Fatalf("Leave(last): %v", err)
		}
		if state := h.spaceState(g.ID); state != "archived" {
			t.Fatalf("state = %q, want archived", state)
		}
		if _, err := h.env.Redis.ZScore(ctx, kv.GeoGroupsKey(), g.ID).Result(); !errors.Is(err, redis.Nil) {
			t.Fatalf("geo:groups に残っている (err=%v)", err)
		}

		h.user(carol)
		h.setLocation(carol, groupLat, groupLon)
		res, err := h.svc.Nearby(ctx, carol)
		if err != nil {
			t.Fatalf("Nearby: %v", err)
		}
		if containsNearby(res, g.ID) {
			t.Fatalf("archived のグループが nearby に出ている: %+v", res)
		}
	})

	t.Run("退出済みは再加入できる", func(t *testing.T) {
		h := newGroupHarness(t)
		h.user(alice)
		h.user(bob)
		h.setLocation(alice, groupLat, groupLon)
		g := h.create(alice, service.CreateGroupInput{Name: "rejoin", Lat: groupLat, Lon: groupLon, RadiusM: 40})

		h.setLocation(bob, groupLat, groupLon)
		if err := h.svc.Join(ctx, bob, g.ID); err != nil {
			t.Fatalf("Join: %v", err)
		}
		if err := h.svc.Leave(ctx, bob, g.ID); err != nil {
			t.Fatalf("Leave: %v", err)
		}
		if err := h.svc.Join(ctx, bob, g.ID); err != nil {
			t.Fatalf("rejoin: %v", err)
		}
		if _, err := h.svc.Members(ctx, alice, g.ID); err != nil {
			t.Fatalf("Members: %v", err)
		}
	})

	t.Run("owner 以外は変更できず、owner は変更できる", func(t *testing.T) {
		h := newGroupHarness(t)
		h.user(alice)
		h.user(bob)
		h.setLocation(alice, groupLat, groupLon)
		g := h.create(alice, service.CreateGroupInput{Name: "patch", Lat: groupLat, Lon: groupLon, RadiusM: 40})

		h.setLocation(bob, groupLat, groupLon)
		if err := h.svc.Join(ctx, bob, g.ID); err != nil {
			t.Fatalf("Join: %v", err)
		}
		name := "changed"
		_, err := h.svc.Update(ctx, bob, g.ID, service.UpdateGroupInput{Name: &name})
		wantCode(t, err, "forbidden")

		detail, err := h.svc.Update(ctx, alice, g.ID, service.UpdateGroupInput{Name: &name})
		if err != nil {
			t.Fatalf("Update(owner): %v", err)
		}
		if detail.Name != "changed" {
			t.Fatalf("name = %q, want changed", detail.Name)
		}
	})

	t.Run("moderation の block は 422", func(t *testing.T) {
		h := newGroupHarness(t)
		h.user(alice)
		h.svc.Moderator = &testutil.BlockingModerator{Words: []string{"ばか"}}
		h.setLocation(alice, groupLat, groupLon)
		_, err := h.svc.Create(ctx, alice, service.CreateGroupInput{Name: "ばか", Lat: groupLat, Lon: groupLon, RadiusM: 40})
		wantCode(t, err, "moderation_blocked")
	})

	t.Run("parent_space_id の入れ子を計算する", func(t *testing.T) {
		h := newGroupHarness(t)
		h.user(alice)
		h.setLocation(alice, groupLat, groupLon)
		a := h.create(alice, service.CreateGroupInput{Name: "A", Lat: groupLat, Lon: groupLon, RadiusM: 800})
		b := h.create(alice, service.CreateGroupInput{Name: "B", Lat: groupLat, Lon: groupLon, RadiusM: 200})
		c := h.create(alice, service.CreateGroupInput{Name: "C", Lat: groupLat, Lon: groupLon, RadiusM: 40})

		if p := h.parentOf(b.ID); p == nil || *p != a.ID {
			t.Fatalf("B parent = %v, want A", p)
		}
		if p := h.parentOf(c.ID); p == nil || *p != b.ID {
			t.Fatalf("C parent = %v, want B", p)
		}

		// A を丸ごと含む D(1000) を足すと A の parent が D になる。
		d := h.create(alice, service.CreateGroupInput{Name: "D", Lat: groupLat, Lon: groupLon, RadiusM: 1000})
		if p := h.parentOf(a.ID); p == nil || *p != d.ID {
			t.Fatalf("A parent = %v, want D", p)
		}
		if p := h.parentOf(b.ID); p == nil || *p != a.ID {
			t.Fatalf("B parent = %v, want A (unchanged)", p)
		}
		if p := h.parentOf(c.ID); p == nil || *p != b.ID {
			t.Fatalf("C parent = %v, want B (unchanged)", p)
		}
	})

	t.Run("closed グループは非メンバーからは見えない", func(t *testing.T) {
		h := newGroupHarness(t)
		h.user(alice)
		h.user(bob)
		h.setLocation(alice, groupLat, groupLon)
		g := h.create(alice, service.CreateGroupInput{Name: "closed", Visibility: "closed", Lat: groupLat, Lon: groupLon, RadiusM: 40})

		if _, err := h.svc.Get(ctx, alice, g.ID); err != nil {
			t.Fatalf("member Get: %v", err)
		}
		_, err := h.svc.Get(ctx, bob, g.ID)
		wantCode(t, err, "not_found")
	})
}

func containsNearby(res *service.NearbyResult, id string) bool {
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
