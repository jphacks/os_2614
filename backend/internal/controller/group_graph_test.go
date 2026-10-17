//go:build integration

package controller_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/applemango/chain/internal/service"
	"github.com/applemango/chain/internal/testutil"
)

// 09 §7 のグラフ API と、09 §6.1 の入場通知が PUT /me/location のフックから動くこと。

type graphNode struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	RadiusM     int    `json:"radiusM"`
	MemberCount int    `json:"memberCount"`
	IsMember    bool   `json:"isMember"`
	Inside      bool   `json:"inside"`
	Origin      string `json:"origin"`
}

type graphEdge struct {
	From   string `json:"from"`
	To     string `json:"to"`
	Kind   string `json:"kind"`
	Weight int    `json:"weight"`
}

type graphResponse struct {
	Nodes []graphNode `json:"nodes"`
	Edges []graphEdge `json:"edges"`
}

func fetchGraph(t *testing.T, app *fiber.App, token string) (int, graphResponse, []byte) {
	t.Helper()
	status, body := request(t, app, http.MethodGet, "/api/v1/groups/graph", "", token)
	var res graphResponse
	if status == http.StatusOK {
		mustJSONBody(t, body, &res)
	}
	return status, res, body
}

func createOpenGroup(t *testing.T, app *fiber.App, token, name string, radiusM int) groupDetail {
	t.Helper()
	body := fmt.Sprintf(`{"name":%q,"geofence":{"lat":%v,"lon":%v,"radiusM":%d}}`, name, groupLat, groupLon, radiusM)
	status, resp := createGroup(t, app, token, body)
	if status != http.StatusCreated {
		t.Fatalf("create %s: status = %d (body=%s)", name, status, resp)
	}
	return decodeGroup(t, resp)
}

func findNode(nodes []graphNode, id string) *graphNode {
	for i := range nodes {
		if nodes[i].ID == id {
			return &nodes[i]
		}
	}
	return nil
}

func directedEdge(edges []graphEdge, from, to, kind string) (graphEdge, bool) {
	for _, e := range edges {
		if e.From == from && e.To == to && e.Kind == kind {
			return e, true
		}
	}
	return graphEdge{}, false
}

func anyEdge(edges []graphEdge, a, b, kind string) (graphEdge, bool) {
	if e, ok := directedEdge(edges, a, b, kind); ok {
		return e, true
	}
	return directedEdge(edges, b, a, kind)
}

func hasJSONKey(v any, key string) bool {
	switch x := v.(type) {
	case map[string]any:
		if _, ok := x[key]; ok {
			return true
		}
		for _, vv := range x {
			if hasJSONKey(vv, key) {
				return true
			}
		}
	case []any:
		for _, vv := range x {
			if hasJSONKey(vv, key) {
				return true
			}
		}
	}
	return false
}

func waitForNotification(t *testing.T, env *testutil.Env, did, typ string) bool {
	t.Helper()
	ctx := context.Background()
	for i := 0; i < 300; i++ {
		var n int
		if err := env.DB.QueryRowContext(ctx,
			"SELECT count(*) FROM notifications WHERE did = ? AND type = ?", did, typ).Scan(&n); err != nil {
			t.Fatalf("count notifications: %v", err)
		}
		if n > 0 {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

func TestGroupGraphNestedAndNoCenter(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)
	env.CreateUser(t, "did:plc:alice", "alice.test")
	alice := env.Login(t, "did:plc:alice")
	setGroupLocation(t, env, "did:plc:alice", groupLat, groupLon)

	big := createOpenGroup(t, app, alice, "大学", 800)
	small := createOpenGroup(t, app, alice, "教室", 40)

	status, res, body := fetchGraph(t, app, alice)
	if status != http.StatusOK {
		t.Fatalf("graph: status = %d, want 200 (body=%s)", status, body)
	}
	bigNode := findNode(res.Nodes, big.ID)
	if bigNode == nil || !bigNode.IsMember || !bigNode.Inside || bigNode.Origin != "explicit" || bigNode.RadiusM != 800 {
		t.Fatalf("big node = %+v, want member / inside / explicit / 800", bigNode)
	}
	if findNode(res.Nodes, small.ID) == nil {
		t.Fatalf("small node がない: %+v", res.Nodes)
	}
	if _, ok := directedEdge(res.Edges, big.ID, small.ID, "contains"); !ok {
		t.Fatalf("contains 辺がない: %+v", res.Edges)
	}

	var raw any
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatalf("decode raw: %v", err)
	}
	for _, key := range []string{"centerLat", "centerLon", "lat", "lon"} {
		if hasJSONKey(raw, key) {
			t.Fatalf("中心座標らしき項目 %q が返っている: %s", key, body)
		}
	}

	// 位置が古いと 403。
	env.Clock.Advance(3 * time.Minute)
	status, _, body = fetchGraph(t, app, alice)
	if status != http.StatusForbidden {
		t.Fatalf("stale graph: status = %d, want 403 (body=%s)", status, body)
	}
	wantErrorCode(t, body, "location_stale")
}

func TestGroupGraphRequiresLocation(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)
	env.CreateUser(t, "did:plc:carol", "carol.test")
	carol := env.Login(t, "did:plc:carol")

	status, _, body := fetchGraph(t, app, carol)
	if status != http.StatusForbidden {
		t.Fatalf("no location: status = %d, want 403 (body=%s)", status, body)
	}
	wantErrorCode(t, body, "location_stale")
}

func TestGroupGraphClosedVisibility(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)
	env.CreateUser(t, "did:plc:alice", "alice.test")
	alice := env.Login(t, "did:plc:alice")
	env.CreateUser(t, "did:plc:bob", "bob.test")
	bob := env.Login(t, "did:plc:bob")
	setGroupLocation(t, env, "did:plc:alice", groupLat, groupLon)

	status, resp := createGroup(t, app, alice,
		fmt.Sprintf(`{"name":"closed","visibility":"closed","geofence":{"lat":%v,"lon":%v,"radiusM":40}}`, groupLat, groupLon))
	if status != http.StatusCreated {
		t.Fatalf("create closed: status = %d (body=%s)", status, resp)
	}
	closed := decodeGroup(t, resp)

	// 非メンバーの bob には closed グループが見えない。
	setGroupLocation(t, env, "did:plc:bob", groupLat, groupLon)
	status, res, body := fetchGraph(t, app, bob)
	if status != http.StatusOK {
		t.Fatalf("bob graph: status = %d (body=%s)", status, body)
	}
	if findNode(res.Nodes, closed.ID) != nil {
		t.Fatalf("closed の非メンバーにグループが見えている: %+v", res.Nodes)
	}

	// メンバーにすると見える（招待は別の作業なので直接入れる）。
	execSQL(t, env, "INSERT INTO space_members (space_id, did, joined_at) VALUES (?, ?, ?)",
		closed.ID, "did:plc:bob", env.Clock.Now().UnixMilli())
	_, res, _ = fetchGraph(t, app, bob)
	node := findNode(res.Nodes, closed.ID)
	if node == nil || !node.IsMember {
		t.Fatalf("closed のメンバーにグループが見えない: %+v", res.Nodes)
	}
}

func TestGroupGraphSharedMembers(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)
	for _, u := range []struct{ did, handle string }{
		{"did:plc:alice", "alice.test"}, {"did:plc:bob", "bob.test"},
		{"did:plc:carol", "carol.test"}, {"did:plc:dave", "dave.test"},
	} {
		env.CreateUser(t, u.did, u.handle)
	}
	alice := env.Login(t, "did:plc:alice")
	bob := env.Login(t, "did:plc:bob")
	carol := env.Login(t, "did:plc:carol")
	dave := env.Login(t, "did:plc:dave")

	setGroupLocation(t, env, "did:plc:alice", groupLat, groupLon)
	a := createOpenGroup(t, app, alice, "A", 800)

	setGroupLocation(t, env, "did:plc:bob", groupLat, groupLon)
	b := createOpenGroup(t, app, bob, "B", 40)

	for _, u := range []struct {
		did, token string
	}{{"did:plc:carol", carol}, {"did:plc:dave", dave}} {
		setGroupLocation(t, env, u.did, groupLat, groupLon)
		joinGroup(t, app, u.token, a.ID)
		joinGroup(t, app, u.token, b.ID)
	}

	status, res, body := fetchGraph(t, app, alice)
	if status != http.StatusOK {
		t.Fatalf("graph: status = %d (body=%s)", status, body)
	}
	if findNode(res.Nodes, a.ID) == nil || findNode(res.Nodes, b.ID) == nil {
		t.Fatalf("A と B の両方が出るはず: %+v", res.Nodes)
	}
	if _, ok := directedEdge(res.Edges, a.ID, b.ID, "contains"); !ok {
		t.Fatalf("A contains B の辺がない: %+v", res.Edges)
	}
	e, ok := anyEdge(res.Edges, a.ID, b.ID, "shared_members")
	if !ok || e.Weight != 2 {
		t.Fatalf("shared_members = %+v (存在=%v), want weight 2（carol と dave）", e, ok)
	}
}

func TestGroupGraphEntryNotificationHook(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)
	env.CreateUser(t, "did:plc:alice", "alice.test")
	alice := env.Login(t, "did:plc:alice")
	env.CreateUser(t, "did:plc:bob", "bob.test")
	bob := env.Login(t, "did:plc:bob")
	setGroupLocation(t, env, "did:plc:alice", groupLat, groupLon)
	createOpenGroup(t, app, alice, "情報科学演習 A", 40)

	now := env.Clock.Now()
	body := fmt.Sprintf(`{"lat":%v,"lon":%v,"accuracyM":5,"recordedAt":%q,"source":"gps","isMock":false}`,
		groupLat, groupLon, now.UTC().Format(time.RFC3339))
	status, respBody := request(t, app, http.MethodPut, "/api/v1/me/location", body, bob)
	if status != http.StatusNoContent {
		t.Fatalf("put location: status = %d, want 204 (body=%s)", status, respBody)
	}
	if !waitForNotification(t, env, "did:plc:bob", service.NotificationGroupNearby) {
		t.Fatal("PUT /me/location のフックから入場通知が積まれていない")
	}
}
