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

	"github.com/applemango/chain/internal/external/atproto"
	"github.com/applemango/chain/internal/httpx"
	"github.com/applemango/chain/internal/service"
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
	sids          map[string]string
}

func newFriendHarness(t *testing.T) *friendHarness {
	t.Helper()
	env := testutil.NewEnv(t)
	broker := testutil.NewFakeBroker()
	env.Broker = broker
	return &friendHarness{
		t:             t,
		env:           env,
		friends:       env.Friends(),
		notifications: &notify.NotificationService{Deps: env.Deps},
		encounters:    &encountersvc.EncounterService{Deps: env.Deps, Proximity: &proximity.BLEPeerProximity{Deps: env.Deps}, Broker: broker},
		broker:        broker,
		sids:          map[string]string{},
	}
}

func (h *friendHarness) users(dids ...string) {
	h.t.Helper()
	for _, did := range dids {
		h.env.CreateUser(h.t, did, strings.TrimPrefix(did, "did:plc:")+".test")
	}
}

// sid は did のアプリセッション ID を返す（無ければログインする）。フォローはこのセッションで書かれる。
func (h *friendHarness) sid(did string) string {
	h.t.Helper()
	if sid, ok := h.sids[did]; ok {
		return sid
	}
	h.env.Login(h.t, did)
	h.sids[did] = testutil.SessionID(h.t, h.env, did)
	return h.sids[did]
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

// bskyFollow は Bluesky のアプリでのフォローを再現する: PDS の repo に record を置き、Jetstream のイベントを流す。
func (h *friendHarness) bskyFollow(author, subject, rkey string) string {
	h.t.Helper()
	rec := map[string]any{
		"$type": atproto.CollectionBskyFollow, "subject": subject,
		"createdAt": h.env.Clock.Now().UTC().Format(time.RFC3339),
	}
	uri := h.env.Writer.SeedRepoRecord(author, atproto.CollectionBskyFollow, rkey, rec)
	h.commit(author, atproto.CollectionBskyFollow, "create", rkey, rec)
	return uri
}

func (h *friendHarness) bskyUnfollow(author, rkey string) {
	h.t.Helper()
	h.env.Writer.RemoveRepoRecord(author, atproto.GraphURI(author, atproto.CollectionBskyFollow, rkey))
	h.commit(author, atproto.CollectionBskyFollow, "delete", rkey, nil)
}

func (h *friendHarness) commit(did, collection, op, rkey string, rec map[string]any) {
	h.t.Helper()
	c := &atproto.JetstreamCommit{Operation: op, Collection: collection, RKey: rkey}
	if rec != nil {
		b, err := json.Marshal(rec)
		if err != nil {
			h.t.Fatalf("marshal: %v", err)
		}
		c.Record = b
	}
	if err := h.friends.HandleGraphCommit(context.Background(), did, c); err != nil {
		h.t.Fatalf("HandleGraphCommit(%s %s): %v", op, collection, err)
	}
}

func (h *friendHarness) met(a, b string) bool {
	h.t.Helper()
	if a > b {
		a, b = b, a
	}
	return h.count("SELECT count(*) FROM friendships WHERE user_a = ? AND user_b = ?", a, b) == 1
}

func (h *friendHarness) follows(author, subject string) bool {
	h.t.Helper()
	return h.count("SELECT count(*) FROM follows WHERE author_did = ? AND subject_did = ?", author, subject) == 1
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
	h.sid(alice)

	inv, err := h.friends.CreateInvite(ctx, alice)
	if err != nil {
		t.Fatalf("CreateInvite: %v", err)
	}
	if len(inv.Code) != 9 || inv.Code[4] != '-' {
		t.Fatalf("code = %q, want XXXX-XXXX", inv.Code)
	}
	if want := h.env.Clock.Now().Add(friendsvc.InviteTTL); !inv.ExpiresAt.Equal(want) {
		t.Fatalf("expiresAt = %v, want %v", inv.ExpiresAt, want)
	}
	// DB には SHA-256 だけを保存する。
	if n := h.count("SELECT count(*) FROM friend_invites WHERE code_hash = ?", inv.Code); n != 0 {
		t.Fatalf("raw code is stored in friend_invites")
	}

	// ハイフンなし・小文字でも使える。両者のフォローが、それぞれのセッションで書かれる（07 §2.1）。
	raw := strings.ToLower(strings.ReplaceAll(inv.Code, "-", ""))
	friend, err := h.friends.AcceptInvite(ctx, bob, h.sid(bob), raw)
	if err != nil {
		t.Fatalf("AcceptInvite(lowercase): %v", err)
	}
	if friend.DID != alice || !friend.Met || friend.Via == nil || *friend.Via != "invite" || friend.Precision != "area" {
		t.Fatalf("friend = %+v", friend)
	}
	calls := h.env.Writer.CreateCalls()
	if len(calls) != 2 || calls[0].DID != bob || calls[1].DID != alice ||
		calls[0].Collection != atproto.CollectionBskyFollow || calls[0].SessionID != "atp-"+bob || calls[1].SessionID != "atp-"+alice {
		t.Fatalf("create calls = %+v", calls)
	}
	if !h.follows(bob, alice) || !h.follows(alice, bob) || !h.met(alice, bob) {
		t.Fatal("follows or friendship missing after accept")
	}
	if n := h.count("SELECT count(*) FROM location_shares WHERE (owner = ? AND viewer = ?) OR (owner = ? AND viewer = ?)", alice, bob, bob, alice); n != 2 {
		t.Fatalf("default location shares = %d, want 2", n)
	}
	if n := len(h.broker.EventsFor(notify.NotificationFriendAccepted, alice)); n != 1 {
		t.Fatalf("friend.accepted for alice = %d, want 1", n)
	}
	if n := len(h.broker.EventsFor("friend.changed", alice)); n != 1 {
		t.Fatalf("friend.changed for alice = %d, want 1", n)
	}

	// 使用済み・自分のコード・ブロック・既に会ったフレンド。
	_, err = h.friends.AcceptInvite(ctx, bob, h.sid(bob), inv.Code)
	wantAPIError(t, err, 410, "gone")
	_, err = h.friends.AcceptInvite(ctx, bob, h.sid(bob), "nope")
	wantAPIError(t, err, 410, "gone")
	own, _ := h.friends.CreateInvite(ctx, alice)
	_, err = h.friends.AcceptInvite(ctx, alice, h.sid(alice), own.Code)
	wantAPIError(t, err, 400, "invalid_request")
	_, err = h.friends.AcceptInvite(ctx, bob, h.sid(bob), own.Code)
	wantAPIError(t, err, 409, "conflict")
	h.exec("INSERT INTO blocks (blocker, blocked, created_at) VALUES (?, ?, ?)", carol, alice, h.env.Clock.Now().UnixMilli())
	_, err = h.friends.AcceptInvite(ctx, carol, h.sid(carol), own.Code)
	wantAPIError(t, err, 403, "blocked")

	// 新しいコードを作ると古いコードは失効する。期限切れは 410。
	old, _ := h.friends.CreateInvite(ctx, alice)
	if _, err := h.friends.CreateInvite(ctx, alice); err != nil {
		t.Fatalf("CreateInvite: %v", err)
	}
	_, err = h.friends.AcceptInvite(ctx, dave, h.sid(dave), old.Code)
	wantAPIError(t, err, 410, "gone")
	exp, _ := h.friends.CreateInvite(ctx, erin)
	h.env.Clock.Advance(friendsvc.InviteTTL + time.Minute)
	_, err = h.friends.AcceptInvite(ctx, dave, h.sid(dave), exp.Code)
	wantAPIError(t, err, 410, "gone")
}

func TestFriendInviteCreatorWithoutSession(t *testing.T) {
	h := newFriendHarness(t)
	h.users(alice, bob)
	ctx := context.Background()
	// alice には ATProto セッションが無い（失効している）。
	inv, err := h.friends.CreateInvite(ctx, alice)
	if err != nil {
		t.Fatalf("CreateInvite: %v", err)
	}
	friend, err := h.friends.AcceptInvite(ctx, bob, h.sid(bob), inv.Code)
	if err != nil {
		t.Fatalf("AcceptInvite: %v", err)
	}
	// 読み取った人のフォローだけが書かれ、作成者のフォローは次のログインに回る。会った裏付けは先に記録される。
	if friend.Met || !h.follows(bob, alice) || h.follows(alice, bob) {
		t.Fatalf("friend = %+v, follows(bob->alice)=%v follows(alice->bob)=%v", friend, h.follows(bob, alice), h.follows(alice, bob))
	}
	if n := h.count("SELECT count(*) FROM friend_invites WHERE did = ? AND used_by = ? AND follow_pending = 1", alice, bob); n != 1 {
		t.Fatalf("follow_pending invites = %d, want 1", n)
	}
	if n := len(h.broker.EventsFor(notify.NotificationFriendAccepted, alice)); n != 1 {
		t.Fatalf("friend.accepted for alice = %d, want 1", n)
	}

	// alice の次のログインで、作成者のフォローが書かれて会ったフレンドになる。
	if err := h.friends.SyncOnLogin(ctx, alice, "https://pds.example", "atp-new-alice"); err != nil {
		t.Fatalf("SyncOnLogin: %v", err)
	}
	if !h.follows(alice, bob) || !h.met(alice, bob) {
		t.Fatal("alice's pending follow was not written on login")
	}
	if n := h.count("SELECT count(*) FROM friend_invites WHERE follow_pending = 1"); n != 0 {
		t.Fatalf("follow_pending after login = %d, want 0", n)
	}
	last := h.env.Writer.CreateCalls()
	if got := last[len(last)-1]; got.DID != alice || got.SessionID != "atp-new-alice" {
		t.Fatalf("last create = %+v", got)
	}
}

func TestFriendRequestAcceptFlow(t *testing.T) {
	h := newFriendHarness(t)
	h.users(alice, bob, carol)
	ctx := context.Background()
	h.encounter("enc-ab", alice, bob, "revealed", h.env.Clock.Now())

	// 関係のない相手は 403。scope が足りなければ 403 atproto_reauth_required（17 §2.4）。
	_, err := h.friends.CreateRequest(ctx, alice, h.sid(alice), friendsvc.CreateRequestInput{DID: carol, Via: "encounter", ViaRef: "enc-ab"})
	wantAPIError(t, err, 403, "forbidden")
	h.env.Writer.SetCreateError(atproto.CollectionBskyFollow, atproto.ErrReauthRequired)
	_, err = h.friends.CreateRequest(ctx, alice, h.sid(alice), friendsvc.CreateRequestInput{DID: bob, Via: "encounter", ViaRef: "enc-ab"})
	wantAPIError(t, err, 403, "atproto_reauth_required")
	h.env.Writer.SetCreateError(atproto.CollectionBskyFollow, atproto.ErrSessionExpired)
	_, err = h.friends.CreateRequest(ctx, alice, h.sid(alice), friendsvc.CreateRequestInput{DID: bob, Via: "encounter", ViaRef: "enc-ab"})
	wantAPIError(t, err, 401, "atproto_session_expired")
	h.env.Writer.SetCreateError(atproto.CollectionBskyFollow, nil)
	if h.follows(alice, bob) {
		t.Fatal("a failed PDS write left a follows row")
	}

	// 申請 = 自分のセッションで follow を書く。
	req, err := h.friends.CreateRequest(ctx, alice, h.sid(alice), friendsvc.CreateRequestInput{DID: bob, Via: "encounter", ViaRef: "enc-ab"})
	if err != nil {
		t.Fatalf("CreateRequest: %v", err)
	}
	if req.Status != "pending" || req.Peer.DID != bob || req.ID == "" {
		t.Fatalf("req = %+v", req)
	}
	calls := h.env.Writer.CreateCalls()
	rec, _ := calls[len(calls)-1].Record.(map[string]any)
	if calls[len(calls)-1].DID != alice || rec["subject"] != bob || rec["$type"] != "app.bsky.graph.follow" {
		t.Fatalf("create call = %+v", calls[len(calls)-1])
	}
	_, err = h.friends.CreateRequest(ctx, alice, h.sid(alice), friendsvc.CreateRequestInput{DID: bob, Via: "encounter", ViaRef: "enc-ab"})
	wantAPIError(t, err, 409, "conflict")
	if n := len(h.broker.EventsFor(notify.NotificationFriendRequest, bob)); n != 1 {
		t.Fatalf("friend.request events for bob = %d, want 1", n)
	}
	if n := h.count("SELECT count(*) FROM notifications WHERE did = ? AND type = 'friend.request'", bob); n != 1 {
		t.Fatalf("friend.request notifications = %d, want 1", n)
	}

	in, err := h.friends.ListRequests(ctx, bob, "incoming")
	if err != nil || len(in) != 1 || in[0].ID != req.ID || in[0].Peer.DID != alice || in[0].Via != "encounter" || in[0].ViaRef != "enc-ab" {
		t.Fatalf("incoming = %+v, %v", in, err)
	}
	out, err := h.friends.ListRequests(ctx, alice, "outgoing")
	if err != nil || len(out) != 1 || out[0].ID != req.ID || out[0].Peer.DID != bob {
		t.Fatalf("outgoing = %+v, %v", out, err)
	}
	if list, _ := h.friends.List(ctx, bob); len(list) != 0 {
		t.Fatalf("friends before accept = %+v", list)
	}

	// 受信者以外は承認できない。承認 = フォローを返す。
	_, err = h.friends.AcceptRequest(ctx, carol, h.sid(carol), req.ID)
	wantAPIError(t, err, 404, "not_found")
	friend, err := h.friends.AcceptRequest(ctx, bob, h.sid(bob), req.ID)
	if err != nil {
		t.Fatalf("AcceptRequest: %v", err)
	}
	if !friend.Met || friend.DID != alice || friend.Via == nil || *friend.Via != "encounter" {
		t.Fatalf("friend = %+v", friend)
	}
	if !h.follows(bob, alice) || !h.met(alice, bob) {
		t.Fatal("follow back or friendship missing")
	}
	if n := len(h.broker.EventsFor(notify.NotificationFriendAccepted, alice)); n != 1 {
		t.Fatalf("friend.accepted for alice = %d, want 1", n)
	}
	if in, _ := h.friends.ListRequests(ctx, bob, "incoming"); len(in) != 0 {
		t.Fatalf("incoming after accept = %+v", in)
	}
	_, err = h.friends.AcceptRequest(ctx, bob, h.sid(bob), req.ID)
	wantAPIError(t, err, 409, "conflict")

	// フレンドを削除すると、自分のフォローが消え、位置共有も双方向で消える。相手には通知しない。
	h.broker.Reset()
	if err := h.friends.Delete(ctx, bob, h.sid(bob), alice); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if h.follows(bob, alice) || !h.follows(alice, bob) || h.met(alice, bob) {
		t.Fatal("after Delete: bob's follow should be gone, alice's kept, friendship removed")
	}
	if n := h.count("SELECT count(*) FROM location_shares"); n != 0 {
		t.Fatalf("location_shares after delete = %d, want 0", n)
	}
	if n := len(h.broker.EventsFor("notification", alice)); n != 0 {
		t.Fatalf("notifications to alice after delete = %d, want 0", n)
	}
	if n := len(h.broker.EventsFor("friend.changed", alice)); n == 0 {
		t.Fatal("friend.changed was not sent to alice")
	}
	dels := h.env.Writer.DeleteCalls()
	if len(dels) != 1 || dels[0].DID != bob || dels[0].Collection != atproto.CollectionBskyFollow {
		t.Fatalf("delete calls = %+v", dels)
	}
	wantAPIError(t, h.friends.Delete(ctx, bob, h.sid(bob), alice), 404, "not_found")

	// フォローを付け直して、裏付けが残っていれば会ったフレンドに戻る（07 §2.4）。
	h.bskyFollow(bob, alice, "again")
	if !h.met(alice, bob) {
		t.Fatal("friendship did not come back after re-following")
	}
}

func TestFriendRequestDeclineAndCancel(t *testing.T) {
	h := newFriendHarness(t)
	h.users(alice, bob, carol)
	ctx := context.Background()
	h.encounter("enc-ab", alice, bob, "revealed", h.env.Clock.Now())
	h.space("sp-ac", "active", alice, carol)

	reqAB, err := h.friends.CreateRequest(ctx, alice, h.sid(alice), friendsvc.CreateRequestInput{DID: bob, Via: "encounter", ViaRef: "enc-ab"})
	if err != nil {
		t.Fatalf("CreateRequest: %v", err)
	}
	reqAC, err := h.friends.CreateRequest(ctx, alice, h.sid(alice), friendsvc.CreateRequestInput{DID: carol, Via: "space", ViaRef: "sp-ac"})
	if err != nil {
		t.Fatalf("CreateRequest(space): %v", err)
	}

	// 拒否: 受信箱から消えるが、Bluesky には何も書かず、相手のフォローは残る。
	deletes := len(h.env.Writer.DeleteCalls())
	creates := len(h.env.Writer.CreateCalls())
	wantAPIError(t, h.friends.DeclineRequest(ctx, carol, reqAB.ID), 404, "not_found")
	if err := h.friends.DeclineRequest(ctx, bob, reqAB.ID); err != nil {
		t.Fatalf("DeclineRequest: %v", err)
	}
	if in, _ := h.friends.ListRequests(ctx, bob, "incoming"); len(in) != 0 {
		t.Fatalf("incoming after decline = %+v", in)
	}
	if len(h.env.Writer.DeleteCalls()) != deletes || len(h.env.Writer.CreateCalls()) != creates {
		t.Fatal("decline wrote to the PDS")
	}
	if !h.follows(alice, bob) {
		t.Fatal("decline removed alice's follow")
	}
	if out, _ := h.friends.ListRequests(ctx, alice, "outgoing"); len(out) != 2 {
		t.Fatalf("outgoing = %+v, want both requests (decline is invisible to the sender)", out)
	}
	// 「7 日間は再申請できない」はやめた: 取り消してすぐ申請し直せる。拒否は付け直しで解ける。
	if err := h.friends.CancelRequest(ctx, alice, h.sid(alice), reqAB.ID); err != nil {
		t.Fatalf("CancelRequest: %v", err)
	}
	if _, err := h.friends.CreateRequest(ctx, alice, h.sid(alice), friendsvc.CreateRequestInput{DID: bob, Via: "encounter", ViaRef: "enc-ab"}); err != nil {
		t.Fatalf("re-request: %v", err)
	}
	if in, _ := h.friends.ListRequests(ctx, bob, "incoming"); len(in) != 1 {
		t.Fatalf("incoming after re-request = %+v", in)
	}

	// 取消は送信者だけ。同じ相手へのフォローが PDS に複数あれば全部消す（07 §5）。
	wantAPIError(t, h.friends.CancelRequest(ctx, bob, h.sid(bob), reqAC.ID), 404, "not_found")
	h.env.Writer.SeedRepoRecord(alice, atproto.CollectionBskyFollow, "dup1", map[string]any{"subject": carol, "createdAt": "2026-01-01T00:00:00Z"})
	h.env.Writer.SeedRepoRecord(alice, atproto.CollectionBskyFollow, "dup2", map[string]any{"subject": carol, "createdAt": "2026-01-02T00:00:00Z"})
	before := len(h.env.Writer.DeleteCalls())
	if err := h.friends.CancelRequest(ctx, alice, h.sid(alice), reqAC.ID); err != nil {
		t.Fatalf("CancelRequest: %v", err)
	}
	if got := len(h.env.Writer.DeleteCalls()) - before; got != 3 {
		t.Fatalf("deleted records = %d, want 3 (the indexed one and two duplicates)", got)
	}
	if uris := h.env.Writer.RepoURIs(alice, atproto.CollectionBskyFollow); len(uris) != 1 {
		t.Fatalf("remaining follow records = %v, want only the follow to bob", uris)
	}
	if h.follows(alice, carol) {
		t.Fatal("follows row remains after cancel")
	}
}

func TestMutualFollowWithoutEvidenceIsNotMet(t *testing.T) {
	h := newFriendHarness(t)
	h.users(alice, bob)
	ctx := context.Background()

	// Bluesky のアプリで相互フォロー。会った裏付けが無い間は、一覧には出るが会ったフレンドではない。
	h.bskyFollow(alice, bob, "a1")
	h.bskyFollow(bob, alice, "b1")
	if h.met(alice, bob) {
		t.Fatal("mutual follow without evidence became a met friend")
	}
	list, err := h.friends.List(ctx, alice)
	if err != nil || len(list) != 1 || list[0].DID != bob || list[0].Met || list[0].Via != nil || list[0].Precision != "off" {
		t.Fatalf("list = %+v, %v", list, err)
	}
	if n := h.count("SELECT count(*) FROM location_shares"); n != 0 {
		t.Fatalf("location_shares = %d, want 0", n)
	}
	// 受信箱にも送信箱にも出ない（§2 の関係が無い）。通知も無い。
	for _, box := range []string{"incoming", "outgoing"} {
		if items, _ := h.friends.ListRequests(ctx, alice, box); len(items) != 0 {
			t.Fatalf("%s = %+v", box, items)
		}
	}

	// 裏付けができたら、Sweep（と encounter の公開）で会ったフレンドになる。
	h.encounter("enc-ab", alice, bob, "revealed", h.env.Clock.Now())
	if err := h.env.Reconciler().Sweep(ctx); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if !h.met(alice, bob) {
		t.Fatal("not met after evidence appeared")
	}
	list, _ = h.friends.List(ctx, alice)
	if len(list) != 1 || !list[0].Met || list[0].Via == nil || *list[0].Via != "encounter" || list[0].Precision != "area" {
		t.Fatalf("list after evidence = %+v", list)
	}

	// hidden にしたら外れ、位置共有も消える。
	h.exec("UPDATE encounters SET visibility = 'hidden' WHERE id = 'enc-ab'")
	if err := h.env.Reconciler().Sweep(ctx); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if h.met(alice, bob) || h.count("SELECT count(*) FROM location_shares") != 0 {
		t.Fatal("friendship or location_shares remain after the encounter was hidden")
	}
	if list, _ = h.friends.List(ctx, alice); len(list) != 1 || list[0].Met {
		t.Fatalf("list after hide = %+v", list)
	}
}

func TestJetstreamFollowNotifications(t *testing.T) {
	h := newFriendHarness(t)
	h.users(alice, bob, carol)
	h.encounter("enc-ab", alice, bob, "revealed", h.env.Clock.Now())

	// 裏付けの無い相手からのフォローは受信箱に出さず、通知しない（Bluesky のフォロワーは申請ではない）。
	h.bskyFollow(carol, bob, "c1")
	if n := h.count("SELECT count(*) FROM notifications WHERE did = ?", bob); n != 0 {
		t.Fatalf("notifications for unrelated follower = %d, want 0", n)
	}
	if in, _ := h.friends.ListRequests(context.Background(), bob, "incoming"); len(in) != 0 {
		t.Fatalf("incoming = %+v", in)
	}
	// subject が CHAIN の利用者でないフォローも索引に入る。
	h.bskyFollow(alice, "did:plc:outsider", "o1")
	if !h.follows(alice, "did:plc:outsider") {
		t.Fatal("follow to a non-user was not indexed")
	}

	// 裏付けのある相手からのフォローは通知され、受信箱に出る。同じ event の再配信では二重に通知しない。
	h.bskyFollow(alice, bob, "a1")
	h.bskyFollow(alice, bob, "a1")
	if n := h.count("SELECT count(*) FROM notifications WHERE did = ? AND type = 'friend.request'", bob); n != 1 {
		t.Fatalf("friend.request notifications = %d, want 1", n)
	}
	in, _ := h.friends.ListRequests(context.Background(), bob, "incoming")
	if len(in) != 1 || in[0].Peer.DID != alice {
		t.Fatalf("incoming = %+v", in)
	}

	// フォローを返す（Bluesky のアプリで）と会ったフレンドになり、alice に friend.accepted が行く。
	h.bskyFollow(bob, alice, "b1")
	if !h.met(alice, bob) {
		t.Fatal("not met after the follow back")
	}
	if n := len(h.broker.EventsFor(notify.NotificationFriendAccepted, alice)); n != 1 {
		t.Fatalf("friend.accepted for alice = %d, want 1", n)
	}
	// 外すと関係が戻る。
	h.bskyUnfollow(alice, "a1")
	if h.met(alice, bob) || h.follows(alice, bob) {
		t.Fatal("unfollow did not dissolve the friendship")
	}
}

func TestBlueskyBlock(t *testing.T) {
	h := newFriendHarness(t)
	h.users(alice, bob)
	ctx := context.Background()
	h.encounter("enc-ab", alice, bob, "revealed", h.env.Clock.Now())
	h.bskyFollow(alice, bob, "a1")
	h.bskyFollow(bob, alice, "b1")
	if !h.met(alice, bob) {
		t.Fatal("setup: not met")
	}

	// Bluesky で bob をブロックすると、一覧から消え、位置共有も消える。
	blockRec := map[string]any{"$type": atproto.CollectionBskyBlock, "subject": bob, "createdAt": "2026-10-01T00:00:00Z"}
	h.commit(alice, atproto.CollectionBskyBlock, "create", "k1", blockRec)
	if h.count("SELECT count(*) FROM blocks WHERE blocker = ? AND blocked = ? AND source = 'bsky'", alice, bob) != 1 {
		t.Fatal("bsky block was not stored")
	}
	if h.met(alice, bob) || h.count("SELECT count(*) FROM location_shares") != 0 {
		t.Fatal("friendship remains after the Bluesky block")
	}
	for _, did := range []string{alice, bob} {
		if list, _ := h.friends.List(ctx, did); len(list) != 0 {
			t.Fatalf("list for %s = %+v, want empty", did, list)
		}
	}
	// CHAIN からは解除できない。
	blocks := &friendsvcBlocks{h}
	wantAPIError(t, blocks.unblock(alice, bob), 409, "conflict")

	// Bluesky で解除すると、相互フォローと裏付けが残っているので会ったフレンドに戻る。
	h.commit(alice, atproto.CollectionBskyBlock, "delete", "k1", nil)
	if h.count("SELECT count(*) FROM blocks") != 0 || !h.met(alice, bob) {
		t.Fatal("friendship did not come back after the Bluesky unblock")
	}
	// CHAIN のブロックは相手の知らないまま CHAIN の中だけ。解除もできる。
	if err := blocks.block(alice, bob); err != nil {
		t.Fatalf("block: %v", err)
	}
	if h.met(alice, bob) {
		t.Fatal("friendship remains after the CHAIN block")
	}
	if err := blocks.unblock(alice, bob); err != nil {
		t.Fatalf("unblock: %v", err)
	}
	if !h.met(alice, bob) {
		t.Fatal("friendship did not come back after the CHAIN unblock")
	}
	// subject が CHAIN の利用者でないブロックは持たない。
	h.commit(alice, atproto.CollectionBskyBlock, "create", "k2", map[string]any{"subject": "did:plc:outsider", "createdAt": "2026-10-01T00:00:00Z"})
	if h.count("SELECT count(*) FROM blocks") != 0 {
		t.Fatal("block of a non-user was stored")
	}
}

func TestSyncOnLogin(t *testing.T) {
	h := newFriendHarness(t)
	h.users(alice, bob, carol)
	ctx := context.Background()
	h.encounter("enc-ab", alice, bob, "revealed", h.env.Clock.Now())
	const pds = "https://pds.example"

	// bob は先にフォロー済み。alice は Bluesky のアプリで bob と carol をフォローし、carol をブロックしている。
	h.bskyFollow(bob, alice, "b1")
	h.env.Writer.SeedRepoRecord(alice, atproto.CollectionBskyFollow, "a1", map[string]any{"subject": bob, "createdAt": "2026-01-01T00:00:00Z"})
	h.env.Writer.SeedRepoRecord(alice, atproto.CollectionBskyFollow, "a2", map[string]any{"subject": carol, "createdAt": "2026-01-02T00:00:00Z"})
	h.env.Writer.SeedRepoRecord(alice, atproto.CollectionBskyBlock, "k1", map[string]any{"subject": carol, "createdAt": "2026-01-03T00:00:00Z"})

	notesBefore := h.count("SELECT count(*) FROM notifications")
	if err := h.friends.SyncOnLogin(ctx, alice, pds, "atp-alice"); err != nil {
		t.Fatalf("SyncOnLogin: %v", err)
	}
	if !h.follows(alice, bob) || !h.follows(alice, carol) || !h.met(alice, bob) {
		t.Fatal("follows were not read on first login")
	}
	if h.count("SELECT count(*) FROM blocks WHERE blocker = ? AND blocked = ? AND source = 'bsky'", alice, carol) != 1 {
		t.Fatal("bsky block was not read")
	}
	// ログイン時の読み直しでは通知しない。
	if n := h.count("SELECT count(*) FROM notifications"); n != notesBefore {
		t.Fatalf("notifications after sync = %d, want %d", n, notesBefore)
	}

	// 24 時間以内の 2 回目は読み直さない（listRecords を呼ばない）。
	h.env.Writer.RemoveRepoRecord(alice, atproto.GraphURI(alice, atproto.CollectionBskyFollow, "a2"))
	h.env.Writer.ListErr = errors.New("must not be called")
	if err := h.friends.SyncOnLogin(ctx, alice, pds, "atp-alice"); err != nil {
		t.Fatalf("second SyncOnLogin: %v", err)
	}
	if !h.follows(alice, carol) {
		t.Fatal("re-synced within 24 hours")
	}
	// 24 時間後は読み直し、PDS に無くなったフォローとブロックを消す。
	h.env.Writer.ListErr = nil
	h.env.Writer.RemoveRepoRecord(alice, atproto.GraphURI(alice, atproto.CollectionBskyBlock, "k1"))
	h.env.Clock.Advance(25 * time.Hour)
	if err := h.friends.SyncOnLogin(ctx, alice, pds, "atp-alice"); err != nil {
		t.Fatalf("third SyncOnLogin: %v", err)
	}
	if h.follows(alice, carol) || !h.follows(alice, bob) || h.count("SELECT count(*) FROM blocks") != 0 {
		t.Fatal("stale follow or block was not removed on resync")
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

	req, err := h.friends.CreateRequest(ctx, bob, h.sid(bob), friendsvc.CreateRequestInput{
		DID: alice, Via: "encounter", ViaRef: "enc-ab",
	})
	if err != nil {
		t.Fatalf("CreateRequest: %v", err)
	}
	// 承認で相互フォローにはなるが、会ったフレンドの上限を超えるので friendships には入らない（07 §1）。
	friend, err := h.friends.AcceptRequest(ctx, alice, h.sid(alice), req.ID)
	if err != nil {
		t.Fatalf("AcceptRequest: %v", err)
	}
	if friend.Met || friend.Precision != "off" {
		t.Fatalf("friend over the limit = %+v, want met=false precision=off", friend)
	}
	if n := h.count("SELECT count(*) FROM friendships WHERE user_a = ? AND user_b = ?", alice, bob); n != 0 {
		t.Fatalf("friendship over the limit = %d, want 0", n)
	}

	// 招待コードでも上限を超えられない。
	inv, err := h.friends.CreateInvite(ctx, bob)
	if err != nil {
		t.Fatalf("CreateInvite: %v", err)
	}
	if _, err := h.friends.AcceptInvite(ctx, alice, h.sid(alice), inv.Code); err == nil {
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
	if _, err := h.friends.CreateRequest(ctx, alice, h.sid(alice), friendsvc.CreateRequestInput{
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
	_, err := h.friends.CreateRequest(ctx, alice, h.sid(alice), friendsvc.CreateRequestInput{
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
		if _, err := h.friends.CreateRequest(ctx, in.from, h.sid(in.from), friendsvc.CreateRequestInput{
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

// friendsvcBlocks は main.go と同じ配線の BlockService を呼ぶ小さな道具。
type friendsvcBlocks struct{ h *friendHarness }

func (b *friendsvcBlocks) svc() *service.BlockService {
	return &service.BlockService{Deps: b.h.env.Deps, Recon: b.h.env.Reconciler()}
}
func (b *friendsvcBlocks) block(blocker, blocked string) error {
	return b.svc().Create(context.Background(), blocker, blocked)
}
func (b *friendsvcBlocks) unblock(blocker, blocked string) error {
	return b.svc().Delete(context.Background(), blocker, blocked)
}
