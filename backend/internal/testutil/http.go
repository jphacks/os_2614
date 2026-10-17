package testutil

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
)

// ここには、複数の機能フォルダーの controller テストが共有する道具を置く。
// HTTP 越しに app を叩く Request と、そのエラー本文を読む APIError、
// 前提を手で整える Space / Group / Member / InsertInstancePost など。

// Request は app.Test を 1 回だけ実行し、ステータスと本文全体を返す。
// body が空でなければ JSON として送り、token が空でなければ Bearer を付ける。
func Request(t *testing.T, app *fiber.App, method, path, body, token string) (int, []byte) {
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

// APIError は API が返すエラー本文。
type APIError struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Details struct {
			Fields map[string]string `json:"fields"`
			Reason string            `json:"reason"`
		} `json:"details"`
	} `json:"error"`
}

// DecodeError はエラー本文を APIError に読む。
func DecodeError(t *testing.T, body []byte) APIError {
	t.Helper()
	var e APIError
	if err := json.Unmarshal(body, &e); err != nil {
		t.Fatalf("decode error JSON: %v (body=%s)", err, body)
	}
	return e
}

// WantErrorCode はエラー本文の code を確かめる。
func WantErrorCode(t *testing.T, body []byte, code string) APIError {
	t.Helper()
	e := DecodeError(t, body)
	if e.Error.Code != code {
		t.Fatalf("error.code = %q, want %q (body=%s)", e.Error.Code, code, body)
	}
	return e
}

// Pair は user_a < user_b の CHECK を満たすように 2 つの DID を並べる。
func Pair(a, b string) (string, string) {
	if a < b {
		return a, b
	}
	return b, a
}

// MustJSONBody は本文を JSON として v に読む。
func MustJSONBody(t *testing.T, body []byte, v any) {
	t.Helper()
	if err := json.Unmarshal(body, v); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}
}

// PostItem は instance の投稿 1 件（GET /instances/{id}/posts の JSON）。
type PostItem struct {
	Kind   string `json:"kind"`
	URI    string `json:"uri"`
	Text   string `json:"text"`
	Hidden string `json:"hidden"`
}

// PostPage は instance の投稿一覧のページ。
type PostPage struct {
	Items  []PostItem `json:"items"`
	Cursor string     `json:"cursor"`
}

// Space は spaces に 1 行入れる。
func Space(t *testing.T, env *Env, id, typ, state, createdBy string) {
	t.Helper()
	now := env.Clock.Now().UnixMilli()
	Exec(t, env, `INSERT INTO spaces (id, type, authority, ref, name, state, created_by, created_at)
		VALUES (?, ?, 'did:web:chain.test', ?, ?, ?, ?, ?)`,
		id, typ, "at://did:web:chain.test/space/"+typ+"/"+id, "space "+id, state, createdBy, now)
}

// Group は groups に 1 行入れる。
func Group(t *testing.T, env *Env, id string, persistent bool) {
	t.Helper()
	Exec(t, env, `INSERT INTO groups (space_id, description, visibility, origin, persistent, center_lat, center_lon, radius_m)
		VALUES (?, '', 'open', 'explicit', ?, 35.0, 135.0, 40)`, id, persistent)
}

// Member は space_members に 1 行入れる。
func Member(t *testing.T, env *Env, id, did string, canWrite bool) {
	t.Helper()
	Exec(t, env, `INSERT INTO space_members (space_id, did, can_read, can_write, role, joined_at)
		VALUES (?, ?, 1, ?, 'member', ?)`, id, did, canWrite, env.Clock.Now().UnixMilli())
}

// InsertInstancePost は instance の投稿を records に直接入れる。
func InsertInstancePost(t *testing.T, env *Env, spaceID, typ, author, rkey, text string, at time.Time, modLabel string) string {
	t.Helper()
	uri := "at://did:web:chain.test/space/" + typ + "/" + spaceID + "/" + author + "/jp.i32.chain.instance.post/" + rkey
	value := fmt.Sprintf(`{"text":%q,"format":"plain","createdAt":%q}`, text, at.UTC().Format(time.RFC3339))
	Exec(t, env, `INSERT INTO records (uri, space_id, author_did, collection, rkey, value_json, created_at, mod_label, mod_state)
		VALUES (?, ?, ?, 'jp.i32.chain.instance.post', ?, ?, ?, ?, 'done')`,
		uri, spaceID, author, rkey, value, at.UnixMilli(), modLabel)
	return uri
}
