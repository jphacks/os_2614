//go:build integration

package encounter_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/applemango/chain/internal/model"
	core "github.com/applemango/chain/internal/service/core"
	mesvc "github.com/applemango/chain/internal/service/me"
	"github.com/applemango/chain/internal/testutil"
)

func (h *encHarness) recordPlace(did string, on bool) {
	h.t.Helper()
	if _, err := h.me.UpdateSettings(context.Background(), did, mesvc.SettingsUpdate{RecordEncounterPlace: &on}); err != nil {
		h.t.Fatalf("UpdateSettings(%s): %v", did, err)
	}
}

func (h *encHarness) geohash() sql.NullString {
	h.t.Helper()
	var g sql.NullString
	if err := h.env.DB.QueryRowContext(context.Background(), "SELECT geohash6 FROM encounters").Scan(&g); err != nil {
		h.t.Fatalf("query geohash6: %v", err)
	}
	return g
}

func TestEncounterPlace(t *testing.T) {
	const lat, lon = 35.681236, 139.767125
	setup := func(t *testing.T, aliceOn, bobOn bool, locAt time.Time) *encHarness {
		h := newEncHarness(t)
		h.users(alice, bob)
		h.setMode(alice, "immediate", 0)
		h.setMode(bob, "immediate", 0)
		h.recordPlace(alice, aliceOn)
		h.recordPlace(bob, bobOn)
		loc := model.Location{DID: bob, Lat: lat, Lon: lon, AccuracyM: 10, RecordedAt: locAt, Source: "gps"}
		if err := model.SaveLocation(context.Background(), h.env.Redis, loc, core.LocationTTL); err != nil {
			t.Fatalf("SaveLocation: %v", err)
		}
		h.sight(alice, bob, testutil.Epoch)
		h.sight(bob, alice, testutil.Epoch.Add(5*time.Minute))
		return h
	}

	t.Run("both opted in stores geohash6 of reporter", func(t *testing.T) {
		h := setup(t, true, true, testutil.Epoch.Add(4*time.Minute))
		g := h.geohash()
		if want := core.EncodeGeohash(lat, lon, 6); !g.Valid || g.String != want {
			t.Fatalf("geohash6 = %+v, want %q", g, want)
		}
	})

	t.Run("one side off stores nothing", func(t *testing.T) {
		h := setup(t, true, false, testutil.Epoch.Add(4*time.Minute))
		if g := h.geohash(); g.Valid {
			t.Fatalf("geohash6 = %q, want NULL", g.String)
		}
	})

	t.Run("stale location stores nothing", func(t *testing.T) {
		h := setup(t, true, true, testutil.Epoch.Add(-20*time.Minute))
		if g := h.geohash(); g.Valid {
			t.Fatalf("geohash6 = %q, want NULL", g.String)
		}
	})
}
