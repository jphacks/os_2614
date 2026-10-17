package encounter

import (
	"testing"
	"time"

	"github.com/applemango/chain/internal/model"
	"github.com/applemango/chain/internal/service/core"
	"github.com/applemango/chain/internal/service/proximity"
)

func at(tm time.Time, d time.Duration) *time.Time {
	v := tm.Add(d)
	return &v
}

func TestResolveReveal(t *testing.T) {
	established := time.Date(2026, 10, 17, 3, 0, 0, 0, time.UTC)
	lastSeen := established.Add(5 * time.Minute)

	cases := []struct {
		name       string
		aMode      string
		aDelay     int
		bMode      string
		bDelay     int
		wantVis    string
		wantReveal *time.Time
	}{
		{"both immediate", "immediate", 0, "immediate", 0, "pending", &established},
		{"delayed takes the longer one", "delayed", 3600, "delayed", 86400, "pending", at(lastSeen, 86400*time.Second)},
		{"immediate vs delayed", "immediate", 0, "delayed", 3600, "pending", at(lastSeen, time.Hour)},
		{"private wins", "private", 3600, "immediate", 0, "hidden", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			vis, reveal := resolveReveal(tc.aMode, tc.aDelay, tc.bMode, tc.bDelay, established, lastSeen)
			if vis != tc.wantVis {
				t.Fatalf("visibility = %q, want %q", vis, tc.wantVis)
			}
			if !core.SameTime(reveal, tc.wantReveal) {
				t.Fatalf("revealAt = %v, want %v", reveal, tc.wantReveal)
			}
		})
	}
}

func TestExtendReveal(t *testing.T) {
	lastSeen := time.Date(2026, 10, 17, 3, 5, 0, 0, time.UTC)

	cases := []struct {
		name        string
		existing    string
		current     *time.Time
		aMode       string
		aDelay      int
		bMode       string
		bDelay      int
		wantVis     string
		wantReveal  *time.Time
		wantRevealN bool
	}{
		{"hidden stays hidden", "hidden", nil, "immediate", 0, "immediate", 0, "hidden", nil, true},
		{"revealed stays revealed", "revealed", at(lastSeen, time.Hour), "immediate", 0, "immediate", 0, "revealed", at(lastSeen, time.Hour), false},
		{"pending moves later", "pending", at(lastSeen, time.Hour), "delayed", 7200, "immediate", 0, "pending", at(lastSeen, 2*time.Hour), false},
		{"pending never moves earlier", "pending", at(lastSeen, 5*time.Hour), "delayed", 7200, "immediate", 0, "pending", at(lastSeen, 5*time.Hour), false},
		{"pending to hidden", "pending", at(lastSeen, time.Hour), "private", 3600, "immediate", 0, "hidden", nil, true},
		{"pending immediate keeps current", "pending", at(lastSeen, time.Hour), "immediate", 0, "immediate", 0, "pending", at(lastSeen, time.Hour), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			vis, reveal := extendReveal(tc.existing, tc.current, tc.aMode, tc.aDelay, tc.bMode, tc.bDelay, lastSeen)
			if vis != tc.wantVis {
				t.Fatalf("visibility = %q, want %q", vis, tc.wantVis)
			}
			if tc.wantRevealN {
				if reveal != nil {
					t.Fatalf("revealAt = %v, want nil", reveal)
				}
				return
			}
			if !core.SameTime(reveal, tc.wantReveal) {
				t.Fatalf("revealAt = %v, want %v", reveal, tc.wantReveal)
			}
		})
	}
}

func TestTimeRounding(t *testing.T) {
	ref := time.Date(2026, 10, 17, 3, 12, 45, 0, time.UTC)
	if got, want := core.RoundDown(ref), time.Date(2026, 10, 17, 3, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Fatalf("core.RoundDown = %v, want %v", got, want)
	}
	if got, want := core.RoundUp(ref), time.Date(2026, 10, 17, 3, 15, 0, 0, time.UTC); !got.Equal(want) {
		t.Fatalf("core.RoundUp = %v, want %v", got, want)
	}

	exact := time.Date(2026, 10, 17, 3, 15, 0, 0, time.UTC)
	if got := core.RoundDown(exact); !got.Equal(exact) {
		t.Fatalf("core.RoundDown(exact) = %v, want %v", got, exact)
	}
	if got := core.RoundUp(exact); !got.Equal(exact) {
		t.Fatalf("core.RoundUp(exact) = %v, want %v", got, exact)
	}
}

func TestObservationInWindow(t *testing.T) {
	base := time.Unix(0, 0)
	cases := []struct {
		name       string
		window     int64
		firstDelta time.Duration
		lastDelta  time.Duration
		want       bool
	}{
		{"inside", 0, 0, 0, true},
		{"at lower bound", 0, -120 * time.Second, -120 * time.Second, true},
		{"before lower bound", 0, -121 * time.Second, -121 * time.Second, false},
		{"at upper bound", 0, 1020 * time.Second, 1020 * time.Second, true},
		{"after upper bound", 0, 1021 * time.Second, 1021 * time.Second, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			first := base.Add(tc.firstDelta)
			last := base.Add(tc.lastDelta)
			if got := proximity.ObservationInWindow(tc.window, first, last); got != tc.want {
				t.Fatalf("proximity.ObservationInWindow = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestJSTDayBounds(t *testing.T) {
	// 2026-10-17T00:00:00Z は JST で 2026-10-17 09:00。日の開始は 2026-10-16T15:00:00Z。
	from, to := core.JstDayBounds(time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC))
	wantFrom := time.Date(2026, 10, 16, 15, 0, 0, 0, time.UTC)
	wantTo := time.Date(2026, 10, 17, 15, 0, 0, 0, time.UTC)
	if !from.Equal(wantFrom) || !to.Equal(wantTo) {
		t.Fatalf("core.JstDayBounds = [%v, %v), want [%v, %v)", from, to, wantFrom, wantTo)
	}
}

func TestOrderDIDs(t *testing.T) {
	if a, b := core.OrderDIDs("b", "a"); a != "a" || b != "b" {
		t.Fatalf("core.OrderDIDs = (%q, %q)", a, b)
	}
	if a, b := core.OrderDIDs("a", "b"); a != "a" || b != "b" {
		t.Fatalf("core.OrderDIDs = (%q, %q)", a, b)
	}
}

func TestRelationString(t *testing.T) {
	cases := []struct {
		r    model.FriendRelation
		want string
	}{
		{model.FriendRelation{Friend: true}, "friend"},
		{model.FriendRelation{Requested: true}, "requested"},
		{model.FriendRelation{Incoming: true}, "incoming"},
		{model.FriendRelation{}, "none"},
		{model.FriendRelation{Friend: true, Requested: true}, "friend"},
	}
	for _, tc := range cases {
		if got := relationString(&tc.r); got != tc.want {
			t.Fatalf("relationString(%+v) = %q, want %q", tc.r, got, tc.want)
		}
	}
}

func TestIsStricterSettings(t *testing.T) {
	cases := []struct {
		name string
		old  model.UserSettings
		next model.UserSettings
		want bool
	}{
		{"to private", model.UserSettings{EncounterMode: "delayed", EncounterDelaySec: 3600}, model.UserSettings{EncounterMode: "private", EncounterDelaySec: 3600}, true},
		{"from private", model.UserSettings{EncounterMode: "private", EncounterDelaySec: 3600}, model.UserSettings{EncounterMode: "delayed", EncounterDelaySec: 3600}, false},
		{"longer delay", model.UserSettings{EncounterMode: "delayed", EncounterDelaySec: 3600}, model.UserSettings{EncounterMode: "delayed", EncounterDelaySec: 7200}, true},
		{"shorter delay", model.UserSettings{EncounterMode: "delayed", EncounterDelaySec: 7200}, model.UserSettings{EncounterMode: "delayed", EncounterDelaySec: 3600}, false},
		{"immediate to delayed", model.UserSettings{EncounterMode: "immediate", EncounterDelaySec: 0}, model.UserSettings{EncounterMode: "delayed", EncounterDelaySec: 3600}, true},
		{"delayed to immediate", model.UserSettings{EncounterMode: "delayed", EncounterDelaySec: 3600}, model.UserSettings{EncounterMode: "immediate", EncounterDelaySec: 0}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsStricterSettings(&tc.old, &tc.next); got != tc.want {
				t.Fatalf("IsStricterSettings = %v, want %v", got, tc.want)
			}
		})
	}
}
