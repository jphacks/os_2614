//go:build integration

package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/applemango/chain/internal/service"
	"github.com/applemango/chain/internal/testutil"
)

var jstZone = time.FixedZone("JST", 9*60*60)

func jstTime(y int, mo time.Month, d, h, mi int) time.Time {
	return time.Date(y, mo, d, h, mi, 0, 0, jstZone)
}

func insertEncounter(t *testing.T, env *testutil.Env, id, a, b string, at time.Time, visibility string) {
	t.Helper()
	aa, bb := a, b
	if aa > bb {
		aa, bb = bb, aa
	}
	if _, err := env.DB.ExecContext(context.Background(), `
		INSERT INTO encounters (id, user_a, user_b, established_at, first_seen_at, last_seen_at, visibility)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, aa, bb, at.UnixMilli(), at.UnixMilli(), at.UnixMilli(), visibility); err != nil {
		t.Fatalf("insert encounter %s: %v", id, err)
	}
}

func summaryFor(t *testing.T, env *testutil.Env, did string) service.EncounterSummary {
	t.Helper()
	enc := &service.EncounterService{Deps: env.Deps}
	sum, err := enc.Summary(context.Background(), did)
	if err != nil {
		t.Fatalf("Summary(%s): %v", did, err)
	}
	return *sum
}

func TestEncounterSummaryHoldbackAndVisibility(t *testing.T) {
	env := testutil.NewEnv(t)
	const me = "did:plc:me"
	env.CreateUser(t, me, "me.test")
	p1, p2, p3 := "did:plc:p1", "did:plc:p2", "did:plc:p3"
	for _, p := range []string{p1, p2, p3} {
		env.CreateUser(t, p, p[8:]+".test")
	}
	now := testutil.Epoch
	env.Clock.Set(now)

	insertEncounter(t, env, "e-59", me, p1, now.Add(-59*time.Minute), "revealed")
	insertEncounter(t, env, "e-61", me, p2, now.Add(-61*time.Minute), "hidden")
	insertEncounter(t, env, "e-61b", me, p3, now.Add(-61*time.Minute), "pending")
	insertEncounter(t, env, "e-yesterday", me, p2, now.Add(-30*time.Hour), "revealed")

	sum := summaryFor(t, env, me)
	if sum.Today != 2 {
		t.Fatalf("today = %d, want 2 (59 分前は数えず 61 分前は数える)", sum.Today)
	}
	if sum.Total != 3 {
		t.Fatalf("total = %d, want 3", sum.Total)
	}
	if sum.UniquePeople != 2 {
		t.Fatalf("uniquePeople = %d, want 2", sum.UniquePeople)
	}
	if sum.Week != 3 || sum.Month != 3 {
		t.Fatalf("week/month = %d/%d, want 3/3", sum.Week, sum.Month)
	}
}

func TestEncounterSummaryJSTBoundaries(t *testing.T) {
	const me = "did:plc:me"
	setup := func(t *testing.T) *testutil.Env {
		env := testutil.NewEnv(t)
		env.CreateUser(t, me, "me.test")
		env.CreateUser(t, "did:plc:p1", "p1.test")
		env.CreateUser(t, "did:plc:p2", "p2.test")
		return env
	}

	t.Run("day starts at JST midnight", func(t *testing.T) {
		env := setup(t)
		env.Clock.Set(jstTime(2026, 10, 17, 12, 0))
		insertEncounter(t, env, "before-day", me, "did:plc:p1", jstTime(2026, 10, 16, 23, 30), "revealed")
		insertEncounter(t, env, "after-day", me, "did:plc:p2", jstTime(2026, 10, 17, 0, 30), "revealed")

		if sum := summaryFor(t, env, me); sum.Today != 1 {
			t.Fatalf("today = %d, want 1 (JST の 0:00 区切り)", sum.Today)
		}
	})

	t.Run("week starts on Monday", func(t *testing.T) {
		env := setup(t)
		env.Clock.Set(jstTime(2026, 10, 19, 12, 0))
		insertEncounter(t, env, "sunday", me, "did:plc:p1", jstTime(2026, 10, 18, 23, 0), "revealed")
		insertEncounter(t, env, "monday", me, "did:plc:p2", jstTime(2026, 10, 19, 0, 30), "revealed")

		if sum := summaryFor(t, env, me); sum.Week != 1 {
			t.Fatalf("week = %d, want 1 (月曜始まり)", sum.Week)
		}
	})

	t.Run("month starts on the 1st", func(t *testing.T) {
		env := setup(t)
		env.Clock.Set(jstTime(2026, 11, 1, 12, 0))
		insertEncounter(t, env, "oct31", me, "did:plc:p1", jstTime(2026, 10, 31, 23, 0), "revealed")
		insertEncounter(t, env, "nov1", me, "did:plc:p2", jstTime(2026, 11, 1, 0, 30), "revealed")

		if sum := summaryFor(t, env, me); sum.Month != 1 {
			t.Fatalf("month = %d, want 1 (1 日区切り)", sum.Month)
		}
	})
}
