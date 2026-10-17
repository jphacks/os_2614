package service

import (
	"reflect"
	"testing"
	"time"
)

func TestConnectedComponents(t *testing.T) {
	tests := []struct {
		name  string
		edges []partyEdge
		want  [][]string
	}{
		{"空", nil, [][]string{}},
		{"1 本", []partyEdge{{A: "a", B: "b"}}, [][]string{{"a", "b"}}},
		{"path", []partyEdge{{A: "a", B: "b"}, {A: "b", B: "c"}}, [][]string{{"a", "b", "c"}}},
		{"2 成分", []partyEdge{{A: "c", B: "d"}, {A: "a", B: "b"}}, [][]string{{"a", "b"}, {"c", "d"}}},
		{"重複 edge", []partyEdge{{A: "a", B: "b"}, {A: "b", B: "a"}, {A: "a", B: "b"}}, [][]string{{"a", "b"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := connectedComponents(tt.edges)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("components = %v, want %v", got, tt.want)
			}
		})
	}
}

func pi(space, phase string, at time.Time, members ...string) partyInfo {
	return partyInfo{SpaceID: space, Phase: phase, TentativeAt: at, Members: members}
}

func TestPlanIntegration(t *testing.T) {
	base := time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC)

	t.Run("所属なしは新規", func(t *testing.T) {
		plan := planIntegration([]string{"a", "b"}, nil, nil, nil)
		if !plan.Create || !reflect.DeepEqual(plan.CreateMembers, []string{"a", "b"}) {
			t.Fatalf("plan = %+v, want create [a b]", plan)
		}
	})

	t.Run("1 party へ未所属を入れる", func(t *testing.T) {
		parties := []partyInfo{pi("p1", "tentative", base, "a")}
		off := map[string]string{"a": "p1"}
		plan := planIntegration([]string{"a", "b", "c"}, parties, nil, off)
		if plan.Create || len(plan.Dissolve) != 0 {
			t.Fatalf("plan = %+v, want assign only", plan)
		}
		if !reflect.DeepEqual(plan.Assign["p1"], []string{"b", "c"}) {
			t.Fatalf("assign = %v, want [b c]", plan.Assign)
		}
	})

	t.Run("仮同士は古い方へ統合", func(t *testing.T) {
		parties := []partyInfo{
			pi("p2", "tentative", base.Add(time.Minute), "c"),
			pi("p1", "tentative", base, "a"),
		}
		off := map[string]string{"a": "p1", "c": "p2"}
		plan := planIntegration([]string{"a", "b", "c", "d"}, parties, nil, off)
		if plan.MergeInto != "p1" || !reflect.DeepEqual(plan.Dissolve, []string{"p2"}) {
			t.Fatalf("plan = %+v, want merge into p1, dissolve p2", plan)
		}
		if !reflect.DeepEqual(plan.Assign["p1"], []string{"b", "d"}) {
			t.Fatalf("assign = %v, want [b d]", plan.Assign)
		}
	})

	t.Run("仮と本は本へ", func(t *testing.T) {
		parties := []partyInfo{
			pi("pt", "tentative", base, "a"),
			pi("pc", "confirmed", base.Add(time.Minute), "c"),
		}
		off := map[string]string{"a": "pt", "c": "pc"}
		plan := planIntegration([]string{"a", "b", "c"}, parties, nil, off)
		if plan.MergeInto != "pc" || !reflect.DeepEqual(plan.Dissolve, []string{"pt"}) {
			t.Fatalf("plan = %+v, want merge into pc, dissolve pt", plan)
		}
		if !reflect.DeepEqual(plan.Assign["pc"], []string{"b"}) {
			t.Fatalf("assign = %v, want [b]", plan.Assign)
		}
	})

	t.Run("本同士は統合せず最も古いエッジの相手へ", func(t *testing.T) {
		parties := []partyInfo{
			pi("p1", "confirmed", base, "a"),
			pi("p2", "confirmed", base.Add(time.Minute), "c"),
		}
		off := map[string]string{"a": "p1", "c": "p2"}
		since := map[string]time.Time{
			pairKey("b", "a"): base.Add(5 * time.Minute),
			pairKey("b", "c"): base.Add(10 * time.Minute),
			pairKey("a", "c"): base.Add(1 * time.Minute),
		}
		plan := planIntegration([]string{"a", "b", "c"}, parties, since, off)
		if len(plan.Dissolve) != 0 || plan.MergeInto != "" || plan.Create {
			t.Fatalf("plan = %+v, want no merge", plan)
		}
		if !reflect.DeepEqual(plan.Assign["p1"], []string{"b"}) {
			t.Fatalf("assign = %v, want b -> p1 (oldest edge)", plan.Assign)
		}
	})
}

func TestPairKey(t *testing.T) {
	if got := pairKey("b", "a"); got != "a|b" {
		t.Fatalf("pairKey(b,a) = %q, want a|b", got)
	}
	if pairKey("a", "b") != pairKey("b", "a") {
		t.Fatal("pairKey is not order independent")
	}
}

func TestPartyName(t *testing.T) {
	tests := []struct {
		at   time.Time
		want string
	}{
		{time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC), "10/17 09:00 の party"},
		{time.Date(2026, 10, 17, 4, 5, 0, 0, time.UTC), "10/17 13:05 の party"},
		{time.Date(2026, 12, 31, 15, 5, 0, 0, time.UTC), "1/1 00:05 の party"},
	}
	for _, tt := range tests {
		if got := partyName(tt.at); got != tt.want {
			t.Errorf("partyName(%s) = %q, want %q", tt.at, got, tt.want)
		}
	}
}
