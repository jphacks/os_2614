//go:build integration

package group_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gofiber/fiber/v3"

	"github.com/applemango/chain/internal/testutil"
)

type voteResponse struct {
	ID            string `json:"id"`
	Kind          string `json:"kind"`
	Status        string `json:"status"`
	CreatedBy     string `json:"createdBy"`
	YesCount      int    `json:"yesCount"`
	NoCount       int    `json:"noCount"`
	RequiredCount int    `json:"requiredCount"`
	MyBallot      *bool  `json:"myBallot"`
}

func decodeVote(t *testing.T, body []byte) voteResponse {
	t.Helper()
	var v voteResponse
	if err := json.Unmarshal(body, &v); err != nil {
		t.Fatalf("decode vote: %v (body=%s)", err, body)
	}
	return v
}

func joinGroup(t *testing.T, app *fiber.App, token, id string) {
	t.Helper()
	status, body := testutil.Request(t, app, http.MethodPost, "/api/v1/groups/"+id+"/join", "", token)
	if status != http.StatusNoContent {
		t.Fatalf("join: status = %d, want 204 (body=%s)", status, body)
	}
}

func TestGroupVoteAPI(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)

	for _, did := range []string{"did:plc:alice", "did:plc:bob", "did:plc:carol", "did:plc:tom"} {
		env.CreateUser(t, did, did[8:]+".test")
	}
	alice := env.Login(t, "did:plc:alice")
	bob := env.Login(t, "did:plc:bob")
	carol := env.Login(t, "did:plc:carol")
	tom := env.Login(t, "did:plc:tom")

	setGroupLocation(t, env, "did:plc:alice", groupLat, groupLon)
	_, body := createGroup(t, app, alice,
		`{"name":"教室","description":"","visibility":"open","persistent":false,"geofence":{"lat":35.0,"lon":135.0,"radiusM":40}}`)
	group := decodeGroup(t, body)

	setGroupLocation(t, env, "did:plc:bob", groupLat, groupLon)
	joinGroup(t, app, bob, group.ID)
	setGroupLocation(t, env, "did:plc:carol", groupLat, groupLon)
	joinGroup(t, app, carol, group.ID)

	// 不正な kind は 400。
	status, body := testutil.Request(t, app, http.MethodPost, "/api/v1/groups/"+group.ID+"/votes", `{"kind":"make_tea"}`, alice)
	if status != http.StatusBadRequest {
		t.Fatalf("invalid kind: status = %d, want 400 (body=%s)", status, body)
	}
	testutil.WantErrorCode(t, body, "invalid_request")

	// 非メンバーは発議できない。
	status, body = testutil.Request(t, app, http.MethodPost, "/api/v1/groups/"+group.ID+"/votes", `{"kind":"make_closed"}`, tom)
	if status != http.StatusForbidden {
		t.Fatalf("non-member: status = %d, want 403 (body=%s)", status, body)
	}
	testutil.WantErrorCode(t, body, "forbidden")

	// 発議すると 201 で詳細が返る。
	status, body = testutil.Request(t, app, http.MethodPost, "/api/v1/groups/"+group.ID+"/votes", `{"kind":"make_closed"}`, alice)
	if status != http.StatusCreated {
		t.Fatalf("propose: status = %d, want 201 (body=%s)", status, body)
	}
	vote := decodeVote(t, body)
	if vote.Status != "open" || vote.YesCount != 1 || vote.RequiredCount != 2 || vote.CreatedBy != "did:plc:alice" {
		t.Fatalf("vote = %+v", vote)
	}
	if vote.MyBallot == nil || !*vote.MyBallot {
		t.Fatalf("myBallot = %v, want true", vote.MyBallot)
	}

	// 同じ種類の二重発議は 409。
	status, body = testutil.Request(t, app, http.MethodPost, "/api/v1/groups/"+group.ID+"/votes", `{"kind":"make_closed"}`, bob)
	if status != http.StatusConflict {
		t.Fatalf("duplicate: status = %d, want 409 (body=%s)", status, body)
	}
	testutil.WantErrorCode(t, body, "conflict")

	// 一覧はメンバーに見え、自分の票は null。
	status, body = testutil.Request(t, app, http.MethodGet, "/api/v1/groups/"+group.ID+"/votes", "", carol)
	if status != http.StatusOK {
		t.Fatalf("list: status = %d, want 200 (body=%s)", status, body)
	}
	var list struct {
		Items []voteResponse `json:"items"`
	}
	testutil.MustJSONBody(t, body, &list)
	if len(list.Items) != 1 || list.Items[0].ID != vote.ID {
		t.Fatalf("list = %+v", list.Items)
	}
	if list.Items[0].MyBallot != nil {
		t.Fatalf("carol の票 = %v, want nil", list.Items[0].MyBallot)
	}

	// 2 票目の賛成で可決し、visibility が closed になる。
	status, body = testutil.Request(t, app, http.MethodPost, "/api/v1/groups/"+group.ID+"/votes/"+vote.ID+"/ballot", `{"yes":true}`, bob)
	if status != http.StatusOK {
		t.Fatalf("ballot: status = %d, want 200 (body=%s)", status, body)
	}
	passed := decodeVote(t, body)
	if passed.Status != "passed" || passed.YesCount != 2 {
		t.Fatalf("passed = %+v", passed)
	}
	status, body = testutil.Request(t, app, http.MethodGet, "/api/v1/groups/"+group.ID, "", alice)
	if status != http.StatusOK {
		t.Fatalf("detail: status = %d (body=%s)", status, body)
	}
	if got := decodeGroup(t, body); got.Visibility != "closed" {
		t.Fatalf("visibility = %q, want closed", got.Visibility)
	}

	// 締まった投票への票は 410。
	status, body = testutil.Request(t, app, http.MethodPost, "/api/v1/groups/"+group.ID+"/votes/"+vote.ID+"/ballot", `{"yes":true}`, carol)
	if status != http.StatusGone {
		t.Fatalf("ballot after close: status = %d, want 410 (body=%s)", status, body)
	}
	testutil.WantErrorCode(t, body, "gone")
}

func TestGroupVoteAPIAlreadyClosed(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)

	env.CreateUser(t, "did:plc:alice", "alice.test")
	alice := env.Login(t, "did:plc:alice")

	setGroupLocation(t, env, "did:plc:alice", groupLat, groupLon)
	_, body := createGroup(t, app, alice,
		`{"name":"招待制","description":"","visibility":"closed","persistent":false,"geofence":{"lat":35.0,"lon":135.0,"radiusM":40}}`)
	group := decodeGroup(t, body)

	status, body := testutil.Request(t, app, http.MethodPost, "/api/v1/groups/"+group.ID+"/votes", `{"kind":"make_closed"}`, alice)
	if status != http.StatusConflict {
		t.Fatalf("already closed: status = %d, want 409 (body=%s)", status, body)
	}
	testutil.WantErrorCode(t, body, "conflict")
}
