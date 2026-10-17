//go:build integration

package controller_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/applemango/chain/internal/testutil"
)

// request は app.Test を 1 回だけ実行し、ステータスと本文全体を返す。
// body が空でなければ JSON として送り、token が空でなければ Bearer を付ける。
func request(t *testing.T, app *fiber.App, method, path, body, token string) (int, []byte) {
	t.Helper()

	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, r)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test(%s %s): %v", method, path, err)
	}
	defer resp.Body.Close()

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body of %s %s: %v", method, path, err)
	}
	return resp.StatusCode, b
}

type apiError struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Details struct {
			Fields map[string]string `json:"fields"`
			Reason string            `json:"reason"`
		} `json:"details"`
	} `json:"error"`
}

func decodeError(t *testing.T, body []byte) apiError {
	t.Helper()
	var e apiError
	if err := json.Unmarshal(body, &e); err != nil {
		t.Fatalf("decode error JSON: %v (body=%s)", err, body)
	}
	return e
}

func wantErrorCode(t *testing.T, body []byte, code string) apiError {
	t.Helper()
	e := decodeError(t, body)
	if e.Error.Code != code {
		t.Fatalf("error.code = %q, want %q (body=%s)", e.Error.Code, code, body)
	}
	return e
}

func execSQL(t *testing.T, env *testutil.Env, query string, args ...any) {
	t.Helper()
	if _, err := env.DB.ExecContext(context.Background(), query, args...); err != nil {
		t.Fatalf("exec %q: %v", query, err)
	}
}

// pair は user_a < user_b の CHECK を満たすように 2 つの DID を並べる。
func pair(a, b string) (string, string) {
	if a < b {
		return a, b
	}
	return b, a
}

func TestHealth(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)

	status, body := request(t, app, http.MethodGet, "/health", "", "")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", status, body)
	}
	var got struct {
		SQLite bool `json:"sqlite"`
		Redis  bool `json:"redis"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode health: %v (body=%s)", err, body)
	}
	if !got.SQLite || !got.Redis {
		t.Fatalf("health = %+v, want sqlite and redis true", got)
	}
}

func TestMeAuth(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)
	env.CreateUser(t, "did:plc:alice", "alice.test")

	status, body := request(t, app, http.MethodGet, "/api/v1/me", "", "")
	if status != http.StatusUnauthorized {
		t.Fatalf("no token: status = %d, want 401 (body=%s)", status, body)
	}
	wantErrorCode(t, body, "unauthenticated")

	status, body = request(t, app, http.MethodGet, "/api/v1/me", "", "broken")
	if status != http.StatusUnauthorized {
		t.Fatalf("broken token: status = %d, want 401 (body=%s)", status, body)
	}
	wantErrorCode(t, body, "unauthenticated")

	token := env.Login(t, "did:plc:alice")
	status, body = request(t, app, http.MethodGet, "/api/v1/me", "", token)
	if status != http.StatusOK {
		t.Fatalf("login token: status = %d, want 200 (body=%s)", status, body)
	}
	var me struct {
		DID    string `json:"did"`
		Handle string `json:"handle"`
	}
	if err := json.Unmarshal(body, &me); err != nil {
		t.Fatalf("decode me: %v (body=%s)", err, body)
	}
	if me.DID != "did:plc:alice" {
		t.Fatalf("did = %q, want %q", me.DID, "did:plc:alice")
	}
	if me.Handle != "alice.test" {
		t.Fatalf("handle = %q, want %q", me.Handle, "alice.test")
	}

	// access JWT の TTL は 15 分（03 §3）。
	expiring := env.Login(t, "did:plc:alice")
	env.Clock.Advance(16 * time.Minute)
	status, body = request(t, app, http.MethodGet, "/api/v1/me", "", expiring)
	if status != http.StatusUnauthorized {
		t.Fatalf("expired token: status = %d, want 401 (body=%s)", status, body)
	}
	wantErrorCode(t, body, "unauthenticated")
}

func TestLogout(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)
	env.CreateUser(t, "did:plc:alice", "alice.test")
	token := env.Login(t, "did:plc:alice")

	status, body := request(t, app, http.MethodPost, "/api/v1/auth/logout", "", token)
	if status != http.StatusNoContent {
		t.Fatalf("logout: status = %d, want 204 (body=%s)", status, body)
	}

	status, body = request(t, app, http.MethodGet, "/api/v1/me", "", token)
	if status != http.StatusUnauthorized {
		t.Fatalf("me after logout: status = %d, want 401 (body=%s)", status, body)
	}
	wantErrorCode(t, body, "unauthenticated")
}

func TestAuthExchange(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)

	status, body := request(t, app, http.MethodPost, "/api/v1/auth/exchange", `{}`, "")
	if status != http.StatusBadRequest {
		t.Fatalf("empty exchange: status = %d, want 400 (body=%s)", status, body)
	}
	e := wantErrorCode(t, body, "invalid_request")
	if e.Error.Details.Fields["code"] == "" {
		t.Fatalf("details.fields.code is empty (body=%s)", body)
	}

	status, body = request(t, app, http.MethodPost, "/api/v1/auth/exchange", `{"code":"does-not-exist"}`, "")
	if status != http.StatusGone {
		t.Fatalf("unknown code: status = %d, want 410 (body=%s)", status, body)
	}
	wantErrorCode(t, body, "gone")
}

func TestAuthRefresh(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)

	status, body := request(t, app, http.MethodPost, "/api/v1/auth/refresh", `{}`, "")
	if status != http.StatusBadRequest {
		t.Fatalf("empty refresh: status = %d, want 400 (body=%s)", status, body)
	}

	status, body = request(t, app, http.MethodPost, "/api/v1/auth/refresh", `{"refreshToken":"nonsense"}`, "")
	if status != http.StatusUnauthorized {
		t.Fatalf("unknown refresh token: status = %d, want 401 (body=%s)", status, body)
	}
	wantErrorCode(t, body, "unauthenticated")
}

func TestUpdateSettings(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)
	env.CreateUser(t, "did:plc:alice", "alice.test")
	token := env.Login(t, "did:plc:alice")

	patch := `{"encounterMode":"immediate","encounterDelaySec":120,"friendBeaconShare":false,"partyAuto":false,"warnDisplay":"hide","notify":{"group.nearby":false}}`
	status, body := request(t, app, http.MethodPatch, "/api/v1/me/settings", patch, token)
	if status != http.StatusOK {
		t.Fatalf("PATCH settings: status = %d, want 200 (body=%s)", status, body)
	}

	status, body = request(t, app, http.MethodGet, "/api/v1/me", "", token)
	if status != http.StatusOK {
		t.Fatalf("GET me: status = %d, want 200 (body=%s)", status, body)
	}
	var me struct {
		Settings struct {
			EncounterMode     string          `json:"encounterMode"`
			EncounterDelaySec int             `json:"encounterDelaySec"`
			FriendBeaconShare bool            `json:"friendBeaconShare"`
			PartyAuto         bool            `json:"partyAuto"`
			WarnDisplay       string          `json:"warnDisplay"`
			Notify            map[string]bool `json:"notify"`
		} `json:"settings"`
	}
	if err := json.Unmarshal(body, &me); err != nil {
		t.Fatalf("decode me: %v (body=%s)", err, body)
	}
	st := me.Settings
	if st.EncounterMode != "immediate" {
		t.Fatalf("encounterMode = %q, want %q", st.EncounterMode, "immediate")
	}
	if st.EncounterDelaySec != 120 {
		t.Fatalf("encounterDelaySec = %d, want 120", st.EncounterDelaySec)
	}
	if st.FriendBeaconShare {
		t.Fatal("friendBeaconShare = true, want false")
	}
	if st.PartyAuto {
		t.Fatal("partyAuto = true, want false")
	}
	if st.WarnDisplay != "hide" {
		t.Fatalf("warnDisplay = %q, want %q", st.WarnDisplay, "hide")
	}
	if v, ok := st.Notify["group.nearby"]; !ok || v {
		t.Fatalf("notify[group.nearby] = %v (present=%v), want false", v, ok)
	}

	invalid := []struct {
		name  string
		body  string
		field string
	}{
		{"delay too small", `{"encounterDelaySec":59}`, "encounterDelaySec"},
		{"delay too large", `{"encounterDelaySec":604801}`, "encounterDelaySec"},
		{"unknown encounter mode", `{"encounterMode":"sometimes"}`, "encounterMode"},
		{"unknown warn display", `{"warnDisplay":"loud"}`, "warnDisplay"},
		{"unknown notify key", `{"notify":{"unknown.key":false}}`, "notify.unknown.key"},
		{"cannot disable moderation notice", `{"notify":{"moderation.hidden":false}}`, "notify.moderation.hidden"},
	}
	for _, tc := range invalid {
		t.Run(tc.name, func(t *testing.T) {
			status, body := request(t, app, http.MethodPatch, "/api/v1/me/settings", tc.body, token)
			if status != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (body=%s)", status, body)
			}
			e := decodeError(t, body)
			if e.Error.Details.Fields[tc.field] == "" {
				t.Fatalf("details.fields[%q] is empty (body=%s)", tc.field, body)
			}
		})
	}

	status, body = request(t, app, http.MethodPatch, "/api/v1/me/settings", `{}`, token)
	if status != http.StatusOK {
		t.Fatalf("empty PATCH: status = %d, want 200 (body=%s)", status, body)
	}

	status, body = request(t, app, http.MethodPatch, "/api/v1/me/settings", `{`, token)
	if status != http.StatusBadRequest {
		t.Fatalf("broken JSON: status = %d, want 400 (body=%s)", status, body)
	}
}

func TestConsents(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)
	env.CreateUser(t, "did:plc:alice", "alice.test")
	token := env.Login(t, "did:plc:alice")

	type consentResponse struct {
		ContentScan int        `json:"contentScan"`
		ConsentedAt *time.Time `json:"consentedAt"`
	}
	type meResponse struct {
		Settings struct {
			ContentScanConsentVersion *int `json:"contentScanConsentVersion"`
		} `json:"settings"`
	}

	status, body := request(t, app, http.MethodPut, "/api/v1/me/consents", `{"contentScan":1}`, token)
	if status != http.StatusOK {
		t.Fatalf("consent 1: status = %d, want 200 (body=%s)", status, body)
	}
	var consent consentResponse
	if err := json.Unmarshal(body, &consent); err != nil {
		t.Fatalf("decode consent: %v (body=%s)", err, body)
	}
	if consent.ContentScan != 1 {
		t.Fatalf("contentScan = %d, want 1", consent.ContentScan)
	}
	if consent.ConsentedAt == nil {
		t.Fatal("consentedAt = nil, want a timestamp")
	}

	status, body = request(t, app, http.MethodGet, "/api/v1/me", "", token)
	if status != http.StatusOK {
		t.Fatalf("GET me after consent: status = %d, want 200 (body=%s)", status, body)
	}
	var me meResponse
	if err := json.Unmarshal(body, &me); err != nil {
		t.Fatalf("decode me: %v (body=%s)", err, body)
	}
	if me.Settings.ContentScanConsentVersion == nil || *me.Settings.ContentScanConsentVersion != 1 {
		t.Fatalf("contentScanConsentVersion = %v, want 1", me.Settings.ContentScanConsentVersion)
	}

	status, body = request(t, app, http.MethodPut, "/api/v1/me/consents", `{"contentScan":0}`, token)
	if status != http.StatusOK {
		t.Fatalf("consent 0: status = %d, want 200 (body=%s)", status, body)
	}
	consent = consentResponse{}
	if err := json.Unmarshal(body, &consent); err != nil {
		t.Fatalf("decode consent 0: %v (body=%s)", err, body)
	}
	if consent.ContentScan != 0 {
		t.Fatalf("contentScan = %d, want 0", consent.ContentScan)
	}
	if consent.ConsentedAt != nil {
		t.Fatalf("consentedAt = %v, want nil", consent.ConsentedAt)
	}

	status, body = request(t, app, http.MethodGet, "/api/v1/me", "", token)
	if status != http.StatusOK {
		t.Fatalf("GET me after revoke: status = %d, want 200 (body=%s)", status, body)
	}
	me = meResponse{}
	if err := json.Unmarshal(body, &me); err != nil {
		t.Fatalf("decode me: %v (body=%s)", err, body)
	}
	if me.Settings.ContentScanConsentVersion != nil {
		t.Fatalf("contentScanConsentVersion = %v, want nil", *me.Settings.ContentScanConsentVersion)
	}

	status, body = request(t, app, http.MethodPut, "/api/v1/me/consents", `{"contentScan":2}`, token)
	if status != http.StatusBadRequest {
		t.Fatalf("unknown consent version: status = %d, want 400 (body=%s)", status, body)
	}
	e := decodeError(t, body)
	if e.Error.Details.Fields["contentScan"] == "" {
		t.Fatalf("details.fields[contentScan] is empty (body=%s)", body)
	}

	status, body = request(t, app, http.MethodPut, "/api/v1/me/consents", `{}`, token)
	if status != http.StatusBadRequest {
		t.Fatalf("missing consent version: status = %d, want 400 (body=%s)", status, body)
	}
	e = decodeError(t, body)
	if e.Error.Details.Fields["contentScan"] == "" {
		t.Fatalf("details.fields[contentScan] is empty (body=%s)", body)
	}
}

func TestGetUser(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)

	const viewer = "did:plc:viewer"
	env.CreateUser(t, viewer, "viewer.test")
	token := env.Login(t, viewer)

	targets := map[string]string{
		"friend":          "did:plc:friend",
		"requested":       "did:plc:requested",
		"incoming":        "did:plc:incoming",
		"revealed":        "did:plc:revealed",
		"pendingEnc":      "did:plc:pendingenc",
		"space":           "did:plc:spacemate",
		"none":            "did:plc:none",
		"blockedByViewer": "did:plc:blockedbyviewer",
		"blockedByTarget": "did:plc:blockedbytarget",
		"deleted":         "did:plc:deleted",
	}
	handles := map[string]string{
		"friend":          "friend.test",
		"requested":       "requested.test",
		"incoming":        "incoming.test",
		"revealed":        "revealed.test",
		"pendingEnc":      "pendingenc.test",
		"space":           "spacemate.test",
		"none":            "none.test",
		"blockedByViewer": "blockedbyviewer.test",
		"blockedByTarget": "blockedbytarget.test",
		"deleted":         "deleted.test",
	}
	for name, did := range targets {
		env.CreateUser(t, did, handles[name])
	}

	now := env.Clock.Now().UnixMilli()

	// friend: 正規化した friendship。
	fa, fb := pair(viewer, targets["friend"])
	execSQL(t, env, `INSERT INTO friendships (user_a, user_b, created_at, via) VALUES (?, ?, ?, 'encounter')`, fa, fb, now)

	// requested: viewer から相手への pending 申請。
	execSQL(t, env, `INSERT INTO friend_requests (id, from_did, to_did, via, status, created_at) VALUES (?, ?, ?, 'encounter', 'pending', ?)`,
		"fr-requested", viewer, targets["requested"], now)

	// incoming: 相手から viewer への pending 申請。
	execSQL(t, env, `INSERT INTO friend_requests (id, from_did, to_did, via, status, created_at) VALUES (?, ?, ?, 'encounter', 'pending', ?)`,
		"fr-incoming", targets["incoming"], viewer, now)

	// revealed: 公開済みの encounter。
	ra, rb := pair(viewer, targets["revealed"])
	execSQL(t, env, `INSERT INTO encounters (id, user_a, user_b, established_at, first_seen_at, last_seen_at, visibility) VALUES (?, ?, ?, ?, ?, ?, 'revealed')`,
		"enc-revealed", ra, rb, now, now, now)

	// pendingEnc: まだ公開されていない encounter は接点に数えない。
	pa, pb := pair(viewer, targets["pendingEnc"])
	execSQL(t, env, `INSERT INTO encounters (id, user_a, user_b, established_at, first_seen_at, last_seen_at, visibility) VALUES (?, ?, ?, ?, ?, ?, 'pending')`,
		"enc-pending", pa, pb, now, now, now)

	// space: 同じ Space のメンバー。viewer の left_at は無視される（過去のメンバーでも接点）。
	execSQL(t, env, `INSERT INTO spaces (id, type, authority, ref, name, state, created_by, created_at) VALUES (?, ?, ?, ?, ?, 'active', ?, ?)`,
		"space-1", "jp.i32.chain.space.group", "did:web:chain.test",
		"at://did:web:chain.test/space/jp.i32.chain.space.group/space-1", "Test Space", viewer, now)
	execSQL(t, env, `INSERT INTO space_members (space_id, did, joined_at, left_at) VALUES (?, ?, ?, ?)`,
		"space-1", viewer, now, now)
	execSQL(t, env, `INSERT INTO space_members (space_id, did, joined_at) VALUES (?, ?, ?)`,
		"space-1", targets["space"], now)

	// blockedByViewer: viewer が相手をブロック。フレンドでも 404。
	ba, bb := pair(viewer, targets["blockedByViewer"])
	execSQL(t, env, `INSERT INTO friendships (user_a, user_b, created_at, via) VALUES (?, ?, ?, 'encounter')`, ba, bb, now)
	execSQL(t, env, `INSERT INTO blocks (blocker, blocked, created_at) VALUES (?, ?, ?)`, viewer, targets["blockedByViewer"], now)

	// blockedByTarget: 相手が viewer をブロック。向きが逆でも 404。
	ca, cb := pair(viewer, targets["blockedByTarget"])
	execSQL(t, env, `INSERT INTO friendships (user_a, user_b, created_at, via) VALUES (?, ?, ?, 'encounter')`, ca, cb, now)
	execSQL(t, env, `INSERT INTO blocks (blocker, blocked, created_at) VALUES (?, ?, ?)`, targets["blockedByTarget"], viewer, now)

	// deleted: 削除済みユーザーはフレンドでも 404。
	da, db := pair(viewer, targets["deleted"])
	execSQL(t, env, `INSERT INTO friendships (user_a, user_b, created_at, via) VALUES (?, ?, ?, 'encounter')`, da, db, now)
	execSQL(t, env, `UPDATE users SET deleted_at = ? WHERE did = ?`, now, targets["deleted"])

	cases := []struct {
		name       string
		target     string
		wantStatus int
		wantHandle string
	}{
		{"self", viewer, http.StatusOK, "viewer.test"},
		{"friend", targets["friend"], http.StatusOK, handles["friend"]},
		{"requested", targets["requested"], http.StatusOK, handles["requested"]},
		{"incoming", targets["incoming"], http.StatusOK, handles["incoming"]},
		{"revealed", targets["revealed"], http.StatusOK, handles["revealed"]},
		{"pendingEnc", targets["pendingEnc"], http.StatusNotFound, ""},
		{"space", targets["space"], http.StatusOK, handles["space"]},
		{"none", targets["none"], http.StatusNotFound, ""},
		{"blockedByViewer", targets["blockedByViewer"], http.StatusNotFound, ""},
		{"blockedByTarget", targets["blockedByTarget"], http.StatusNotFound, ""},
		{"deleted", targets["deleted"], http.StatusNotFound, ""},
		{"invalidDID", "not-a-did", http.StatusBadRequest, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, body := request(t, app, http.MethodGet, "/api/v1/users/"+tc.target, "", token)
			if status != tc.wantStatus {
				t.Fatalf("status = %d, want %d (body=%s)", status, tc.wantStatus, body)
			}
			switch tc.wantStatus {
			case http.StatusOK:
				var u struct {
					DID         string `json:"did"`
					Handle      string `json:"handle"`
					DisplayName string `json:"displayName"`
				}
				if err := json.Unmarshal(body, &u); err != nil {
					t.Fatalf("decode user: %v (body=%s)", err, body)
				}
				if u.DID != tc.target {
					t.Fatalf("did = %q, want %q", u.DID, tc.target)
				}
				if u.Handle != tc.wantHandle {
					t.Fatalf("handle = %q, want %q", u.Handle, tc.wantHandle)
				}
				if u.DisplayName != tc.wantHandle {
					t.Fatalf("displayName = %q, want %q", u.DisplayName, tc.wantHandle)
				}
			case http.StatusNotFound:
				wantErrorCode(t, body, "not_found")
			case http.StatusBadRequest:
				wantErrorCode(t, body, "invalid_request")
			}
		})
	}
}

// TestUnknownPrivateRouteReturnsUnauthenticated は、存在しない /api/v1 配下の
// パスでも未認証なら 404 ではなく 401 になることを固定する。
//
// 理由: controller.Mount は api.Group("", requireAuth) を作り、それを
// /api/v1 の prefix middleware として登録する。Fiber はルートが存在するかを
// 解決する前に、マッチした prefix の Use ミドルウェアを実行するため、
// 未認証の呼び出し元は未知のパスでも requireAuth で止められ 401 になる。
func TestUnknownPrivateRouteReturnsUnauthenticated(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)

	status, body := request(t, app, http.MethodGet, "/api/v1/xxx", "", "")
	if status != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (body=%s)", status, body)
	}
	wantErrorCode(t, body, "unauthenticated")
}
