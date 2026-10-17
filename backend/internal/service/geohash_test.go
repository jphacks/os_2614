package service

import (
	"math"
	"testing"
)

func TestEncodeGeohash(t *testing.T) {
	cases := []struct {
		name   string
		lat    float64
		lon    float64
		length int
		want   string
	}{
		{"canonical wikipedia example", 57.64911, 10.40744, 6, "u4pruy"},
		{"length zero is empty", 35.02613, 135.78089, 0, ""},
		{"negative length is empty", 35.02613, 135.78089, -3, ""},
		{"origin", 0, 0, 1, "s"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := EncodeGeohash(tc.lat, tc.lon, tc.length); got != tc.want {
				t.Fatalf("EncodeGeohash(%v, %v, %d) = %q, want %q", tc.lat, tc.lon, tc.length, got, tc.want)
			}
		})
	}
}

func TestGeohashCellCenter(t *testing.T) {
	const (
		lat = 57.64911
		lon = 10.40744
	)
	centerLat, centerLon := GeohashCellCenter(lat, lon, 6)

	if got := EncodeGeohash(centerLat, centerLon, 6); got != "u4pruy" {
		t.Fatalf("center re-encodes to %q, want %q", got, "u4pruy")
	}
	if centerLat == lat && centerLon == lon {
		t.Fatalf("center (%v, %v) equals the original point; want a cell center", centerLat, centerLon)
	}
}

func TestDecodeGeohashWithinCellBounds(t *testing.T) {
	lat, lon, ok := DecodeGeohash("u4pruy")
	if !ok {
		t.Fatal("DecodeGeohash(\"u4pruy\") ok = false, want true")
	}
	if math.Abs(lat-57.64911) > 0.003 {
		t.Fatalf("decoded lat = %v, want within %v of 57.64911", lat, 0.003)
	}
	if math.Abs(lon-10.40744) > 0.003 {
		t.Fatalf("decoded lon = %v, want within %v of 10.40744", lon, 0.003)
	}
}

func TestGeohashRoundTrip(t *testing.T) {
	points := []struct {
		name string
		lat  float64
		lon  float64
	}{
		{"Sydney", -33.86, 151.21},
		{"Kyoto", 35.02613, 135.78089},
		{"Denmark", 57.64911, 10.40744},
		{"NullIsland", 0, 0},
	}
	for _, p := range points {
		for length := 1; length <= 9; length++ {
			hash := EncodeGeohash(p.lat, p.lon, length)
			centerLat, centerLon := GeohashCellCenter(p.lat, p.lon, length)
			got := EncodeGeohash(centerLat, centerLon, length)
			if got != hash {
				t.Fatalf("%s length %d: center re-encoded to %q, want %q", p.name, length, got, hash)
			}
		}
	}
}

func TestDecodeGeohashErrors(t *testing.T) {
	cases := []struct {
		name string
		hash string
		ok   bool
	}{
		{"empty", "", false},
		{"invalid character", "invalid!", false},
		{"excluded letter a", "a", false},
		{"excluded letter i", "i", false},
		{"excluded letter l", "l", false},
		{"excluded letter o", "o", false},
		{"valid lowercase", "u4pruy", true},
		{"valid uppercase", "U4PRUY", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, ok := DecodeGeohash(tc.hash)
			if ok != tc.ok {
				t.Fatalf("DecodeGeohash(%q) ok = %v, want %v", tc.hash, ok, tc.ok)
			}
		})
	}

	lower, lowerLon, _ := DecodeGeohash("u4pruy")
	upper, upperLon, ok := DecodeGeohash("U4PRUY")
	if !ok {
		t.Fatal("DecodeGeohash(\"U4PRUY\") ok = false, want true")
	}
	if lower != upper || lowerLon != upperLon {
		t.Fatalf("uppercase decode = (%v, %v), want (%v, %v)", upper, upperLon, lower, lowerLon)
	}
}
