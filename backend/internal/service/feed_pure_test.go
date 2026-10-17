package service

import (
	"context"
	"testing"
	"time"
)

// 複数 source のマージ（17 §3.3）の単体テスト。API では 1 つしか使わないが、
// FeedService はカンマ区切りの複数を扱えるようにしてある。

type fakeFeedSource struct{ items []FeedItem }

func (f *fakeFeedSource) Fetch(_ context.Context, _ string, before FeedCursor, limit int) ([]FeedItem, error) {
	out := make([]FeedItem, 0, limit)
	for _, it := range f.items {
		if !feedItemOlderThan(it, before) {
			continue
		}
		out = append(out, it)
		if len(out) == limit {
			break
		}
	}
	return out, nil
}

func feedItemOlderThan(it FeedItem, before FeedCursor) bool {
	if before.At.IsZero() && before.URI == "" {
		return true
	}
	if !it.CreatedAt.Equal(before.At) {
		return it.CreatedAt.Before(before.At)
	}
	return it.URI < before.URI
}

func feedItem(uri string, at time.Time) FeedItem {
	return FeedItem{URI: uri, CreatedAt: at}
}

func keepAll(*FeedItem) (bool, error) { return true, nil }

func TestMergeSourcesOrderAndCursor(t *testing.T) {
	base := time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC)
	a := &fakeFeedSource{items: []FeedItem{
		feedItem("a5", base.Add(5*time.Minute)),
		feedItem("a4", base.Add(4*time.Minute)),
		feedItem("a2", base.Add(2*time.Minute)),
	}}
	b := &fakeFeedSource{items: []FeedItem{
		feedItem("b5", base.Add(5*time.Minute)),
		feedItem("b3", base.Add(3*time.Minute)),
		feedItem("b1", base.Add(1*time.Minute)),
	}}
	sources := []FeedSource{a, b}

	// createdAt の新しい順、同じなら uri の大きい順（b5 > a5 の順になる）。
	want := []string{"b5", "a5", "a4", "b3", "a2", "b1"}

	var got []string
	before := FeedCursor{}
	for page := 0; page < 10; page++ {
		items, next, hasMore, err := mergeSources(context.Background(), sources, "did:plc:me", before, 2, keepAll)
		if err != nil {
			t.Fatalf("page %d: %v", page, err)
		}
		for _, it := range items {
			got = append(got, it.URI)
		}
		if !hasMore {
			break
		}
		before = next
	}

	if len(got) != len(want) {
		t.Fatalf("merged = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("merged[%d] = %q, want %q (all=%v)", i, got[i], want[i], got)
		}
	}
}

func TestMergeSourcesFilteringKeepsCursor(t *testing.T) {
	base := time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC)
	a := &fakeFeedSource{items: []FeedItem{
		feedItem("a5", base.Add(5*time.Minute)),
		feedItem("a4", base.Add(4*time.Minute)),
		feedItem("a2", base.Add(2*time.Minute)),
	}}
	b := &fakeFeedSource{items: []FeedItem{
		feedItem("b5", base.Add(5*time.Minute)),
		feedItem("b3", base.Add(3*time.Minute)),
		feedItem("b1", base.Add(1*time.Minute)),
	}}
	sources := []FeedSource{a, b}
	keep := func(it *FeedItem) (bool, error) { return it.URI[0] == 'b', nil }

	var got []string
	before := FeedCursor{}
	for page := 0; page < 10; page++ {
		items, next, hasMore, err := mergeSources(context.Background(), sources, "did:plc:me", before, 1, keep)
		if err != nil {
			t.Fatalf("page %d: %v", page, err)
		}
		for _, it := range items {
			got = append(got, it.URI)
		}
		if !hasMore {
			break
		}
		before = next
	}

	want := []string{"b5", "b3", "b1"}
	if len(got) != len(want) {
		t.Fatalf("filtered merged = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("filtered merged[%d] = %q, want %q (all=%v)", i, got[i], want[i], got)
		}
	}
}

func TestBuildSources(t *testing.T) {
	s := &FeedService{}
	for _, name := range []string{"home", "groups", "space:0192", "user:did:plc:abc", "home,groups"} {
		if _, err := s.buildSources(name); err != nil {
			t.Fatalf("buildSources(%q) = %v, want nil", name, err)
		}
	}
	for _, name := range []string{"", "nope", "space:", "user:", "home,", "home,,groups", "posts"} {
		if _, err := s.buildSources(name); err == nil {
			t.Fatalf("buildSources(%q) = nil, want error", name)
		}
	}
}
