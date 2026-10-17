//go:build integration

package service_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/applemango/chain/internal/httpx"
	"github.com/applemango/chain/internal/service"
	"github.com/applemango/chain/internal/storage"
	"github.com/applemango/chain/internal/testutil"
)

// Pin（10 §6）の自動・手動・上限・削除を確かめる。

func rkeyOf(t *testing.T, uri string) string {
	t.Helper()
	parts := strings.Split(uri, "/")
	if len(parts) == 0 {
		t.Fatalf("uri %q has no rkey", uri)
	}
	return parts[len(parts)-1]
}

func wantHTTPError(t *testing.T, err error, code string) {
	t.Helper()
	var e *httpx.Error
	if !errors.As(err, &e) {
		t.Fatalf("err = %v, want *httpx.Error with code %q", err, code)
	}
	if e.Code != code {
		t.Fatalf("err code = %q, want %q", e.Code, code)
	}
}

type pinHarness struct {
	t      *testing.T
	env    *testutil.Env
	inst   *service.InstanceService
	pins   *service.PinService
	queue  *service.ModerationQueueService
	broker *testutil.FakeBroker
}

func newPinHarness(t *testing.T, assessor service.PostAssessor) *pinHarness {
	t.Helper()
	env := testutil.NewEnv(t)
	env.CreateUser(t, "did:plc:member", "member.test")
	broker := testutil.NewFakeBroker()
	access := &service.AccessService{Deps: env.Deps, Zones: env.Zones}
	pins := &service.PinService{Deps: env.Deps, Store: env.Store, Access: access, Broker: broker}
	inst := &service.InstanceService{
		Deps: env.Deps, Store: env.Store, Access: access, Assessor: assessor, Broker: broker,
		Signer: storage.NewSigner(env.Config.JWTKey), Pins: pins,
	}
	queue := &service.ModerationQueueService{Deps: env.Deps, Assessor: assessor, Broker: broker, Pins: pins}
	return &pinHarness{t: t, env: env, inst: inst, pins: pins, queue: queue, broker: broker}
}

func (h *pinHarness) setup(did string) {
	h.t.Helper()
	h.env.CreateUser(h.t, did, "u.test")
	h.env.Consent(h.t, did)
	writableGroup(h.t, h.env, "sp1", did)
}

func (h *pinHarness) post(did, text string) string {
	h.t.Helper()
	res, err := h.inst.CreatePosts(context.Background(), did, service.CreatePostInput{
		SpaceIDs: []string{"sp1"}, Text: text, Format: "plain",
	})
	if err != nil {
		h.t.Fatalf("CreatePosts(%q): %v", text, err)
	}
	return res.Items[0].URI
}

func (h *pinHarness) list(did string) []service.Pin {
	h.t.Helper()
	pins, err := h.pins.List(context.Background(), did, "sp1")
	if err != nil {
		h.t.Fatalf("List: %v", err)
	}
	return pins
}

func TestPinAutoOnCreate(t *testing.T) {
	h := newPinHarness(t, &testutil.FakeAssessor{Pin: true})
	did := "did:plc:auto"
	h.setup(did)

	uri := h.post(did, "集合は 9:00 正門")

	pins := h.list(did)
	if len(pins) != 1 {
		t.Fatalf("pins = %+v, want 1", pins)
	}
	if pins[0].Subject != uri || pins[0].Reason != service.PinReasonAuto || pins[0].PinnedBy != "" {
		t.Fatalf("pin = %+v, want auto pin for %s", pins[0], uri)
	}
	if len(h.broker.EventsFor("pin.created", "")) != 1 {
		t.Fatalf("pin.created events = %d, want 1", len(h.broker.EventsFor("pin.created", "")))
	}
}

func TestPinAutoSkippedWhenNotImportant(t *testing.T) {
	h := newPinHarness(t, &testutil.FakeAssessor{Pin: false})
	did := "did:plc:noauto"
	h.setup(did)

	h.post(did, "雑談")

	if pins := h.list(did); len(pins) != 0 {
		t.Fatalf("pins = %+v, want 0", pins)
	}
}

func TestPinManualAndDelete(t *testing.T) {
	h := newPinHarness(t, service.ModerationNoop{})
	did := "did:plc:manual"
	h.setup(did)
	uri := h.post(did, "手動で固定する投稿")

	pin, err := h.pins.CreateManual(context.Background(), did, "sp1", uri)
	if err != nil {
		t.Fatalf("CreateManual: %v", err)
	}
	if pin.Reason != service.PinReasonManual || pin.PinnedBy != did || pin.Subject != uri {
		t.Fatalf("pin = %+v", pin)
	}

	rkey := rkeyOf(t, pin.URI)
	if err := h.pins.Delete(context.Background(), did, "sp1", rkey); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if pins := h.list(did); len(pins) != 0 {
		t.Fatalf("pins after delete = %+v, want 0", pins)
	}
	if len(h.broker.EventsFor("pin.deleted", "")) != 1 {
		t.Fatalf("pin.deleted events = %d, want 1", len(h.broker.EventsFor("pin.deleted", "")))
	}
}

func TestPinManualDoubleConflict(t *testing.T) {
	h := newPinHarness(t, service.ModerationNoop{})
	did := "did:plc:double"
	h.setup(did)
	uri := h.post(did, "一度だけ Pin")

	if _, err := h.pins.CreateManual(context.Background(), did, "sp1", uri); err != nil {
		t.Fatalf("first CreateManual: %v", err)
	}
	_, err := h.pins.CreateManual(context.Background(), did, "sp1", uri)
	wantHTTPError(t, err, "conflict")
}

func TestPinManualRejectsForeignSubject(t *testing.T) {
	h := newPinHarness(t, service.ModerationNoop{})
	did := "did:plc:foreign"
	h.setup(did)

	// 別 Space の投稿。
	mkSpace(t, h.env, "sp2", "jp.i32.chain.space.group", "active")
	mkGroup(t, h.env, "sp2", true, nil)
	mkMember(t, h.env, "sp2", did, true, true, false)
	other := "at://did:web:chain.test/space/jp.i32.chain.space.group/sp2/" + did + "/jp.i32.chain.instance.post/rkey-x"

	_, err := h.pins.CreateManual(context.Background(), did, "sp1", other)
	wantHTTPError(t, err, "invalid_request")
}

func TestPinLimitEvictsOldestAuto(t *testing.T) {
	h := newPinHarness(t, &testutil.FakeAssessor{Pin: true})
	did := "did:plc:limit"
	h.setup(did)

	var first string
	for i := 0; i < service.MaxPinsPerInstance+1; i++ {
		uri := h.post(did, fmt.Sprintf("重要な連絡 %d", i))
		if i == 0 {
			first = uri
		}
	}

	pins := h.list(did)
	if len(pins) != service.MaxPinsPerInstance {
		t.Fatalf("pins = %d, want %d", len(pins), service.MaxPinsPerInstance)
	}
	for _, p := range pins {
		if p.Subject == first {
			t.Fatalf("oldest auto pin was not evicted: %s", first)
		}
	}
}

func TestPinLimitManualWithoutAutoConflict(t *testing.T) {
	h := newPinHarness(t, service.ModerationNoop{})
	did := "did:plc:full"
	h.setup(did)

	for i := 0; i < service.MaxPinsPerInstance; i++ {
		uri := h.post(did, fmt.Sprintf("投稿 %d", i))
		if _, err := h.pins.CreateManual(context.Background(), did, "sp1", uri); err != nil {
			t.Fatalf("CreateManual #%d: %v", i, err)
		}
	}
	extra := h.post(did, "11 件目")
	if _, err := h.pins.CreateManual(context.Background(), did, "sp1", extra); err == nil {
		t.Fatalf("11th CreateManual succeeded, want conflict")
	} else {
		wantHTTPError(t, err, "conflict")
	}
}

func TestPinRemovedWhenPostDeleted(t *testing.T) {
	h := newPinHarness(t, service.ModerationNoop{})
	did := "did:plc:delete"
	h.setup(did)
	uri := h.post(did, "削除する投稿")
	if _, err := h.pins.CreateManual(context.Background(), did, "sp1", uri); err != nil {
		t.Fatalf("CreateManual: %v", err)
	}

	if err := h.inst.DeletePost(context.Background(), did, "sp1", rkeyOf(t, uri)); err != nil {
		t.Fatalf("DeletePost: %v", err)
	}
	if pins := h.list(did); len(pins) != 0 {
		t.Fatalf("pins after post delete = %+v, want 0", pins)
	}
}

func TestPinCreatedByRequeue(t *testing.T) {
	h := newPinHarness(t, &testutil.FakeAssessor{Pin: true})
	did := "did:plc:requeue"
	h.setup(did)

	uri := "at://did:web:chain.test/space/jp.i32.chain.space.group/sp1/" + did + "/jp.i32.chain.instance.post/rkey-q"
	insertPendingPost(t, h.env, uri, "sp1", did, "再判定で Pin される投稿")
	enqueue(t, h.env, uri)

	if _, err := h.queue.ProcessPending(context.Background()); err != nil {
		t.Fatalf("ProcessPending: %v", err)
	}
	pins := h.list(did)
	if len(pins) != 1 || pins[0].Subject != uri || pins[0].Reason != service.PinReasonAuto {
		t.Fatalf("pins = %+v, want 1 auto pin for %s", pins, uri)
	}
}
