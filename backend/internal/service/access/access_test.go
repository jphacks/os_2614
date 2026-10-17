//go:build integration

package access_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/applemango/chain/internal/httpx"
	"github.com/applemango/chain/internal/service/access"
	"github.com/applemango/chain/internal/testutil"
)

// アクセス判定の表（10 §5、グループは 09 §4）を fake の ZonePresence で確かめる。

func mkParty(t *testing.T, env *testutil.Env, id, phase string) {
	t.Helper()
	now := env.Clock.Now().UnixMilli()
	testutil.Exec(t, env, `INSERT INTO parties (space_id, phase, locked, tentative_at, last_activity_at)
		VALUES (?, ?, 0, ?, ?)`, id, phase, now, now)
}

func TestAccessTable(t *testing.T) {
	env := testutil.NewEnv(t)
	svc := &access.AccessService{Deps: env.Deps, Zones: env.Zones}
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
		mode    access.Mode
		zones   func()
		wantOK  bool
		wantErr string
	}

	// persistent グループ（位置条件なし）。
	testutil.MkSpace(t, env, "g-persist", "jp.i32.chain.space.group", "active")
	testutil.MkGroup(t, env, "g-persist", true, nil)
	testutil.MkMember(t, env, "g-persist", member, true, true, false)

	testutil.MkSpace(t, env, "g-persist-nowrite", "jp.i32.chain.space.group", "active")
	testutil.MkGroup(t, env, "g-persist-nowrite", true, nil)
	testutil.MkMember(t, env, "g-persist-nowrite", member, true, false, false)

	// 非 persistent・schedule なし（位置条件あり）。ケースごとに別の space を使う（判定キャッシュの衝突を避ける）。
	testutil.MkSpace(t, env, "g-geo-in", "jp.i32.chain.space.group", "active")
	testutil.MkGroup(t, env, "g-geo-in", false, nil)
	testutil.MkMember(t, env, "g-geo-in", member, true, true, false)

	testutil.MkSpace(t, env, "g-geo-out", "jp.i32.chain.space.group", "active")
	testutil.MkGroup(t, env, "g-geo-out", false, nil)
	testutil.MkMember(t, env, "g-geo-out", member, true, true, false)

	testutil.MkSpace(t, env, "g-geo-stale", "jp.i32.chain.space.group", "active")
	testutil.MkGroup(t, env, "g-geo-stale", false, nil)
	testutil.MkMember(t, env, "g-geo-stale", member, true, true, false)

	// 非メンバー。
	testutil.MkSpace(t, env, "g-nonmember", "jp.i32.chain.space.group", "active")
	testutil.MkGroup(t, env, "g-nonmember", false, nil)

	// schedule が時間外。
	testutil.MkSpace(t, env, "g-sched-off", "jp.i32.chain.space.group", "active")
	testutil.MkGroup(t, env, "g-sched-off", false, &inactiveSchedule)
	testutil.MkMember(t, env, "g-sched-off", member, true, true, false)

	// schedule が有効・位置条件あり。
	testutil.MkSpace(t, env, "g-sched-on", "jp.i32.chain.space.group", "active")
	testutil.MkGroup(t, env, "g-sched-on", false, &activeSchedule)
	testutil.MkMember(t, env, "g-sched-on", member, true, true, false)

	// 退出済みメンバー（active グループ）。
	testutil.MkSpace(t, env, "g-left", "jp.i32.chain.space.group", "active")
	testutil.MkGroup(t, env, "g-left", true, nil)
	testutil.MkMember(t, env, "g-left", member, true, true, true)

	// can_read = 0。
	testutil.MkSpace(t, env, "g-noread", "jp.i32.chain.space.group", "active")
	testutil.MkGroup(t, env, "g-noread", true, nil)
	testutil.MkMember(t, env, "g-noread", member, false, true, false)

	// archived グループ（退出済みメンバー）。
	testutil.MkSpace(t, env, "g-archived", "jp.i32.chain.space.group", "archived")
	testutil.MkMember(t, env, "g-archived", member, true, false, true)

	// deleted。
	testutil.MkSpace(t, env, "g-deleted", "jp.i32.chain.space.group", "deleted")
	testutil.MkMember(t, env, "g-deleted", member, true, true, false)

	// Party tentative。
	testutil.MkSpace(t, env, "p-tentative", "jp.i32.chain.space.party", "active")
	mkParty(t, env, "p-tentative", "tentative")
	testutil.MkMember(t, env, "p-tentative", member, true, true, false)

	// Party confirmed（未離脱・離脱済み）。
	testutil.MkSpace(t, env, "p-confirmed", "jp.i32.chain.space.party", "active")
	mkParty(t, env, "p-confirmed", "confirmed")
	testutil.MkMember(t, env, "p-confirmed", member, true, true, false)

	testutil.MkSpace(t, env, "p-confirmed-left", "jp.i32.chain.space.party", "active")
	mkParty(t, env, "p-confirmed-left", "confirmed")
	testutil.MkMember(t, env, "p-confirmed-left", member, true, false, true)

	// archived Party（離脱済みメンバー）。
	testutil.MkSpace(t, env, "p-archived", "jp.i32.chain.space.party", "archived")
	testutil.MkMember(t, env, "p-archived", member, true, false, true)

	cases := []testCase{
		{"group active read (persistent)", "g-persist", member, access.ModeRead, nil, true, ""},
		{"group active write (persistent)", "g-persist", member, access.ModeWrite, nil, true, ""},
		{"group active write without can_write", "g-persist-nowrite", member, access.ModeWrite, nil, false, ""},
		{"group active read inside geofence", "g-geo-in", member, access.ModeRead, func() {
			env.Zones.SetStale(false)
			env.Zones.Set(member, true)
		}, true, ""},
		{"group active read outside geofence", "g-geo-out", member, access.ModeRead, func() {
			env.Zones.SetStale(false)
			env.Zones.Set(member, false)
		}, false, "not_in_geofence"},
		{"group active read location stale", "g-geo-stale", member, access.ModeRead, func() {
			env.Zones.SetStale(true)
		}, false, "location_stale"},
		{"group schedule closed", "g-sched-off", member, access.ModeRead, func() {
			env.Zones.SetStale(false)
			env.Zones.Set(member, true)
		}, false, ""},
		{"group schedule open inside", "g-sched-on", member, access.ModeRead, func() {
			env.Zones.SetStale(false)
			env.Zones.Set(member, true)
		}, true, ""},
		{"group left member denied", "g-left", member, access.ModeRead, nil, false, ""},
		{"group can_read=0 denied", "g-noread", member, access.ModeRead, nil, false, ""},
		{"group non-member denied", "g-nonmember", other, access.ModeRead, func() {
			env.Zones.SetStale(false)
			env.Zones.Set(other, true)
		}, false, ""},
		{"group archived read", "g-archived", member, access.ModeRead, nil, true, ""},
		{"group archived write gone", "g-archived", member, access.ModeWrite, nil, false, "gone"},
		{"group deleted not found", "g-deleted", member, access.ModeRead, nil, false, "not_found"},
		{"party tentative read", "p-tentative", member, access.ModeRead, nil, true, ""},
		{"party tentative write denied", "p-tentative", member, access.ModeWrite, nil, false, ""},
		{"party confirmed read active", "p-confirmed", member, access.ModeRead, nil, true, ""},
		{"party confirmed write active", "p-confirmed", member, access.ModeWrite, nil, true, ""},
		{"party confirmed read left member", "p-confirmed-left", member, access.ModeRead, nil, true, ""},
		{"party confirmed write left member denied", "p-confirmed-left", member, access.ModeWrite, nil, false, ""},
		{"party archived read", "p-archived", member, access.ModeRead, nil, true, ""},
		{"party archived write gone", "p-archived", member, access.ModeWrite, nil, false, "gone"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.zones != nil {
				tc.zones()
			}
			ok, err := svc.Check(ctx, tc.spaceID, syntax.DID(tc.did), tc.mode)
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
	svc := &access.AccessService{Deps: env.Deps, Zones: env.Zones}
	ctx := context.Background()

	const member = "did:plc:cache"
	env.CreateUser(t, member, "cache.test")
	env.CreateUser(t, "did:plc:member", "member.test")
	testutil.MkSpace(t, env, "g-cache", "jp.i32.chain.space.group", "active")
	testutil.MkGroup(t, env, "g-cache", true, nil)
	testutil.MkMember(t, env, "g-cache", member, true, true, false)

	ok, err := svc.Check(ctx, "g-cache", member, access.ModeRead)
	if err != nil || !ok {
		t.Fatalf("first Check = (%v, %v), want (true, nil)", ok, err)
	}
	if err := svc.Invalidate(ctx, "g-cache", member); err != nil {
		t.Fatalf("Invalidate: %v", err)
	}
	// 無効化後に拒否へ変えても、キャッシュが消えていれば反映される。
	if _, err := env.DB.ExecContext(ctx, `UPDATE space_members SET can_read = 0 WHERE space_id = ? AND did = ?`, "g-cache", member); err != nil {
		t.Fatalf("update: %v", err)
	}
	ok, err = svc.Check(ctx, "g-cache", member, access.ModeRead)
	if err != nil || ok {
		t.Fatalf("Check after invalidate = (%v, %v), want (false, nil)", ok, err)
	}
}
