package proximity

import (
	"math"
	"testing"

	"github.com/applemango/chain/internal/model"
)

func TestHaversine(t *testing.T) {
	if d := Haversine(35.0, 135.0, 35.0, 135.0); d != 0 {
		t.Fatalf("distance to itself = %v, want 0", d)
	}
	oneDegreeLat := Haversine(35.0, 135.0, 36.0, 135.0)
	if math.Abs(oneDegreeLat-111195) > 300 {
		t.Fatalf("one degree of latitude = %.1f m, want ~111195 m", oneDegreeLat)
	}
}

func TestInsideZone(t *testing.T) {
	const metersPerDegLat = 111194.93
	zone := Zone{ID: "g", CenterLat: 35.0, CenterLon: 135.0, RadiusM: 40}

	locAt := func(meters, accuracy float64) *model.Location {
		return &model.Location{Lat: 35.0 + meters/metersPerDegLat, Lon: 135.0, AccuracyM: accuracy}
	}

	cases := []struct {
		name string
		loc  *model.Location
		want bool
	}{
		{"inside", locAt(30, 10), true},
		{"outside", locAt(80, 10), false},
		{"within accuracy tolerance", locAt(50, 100), true},
		{"accuracy capped at half radius", locAt(70, 100), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := InsideZone(zone, tc.loc); got != tc.want {
				t.Fatalf("InsideZone = %v, want %v", got, tc.want)
			}
		})
	}
}
