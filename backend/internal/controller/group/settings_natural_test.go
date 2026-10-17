//go:build integration

package group_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/applemango/chain/internal/testutil"
)

func TestMeSettingsNaturalGroups(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)
	env.CreateUser(t, "did:plc:alice", "alice.test")
	token := env.Login(t, "did:plc:alice")

	settings := func() (bool, []byte) {
		status, body := testutil.Request(t, app, http.MethodGet, "/api/v1/me", "", token)
		if status != http.StatusOK {
			t.Fatalf("GET /me: status = %d (body=%s)", status, body)
		}
		var me struct {
			Settings struct {
				NaturalGroups bool `json:"naturalGroups"`
			} `json:"settings"`
		}
		if err := json.Unmarshal(body, &me); err != nil {
			t.Fatalf("decode me: %v (body=%s)", err, body)
		}
		return me.Settings.NaturalGroups, body
	}

	if on, _ := settings(); !on {
		t.Fatal("既定の naturalGroups = false, want true")
	}

	status, body := testutil.Request(t, app, http.MethodPatch, "/api/v1/me/settings", `{"naturalGroups":false}`, token)
	if status != http.StatusOK {
		t.Fatalf("PATCH naturalGroups:false status = %d (body=%s)", status, body)
	}
	if on, body := settings(); on {
		t.Fatalf("naturalGroups = true, want false (body=%s)", body)
	}

	status, body = testutil.Request(t, app, http.MethodPatch, "/api/v1/me/settings", `{"naturalGroups":true}`, token)
	if status != http.StatusOK {
		t.Fatalf("PATCH naturalGroups:true status = %d (body=%s)", status, body)
	}
	if on, body := settings(); !on {
		t.Fatalf("naturalGroups = false, want true (body=%s)", body)
	}
}

func TestGroupCreateWithDates(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)
	env.CreateUser(t, "did:plc:alice", "alice.test")
	token := env.Login(t, "did:plc:alice")
	setGroupLocation(t, env, "did:plc:alice", groupLat, groupLon)

	body := `{"name":"dates","geofence":{"lat":35.0,"lon":135.0,"radiusM":40},` +
		`"schedule":{"tz":"Asia/Tokyo","dates":[{"date":"2026-10-17","start":"09:00","end":"10:00"}]}}`
	status, resp := createGroup(t, app, token, body)
	if status != http.StatusCreated {
		t.Fatalf("create with dates: status = %d (body=%s)", status, resp)
	}
	var detail struct {
		Schedule struct {
			Dates []struct {
				Date string `json:"date"`
			} `json:"dates"`
		} `json:"schedule"`
	}
	if err := json.Unmarshal(resp, &detail); err != nil {
		t.Fatalf("decode group: %v (body=%s)", err, resp)
	}
	if len(detail.Schedule.Dates) != 1 || detail.Schedule.Dates[0].Date != "2026-10-17" {
		t.Fatalf("schedule.dates = %+v, want 2026-10-17", detail.Schedule.Dates)
	}

	invalid := `{"name":"bad","geofence":{"lat":35.0,"lon":135.0,"radiusM":40},` +
		`"schedule":{"tz":"Asia/Tokyo","dates":[{"date":"2026-13-01","start":"09:00","end":"10:00"}]}}`
	status, resp = createGroup(t, app, token, invalid)
	if status != http.StatusBadRequest {
		t.Fatalf("create with bad date: status = %d (body=%s)", status, resp)
	}
	if f := testutil.WantErrorCode(t, resp, "invalid_request").Error.Details.Fields["schedule.dates.0.date"]; f == "" {
		t.Fatalf("details.fields[schedule.dates.0.date] is empty (body=%s)", resp)
	}
}
