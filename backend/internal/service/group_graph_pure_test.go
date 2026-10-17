package service

import "testing"

func edgeKey(e GraphEdge) string { return e.From + ">" + e.To + ":" + e.Kind }

func edgeSet(t *testing.T, edges []GraphEdge) map[string]GraphEdge {
	t.Helper()
	m := make(map[string]GraphEdge, len(edges))
	for _, e := range edges {
		k := edgeKey(e)
		if _, dup := m[k]; dup {
			t.Fatalf("重複した辺: %s", k)
		}
		m[k] = e
	}
	return m
}

func TestGraphGeometricEdgesNested(t *testing.T) {
	// 3 重の入れ子 A(800) ⊃ B(200) ⊃ C(40)。すべて同一中心。
	edges := graphGeometricEdges([]namedCircle{named("A", 800), named("B", 200), named("C", 40)})
	got := edgeSet(t, edges)
	for _, want := range []string{"A>B:contains", "A>C:contains", "B>C:contains"} {
		if _, ok := got[want]; !ok {
			t.Fatalf("辺 %s がない: %+v", want, edges)
		}
	}
	if len(got) != 3 {
		t.Fatalf("辺 = %d 本, want 3: %+v", len(got), edges)
	}
}

func TestGraphGeometricEdgesPartialOverlap(t *testing.T) {
	a := namedCircle{ID: "A", GroupCircle: GroupCircle{Lat: 35, Lon: 135, RadiusM: 100}}
	b := namedCircle{ID: "B", GroupCircle: GroupCircle{Lat: 35.001, Lon: 135, RadiusM: 100}}
	got := edgeSet(t, graphGeometricEdges([]namedCircle{a, b}))
	if _, ok := got["A>B:overlaps"]; !ok {
		t.Fatalf("部分的な重なりの辺がない: %+v", got)
	}
	if len(got) != 1 {
		t.Fatalf("辺 = %d 本, want 1: %+v", len(got), got)
	}
}

func TestGraphGeometricEdgesEqualCirclesOverlap(t *testing.T) {
	a := namedCircle{ID: "A", GroupCircle: GroupCircle{Lat: 35, Lon: 135, RadiusM: 100}}
	b := namedCircle{ID: "B", GroupCircle: GroupCircle{Lat: 35, Lon: 135, RadiusM: 100}}
	got := edgeSet(t, graphGeometricEdges([]namedCircle{a, b}))
	if _, ok := got["A>B:overlaps"]; !ok {
		t.Fatalf("同一の円は overlaps のはず: %+v", got)
	}
}

func TestGraphGeometricEdgesFarApart(t *testing.T) {
	nodes := []namedCircle{
		{ID: "A", GroupCircle: GroupCircle{Lat: 35, Lon: 135, RadiusM: 40}},
		{ID: "B", GroupCircle: GroupCircle{Lat: 36, Lon: 136, RadiusM: 40}},
	}
	if edges := graphGeometricEdges(nodes); len(edges) != 0 {
		t.Fatalf("離れた円に辺がある: %+v", edges)
	}
}

func TestGraphSharedEdges(t *testing.T) {
	members := map[string]map[string]bool{
		"A": {"u1": true, "u2": true, "u3": true},
		"B": {"u2": true, "u3": true, "u4": true},
		"C": {"u3": true, "u5": true},
	}
	edges := graphSharedEdges(members)
	got := edgeSet(t, edges)
	e, ok := got["A>B:shared_members"]
	if !ok || e.Weight != 2 {
		t.Fatalf("A-B の shared_members = %+v (存在=%v), want weight 2", e, ok)
	}
	if len(got) != 1 {
		t.Fatalf("辺 = %d 本, want 1（1 人しか共通しない組は出さない）: %+v", len(got), got)
	}
}
