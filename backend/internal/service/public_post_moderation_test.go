//go:build integration

package service_test

import (
	"context"
	"testing"

	"github.com/applemango/chain/internal/external/atproto"
	"github.com/applemango/chain/internal/model"
	"github.com/applemango/chain/internal/service"
	"github.com/applemango/chain/internal/testutil"
)

// 取り込んだ公開投稿の moderation（11 §2・§3）。

func insertPublicPost(t *testing.T, env *testutil.Env, uri, did, text, state string) {
	t.Helper()
	now := env.Clock.Now()
	if _, err := model.UpsertPublicPost(context.Background(), env.DB, model.PublicPost{
		URI: uri, CID: "c1", AuthorDID: did, Text: text, CreatedAt: now, IndexedAt: now,
		ModLabel: "ok", ModState: state,
	}); err != nil {
		t.Fatalf("UpsertPublicPost: %v", err)
	}
}

func TestPublicPostModerationHidesOnBlock(t *testing.T) {
	ctx := context.Background()
	h := newModHarness(t, &testutil.FakeAssessor{BlockWords: []string{"誹謗"}})
	h.env.CreateUser(t, alice, "alice.test")

	uri := atproto.FeedPostURI(alice, "rkey1")
	insertPublicPost(t, h.env, uri, alice, "これは誹謗です", "pending")
	enqueue(t, h.env, uri)

	if _, err := h.queue.ProcessPending(ctx); err != nil {
		t.Fatalf("ProcessPending: %v", err)
	}

	p, err := model.GetPublicPost(ctx, h.env.DB, uri)
	if err != nil {
		t.Fatalf("GetPublicPost: %v", err)
	}
	if p.ModLabel != "block" || p.ModState != "done" {
		t.Fatalf("mod = (%q,%q), want (block,done)", p.ModLabel, p.ModState)
	}
	if jobs := modQueueJobs(t, h.env); len(jobs) != 0 {
		t.Fatalf("modq = %+v, want empty", jobs)
	}

	var n int
	if err := h.env.DB.QueryRowContext(ctx,
		"SELECT count(*) FROM notifications WHERE did = ? AND type = ?", alice, service.NotificationModerationHidden).Scan(&n); err != nil {
		t.Fatalf("count notifications: %v", err)
	}
	if n != 1 {
		t.Fatalf("moderation.hidden notifications = %d, want 1", n)
	}
	if ev := h.broker.EventsFor("notification", alice); len(ev) != 1 {
		t.Fatalf("notification events = %d, want 1", len(ev))
	}
}

func TestPublicPostModerationUpdatesNonBlockLabel(t *testing.T) {
	ctx := context.Background()
	cases := map[string]struct {
		assessor *testutil.FakeAssessor
		text     string
		want     string
	}{
		"warn": {&testutil.FakeAssessor{WarnWords: []string{"乱暴"}}, "少し乱暴な表現", "warn"},
		"ok":   {&testutil.FakeAssessor{}, "ふつうの投稿", "ok"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			h := newModHarness(t, tc.assessor)
			h.env.CreateUser(t, alice, "alice.test")
			uri := atproto.FeedPostURI(alice, "rkey2")
			insertPublicPost(t, h.env, uri, alice, tc.text, "pending")
			enqueue(t, h.env, uri)

			if _, err := h.queue.ProcessPending(ctx); err != nil {
				t.Fatalf("ProcessPending: %v", err)
			}
			p, err := model.GetPublicPost(ctx, h.env.DB, uri)
			if err != nil {
				t.Fatalf("GetPublicPost: %v", err)
			}
			if p.ModLabel != tc.want || p.ModState != "done" {
				t.Fatalf("mod = (%q,%q), want (%s,done)", p.ModLabel, p.ModState, tc.want)
			}
			if jobs := modQueueJobs(t, h.env); len(jobs) != 0 {
				t.Fatalf("modq = %+v, want empty", jobs)
			}
		})
	}
}

func TestPublicPostModerationSkipsUnknownAndDone(t *testing.T) {
	ctx := context.Background()
	h := newModHarness(t, &testutil.FakeAssessor{BlockWords: []string{"誹謗"}})
	h.env.CreateUser(t, alice, "alice.test")

	enqueue(t, h.env, atproto.FeedPostURI(alice, "missing"))

	done := atproto.FeedPostURI(alice, "rkey3")
	insertPublicPost(t, h.env, done, alice, "誹謗です", "done")
	enqueue(t, h.env, done)

	if _, err := h.queue.ProcessPending(ctx); err != nil {
		t.Fatalf("ProcessPending: %v", err)
	}
	if jobs := modQueueJobs(t, h.env); len(jobs) != 0 {
		t.Fatalf("modq = %+v, want empty", jobs)
	}
	p, err := model.GetPublicPost(ctx, h.env.DB, done)
	if err != nil {
		t.Fatalf("GetPublicPost: %v", err)
	}
	if p.ModLabel != "ok" {
		t.Fatalf("mod_label = %q, want unchanged ok", p.ModLabel)
	}
}
