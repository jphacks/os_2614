//go:build integration

package controller_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/applemango/chain/internal/testutil"
)

type inviteResponse struct {
	Code      string    `json:"code"`
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expiresAt"`
}

type friendItemResponse struct {
	DID         string    `json:"did"`
	Handle      string    `json:"handle"`
	DisplayName string    `json:"displayName"`
	Since       time.Time `json:"since"`
	Precision   string    `json:"precision"`
}

type friendListResponse struct {
	Items []friendItemResponse `json:"items"`
}

type friendRequestItemResponse struct {
	ID   string `json:"id"`
	Peer struct {
		DID string `json:"did"`
	} `json:"peer"`
	Via    string `json:"via"`
	ViaRef string `json:"viaRef"`
	Status string `json:"status"`
}

func createInvite(t *testing.T, app *fiber.App, token string) inviteResponse {
	t.Helper()
	status, body := request(t, app, http.MethodPost, "/api/v1/friends/invites", "", token)
	if status != http.StatusCreated {
		t.Fatalf("create invite: status = %d, want 201 (body=%s)", status, body)
	}
	var inv inviteResponse
	if err := json.Unmarshal(body, &inv); err != nil {
		t.Fatalf("decode invite: %v (body=%s)", err, body)
	}
	return inv
}

func TestFriendsAPIAcceptInvite(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)
	env.CreateUser(t, "did:plc:alice", "alice.test")
	env.CreateUser(t, "did:plc:bob", "bob.test")
	aliceToken := env.Login(t, "did:plc:alice")
	bobToken := env.Login(t, "did:plc:bob")

	inv := createInvite(t, app, aliceToken)
	if len(inv.Code) != 9 || inv.Code[4] != '-' {
		t.Fatalf("code = %q, want XXXX-XXXX", inv.Code)
	}
	if inv.URL != "chain://friend/"+strings.ReplaceAll(inv.Code, "-", "") {
		t.Fatalf("url = %q", inv.URL)
	}

	// ハイフンなし・小文字で受け付ける。
	code := strings.ToLower(strings.ReplaceAll(inv.Code, "-", ""))
	status, body := request(t, app, http.MethodPost, "/api/v1/friends/invites/"+code+"/accept", "", bobToken)
	if status != http.StatusOK {
		t.Fatalf("accept invite: status = %d, want 200 (body=%s)", status, body)
	}
	var accepted struct {
		Friend friendItemResponse `json:"friend"`
	}
	if err := json.Unmarshal(body, &accepted); err != nil {
		t.Fatalf("decode accept: %v (body=%s)", err, body)
	}
	if accepted.Friend.DID != "did:plc:alice" || accepted.Friend.Precision != "area" {
		t.Fatalf("friend = %+v", accepted.Friend)
	}

	// 一覧（07 §2.3）。
	status, body = request(t, app, http.MethodGet, "/api/v1/friends", "", bobToken)
	if status != http.StatusOK {
		t.Fatalf("list friends: status = %d (body=%s)", status, body)
	}
	var list friendListResponse
	if err := json.Unmarshal(body, &list); err != nil {
		t.Fatalf("decode friends: %v (body=%s)", err, body)
	}
	if len(list.Items) != 1 || list.Items[0].DID != "did:plc:alice" || list.Items[0].Handle != "alice.test" {
		t.Fatalf("items = %+v", list.Items)
	}

	// 使用済みは 410。
	status, body = request(t, app, http.MethodPost, "/api/v1/friends/invites/"+inv.Code+"/accept", "", bobToken)
	if status != http.StatusGone {
		t.Fatalf("used invite: status = %d, want 410 (body=%s)", status, body)
	}
	wantErrorCode(t, body, "gone")

	// 不正な形式も 410（存在しないコードとして扱う）。
	status, body = request(t, app, http.MethodPost, "/api/v1/friends/invites/nope/accept", "", bobToken)
	if status != http.StatusGone {
		t.Fatalf("malformed invite: status = %d, want 410 (body=%s)", status, body)
	}

	// 自分のコードは 400。
	selfInvite := createInvite(t, app, aliceToken)
	status, body = request(t, app, http.MethodPost, "/api/v1/friends/invites/"+selfInvite.Code+"/accept", "", aliceToken)
	if status != http.StatusBadRequest {
		t.Fatalf("own invite: status = %d, want 400 (body=%s)", status, body)
	}
	e := wantErrorCode(t, body, "invalid_request")
	if e.Error.Details.Fields["code"] == "" {
		t.Fatalf("details.fields.code is empty (body=%s)", body)
	}

	// 既にフレンドは 409。
	status, body = request(t, app, http.MethodPost, "/api/v1/friends/invites/"+selfInvite.Code+"/accept", "", bobToken)
	if status != http.StatusConflict {
		t.Fatalf("already friend: status = %d, want 409 (body=%s)", status, body)
	}
	wantErrorCode(t, body, "conflict")

	// ブロック関係は 403。
	env.CreateUser(t, "did:plc:carol", "carol.test")
	carolToken := env.Login(t, "did:plc:carol")
	execSQL(t, env, "INSERT INTO blocks (blocker, blocked, created_at) VALUES (?, ?, ?)",
		"did:plc:carol", "did:plc:alice", env.Clock.Now().UnixMilli())
	blockedInvite := createInvite(t, app, aliceToken)
	status, body = request(t, app, http.MethodPost, "/api/v1/friends/invites/"+blockedInvite.Code+"/accept", "", carolToken)
	if status != http.StatusForbidden {
		t.Fatalf("blocked invite: status = %d, want 403 (body=%s)", status, body)
	}
	wantErrorCode(t, body, "blocked")

	// 期限切れは 410。
	env.CreateUser(t, "did:plc:dave", "dave.test")
	daveToken := env.Login(t, "did:plc:dave")
	expiring := createInvite(t, app, aliceToken)
	env.Clock.Advance(11 * time.Minute)
	status, body = request(t, app, http.MethodPost, "/api/v1/friends/invites/"+expiring.Code+"/accept", "", daveToken)
	if status != http.StatusGone {
		t.Fatalf("expired invite: status = %d, want 410 (body=%s)", status, body)
	}
}

func TestFriendRequestsAPI(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)
	const alice, bob, carol, dave = "did:plc:alice", "did:plc:bob", "did:plc:carol", "did:plc:dave"
	for _, u := range []struct{ did, handle string }{
		{alice, "alice.test"}, {bob, "bob.test"}, {carol, "carol.test"}, {dave, "dave.test"},
	} {
		env.CreateUser(t, u.did, u.handle)
	}
	aliceToken := env.Login(t, alice)
	bobToken := env.Login(t, bob)
	carolToken := env.Login(t, carol)

	now := env.Clock.Now().UnixMilli()
	a, b := pair(alice, bob)
	execSQL(t, env, `INSERT INTO encounters (id, user_a, user_b, established_at, first_seen_at, last_seen_at, visibility)
		VALUES ('enc-ab', ?, ?, ?, ?, ?, 'revealed')`, a, b, now, now, now)

	// 検証エラー。
	for _, tc := range []struct {
		name  string
		body  string
		field string
	}{
		{"missing did", `{"via":"encounter","viaRef":"enc-ab"}`, "did"},
		{"bad did", `{"did":"nope","via":"encounter","viaRef":"enc-ab"}`, "did"},
		{"bad via", `{"did":"` + bob + `","via":"email","viaRef":"enc-ab"}`, "via"},
		{"missing viaRef", `{"did":"` + bob + `","via":"encounter"}`, "viaRef"},
		{"self", `{"did":"` + alice + `","via":"encounter","viaRef":"enc-ab"}`, "did"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, body := request(t, app, http.MethodPost, "/api/v1/friends/requests", tc.body, aliceToken)
			if status != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (body=%s)", status, body)
			}
			e := decodeError(t, body)
			if e.Error.Details.Fields[tc.field] == "" {
				t.Fatalf("details.fields[%q] is empty (body=%s)", tc.field, body)
			}
		})
	}

	// 関係のない相手は 403。
	status, body := request(t, app, http.MethodPost, "/api/v1/friends/requests",
		`{"did":"`+dave+`","via":"encounter","viaRef":"enc-ab"}`, aliceToken)
	if status != http.StatusForbidden {
		t.Fatalf("unrelated: status = %d, want 403 (body=%s)", status, body)
	}
	wantErrorCode(t, body, "forbidden")

	// 正常系。
	status, body = request(t, app, http.MethodPost, "/api/v1/friends/requests",
		`{"did":"`+bob+`","via":"encounter","viaRef":"enc-ab"}`, aliceToken)
	if status != http.StatusCreated {
		t.Fatalf("create request: status = %d, want 201 (body=%s)", status, body)
	}
	var created friendRequestItemResponse
	if err := json.Unmarshal(body, &created); err != nil {
		t.Fatalf("decode request: %v (body=%s)", err, body)
	}
	if created.Status != "pending" || created.Peer.DID != bob || created.Via != "encounter" || created.ViaRef != "enc-ab" {
		t.Fatalf("request = %+v", created)
	}

	// 二重申請は 409。
	status, body = request(t, app, http.MethodPost, "/api/v1/friends/requests",
		`{"did":"`+bob+`","via":"encounter","viaRef":"enc-ab"}`, aliceToken)
	if status != http.StatusConflict {
		t.Fatalf("duplicate request: status = %d, want 409 (body=%s)", status, body)
	}

	// 一覧。
	status, body = request(t, app, http.MethodGet, "/api/v1/friends/requests?box=incoming", "", bobToken)
	if status != http.StatusOK {
		t.Fatalf("incoming: status = %d (body=%s)", status, body)
	}
	var page struct {
		Items []friendRequestItemResponse `json:"items"`
	}
	if err := json.Unmarshal(body, &page); err != nil {
		t.Fatalf("decode incoming: %v (body=%s)", err, body)
	}
	if len(page.Items) != 1 || page.Items[0].ID != created.ID || page.Items[0].Peer.DID != alice {
		t.Fatalf("incoming = %+v", page.Items)
	}
	status, body = request(t, app, http.MethodGet, "/api/v1/friends/requests?box=outgoing", "", aliceToken)
	if status != http.StatusOK {
		t.Fatalf("outgoing: status = %d (body=%s)", status, body)
	}
	page.Items = nil
	if err := json.Unmarshal(body, &page); err != nil {
		t.Fatalf("decode outgoing: %v (body=%s)", err, body)
	}
	if len(page.Items) != 1 || page.Items[0].ID != created.ID {
		t.Fatalf("outgoing = %+v", page.Items)
	}
	status, body = request(t, app, http.MethodGet, "/api/v1/friends/requests?box=sideways", "", aliceToken)
	if status != http.StatusBadRequest {
		t.Fatalf("bad box: status = %d, want 400 (body=%s)", status, body)
	}

	// 受信者以外の承認は 404。
	status, body = request(t, app, http.MethodPost, "/api/v1/friends/requests/"+created.ID+"/accept", "", carolToken)
	if status != http.StatusNotFound {
		t.Fatalf("non-recipient accept: status = %d, want 404 (body=%s)", status, body)
	}

	// 承認。
	status, body = request(t, app, http.MethodPost, "/api/v1/friends/requests/"+created.ID+"/accept", "", bobToken)
	if status != http.StatusOK {
		t.Fatalf("accept: status = %d, want 200 (body=%s)", status, body)
	}
	var accepted struct {
		Friend friendItemResponse `json:"friend"`
	}
	if err := json.Unmarshal(body, &accepted); err != nil {
		t.Fatalf("decode accept: %v (body=%s)", err, body)
	}
	if accepted.Friend.DID != alice {
		t.Fatalf("friend = %+v", accepted.Friend)
	}
	status, body = request(t, app, http.MethodGet, "/api/v1/friends", "", bobToken)
	if status != http.StatusOK {
		t.Fatalf("friends: status = %d (body=%s)", status, body)
	}

	// フレンド削除で一覧から消える。
	status, body = request(t, app, http.MethodDelete, "/api/v1/friends/"+alice, "", bobToken)
	if status != http.StatusNoContent {
		t.Fatalf("delete friend: status = %d, want 204 (body=%s)", status, body)
	}
	status, body = request(t, app, http.MethodGet, "/api/v1/friends", "", bobToken)
	var list friendListResponse
	if err := json.Unmarshal(body, &list); err != nil {
		t.Fatalf("decode friends after delete: %v (body=%s)", err, body)
	}
	if len(list.Items) != 0 {
		t.Fatalf("friends after delete = %+v", list.Items)
	}
	status, body = request(t, app, http.MethodDelete, "/api/v1/friends/"+alice, "", bobToken)
	if status != http.StatusNotFound {
		t.Fatalf("second delete: status = %d, want 404 (body=%s)", status, body)
	}

	// 拒否。
	c, d := pair(bob, carol)
	execSQL(t, env, `INSERT INTO encounters (id, user_a, user_b, established_at, first_seen_at, last_seen_at, visibility)
		VALUES ('enc-bc', ?, ?, ?, ?, ?, 'revealed')`, c, d, now, now, now)
	status, body = request(t, app, http.MethodPost, "/api/v1/friends/requests",
		`{"did":"`+carol+`","via":"encounter","viaRef":"enc-bc"}`, bobToken)
	if status != http.StatusCreated {
		t.Fatalf("bob->carol: status = %d (body=%s)", status, body)
	}
	var bc friendRequestItemResponse
	if err := json.Unmarshal(body, &bc); err != nil {
		t.Fatalf("decode: %v (body=%s)", err, body)
	}
	status, body = request(t, app, http.MethodPost, "/api/v1/friends/requests/"+bc.ID+"/decline", "", carolToken)
	if status != http.StatusNoContent {
		t.Fatalf("decline: status = %d, want 204 (body=%s)", status, body)
	}
	// declined 後 7 日以内の再申請は 409。
	status, body = request(t, app, http.MethodPost, "/api/v1/friends/requests",
		`{"did":"`+carol+`","via":"encounter","viaRef":"enc-bc"}`, bobToken)
	if status != http.StatusConflict {
		t.Fatalf("re-request: status = %d, want 409 (body=%s)", status, body)
	}

	// 送信者以外の取消は 404、送信者なら 204。
	e, f := pair(alice, dave)
	execSQL(t, env, `INSERT INTO encounters (id, user_a, user_b, established_at, first_seen_at, last_seen_at, visibility)
		VALUES ('enc-ad', ?, ?, ?, ?, ?, 'revealed')`, e, f, now, now, now)
	status, body = request(t, app, http.MethodPost, "/api/v1/friends/requests",
		`{"did":"`+dave+`","via":"encounter","viaRef":"enc-ad"}`, aliceToken)
	if status != http.StatusCreated {
		t.Fatalf("alice->dave: status = %d (body=%s)", status, body)
	}
	var ad friendRequestItemResponse
	if err := json.Unmarshal(body, &ad); err != nil {
		t.Fatalf("decode: %v (body=%s)", err, body)
	}
	status, body = request(t, app, http.MethodDelete, "/api/v1/friends/requests/"+ad.ID, "", bobToken)
	if status != http.StatusNotFound {
		t.Fatalf("non-sender cancel: status = %d, want 404 (body=%s)", status, body)
	}
	status, body = request(t, app, http.MethodDelete, "/api/v1/friends/requests/"+ad.ID, "", aliceToken)
	if status != http.StatusNoContent {
		t.Fatalf("cancel: status = %d, want 204 (body=%s)", status, body)
	}
}

func TestNotificationsAPI(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)
	const alice, bob = "did:plc:alice", "did:plc:bob"
	env.CreateUser(t, alice, "alice.test")
	env.CreateUser(t, bob, "bob.test")
	aliceToken := env.Login(t, alice)
	bobToken := env.Login(t, bob)

	now := env.Clock.Now().UnixMilli()
	a, b := pair(alice, bob)
	execSQL(t, env, `INSERT INTO encounters (id, user_a, user_b, established_at, first_seen_at, last_seen_at, visibility)
		VALUES ('enc-ab', ?, ?, ?, ?, ?, 'revealed')`, a, b, now, now, now)

	status, body := request(t, app, http.MethodPost, "/api/v1/friends/requests",
		`{"did":"`+bob+`","via":"encounter","viaRef":"enc-ab"}`, aliceToken)
	if status != http.StatusCreated {
		t.Fatalf("create request: status = %d (body=%s)", status, body)
	}

	type notificationItem struct {
		ID   string `json:"id"`
		Type string `json:"type"`
		Data struct {
			Text  string `json:"text"`
			Actor struct {
				DID string `json:"did"`
			} `json:"actor"`
		} `json:"data"`
		CreatedAt time.Time  `json:"createdAt"`
		ReadAt    *time.Time `json:"readAt"`
	}
	type notificationsResponse struct {
		Items  []notificationItem `json:"items"`
		Cursor string             `json:"cursor"`
	}

	status, body = request(t, app, http.MethodGet, "/api/v1/notifications", "", bobToken)
	if status != http.StatusOK {
		t.Fatalf("list: status = %d (body=%s)", status, body)
	}
	var page notificationsResponse
	if err := json.Unmarshal(body, &page); err != nil {
		t.Fatalf("decode notifications: %v (body=%s)", err, body)
	}
	if len(page.Items) != 1 {
		t.Fatalf("items = %+v", page.Items)
	}
	n := page.Items[0]
	if n.Type != "friend.request" || n.ReadAt != nil || n.Data.Actor.DID != alice || n.Data.Text == "" {
		t.Fatalf("notification = %+v", n)
	}

	// ids で既読にする。
	status, body = request(t, app, http.MethodPost, "/api/v1/notifications/read",
		`{"ids":["`+n.ID+`"]}`, bobToken)
	if status != http.StatusOK {
		t.Fatalf("read: status = %d (body=%s)", status, body)
	}
	var readResult struct {
		Updated int64 `json:"updated"`
	}
	if err := json.Unmarshal(body, &readResult); err != nil {
		t.Fatalf("decode read: %v (body=%s)", err, body)
	}
	if readResult.Updated != 1 {
		t.Fatalf("updated = %d, want 1", readResult.Updated)
	}
	status, body = request(t, app, http.MethodGet, "/api/v1/notifications", "", bobToken)
	page = notificationsResponse{}
	if err := json.Unmarshal(body, &page); err != nil {
		t.Fatalf("decode after read: %v (body=%s)", err, body)
	}
	if len(page.Items) != 1 || page.Items[0].ReadAt == nil {
		t.Fatalf("after read = %+v", page.Items)
	}

	// どちらも指定しなければ 400。
	status, body = request(t, app, http.MethodPost, "/api/v1/notifications/read", `{}`, bobToken)
	if status != http.StatusBadRequest {
		t.Fatalf("empty read: status = %d, want 400 (body=%s)", status, body)
	}
	e := decodeError(t, body)
	if e.Error.Details.Fields["ids"] == "" {
		t.Fatalf("details.fields.ids is empty (body=%s)", body)
	}

	// 自分の通知がないユーザーは空。
	status, body = request(t, app, http.MethodGet, "/api/v1/notifications", "", aliceToken)
	if status != http.StatusOK {
		t.Fatalf("alice notifications: status = %d (body=%s)", status, body)
	}
	page = notificationsResponse{}
	if err := json.Unmarshal(body, &page); err != nil {
		t.Fatalf("decode alice notifications: %v (body=%s)", err, body)
	}
	if len(page.Items) != 0 {
		t.Fatalf("alice items = %+v", page.Items)
	}

	// 不正な limit は 400。
	status, body = request(t, app, http.MethodGet, "/api/v1/notifications?limit=abc", "", bobToken)
	if status != http.StatusBadRequest {
		t.Fatalf("bad limit: status = %d, want 400 (body=%s)", status, body)
	}
}
