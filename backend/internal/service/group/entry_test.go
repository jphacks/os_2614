//go:build integration

package group_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/applemango/chain/internal/kv"
	"github.com/applemango/chain/internal/model"
	core "github.com/applemango/chain/internal/service/core"
	groupsvc "github.com/applemango/chain/internal/service/group"
	"github.com/applemango/chain/internal/service/notify"
	"github.com/applemango/chain/internal/service/schedule"
	"github.com/applemango/chain/internal/testutil"
)

// 09 §6.1 のエリア入場通知。位置の保存後フックを直接（同期で）呼んで確かめる。

type nearbyNote struct {
	Text    string `json:"text"`
	SpaceID string `json:"spaceId"`
}

func nearbyNotes(t *testing.T, env *testutil.Env, did string) []nearbyNote {
	t.Helper()
	rows, err := env.DB.QueryContext(context.Background(),
		"SELECT payload_json FROM notifications WHERE did = ? AND type = ? ORDER BY created_at, id",
		did, notify.NotificationGroupNearby)
	if err != nil {
		t.Fatalf("notifications の取得: %v", err)
	}
	defer rows.Close()
	var out []nearbyNote
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			t.Fatalf("scan: %v", err)
		}
		var n nearbyNote
		if err := json.Unmarshal([]byte(raw), &n); err != nil {
			t.Fatalf("payload の decode: %v (%s)", err, raw)
		}
		out = append(out, n)
	}
	return out
}

// enterArea は位置を保存し、入場通知のフックを同期で呼ぶ。
func enterArea(t *testing.T, h *testutil.GroupHarness, did string, lat, lon float64) {
	t.Helper()
	loc := model.Location{DID: did, Lat: lat, Lon: lon, AccuracyM: 5, RecordedAt: h.Env.Clock.Now(), Source: "gps"}
	if err := model.SaveLocation(context.Background(), h.Env.Redis, loc, core.LocationTTL); err != nil {
		t.Fatalf("SaveLocation(%s): %v", did, err)
	}
	(&groupsvc.GroupEntryService{Deps: h.Env.Deps}).LocationUpdated(context.Background(), did, loc)
}

func strPtr(s string) *string { return &s }

func TestGroupEntryNotification(t *testing.T) {
	ctx := context.Background()

	t.Run("E1 エリアに入ると 1 回通知し gn を 24 時間立てる", func(t *testing.T) {
		h := testutil.NewGroupHarness(t)
		h.User(alice)
		h.User(bob)
		h.SetLocation(alice, groupLat, groupLon)
		g := h.Create(alice, groupsvc.CreateGroupInput{Name: "情報科学演習 A", Lat: groupLat, Lon: groupLon, RadiusM: 40})

		enterArea(t, h, bob, groupLat, groupLon)
		notes := nearbyNotes(t, h.Env, bob)
		if len(notes) != 1 {
			t.Fatalf("notifications = %d, want 1", len(notes))
		}
		if notes[0].SpaceID != g.ID || !strings.Contains(notes[0].Text, "情報科学演習 A") {
			t.Fatalf("payload = %+v, want space %s とグループ名", notes[0], g.ID)
		}
		ttl, err := h.Env.Redis.TTL(ctx, kv.GroupNotifiedKey(bob, g.ID)).Result()
		if err != nil {
			t.Fatalf("TTL: %v", err)
		}
		if ttl <= 23*time.Hour || ttl > 24*time.Hour {
			t.Fatalf("gn の TTL = %v, want ~24h", ttl)
		}

		// 24 時間以内の再入場では通知しない。
		enterArea(t, h, bob, groupLat, groupLon)
		if n := len(nearbyNotes(t, h.Env, bob)); n != 1 {
			t.Fatalf("24 時間以内の再入場で通知された: %d", n)
		}

		// 24 時間後（印の期限切れ）は再び通知する。
		if err := h.Env.Redis.PExpire(ctx, kv.GroupNotifiedKey(bob, g.ID), 30*time.Millisecond).Err(); err != nil {
			t.Fatalf("PExpire: %v", err)
		}
		time.Sleep(120 * time.Millisecond)
		enterArea(t, h, bob, groupLat, groupLon)
		if n := len(nearbyNotes(t, h.Env, bob)); n != 2 {
			t.Fatalf("印の期限切れ後に通知されない: %d", n)
		}
	})

	t.Run("E2 メンバーには通知しない", func(t *testing.T) {
		h := testutil.NewGroupHarness(t)
		h.User(alice)
		h.User(bob)
		h.SetLocation(alice, groupLat, groupLon)
		g := h.Create(alice, groupsvc.CreateGroupInput{Name: "member", Lat: groupLat, Lon: groupLon, RadiusM: 40})

		h.SetLocation(bob, groupLat, groupLon)
		if err := h.Svc.Join(ctx, bob, g.ID); err != nil {
			t.Fatalf("Join: %v", err)
		}
		enterArea(t, h, bob, groupLat, groupLon)
		if n := len(nearbyNotes(t, h.Env, bob)); n != 0 {
			t.Fatalf("メンバーに通知された: %d", n)
		}
	})

	t.Run("E3 退出済みには通知しない", func(t *testing.T) {
		h := testutil.NewGroupHarness(t)
		h.User(alice)
		h.User(bob)
		h.SetLocation(alice, groupLat, groupLon)
		g := h.Create(alice, groupsvc.CreateGroupInput{Name: "left", Lat: groupLat, Lon: groupLon, RadiusM: 40})

		h.SetLocation(bob, groupLat, groupLon)
		if err := h.Svc.Join(ctx, bob, g.ID); err != nil {
			t.Fatalf("Join: %v", err)
		}
		if err := h.Svc.Leave(ctx, bob, g.ID); err != nil {
			t.Fatalf("Leave: %v", err)
		}
		enterArea(t, h, bob, groupLat, groupLon)
		if n := len(nearbyNotes(t, h.Env, bob)); n != 0 {
			t.Fatalf("退出済みに通知された: %d", n)
		}
	})

	t.Run("E4 closed では通知しない", func(t *testing.T) {
		h := testutil.NewGroupHarness(t)
		h.User(alice)
		h.User(bob)
		h.SetLocation(alice, groupLat, groupLon)
		h.Create(alice, groupsvc.CreateGroupInput{Name: "closed", Visibility: "closed", Lat: groupLat, Lon: groupLon, RadiusM: 40})

		enterArea(t, h, bob, groupLat, groupLon)
		if n := len(nearbyNotes(t, h.Env, bob)); n != 0 {
			t.Fatalf("closed で通知された: %d", n)
		}
	})

	t.Run("E5 schedule の時間外では通知しない", func(t *testing.T) {
		h := testutil.NewGroupHarness(t)
		h.User(alice)
		h.User(bob)
		weekday := int(testutil.Epoch.Weekday())
		if weekday == 0 {
			weekday = 7
		}
		// Epoch は 00:00 UTC なので 10:00–11:00 の枠は時間外。
		sched := &schedule.GroupSchedule{TZ: "UTC", Slots: []schedule.GroupScheduleSlot{
			{Weekday: weekday, Start: "10:00", End: "11:00"},
		}}
		h.SetLocation(alice, groupLat, groupLon)
		h.Create(alice, groupsvc.CreateGroupInput{Name: "sched", Lat: groupLat, Lon: groupLon, RadiusM: 40, Schedule: sched})

		enterArea(t, h, bob, groupLat, groupLon)
		if n := len(nearbyNotes(t, h.Env, bob)); n != 0 {
			t.Fatalf("schedule の時間外で通知された: %d", n)
		}
	})

	t.Run("E6 自動グループでは通知しない", func(t *testing.T) {
		h := testutil.NewGroupHarness(t)
		h.User(alice)
		h.User(bob)
		h.SetLocation(alice, groupLat, groupLon)
		g := h.Create(alice, groupsvc.CreateGroupInput{Name: "natural", Lat: groupLat, Lon: groupLon, RadiusM: 40})
		if _, err := h.Env.DB.ExecContext(ctx, "UPDATE groups SET origin = 'natural' WHERE space_id = ?", g.ID); err != nil {
			t.Fatalf("origin の更新: %v", err)
		}

		enterArea(t, h, bob, groupLat, groupLon)
		if n := len(nearbyNotes(t, h.Env, bob)); n != 0 {
			t.Fatalf("自動グループで通知された: %d", n)
		}
	})

	t.Run("E7 2 つに入ったら小さい方だけ通知する", func(t *testing.T) {
		h := testutil.NewGroupHarness(t)
		h.User(alice)
		h.User(bob)
		h.SetLocation(alice, groupLat, groupLon)
		h.Create(alice, groupsvc.CreateGroupInput{Name: "big", Lat: groupLat, Lon: groupLon, RadiusM: 200})
		small := h.Create(alice, groupsvc.CreateGroupInput{Name: "small", Lat: groupLat, Lon: groupLon, RadiusM: 40})

		enterArea(t, h, bob, groupLat, groupLon)
		notes := nearbyNotes(t, h.Env, bob)
		if len(notes) != 1 {
			t.Fatalf("notifications = %d, want 1", len(notes))
		}
		if notes[0].SpaceID != small.ID {
			t.Fatalf("通知した space = %s, want %s（小さい方）", notes[0].SpaceID, small.ID)
		}
	})

	t.Run("E8 notify off なら積まない", func(t *testing.T) {
		h := testutil.NewGroupHarness(t)
		h.User(alice)
		h.User(bob)
		h.SetLocation(alice, groupLat, groupLon)
		h.Create(alice, groupsvc.CreateGroupInput{Name: "off", Lat: groupLat, Lon: groupLon, RadiusM: 40})

		if err := model.UpdateSettings(ctx, h.Env.DB, bob,
			model.SettingsPatch{NotifyJSON: strPtr(`{"group.nearby":false}`)}, h.Env.Clock.Now()); err != nil {
			t.Fatalf("UpdateSettings: %v", err)
		}
		enterArea(t, h, bob, groupLat, groupLon)
		if n := len(nearbyNotes(t, h.Env, bob)); n != 0 {
			t.Fatalf("notify off なのに通知された: %d", n)
		}
	})

	t.Run("E9 notification イベントも配る", func(t *testing.T) {
		h := testutil.NewGroupHarness(t)
		h.User(alice)
		h.User(bob)
		h.SetLocation(alice, groupLat, groupLon)
		h.Create(alice, groupsvc.CreateGroupInput{Name: "event", Lat: groupLat, Lon: groupLon, RadiusM: 40})

		broker := testutil.NewFakeBroker()
		loc := model.Location{DID: bob, Lat: groupLat, Lon: groupLon, AccuracyM: 5, RecordedAt: h.Env.Clock.Now(), Source: "gps"}
		if err := model.SaveLocation(ctx, h.Env.Redis, loc, core.LocationTTL); err != nil {
			t.Fatalf("SaveLocation: %v", err)
		}
		(&groupsvc.GroupEntryService{Deps: h.Env.Deps, Broker: broker}).LocationUpdated(ctx, bob, loc)
		if events := broker.EventsFor("notification", bob); len(events) != 1 {
			t.Fatalf("notification イベント = %d, want 1", len(events))
		}
	})
}
