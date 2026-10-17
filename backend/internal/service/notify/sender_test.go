//go:build integration

package notify_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/applemango/chain/internal/external/fcm"
	"github.com/applemango/chain/internal/model"
	"github.com/applemango/chain/internal/service/notify"
	"github.com/applemango/chain/internal/testutil"
)

// 12 §4 の送信。due な outbox を全 devices に送り、失敗はバックオフ、5 回で failed、
// UNREGISTERED のトークンは削除する。

// notify のテストが共有する DID。
const alice = "did:plc:alice"

func addDevice(t *testing.T, env *testutil.Env, did, token string) {
	t.Helper()
	if err := model.UpsertDevice(context.Background(), env.DB, model.Device{
		ID: env.IDs.NewID(), DID: did, FCMToken: token, Platform: "android", UpdatedAt: env.Clock.Now(),
	}); err != nil {
		t.Fatalf("UpsertDevice(%s): %v", token, err)
	}
}

func queuedNotification(t *testing.T, env *testutil.Env, did, typ string, payload map[string]string) string {
	t.Helper()
	ctx := context.Background()
	b, _ := json.Marshal(payload)
	nid := env.IDs.NewID()
	if err := model.CreateNotification(ctx, env.DB, model.Notification{
		ID: nid, DID: did, Type: typ, PayloadJSON: string(b), CreatedAt: env.Clock.Now(),
	}); err != nil {
		t.Fatalf("CreateNotification: %v", err)
	}
	if err := model.EnqueueNotification(ctx, env.DB, env.IDs.NewID(), nid, env.Clock.Now()); err != nil {
		t.Fatalf("EnqueueNotification: %v", err)
	}
	return nid
}

func outboxRow(t *testing.T, env *testutil.Env, notificationID string) (status string, attempts int, nextAt int64) {
	t.Helper()
	if err := env.DB.QueryRowContext(context.Background(),
		"SELECT status, attempts, next_attempt_at FROM notification_outbox WHERE notification_id = ?", notificationID).
		Scan(&status, &attempts, &nextAt); err != nil {
		t.Fatalf("select outbox: %v", err)
	}
	return status, attempts, nextAt
}

func TestNotifySenderSendsToAllDevices(t *testing.T) {
	ctx := context.Background()
	env := testutil.NewEnv(t)
	env.CreateUser(t, alice, "alice.test")

	fake := testutil.NewFakeFCM()
	sender := &notify.NotifySender{Deps: env.Deps, Sender: fake}
	addDevice(t, env, alice, "tok-a")
	addDevice(t, env, alice, "tok-b")

	nid := queuedNotification(t, env, alice, notify.NotificationFriendRequest, map[string]string{"text": "Bob さんからフレンド申請"})

	n, err := sender.SendDue(ctx)
	if err != nil {
		t.Fatalf("SendDue: %v", err)
	}
	if n != 1 {
		t.Fatalf("sent = %d, want 1", n)
	}
	if status, attempts, _ := outboxRow(t, env, nid); status != "sent" || attempts != 0 {
		t.Fatalf("outbox = (%q,%d), want (sent,0)", status, attempts)
	}

	sent := fake.Sent()
	if len(sent) != 2 {
		t.Fatalf("fcm sends = %d, want 2", len(sent))
	}
	tokens := map[string]bool{}
	for _, m := range sent {
		tokens[m.Token] = true
		if m.Data["type"] != notify.NotificationFriendRequest {
			t.Fatalf("data.type = %q, want friend.request", m.Data["type"])
		}
		if m.ChannelID != "social" {
			t.Fatalf("channel = %q, want social", m.ChannelID)
		}
		if m.Title == "" || m.Body != "Bob さんからフレンド申請" {
			t.Fatalf("notification = (%q,%q), want a title and the payload body", m.Title, m.Body)
		}
	}
	if !tokens["tok-a"] || !tokens["tok-b"] {
		t.Fatalf("tokens = %v, want both tok-a and tok-b", tokens)
	}
}

func TestNotifySenderDigestChannelAndCollapseKey(t *testing.T) {
	ctx := context.Background()
	env := testutil.NewEnv(t)
	env.CreateUser(t, alice, "alice.test")
	fake := testutil.NewFakeFCM()
	sender := &notify.NotifySender{Deps: env.Deps, Sender: fake}
	addDevice(t, env, alice, "tok")

	queuedNotification(t, env, alice, "encounter.daily", map[string]string{"text": "今日のすれ違い: 3 人"})
	if _, err := sender.SendDue(ctx); err != nil {
		t.Fatalf("SendDue: %v", err)
	}
	sent := fake.Sent()
	if len(sent) != 1 || sent[0].ChannelID != "digest" || sent[0].CollapseKey != "encounter.daily" {
		t.Fatalf("message = %+v, want digest channel and type collapse key", sent)
	}
}

func TestNotifySenderBacksOffAndFails(t *testing.T) {
	ctx := context.Background()
	env := testutil.NewEnv(t)
	env.CreateUser(t, alice, "alice.test")
	fake := testutil.NewFakeFCM()
	fake.FailAll(errors.New("boom"))
	sender := &notify.NotifySender{Deps: env.Deps, Sender: fake}
	addDevice(t, env, alice, "tok")

	nid := queuedNotification(t, env, alice, notify.NotificationFriendRequest, map[string]string{"text": "x"})
	start := env.Clock.Now()

	if _, err := sender.SendDue(ctx); err != nil {
		t.Fatalf("SendDue #1: %v", err)
	}
	status, attempts, nextAt := outboxRow(t, env, nid)
	if status != "queued" || attempts != 1 {
		t.Fatalf("after #1: outbox = (%q,%d), want (queued,1)", status, attempts)
	}
	if want := start.Add(20 * time.Second).UnixMilli(); nextAt != want {
		t.Fatalf("next_attempt_at = %d, want %d (2^1 * 10s)", nextAt, want)
	}

	for i := 2; i <= 5; i++ {
		env.Clock.Advance(time.Hour)
		if _, err := sender.SendDue(ctx); err != nil {
			t.Fatalf("SendDue #%d: %v", i, err)
		}
	}
	status, attempts, _ = outboxRow(t, env, nid)
	if status != "failed" || attempts != 5 {
		t.Fatalf("after 5 failures: outbox = (%q,%d), want (failed,5)", status, attempts)
	}
}

func TestNotifySenderRemovesUnregisteredToken(t *testing.T) {
	ctx := context.Background()
	env := testutil.NewEnv(t)
	env.CreateUser(t, alice, "alice.test")
	fake := testutil.NewFakeFCM()
	fake.FailToken("tok-dead", fcm.ErrUnregistered)
	sender := &notify.NotifySender{Deps: env.Deps, Sender: fake}
	addDevice(t, env, alice, "tok-dead")
	addDevice(t, env, alice, "tok-live")

	nid := queuedNotification(t, env, alice, notify.NotificationFriendRequest, map[string]string{"text": "x"})
	if _, err := sender.SendDue(ctx); err != nil {
		t.Fatalf("SendDue: %v", err)
	}

	if status, _, _ := outboxRow(t, env, nid); status != "sent" {
		t.Fatalf("outbox status = %q, want sent", status)
	}
	devices, err := model.ListDevicesByDID(ctx, env.DB, alice)
	if err != nil {
		t.Fatalf("ListDevicesByDID: %v", err)
	}
	if len(devices) != 1 || devices[0].FCMToken != "tok-live" {
		t.Fatalf("devices = %+v, want only tok-live", devices)
	}
}
