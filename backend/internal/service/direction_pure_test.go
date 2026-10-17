package service

import (
	"math"
	"testing"
)

func TestCompassDirection(t *testing.T) {
	cases := []struct {
		name    string
		bearing float64
		want    string
	}{
		{"north", 0, "N"},
		{"northeast", 45, "NE"},
		{"east", 90, "E"},
		{"southeast", 135, "SE"},
		{"south", 180, "S"},
		{"southwest", 225, "SW"},
		{"west", 270, "W"},
		{"northwest", 315, "NW"},
		// 帯の境界（中央 ±22.5°）。下側は前の帯、境界そのものは次の帯。
		{"just below NE", 22.4, "N"},
		{"NE boundary", 22.5, "NE"},
		{"just below E", 67.4, "NE"},
		{"E boundary", 67.5, "E"},
		{"just below SE", 112.4, "E"},
		{"SE boundary", 112.5, "SE"},
		{"S boundary", 157.5, "S"},
		{"SW boundary", 202.5, "SW"},
		{"W boundary", 247.5, "W"},
		{"NW boundary", 292.5, "NW"},
		{"just below N wrap", 337.4, "NW"},
		{"N boundary wrap", 337.5, "N"},
		{"wrap around 360", 359.9, "N"},
		{"over 360", 360, "N"},
		{"negative wraps", -10, "N"},
		{"negative to NW", -45, "NW"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := CompassDirection(tc.bearing); got != tc.want {
				t.Fatalf("CompassDirection(%v) = %q, want %q", tc.bearing, got, tc.want)
			}
		})
	}
}

func TestDistanceBucket(t *testing.T) {
	cases := []struct {
		name  string
		distM float64
		want  string
	}{
		{"zero", 0, "<100m"},
		{"just below 100", 99.9, "<100m"},
		{"at 100", 100, "<500m"},
		{"just below 500", 499.9, "<500m"},
		{"at 500", 500, "<2km"},
		{"just below 2km", 1999.9, "<2km"},
		{"at 2km", 2000, "<10km"},
		{"just below 10km", 9999.9, "<10km"},
		{"at 10km", 10000, "far"},
		{"very far", 1e9, "far"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := DistanceBucket(tc.distM); got != tc.want {
				t.Fatalf("DistanceBucket(%v) = %q, want %q", tc.distM, got, tc.want)
			}
		})
	}
}

func TestInitialBearing(t *testing.T) {
	cases := []struct {
		name                   string
		lat1, lon1, lat2, lon2 float64
		want                   float64
		tolerance              float64
	}{
		{"same point is north", 35.0, 135.0, 35.0, 135.0, 0, 0},
		{"due north", 35.0, 135.0, 36.0, 135.0, 0, 0.01},
		{"due south", 35.0, 135.0, 34.0, 135.0, 180, 0.01},
		{"due east", 35.0, 135.0, 35.0, 136.0, 90, 1},
		{"due west", 35.0, 135.0, 35.0, 134.0, 270, 1},
		// 緯度差 0.1° に対し経度差 0.1/cos(35°) とすると真方位はほぼ 45°（北東）。
		{"northeast", 35.0, 135.0, 35.1, 135.1221, 45, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := InitialBearing(tc.lat1, tc.lon1, tc.lat2, tc.lon2)
			if got < 0 || got >= 360 {
				t.Fatalf("InitialBearing out of range: %v", got)
			}
			diff := math.Abs(got - tc.want)
			if diff > 180 {
				diff = 360 - diff
			}
			if diff > tc.tolerance {
				t.Fatalf("InitialBearing = %v, want %v (±%v)", got, tc.want, tc.tolerance)
			}
		})
	}
}
