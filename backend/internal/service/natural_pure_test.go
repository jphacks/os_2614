package service

import (
	"testing"
	"time"

	"github.com/applemango/chain/internal/model"
)

func TestDBSCAN(t *testing.T) {
	close3 := []geoPoint{
		{Lat: 35.0, Lon: 135.0},
		{Lat: 35.0002, Lon: 135.0},
		{Lat: 35.0, Lon: 135.0002},
	}
	if got := dbscan(close3, NaturalClusterEps, NaturalMinSize); len(got) != 1 || len(got[0]) != 3 {
		t.Fatalf("3 人の塊 = %v, want 1 塊 3 人", got)
	}

	two := []geoPoint{{Lat: 35.0, Lon: 135.0}, {Lat: 35.0002, Lon: 135.0}}
	if got := dbscan(two, NaturalClusterEps, NaturalMinSize); len(got) != 0 {
		t.Fatalf("2 人は塊にならないはず: %v", got)
	}

	far := []geoPoint{
		{Lat: 35.0, Lon: 135.0}, {Lat: 35.0002, Lon: 135.0}, {Lat: 35.0, Lon: 135.0002},
		{Lat: 35.01, Lon: 135.0}, {Lat: 35.0102, Lon: 135.0}, {Lat: 35.01, Lon: 135.0002},
	}
	if got := dbscan(far, NaturalClusterEps, NaturalMinSize); len(got) != 2 {
		t.Fatalf("離れた 2 つの塊 = %v, want 2 塊", got)
	}
}

func TestOverlapRatio(t *testing.T) {
	cases := []struct {
		name           string
		aS, aE, bS, bE int
		want           float64
	}{
		{"半分重なる", 600, 720, 660, 780, 0.5},
		{"重ならない", 600, 660, 700, 760, 0},
		{"短い方が完全に含まれる", 600, 720, 630, 660, 1},
		{"一致", 600, 720, 600, 720, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := overlapRatio(tc.aS, tc.aE, tc.bS, tc.bE); !approx(got, tc.want, 1e-9) {
				t.Fatalf("overlapRatio = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestEntriesSimilar(t *testing.T) {
	a := model.MatchEntry{StartMin: 600, EndMin: 720, Embedding: []float32{1, 0}}
	b := model.MatchEntry{StartMin: 660, EndMin: 780, Embedding: []float32{1, 0}}
	if !entriesSimilar(a, b) {
		t.Fatal("同じ向き・半分重なりは似ているはず")
	}

	orthogonal := model.MatchEntry{StartMin: 600, EndMin: 720, Embedding: []float32{0, 1}}
	if entriesSimilar(a, orthogonal) {
		t.Fatal("直交する埋め込みは似ていないはず")
	}

	noOverlap := model.MatchEntry{StartMin: 800, EndMin: 900, Embedding: []float32{1, 0}}
	if entriesSimilar(a, noOverlap) {
		t.Fatal("時間が重ならないものは似ていないはず")
	}

	if entriesSimilar(a, model.MatchEntry{StartMin: 600, EndMin: 720}) {
		t.Fatal("埋め込みがなければ似ていないはず")
	}
}

func TestNaturalComponents(t *testing.T) {
	comps := naturalComponents(4, func(i, j int) bool {
		return (i == 0 && j == 1) || (i == 1 && j == 2)
	})
	if len(comps) != 2 {
		t.Fatalf("連結成分 = %v, want 2", comps)
	}
	var sizes []int
	for _, c := range comps {
		sizes = append(sizes, len(c))
	}
	if !(sizes[0] == 3 && sizes[1] == 1) && !(sizes[0] == 1 && sizes[1] == 3) {
		t.Fatalf("成分の大きさ = %v, want {3,1}", sizes)
	}
}

func TestMedoidIndex(t *testing.T) {
	idx := medoidIndex([][]float32{{1, 0}, {1, 0}, {0, 1}})
	if idx != 0 && idx != 1 {
		t.Fatalf("medoid = %d, want 0 or 1", idx)
	}
	if medoidIndex(nil) != -1 {
		t.Fatal("空の medoid は -1 のはず")
	}
}

func TestSmallestEnclosingCircle(t *testing.T) {
	cx, cy, r := smallestEnclosingCircle([]circle2{{X: 0, Y: 0}, {X: 10, Y: 0}})
	if !approx(cx, 5, 1e-9) || !approx(cy, 0, 1e-9) || !approx(r, 5, 1e-9) {
		t.Fatalf("2 点の円 = (%v,%v,%v), want (5,0,5)", cx, cy, r)
	}

	cx, cy, r = smallestEnclosingCircle([]circle2{{X: 0, Y: 0}, {X: 10, Y: 0}, {X: 5, Y: 5}})
	if !approx(cx, 5, 1e-6) || !approx(cy, 0, 1e-6) || !approx(r, 5, 1e-6) {
		t.Fatalf("直角三角形の円 = (%v,%v,%v), want (5,0,5)", cx, cy, r)
	}
}

func TestNaturalGeofenceRadius(t *testing.T) {
	_, _, r := naturalGeofence([]geoPoint{{Lat: 35.0, Lon: 135.0}})
	if r != NaturalRadiusMin {
		t.Fatalf("1 人の半径 = %d, want %d", r, NaturalRadiusMin)
	}

	_, _, r = naturalGeofence([]geoPoint{
		{Lat: 35.0, Lon: 135.0}, {Lat: 35.0002, Lon: 135.0}, {Lat: 35.0, Lon: 135.0002},
	})
	if r < NaturalRadiusMin || r > NaturalRadiusMax {
		t.Fatalf("小さい塊の半径 = %d, want %d〜%d", r, NaturalRadiusMin, NaturalRadiusMax)
	}

	_, _, r = naturalGeofence([]geoPoint{{Lat: 35.0, Lon: 135.0}, {Lat: 35.05, Lon: 135.0}})
	if r != NaturalRadiusMax {
		t.Fatalf("広い塊の半径 = %d, want %d", r, NaturalRadiusMax)
	}
}

func TestClampNaturalRadius(t *testing.T) {
	if clampNaturalRadius(5) != NaturalRadiusMin {
		t.Fatal("下限に丸めるはず")
	}
	if clampNaturalRadius(1000) != NaturalRadiusMax {
		t.Fatal("上限に丸めるはず")
	}
	if clampNaturalRadius(120) != 120 {
		t.Fatal("範囲内はそのままのはず")
	}
}

func TestScheduleActiveDates(t *testing.T) {
	raw := `{"tz":"UTC","dates":[{"date":"2026-10-17","start":"09:00","end":"10:00"}]}`
	at := func(h, m int) time.Time { return time.Date(2026, 10, 17, h, m, 0, 0, time.UTC) }
	if !scheduleActive(&raw, at(9, 30)) {
		t.Fatal("時間帯の中は有効のはず")
	}
	if scheduleActive(&raw, at(10, 30)) {
		t.Fatal("時間帯の外は無効のはず")
	}
	if scheduleActive(&raw, time.Date(2026, 10, 18, 9, 30, 0, 0, time.UTC)) {
		t.Fatal("別の日は無効のはず")
	}

	both := `{"tz":"UTC","slots":[],"dates":[{"date":"2026-10-17","start":"09:00","end":"10:00"}]}`
	if !scheduleActive(&both, at(9, 30)) {
		t.Fatal("slots が空でも dates で有効のはず")
	}

	empty := `{"tz":"UTC"}`
	if !scheduleActive(&empty, at(3, 0)) {
		t.Fatal("slots も dates も無ければ常時有効のはず")
	}
}

func TestScheduleOverlapsRangeWithDates(t *testing.T) {
	raw := `{"tz":"UTC","dates":[{"date":"2026-10-17","start":"10:00","end":"12:00"}]}`
	if !scheduleOverlapsRange(&raw, "2026-10-17", 11*60, 13*60) {
		t.Fatal("同じ日の重なる範囲は true のはず")
	}
	if scheduleOverlapsRange(&raw, "2026-10-18", 11*60, 13*60) {
		t.Fatal("別の日は false のはず")
	}
	if scheduleOverlapsRange(&raw, "2026-10-17", 13*60, 14*60) {
		t.Fatal("重ならない範囲は false のはず")
	}
}

func TestScheduleLatestEnd(t *testing.T) {
	raw := `{"tz":"UTC","dates":[{"date":"2026-10-17","start":"10:00","end":"12:00"}]}`
	end, ok := scheduleLatestEnd(&raw, time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC))
	if !ok {
		t.Fatal("dates があるので ok のはず")
	}
	want := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	if !end.Equal(want) {
		t.Fatalf("end = %v, want %v", end, want)
	}
	none := `{"tz":"UTC","slots":[{"weekday":1,"start":"10:00","end":"12:00"}]}`
	if _, ok := scheduleLatestEnd(&none, time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC)); ok {
		t.Fatal("dates が無ければ ok=false のはず")
	}
}

func TestValidateGroupScheduleDates(t *testing.T) {
	valid := &GroupSchedule{TZ: "UTC", Dates: []GroupScheduleDate{{Date: "2026-10-17", Start: "09:00", End: "10:00"}}}
	if fields := validateGroupSchedule(valid); len(fields) != 0 {
		t.Fatalf("dates だけの有効な schedule でエラー: %v", fields)
	}

	dates := func(n int) []GroupScheduleDate {
		out := make([]GroupScheduleDate, n)
		for i := range out {
			out[i] = GroupScheduleDate{Date: "2026-10-17", Start: "09:00", End: "10:00"}
		}
		return out
	}
	cases := []struct {
		name  string
		s     *GroupSchedule
		field string
	}{
		{"dates が 15 件", &GroupSchedule{TZ: "UTC", Dates: dates(15)}, "schedule.dates"},
		{"日付の形式", &GroupSchedule{TZ: "UTC", Dates: []GroupScheduleDate{{Date: "2026-13-01", Start: "09:00", End: "10:00"}}}, "schedule.dates.0.date"},
		{"start >= end", &GroupSchedule{TZ: "UTC", Dates: []GroupScheduleDate{{Date: "2026-10-17", Start: "10:00", End: "09:00"}}}, "schedule.dates.0.start"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fields := validateGroupSchedule(tc.s)
			if fields[tc.field] == "" {
				t.Fatalf("fields[%q] が空: %v", tc.field, fields)
			}
		})
	}
}
