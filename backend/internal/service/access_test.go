//go:build integration

package service_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/applemango/chain/internal/httpx"
	"github.com/applemango/chain/internal/service"
	"github.com/applemango/chain/internal/testutil"
)

// アクセス判定の表（10 §5、グループは 09 §4）を fake の ZonePresence で確かめる。

func exec(t *testing.T, env *testutil.Env, query string, args ...any) {
	t.Helper()
	if _, err := env.DB.ExecContext(context.Background(), query, args...); err != nil {
		t.Fatalf("exec %q: %v", query, err)
	}
}

func mkSpace(t *testing.T, env *testutil.Env, id, typ, state string) {
	t.Helper()
	now := env.Clock.Now().UnixMilli()
	exec(t, env, `INSERT INTO spaces (id, type, authority, ref, name, state, created_by, created_at)
		VALUES (?, ?, 'did:web:chain.test', ?, ?, ?, ?, ?)`,
		id, typ, "at://did:web:chain.test/space/"+typ+"/"+id, "space "+id, state, "did:plc:member", now)
}

func mkGroup(t *testing.T, env *testutil.Env, id string, persistent bool, schedule *string) {
	t.Helper()
	exec(t, env, `INSERT INTO groups (space_id, description, visibility, origin, persistent, center_lat, center_lon, radius_m, schedule_json)
		VALUES (?, '', 'open', 'explicit', ?, 35.0, 135.0, 40, ?)`, id, persistent, schedule)
}

func mkParty(t *testing.T, env *testutil.Env, id, phase string) {
	t.Helper()
	now := env.Clock.Now().UnixMilli()
	exec(t, env, `INSERT INTO parties (space_id, phase, locked, tentative_at, last_activity_at)
		VALUES (?, ?, 0, ?, ?)`, id, phase, now, now)
}

func mkMember(t *testing.T, env *testutil.Env, id, did string, canRead, canWrite, left bool) {
	t.Helper()
	now := env.Clock.Now().UnixMilli()
	var leftAt any
	if left {
		leftAt = now
	}
	exec(t, env, `INSERT INTO space_members (space_id, did, can_read, can_write, role, joined_at, left_at)
		VALUES (?, ?, ?, ?, 'member', ?, ?)`, id, did, canRead, canWrite, now, leftAt)
}

func TestAccessTable(t *testing.T) {
	env := testutil.NewEnv(t)
	access := &service.AccessService{Deps: env.Deps, Zones: env.Zones}
	ctx := context.Background()

	const member = "did:plc:member"
	const other = "did:plc:other"

	env.CreateUser(t, member, "member.test")
	env.CreateUser(t, other, "other.test")

	weekday := int(testutil.Epoch.Weekday())
	if weekday == 0 {
		weekday = 7
	}
	activeSchedule := fmt.Sprintf(`{"tz":"UTC","slots":[{"weekday":%d,"start":"00:00","end":"23:59"}]}`, weekday)
	inactiveSchedule := fmt.Sprintf(`{"tz":"UTC","slots":[{"weekday":%d,"start":"10:00","end":"11:00"}]}`, weekday)

	type testCase struct {
		name    string
		spaceID string
		did     string
		mode    service.Mode
		zones   func()
		wantOK  bool
		wantErr string
	}

	// persistent グループ（位置条件なし）。
	mkSpace(t, env, "g-persist", "jp.i32.chain.space.group", "active")
	mkGroup(t, env, "g-persist", true, nil)
	mkMember(t, env, "g-persist", member, true, true, false)

	mkSpace(t, env, "g-persist-nowrite", "jp.i32.chain.space.group", "active")
	mkGroup(t, env, "g-persist-nowrite", true, nil)
	mkMember(t, env, "g-persist-nowrite", member, true, false, false)

	// 非 persistent・schedule なし（位置条件あり）。ケースごとに別の space を使う（判定キャッシュの衝突を避ける）。
	mkSpace(t, env, "g-geo-in", "jp.i32.chain.space.group", "active")
	mkGroup(t, env, "g-geo-in", false, nil)
	mkMember(t, env, "g-geo-in", member, true, true, false)

	mkSpace(t, env, "g-geo-out", "jp.i32.chain.space.group", "active")
	mkGroup(t, env, "g-geo-out", false, nil)
	mkMember(t, env, "g-geo-out", member, true, true, false)

	mkSpace(t, env, "g-geo-stale", "jp.i32.chain.space.group", "active")
	mkGroup(t, env, "g-geo-stale", false, nil)
	mkMember(t, env, "g-geo-stale", member, true, true, false)

	// 非メンバー。
	mkSpace(t, env, "g-nonmember", "jp.i32.chain.space.group", "active")
	mkGroup(t, env, "g-nonmember", false, nil)

	// schedule が時間外。
	mkSpace(t, env, "g-sched-off", "jp.i32.chain.space.group", "active")
	mkGroup(t, env, "g-sched-off", false, &inactiveSchedule)
	mkMember(t, env, "g-sched-off", member, true, true, false)

	// schedule が有効・位置条件あり。
	mkSpace(t, env, "g-sched-on", "jp.i32.chain.space.group", "active")
	mkGroup(t, env, "g-sched-on", false, &activeSchedule)
	mkMember(t, env, "g-sched-on", member, true, true, false)

	// 退出済みメンバー（active グループ）。
	mkSpace(t, env, "g-left", "jp.i32.chain.space.group", "active")
	mkGroup(t, env, "g-left", true, nil)
	mkMember(t, env, "g-left", member, true, true, true)

	// can_read = 0。
	mkSpace(t, env, "g-noread", "jp.i32.chain.space.group", "active")
	mkGroup(t, env, "g-noread", true, nil)
	mkMember(t, env, "g-noread", member, false, true, false)

	// archived グループ（退出済みメンバー）。
	mkSpace(t, env, "g-archived", "jp.i32.chain.space.group", "archived")
	mkMember(t, env, "g-archived", member, true, false, true)

	// deleted。
	mkSpace(t, env, "g-deleted", "jp.i32.chain.space.group", "deleted")
	mkMember(t, env, "g-deleted", member, true, true, false)

	// Party tentative。
	mkSpace(t, env, "p-tentative", "jp.i32.chain.space.party", "active")
	mkParty(t, env, "p-tentative", "tentative")
	mkMember(t, env, "p-tentative", member, true, true, false)

	// Party confirmed（未離脱・離脱済み）。
	mkSpace(t, env, "p-confirmed", "jp.i32.chain.space.party", "active")
	mkParty(t, env, "p-confirmed", "confirmed")
	mkMember(t, env, "p-confirmed", member, true, true, false)

	mkSpace(t, env, "p-confirmed-left", "jp.i32.chain.space.party", "active")
	mkParty(t, env, "p-confirmed-left", "confirmed")
	mkMember(t, env, "p-confirmed-left", member, true, false, true)

	// archived Party（離脱済みメンバー）。
	mkSpace(t, env, "p-archived", "jp.i32.chain.space.party", "archived")
	mkMember(t, env, "p-archived", member, true, false, true)

	cases := []testCase{
		{"group active read (persistent)", "g-persist", member, service.ModeRead, nil, true, ""},
		{"group active write (persistent)", "g-persist", member, service.ModeWrite, nil, true, ""},
		{"group active write without can_write", "g-persist-nowrite", member, service.ModeWrite, nil, false, ""},
		{"group active read inside geofence", "g-geo-in", member, service.ModeRead, func() {
			env.Zones.SetStale(false)
			env.Zones.Set(member, true)
		}, true, ""},
		{"group active read outside geofence", "g-geo-out", member, service.ModeRead, func() {
			env.Zones.SetStale(false)
			env.Zones.Set(member, false)
		}, false, "not_in_geofence"},
		{"group active read location stale", "g-geo-stale", member, service.ModeRead, func() {
			env.Zones.SetStale(true)
		}, false, "location_stale"},
		{"group schedule closed", "g-sched-off", member, service.ModeRead, func() {
			env.Zones.SetStale(false)
			env.Zones.Set(member, true)
		}, false, ""},
		{"group schedule open inside", "g-sched-on", member, service.ModeRead, func() {
			env.Zones.SetStale(false)
			env.Zones.Set(member, true)
		}, true, ""},
		{"group left member denied", "g-left", member, service.ModeRead, nil, false, ""},
		{"group can_read=0 denied", "g-noread", member, service.ModeRead, nil, false, ""},
		{"group non-member denied", "g-nonmember", other, service.ModeRead, func() {
			env.Zones.SetStale(false)
			env.Zones.Set(other, true)
		}, false, ""},
		{"group archived read", "g-archived", member, service.ModeRead, nil, true, ""},
		{"group archived write gone", "g-archived", member, service.ModeWrite, nil, false, "gone"},
		{"group deleted not found", "g-deleted", member, service.ModeRead, nil, false, "not_found"},
		{"party tentative read", "p-tentative", member, service.ModeRead, nil, true, ""},
		{"party tentative write denied", "p-tentative", member, service.ModeWrite, nil, false, ""},
		{"party confirmed read active", "p-confirmed", member, service.ModeRead, nil, true, ""},
		{"party confirmed write active", "p-confirmed", member, service.ModeWrite, nil, true, ""},
		{"party confirmed read left member", "p-confirmed-left", member, service.ModeRead, nil, true, ""},
		{"party confirmed write left member denied", "p-confirmed-left", member, service.ModeWrite, nil, false, ""},
		{"party archived read", "p-archived", member, service.ModeRead, nil, true, ""},
		{"party archived write gone", "p-archived", member, service.ModeWrite, nil, false, "gone"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.zones != nil {
				tc.zones()
			}
			ok, err := access.Check(ctx, tc.spaceID, syntax.DID(tc.did), tc.mode)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("Check = (%v, nil), want error %q", ok, tc.wantErr)
				}
				e, isHTTP := err.(*httpx.Error)
				if !isHTTP || e.Code != tc.wantErr {
					t.Fatalf("Check error = %v, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Check error = %v, want nil", err)
			}
			if ok != tc.wantOK {
				t.Fatalf("Check = %v, want %v", ok, tc.wantOK)
			}
		})
	}
}

// TestAccessCacheInvalidation は許可 60 秒 / 拒否 10 秒のキャッシュと、無効化を確かめる（10 §5）。
func TestAccessCacheInvalidation(t *testing.T) {
	env := testutil.NewEnv(t)
	access := &service.AccessService{Deps: env.Deps, Zones: env.Zones}
	ctx := context.Background()

	const member = "did:plc:cache"
	env.CreateUser(t, member, "cache.test")
	env.CreateUser(t, "did:plc:member", "member.test")
	mkSpace(t, env, "g-cache", "jp.i32.chain.space.group", "active")
	mkGroup(t, env, "g-cache", true, nil)
	mkMember(t, env, "g-cache", member, true, true, false)

	ok, err := access.Check(ctx, "g-cache", member, service.ModeRead)
	if err != nil || !ok {
		t.Fatalf("first Check = (%v, %v), want (true, nil)", ok, err)
	}
	if err := access.Invalidate(ctx, "g-cache", member); err != nil {
		t.Fatalf("Invalidate: %v", err)
	}
	// 無効化後に拒否へ変えても、キャッシュが消えていれば反映される。
	if _, err := env.DB.ExecContext(ctx, `UPDATE space_members SET can_read = 0 WHERE space_id = ? AND did = ?`, "g-cache", member); err != nil {
		t.Fatalf("update: %v", err)
	}
	ok, err = access.Check(ctx, "g-cache", member, service.ModeRead)
	if err != nil || ok {
		t.Fatalf("Check after invalidate = (%v, %v), want (false, nil)", ok, err)
	}
}
