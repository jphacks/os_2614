//go:build integration

package encounter_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/applemango/chain/internal/kv"
	"github.com/applemango/chain/internal/testutil"
)

func TestBeaconTokens(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)
	env.CreateUser(t, "did:plc:alice", "alice.test")
	token := env.Login(t, "did:plc:alice")

	status, body := testutil.Request(t, app, http.MethodGet, "/api/v1/beacon/tokens", "", "")
	if status != http.StatusUnauthorized {
		t.Fatalf("no token: status = %d, want 401 (body=%s)", status, body)
	}
	testutil.WantErrorCode(t, body, "unauthenticated")

	type tokensResponse struct {
		WindowSec int `json:"windowSec"`
		Tokens    []struct {
			WindowStart string `json:"windowStart"`
			Token       string `json:"token"`
		} `json:"tokens"`
	}

	for _, tc := range []struct {
		name  string
		query string
		want  int
	}{
		{"invalid hours", "?hours=0", http.StatusBadRequest},
		{"non-numeric hours", "?hours=abc", http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, body := testutil.Request(t, app, http.MethodGet, "/api/v1/beacon/tokens"+tc.query, "", token)
			if status != tc.want {
				t.Fatalf("status = %d, want %d (body=%s)", status, tc.want, body)
			}
			testutil.WantErrorCode(t, body, "invalid_request")
		})
	}

	status, body = testutil.Request(t, app, http.MethodGet, "/api/v1/beacon/tokens?hours=1", "", token)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", status, body)
	}
	var first tokensResponse
	if err := json.Unmarshal(body, &first); err != nil {
		t.Fatalf("decode: %v (body=%s)", err, body)
	}
	if first.WindowSec != 900 {
		t.Fatalf("windowSec = %d, want 900", first.WindowSec)
	}
	if len(first.Tokens) != 4 {
		t.Fatalf("tokens = %d, want 4", len(first.Tokens))
	}
	for i, tok := range first.Tokens {
		raw, err := base64.RawURLEncoding.DecodeString(tok.Token)
		if err != nil || len(raw) != 16 {
			t.Fatalf("token[%d] is not 16-byte base64url (%d bytes, err=%v)", i, len(raw), err)
		}
	}

	_, body2 := testutil.Request(t, app, http.MethodGet, "/api/v1/beacon/tokens?hours=1", "", token)
	var second tokensResponse
	if err := json.Unmarshal(body2, &second); err != nil {
		t.Fatalf("decode second: %v", err)
	}
	if !reflect.DeepEqual(first.Tokens, second.Tokens) {
		t.Fatalf("tokens are not idempotent:\n%+v\n%+v", first.Tokens, second.Tokens)
	}
}

func TestLocation(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)
	env.CreateUser(t, "did:plc:alice", "alice.test")
	token := env.Login(t, "did:plc:alice")
	now := env.Clock.Now()

	body := func(lat, lon, acc float64, recordedAt time.Time, mock bool) string {
		return fmt.Sprintf(`{"lat":%v,"lon":%v,"accuracyM":%v,"recordedAt":%q,"source":"gps","isMock":%v}`,
			lat, lon, acc, recordedAt.UTC().Format(time.RFC3339), mock)
	}

	status, respBody := testutil.Request(t, app, http.MethodPut, "/api/v1/me/location", body(35, 135, 12.5, now, false), "")
	if status != http.StatusUnauthorized {
		t.Fatalf("no token: status = %d, want 401 (body=%s)", status, respBody)
	}

	status, respBody = testutil.Request(t, app, http.MethodPut, "/api/v1/me/location", body(35.0, 135.0, 12.5, now, false), token)
	if status != http.StatusNoContent {
		t.Fatalf("valid: status = %d, want 204 (body=%s)", status, respBody)
	}
	stored, err := env.Redis.HGetAll(context.Background(), kv.LocationKey("did:plc:alice")).Result()
	if err != nil {
		t.Fatalf("HGetAll: %v", err)
	}
	if len(stored) == 0 {
		t.Fatal("loc:{did} was not saved")
	}
	if stored["lat"] != "35" || stored["src"] != "gps" {
		t.Fatalf("stored = %+v", stored)
	}

	invalid := []struct {
		name  string
		body  string
		field string
	}{
		{"missing lat", `{"lon":135,"recordedAt":"` + now.Format(time.RFC3339) + `"}`, "lat"},
		{"out of range lat", body(91, 135, 10, now, false), "lat"},
		{"accuracy too large", body(35, 135, 250, now, false), "accuracyM"},
		{"recordedAt too old", body(35, 135, 10, now.Add(-5*time.Minute), false), "recordedAt"},
		{"mocked", body(35, 135, 10, now, true), "isMock"},
	}
	for _, tc := range invalid {
		t.Run(tc.name, func(t *testing.T) {
			status, respBody := testutil.Request(t, app, http.MethodPut, "/api/v1/me/location", tc.body, token)
			if status != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (body=%s)", status, respBody)
			}
			e := testutil.DecodeError(t, respBody)
			if e.Error.Details.Fields[tc.field] == "" {
				t.Fatalf("details.fields[%q] is empty (body=%s)", tc.field, respBody)
			}
		})
	}
}

func TestLocationRejectsImpossibleSpeed(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)
	env.CreateUser(t, "did:plc:alice", "alice.test")
	token := env.Login(t, "did:plc:alice")
	now := env.Clock.Now()

	first := fmt.Sprintf(`{"lat":35.0,"lon":135.0,"accuracyM":10,"recordedAt":%q}`, now.UTC().Format(time.RFC3339))
	if status, body := testutil.Request(t, app, http.MethodPut, "/api/v1/me/location", first, token); status != http.StatusNoContent {
		t.Fatalf("first: status = %d, want 204 (body=%s)", status, body)
	}

	// 60 秒で約 11 km 移動 = 660 km/h。
	second := fmt.Sprintf(`{"lat":35.1,"lon":135.0,"accuracyM":10,"recordedAt":%q}`,
		now.Add(time.Minute).UTC().Format(time.RFC3339))
	env.Clock.Set(now.Add(time.Minute))
	status, body := testutil.Request(t, app, http.MethodPut, "/api/v1/me/location", second, token)
	if status != http.StatusBadRequest {
		t.Fatalf("fast move: status = %d, want 400 (body=%s)", status, body)
	}
	testutil.WantErrorCode(t, body, "invalid_request")

	var n int
	if err := env.DB.QueryRowContext(context.Background(),
		"SELECT count(*) FROM audit_log WHERE action = 'location.speed_rejected'").Scan(&n); err != nil {
		t.Fatalf("count audit_log: %v", err)
	}
	if n != 1 {
		t.Fatalf("audit_log rows = %d, want 1", n)
	}
}

func TestSightings(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)
	env.CreateUser(t, "did:plc:alice", "alice.test")
	env.CreateUser(t, "did:plc:bob", "bob.test")
	aliceToken := env.Login(t, "did:plc:alice")
	bobToken := env.Login(t, "did:plc:bob")
	now := env.Clock.Now().UTC().Format(time.RFC3339)

	status, body := testutil.Request(t, app, http.MethodPost, "/api/v1/encounters/sightings", `{"items":[]}`, "")
	if status != http.StatusUnauthorized {
		t.Fatalf("no token: status = %d, want 401 (body=%s)", status, body)
	}

	status, body = testutil.Request(t, app, http.MethodPost, "/api/v1/encounters/sightings", `{"items":[]}`, aliceToken)
	if status != http.StatusOK {
		t.Fatalf("empty items: status = %d, want 200 (body=%s)", status, body)
	}
	var empty struct {
		Accepted int `json:"accepted"`
		Dropped  int `json:"dropped"`
	}
	if err := json.Unmarshal(body, &empty); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if empty.Accepted != 0 || empty.Dropped != 0 {
		t.Fatalf("empty = %+v, want 0/0", empty)
	}

	bogus := fmt.Sprintf(`{"items":[{"token":"AAAAAAAAAAAAAAAAAAAAAA","firstSeenAt":%q,"lastSeenAt":%q,"maxRssi":-70}]}`, now, now)
	status, body = testutil.Request(t, app, http.MethodPost, "/api/v1/encounters/sightings", bogus, aliceToken)
	if status != http.StatusOK {
		t.Fatalf("unknown token: status = %d, want 200 (body=%s)", status, body)
	}
	var dropped struct {
		Accepted int `json:"accepted"`
		Dropped  int `json:"dropped"`
	}
	if err := json.Unmarshal(body, &dropped); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if dropped.Accepted != 0 || dropped.Dropped != 1 {
		t.Fatalf("unknown token = %+v, want 0/1", dropped)
	}

	var tooMany strings.Builder
	tooMany.WriteString(`{"items":[`)
	for i := 0; i < 501; i++ {
		if i > 0 {
			tooMany.WriteByte(',')
		}
		fmt.Fprintf(&tooMany, `{"token":"AAAAAAAAAAAAAAAAAAAAAA","firstSeenAt":%q,"lastSeenAt":%q,"maxRssi":-70}`, now, now)
	}
	tooMany.WriteString(`]}`)
	status, body = testutil.Request(t, app, http.MethodPost, "/api/v1/encounters/sightings", tooMany.String(), aliceToken)
	if status != http.StatusBadRequest {
		t.Fatalf("501 items: status = %d, want 400 (body=%s)", status, body)
	}
	e := testutil.DecodeError(t, body)
	if e.Error.Details.Fields["items"] == "" {
		t.Fatalf("details.fields[items] is empty (body=%s)", body)
	}

	// bob の token を発行し、alice がその token を観測したことにする。
	_, tokensBody := testutil.Request(t, app, http.MethodGet, "/api/v1/beacon/tokens?hours=1", "", bobToken)
	var tokens struct {
		Tokens []struct {
			Token string `json:"token"`
		} `json:"tokens"`
	}
	if err := json.Unmarshal(tokensBody, &tokens); err != nil {
		t.Fatalf("decode tokens: %v (body=%s)", err, tokensBody)
	}
	if len(tokens.Tokens) == 0 {
		t.Fatal("no tokens returned")
	}
	accepted := fmt.Sprintf(`{"items":[{"token":%q,"firstSeenAt":%q,"lastSeenAt":%q,"maxRssi":-70}]}`,
		tokens.Tokens[0].Token, now, now)
	status, body = testutil.Request(t, app, http.MethodPost, "/api/v1/encounters/sightings", accepted, aliceToken)
	if status != http.StatusOK {
		t.Fatalf("accepted: status = %d, want 200 (body=%s)", status, body)
	}
	var got struct {
		Accepted int `json:"accepted"`
		Dropped  int `json:"dropped"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Accepted != 1 || got.Dropped != 0 {
		t.Fatalf("accepted = %+v, want 1/0", got)
	}

	if strings.Contains(string(body), tokens.Tokens[0].Token) {
		t.Fatal("response leaks the token")
	}
}

func TestEncountersList(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)

	const viewer, friend, other = "did:plc:viewer", "did:plc:friend", "did:plc:other"
	env.CreateUser(t, viewer, "viewer.test")
	env.CreateUser(t, friend, "friend.test")
	env.CreateUser(t, other, "other.test")
	token := env.Login(t, viewer)
	now := env.Clock.Now()

	fa, fb := testutil.Pair(viewer, friend)
	oa, ob := testutil.Pair(viewer, other)
	testutil.Exec(t, env, `INSERT INTO friendships (user_a, user_b, created_at, via) VALUES (?, ?, ?, 'encounter')`, fa, fb, now.UnixMilli())
	testutil.Exec(t, env, `INSERT INTO encounters (id, user_a, user_b, established_at, first_seen_at, last_seen_at, visibility) VALUES (?, ?, ?, ?, ?, ?, 'revealed')`,
		"enc-friend", fa, fb, now.Add(-time.Hour).UnixMilli(), now.Add(-time.Hour).UnixMilli(), now.Add(-time.Hour).Add(75*time.Second).UnixMilli())
	testutil.Exec(t, env, `INSERT INTO encounters (id, user_a, user_b, established_at, first_seen_at, last_seen_at, visibility) VALUES (?, ?, ?, ?, ?, ?, 'revealed')`,
		"enc-other", oa, ob, now.Add(-2*time.Hour).UnixMilli(), now.Add(-2*time.Hour).UnixMilli(), now.Add(-2*time.Hour).UnixMilli())
	testutil.Exec(t, env, `INSERT INTO encounters (id, user_a, user_b, established_at, first_seen_at, last_seen_at, visibility) VALUES (?, ?, ?, ?, ?, ?, 'pending')`,
		"enc-pending", fa, fb, now.UnixMilli(), now.UnixMilli(), now.UnixMilli())

	type listResponse struct {
		Items []struct {
			ID   string `json:"id"`
			Peer struct {
				DID    string `json:"did"`
				Handle string `json:"handle"`
			} `json:"peer"`
			SeenFrom      time.Time `json:"seenFrom"`
			SeenUntil     time.Time `json:"seenUntil"`
			TimesWithPeer int       `json:"timesWithPeer"`
			Relation      string    `json:"relation"`
		} `json:"items"`
		Cursor string `json:"cursor"`
	}

	status, body := testutil.Request(t, app, http.MethodGet, "/api/v1/encounters?limit=10", "", token)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", status, body)
	}
	var page listResponse
	if err := json.Unmarshal(body, &page); err != nil {
		t.Fatalf("decode: %v (body=%s)", err, body)
	}
	if len(page.Items) != 2 {
		t.Fatalf("items = %d, want 2 (body=%s)", len(page.Items), body)
	}
	if page.Cursor != "" {
		t.Fatalf("cursor = %q, want empty", page.Cursor)
	}
	if page.Items[0].ID != "enc-friend" {
		t.Fatalf("first item = %q, want enc-friend (newest first)", page.Items[0].ID)
	}
	item := page.Items[0]
	if item.Relation != "friend" {
		t.Fatalf("relation = %q, want friend", item.Relation)
	}
	if item.TimesWithPeer != 1 {
		t.Fatalf("timesWithPeer = %d, want 1", item.TimesWithPeer)
	}
	if item.Peer.DID != friend || item.Peer.Handle != "friend.test" {
		t.Fatalf("peer = %+v", item.Peer)
	}
	if got, want := item.SeenFrom.UTC(), now.Add(-time.Hour).Truncate(15*time.Minute); !got.Equal(want) {
		t.Fatalf("seenFrom = %v, want %v", got, want)
	}

	// limit=1 でカーソルをたどる。
	status, body = testutil.Request(t, app, http.MethodGet, "/api/v1/encounters?limit=1", "", token)
	if status != http.StatusOK {
		t.Fatalf("limit=1: status = %d (body=%s)", status, body)
	}
	var p1 listResponse
	if err := json.Unmarshal(body, &p1); err != nil {
		t.Fatalf("decode p1: %v", err)
	}
	if len(p1.Items) != 1 || p1.Cursor == "" {
		t.Fatalf("p1 = %+v, want 1 item and a cursor", p1)
	}
	status, body = testutil.Request(t, app, http.MethodGet, "/api/v1/encounters?limit=1&cursor="+p1.Cursor, "", token)
	if status != http.StatusOK {
		t.Fatalf("cursor page: status = %d (body=%s)", status, body)
	}
	var p2 listResponse
	if err := json.Unmarshal(body, &p2); err != nil {
		t.Fatalf("decode p2: %v", err)
	}
	if len(p2.Items) != 1 || p2.Items[0].ID != "enc-other" {
		t.Fatalf("p2 = %+v, want enc-other", p2.Items)
	}

	// ブロックした相手の encounter は出ない。
	testutil.Exec(t, env, `INSERT INTO blocks (blocker, blocked, created_at) VALUES (?, ?, ?)`, viewer, other, now.UnixMilli())
	status, body = testutil.Request(t, app, http.MethodGet, "/api/v1/encounters?limit=10", "", token)
	if status != http.StatusOK {
		t.Fatalf("after block: status = %d (body=%s)", status, body)
	}
	var blocked listResponse
	if err := json.Unmarshal(body, &blocked); err != nil {
		t.Fatalf("decode blocked: %v", err)
	}
	if len(blocked.Items) != 1 || blocked.Items[0].ID != "enc-friend" {
		t.Fatalf("blocked list = %+v, want only enc-friend", blocked.Items)
	}

	status, body = testutil.Request(t, app, http.MethodGet, "/api/v1/encounters?cursor=!!!", "", token)
	if status != http.StatusBadRequest {
		t.Fatalf("bad cursor: status = %d, want 400 (body=%s)", status, body)
	}
	testutil.WantErrorCode(t, body, "invalid_request")
}

func TestBlocks(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)

	const viewer, target, friend = "did:plc:viewer", "did:plc:target", "did:plc:friend"
	env.CreateUser(t, viewer, "viewer.test")
	env.CreateUser(t, target, "target.test")
	env.CreateUser(t, friend, "friend.test")
	token := env.Login(t, viewer)
	now := env.Clock.Now().UnixMilli()

	ta, tb := testutil.Pair(viewer, target)
	testutil.Exec(t, env, `INSERT INTO friendships (user_a, user_b, created_at, via) VALUES (?, ?, ?, 'encounter')`, ta, tb, now)
	testutil.Exec(t, env, `INSERT INTO friend_requests (id, from_did, to_did, via, status, created_at) VALUES ('fr-1', ?, ?, 'encounter', 'pending', ?)`, viewer, target, now)
	testutil.Exec(t, env, `INSERT INTO location_shares (owner, viewer, precision, updated_at) VALUES (?, ?, 'exact', ?)`, viewer, target, now)

	status, body := testutil.Request(t, app, http.MethodPost, "/api/v1/blocks", `{"did":"`+target+`"}`, "")
	if status != http.StatusUnauthorized {
		t.Fatalf("no token: status = %d, want 401 (body=%s)", status, body)
	}

	status, body = testutil.Request(t, app, http.MethodPost, "/api/v1/blocks", `{"did":"`+target+`"}`, token)
	if status != http.StatusNoContent {
		t.Fatalf("block: status = %d, want 204 (body=%s)", status, body)
	}

	checkEmpty := func(query string, args ...any) {
		t.Helper()
		var n int
		if err := env.DB.QueryRowContext(context.Background(), query, args...).Scan(&n); err != nil {
			t.Fatalf("count %q: %v", query, err)
		}
		if n != 0 {
			t.Fatalf("%q = %d, want 0", query, n)
		}
	}
	checkEmpty("SELECT count(*) FROM friendships WHERE user_a = ? AND user_b = ?", ta, tb)
	checkEmpty("SELECT count(*) FROM friend_requests WHERE from_did = ? AND to_did = ?", viewer, target)
	checkEmpty("SELECT count(*) FROM location_shares WHERE owner = ? AND viewer = ?", viewer, target)

	var audits int
	if err := env.DB.QueryRowContext(context.Background(),
		"SELECT count(*) FROM audit_log WHERE action = 'block.create' AND actor_did = ? AND target = ?", viewer, target).Scan(&audits); err != nil {
		t.Fatalf("count audit: %v", err)
	}
	if audits != 1 {
		t.Fatalf("audit rows = %d, want 1", audits)
	}

	type blocksResponse struct {
		Items []struct {
			DID    string `json:"did"`
			Handle string `json:"handle"`
		} `json:"items"`
	}
	status, body = testutil.Request(t, app, http.MethodGet, "/api/v1/blocks", "", token)
	if status != http.StatusOK {
		t.Fatalf("list: status = %d (body=%s)", status, body)
	}
	var list blocksResponse
	if err := json.Unmarshal(body, &list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(list.Items) != 1 || list.Items[0].DID != target || list.Items[0].Handle != "target.test" {
		t.Fatalf("blocks = %+v", list.Items)
	}

	invalid := []struct {
		name       string
		body       string
		wantStatus int
		wantCode   string
	}{
		{"self", `{"did":"` + viewer + `"}`, http.StatusBadRequest, "invalid_request"},
		{"bad did", `{"did":"not-a-did"}`, http.StatusBadRequest, "invalid_request"},
		{"missing did", `{}`, http.StatusBadRequest, "invalid_request"},
		{"unknown did", `{"did":"did:plc:nobody"}`, http.StatusNotFound, "not_found"},
	}
	for _, tc := range invalid {
		t.Run(tc.name, func(t *testing.T) {
			status, body := testutil.Request(t, app, http.MethodPost, "/api/v1/blocks", tc.body, token)
			if status != tc.wantStatus {
				t.Fatalf("status = %d, want %d (body=%s)", status, tc.wantStatus, body)
			}
			testutil.WantErrorCode(t, body, tc.wantCode)
		})
	}

	status, body = testutil.Request(t, app, http.MethodDelete, "/api/v1/blocks/"+target, "", token)
	if status != http.StatusNoContent {
		t.Fatalf("unblock: status = %d, want 204 (body=%s)", status, body)
	}
	status, body = testutil.Request(t, app, http.MethodDelete, "/api/v1/blocks/did:plc:unknown", "", token)
	if status != http.StatusNoContent {
		t.Fatalf("unblock unknown: status = %d, want 204 (body=%s)", status, body)
	}
	status, body = testutil.Request(t, app, http.MethodDelete, "/api/v1/blocks/not-a-did", "", token)
	if status != http.StatusBadRequest {
		t.Fatalf("unblock bad did: status = %d, want 400 (body=%s)", status, body)
	}

	_, body = testutil.Request(t, app, http.MethodGet, "/api/v1/blocks", "", token)
	if err := json.Unmarshal(body, &list); err != nil {
		t.Fatalf("decode after unblock: %v", err)
	}
	if len(list.Items) != 0 {
		t.Fatalf("blocks after unblock = %+v, want empty", list.Items)
	}
}
