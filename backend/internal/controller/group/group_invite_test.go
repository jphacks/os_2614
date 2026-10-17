//go:build integration

package group_test

import (
	"context"
	"net/http"
	"testing"

	friendmodel "github.com/applemango/chain/internal/model/friend"
	core "github.com/applemango/chain/internal/service/core"
	"github.com/applemango/chain/internal/testutil"
)

type groupInviteResponse struct {
	ID            string `json:"id"`
	TargetDID     string `json:"targetDid"`
	CreatedBy     string `json:"createdBy"`
	Status        string `json:"status"`
	ApprovedCount int    `json:"approvedCount"`
	RequiredCount int    `json:"requiredCount"`
}

type groupInviteListResponse struct {
	Items []groupInviteResponse `json:"items"`
}

func createFriendship(t *testing.T, env *testutil.Env, a, b string) {
	t.Helper()
	x, y := core.OrderDIDs(a, b)
	if err := friendmodel.CreateFriendship(context.Background(), env.DB, x, y, "invite", env.Clock.Now()); err != nil {
		t.Fatalf("CreateFriendship: %v", err)
	}
}

func TestGroupInviteFlowAPI(t *testing.T) {
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
		`{"name":"教室","geofence":{"lat":35.0,"lon":135.0,"radiusM":40},"inviteQuorum":2}`)
	if status != http.StatusCreated {
		t.Fatalf("create: status = %d (body=%s)", status, body)
	}
	id := decodeGroup(t, body).ID

	setGroupLocation(t, env, "did:plc:bob", groupLat, groupLon)
	status, body = testutil.Request(t, app, http.MethodPost, "/api/v1/groups/"+id+"/join", "", bob)
	if status != http.StatusNoContent {
		t.Fatalf("join: status = %d (body=%s)", status, body)
	}
	createFriendship(t, env, "did:plc:alice", "did:plc:carol")

	status, body = testutil.Request(t, app, http.MethodPatch, "/api/v1/groups/"+id, `{"visibility":"closed"}`, alice)
	if status != http.StatusOK {
		t.Fatalf("patch closed: status = %d (body=%s)", status, body)
	}

	status, body = testutil.Request(t, app, http.MethodPost, "/api/v1/groups/"+id+"/invites", `{"did":"did:plc:carol"}`, alice)
	if status != http.StatusCreated {
		t.Fatalf("create invite: status = %d (body=%s)", status, body)
	}
	var invite groupInviteResponse
	testutil.MustJSONBody(t, body, &invite)
	if invite.Status != "collecting" || invite.ApprovedCount != 1 || invite.RequiredCount != 2 {
		t.Fatalf("invite = %+v, want collecting 1/2", invite)
	}

	status, body = testutil.Request(t, app, http.MethodGet, "/api/v1/groups/"+id+"/invites", "", alice)
	if status != http.StatusOK {
		t.Fatalf("list invites: status = %d (body=%s)", status, body)
	}
	var list groupInviteListResponse
	testutil.MustJSONBody(t, body, &list)
	if len(list.Items) != 1 || list.Items[0].ID != invite.ID {
		t.Fatalf("invite list = %+v", list.Items)
	}

	status, body = testutil.Request(t, app, http.MethodPost, "/api/v1/groups/"+id+"/invites/"+invite.ID+"/approve", "", bob)
	if status != http.StatusOK {
		t.Fatalf("approve: status = %d (body=%s)", status, body)
	}
	testutil.MustJSONBody(t, body, &invite)
	if invite.Status != "offered" {
		t.Fatalf("status after approve = %q, want offered", invite.Status)
	}

	status, body = testutil.Request(t, app, http.MethodGet, "/api/v1/group-invites", "", carol)
	if status != http.StatusOK {
		t.Fatalf("inbox: status = %d (body=%s)", status, body)
	}
	var inbox struct {
		Items []struct {
			ID        string `json:"id"`
			SpaceID   string `json:"spaceId"`
			GroupName string `json:"groupName"`
		} `json:"items"`
	}
	testutil.MustJSONBody(t, body, &inbox)
	if len(inbox.Items) != 1 || inbox.Items[0].ID != invite.ID || inbox.Items[0].SpaceID != id {
		t.Fatalf("inbox = %+v", inbox.Items)
	}

	setGroupLocation(t, env, "did:plc:carol", groupLat, groupLon)
	status, body = testutil.Request(t, app, http.MethodPost, "/api/v1/group-invites/"+invite.ID+"/accept", "", carol)
	if status != http.StatusNoContent {
		t.Fatalf("accept: status = %d (body=%s)", status, body)
	}

	status, body = testutil.Request(t, app, http.MethodGet, "/api/v1/groups/"+id+"/members", "", alice)
	if status != http.StatusOK {
		t.Fatalf("members: status = %d (body=%s)", status, body)
	}
	var members struct {
		Items []struct {
			DID string `json:"did"`
		} `json:"items"`
	}
	testutil.MustJSONBody(t, body, &members)
	if len(members.Items) != 3 {
		t.Fatalf("members = %+v, want 3", members.Items)
	}
}

func TestGroupInviteErrorsAPI(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)
	env.CreateUser(t, "did:plc:alice", "alice.test")
	alice := env.Login(t, "did:plc:alice")
	env.CreateUser(t, "did:plc:bob", "bob.test")
	bob := env.Login(t, "did:plc:bob")
	env.CreateUser(t, "did:plc:tom", "tom.test")
	tom := env.Login(t, "did:plc:tom")

	setGroupLocation(t, env, "did:plc:alice", groupLat, groupLon)
	status, body := createGroup(t, app, alice,
		`{"name":"教室","geofence":{"lat":35.0,"lon":135.0,"radiusM":40},"inviteQuorum":3}`)
	if status != http.StatusCreated {
		t.Fatalf("create: status = %d (body=%s)", status, body)
	}
	id := decodeGroup(t, body).ID

	setGroupLocation(t, env, "did:plc:bob", groupLat, groupLon)
	status, body = testutil.Request(t, app, http.MethodPost, "/api/v1/groups/"+id+"/join", "", bob)
	if status != http.StatusNoContent {
		t.Fatalf("join: status = %d (body=%s)", status, body)
	}

	// open のグループでは招待を作れない。
	status, body = testutil.Request(t, app, http.MethodPost, "/api/v1/groups/"+id+"/invites", `{"did":"did:plc:tom"}`, alice)
	if status != http.StatusConflict {
		t.Fatalf("open invite: status = %d, want 409 (body=%s)", status, body)
	}
	testutil.WantErrorCode(t, body, "conflict")

	// did なしは 400。
	status, body = testutil.Request(t, app, http.MethodPost, "/api/v1/groups/"+id+"/invites", `{}`, alice)
	if status != http.StatusBadRequest {
		t.Fatalf("missing did: status = %d, want 400 (body=%s)", status, body)
	}
	testutil.WantErrorCode(t, body, "invalid_request")

	closed := "closed"
	status, body = testutil.Request(t, app, http.MethodPatch, "/api/v1/groups/"+id, `{"visibility":"`+closed+`"}`, alice)
	if status != http.StatusOK {
		t.Fatalf("patch closed: status = %d (body=%s)", status, body)
	}

	// フレンドでも encounter でもない相手は 403。
	status, body = testutil.Request(t, app, http.MethodPost, "/api/v1/groups/"+id+"/invites", `{"did":"did:plc:tom"}`, alice)
	if status != http.StatusForbidden {
		t.Fatalf("non-relation invite: status = %d, want 403 (body=%s)", status, body)
	}
	testutil.WantErrorCode(t, body, "forbidden")

	// 自分は 400。
	status, body = testutil.Request(t, app, http.MethodPost, "/api/v1/groups/"+id+"/invites", `{"did":"did:plc:alice"}`, alice)
	if status != http.StatusBadRequest {
		t.Fatalf("self invite: status = %d, want 400 (body=%s)", status, body)
	}

	createFriendship(t, env, "did:plc:alice", "did:plc:tom")
	status, body = testutil.Request(t, app, http.MethodPost, "/api/v1/groups/"+id+"/invites", `{"did":"did:plc:tom"}`, alice)
	if status != http.StatusCreated {
		t.Fatalf("invite: status = %d (body=%s)", status, body)
	}
	var invite groupInviteResponse
	testutil.MustJSONBody(t, body, &invite)

	// 非メンバーの承認は 403。
	status, body = testutil.Request(t, app, http.MethodPost, "/api/v1/groups/"+id+"/invites/"+invite.ID+"/approve", "", tom)
	if status != http.StatusForbidden {
		t.Fatalf("non-member approve: status = %d, want 403 (body=%s)", status, body)
	}

	// 招待者以外の取り消しは 403。
	status, body = testutil.Request(t, app, http.MethodDelete, "/api/v1/groups/"+id+"/invites/"+invite.ID, "", bob)
	if status != http.StatusForbidden {
		t.Fatalf("non-inviter cancel: status = %d, want 403 (body=%s)", status, body)
	}

	// 招待者が取り消すと 204。同じ相手に再度招待できる。
	status, body = testutil.Request(t, app, http.MethodDelete, "/api/v1/groups/"+id+"/invites/"+invite.ID, "", alice)
	if status != http.StatusNoContent {
		t.Fatalf("cancel: status = %d, want 204 (body=%s)", status, body)
	}

	status, body = testutil.Request(t, app, http.MethodPost, "/api/v1/groups/"+id+"/invites", `{"did":"did:plc:tom"}`, alice)
	if status != http.StatusCreated {
		t.Fatalf("re-invite: status = %d (body=%s)", status, body)
	}
	testutil.MustJSONBody(t, body, &invite)
	status, body = testutil.Request(t, app, http.MethodPost, "/api/v1/groups/"+id+"/invites", `{"did":"did:plc:tom"}`, alice)
	if status != http.StatusConflict {
		t.Fatalf("double invite: status = %d, want 409 (body=%s)", status, body)
	}

	// 対象者が断ると 204、二度目は 410。
	// tom の招待は collecting のままなので、先に承認できないことを確かめてから辞退する。
	status, body = testutil.Request(t, app, http.MethodPost, "/api/v1/group-invites/"+invite.ID+"/decline", "", tom)
	if status != http.StatusConflict {
		t.Fatalf("decline collecting: status = %d, want 409 (body=%s)", status, body)
	}
}

func TestGroupVisibilityPatchAPI(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)
	env.CreateUser(t, "did:plc:alice", "alice.test")
	alice := env.Login(t, "did:plc:alice")
	env.CreateUser(t, "did:plc:bob", "bob.test")
	bob := env.Login(t, "did:plc:bob")
	setGroupLocation(t, env, "did:plc:alice", groupLat, groupLon)

	status, body := createGroup(t, app, alice,
		`{"name":"教室","geofence":{"lat":35.0,"lon":135.0,"radiusM":40}}`)
	if status != http.StatusCreated {
		t.Fatalf("create: status = %d (body=%s)", status, body)
	}
	id := decodeGroup(t, body).ID

	status, body = testutil.Request(t, app, http.MethodGet, "/api/v1/groups/"+id, "", bob)
	if status != http.StatusOK {
		t.Fatalf("non-member detail(open): status = %d (body=%s)", status, body)
	}

	status, body = testutil.Request(t, app, http.MethodPatch, "/api/v1/groups/"+id, `{"visibility":"closed"}`, bob)
	if status != http.StatusForbidden {
		t.Fatalf("non-owner patch: status = %d, want 403 (body=%s)", status, body)
	}

	status, body = testutil.Request(t, app, http.MethodPatch, "/api/v1/groups/"+id, `{"visibility":"closed"}`, alice)
	if status != http.StatusOK {
		t.Fatalf("owner patch: status = %d (body=%s)", status, body)
	}
	if d := decodeGroup(t, body); d.Visibility != "closed" {
		t.Fatalf("visibility = %q, want closed", d.Visibility)
	}
	status, body = testutil.Request(t, app, http.MethodGet, "/api/v1/groups/"+id, "", bob)
	if status != http.StatusNotFound {
		t.Fatalf("non-member detail(closed): status = %d, want 404 (body=%s)", status, body)
	}
	testutil.WantErrorCode(t, body, "not_found")

	// 不正な visibility は 400。
	status, body = testutil.Request(t, app, http.MethodPatch, "/api/v1/groups/"+id, `{"visibility":"hidden"}`, alice)
	if status != http.StatusBadRequest {
		t.Fatalf("bad visibility: status = %d, want 400 (body=%s)", status, body)
	}
	e := testutil.WantErrorCode(t, body, "invalid_request")
	if e.Error.Details.Fields["visibility"] == "" {
		t.Fatalf("details.fields.visibility is empty (body=%s)", body)
	}
}
