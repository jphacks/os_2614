//go:build integration

package friend_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/applemango/chain/internal/httpx"
	core "github.com/applemango/chain/internal/service/core"
	encountersvc "github.com/applemango/chain/internal/service/encounter"
	friendsvc "github.com/applemango/chain/internal/service/friend"
	"github.com/applemango/chain/internal/service/notify"
	"github.com/applemango/chain/internal/service/proximity"
	"github.com/applemango/chain/internal/testutil"
)

const (
	alice = "did:plc:alice"
	bob   = "did:plc:bob"
	carol = "did:plc:carol"
	dave  = "did:plc:dave"
	erin  = "did:plc:erin"
)

type friendHarness struct {
	t             *testing.T
	env           *testutil.Env
	friends       *friendsvc.FriendService
	notifications *notify.NotificationService
	encounters    *encountersvc.EncounterService
	broker        *testutil.FakeBroker
}

func newFriendHarness(t *testing.T) *friendHarness {
	t.Helper()
	env := testutil.NewEnv(t)
	broker := testutil.NewFakeBroker()
	return &friendHarness{
		t:             t,
		env:           env,
		friends:       &friendsvc.FriendService{Deps: env.Deps, Broker: broker},
		notifications: &notify.NotificationService{Deps: env.Deps},
		encounters:    &encountersvc.EncounterService{Deps: env.Deps, Proximity: &proximity.BLEPeerProximity{Deps: env.Deps}, Broker: broker},
		broker:        broker,
	}
}

func (h *friendHarness) users(dids ...string) {
	h.t.Helper()
	for _, did := range dids {
		h.env.CreateUser(h.t, did, strings.TrimPrefix(did, "did:plc:")+".test")
	}
}

func (h *friendHarness) exec(query string, args ...any) {
	h.t.Helper()
	if _, err := h.env.DB.ExecContext(context.Background(), query, args...); err != nil {
		h.t.Fatalf("exec %q: %v", query, err)
	}
}

func (h *friendHarness) count(query string, args ...any) int {
	h.t.Helper()
	var n int
	if err := h.env.DB.QueryRowContext(context.Background(), query, args...).Scan(&n); err != nil {
		h.t.Fatalf("count %q: %v", query, err)
	}
	return n
}

func (h *friendHarness) encounter(id, a, b, visibility string, established time.Time) {
	h.t.Helper()
	if a > b {
		a, b = b, a
	}
	h.exec(`INSERT INTO encounters (id, user_a, user_b, established_at, first_seen_at, last_seen_at, visibility)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, a, b, established.UnixMilli(), established.UnixMilli(), established.UnixMilli(), visibility)
}

func (h *friendHarness) space(id, state string, members ...string) {
	h.t.Helper()
	now := h.env.Clock.Now().UnixMilli()
	h.exec(`INSERT INTO spaces (id, type, authority, ref, name, state, created_by, created_at)
		VALUES (?, 'jp.i32.chain.space.group', 'did:web:chain.test', ?, ?, ?, ?, ?)`,
		id, "at://did:web:chain.test/space/jp.i32.chain.space.group/"+id, "space "+id, state, members[0], now)
	for _, did := range members {
		h.exec(`INSERT INTO space_members (space_id, did, joined_at) VALUES (?, ?, ?)`, id, did, now)
	}
}

func wantAPIError(t *testing.T, err error, status int, code string) {
	t.Helper()
	var e *httpx.Error
	if !errors.As(err, &e) {
		t.Fatalf("err = %v, want *httpx.Error", err)
	}
	if e.Status != status || e.Code != code {
		t.Fatalf("err = %d/%s, want %d/%s (%v)", e.Status, e.Code, status, code, err)
	}
}

func TestFriendInviteFlow(t *testing.T) {
	h := newFriendHarness(t)
	h.users(alice, bob, carol, dave, erin)
	ctx := context.Background()

	inv, err := h.friends.CreateInvite(ctx, alice)
	if err != nil {
		t.Fatalf("CreateInvite: %v", err)
	}
	if len(inv.Code) != 9 || inv.Code[4] != '-' {
		t.Fatalf("code = %q, want XXXX-XXXX", inv.Code)
	}
	if !strings.HasPrefix(inv.URL, "chain://friend/") {
		t.Fatalf("url = %q", inv.URL)
	}
	if want := h.env.Clock.Now().Add(friendsvc.InviteTTL); !inv.ExpiresAt.Equal(want) {
		t.Fatalf("expiresAt = %v, want %v", inv.ExpiresAt, want)
	}
	// DB には SHA-256 だけを保存する。
	if n := h.count("SELECT count(*) FROM friend_invites WHERE code_hash = ?", inv.Code); n != 0 {
		t.Fatalf("raw code is stored in friend_invites")
	}

	// ハイフンなし・小文字でも使える（07 §2.1）。
	raw := strings.ToLower(strings.ReplaceAll(inv.Code, "-", ""))
	friend, err := h.friends.AcceptInvite(ctx, bob, raw)
	if err != nil {
		t.Fatalf("AcceptInvite(lowercase): %v", err)
	}
	if friend.DID != alice || friend.Precision != "area" || friend.Since.IsZero() {
		t.Fatalf("friend = %+v", friend)
	}
	if n := h.count(`SELECT count(*) FROM friendships WHERE user_a = ? AND user_b = ? AND via = 'invite'`,
		minString(alice, bob), maxString(alice, bob)); n != 1 {
		t.Fatalf("friendship via invite = %d, want 1", n)
	}

	// 作成者に friend.accepted の通知と outbox が積まれる（12 §4）。
	if n := h.count(`SELECT count(*) FROM notifications WHERE did = ? AND type = 'friend.accepted'`, alice); n != 1 {
		t.Fatalf("notifications for inviter = %d, want 1", n)
	}
	if n := h.count(`SELECT count(*) FROM notification_outbox o
		JOIN notifications n ON n.id = o.notification_id
		WHERE n.did = ? AND n.type = 'friend.accepted' AND o.status = 'queued'`, alice); n != 1 {
		t.Fatalf("queued outbox for inviter = %d, want 1", n)
	}
	var payload struct {
		Text  string           `json:"text"`
		Actor core.UserSummary `json:"actor"`
	}
	var payloadJSON string
	if err := h.env.DB.QueryRowContext(ctx,
		`SELECT payload_json FROM notifications WHERE did = ? AND type = 'friend.accepted'`, alice).Scan(&payloadJSON); err != nil {
		t.Fatalf("payload: %v", err)
	}
	if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
		t.Fatalf("payload JSON: %v", err)
	}
	if payload.Actor.DID != bob || !strings.Contains(payload.Text, "bob.test") {
		t.Fatalf("payload = %+v", payload)
	}

	// 両者に friend.accepted が届く。
	for _, did := range []string{alice, bob} {
		events := h.broker.EventsFor("friend.accepted", did)
		if len(events) != 1 {
			t.Fatalf("friend.accepted for %s = %d events, want 1", did, len(events))
		}
		sum, ok := events[0].Data.(*core.UserSummary)
		if !ok {
			t.Fatalf("event data = %T, want *core.UserSummary", events[0].Data)
		}
		wantPeer := alice
		if did == alice {
			wantPeer = bob
		}
		if sum.DID != wantPeer {
			t.Fatalf("event peer for %s = %s, want %s", did, sum.DID, wantPeer)
		}
	}

	// 使用済みは 410。
	if _, err := h.friends.AcceptInvite(ctx, bob, inv.Code); err == nil {
		t.Fatal("used invite succeeded")
	} else {
		wantAPIError(t, err, 410, "gone")
	}

	// 既にフレンドは 409。
	inv2, err := h.friends.CreateInvite(ctx, alice)
	if err != nil {
		t.Fatalf("CreateInvite: %v", err)
	}
	if _, err := h.friends.AcceptInvite(ctx, bob, inv2.Code); err == nil {
		t.Fatal("already-friend invite succeeded")
	} else {
		wantAPIError(t, err, 409, "conflict")
	}

	// 自分のコードは 400。
	if _, err := h.friends.AcceptInvite(ctx, alice, inv2.Code); err == nil {
		t.Fatal("own invite succeeded")
	} else {
		wantAPIError(t, err, 400, "invalid_request")
	}

	// ブロック関係は 403。
	inv3, err := h.friends.CreateInvite(ctx, alice)
	if err != nil {
		t.Fatalf("CreateInvite: %v", err)
	}
	h.exec("INSERT INTO blocks (blocker, blocked, created_at) VALUES (?, ?, ?)",
		carol, alice, h.env.Clock.Now().UnixMilli())
	if _, err := h.friends.AcceptInvite(ctx, carol, inv3.Code); err == nil {
		t.Fatal("blocked invite succeeded")
	} else {
		wantAPIError(t, err, 403, "blocked")
	}

	// 期限切れは 410。
	inv4, err := h.friends.CreateInvite(ctx, alice)
	if err != nil {
		t.Fatalf("CreateInvite: %v", err)
	}
	h.env.Clock.Advance(friendsvc.InviteTTL + time.Second)
	if _, err := h.friends.AcceptInvite(ctx, dave, inv4.Code); err == nil {
		t.Fatal("expired invite succeeded")
	} else {
		wantAPIError(t, err, 410, "gone")
	}

	// 新しく作ると古いコードは失効する。
	inv5, err := h.friends.CreateInvite(ctx, alice)
	if err != nil {
		t.Fatalf("CreateInvite: %v", err)
	}
	inv6, err := h.friends.CreateInvite(ctx, alice)
	if err != nil {
		t.Fatalf("CreateInvite: %v", err)
	}
	if _, err := h.friends.AcceptInvite(ctx, dave, inv5.Code); err == nil {
		t.Fatal("old invite succeeded after creating a new one")
	} else {
		wantAPIError(t, err, 410, "gone")
	}
	if _, err := h.friends.AcceptInvite(ctx, dave, inv6.Code); err != nil {
		t.Fatalf("newest invite: %v", err)
	}

	// 自分のフレンド一覧に相手が出る。
	list, err := h.friends.List(ctx, alice)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("friends = %d, want 2 (%+v)", len(list), list)
	}
}

func TestFriendRequestViaEncounter(t *testing.T) {
	h := newFriendHarness(t)
	h.users(alice, bob, carol, dave)
	ctx := context.Background()
	now := h.env.Clock.Now()

	h.encounter("enc-ab", alice, bob, "revealed", now)
	h.encounter("enc-cb", bob, carol, "revealed", now)

	// 関係のない相手への申請は 403（07 §2）。
	if _, err := h.friends.CreateRequest(ctx, alice, friendsvc.CreateRequestInput{
		DID: carol, Via: "encounter", ViaRef: "enc-cb",
	}); err == nil {
		t.Fatal("unrelated request succeeded")
	} else {
		wantAPIError(t, err, 403, "forbidden")
	}
	// pending の encounter では申請できない。
	h.encounter("enc-pending", alice, dave, "pending", now)
	if _, err := h.friends.CreateRequest(ctx, alice, friendsvc.CreateRequestInput{
		DID: dave, Via: "encounter", ViaRef: "enc-pending",
	}); err == nil {
		t.Fatal("pending encounter request succeeded")
	} else {
		wantAPIError(t, err, 403, "forbidden")
	}

	// viaRef が別の組の encounter でも 403。
	if _, err := h.friends.CreateRequest(ctx, alice, friendsvc.CreateRequestInput{
		DID: bob, Via: "encounter", ViaRef: "enc-cb",
	}); err == nil {
		t.Fatal("mismatched encounter ref succeeded")
	} else {
		wantAPIError(t, err, 403, "forbidden")
	}

	req, err := h.friends.CreateRequest(ctx, alice, friendsvc.CreateRequestInput{
		DID: bob, Via: "encounter", ViaRef: "enc-ab",
	})
	if err != nil {
		t.Fatalf("CreateRequest: %v", err)
	}
	if req.Status != "pending" || req.Peer.DID != bob || req.Via != "encounter" || req.ViaRef != "enc-ab" {
		t.Fatalf("request = %+v", req)
	}

	// 受信者に friend.request の通知と outbox、WS イベントが届く。
	if n := h.count(`SELECT count(*) FROM notifications WHERE did = ? AND type = 'friend.request'`, bob); n != 1 {
		t.Fatalf("notifications for recipient = %d, want 1", n)
	}
	if n := h.count(`SELECT count(*) FROM notification_outbox o
		JOIN notifications n ON n.id = o.notification_id
		WHERE n.did = ? AND n.type = 'friend.request' AND o.status = 'queued'`, bob); n != 1 {
		t.Fatalf("queued outbox for recipient = %d, want 1", n)
	}
	if n := len(h.broker.EventsFor("friend.request", bob)); n != 1 {
		t.Fatalf("friend.request events for recipient = %d, want 1", n)
	}
	if n := len(h.broker.EventsFor("friend.changed", alice)); n != 1 {
		t.Fatalf("friend.changed events for sender = %d, want 1", n)
	}
	// 通知の WS イベントはコミット後に宛先へ送られる。
	notes := h.broker.EventsFor("notification", bob)
	if len(notes) != 1 {
		t.Fatalf("notification events = %d, want 1", len(notes))
	}
	if item, ok := notes[0].Data.(notify.NotificationItem); !ok || item.Type != "friend.request" {
		t.Fatalf("notification event data = %#v", notes[0].Data)
	}

	// 一覧（07 §2.3）。
	in, err := h.friends.ListRequests(ctx, bob, "incoming")
	if err != nil {
		t.Fatalf("ListRequests(incoming): %v", err)
	}
	if len(in) != 1 || in[0].ID != req.ID || in[0].Peer.DID != alice {
		t.Fatalf("incoming = %+v", in)
	}
	out, err := h.friends.ListRequests(ctx, alice, "outgoing")
	if err != nil {
		t.Fatalf("ListRequests(outgoing): %v", err)
	}
	if len(out) != 1 || out[0].ID != req.ID {
		t.Fatalf("outgoing = %+v", out)
	}
	if _, err := h.friends.ListRequests(ctx, alice, "sideways"); err == nil {
		t.Fatal("invalid box succeeded")
	} else {
		wantAPIError(t, err, 400, "invalid_request")
	}

	// 受信者以外の accept、送信者以外の取消は 404。
	if _, err := h.friends.AcceptRequest(ctx, carol, req.ID); err == nil {
		t.Fatal("non-recipient accept succeeded")
	} else {
		wantAPIError(t, err, 404, "not_found")
	}
	if err := h.friends.CancelRequest(ctx, bob, req.ID); err == nil {
		t.Fatal("non-sender cancel succeeded")
	} else {
		wantAPIError(t, err, 404, "not_found")
	}

	// 承認すると両者に friend.accepted、申請者に通知。
	friend, err := h.friends.AcceptRequest(ctx, bob, req.ID)
	if err != nil {
		t.Fatalf("AcceptRequest: %v", err)
	}
	if friend.DID != alice {
		t.Fatalf("friend = %+v", friend)
	}
	for _, did := range []string{alice, bob} {
		if n := len(h.broker.EventsFor("friend.accepted", did)); n != 1 {
			t.Fatalf("friend.accepted for %s = %d, want 1", did, n)
		}
	}
	if n := h.count(`SELECT count(*) FROM notifications WHERE did = ? AND type = 'friend.accepted'`, alice); n != 1 {
		t.Fatalf("accepted notifications for requester = %d, want 1", n)
	}

	// 二重の承認は 409。
	if _, err := h.friends.AcceptRequest(ctx, bob, req.ID); err == nil {
		t.Fatal("double accept succeeded")
	} else {
		wantAPIError(t, err, 409, "conflict")
	}

	// 削除で双方向の location_shares も消え、両者に friend.changed（07 §2.3）。
	nowMS := h.env.Clock.Now().UnixMilli()
	h.exec(`INSERT INTO location_shares (owner, viewer, precision, updated_at) VALUES (?, ?, 'exact', ?)
		ON CONFLICT (owner, viewer) DO UPDATE SET precision = excluded.precision, updated_at = excluded.updated_at`, alice, bob, nowMS)
	h.exec(`INSERT INTO location_shares (owner, viewer, precision, updated_at) VALUES (?, ?, 'area', ?)
		ON CONFLICT (owner, viewer) DO UPDATE SET precision = excluded.precision, updated_at = excluded.updated_at`, bob, alice, nowMS)
	h.broker.Reset()
	if err := h.friends.Delete(ctx, alice, bob); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if n := h.count("SELECT count(*) FROM location_shares WHERE (owner = ? AND viewer = ?) OR (owner = ? AND viewer = ?)",
		alice, bob, bob, alice); n != 0 {
		t.Fatalf("location_shares = %d, want 0", n)
	}
	if n := h.count("SELECT count(*) FROM friendships WHERE user_a = ? AND user_b = ?",
		minString(alice, bob), maxString(alice, bob)); n != 0 {
		t.Fatalf("friendship = %d, want 0", n)
	}
	for _, did := range []string{alice, bob} {
		if n := len(h.broker.EventsFor("friend.changed", did)); n != 1 {
			t.Fatalf("friend.changed for %s = %d, want 1", did, n)
		}
	}
	if err := h.friends.Delete(ctx, alice, bob); err == nil {
		t.Fatal("second delete succeeded")
	} else {
		wantAPIError(t, err, 404, "not_found")
	}
}

func TestFriendRequestViaSpace(t *testing.T) {
	h := newFriendHarness(t)
	h.users(alice, bob, carol, dave, erin)
	ctx := context.Background()

	h.space("space-active", "active", alice, bob)
	h.space("space-archived", "archived", alice, carol)
	h.space("space-deleted", "deleted", alice, dave)

	if _, err := h.friends.CreateRequest(ctx, alice, friendsvc.CreateRequestInput{
		DID: bob, Via: "space", ViaRef: "space-active",
	}); err != nil {
		t.Fatalf("active space request: %v", err)
	}
	if _, err := h.friends.CreateRequest(ctx, alice, friendsvc.CreateRequestInput{
		DID: carol, Via: "space", ViaRef: "space-archived",
	}); err != nil {
		t.Fatalf("archived space request: %v", err)
	}
	for _, tc := range []struct {
		name string
		did  string
		ref  string
	}{
		{"deleted space", dave, "space-deleted"},
		{"not a member", erin, "space-active"},
		{"unknown space", erin, "space-unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := h.friends.CreateRequest(ctx, alice, friendsvc.CreateRequestInput{
				DID: tc.did, Via: "space", ViaRef: tc.ref,
			})
			if err == nil {
				t.Fatal("request succeeded, want 403")
			}
			wantAPIError(t, err, 403, "forbidden")
		})
	}
}

func TestFriendRequestAutoAccept(t *testing.T) {
	h := newFriendHarness(t)
	h.users(carol, dave)
	ctx := context.Background()
	now := h.env.Clock.Now()
	h.encounter("enc-cd", carol, dave, "revealed", now)

	first, err := h.friends.CreateRequest(ctx, carol, friendsvc.CreateRequestInput{
		DID: dave, Via: "encounter", ViaRef: "enc-cd",
	})
	if err != nil {
		t.Fatalf("first request: %v", err)
	}

	// 逆向きの pending があれば、新しい申請は作らずに承認する（07 §2.2）。
	second, err := h.friends.CreateRequest(ctx, dave, friendsvc.CreateRequestInput{
		DID: carol, Via: "encounter", ViaRef: "enc-cd",
	})
	if err != nil {
		t.Fatalf("reverse request: %v", err)
	}
	if second.Status != "accepted" || second.ID != first.ID {
		t.Fatalf("auto accept = %+v, want accepted request %s", second, first.ID)
	}
	if n := h.count("SELECT count(*) FROM friendships WHERE user_a = ? AND user_b = ?",
		minString(carol, dave), maxString(carol, dave)); n != 1 {
		t.Fatalf("friendship = %d, want 1", n)
	}
	if n := h.count("SELECT count(*) FROM friend_requests WHERE status = 'pending'"); n != 0 {
		t.Fatalf("pending requests = %d, want 0", n)
	}
	// 元の申請者（carol）に friend.accepted が通知される。
	if n := h.count(`SELECT count(*) FROM notifications WHERE did = ? AND type = 'friend.accepted'`, carol); n != 1 {
		t.Fatalf("accepted notification = %d, want 1", n)
	}
}

func TestFriendRequestDeclineCooldown(t *testing.T) {
	h := newFriendHarness(t)
	h.users(alice, bob, carol)
	ctx := context.Background()
	now := h.env.Clock.Now()
	h.encounter("enc-ab", alice, bob, "revealed", now)

	req, err := h.friends.CreateRequest(ctx, alice, friendsvc.CreateRequestInput{
		DID: bob, Via: "encounter", ViaRef: "enc-ab",
	})
	if err != nil {
		t.Fatalf("CreateRequest: %v", err)
	}

	// 受信者以外は拒否できない。
	if err := h.friends.DeclineRequest(ctx, carol, req.ID); err == nil {
		t.Fatal("non-recipient decline succeeded")
	} else {
		wantAPIError(t, err, 404, "not_found")
	}
	h.broker.Reset()
	if err := h.friends.DeclineRequest(ctx, bob, req.ID); err != nil {
		t.Fatalf("DeclineRequest: %v", err)
	}
	if n := len(h.broker.EventsFor("friend.changed", alice)); n != 1 {
		t.Fatalf("friend.changed for sender = %d, want 1", n)
	}

	// declined から 7 日間は再申請できない。
	_, err = h.friends.CreateRequest(ctx, alice, friendsvc.CreateRequestInput{
		DID: bob, Via: "encounter", ViaRef: "enc-ab",
	})
	if err == nil {
		t.Fatal("re-request within 7 days succeeded")
	}
	wantAPIError(t, err, 409, "conflict")

	h.env.Clock.Advance(friendsvc.FriendRequestCooldown + time.Second)
	if _, err := h.friends.CreateRequest(ctx, alice, friendsvc.CreateRequestInput{
		DID: bob, Via: "encounter", ViaRef: "enc-ab",
	}); err != nil {
		t.Fatalf("re-request after 7 days: %v", err)
	}
}

func TestFriendLimit(t *testing.T) {
	h := newFriendHarness(t)
	h.users(alice, bob)
	ctx := context.Background()
	now := h.env.Clock.Now()
	h.encounter("enc-ab", alice, bob, "revealed", now)

	// alice を 500 人にする。
	tx, err := h.env.DB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	nowMS := now.UnixMilli()
	for i := 0; i < friendsvc.MaxFriends; i++ {
		did := "did:plc:bulk" + pad(i)
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO users (did, handle, pds_url, created_at) VALUES (?, ?, 'https://pds.example', ?)",
			did, "bulk"+pad(i)+".test", nowMS); err != nil {
			t.Fatalf("insert bulk user: %v", err)
		}
		a, b := minString(alice, did), maxString(alice, did)
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO friendships (user_a, user_b, created_at, via) VALUES (?, ?, ?, 'encounter')",
			a, b, nowMS); err != nil {
			t.Fatalf("insert bulk friendship: %v", err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	req, err := h.friends.CreateRequest(ctx, bob, friendsvc.CreateRequestInput{
		DID: alice, Via: "encounter", ViaRef: "enc-ab",
	})
	if err != nil {
		t.Fatalf("CreateRequest: %v", err)
	}
	if _, err := h.friends.AcceptRequest(ctx, alice, req.ID); err == nil {
		t.Fatal("accept over the friend limit succeeded")
	} else {
		wantAPIError(t, err, 409, "conflict")
	}

	// 招待コードでも上限を超えられない。
	h.users(carol)
	inv, err := h.friends.CreateInvite(ctx, bob)
	if err != nil {
		t.Fatalf("CreateInvite: %v", err)
	}
	if _, err := h.friends.AcceptInvite(ctx, alice, inv.Code); err == nil {
		t.Fatal("invite over the friend limit succeeded")
	} else {
		wantAPIError(t, err, 409, "conflict")
	}
}

func TestNotificationPreferenceAndRollback(t *testing.T) {
	h := newFriendHarness(t)
	h.users(alice, bob, carol)
	ctx := context.Background()
	now := h.env.Clock.Now()
	h.encounter("enc-ab", alice, bob, "revealed", now)

	// notify_json で off なら積まない（12 §3）。
	h.exec(`UPDATE user_settings SET notify_json = '{"friend.request":false}' WHERE did = ?`, bob)
	if _, err := h.friends.CreateRequest(ctx, alice, friendsvc.CreateRequestInput{
		DID: bob, Via: "encounter", ViaRef: "enc-ab",
	}); err != nil {
		t.Fatalf("CreateRequest: %v", err)
	}
	if n := h.count("SELECT count(*) FROM notifications WHERE did = ?", bob); n != 0 {
		t.Fatalf("notifications = %d, want 0", n)
	}
	if n := h.count(`SELECT count(*) FROM notification_outbox o
		JOIN notifications n ON n.id = o.notification_id WHERE n.did = ?`, bob); n != 0 {
		t.Fatalf("outbox = %d, want 0", n)
	}
	// 通知が off でも WS の friend.request は届く（10 §7.3）。
	if n := len(h.broker.EventsFor("friend.request", bob)); n != 1 {
		t.Fatalf("friend.request events = %d, want 1", n)
	}

	// 業務処理が失敗したら通知も残らない。
	before := h.count("SELECT count(*) FROM notifications")
	_, err := h.friends.CreateRequest(ctx, alice, friendsvc.CreateRequestInput{
		DID: carol, Via: "encounter", ViaRef: "enc-ab",
	})
	if err == nil {
		t.Fatal("unrelated request succeeded")
	}
	wantAPIError(t, err, 403, "forbidden")
	if after := h.count("SELECT count(*) FROM notifications"); after != before {
		t.Fatalf("notifications after failed request = %d, want %d", after, before)
	}
}

func TestNotificationListAndRead(t *testing.T) {
	h := newFriendHarness(t)
	h.users(alice, bob, carol, dave)
	ctx := context.Background()
	now := h.env.Clock.Now()
	h.encounter("enc-ab", alice, bob, "revealed", now)
	h.encounter("enc-cb", carol, bob, "revealed", now)
	h.encounter("enc-ad", alice, dave, "revealed", now)

	// bob に 2 件（alice と carol から）、dave に 1 件。
	for _, in := range []struct {
		from string
		to   string
		ref  string
	}{
		{alice, bob, "enc-ab"},
		{carol, bob, "enc-cb"},
		{alice, dave, "enc-ad"},
	} {
		h.env.Clock.Advance(time.Second)
		if _, err := h.friends.CreateRequest(ctx, in.from, friendsvc.CreateRequestInput{
			DID: in.to, Via: "encounter", ViaRef: in.ref,
		}); err != nil {
			t.Fatalf("CreateRequest(%s->%s): %v", in.from, in.to, err)
		}
	}

	page, err := h.notifications.List(ctx, bob, "", 1)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(page.Items) != 1 || page.Cursor == "" {
		t.Fatalf("page = %+v, want 1 item and a cursor", page)
	}
	first := page.Items[0]
	if first.Type != "friend.request" || first.ReadAt != nil {
		t.Fatalf("item = %+v", first)
	}

	next, err := h.notifications.List(ctx, bob, page.Cursor, 10)
	if err != nil {
		t.Fatalf("List(cursor): %v", err)
	}
	if len(next.Items) != 1 || next.Cursor != "" {
		t.Fatalf("second page = %+v, want 1 item and no cursor", next)
	}
	if next.Items[0].ID == first.ID {
		t.Fatal("second page repeated the first item")
	}

	if _, err := h.notifications.List(ctx, bob, "!!bad!!", 10); err == nil {
		t.Fatal("bad cursor succeeded")
	} else {
		wantAPIError(t, err, 400, "invalid_request")
	}

	updated, err := h.notifications.MarkRead(ctx, bob, []string{first.ID}, false)
	if err != nil {
		t.Fatalf("MarkRead: %v", err)
	}
	if updated != 1 {
		t.Fatalf("updated = %d, want 1", updated)
	}
	page, err = h.notifications.List(ctx, bob, "", 10)
	if err != nil {
		t.Fatalf("List after read: %v", err)
	}
	if page.Items[0].ReadAt == nil {
		t.Fatal("readAt is nil after MarkRead")
	}
	// 他のユーザーの通知は既読にならない。
	if n := h.count("SELECT count(*) FROM notifications WHERE did = ? AND read_at IS NULL", carol); n != 0 {
		t.Fatalf("carol unread = %d, want 0", n)
	}

	// 未読が残っている状態で all。bob の 2 件目が既読になる。
	updated, err = h.notifications.MarkRead(ctx, bob, nil, true)
	if err != nil {
		t.Fatalf("MarkRead(all): %v", err)
	}
	if updated != 1 {
		t.Fatalf("updated(all) = %d, want 1", updated)
	}
	if n := h.count("SELECT count(*) FROM notifications WHERE did = ? AND read_at IS NULL", bob); n != 0 {
		t.Fatalf("bob unread = %d, want 0", n)
	}
}

func TestEncounterRevealedEvents(t *testing.T) {
	h := newFriendHarness(t)
	h.users(alice, bob)
	ctx := context.Background()
	now := h.env.Clock.Now()

	// 公開時刻を過ぎた pending の encounter。
	h.exec(`INSERT INTO encounters (id, user_a, user_b, established_at, first_seen_at, last_seen_at, visibility, reveal_at)
		VALUES ('enc-reveal', ?, ?, ?, ?, ?, 'pending', ?)`,
		minString(alice, bob), maxString(alice, bob),
		now.Add(-time.Hour).UnixMilli(), now.Add(-time.Hour).UnixMilli(), now.Add(-time.Hour).UnixMilli(),
		now.Add(-time.Minute).UnixMilli())

	n, err := h.encounters.RevealPending(ctx)
	if err != nil {
		t.Fatalf("RevealPending: %v", err)
	}
	if n != 1 {
		t.Fatalf("revealed = %d, want 1", n)
	}

	for _, viewer := range []string{alice, bob} {
		events := h.broker.EventsFor("encounter.revealed", viewer)
		if len(events) != 1 {
			t.Fatalf("encounter.revealed for %s = %d events, want 1", viewer, len(events))
		}
		item, ok := events[0].Data.(*encountersvc.EncounterItem)
		if !ok {
			t.Fatalf("event data = %T, want *encountersvc.EncounterItem", events[0].Data)
		}
		wantPeer := alice
		wantHandle := "alice.test"
		if viewer == alice {
			wantPeer = bob
			wantHandle = "bob.test"
		}
		if item.ID != "enc-reveal" || item.Peer.DID != wantPeer || item.Peer.Handle != wantHandle {
			t.Fatalf("item for %s = %+v", viewer, item)
		}
		if item.TimesWithPeer != 1 || item.Relation != "none" {
			t.Fatalf("item for %s = %+v", viewer, item)
		}
		if !item.SeenFrom.Equal(now.Add(-time.Hour).Truncate(core.TimeRounding)) {
			t.Fatalf("seenFrom = %v", item.SeenFrom)
		}
	}

	// 2 回目は何も起きない。
	h.broker.Reset()
	if n, err := h.encounters.RevealPending(ctx); err != nil || n != 0 {
		t.Fatalf("second RevealPending = %d, %v", n, err)
	}
	if len(h.broker.Events()) != 0 {
		t.Fatalf("events after second reveal = %d, want 0", len(h.broker.Events()))
	}
}

func minString(a, b string) string {
	if a < b {
		return a
	}
	return b
}

func maxString(a, b string) string {
	if a > b {
		return a
	}
	return b
}

func pad(i int) string { return fmt.Sprintf("%03d", i) }
