//go:build integration

package content_test

import (
	"context"
	"testing"

	"github.com/applemango/chain/internal/service/access"
	"github.com/applemango/chain/internal/service/content"
	groupsvc "github.com/applemango/chain/internal/service/group"
	"github.com/applemango/chain/internal/testutil"
)

// 11 §3 の 3 経路を fake の判定器で確かめる。投稿は block / warn / pending、
// グループ名は block / warn。

func TestPostModerationRoutes(t *testing.T) {
	ctx := context.Background()

	t.Run("block は 422 で保存しない", func(t *testing.T) {
		h := newModHarness(t, &testutil.FakeAssessor{BlockWords: []string{"ばか"}})
		h.env.CreateUser(t, alice, "alice.test")
		h.env.Consent(t, alice)
		writableGroup(t, h.env, "sp1", alice)

		_, err := h.inst.CreatePosts(ctx, alice, content.CreatePostInput{SpaceIDs: []string{"sp1"}, Text: "ばか"})
		testutil.WantCode(t, err, "moderation_blocked")

		var n int
		if err := h.env.DB.QueryRowContext(ctx, "SELECT count(*) FROM records").Scan(&n); err != nil {
			t.Fatalf("count records: %v", err)
		}
		if n != 0 {
			t.Fatalf("records = %d, want 0 (blocked posts must not be stored)", n)
		}
	})

	t.Run("warn は warn で保存", func(t *testing.T) {
		h := newModHarness(t, &testutil.FakeAssessor{WarnWords: []string{"きもい"}})
		h.env.CreateUser(t, alice, "alice.test")
		h.env.Consent(t, alice)
		writableGroup(t, h.env, "sp1", alice)

		res, err := h.inst.CreatePosts(ctx, alice, content.CreatePostInput{SpaceIDs: []string{"sp1"}, Text: "きもい"})
		if err != nil {
			t.Fatalf("CreatePosts: %v", err)
		}
		if label, state := recordMod(t, h.env, res.Items[0].URI); label != "warn" || state != "done" {
			t.Fatalf("mod = (%q,%q), want (warn,done)", label, state)
		}
	})

	t.Run("判定エラーは pending で保存し modq に積む", func(t *testing.T) {
		h := newModHarness(t, &testutil.FakeAssessor{FailWords: []string{"とりあえず"}})
		h.env.CreateUser(t, alice, "alice.test")
		h.env.Consent(t, alice)
		writableGroup(t, h.env, "sp1", alice)

		res, err := h.inst.CreatePosts(ctx, alice, content.CreatePostInput{SpaceIDs: []string{"sp1"}, Text: "とりあえず"})
		if err != nil {
			t.Fatalf("CreatePosts: %v", err)
		}
		if label, state := recordMod(t, h.env, res.Items[0].URI); label != "ok" || state != "pending" {
			t.Fatalf("mod = (%q,%q), want (ok,pending)", label, state)
		}
		if jobs := modQueueJobs(t, h.env); len(jobs) != 1 || jobs[0].URI != res.Items[0].URI {
			t.Fatalf("modq = %+v, want the created URI", jobs)
		}
	})
}

func TestGroupNameModerationRoutes(t *testing.T) {
	ctx := context.Background()
	env := testutil.NewEnv(t)
	env.CreateUser(t, alice, "alice.test")

	zones := testutil.NewFakeZonePresence()
	zones.SetStale(false)
	zones.Set(alice, true)
	access := &access.AccessService{Deps: env.Deps, Zones: zones}
	groups := &groupsvc.GroupService{Deps: env.Deps, Zones: zones, Access: access,
		Moderator: &testutil.FakeAssessor{BlockWords: []string{"ばか"}, WarnWords: []string{"きもい"}}}

	t.Run("block は 422", func(t *testing.T) {
		_, err := groups.Create(ctx, alice, groupsvc.CreateGroupInput{Name: "ばか", Lat: 35, Lon: 135, RadiusM: 40})
		testutil.WantCode(t, err, "moderation_blocked")
	})

	t.Run("warn は warn で保存", func(t *testing.T) {
		d, err := groups.Create(ctx, alice, groupsvc.CreateGroupInput{Name: "きもい", Lat: 35, Lon: 135, RadiusM: 40})
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		var label string
		if err := env.DB.QueryRowContext(ctx, "SELECT mod_label FROM groups WHERE space_id = ?", d.ID).Scan(&label); err != nil {
			t.Fatalf("select mod_label: %v", err)
		}
		if label != "warn" {
			t.Fatalf("group mod_label = %q, want warn", label)
		}
	})
}
