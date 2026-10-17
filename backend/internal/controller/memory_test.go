//go:build integration

package controller_test

import (
	"net/http"
	"testing"

	"github.com/applemango/chain/internal/testutil"
)

// memory の API テスト（13 §2）。

const (
	archivedPartyID   = "party-arch"
	archivedGroupID   = "group-arch"
	activeGroupID     = "group-active"
	deletedGroupID    = "group-deleted"
	nonMemberGroupID  = "group-nonmember"
	groupTypeNSID     = "jp.i32.chain.space.group"
	partyTypeNSID     = "jp.i32.chain.space.party"
	spaceAuthorityDID = "did:web:chain.test"
)

// archivedSpace は archived の Space を、created_at と archived_at を指定して作る。
func archivedSpace(t *testing.T, env *testutil.Env, id, typ, createdBy string, createdAt, archivedAt int64) {
	t.Helper()
	execSQL(t, env, `INSERT INTO spaces (id, type, authority, ref, name, state, created_by, created_at, archived_at)
		VALUES (?, ?, ?, ?, ?, 'archived', ?, ?, ?)`,
		id, typ, spaceAuthorityDID,
		"at://"+spaceAuthorityDID+"/space/"+typ+"/"+id, "memory "+id, createdBy, createdAt, archivedAt)
}

// leftMember は退出済みのメンバー行を作る。
func leftMember(t *testing.T, env *testutil.Env, id, did string, joinedAt, leftAt int64) {
	t.Helper()
	execSQL(t, env, `INSERT INTO space_members (space_id, did, can_read, can_write, role, joined_at, left_at)
		VALUES (?, ?, 1, 1, 'member', ?, ?)`, id, did, joinedAt, leftAt)
}

type memoryItem struct {
	SpaceID     string `json:"spaceId"`
	Type        string `json:"type"`
	Name        string `json:"name"`
	From        string `json:"from"`
	Until       string `json:"until"`
	MemberCount int    `json:"memberCount"`
	MediaCount  int    `json:"mediaCount"`
	Cover       *struct {
		ThumbURL string `json:"thumbUrl"`
	} `json:"cover"`
}

type memoryPage struct {
	Items  []memoryItem `json:"items"`
	Cursor string       `json:"cursor"`
}

// TestMemoriesMixPartiesAndGroups は archived の Party とグループが until の新しい順に混ざり、
// active・deleted・メンバーでない Space は出ないことを確かめる。
func TestMemoriesMixPartiesAndGroups(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)
	env.CreateUser(t, apiAlice, "alice.test")
	alice := env.Login(t, apiAlice)
	env.CreateUser(t, apiBob, "bob.test")

	base := env.Clock.Now().UnixMilli()

	// グループ: created_at が from、archived_at が until。退出済みの alice と在籍中の bob。
	archivedSpace(t, env, archivedGroupID, groupTypeNSID, apiAlice, base-2000, base+2000)
	group(t, env, archivedGroupID, true)
	leftMember(t, env, archivedGroupID, apiAlice, base-2000, base)
	member(t, env, archivedGroupID, apiBob, true)
	execSQL(t, env, `INSERT INTO media (id, owner_did, space_id, mime, width, height, bytes, sha256, created_at)
		VALUES ('gm-1', ?, ?, 'image/jpeg', 8, 8, 100, 'abc', ?)`, apiAlice, archivedGroupID, base-1500)

	// Party: until は group より古い。
	archivedSpace(t, env, archivedPartyID, partyTypeNSID, apiAlice, base-4000, base+1000)
	execSQL(t, env, `INSERT INTO parties (space_id, phase, tentative_at, confirmed_at, ended_at, last_activity_at)
		VALUES (?, 'ended', ?, ?, ?, ?)`, archivedPartyID, base-4000, base-4000, base+1000, base+1000)
	leftMember(t, env, archivedPartyID, apiAlice, base-4000, base)

	// 出てはいけないもの。
	space(t, env, activeGroupID, groupTypeNSID, "active", apiAlice)
	group(t, env, activeGroupID, true)
	member(t, env, activeGroupID, apiAlice, true)

	space(t, env, deletedGroupID, groupTypeNSID, "deleted", apiAlice)
	group(t, env, deletedGroupID, true)
	member(t, env, deletedGroupID, apiAlice, true)

	archivedSpace(t, env, nonMemberGroupID, groupTypeNSID, apiBob, base-1000, base+3000)
	group(t, env, nonMemberGroupID, true)
	member(t, env, nonMemberGroupID, apiBob, true)

	status, body := request(t, app, http.MethodGet, "/api/v1/memories", "", alice)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", status, body)
	}
	var page memoryPage
	mustJSONBody(t, body, &page)
	if len(page.Items) != 2 {
		t.Fatalf("items = %+v, want 2 (group + party)", page.Items)
	}

	first := page.Items[0]
	if first.SpaceID != archivedGroupID || first.Type != "group" {
		t.Fatalf("first = %+v, want the group (newest until)", first)
	}
	if first.MemberCount != 2 || first.MediaCount != 1 {
		t.Fatalf("group counts = %+v, want memberCount 2 (延べ), mediaCount 1", first)
	}
	if first.Cover == nil || first.Cover.ThumbURL == "" {
		t.Fatalf("group cover = %+v, want a signed thumb url", first.Cover)
	}

	second := page.Items[1]
	if second.SpaceID != archivedPartyID || second.Type != "party" {
		t.Fatalf("second = %+v, want the party", second)
	}
	if second.MemberCount != 1 || second.MediaCount != 0 {
		t.Fatalf("party counts = %+v, want memberCount 1, mediaCount 0", second)
	}
}

// TestDeleteMemoryHidesOnlyForSelf は DELETE /memories/{id} が自分の一覧からだけ消し、
// 関係のない Space は 404 になることを確かめる。
func TestDeleteMemoryHidesOnlyForSelf(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)
	env.CreateUser(t, apiAlice, "alice.test")
	alice := env.Login(t, apiAlice)
	env.CreateUser(t, apiBob, "bob.test")
	bob := env.Login(t, apiBob)
	env.CreateUser(t, apiCarol, "carol.test")
	carol := env.Login(t, apiCarol)

	base := env.Clock.Now().UnixMilli()
	archivedSpace(t, env, archivedGroupID, groupTypeNSID, apiAlice, base-1000, base+1000)
	group(t, env, archivedGroupID, true)
	leftMember(t, env, archivedGroupID, apiAlice, base-1000, base)
	member(t, env, archivedGroupID, apiBob, true)

	space(t, env, activeGroupID, groupTypeNSID, "active", apiAlice)
	group(t, env, activeGroupID, true)
	member(t, env, activeGroupID, apiAlice, true)

	status, body := request(t, app, http.MethodDelete, "/api/v1/memories/"+archivedGroupID, "", alice)
	if status != http.StatusNoContent {
		t.Fatalf("delete: status = %d, want 204 (body=%s)", status, body)
	}

	_, aliceBody := request(t, app, http.MethodGet, "/api/v1/memories", "", alice)
	var alicePage memoryPage
	mustJSONBody(t, aliceBody, &alicePage)
	if len(alicePage.Items) != 0 {
		t.Fatalf("alice items = %+v, want 0 after hiding", alicePage.Items)
	}

	_, bobBody := request(t, app, http.MethodGet, "/api/v1/memories", "", bob)
	var bobPage memoryPage
	mustJSONBody(t, bobBody, &bobPage)
	if len(bobPage.Items) != 1 || bobPage.Items[0].SpaceID != archivedGroupID {
		t.Fatalf("bob items = %+v, want the group still visible", bobPage.Items)
	}

	// メンバーでない人・active・存在しない Space は 404。
	status, _ = request(t, app, http.MethodDelete, "/api/v1/memories/"+archivedGroupID, "", carol)
	if status != http.StatusNotFound {
		t.Fatalf("carol delete archived: status = %d, want 404", status)
	}
	status, _ = request(t, app, http.MethodDelete, "/api/v1/memories/"+activeGroupID, "", alice)
	if status != http.StatusNotFound {
		t.Fatalf("delete active: status = %d, want 404", status)
	}
	status, _ = request(t, app, http.MethodDelete, "/api/v1/memories/no-such-space", "", alice)
	if status != http.StatusNotFound {
		t.Fatalf("delete missing: status = %d, want 404", status)
	}
}

// TestArchivedGroupReadableByFormerMember は、元メンバー（退出済みを含む）が archived の
// グループの中身を読め、関係のない人は読めないことを確かめる（10 §5、17 §3）。
func TestArchivedGroupReadableByFormerMember(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)
	env.CreateUser(t, apiAlice, "alice.test")
	alice := env.Login(t, apiAlice)
	env.CreateUser(t, apiBob, "bob.test")
	env.CreateUser(t, apiCarol, "carol.test")
	carol := env.Login(t, apiCarol)

	base := env.Clock.Now().UnixMilli()
	archivedSpace(t, env, archivedGroupID, groupTypeNSID, apiAlice, base-1000, base+1000)
	group(t, env, archivedGroupID, true)
	leftMember(t, env, archivedGroupID, apiAlice, base-1000, base)
	insertInstancePost(t, env, archivedGroupID, groupTypeNSID, apiAlice, "r1", "昔の投稿", env.Clock.Now(), "ok")

	status, body := request(t, app, http.MethodGet, "/api/v1/feed?source=space:"+archivedGroupID, "", alice)
	if status != http.StatusOK {
		t.Fatalf("feed: status = %d, want 200 (body=%s)", status, body)
	}
	var page postPage
	mustJSONBody(t, body, &page)
	if len(page.Items) != 1 || page.Items[0].Text != "昔の投稿" {
		t.Fatalf("feed items = %+v, want the archived post", page.Items)
	}

	status, body = request(t, app, http.MethodGet, "/api/v1/instances/"+archivedGroupID+"/posts", "", alice)
	if status != http.StatusOK {
		t.Fatalf("instance posts: status = %d, want 200 (body=%s)", status, body)
	}
	page = postPage{}
	mustJSONBody(t, body, &page)
	if len(page.Items) != 1 || page.Items[0].Text != "昔の投稿" {
		t.Fatalf("instance posts = %+v, want the archived post", page.Items)
	}

	status, body = request(t, app, http.MethodGet, "/api/v1/feed?source=space:"+archivedGroupID, "", carol)
	if status != http.StatusForbidden {
		t.Fatalf("carol feed: status = %d, want 403 (body=%s)", status, body)
	}
}
