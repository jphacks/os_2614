package service

import (
	"math"
	"testing"
)

func approx(a, b, tol float64) bool { return math.Abs(a-b) <= tol }

func TestCircleIntersectionArea(t *testing.T) {
	const r = 100.0
	cases := []struct {
		name      string
		r1, r2, d float64
		want      float64
	}{
		{"離れている", r, r, 2 * r, 0},
		{"同心（同一）", r, r, 0, math.Pi * r * r},
		{"一方が他方を含む", r, 50, 0, math.Pi * 50 * 50},
		{"半分重なる（d=r）", r, r, r, 12283.7},
		{"少し重なる（d=1.5r）", r, r, 1.5 * r, 4533.0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := circleIntersectionArea(tc.r1, tc.r2, tc.d)
			if !approx(got, tc.want, 5) {
				t.Fatalf("circleIntersectionArea = %v, want ~%v", got, tc.want)
			}
		})
	}
}

func TestCircleIoU(t *testing.T) {
	cases := []struct {
		name string
		a, b GroupCircle
		want float64
	}{
		{"同心・同一半径", GroupCircle{35, 135, 40}, GroupCircle{35, 135, 40}, 1},
		{"離れている", GroupCircle{35, 135, 40}, GroupCircle{36, 136, 40}, 0},
		{"一方が他方を含む", GroupCircle{35, 135, 40}, GroupCircle{35, 135, 20}, 0.25},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := circleIoU(tc.a, tc.b)
			if !approx(got, tc.want, 1e-9) {
				t.Fatalf("circleIoU = %v, want %v", got, tc.want)
			}
		})
	}

	// 半分だけ重なる円は 0 と 1 の間になる。
	half := GroupCircle{35, 135, 100}
	offset := GroupCircle{35, 135.001, 100}
	if got := circleIoU(half, offset); got <= 0 || got >= 1 {
		t.Fatalf("部分的な重なりの IoU = %v, want (0,1)", got)
	}
}

func TestContainsCircle(t *testing.T) {
	outer := GroupCircle{35, 135, 100}
	if !containsCircle(outer, GroupCircle{35, 135, 40}) {
		t.Fatal("同心の小さい円を含むはず")
	}
	if containsCircle(GroupCircle{35, 135, 40}, outer) {
		t.Fatal("小さい円が大きい円を含んではいけない")
	}
	// 中心間 300m + 半径 40m <= 400m なので含む。
	if !containsCircle(GroupCircle{35, 135, 400}, GroupCircle{35.002698, 135, 40}) {
		t.Fatal("境界内の円を含むはず")
	}
}

func named(id string, radius int) namedCircle {
	return namedCircle{ID: id, GroupCircle: GroupCircle{Lat: 35, Lon: 135, RadiusM: radius}}
}

func TestPlanParentsNested(t *testing.T) {
	// 3 重の入れ子: A(800) ⊃ B(200) ⊃ C(40)。
	existing := []namedCircle{named("A", 800), named("B", 200)}
	parents := map[string]*string{"A": nil, "B": strptr("A")}

	parent, updates := planParents(named("C", 40), existing, parents)
	if parent == nil || *parent != "B" {
		t.Fatalf("C の parent = %v, want B", deref(parent))
	}
	if len(updates) != 0 {
		t.Fatalf("updates = %v, want none", updates)
	}

	// 既存の A(800) ⊃ B(200) に D(1000)（A を含む）を足す → A の parent が D になる。
	existing = []namedCircle{named("A", 800), named("B", 200)}
	parents = map[string]*string{"A": nil, "B": strptr("A")}
	parent, updates = planParents(named("D", 1000), existing, parents)
	if parent != nil {
		t.Fatalf("D の parent = %v, want nil", deref(parent))
	}
	if updates["A"] == nil || *updates["A"] != "D" {
		t.Fatalf("A の parent = %v, want D", deref(updates["A"]))
	}
	if _, ok := updates["B"]; ok {
		t.Fatalf("B の parent は変わらないはず: %v", deref(updates["B"]))
	}

	// 中間に差し込む: A(800) ⊃ C(40) に B(200) を足す → C の parent が B になる。
	existing = []namedCircle{named("A", 800), named("C", 40)}
	parents = map[string]*string{"A": nil, "C": strptr("A")}
	parent, updates = planParents(named("B", 200), existing, parents)
	if parent == nil || *parent != "A" {
		t.Fatalf("B の parent = %v, want A", deref(parent))
	}
	if updates["C"] == nil || *updates["C"] != "B" {
		t.Fatalf("C の parent = %v, want B", deref(updates["C"]))
	}

	// 新しいグループが既存を含む（小さな N(100) ⊃ B(40)、A(200) ⊃ N）。
	existing = []namedCircle{named("A", 200), named("B", 40)}
	parents = map[string]*string{"A": nil, "B": strptr("A")}
	parent, updates = planParents(named("N", 100), existing, parents)
	if parent == nil || *parent != "A" {
		t.Fatalf("N の parent = %v, want A", deref(parent))
	}
	if updates["B"] == nil || *updates["B"] != "N" {
		t.Fatalf("B の parent = %v, want N", deref(updates["B"]))
	}
}

func TestValidateGroupSchedule(t *testing.T) {
	valid := &GroupSchedule{TZ: "Asia/Tokyo", Slots: []GroupScheduleSlot{{Weekday: 1, Start: "10:40", End: "12:10"}}}
	if fields := validateGroupSchedule(valid); len(fields) != 0 {
		t.Fatalf("valid schedule fields = %v, want none", fields)
	}
	if fields := validateGroupSchedule(nil); len(fields) != 0 {
		t.Fatalf("nil schedule fields = %v, want none", fields)
	}

	slots := func(n int) []GroupScheduleSlot {
		out := make([]GroupScheduleSlot, n)
		for i := range out {
			out[i] = GroupScheduleSlot{Weekday: 1, Start: "10:00", End: "11:00"}
		}
		return out
	}
	cases := []struct {
		name string
		s    *GroupSchedule
		key  string
	}{
		{"slots が空", &GroupSchedule{TZ: "UTC"}, "schedule.slots"},
		{"slots が 15 件", &GroupSchedule{TZ: "UTC", Slots: slots(15)}, "schedule.slots"},
		{"weekday が範囲外", &GroupSchedule{TZ: "UTC", Slots: []GroupScheduleSlot{{Weekday: 0, Start: "10:00", End: "11:00"}}}, "schedule.slots.0.weekday"},
		{"start の形式", &GroupSchedule{TZ: "UTC", Slots: []GroupScheduleSlot{{Weekday: 1, Start: "25:00", End: "26:00"}}}, "schedule.slots.0.start"},
		{"start >= end", &GroupSchedule{TZ: "UTC", Slots: []GroupScheduleSlot{{Weekday: 1, Start: "12:00", End: "10:00"}}}, "schedule.slots.0.start"},
		{"tz が空", &GroupSchedule{Slots: []GroupScheduleSlot{{Weekday: 1, Start: "10:00", End: "11:00"}}}, "schedule.tz"},
		{"tz が IANA 名でない", &GroupSchedule{TZ: "Mars/Olympus", Slots: []GroupScheduleSlot{{Weekday: 1, Start: "10:00", End: "11:00"}}}, "schedule.tz"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fields := validateGroupSchedule(tc.s)
			if _, ok := fields[tc.key]; !ok {
				t.Fatalf("fields = %v, want key %q", fields, tc.key)
			}
		})
	}
}

func TestSchedulesOverlap(t *testing.T) {
	a := &GroupSchedule{TZ: "UTC", Slots: []GroupScheduleSlot{{Weekday: 1, Start: "10:00", End: "11:00"}}}
	b := `{"tz":"UTC","slots":[{"weekday":1,"start":"10:30","end":"11:30"}]}`
	c := `{"tz":"UTC","slots":[{"weekday":1,"start":"12:00","end":"13:00"}]}`
	d := `{"tz":"UTC","slots":[{"weekday":2,"start":"10:00","end":"11:00"}]}`

	if !schedulesOverlap(a, &b) {
		t.Fatal("同じ曜日で重なるはず")
	}
	if schedulesOverlap(a, &c) {
		t.Fatal("同じ曜日でも重ならないはず")
	}
	if schedulesOverlap(a, &d) {
		t.Fatal("曜日が違えば重ならないはず")
	}
	if !schedulesOverlap(nil, &c) {
		t.Fatal("schedule なしは何とでも重なるはず")
	}
	if !schedulesOverlap(a, nil) {
		t.Fatal("相手に schedule がなければ重なるはず")
	}
}

func strptr(s string) *string { return &s }

func deref(s *string) string {
	if s == nil {
		return "<nil>"
	}
	return *s
}
