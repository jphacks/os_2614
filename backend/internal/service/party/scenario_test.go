//go:build integration

package party_test

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"strings"
	"testing"
	"time"

	"github.com/applemango/chain/internal/httpx"
	"github.com/applemango/chain/internal/model"
	friendmodel "github.com/applemango/chain/internal/model/friend"
	"github.com/applemango/chain/internal/service/access"
	"github.com/applemango/chain/internal/service/content"
	core "github.com/applemango/chain/internal/service/core"
	"github.com/applemango/chain/internal/service/moderation"
	"github.com/applemango/chain/internal/service/notify"
	partysvc "github.com/applemango/chain/internal/service/party"
	"github.com/applemango/chain/internal/service/proximity"
	"github.com/applemango/chain/internal/storage"
	"github.com/applemango/chain/internal/testutil"
)

const (
	pa = "did:plc:pa"
	pb = "did:plc:pb"
	pc = "did:plc:pc"
	pd = "did:plc:pd"
	pe = "did:plc:pe"
)

type partyRow struct {
	spaceID string
	phase   string
	locked  bool
}

type partyHarness struct {
	t         *testing.T
	env       *testutil.Env
	svc       *partysvc.PartyService
	broker    *testutil.FakeBroker
	instances *content.InstanceService
	media     *content.MediaService
}

func newPartyHarness(t *testing.T) *partyHarness {
	t.Helper()
	env := testutil.NewEnv(t)
	broker := testutil.NewFakeBroker()
	signer := storage.NewSigner(env.Config.JWTKey)
	access := &access.AccessService{Deps: env.Deps, Zones: env.Zones}
	svc := &partysvc.PartyService{
		Deps: env.Deps, Proximity: &proximity.BLEPeerProximity{Deps: env.Deps},
		Access: access, Moderator: moderation.ModerationNoop{}, Broker: broker,
		Signer: signer, Beacon: &proximity.BeaconService{Deps: env.Deps}, Store: env.Store,
	}
	mediaStore := storage.NewStore(env.Config.MediaDir)
	return &partyHarness{
		t:      t,
		env:    env,
		svc:    svc,
		broker: broker,
		instances: &content.InstanceService{
			Deps: env.Deps, Store: env.Store, Access: access,
			Assessor: moderation.ModerationNoop{}, Broker: broker, Signer: signer,
		},
		media: &content.MediaService{
			Deps: env.Deps, Store: env.Store, Access: access, Broker: broker,
			Storage: mediaStore, Signer: signer,
		},
	}
}

func (h *partyHarness) users(dids ...string) {
	h.t.Helper()
	for _, did := range dids {
		h.env.CreateUser(h.t, did, did[8:]+".test")
	}
}

func (h *partyHarness) friend(a, b string) {
	h.t.Helper()
	x, y := a, b
	if x > y {
		x, y = y, x
	}
	if err := friendmodel.CreateFriendship(context.Background(), h.env.DB, x, y, "encounter", h.env.Clock.Now()); err != nil {
		h.t.Fatalf("CreateFriendship(%s,%s): %v", a, b, err)
	}
}

func (h *partyHarness) setAuto(did string, auto bool) {
	h.t.Helper()
	a := auto
	if err := model.UpdateSettings(context.Background(), h.env.DB, did, model.SettingsPatch{PartyAuto: &a}, h.env.Clock.Now()); err != nil {
		h.t.Fatalf("UpdateSettings(%s): %v", did, err)
	}
}

func (h *partyHarness) observe(pairs [][2]string) {
	h.t.Helper()
	now := h.env.Clock.Now()
	for _, p := range pairs {
		if err := h.svc.Presence(context.Background(), p[0], []partysvc.PresenceObservation{{DID: p[1], LastSeenAt: now}}); err != nil {
			h.t.Fatalf("Presence(%s->%s): %v", p[0], p[1], err)
		}
	}
}

// pass は今の時刻で観測し、時計を d 進め、Tick を 1 回呼ぶ（15 秒ごとの報告と 5 秒 tick を模す）。
func (h *partyHarness) pass(d time.Duration, pairs [][2]string) {
	h.t.Helper()
	h.observe(pairs)
	h.env.Clock.Advance(d)
	h.tick()
}

func (h *partyHarness) tick() {
	h.t.Helper()
	if err := h.svc.Tick(context.Background()); err != nil {
		h.t.Fatalf("Tick: %v", err)
	}
}

func (h *partyHarness) parties() []partyRow {
	h.t.Helper()
	rows, err := h.env.DB.QueryContext(context.Background(), `
		SELECT p.space_id, p.phase, p.locked FROM parties p
		JOIN spaces s ON s.id = p.space_id
		WHERE s.state = 'active' AND p.phase IN ('tentative','confirmed')
		ORDER BY p.tentative_at, p.space_id`)
	if err != nil {
		h.t.Fatalf("query parties: %v", err)
	}
	defer rows.Close()
	var out []partyRow
	for rows.Next() {
		var r partyRow
		if err := rows.Scan(&r.spaceID, &r.phase, &r.locked); err != nil {
			h.t.Fatalf("scan party: %v", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		h.t.Fatalf("rows: %v", err)
	}
	return out
}

func (h *partyHarness) members(spaceID string) []string {
	h.t.Helper()
	rows, err := h.env.DB.QueryContext(context.Background(),
		"SELECT did FROM space_members WHERE space_id = ? AND left_at IS NULL ORDER BY did", spaceID)
	if err != nil {
		h.t.Fatalf("query members: %v", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var did string
		if err := rows.Scan(&did); err != nil {
			h.t.Fatalf("scan member: %v", err)
		}
		out = append(out, did)
	}
	return out
}

func (h *partyHarness) spaceState(spaceID string) string {
	h.t.Helper()
	var state string
	if err := h.env.DB.QueryRowContext(context.Background(), "SELECT state FROM spaces WHERE id = ?", spaceID).Scan(&state); err != nil {
		h.t.Fatalf("query state: %v", err)
	}
	return state
}

func (h *partyHarness) leftAt(spaceID, did string) (bool, error) {
	var left interface{}
	err := h.env.DB.QueryRowContext(context.Background(),
		"SELECT left_at FROM space_members WHERE space_id = ? AND did = ?", spaceID, did).Scan(&left)
	if err != nil {
		return false, err
	}
	return left != nil, nil
}

// lock は固定（lock / unlock）を切り替える（08 §4.4）。
func (h *partyHarness) lock(spaceID, did string, locked bool) {
	h.t.Helper()
	if _, err := h.svc.SetLocked(context.Background(), did, spaceID, locked); err != nil {
		h.t.Fatalf("SetLocked(%v) by %s: %v", locked, did, err)
	}
}

// partySystemPosts は Space に残った system 投稿の本文を返す（08 §4.4）。
func (h *partyHarness) partySystemPosts(spaceID string) []string {
	h.t.Helper()
	rows, err := h.env.DB.QueryContext(context.Background(),
		`SELECT value_json FROM records WHERE space_id = ? AND collection = 'jp.i32.chain.instance.post' AND value_json LIKE '%"system":true%'`,
		spaceID)
	if err != nil {
		h.t.Fatalf("query system posts: %v", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			h.t.Fatalf("scan system post: %v", err)
		}
		out = append(out, raw)
	}
	return out
}

// confirm は 2 人の近接を 5 分続け、confirmed の party の spaceID を返す。
func (h *partyHarness) confirm(a, b string) string {
	h.t.Helper()
	pairs := [][2]string{{a, b}}
	h.pass(31*time.Second, pairs)
	for i := 0; i < 20; i++ {
		h.pass(15*time.Second, pairs)
	}
	parties := h.parties()
	if len(parties) != 1 || parties[0].phase != "confirmed" {
		h.t.Fatalf("parties = %+v, want 1 confirmed", parties)
	}
	return parties[0].spaceID
}

func (h *partyHarness) jpeg() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			img.Set(x, y, color.RGBA{R: 200, G: 100, B: 50, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		h.t.Fatalf("encode jpeg: %v", err)
	}
	return buf.Bytes()
}

func TestPartyScenarios(t *testing.T) {
	t.Run("P1 30 秒の近接で仮 party", func(t *testing.T) {
		h := newPartyHarness(t)
		h.users(pa, pb)
		h.friend(pa, pb)

		h.pass(31*time.Second, [][2]string{{pa, pb}})

		parties := h.parties()
		if len(parties) != 1 || parties[0].phase != "tentative" {
			t.Fatalf("parties = %+v, want 1 tentative", parties)
		}
	})

	t.Run("P2 5 分続くと本 party", func(t *testing.T) {
		h := newPartyHarness(t)
		h.users(pa, pb)
		h.friend(pa, pb)

		pairs := [][2]string{{pa, pb}}
		h.pass(31*time.Second, pairs)
		for i := 0; i < 20; i++ {
			h.pass(15*time.Second, pairs)
		}

		parties := h.parties()
		if len(parties) != 1 || parties[0].phase != "confirmed" {
			t.Fatalf("parties = %+v, want 1 confirmed", parties)
		}
		spaceID := parties[0].spaceID
		if got := h.broker.EventsFor("party.updated", pa); len(got) == 0 {
			t.Fatalf("party.updated が %s のユーザーチャンネルに送られていない", pa)
		}
		spaceEvents := 0
		for _, e := range h.broker.Events() {
			if e.Name == "party.updated" && e.Space == spaceID {
				spaceEvents++
			}
		}
		if spaceEvents == 0 {
			t.Fatalf("party.updated が Space %s に送られていない", spaceID)
		}
	})

	t.Run("P3 仮 party の途中で 70 秒途切れると破棄", func(t *testing.T) {
		h := newPartyHarness(t)
		h.users(pa, pb)
		h.friend(pa, pb)

		h.pass(31*time.Second, [][2]string{{pa, pb}})
		if len(h.parties()) != 1 {
			t.Fatalf("仮 party ができていない")
		}
		h.pass(70*time.Second, nil)

		if parties := h.parties(); len(parties) != 0 {
			t.Fatalf("parties = %+v, want 0 (dissolved)", parties)
		}
	})

	t.Run("P4 本 party で 10 分不在", func(t *testing.T) {
		h := newPartyHarness(t)
		h.users(pa, pb)
		h.friend(pa, pb)
		spaceID := h.confirm(pa, pb)

		h.pass(10*time.Minute+time.Second, nil)

		left, err := h.leftAt(spaceID, pb)
		if err != nil {
			t.Fatalf("leftAt: %v", err)
		}
		if !left {
			t.Fatalf("B が離脱していない")
		}
		if state := h.spaceState(spaceID); state != "archived" {
			t.Fatalf("state = %q, want archived", state)
		}
	})

	t.Run("P5 フレンドのフレンドが同じ party に入る", func(t *testing.T) {
		h := newPartyHarness(t)
		h.users(pa, pb, pc)
		h.friend(pa, pb)
		h.friend(pb, pc)
		spaceID := h.confirm(pa, pb)

		pairs := [][2]string{{pa, pb}, {pb, pc}, {pc, pb}}
		for i := 0; i < 3; i++ {
			h.pass(16*time.Second, pairs)
		}

		members := h.members(spaceID)
		found := false
		for _, m := range members {
			if m == pc {
				found = true
			}
		}
		if !found {
			t.Fatalf("members = %v, want C to join", members)
		}
	})

	t.Run("P6 仮同士は古い方に統合", func(t *testing.T) {
		h := newPartyHarness(t)
		h.users(pa, pb, pc, pd)
		h.friend(pa, pb)
		h.friend(pc, pd)
		h.friend(pb, pc)

		// T1（A,B）と T2（C,D）を別々に作る。T1 を消さないよう A-B は観測し続ける。
		h.pass(31*time.Second, [][2]string{{pa, pb}})
		for i := 0; i < 3; i++ {
			h.pass(16*time.Second, [][2]string{{pa, pb}, {pc, pd}})
		}
		if parties := h.parties(); len(parties) != 2 {
			t.Fatalf("parties = %+v, want 2 tentatives", parties)
		}
		oldest := h.parties()[0].spaceID

		// B-C を繋いで統合させる。
		merged := [][2]string{{pa, pb}, {pb, pc}, {pc, pd}}
		for i := 0; i < 3; i++ {
			h.pass(16*time.Second, merged)
		}

		parties := h.parties()
		if len(parties) != 1 {
			t.Fatalf("parties = %+v, want 1 after merge", parties)
		}
		if parties[0].spaceID != oldest {
			t.Fatalf("merged party = %s, want oldest %s", parties[0].spaceID, oldest)
		}
		if members := h.members(oldest); len(members) != 4 {
			t.Fatalf("members = %v, want all 4", members)
		}
	})

	t.Run("P7 本同士は統合しない", func(t *testing.T) {
		h := newPartyHarness(t)
		h.users(pa, pb, pc, pd)
		h.friend(pa, pb)
		h.friend(pc, pd)
		h.friend(pb, pc)

		both := [][2]string{{pa, pb}, {pc, pd}}
		h.pass(31*time.Second, both)
		for i := 0; i < 20; i++ {
			h.pass(15*time.Second, both)
		}
		if parties := h.parties(); len(parties) != 2 {
			t.Fatalf("parties = %+v, want 2 confirmed", parties)
		}

		connected := [][2]string{{pa, pb}, {pc, pd}, {pb, pc}}
		for i := 0; i < 3; i++ {
			h.pass(16*time.Second, connected)
		}
		if parties := h.parties(); len(parties) != 2 {
			t.Fatalf("parties = %+v, want 2 (confirmed do not merge)", parties)
		}
	})

	t.Run("P8 locked の本 party は 1 日不在でも終了せず 72 時間で終了", func(t *testing.T) {
		h := newPartyHarness(t)
		h.users(pa, pb)
		h.friend(pa, pb)
		spaceID := h.confirm(pa, pb)

		h.lock(spaceID, pa, true)

		h.pass(24*time.Hour, nil)
		if state := h.spaceState(spaceID); state != "active" {
			t.Fatalf("state after 24h = %q, want active", state)
		}
		if left, err := h.leftAt(spaceID, pb); err != nil {
			t.Fatalf("leftAt: %v", err)
		} else if left {
			t.Fatalf("B が locked 中に離脱した")
		}

		h.pass(48*time.Hour, nil)
		if state := h.spaceState(spaceID); state != "archived" {
			t.Fatalf("state after 72h = %q, want archived", state)
		}
	})

	t.Run("P9 party_auto = 0 は入らない", func(t *testing.T) {
		h := newPartyHarness(t)
		h.users(pa, pb)
		h.friend(pa, pb)
		h.setAuto(pb, false)

		h.pass(31*time.Second, [][2]string{{pa, pb}})

		if parties := h.parties(); len(parties) != 0 {
			t.Fatalf("parties = %+v, want 0", parties)
		}
	})

	t.Run("P10 仮 party には投稿できない", func(t *testing.T) {
		h := newPartyHarness(t)
		h.users(pa, pb)
		h.friend(pa, pb)
		h.env.Consent(t, pa)

		h.pass(31*time.Second, [][2]string{{pa, pb}})
		parties := h.parties()
		if len(parties) != 1 {
			t.Fatalf("parties = %+v, want 1", parties)
		}
		spaceID := parties[0].spaceID

		_, err := h.instances.CreatePosts(context.Background(), pa, content.CreatePostInput{
			SpaceIDs: []string{spaceID}, Text: "hello", Format: "plain",
		})
		if !errors.Is(err, httpx.ErrForbidden) {
			t.Fatalf("CreatePosts err = %v, want forbidden", err)
		}
		_, err = h.media.ShareToSpace(context.Background(), pa, spaceID, "m1", nil)
		if !errors.Is(err, httpx.ErrForbidden) {
			t.Fatalf("ShareToSpace err = %v, want forbidden", err)
		}
	})
}

func TestPartyMembershipScenarios(t *testing.T) {
	setup := func(t *testing.T) (*partyHarness, string) {
		t.Helper()
		h := newPartyHarness(t)
		h.users(pa, pb, pc, pd)
		h.friend(pa, pb)
		h.friend(pa, pc)
		h.friend(pb, pc)
		h.friend(pc, pd)
		pairs := [][2]string{{pa, pb}, {pb, pa}, {pa, pc}, {pc, pa}, {pb, pc}, {pc, pb}}
		h.pass(31*time.Second, pairs)
		for i := 0; i < 20; i++ {
			h.pass(15*time.Second, pairs)
		}
		parties := h.parties()
		if len(parties) != 1 || parties[0].phase != "confirmed" {
			t.Fatalf("parties = %+v, want 1 confirmed", parties)
		}
		return h, parties[0].spaceID
	}

	t.Run("P11 明示退出者とそのフレンドが元 party に入らない", func(t *testing.T) {
		h, spaceID := setup(t)
		if err := h.svc.Leave(context.Background(), pc, spaceID); err != nil {
			t.Fatalf("Leave: %v", err)
		}
		pairs := [][2]string{{pa, pb}, {pb, pa}, {pc, pd}, {pd, pc}}
		for i := 0; i < 26; i++ {
			h.pass(15*time.Second, pairs)
		}

		for _, did := range h.members(spaceID) {
			if did == pd {
				t.Fatalf("元 party の members = %v に D がいる", h.members(spaceID))
			}
		}
		found := false
		for _, p := range h.parties() {
			members := h.members(p.spaceID)
			if len(members) == 2 && members[0] == pc && members[1] == pd {
				found = true
			}
		}
		if !found {
			t.Fatalf("C と D の party がない: %+v", h.parties())
		}
	})

	t.Run("P12 自然離脱者とそのフレンドが元 party に入らない", func(t *testing.T) {
		h, spaceID := setup(t)
		for i := 0; i < 42; i++ {
			h.pass(15*time.Second, [][2]string{{pa, pb}, {pb, pa}})
		}
		left, err := h.leftAt(spaceID, pc)
		if err != nil || !left {
			t.Fatalf("C の自然離脱 = (%v, %v), want true", left, err)
		}
		for i := 0; i < 26; i++ {
			h.pass(15*time.Second, [][2]string{{pa, pb}, {pb, pa}, {pc, pd}, {pd, pc}})
		}

		for _, did := range h.members(spaceID) {
			if did == pd {
				t.Fatalf("元 party の members = %v に D がいる", h.members(spaceID))
			}
		}
		found := false
		for _, p := range h.parties() {
			members := h.members(p.spaceID)
			if len(members) == 2 && members[0] == pc && members[1] == pd {
				found = true
			}
		}
		if !found {
			t.Fatalf("C と D の party がない: %+v", h.parties())
		}
	})

	t.Run("P13 自然離脱者は元 party に再合流する", func(t *testing.T) {
		h, spaceID := setup(t)
		for i := 0; i < 42; i++ {
			h.pass(15*time.Second, [][2]string{{pa, pb}, {pb, pa}})
		}
		pairs := [][2]string{{pa, pb}, {pb, pa}, {pa, pc}, {pc, pa}, {pb, pc}, {pc, pb}}
		for i := 0; i < 4; i++ {
			h.pass(15*time.Second, pairs)
		}
		left, err := h.leftAt(spaceID, pc)
		if err != nil || left {
			t.Fatalf("C の left_at = (%v, %v), want NULL", left, err)
		}
		current, err := h.svc.Current(context.Background(), pc)
		if err != nil || current == nil || current.ID != spaceID {
			t.Fatalf("Current(C) = (%v, %v), want %s", current, err, spaceID)
		}
	})

	t.Run("P14 明示退出者は元 party に再合流しない", func(t *testing.T) {
		h, spaceID := setup(t)
		if err := h.svc.Leave(context.Background(), pc, spaceID); err != nil {
			t.Fatalf("Leave: %v", err)
		}
		pairs := [][2]string{{pa, pb}, {pb, pa}, {pa, pc}, {pc, pa}, {pb, pc}, {pc, pb}}
		for i := 0; i < 4; i++ {
			h.pass(15*time.Second, pairs)
		}
		left, err := h.leftAt(spaceID, pc)
		if err != nil || !left {
			t.Fatalf("C の left_at = (%v, %v), want non-NULL", left, err)
		}
		current, err := h.svc.Current(context.Background(), pc)
		if err != nil || current != nil {
			t.Fatalf("Current(C) = (%v, %v), want nil", current, err)
		}
	})

	t.Run("P17 RebuildIndex は自然離脱者を復元しない", func(t *testing.T) {
		h, spaceID := setup(t)
		for i := 0; i < 42; i++ {
			h.pass(15*time.Second, [][2]string{{pa, pb}, {pb, pa}})
		}
		if err := h.svc.RebuildIndex(context.Background()); err != nil {
			t.Fatalf("RebuildIndex: %v", err)
		}
		currentC, err := h.svc.Current(context.Background(), pc)
		if err != nil || currentC != nil {
			t.Fatalf("Current(C) = (%v, %v), want nil", currentC, err)
		}
		currentA, err := h.svc.Current(context.Background(), pa)
		if err != nil || currentA == nil || currentA.ID != spaceID {
			t.Fatalf("Current(A) = (%v, %v), want %s", currentA, err, spaceID)
		}
	})

	t.Run("P18 別 party 所属者は元 party に再合流しない", func(t *testing.T) {
		h, oldSpaceID := setup(t)
		for i := 0; i < 42; i++ {
			h.pass(15*time.Second, [][2]string{{pa, pb}, {pb, pa}})
		}
		for i := 0; i < 26; i++ {
			h.pass(15*time.Second, [][2]string{{pa, pb}, {pb, pa}, {pc, pd}, {pd, pc}})
		}
		current, err := h.svc.Current(context.Background(), pc)
		if err != nil || current == nil || current.ID == oldSpaceID || current.Phase != "confirmed" {
			t.Fatalf("C の party = (%v, %v), want old party 以外の confirmed", current, err)
		}
		newSpaceID := current.ID

		pairs := [][2]string{{pa, pb}, {pb, pa}, {pa, pc}, {pc, pa}, {pb, pc}, {pc, pb}, {pc, pd}, {pd, pc}}
		for i := 0; i < 4; i++ {
			h.pass(15*time.Second, pairs)
		}
		current, err = h.svc.Current(context.Background(), pc)
		if err != nil || current == nil || current.ID != newSpaceID {
			t.Fatalf("Current(C) = (%v, %v), want %s", current, err, newSpaceID)
		}
		if left, err := h.leftAt(oldSpaceID, pc); err != nil || !left {
			t.Fatalf("元 party の C の left_at = (%v, %v), want non-NULL", left, err)
		}
	})

	t.Run("P19 明示退出者は元 party の近くにいても party に入らない", func(t *testing.T) {
		h := newPartyHarness(t)
		h.users(pa, pb, pc, pe)
		h.friend(pa, pb)
		h.friend(pa, pc)
		h.friend(pb, pc)
		h.friend(pc, pe)
		pairs := [][2]string{{pa, pb}, {pb, pa}, {pa, pc}, {pc, pa}, {pb, pc}, {pc, pb}}
		h.pass(31*time.Second, pairs)
		for i := 0; i < 20; i++ {
			h.pass(15*time.Second, pairs)
		}
		parties := h.parties()
		if len(parties) != 1 || parties[0].phase != "confirmed" {
			t.Fatalf("parties = %+v, want 1 confirmed", parties)
		}
		spaceID := parties[0].spaceID
		if err := h.svc.Leave(context.Background(), pc, spaceID); err != nil {
			t.Fatalf("Leave: %v", err)
		}
		for i := 0; i < 26; i++ {
			h.pass(15*time.Second, pairs)
		}
		if parties := h.parties(); len(parties) != 1 || parties[0].spaceID != spaceID {
			t.Fatalf("parties = %+v, want only original party", parties)
		}
		if members := h.members(spaceID); len(members) != 2 || members[0] != pa || members[1] != pb {
			t.Fatalf("元 party の members = %v, want [pa pb]", members)
		}
		if left, err := h.leftAt(spaceID, pc); err != nil || !left {
			t.Fatalf("C の left_at = (%v, %v), want non-NULL", left, err)
		}
		current, err := h.svc.Current(context.Background(), pc)
		if err != nil || current != nil {
			t.Fatalf("Current(C) = (%v, %v), want nil", current, err)
		}
	})
}

func TestPartyPhotoSharing(t *testing.T) {
	h := newPartyHarness(t)
	h.users(pa, pb)
	h.friend(pa, pb)
	h.env.Consent(t, pa)
	spaceID := h.confirm(pa, pb)

	data := h.jpeg()
	up, err := h.media.Upload(context.Background(), pa, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}
	shared, err := h.media.ShareToSpace(context.Background(), pa, spaceID, up.ID, nil)
	if err != nil {
		t.Fatalf("ShareToSpace: %v", err)
	}
	if shared.SpaceID != spaceID {
		t.Fatalf("shared space = %s, want %s", shared.SpaceID, spaceID)
	}
	if _, err := h.instances.CreatePosts(context.Background(), pa, content.CreatePostInput{
		SpaceIDs: []string{spaceID}, Text: "本 party の投稿", Format: "plain",
	}); err != nil {
		t.Fatalf("CreatePosts: %v", err)
	}
}

func TestPartyUpdatedLifecycle(t *testing.T) {
	t.Run("P15 仮 party の破棄を全メンバーへ通知する", func(t *testing.T) {
		h := newPartyHarness(t)
		h.users(pa, pb)
		h.friend(pa, pb)
		h.pass(31*time.Second, [][2]string{{pa, pb}})
		parties := h.parties()
		if len(parties) != 1 {
			t.Fatalf("parties = %+v, want 1", parties)
		}
		spaceID := parties[0].spaceID
		h.broker.Reset()
		h.pass(70*time.Second, nil)

		for _, did := range []string{pa, pb} {
			events := h.broker.EventsFor("party.updated", did)
			if len(events) != 1 {
				t.Fatalf("party.updated for %s = %d, want 1", did, len(events))
			}
			data, ok := events[0].Data.(map[string]any)
			if !ok || data["id"] != spaceID || data["phase"] != "dissolved" {
				t.Fatalf("party.updated for %s = %+v, want dissolved %s", did, events[0].Data, spaceID)
			}
		}
	})

	t.Run("P16 終了を離脱済みを含む全メンバーへ通知する", func(t *testing.T) {
		h := newPartyHarness(t)
		h.users(pa, pb)
		h.friend(pa, pb)
		spaceID := h.confirm(pa, pb)
		h.broker.Reset()
		h.pass(10*time.Minute+time.Second, nil)

		for _, did := range []string{pa, pb} {
			events := h.broker.EventsFor("party.updated", did)
			if len(events) != 1 {
				t.Fatalf("party.updated for %s = %d, want 1", did, len(events))
			}
			data, ok := events[0].Data.(map[string]any)
			if !ok || data["id"] != spaceID || data["phase"] != "ended" {
				t.Fatalf("party.updated for %s = %+v, want ended %s", did, events[0].Data, spaceID)
			}
		}
	})

	t.Run("名前の変更を他のメンバーへ通知する", func(t *testing.T) {
		h := newPartyHarness(t)
		h.users(pa, pb)
		h.friend(pa, pb)
		spaceID := h.confirm(pa, pb)
		h.broker.Reset()
		if _, err := h.svc.Rename(context.Background(), pa, spaceID, "新しい名前"); err != nil {
			t.Fatalf("Rename: %v", err)
		}

		events := h.broker.EventsFor("party.updated", pb)
		if len(events) != 1 {
			t.Fatalf("party.updated for %s = %d, want 1", pb, len(events))
		}
		data, ok := events[0].Data.(map[string]any)
		if !ok || data["id"] != spaceID || data["name"] != "新しい名前" {
			t.Fatalf("party.updated = %+v, want name 新しい名前", events[0].Data)
		}
	})
}

func TestPartyIndexRebuild(t *testing.T) {
	h := newPartyHarness(t)
	h.users(pa, pb)
	h.friend(pa, pb)
	spaceID := h.confirm(pa, pb)

	ctx := context.Background()
	if err := model.ClearAllPartyOf(ctx, h.env.Redis); err != nil {
		t.Fatalf("ClearAllPartyOf: %v", err)
	}
	if _, ok, err := model.GetPartyOf(ctx, h.env.Redis, pa); err != nil || ok {
		t.Fatalf("party:of not cleared (ok=%v err=%v)", ok, err)
	}
	if err := h.svc.RebuildIndex(ctx); err != nil {
		t.Fatalf("RebuildIndex: %v", err)
	}
	for _, did := range []string{pa, pb} {
		got, ok, err := model.GetPartyOf(ctx, h.env.Redis, did)
		if err != nil || !ok || got != spaceID {
			t.Fatalf("party:of:%s = (%q, %v, %v), want %s", did, got, ok, err, spaceID)
		}
	}
}

func TestPartyLock(t *testing.T) {
	ctx := context.Background()

	t.Run("locked 中は 10 分不在でも離脱しない", func(t *testing.T) {
		h := newPartyHarness(t)
		h.users(pa, pb)
		h.friend(pa, pb)
		spaceID := h.confirm(pa, pb)
		h.lock(spaceID, pa, true)

		h.pass(11*time.Minute, nil)

		if left, _ := h.leftAt(spaceID, pb); left {
			t.Fatalf("B が locked 中に離脱した")
		}
		if state := h.spaceState(spaceID); state != "active" {
			t.Fatalf("state = %q, want active", state)
		}
	})

	t.Run("未離脱が 2 人未満になると終了", func(t *testing.T) {
		h := newPartyHarness(t)
		h.users(pa, pb)
		h.friend(pa, pb)
		spaceID := h.confirm(pa, pb)
		h.lock(spaceID, pa, true)

		if err := h.svc.Leave(ctx, pb, spaceID); err != nil {
			t.Fatalf("Leave: %v", err)
		}
		h.tick()

		if state := h.spaceState(spaceID); state != "archived" {
			t.Fatalf("state = %q, want archived", state)
		}
	})

	t.Run("7 日で終了", func(t *testing.T) {
		h := newPartyHarness(t)
		h.users(pa, pb)
		h.friend(pa, pb)
		spaceID := h.confirm(pa, pb)
		h.lock(spaceID, pa, true)

		h.pass(core.MaxDuration, nil)

		if state := h.spaceState(spaceID); state != "archived" {
			t.Fatalf("state = %q, want archived", state)
		}
	})

	t.Run("仮 party は lock できない", func(t *testing.T) {
		h := newPartyHarness(t)
		h.users(pa, pb)
		h.friend(pa, pb)
		h.pass(31*time.Second, [][2]string{{pa, pb}})
		parties := h.parties()
		if len(parties) != 1 {
			t.Fatalf("parties = %+v, want 1", parties)
		}

		if _, err := h.svc.SetLocked(ctx, pa, parties[0].spaceID, true); !errors.Is(err, httpx.ErrForbidden) {
			t.Fatalf("SetLocked err = %v, want forbidden", err)
		}
	})

	t.Run("メンバー以外は 403", func(t *testing.T) {
		h := newPartyHarness(t)
		h.users(pa, pb, pc)
		h.friend(pa, pb)
		spaceID := h.confirm(pa, pb)

		if _, err := h.svc.SetLocked(ctx, pc, spaceID, true); !errors.Is(err, httpx.ErrForbidden) {
			t.Fatalf("SetLocked err = %v, want forbidden", err)
		}
	})

	t.Run("system 投稿が残る", func(t *testing.T) {
		h := newPartyHarness(t)
		h.users(pa, pb)
		h.friend(pa, pb)
		spaceID := h.confirm(pa, pb)

		h.lock(spaceID, pa, true)

		posts := h.partySystemPosts(spaceID)
		if len(posts) != 1 {
			t.Fatalf("system posts = %d, want 1 (%v)", len(posts), posts)
		}
		if !strings.Contains(posts[0], `"system":true`) || !strings.Contains(posts[0], "固定しました") {
			t.Fatalf("system post = %s", posts[0])
		}

		h.lock(spaceID, pa, false)
		if posts := h.partySystemPosts(spaceID); len(posts) != 2 {
			t.Fatalf("system posts after unlock = %d, want 2", len(posts))
		}
	})
}

func TestPartyConfirmedNotification(t *testing.T) {
	ctx := context.Background()

	t.Run("confirmed でメンバーに通知が積まれる", func(t *testing.T) {
		h := newPartyHarness(t)
		h.users(pa, pb)
		h.friend(pa, pb)
		h.confirm(pa, pb)

		for _, did := range []string{pa, pb} {
			var n int
			if err := h.env.DB.QueryRowContext(ctx,
				"SELECT count(*) FROM notifications WHERE did = ? AND type = ?", did, notify.NotificationPartyConfirmed).Scan(&n); err != nil {
				t.Fatalf("count: %v", err)
			}
			if n != 1 {
				t.Fatalf("party.confirmed for %s = %d, want 1", did, n)
			}
		}
	})

	t.Run("notify_json で off なら積まない", func(t *testing.T) {
		h := newPartyHarness(t)
		h.users(pa, pb)
		h.friend(pa, pb)
		testutil.Exec(t, h.env, "UPDATE user_settings SET notify_json = ? WHERE did = ?", `{"party.confirmed":false}`, pb)

		h.confirm(pa, pb)

		var n int
		if err := h.env.DB.QueryRowContext(ctx,
			"SELECT count(*) FROM notifications WHERE did = ? AND type = ?", pb, notify.NotificationPartyConfirmed).Scan(&n); err != nil {
			t.Fatalf("count: %v", err)
		}
		if n != 0 {
			t.Fatalf("party.confirmed for %s = %d, want 0", pb, n)
		}
	})
}
