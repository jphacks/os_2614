//go:build integration

package controller_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/applemango/chain/internal/model"
	"github.com/applemango/chain/internal/service"
	"github.com/applemango/chain/internal/testutil"
)

const (
	groupLat = 35.0
	groupLon = 135.0
)

func setGroupLocation(t *testing.T, env *testutil.Env, did string, lat, lon float64) {
	t.Helper()
	loc := model.Location{DID: did, Lat: lat, Lon: lon, AccuracyM: 5, RecordedAt: env.Clock.Now(), Source: "gps"}
	if err := model.SaveLocation(context.Background(), env.Redis, loc, service.LocationTTL); err != nil {
		t.Fatalf("SaveLocation(%s): %v", did, err)
	}
}

type groupDetail struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Visibility  string   `json:"visibility"`
	MemberCount int      `json:"memberCount"`
	IsMember    bool     `json:"isMember"`
	State       string   `json:"state"`
	Role        string   `json:"role"`
	Persistent  *bool    `json:"persistent"`
	RadiusM     *int     `json:"radiusM"`
	CenterLat   *float64 `json:"centerLat"`
	CenterLon   *float64 `json:"centerLon"`
}

func createGroup(t *testing.T, app *fiber.App, token, body string) (int, []byte) {
	t.Helper()
	return request(t, app, http.MethodPost, "/api/v1/groups", body, token)
}

func decodeGroup(t *testing.T, body []byte) groupDetail {
	t.Helper()
	var d groupDetail
	if err := json.Unmarshal(body, &d); err != nil {
		t.Fatalf("decode group: %v (body=%s)", err, body)
	}
	return d
}

func TestGroupCreateNearbyJoin(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)

	env.CreateUser(t, "did:plc:alice", "alice.test")
	alice := env.Login(t, "did:plc:alice")
	env.CreateUser(t, "did:plc:bob", "bob.test")
	bob := env.Login(t, "did:plc:bob")

	setGroupLocation(t, env, "did:plc:alice", groupLat, groupLon)

	status, body := createGroup(t, app, alice,
		`{"name":"教室","description":"","visibility":"open","persistent":false,"geofence":{"lat":35.0,"lon":135.0,"radiusM":40}}`)
	if status != http.StatusCreated {
		t.Fatalf("create: status = %d, want 201 (body=%s)", status, body)
	}
	created := decodeGroup(t, body)
	if created.ID == "" || !created.IsMember || created.MemberCount != 1 || created.State != "active" {
		t.Fatalf("created = %+v", created)
	}
	if created.CenterLat == nil || created.CenterLon == nil {
		t.Fatalf("作成者に中心座標が返っていない: %+v", created)
	}
	if created.Role != "owner" {
		t.Fatalf("role = %q, want owner", created.Role)
	}

	// 非メンバーには限られた項目だけ。中心座標は返らない。
	setGroupLocation(t, env, "did:plc:bob", groupLat, groupLon)
	status, body = request(t, app, http.MethodGet, "/api/v1/groups/"+created.ID, "", bob)
	if status != http.StatusOK {
		t.Fatalf("bob detail: status = %d, want 200 (body=%s)", status, body)
	}
	limited := decodeGroup(t, body)
	if limited.IsMember {
		t.Fatal("bob should not be a member yet")
	}
	if limited.CenterLat != nil || limited.CenterLon != nil {
		t.Fatalf("非メンバーに中心座標が返っている: %+v", limited)
	}
	if limited.Name != "教室" || limited.Visibility != "open" || limited.MemberCount != 1 {
		t.Fatalf("limited = %+v", limited)
	}
	// 非メンバーに見せるのは name / description / memberCount / visibility だけ（＋ id / isMember）。
	var raw map[string]json.RawMessage
	mustJSONBody(t, body, &raw)
	allowed := map[string]bool{"id": true, "name": true, "description": true, "memberCount": true, "visibility": true, "isMember": true}
	for k := range raw {
		if !allowed[k] {
			t.Fatalf("非メンバーに余分な項目 %q が返っている: %s", k, body)
		}
	}

	// nearby に出る。
	status, body = request(t, app, http.MethodGet, "/api/v1/groups/nearby", "", bob)
	if status != http.StatusOK {
		t.Fatalf("nearby: status = %d, want 200 (body=%s)", status, body)
	}
	var nearby struct {
		Inside []struct {
			ID         string `json:"id"`
			IsMember   bool   `json:"isMember"`
			RadiusM    int    `json:"radiusM"`
			Persistent bool   `json:"persistent"`
		} `json:"inside"`
	}
	mustJSONBody(t, body, &nearby)
	if len(nearby.Inside) != 1 || nearby.Inside[0].ID != created.ID {
		t.Fatalf("nearby inside = %+v, want the group", nearby.Inside)
	}

	// join → 204、メンバーには中心座標が返る。
	status, body = request(t, app, http.MethodPost, "/api/v1/groups/"+created.ID+"/join", "", bob)
	if status != http.StatusNoContent {
		t.Fatalf("join: status = %d, want 204 (body=%s)", status, body)
	}
	status, body = request(t, app, http.MethodGet, "/api/v1/groups/"+created.ID, "", bob)
	if status != http.StatusOK {
		t.Fatalf("detail after join: status = %d (body=%s)", status, body)
	}
	joined := decodeGroup(t, body)
	if !joined.IsMember || joined.CenterLat == nil {
		t.Fatalf("after join = %+v, want member with center", joined)
	}

	// メンバー一覧。
	status, body = request(t, app, http.MethodGet, "/api/v1/groups/"+created.ID+"/members", "", alice)
	if status != http.StatusOK {
		t.Fatalf("members: status = %d, want 200 (body=%s)", status, body)
	}
	var members struct {
		Items []struct {
			DID  string `json:"did"`
			Role string `json:"role"`
		} `json:"items"`
	}
	mustJSONBody(t, body, &members)
	if len(members.Items) != 2 {
		t.Fatalf("members = %+v, want 2", members.Items)
	}

	// owner 以外の PATCH は 403。
	status, body = request(t, app, http.MethodPatch, "/api/v1/groups/"+created.ID, `{"name":"x"}`, bob)
	if status != http.StatusForbidden {
		t.Fatalf("non-owner patch: status = %d, want 403 (body=%s)", status, body)
	}
	wantErrorCode(t, body, "forbidden")

	// owner の PATCH は 200。
	status, body = request(t, app, http.MethodPatch, "/api/v1/groups/"+created.ID, `{"name":"新教室"}`, alice)
	if status != http.StatusOK {
		t.Fatalf("owner patch: status = %d, want 200 (body=%s)", status, body)
	}
	if d := decodeGroup(t, body); d.Name != "新教室" {
		t.Fatalf("patched name = %q, want 新教室", d.Name)
	}

	// geofence の変更は 400。
	status, body = request(t, app, http.MethodPatch, "/api/v1/groups/"+created.ID,
		`{"geofence":{"lat":35.0,"lon":135.0,"radiusM":50}}`, alice)
	if status != http.StatusBadRequest {
		t.Fatalf("patch geofence: status = %d, want 400 (body=%s)", status, body)
	}
	e := wantErrorCode(t, body, "invalid_request")
	if e.Error.Details.Fields["geofence"] == "" {
		t.Fatalf("details.fields.geofence is empty (body=%s)", body)
	}

	// bob が退出すると 1 人に戻る。
	status, body = request(t, app, http.MethodPost, "/api/v1/groups/"+created.ID+"/leave", "", bob)
	if status != http.StatusNoContent {
		t.Fatalf("leave: status = %d, want 204 (body=%s)", status, body)
	}
	status, body = request(t, app, http.MethodGet, "/api/v1/groups/"+created.ID+"/members", "", alice)
	if status != http.StatusOK {
		t.Fatalf("members after leave: status = %d (body=%s)", status, body)
	}
	mustJSONBody(t, body, &members)
	if len(members.Items) != 1 {
		t.Fatalf("members after leave = %+v, want 1", members.Items)
	}
}

func TestGroupCreateValidation(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)
	env.CreateUser(t, "did:plc:alice", "alice.test")
	alice := env.Login(t, "did:plc:alice")
	setGroupLocation(t, env, "did:plc:alice", groupLat, groupLon)

	longName := strings.Repeat("あ", 41)
	cases := []struct {
		name  string
		body  string
		field string
	}{
		{"geofence なし", `{"name":"x"}`, "geofence"},
		{"半径が小さすぎる", `{"name":"x","geofence":{"lat":35.0,"lon":135.0,"radiusM":10}}`, "geofence.radiusM"},
		{"半径が大きすぎる", `{"name":"x","geofence":{"lat":35.0,"lon":135.0,"radiusM":2001}}`, "geofence.radiusM"},
		{"名前が長すぎる", `{"name":"` + longName + `","geofence":{"lat":35.0,"lon":135.0,"radiusM":40}}`, "name"},
		{"不正な visibility", `{"name":"x","visibility":"hidden","geofence":{"lat":35.0,"lon":135.0,"radiusM":40}}`, "visibility"},
		{"schedule の start >= end", `{"name":"x","geofence":{"lat":35.0,"lon":135.0,"radiusM":40},"schedule":{"tz":"UTC","slots":[{"weekday":1,"start":"12:00","end":"10:00"}]}}`, "schedule.slots.0.start"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, body := createGroup(t, app, alice, tc.body)
			if status != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (body=%s)", status, body)
			}
			e := wantErrorCode(t, body, "invalid_request")
			if e.Error.Details.Fields[tc.field] == "" {
				t.Fatalf("details.fields[%q] is empty (body=%s)", tc.field, body)
			}
		})
	}
}

func TestGroupModerationBlocked(t *testing.T) {
	env := testutil.NewEnv(t)
	env.Moderator = &testutil.BlockingModerator{Words: []string{"禁止"}}
	app := testutil.NewApp(env)
	env.CreateUser(t, "did:plc:alice", "alice.test")
	alice := env.Login(t, "did:plc:alice")
	setGroupLocation(t, env, "did:plc:alice", groupLat, groupLon)

	status, body := createGroup(t, app, alice,
		`{"name":"禁止ワード","geofence":{"lat":35.0,"lon":135.0,"radiusM":40}}`)
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422 (body=%s)", status, body)
	}
	wantErrorCode(t, body, "moderation_blocked")
}

func TestGroupClosedHiddenFromNonMember(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)
	env.CreateUser(t, "did:plc:alice", "alice.test")
	alice := env.Login(t, "did:plc:alice")
	env.CreateUser(t, "did:plc:bob", "bob.test")
	bob := env.Login(t, "did:plc:bob")
	setGroupLocation(t, env, "did:plc:alice", groupLat, groupLon)

	status, body := createGroup(t, app, alice,
		`{"name":"closed","visibility":"closed","geofence":{"lat":35.0,"lon":135.0,"radiusM":40}}`)
	if status != http.StatusCreated {
		t.Fatalf("create: status = %d (body=%s)", status, body)
	}
	id := decodeGroup(t, body).ID

	status, body = request(t, app, http.MethodGet, "/api/v1/groups/"+id, "", alice)
	if status != http.StatusOK {
		t.Fatalf("member detail: status = %d (body=%s)", status, body)
	}
	status, body = request(t, app, http.MethodGet, "/api/v1/groups/"+id, "", bob)
	if status != http.StatusNotFound {
		t.Fatalf("non-member detail: status = %d, want 404 (body=%s)", status, body)
	}
	wantErrorCode(t, body, "not_found")
}

func TestGroupJoinErrors(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)
	env.CreateUser(t, "did:plc:alice", "alice.test")
	alice := env.Login(t, "did:plc:alice")
	env.CreateUser(t, "did:plc:bob", "bob.test")
	bob := env.Login(t, "did:plc:bob")
	env.CreateUser(t, "did:plc:carol", "carol.test")
	carol := env.Login(t, "did:plc:carol")
	setGroupLocation(t, env, "did:plc:alice", groupLat, groupLon)

	status, body := createGroup(t, app, alice,
		`{"name":"join","geofence":{"lat":35.0,"lon":135.0,"radiusM":40}}`)
	if status != http.StatusCreated {
		t.Fatalf("create: status = %d (body=%s)", status, body)
	}
	id := decodeGroup(t, body).ID

	// エリア外。
	setGroupLocation(t, env, "did:plc:bob", groupLat+0.01, groupLon)
	status, body = request(t, app, http.MethodPost, "/api/v1/groups/"+id+"/join", "", bob)
	if status != http.StatusForbidden {
		t.Fatalf("outside join: status = %d, want 403 (body=%s)", status, body)
	}
	wantErrorCode(t, body, "not_in_geofence")

	// エリア内 → 204、二度目は 409。
	setGroupLocation(t, env, "did:plc:bob", groupLat, groupLon)
	status, body = request(t, app, http.MethodPost, "/api/v1/groups/"+id+"/join", "", bob)
	if status != http.StatusNoContent {
		t.Fatalf("inside join: status = %d, want 204 (body=%s)", status, body)
	}
	status, body = request(t, app, http.MethodPost, "/api/v1/groups/"+id+"/join", "", bob)
	if status != http.StatusConflict {
		t.Fatalf("double join: status = %d, want 409 (body=%s)", status, body)
	}
	wantErrorCode(t, body, "conflict")

	// 位置が古い。
	setGroupLocation(t, env, "did:plc:carol", groupLat, groupLon)
	env.Clock.Advance(3 * time.Minute)
	status, body = request(t, app, http.MethodPost, "/api/v1/groups/"+id+"/join", "", carol)
	if status != http.StatusForbidden {
		t.Fatalf("stale join: status = %d, want 403 (body=%s)", status, body)
	}
	wantErrorCode(t, body, "location_stale")
}

func TestGroupNearbyRequiresLocation(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)
	env.CreateUser(t, "did:plc:alice", "alice.test")
	alice := env.Login(t, "did:plc:alice")

	status, body := request(t, app, http.MethodGet, "/api/v1/groups/nearby", "", alice)
	if status != http.StatusForbidden {
		t.Fatalf("nearby without location: status = %d, want 403 (body=%s)", status, body)
	}
	wantErrorCode(t, body, "location_stale")
}
