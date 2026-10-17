//go:build integration

package service_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/applemango/chain/internal/service"
	"github.com/applemango/chain/internal/testutil"
)

const (
	alice = "did:plc:alice"
	bob   = "did:plc:bob"
)

type encounterRow struct {
	id         string
	visibility string
	revealAt   sql.NullInt64
	firstSeen  int64
	lastSeen   int64
}

type encHarness struct {
	t      *testing.T
	env    *testutil.Env
	beacon *service.BeaconService
	enc    *service.EncounterService
	me     *service.MeService
	blocks *service.BlockService
}

func newEncHarness(t *testing.T) *encHarness {
	t.Helper()
	env := testutil.NewEnv(t)
	return &encHarness{
		t:      t,
		env:    env,
		beacon: &service.BeaconService{Deps: env.Deps},
		enc:    &service.EncounterService{Deps: env.Deps, Proximity: &service.BLEPeerProximity{Deps: env.Deps}},
		me:     &service.MeService{Deps: env.Deps},
		blocks: &service.BlockService{Deps: env.Deps},
	}
}

func (h *encHarness) token(owner string, at time.Time) string {
	h.t.Helper()
	h.env.Clock.Set(at)
	_, tokens, err := h.beacon.Tokens(context.Background(), owner, 1)
	if err != nil {
		h.t.Fatalf("Tokens(%s): %v", owner, err)
	}
	want := at.Truncate(service.BeaconWindow)
	for _, tok := range tokens {
		if tok.WindowStart.Equal(want) {
			return tok.Token
		}
	}
	h.t.Fatalf("no token for window %s", want)
	return ""
}

// sight は observer が owner の token を at に観測した sighting を送る。
func (h *encHarness) sight(observer, owner string, at time.Time) service.SightingResult {
	h.t.Helper()
	tok := h.token(owner, at)
	h.env.Clock.Set(at)
	rssi := -70
	res, err := h.enc.RecordSightings(context.Background(), observer, []service.SightingInput{
		{Token: tok, FirstSeenAt: at, LastSeenAt: at, MaxRssi: &rssi},
	})
	if err != nil {
		h.t.Fatalf("RecordSightings(%s->%s): %v", observer, owner, err)
	}
	return res
}

func (h *encHarness) setMode(did, mode string, delay int) {
	h.t.Helper()
	m, d := mode, delay
	if _, err := h.me.UpdateSettings(context.Background(), did, service.SettingsUpdate{EncounterMode: &m, EncounterDelaySec: &d}); err != nil {
		h.t.Fatalf("UpdateSettings(%s): %v", did, err)
	}
}

func (h *encHarness) rows() []encounterRow {
	h.t.Helper()
	rows, err := h.env.DB.QueryContext(context.Background(),
		"SELECT id, visibility, reveal_at, first_seen_at, last_seen_at FROM encounters ORDER BY established_at")
	if err != nil {
		h.t.Fatalf("query encounters: %v", err)
	}
	defer rows.Close()
	var out []encounterRow
	for rows.Next() {
		var e encounterRow
		if err := rows.Scan(&e.id, &e.visibility, &e.revealAt, &e.firstSeen, &e.lastSeen); err != nil {
			h.t.Fatalf("scan encounter: %v", err)
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		h.t.Fatalf("rows: %v", err)
	}
	return out
}

func (h *encHarness) mustReveal() {
	h.t.Helper()
	if _, err := h.enc.RevealPending(context.Background()); err != nil {
		h.t.Fatalf("RevealPending: %v", err)
	}
}

func (h *encHarness) users(dids ...string) {
	h.t.Helper()
	for _, did := range dids {
		h.env.CreateUser(h.t, did, did[8:]+".test")
	}
}

func TestEncounterScenarios(t *testing.T) {
	t.Run("E1 immediate bidirectional reveals", func(t *testing.T) {
		h := newEncHarness(t)
		h.users(alice, bob)
		h.setMode(alice, "immediate", 0)
		h.setMode(bob, "immediate", 0)

		if res := h.sight(alice, bob, testutil.Epoch); res.Accepted != 1 {
			t.Fatalf("first sighting = %+v", res)
		}
		h.sight(bob, alice, testutil.Epoch.Add(5*time.Minute))
		h.mustReveal()

		rows := h.rows()
		if len(rows) != 1 {
			t.Fatalf("encounters = %d, want 1", len(rows))
		}
		if rows[0].visibility != "revealed" {
			t.Fatalf("visibility = %q, want revealed", rows[0].visibility)
		}
	})

	t.Run("E2 outside MatchWindow does not establish", func(t *testing.T) {
		h := newEncHarness(t)
		h.users(alice, bob)
		h.setMode(alice, "immediate", 0)
		h.setMode(bob, "immediate", 0)

		h.sight(alice, bob, testutil.Epoch)
		h.sight(bob, alice, testutil.Epoch.Add(15*time.Minute))

		if rows := h.rows(); len(rows) != 0 {
			t.Fatalf("encounters = %d, want 0", len(rows))
		}
	})

	t.Run("E3 one direction only does not establish", func(t *testing.T) {
		h := newEncHarness(t)
		h.users(alice, bob)
		h.setMode(alice, "immediate", 0)

		h.sight(alice, bob, testutil.Epoch)
		h.sight(alice, bob, testutil.Epoch.Add(5*time.Minute))

		if rows := h.rows(); len(rows) != 0 {
			t.Fatalf("encounters = %d, want 0", len(rows))
		}
	})

	t.Run("E4 within MergeGap merges", func(t *testing.T) {
		h := newEncHarness(t)
		h.users(alice, bob)
		h.setMode(alice, "immediate", 0)
		h.setMode(bob, "immediate", 0)

		base := testutil.Epoch
		h.sight(alice, bob, base)
		h.sight(bob, alice, base.Add(5*time.Minute))

		second := base.Add(25 * time.Minute)
		h.sight(alice, bob, second)
		h.sight(bob, alice, second.Add(5*time.Minute))

		if rows := h.rows(); len(rows) != 1 {
			t.Fatalf("encounters = %d, want 1 (merged)", len(rows))
		}
	})

	t.Run("E5 beyond MergeGap is a new encounter", func(t *testing.T) {
		h := newEncHarness(t)
		h.users(alice, bob)
		h.setMode(alice, "immediate", 0)
		h.setMode(bob, "immediate", 0)

		base := testutil.Epoch
		h.sight(alice, bob, base)
		h.sight(bob, alice, base.Add(5*time.Minute))

		second := base.Add(45 * time.Minute)
		h.sight(alice, bob, second)
		h.sight(bob, alice, second.Add(5*time.Minute))

		if rows := h.rows(); len(rows) != 2 {
			t.Fatalf("encounters = %d, want 2", len(rows))
		}
	})

	t.Run("E6 longer delay wins", func(t *testing.T) {
		h := newEncHarness(t)
		h.users(alice, bob)
		h.setMode(alice, "delayed", 3600)
		h.setMode(bob, "delayed", 86400)

		base := testutil.Epoch
		h.sight(alice, bob, base)
		last := base.Add(5 * time.Minute)
		h.sight(bob, alice, last)

		rows := h.rows()
		if len(rows) != 1 {
			t.Fatalf("encounters = %d, want 1", len(rows))
		}
		want := last.Add(24 * time.Hour).UnixMilli()
		if !rows[0].revealAt.Valid || rows[0].revealAt.Int64 != want {
			t.Fatalf("reveal_at = %v, want %d", rows[0].revealAt, want)
		}
	})

	t.Run("E7 private is hidden and not listed", func(t *testing.T) {
		h := newEncHarness(t)
		h.users(alice, bob)
		h.setMode(alice, "private", 0)
		h.setMode(bob, "immediate", 0)

		base := testutil.Epoch
		h.sight(alice, bob, base)
		h.sight(bob, alice, base.Add(5*time.Minute))
		h.mustReveal()

		rows := h.rows()
		if len(rows) != 1 || rows[0].visibility != "hidden" {
			t.Fatalf("rows = %+v, want 1 hidden", rows)
		}
		for _, viewer := range []string{alice, bob} {
			page, err := h.enc.List(context.Background(), viewer, "", 0)
			if err != nil {
				t.Fatalf("List(%s): %v", viewer, err)
			}
			if len(page.Items) != 0 {
				t.Fatalf("List(%s) items = %d, want 0 (hidden)", viewer, len(page.Items))
			}
		}
	})

	t.Run("E8 pending becomes hidden on strict change", func(t *testing.T) {
		h := newEncHarness(t)
		h.users(alice, bob)
		h.setMode(alice, "delayed", 3600)
		h.setMode(bob, "immediate", 0)

		base := testutil.Epoch
		h.sight(alice, bob, base)
		h.sight(bob, alice, base.Add(5*time.Minute))

		h.setMode(alice, "private", 0)

		rows := h.rows()
		if len(rows) != 1 || rows[0].visibility != "hidden" {
			t.Fatalf("rows = %+v, want hidden", rows)
		}
		if rows[0].revealAt.Valid {
			t.Fatalf("reveal_at = %v, want NULL", rows[0].revealAt)
		}
	})

	t.Run("E9 relaxing change keeps pending reveal_at", func(t *testing.T) {
		h := newEncHarness(t)
		h.users(alice, bob)
		h.setMode(alice, "delayed", 3600)
		h.setMode(bob, "delayed", 3600)

		base := testutil.Epoch
		h.sight(alice, bob, base)
		h.sight(bob, alice, base.Add(5*time.Minute))

		before := h.rows()
		if len(before) != 1 {
			t.Fatalf("encounters = %d, want 1", len(before))
		}
		h.setMode(alice, "immediate", 0)

		after := h.rows()
		if len(after) != 1 {
			t.Fatalf("encounters = %d, want 1", len(after))
		}
		if !after[0].revealAt.Valid || after[0].revealAt.Int64 != before[0].revealAt.Int64 {
			t.Fatalf("reveal_at changed: %v -> %v", before[0].revealAt, after[0].revealAt)
		}
	})

	t.Run("E10 outside the window is dropped", func(t *testing.T) {
		h := newEncHarness(t)
		h.users(alice, bob)
		h.setMode(alice, "immediate", 0)

		base := testutil.Epoch
		tok := h.token(bob, base)
		late := base.Add(service.BeaconWindow + 121*time.Second)
		h.env.Clock.Set(late)
		rssi := -70
		res, err := h.enc.RecordSightings(context.Background(), alice, []service.SightingInput{
			{Token: tok, FirstSeenAt: late, LastSeenAt: late, MaxRssi: &rssi},
		})
		if err != nil {
			t.Fatalf("RecordSightings: %v", err)
		}
		if res.Accepted != 0 || res.Dropped != 1 {
			t.Fatalf("result = %+v, want accepted 0 dropped 1", res)
		}
	})

	t.Run("E11 block prevents establishment", func(t *testing.T) {
		h := newEncHarness(t)
		h.users(alice, bob)
		h.setMode(alice, "immediate", 0)
		h.setMode(bob, "immediate", 0)

		if err := h.blocks.Create(context.Background(), bob, alice); err != nil {
			t.Fatalf("Create block: %v", err)
		}
		base := testutil.Epoch
		if res := h.sight(alice, bob, base); res.Accepted != 0 || res.Dropped != 1 {
			t.Fatalf("sighting after block = %+v, want dropped", res)
		}
		h.sight(bob, alice, base.Add(5*time.Minute))

		if rows := h.rows(); len(rows) != 0 {
			t.Fatalf("encounters = %d, want 0", len(rows))
		}
	})

	t.Run("E12 late delivery matches by observation time", func(t *testing.T) {
		h := newEncHarness(t)
		h.users(alice, bob)
		h.setMode(alice, "immediate", 0)
		h.setMode(bob, "immediate", 0)

		base := testutil.Epoch
		bobToken := h.token(bob, base)
		aliceToken := h.token(alice, base)
		rssi := -70

		h.env.Clock.Set(base)
		if _, err := h.enc.RecordSightings(context.Background(), alice, []service.SightingInput{
			{Token: bobToken, FirstSeenAt: base, LastSeenAt: base, MaxRssi: &rssi},
		}); err != nil {
			t.Fatalf("A sighting: %v", err)
		}

		late := base.Add(20 * time.Hour)
		h.env.Clock.Set(late)
		res, err := h.enc.RecordSightings(context.Background(), bob, []service.SightingInput{
			{Token: aliceToken, FirstSeenAt: base, LastSeenAt: base, MaxRssi: &rssi},
		})
		if err != nil {
			t.Fatalf("B late sighting: %v", err)
		}
		if res.Accepted != 1 {
			t.Fatalf("result = %+v, want accepted 1", res)
		}
		if rows := h.rows(); len(rows) != 1 {
			t.Fatalf("encounters = %d, want 1", len(rows))
		}
	})
}
