//go:build integration

package service_test

import (
	"context"
	"encoding/binary"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/applemango/chain/internal/db"
	"github.com/applemango/chain/internal/kv"
	"github.com/applemango/chain/internal/model"
	"github.com/applemango/chain/internal/service"
	"github.com/applemango/chain/internal/testutil"
)

const (
	naturalLat   = 35.0
	naturalLon   = 135.0
	naturalDate  = "2026-10-17"
	naturalStart = 540 // 09:00 JST（Epoch = 2026-10-17 00:00 UTC）
	naturalEnd   = 600 // 10:00 JST
	eve          = "did:plc:eve"
)

var (
	vecClassA = []float32{1, 0, 0}
	vecClassB = []float32{0.98, 0.2, 0}
	vecOther  = []float32{0, 0, 1}
)

type naturalHarness struct {
	*groupHarness
	ngs    *service.NaturalGroupService
	broker *testutil.FakeBroker
}

func newNaturalHarness(t *testing.T) *naturalHarness {
	t.Helper()
	h := newGroupHarness(t)
	broker := testutil.NewFakeBroker()
	return &naturalHarness{
		groupHarness: h,
		ngs:          &service.NaturalGroupService{Deps: h.env.Deps, Moderator: service.ModerationNoop{}, Broker: broker},
		broker:       broker,
	}
}

func (h *naturalHarness) tick() {
	h.t.Helper()
	if err := h.ngs.Tick(context.Background()); err != nil {
		h.t.Fatalf("Tick: %v", err)
	}
}

func (h *naturalHarness) entry(did, title string, startMin, endMin int, vec []float32) {
	h.t.Helper()
	_, err := db.Execute(context.Background(), h.env.DB, `
		INSERT INTO schedule_entries (id, did, date, source, start_min, end_min, title, embedding, embed_model, created_at)
		VALUES (?, ?, ?, 'manual', ?, ?, ?, ?, 'test', ?)`,
		h.env.IDs.NewID(), did, naturalDate, startMin, endMin, title, encodeVec(vec), h.env.Clock.Now().UnixMilli())
	if err != nil {
		h.t.Fatalf("schedule_entries (%s): %v", did, err)
	}
}

func (h *naturalHarness) setNaturalGroups(did string, on bool) {
	h.t.Helper()
	if _, err := db.Execute(context.Background(), h.env.DB,
		"UPDATE user_settings SET natural_groups = ? WHERE did = ?", on, did); err != nil {
		h.t.Fatalf("set natural_groups (%s): %v", did, err)
	}
}

func (h *naturalHarness) naturalGroups() []model.GroupWithSpace {
	h.t.Helper()
	all, err := model.ListActiveGroups(context.Background(), h.env.DB)
	if err != nil {
		h.t.Fatalf("ListActiveGroups: %v", err)
	}
	var out []model.GroupWithSpace
	for _, g := range all {
		if g.Origin == "natural" {
			out = append(out, g)
		}
	}
	return out
}

func (h *naturalHarness) activeMember(spaceID, did string) bool {
	h.t.Helper()
	m, err := model.GetSpaceMember(context.Background(), h.env.DB, spaceID, did)
	if err != nil {
		return false
	}
	return m.LeftAt == nil
}

func (h *naturalHarness) autoJoined(did string) int {
	h.t.Helper()
	var n int
	if err := h.env.DB.QueryRowContext(context.Background(),
		"SELECT count(*) FROM notifications WHERE did = ? AND type = ?",
		did, service.NotificationGroupAutoJoined).Scan(&n); err != nil {
		h.t.Fatalf("count notifications (%s): %v", did, err)
	}
	return n
}

func (h *naturalHarness) candidateKeys() []string {
	h.t.Helper()
	keys, _, err := h.env.Redis.Scan(context.Background(), 0, kv.NaturalCandidatePrefix()+"*", 100).Result()
	if err != nil {
		h.t.Fatalf("Scan ng:cand: %v", err)
	}
	return keys
}

func encodeVec(v []float32) []byte {
	b := make([]byte, len(v)*4)
	for i, x := range v {
		binary.LittleEndian.PutUint32(b[i*4:], math.Float32bits(x))
	}
	return b
}

// threeUsers は同じ時間帯・同じ場所で似た予定を持つ 3 人を用意する。
func (h *naturalHarness) threeUsers(vec []float32) {
	h.user(alice)
	h.user(bob)
	h.user(carol)
	for _, did := range []string{alice, bob, carol} {
		h.setLocation(did, naturalLat, naturalLon)
		h.entry(did, "線形代数 II", naturalStart, naturalEnd, vec)
	}
}

func TestNaturalGroupCreatedOnThirdTick(t *testing.T) {
	h := newNaturalHarness(t)
	h.threeUsers(vecClassA)

	h.tick()
	if len(h.naturalGroups()) != 0 {
		t.Fatal("1 回目の tick でグループができている")
	}
	if len(h.candidateKeys()) != 1 {
		t.Fatalf("1 回目の候補 = %v, want 1 件", h.candidateKeys())
	}

	h.tick()
	if len(h.naturalGroups()) != 0 {
		t.Fatal("2 回目の tick でグループができている")
	}

	h.tick()
	groups := h.naturalGroups()
	if len(groups) != 1 {
		t.Fatalf("3 回目のグループ = %d, want 1", len(groups))
	}
	g := groups[0]
	if len(h.candidateKeys()) != 0 {
		t.Fatalf("作成後に候補が残っている: %v", h.candidateKeys())
	}
	for _, did := range []string{alice, bob, carol} {
		if !h.activeMember(g.SpaceID, did) {
			t.Fatalf("%s が member になっていない", did)
		}
		if h.autoJoined(did) != 1 {
			t.Fatalf("%s の group.auto_joined = %d, want 1", did, h.autoJoined(did))
		}
	}
	if len(h.broker.EventsFor("member.joined", "")) != 3 {
		t.Fatalf("member.joined = %d, want 3", len(h.broker.EventsFor("member.joined", "")))
	}
	if g.RadiusM < service.NaturalRadiusMin || g.RadiusM > service.NaturalRadiusMax {
		t.Fatalf("半径 = %d, want %d〜%d", g.RadiusM, service.NaturalRadiusMin, service.NaturalRadiusMax)
	}
}

func TestNaturalGroupCandidateClearedWhenGone(t *testing.T) {
	h := newNaturalHarness(t)
	h.threeUsers(vecClassA)
	h.tick()
	h.tick()
	if len(h.candidateKeys()) != 1 {
		t.Fatalf("2 回目の候補 = %v, want 1 件", h.candidateKeys())
	}

	h.env.Clock.Advance(2 * time.Hour) // 10:00 を過ぎて対象の予定がなくなる
	h.tick()
	if len(h.candidateKeys()) != 0 {
		t.Fatalf("見つからなかった候補が消えていない: %v", h.candidateKeys())
	}
	if len(h.naturalGroups()) != 0 {
		t.Fatal("候補が消えたのにグループができている")
	}
}

func TestNaturalGroupDifferentScheduleNotJoined(t *testing.T) {
	h := newNaturalHarness(t)
	h.threeUsers(vecClassA)
	h.user(dave)
	h.setLocation(dave, naturalLat, naturalLon)
	h.entry(dave, "まったく別の予定", naturalStart, naturalEnd, vecOther)

	for i := 0; i < 3; i++ {
		h.tick()
	}
	groups := h.naturalGroups()
	if len(groups) != 1 {
		t.Fatalf("グループ = %d, want 1", len(groups))
	}
	if h.activeMember(groups[0].SpaceID, dave) {
		t.Fatal("似ていない予定の人が参加している")
	}
}

func TestNaturalGroupRespectsOptOut(t *testing.T) {
	h := newNaturalHarness(t)
	h.threeUsers(vecClassA)
	h.user(dave)
	h.setLocation(dave, naturalLat, naturalLon)
	h.entry(dave, "線形代数 II", naturalStart, naturalEnd, vecClassA)
	h.setNaturalGroups(dave, false)

	for i := 0; i < 4; i++ {
		h.tick()
	}
	groups := h.naturalGroups()
	if len(groups) != 1 {
		t.Fatalf("グループ = %d, want 1", len(groups))
	}
	if h.activeMember(groups[0].SpaceID, dave) {
		t.Fatal("natural_groups = 0 の人が参加している")
	}
}

func TestNaturalGroupStaleLocationExcluded(t *testing.T) {
	h := newNaturalHarness(t)
	h.user(alice)
	h.user(bob)
	h.user(carol)
	h.setLocation(alice, naturalLat, naturalLon)
	h.setLocation(bob, naturalLat, naturalLon)
	h.setLocation(carol, naturalLat, naturalLon)
	for _, did := range []string{alice, bob, carol} {
		h.entry(did, "線形代数 II", naturalStart, naturalEnd, vecClassA)
	}

	h.env.Clock.Advance(6 * time.Minute) // carol の位置だけ古くする
	h.setLocation(alice, naturalLat, naturalLon)
	h.setLocation(bob, naturalLat, naturalLon)

	for i := 0; i < 3; i++ {
		h.tick()
	}
	if len(h.naturalGroups()) != 0 {
		t.Fatal("位置が古い人を含めてグループができている")
	}
}

func TestNaturalGroupJoinsExistingOpenGroup(t *testing.T) {
	h := newNaturalHarness(t)
	h.user(eve)
	h.setLocation(eve, naturalLat, naturalLon)
	existing := h.create(eve, service.CreateGroupInput{Name: "既存の授業", Lat: naturalLat, Lon: naturalLon, RadiusM: 200})
	h.entry(eve, "線形代数 II", naturalStart, naturalEnd, vecClassA)

	h.threeUsers(vecClassA)
	h.tick()

	if len(h.naturalGroups()) != 0 {
		t.Fatal("合流できたのに新しい自動グループができている")
	}
	for _, did := range []string{alice, bob, carol} {
		if !h.activeMember(existing.ID, did) {
			t.Fatalf("%s が既存グループに参加していない", did)
		}
		if h.autoJoined(did) != 1 {
			t.Fatalf("%s の group.auto_joined = %d, want 1", did, h.autoJoined(did))
		}
	}
}

func TestNaturalGroupLaterArrivalJoins(t *testing.T) {
	h := newNaturalHarness(t)
	h.threeUsers(vecClassA)
	for i := 0; i < 3; i++ {
		h.tick()
	}
	groups := h.naturalGroups()
	if len(groups) != 1 {
		t.Fatalf("グループ = %d, want 1", len(groups))
	}

	h.user(dave)
	h.setLocation(dave, naturalLat, naturalLon)
	h.entry(dave, "線形代数 II", naturalStart, naturalEnd, vecClassA)
	h.tick()

	if !h.activeMember(groups[0].SpaceID, dave) {
		t.Fatal("あとから来た人が自動参加していない")
	}
	if h.autoJoined(dave) != 1 {
		t.Fatalf("dave の group.auto_joined = %d, want 1", h.autoJoined(dave))
	}
}

func TestNaturalGroupDoesNotRejoinAfterLeaving(t *testing.T) {
	h := newNaturalHarness(t)
	h.threeUsers(vecClassA)
	for i := 0; i < 3; i++ {
		h.tick()
	}
	groups := h.naturalGroups()
	if len(groups) != 1 {
		t.Fatalf("グループ = %d, want 1", len(groups))
	}
	id := groups[0].SpaceID
	if err := h.svc.Leave(context.Background(), alice, id); err != nil {
		t.Fatalf("Leave: %v", err)
	}
	h.tick()

	if h.activeMember(id, alice) {
		t.Fatal("退出した人が自動で戻っている")
	}
	m, err := model.GetSpaceMember(context.Background(), h.env.DB, id, alice)
	if err != nil || m.LeftAt == nil {
		t.Fatalf("alice の left_at が残っていない: %+v (err=%v)", m, err)
	}
	if len(h.naturalGroups()) != 1 {
		t.Fatal("退出後に別のグループができている")
	}
}

func TestNaturalGroupArchivedAfterEnd(t *testing.T) {
	h := newNaturalHarness(t)
	h.threeUsers(vecClassA)
	for i := 0; i < 3; i++ {
		h.tick()
	}
	groups := h.naturalGroups()
	if len(groups) != 1 {
		t.Fatalf("グループ = %d, want 1", len(groups))
	}
	id := groups[0].SpaceID

	h.env.Clock.Advance(2 * time.Hour) // 10:30 の猶予を過ぎ、位置も古くなる
	h.tick()

	if state := h.spaceState(id); state != "archived" {
		t.Fatalf("state = %q, want archived", state)
	}
	if _, err := h.env.Redis.ZScore(context.Background(), kv.GeoGroupsKey(), id).Result(); !errors.Is(err, redis.Nil) {
		t.Fatalf("geo:groups から外れていない (err=%v)", err)
	}
	if len(h.broker.EventsFor("space.archived", "")) != 1 {
		t.Fatalf("space.archived = %d, want 1", len(h.broker.EventsFor("space.archived", "")))
	}
}

func TestNaturalGroupBlockedNameFallback(t *testing.T) {
	h := newNaturalHarness(t)
	h.ngs.Moderator = &testutil.BlockingModerator{Words: []string{"blockme"}}
	h.user(alice)
	h.user(bob)
	h.user(carol)
	for _, did := range []string{alice, bob, carol} {
		h.setLocation(did, naturalLat, naturalLon)
		h.entry(did, "blockme の予定", naturalStart, naturalEnd, vecClassA)
	}
	for i := 0; i < 3; i++ {
		h.tick()
	}
	groups := h.naturalGroups()
	if len(groups) != 1 {
		t.Fatalf("グループ = %d, want 1", len(groups))
	}
	if groups[0].Name != "近くのグループ" {
		t.Fatalf("name = %q, want 近くのグループ", groups[0].Name)
	}
}

func TestNaturalGroupNameTruncated(t *testing.T) {
	h := newNaturalHarness(t)
	long := ""
	for i := 0; i < 60; i++ {
		long += "あ"
	}
	h.user(alice)
	h.user(bob)
	h.user(carol)
	for _, did := range []string{alice, bob, carol} {
		h.setLocation(did, naturalLat, naturalLon)
		h.entry(did, long, naturalStart, naturalEnd, vecClassA)
	}
	for i := 0; i < 3; i++ {
		h.tick()
	}
	groups := h.naturalGroups()
	if len(groups) != 1 {
		t.Fatalf("グループ = %d, want 1", len(groups))
	}
	if n := len([]rune(groups[0].Name)); n != 40 {
		t.Fatalf("name の長さ = %d, want 40", n)
	}
}

func TestNaturalGroupSimilarDifferentVectors(t *testing.T) {
	h := newNaturalHarness(t)
	h.user(alice)
	h.user(bob)
	h.user(carol)
	h.setLocation(alice, naturalLat, naturalLon)
	h.setLocation(bob, naturalLat, naturalLon)
	h.setLocation(carol, naturalLat, naturalLon)
	h.entry(alice, "線形代数", naturalStart, naturalEnd, vecClassA)
	h.entry(bob, "線形代数学", naturalStart, naturalEnd, vecClassB)
	h.entry(carol, "線形代数 II", naturalStart, naturalEnd, vecClassA)

	for i := 0; i < 3; i++ {
		h.tick()
	}
	if len(h.naturalGroups()) != 1 {
		t.Fatal("cos 類似度が高い別ベクトルが同じ成分になっていない")
	}
}
