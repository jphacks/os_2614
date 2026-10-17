//go:build integration

package group_test

import (
	"context"
	"testing"

	"github.com/applemango/chain/internal/kv"
	groupsvc "github.com/applemango/chain/internal/service/group"
	"github.com/applemango/chain/internal/testutil"
)

// seed で作ったグループは Redis の geo:groups に入らない（09 §5）。
// RebuildGeoIndex が SQLite から組み直し、nearby に出ることを確かめる。

func TestRebuildGeoIndexAddsActiveGroups(t *testing.T) {
	ctx := context.Background()
	h := testutil.NewGroupHarness(t)
	h.User(alice)
	h.User(bob)

	h.SetLocation(alice, groupLat, groupLon)
	live := h.Create(alice, groupsvc.CreateGroupInput{Name: "seed-live", Lat: groupLat, Lon: groupLon, RadiusM: 40})

	// 400m ほど離した対照群を作り、owner が抜けて archived にする。
	h.SetLocation(alice, groupLat+0.004, groupLon)
	archived := h.Create(alice, groupsvc.CreateGroupInput{Name: "seed-archived", Lat: groupLat + 0.004, Lon: groupLon, RadiusM: 40})
	if err := h.Svc.Leave(ctx, alice, archived.ID); err != nil {
		t.Fatalf("Leave: %v", err)
	}

	// seed 直後と同じ状態にする（SQLite にはあるが geo:groups には無い）。
	if err := h.Env.Redis.Del(ctx, kv.GeoGroupsKey()).Err(); err != nil {
		t.Fatalf("Del geo:groups: %v", err)
	}
	h.SetLocation(bob, groupLat, groupLon)

	res, err := h.Svc.Nearby(ctx, bob)
	if err != nil {
		t.Fatalf("Nearby before rebuild: %v", err)
	}
	if containsNearby(res, live.ID) {
		t.Fatalf("rebuild 前なのに nearby に出ている: %+v", res)
	}

	if err := h.Svc.RebuildGeoIndex(ctx); err != nil {
		t.Fatalf("RebuildGeoIndex: %v", err)
	}

	res, err = h.Svc.Nearby(ctx, bob)
	if err != nil {
		t.Fatalf("Nearby after rebuild: %v", err)
	}
	if !containsNearby(res, live.ID) {
		t.Fatalf("rebuild 後に active なグループが nearby に出ない: %+v", res)
	}
	if containsNearby(res, archived.ID) {
		t.Fatalf("rebuild が archived のグループまで載せている: %+v", res)
	}
}
