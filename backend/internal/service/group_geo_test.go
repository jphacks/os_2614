//go:build integration

package service_test

import (
	"context"
	"testing"

	"github.com/applemango/chain/internal/kv"
	"github.com/applemango/chain/internal/service"
)

// seed で作ったグループは Redis の geo:groups に入らない（09 §5）。
// RebuildGeoIndex が SQLite から組み直し、nearby に出ることを確かめる。

func TestRebuildGeoIndexAddsActiveGroups(t *testing.T) {
	ctx := context.Background()
	h := newGroupHarness(t)
	h.user(alice)
	h.user(bob)

	h.setLocation(alice, groupLat, groupLon)
	live := h.create(alice, service.CreateGroupInput{Name: "seed-live", Lat: groupLat, Lon: groupLon, RadiusM: 40})

	// 400m ほど離した対照群を作り、owner が抜けて archived にする。
	h.setLocation(alice, groupLat+0.004, groupLon)
	archived := h.create(alice, service.CreateGroupInput{Name: "seed-archived", Lat: groupLat + 0.004, Lon: groupLon, RadiusM: 40})
	if err := h.svc.Leave(ctx, alice, archived.ID); err != nil {
		t.Fatalf("Leave: %v", err)
	}

	// seed 直後と同じ状態にする（SQLite にはあるが geo:groups には無い）。
	if err := h.env.Redis.Del(ctx, kv.GeoGroupsKey()).Err(); err != nil {
		t.Fatalf("Del geo:groups: %v", err)
	}
	h.setLocation(bob, groupLat, groupLon)

	res, err := h.svc.Nearby(ctx, bob)
	if err != nil {
		t.Fatalf("Nearby before rebuild: %v", err)
	}
	if containsNearby(res, live.ID) {
		t.Fatalf("rebuild 前なのに nearby に出ている: %+v", res)
	}

	if err := h.svc.RebuildGeoIndex(ctx); err != nil {
		t.Fatalf("RebuildGeoIndex: %v", err)
	}

	res, err = h.svc.Nearby(ctx, bob)
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
