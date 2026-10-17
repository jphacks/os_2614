//go:build integration

package service_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/applemango/chain/internal/testutil"
)

type digestNotification struct {
	typ     string
	payload string
}

func notificationsFor(t *testing.T, env *testutil.Env, did string) []digestNotification {
	t.Helper()
	rows, err := env.DB.QueryContext(context.Background(),
		"SELECT type, payload_json FROM notifications WHERE did = ? ORDER BY type", did)
	if err != nil {
		t.Fatalf("query notifications: %v", err)
	}
	defer rows.Close()
	var out []digestNotification
	for rows.Next() {
		var n digestNotification
		if err := rows.Scan(&n.typ, &n.payload); err != nil {
			t.Fatalf("scan notification: %v", err)
		}
		out = append(out, n)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	return out
}

func runDigest(t *testing.T, env *testutil.Env, at time.Time) {
	t.Helper()
	if err := env.Digest().Run(context.Background(), at); err != nil {
		t.Fatalf("Digest.Run(%s): %v", at, err)
	}
}

func TestDigestDailyTimingAndDedup(t *testing.T) {
	env := testutil.NewEnv(t)
	const me, peer = "did:plc:me", "did:plc:peer"
	env.CreateUser(t, me, "me.test")
	env.CreateUser(t, peer, "peer.test")
	insertEncounter(t, env, "enc-1", me, peer, jstTime(2026, 10, 14, 10, 0), "revealed")

	runDigest(t, env, jstTime(2026, 10, 14, 20, 59))
	if n := notificationsFor(t, env, me); len(n) != 0 {
		t.Fatalf("20:59 = %+v, want none", n)
	}

	runDigest(t, env, jstTime(2026, 10, 14, 21, 0))
	n := notificationsFor(t, env, me)
	if len(n) != 1 {
		t.Fatalf("21:00 = %+v, want 1 daily", n)
	}
	if n[0].typ != "encounter.daily" {
		t.Fatalf("type = %q, want encounter.daily", n[0].typ)
	}
	if !strings.Contains(n[0].payload, `"period":"2026-10-14"`) {
		t.Fatalf("payload = %s, want period 2026-10-14", n[0].payload)
	}
	if !strings.Contains(n[0].payload, "今日のすれ違い: 1 人") {
		t.Fatalf("payload = %s, want 今日のすれ違い: 1 人", n[0].payload)
	}

	runDigest(t, env, jstTime(2026, 10, 14, 21, 30))
	if n := notificationsFor(t, env, me); len(n) != 1 {
		t.Fatalf("same day 2nd run = %+v, want still 1", n)
	}

	var outbox int
	if err := env.DB.QueryRowContext(context.Background(), `
		SELECT count(*) FROM notification_outbox o
		JOIN notifications n ON n.id = o.notification_id WHERE n.did = ?`, me).Scan(&outbox); err != nil {
		t.Fatalf("count outbox: %v", err)
	}
	if outbox != 1 {
		t.Fatalf("outbox = %d, want 1", outbox)
	}
}

func TestDigestSkipsZeroAndDisabled(t *testing.T) {
	t.Run("zero encounters", func(t *testing.T) {
		env := testutil.NewEnv(t)
		const me = "did:plc:me"
		env.CreateUser(t, me, "me.test")
		runDigest(t, env, jstTime(2026, 10, 14, 21, 0))
		if n := notificationsFor(t, env, me); len(n) != 0 {
			t.Fatalf("zero = %+v, want none", n)
		}
	})

	t.Run("notify off", func(t *testing.T) {
		env := testutil.NewEnv(t)
		const me, peer = "did:plc:me", "did:plc:peer"
		env.CreateUser(t, me, "me.test")
		env.CreateUser(t, peer, "peer.test")
		insertEncounter(t, env, "enc-1", me, peer, jstTime(2026, 10, 14, 10, 0), "revealed")
		if _, err := env.DB.ExecContext(context.Background(),
			"UPDATE user_settings SET notify_json = ? WHERE did = ?", `{"encounter.daily":false}`, me); err != nil {
			t.Fatalf("set notify off: %v", err)
		}
		runDigest(t, env, jstTime(2026, 10, 14, 21, 0))
		if n := notificationsFor(t, env, me); len(n) != 0 {
			t.Fatalf("off = %+v, want none", n)
		}
	})
}

func TestDigestWeeklyOnlyOnMonday(t *testing.T) {
	env := testutil.NewEnv(t)
	const me, peer = "did:plc:me", "did:plc:peer"
	env.CreateUser(t, me, "me.test")
	env.CreateUser(t, peer, "peer.test")
	insertEncounter(t, env, "prev-week", me, peer, jstTime(2026, 10, 15, 12, 0), "revealed")
	insertEncounter(t, env, "today", me, peer, jstTime(2026, 10, 19, 10, 0), "revealed")

	runDigest(t, env, jstTime(2026, 10, 14, 21, 0)) // 水曜
	for _, n := range notificationsFor(t, env, me) {
		if n.typ == "encounter.weekly" {
			t.Fatalf("weekly on Wednesday = %+v", n)
		}
	}

	runDigest(t, env, jstTime(2026, 10, 19, 21, 0)) // 月曜
	byType := map[string]string{}
	for _, n := range notificationsFor(t, env, me) {
		byType[n.typ] = n.payload
	}
	if _, ok := byType["encounter.daily"]; !ok {
		t.Fatalf("notifications = %+v, want daily on Monday", byType)
	}
	weekly, ok := byType["encounter.weekly"]
	if !ok {
		t.Fatalf("notifications = %+v, want weekly on Monday", byType)
	}
	if !strings.Contains(weekly, `"period":"2026-10-12"`) {
		t.Fatalf("weekly payload = %s, want previous week 2026-10-12", weekly)
	}
}

func TestDigestMonthlyOnlyOnFirst(t *testing.T) {
	env := testutil.NewEnv(t)
	const me, peer = "did:plc:me", "did:plc:peer"
	env.CreateUser(t, me, "me.test")
	env.CreateUser(t, peer, "peer.test")
	insertEncounter(t, env, "oct", me, peer, jstTime(2026, 10, 20, 12, 0), "revealed")

	runDigest(t, env, jstTime(2026, 10, 15, 21, 0)) // 1 日ではない
	for _, n := range notificationsFor(t, env, me) {
		if n.typ == "encounter.monthly" {
			t.Fatalf("monthly on the 15th = %+v", n)
		}
	}

	runDigest(t, env, jstTime(2026, 11, 1, 21, 0)) // 1 日
	var monthly string
	for _, n := range notificationsFor(t, env, me) {
		if n.typ == "encounter.monthly" {
			monthly = n.payload
		}
	}
	if monthly == "" {
		t.Fatalf("notifications = %+v, want monthly on the 1st", notificationsFor(t, env, me))
	}
	if !strings.Contains(monthly, `"period":"2026-10"`) || !strings.Contains(monthly, "10 月のすれ違い: 1 人") {
		t.Fatalf("monthly payload = %s, want 2026-10 / 10 月のすれ違い: 1 人", monthly)
	}
}

func TestDigestCatchUpBeforeMidnight(t *testing.T) {
	env := testutil.NewEnv(t)
	const me, peer = "did:plc:me", "did:plc:peer"
	env.CreateUser(t, me, "me.test")
	env.CreateUser(t, peer, "peer.test")
	insertEncounter(t, env, "enc-1", me, peer, jstTime(2026, 10, 14, 10, 0), "revealed")

	runDigest(t, env, jstTime(2026, 10, 14, 23, 59))
	if n := notificationsFor(t, env, me); len(n) != 1 {
		t.Fatalf("23:59 = %+v, want 1 (catch up)", n)
	}

	runDigest(t, env, jstTime(2026, 10, 15, 0, 0))
	if n := notificationsFor(t, env, me); len(n) != 1 {
		t.Fatalf("next day 0:00 = %+v, want still 1 (積まない)", n)
	}
}
