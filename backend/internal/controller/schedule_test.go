//go:build integration

package controller_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/applemango/chain/internal/testutil"
)

// 09 §8.2 の PUT / GET / DELETE /api/v1/me/schedule-today。

type scheduleEntryJSON struct {
	Source string `json:"source"`
	Title  string `json:"title"`
	Start  string `json:"start"`
	End    string `json:"end"`
}

type scheduleJSON struct {
	Date    string              `json:"date"`
	Entries []scheduleEntryJSON `json:"entries"`
}

func decodeSchedule(t *testing.T, body []byte) scheduleJSON {
	t.Helper()
	var got scheduleJSON
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode schedule: %v (body=%s)", err, body)
	}
	return got
}

func TestScheduleTodayAPI(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)
	env.CreateUser(t, "did:plc:sched", "sched.test")
	token := env.Login(t, "did:plc:sched")

	// 認証なしは 401。
	status, body := request(t, app, http.MethodGet, "/api/v1/me/schedule-today", "", "")
	if status != http.StatusUnauthorized {
		t.Fatalf("no token: status = %d, want 401 (body=%s)", status, body)
	}

	// calendar を保存。
	status, body = request(t, app, http.MethodPut, "/api/v1/me/schedule-today",
		`{"date":"2026-10-17","source":"calendar","entries":[{"title":"線形代数 II","start":"10:40","end":"12:10"}]}`, token)
	if status != http.StatusOK {
		t.Fatalf("PUT calendar: status = %d, want 200 (body=%s)", status, body)
	}
	got := decodeSchedule(t, body)
	if got.Date != "2026-10-17" || len(got.Entries) != 1 {
		t.Fatalf("PUT response = %+v, want date=2026-10-17 entries=1", got)
	}
	if e := got.Entries[0]; e.Source != "calendar" || e.Title != "線形代数 II" || e.Start != "10:40" || e.End != "12:10" {
		t.Fatalf("entry = %+v, want calendar/線形代数 II/10:40/12:10", e)
	}

	// manual を足すと両方返る。
	status, body = request(t, app, http.MethodPut, "/api/v1/me/schedule-today",
		`{"date":"2026-10-17","source":"manual","entries":[{"title":"ランチ","start":"12:30","end":"13:00"}]}`, token)
	if status != http.StatusOK {
		t.Fatalf("PUT manual: status = %d, want 200 (body=%s)", status, body)
	}
	if got = decodeSchedule(t, body); len(got.Entries) != 2 {
		t.Fatalf("entries = %+v, want 2", got.Entries)
	}

	// calendar だけを置き換えても manual は残る。
	status, body = request(t, app, http.MethodPut, "/api/v1/me/schedule-today",
		`{"date":"2026-10-17","source":"calendar","entries":[{"title":"線形代数学 II","start":"13:00","end":"14:30"}]}`, token)
	if status != http.StatusOK {
		t.Fatalf("PUT calendar again: status = %d (body=%s)", status, body)
	}
	if got = decodeSchedule(t, body); len(got.Entries) != 2 {
		t.Fatalf("entries after replace = %+v, want 2", got.Entries)
	}

	// GET は date を省くと今日。
	status, body = request(t, app, http.MethodGet, "/api/v1/me/schedule-today", "", token)
	if status != http.StatusOK {
		t.Fatalf("GET today: status = %d (body=%s)", status, body)
	}
	got = decodeSchedule(t, body)
	if got.Date != "2026-10-17" || len(got.Entries) != 2 {
		t.Fatalf("GET = %+v, want date=2026-10-17 entries=2", got)
	}

	// 空の entries で calendar だけ消える。
	status, body = request(t, app, http.MethodPut, "/api/v1/me/schedule-today",
		`{"date":"2026-10-17","source":"calendar","entries":[]}`, token)
	if status != http.StatusOK {
		t.Fatalf("PUT empty calendar: status = %d (body=%s)", status, body)
	}
	got = decodeSchedule(t, body)
	if len(got.Entries) != 1 || got.Entries[0].Source != "manual" {
		t.Fatalf("entries after clearing calendar = %+v, want only manual", got.Entries)
	}

	// DELETE で manual を消す。
	status, body = request(t, app, http.MethodDelete, "/api/v1/me/schedule-today?date=2026-10-17&source=manual", "", token)
	if status != http.StatusNoContent {
		t.Fatalf("DELETE manual: status = %d, want 204 (body=%s)", status, body)
	}
	status, body = request(t, app, http.MethodGet, "/api/v1/me/schedule-today?date=2026-10-17", "", token)
	if status != http.StatusOK {
		t.Fatalf("GET after delete: status = %d (body=%s)", status, body)
	}
	if got = decodeSchedule(t, body); len(got.Entries) != 0 {
		t.Fatalf("entries after delete = %+v, want 0", got.Entries)
	}

	// 明日も指定できる。
	status, body = request(t, app, http.MethodPut, "/api/v1/me/schedule-today",
		`{"date":"2026-10-18","source":"calendar","entries":[{"title":"明日の予定","start":"09:00","end":"10:00"}]}`, token)
	if status != http.StatusOK {
		t.Fatalf("PUT tomorrow: status = %d (body=%s)", status, body)
	}
	if got = decodeSchedule(t, body); got.Date != "2026-10-18" {
		t.Fatalf("tomorrow response date = %q, want 2026-10-18", got.Date)
	}
}

func TestScheduleTodayValidation(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)
	env.CreateUser(t, "did:plc:sched", "sched.test")
	token := env.Login(t, "did:plc:sched")

	cases := []struct {
		name  string
		body  string
		field string
	}{
		{"past date", `{"date":"2026-10-16","source":"calendar","entries":[]}`, "date"},
		{"far future date", `{"date":"2026-10-19","source":"calendar","entries":[]}`, "date"},
		{"bad source", `{"date":"2026-10-17","source":"work","entries":[]}`, "source"},
		{"title too long", `{"date":"2026-10-17","source":"calendar","entries":[{"title":"` + repeatRune("あ", 101) + `","start":"09:00","end":"10:00"}]}`, "entries[0].title"},
		{"start equals end", `{"date":"2026-10-17","source":"calendar","entries":[{"title":"x","start":"10:00","end":"10:00"}]}`, "entries[0].end"},
		{"start after end", `{"date":"2026-10-17","source":"calendar","entries":[{"title":"x","start":"11:00","end":"10:00"}]}`, "entries[0].end"},
		{"invalid clock", `{"date":"2026-10-17","source":"calendar","entries":[{"title":"x","start":"25:00","end":"26:00"}]}`, "entries[0].start"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, body := request(t, app, http.MethodPut, "/api/v1/me/schedule-today", tc.body, token)
			if status != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (body=%s)", status, body)
			}
			e := decodeError(t, body)
			if e.Error.Details.Fields[tc.field] == "" {
				t.Fatalf("fields[%q] is empty (body=%s)", tc.field, body)
			}
		})
	}

	// 21 件は上限を超える。
	entries := make([]string, 21)
	for i := range entries {
		entries[i] = `{"title":"x","start":"09:00","end":"10:00"}`
	}
	body := `{"date":"2026-10-17","source":"calendar","entries":[` + joinStrings(entries) + `]}`
	status, respBody := request(t, app, http.MethodPut, "/api/v1/me/schedule-today", body, token)
	if status != http.StatusBadRequest {
		t.Fatalf("21 entries: status = %d, want 400 (body=%s)", status, respBody)
	}
	if e := decodeError(t, respBody); e.Error.Details.Fields["entries"] == "" {
		t.Fatalf("fields[entries] is empty (body=%s)", respBody)
	}
}

func repeatRune(s string, n int) string {
	out := make([]byte, 0, len(s)*n)
	for i := 0; i < n; i++ {
		out = append(out, s...)
	}
	return string(out)
}

func joinStrings(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += ","
		}
		out += p
	}
	return out
}
