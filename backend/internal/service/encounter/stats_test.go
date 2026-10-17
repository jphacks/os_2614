//go:build integration

package encounter_test

import (
	"context"
	"testing"
	"time"

	"github.com/applemango/chain/internal/testutil"
)

func insertFriendship(t *testing.T, env *testutil.Env, a, b string, at time.Time) {
	t.Helper()
	aa, bb := a, b
	if aa > bb {
		aa, bb = bb, aa
	}
	if _, err := env.DB.ExecContext(context.Background(),
		`INSERT INTO friendships (user_a, user_b, created_at, via) VALUES (?, ?, ?, 'encounter')`,
		aa, bb, at.UnixMilli()); err != nil {
		t.Fatalf("insert friendship: %v", err)
	}
}

func insertSpace(t *testing.T, env *testutil.Env, id, typ, state, owner string) {
	t.Helper()
	if _, err := env.DB.ExecContext(context.Background(), `
		INSERT INTO spaces (id, type, authority, ref, name, state, created_by, created_at)
		VALUES (?, ?, 'did:web:chain.test', ?, ?, ?, ?, ?)`,
		id, typ, "at://did:web:chain.test/space/"+typ+"/"+id, id, state, owner, env.Clock.Now().UnixMilli()); err != nil {
		t.Fatalf("insert space %s: %v", id, err)
	}
}

func insertParty(t *testing.T, env *testutil.Env, id, owner, state, phase string) {
	t.Helper()
	insertSpace(t, env, id, "jp.i32.chain.space.party", state, owner)
	if _, err := env.DB.ExecContext(context.Background(),
		`INSERT INTO parties (space_id, phase, locked, tentative_at, last_activity_at) VALUES (?, ?, 0, ?, ?)`,
		id, phase, env.Clock.Now().UnixMilli(), env.Clock.Now().UnixMilli()); err != nil {
		t.Fatalf("insert party %s: %v", id, err)
	}
}

func insertGroup(t *testing.T, env *testutil.Env, id, owner, state string) {
	t.Helper()
	insertSpace(t, env, id, "jp.i32.chain.space.group", state, owner)
	if _, err := env.DB.ExecContext(context.Background(), `
		INSERT INTO groups (space_id, visibility, origin, center_lat, center_lon, radius_m)
		VALUES (?, 'open', 'explicit', 35.0, 139.0, 100)`, id); err != nil {
		t.Fatalf("insert group %s: %v", id, err)
	}
}

func insertMember(t *testing.T, env *testutil.Env, spaceID, did string, leftAt *time.Time) {
	t.Helper()
	var left any
	if leftAt != nil {
		left = leftAt.UnixMilli()
	}
	if _, err := env.DB.ExecContext(context.Background(), `
		INSERT INTO space_members (space_id, did, can_read, can_write, role, joined_at, left_at, hidden_from_memory)
		VALUES (?, ?, 1, 1, 'member', ?, ?, 0)`,
		spaceID, did, env.Clock.Now().UnixMilli(), left); err != nil {
		t.Fatalf("insert member %s/%s: %v", spaceID, did, err)
	}
}

func insertPost(t *testing.T, env *testutil.Env, id, spaceID, author string, at time.Time, deleted bool) {
	t.Helper()
	var deletedAt any
	if deleted {
		deletedAt = at.UnixMilli()
	}
	if _, err := env.DB.ExecContext(context.Background(), `
		INSERT INTO records (uri, space_id, author_did, collection, rkey, value_json, created_at, deleted_at)
		VALUES (?, ?, ?, 'jp.i32.chain.instance.post', ?, '{}', ?, ?)`,
		"at://did:web:chain.test/post/"+id, spaceID, author, id, at.UnixMilli(), deletedAt); err != nil {
		t.Fatalf("insert post %s: %v", id, err)
	}
}

func TestStatsServiceCounts(t *testing.T) {
	env := testutil.NewEnv(t)
	const me = "did:plc:me"
	env.CreateUser(t, me, "me.test")
	f1, f2 := "did:plc:f1", "did:plc:f2"
	for _, d := range []string{f1, f2} {
		env.CreateUser(t, d, d[8:]+".test")
	}
	now := testutil.JSTTime(2026, 10, 17, 12, 0)
	env.Clock.Set(now)

	insertFriendship(t, env, me, f1, now)
	insertFriendship(t, env, me, f2, now)

	// Party: active + archived は数え、破棄された仮 party（物理削除）は数えない。
	insertParty(t, env, "party-a", me, "active", "confirmed")
	insertParty(t, env, "party-b", me, "archived", "ended")
	insertMember(t, env, "party-a", me, nil)
	insertMember(t, env, "party-b", me, nil)
	insertParty(t, env, "party-dissolved", me, "active", "tentative")
	insertMember(t, env, "party-dissolved", me, nil)
	if _, err := env.DB.ExecContext(context.Background(), "DELETE FROM spaces WHERE id = 'party-dissolved'"); err != nil {
		t.Fatalf("dissolve party: %v", err)
	}

	// Group: active で今メンバーのものだけ数える（退出済み・archived は数えない）。
	insertGroup(t, env, "group-a", me, "active")
	insertGroup(t, env, "group-b", me, "active")
	insertGroup(t, env, "group-archived", me, "archived")
	insertGroup(t, env, "group-left", me, "active")
	insertMember(t, env, "group-a", me, nil)
	insertMember(t, env, "group-b", me, nil)
	insertMember(t, env, "group-archived", me, nil)
	leftAt := now.Add(-time.Hour)
	insertMember(t, env, "group-left", me, &leftAt)

	// Post: 今月の自分の instance の投稿だけ数える（先月・削除済みは数えない）。
	insertPost(t, env, "post-1", "group-a", me, testutil.JSTTime(2026, 10, 2, 10, 0), false)
	insertPost(t, env, "post-2", "group-a", me, testutil.JSTTime(2026, 10, 17, 9, 0), false)
	insertPost(t, env, "post-3", "group-b", me, testutil.JSTTime(2026, 10, 17, 11, 0), false)
	insertPost(t, env, "post-deleted", "group-a", me, testutil.JSTTime(2026, 10, 5, 10, 0), true)
	insertPost(t, env, "post-sep", "group-a", me, testutil.JSTTime(2026, 9, 20, 10, 0), false)

	testutil.InsertEncounter(t, env, "enc-1", me, f1, now.Add(-2*time.Hour), "revealed")
	testutil.InsertEncounter(t, env, "enc-2", me, f2, now.Add(-3*time.Hour), "hidden")

	st, err := env.Stats().Get(context.Background(), me)
	if err != nil {
		t.Fatalf("Stats.Get: %v", err)
	}
	if st.Friends != 2 {
		t.Fatalf("friends = %d, want 2", st.Friends)
	}
	if st.Parties.Total != 2 {
		t.Fatalf("parties.total = %d, want 2", st.Parties.Total)
	}
	if st.Groups.Member != 2 {
		t.Fatalf("groups.member = %d, want 2", st.Groups.Member)
	}
	if st.Posts.Month != 3 {
		t.Fatalf("posts.month = %d, want 3", st.Posts.Month)
	}
	if st.Encounters.Today != 2 || st.Encounters.Total != 2 || st.Encounters.UniquePeople != 2 {
		t.Fatalf("encounters = %+v, want today/total/unique = 2", st.Encounters)
	}
}

func TestStatsServiceCache(t *testing.T) {
	env := testutil.NewEnv(t)
	const me = "did:plc:me"
	env.CreateUser(t, me, "me.test")
	env.CreateUser(t, "did:plc:f1", "f1.test")
	env.Clock.Set(testutil.JSTTime(2026, 10, 17, 12, 0))

	if st, err := env.Stats().Get(context.Background(), me); err != nil || st.Friends != 0 {
		t.Fatalf("first Get = %+v, %v; want friends 0", st, err)
	}
	insertFriendship(t, env, me, "did:plc:f1", env.Clock.Now())

	// 5 分以内はキャッシュされた古い値。
	if st, err := env.Stats().Get(context.Background(), me); err != nil || st.Friends != 0 {
		t.Fatalf("cached Get = %+v, %v; want friends 0 (5 分以内は古い値)", st, err)
	}
	// 5 分後は集計し直す。
	env.Clock.Advance(5 * time.Minute)
	if st, err := env.Stats().Get(context.Background(), me); err != nil || st.Friends != 1 {
		t.Fatalf("refreshed Get = %+v, %v; want friends 1 (5 分後に更新)", st, err)
	}
}
