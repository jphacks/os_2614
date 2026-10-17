//go:build integration

package natural_test

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
	groupmodel "github.com/applemango/chain/internal/model/group"
	spacemodel "github.com/applemango/chain/internal/model/space"
	core "github.com/applemango/chain/internal/service/core"
	groupsvc "github.com/applemango/chain/internal/service/group"
	"github.com/applemango/chain/internal/service/moderation"
	naturalsvc "github.com/applemango/chain/internal/service/natural"
	"github.com/applemango/chain/internal/service/notify"
	"github.com/applemango/chain/internal/testutil"
)

const (
	alice = "did:plc:alice"
	bob   = "did:plc:bob"
	carol = "did:plc:carol"
	dave  = "did:plc:dave"
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
	*testutil.GroupHarness
	ngs    *naturalsvc.NaturalGroupService
	broker *testutil.FakeBroker
}

func newNaturalHarness(t *testing.T) *naturalHarness {
	t.Helper()
	h := testutil.NewGroupHarness(t)
	broker := testutil.NewFakeBroker()
	return &naturalHarness{
		GroupHarness: h,
		ngs:          &naturalsvc.NaturalGroupService{Deps: h.Env.Deps, Moderator: moderation.ModerationNoop{}, Broker: broker},
		broker:       broker,
	}
}

func (h *naturalHarness) tick() {
	h.T.Helper()
	if err := h.ngs.Tick(context.Background()); err != nil {
		h.T.Fatalf("Tick: %v", err)
	}
}

func (h *naturalHarness) entry(did, title string, startMin, endMin int, vec []float32) {
	h.T.Helper()
	_, err := db.Execute(context.Background(), h.Env.DB, `
		INSERT INTO schedule_entries (id, did, date, source, start_min, end_min, title, embedding, embed_model, created_at)
		VALUES (?, ?, ?, 'manual', ?, ?, ?, ?, 'test', ?)`,
		h.Env.IDs.NewID(), did, naturalDate, startMin, endMin, title, encodeVec(vec), h.Env.Clock.Now().UnixMilli())
	if err != nil {
		h.T.Fatalf("schedule_entries (%s): %v", did, err)
	}
}

func (h *naturalHarness) setNaturalGroups(did string, on bool) {
	h.T.Helper()
	if _, err := db.Execute(context.Background(), h.Env.DB,
		"UPDATE user_settings SET natural_groups = ? WHERE did = ?", on, did); err != nil {
		h.T.Fatalf("set natural_groups (%s): %v", did, err)
	}
}

func (h *naturalHarness) naturalGroups() []groupmodel.GroupWithSpace {
	h.T.Helper()
	all, err := groupmodel.ListActiveGroups(context.Background(), h.Env.DB)
	if err != nil {
		h.T.Fatalf("ListActiveGroups: %v", err)
	}
	var out []groupmodel.GroupWithSpace
	for _, g := range all {
		if g.Origin == "natural" {
			out = append(out, g)
		}
	}
	return out
}

func (h *naturalHarness) activeMember(spaceID, did string) bool {
	h.T.Helper()
	m, err := spacemodel.GetSpaceMember(context.Background(), h.Env.DB, spaceID, did)
	if err != nil {
		return false
	}
	return m.LeftAt == nil
}

func (h *naturalHarness) autoJoined(did string) int {
	h.T.Helper()
	var n int
	if err := h.Env.DB.QueryRowContext(context.Background(),
		"SELECT count(*) FROM notifications WHERE did = ? AND type = ?",
		did, notify.NotificationGroupAutoJoined).Scan(&n); err != nil {
		h.T.Fatalf("count notifications (%s): %v", did, err)
	}
	return n
}

func (h *naturalHarness) candidateKeys() []string {
	h.T.Helper()
	keys, _, err := h.Env.Redis.Scan(context.Background(), 0, kv.NaturalCandidatePrefix()+"*", 100).Result()
	if err != nil {
		h.T.Fatalf("Scan ng:cand: %v", err)
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
	h.User(alice)
	h.User(bob)
	h.User(carol)
	for _, did := range []string{alice, bob, carol} {
		h.SetLocation(did, naturalLat, naturalLon)
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
	if g.RadiusM < core.NaturalRadiusMin || g.RadiusM > core.NaturalRadiusMax {
		t.Fatalf("半径 = %d, want %d〜%d", g.RadiusM, core.NaturalRadiusMin, core.NaturalRadiusMax)
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

	h.Env.Clock.Advance(2 * time.Hour) // 10:00 を過ぎて対象の予定がなくなる
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
	h.User(dave)
	h.SetLocation(dave, naturalLat, naturalLon)
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
	h.User(dave)
	h.SetLocation(dave, naturalLat, naturalLon)
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
	h.User(alice)
	h.User(bob)
	h.User(carol)
	h.SetLocation(alice, naturalLat, naturalLon)
	h.SetLocation(bob, naturalLat, naturalLon)
	h.SetLocation(carol, naturalLat, naturalLon)
	for _, did := range []string{alice, bob, carol} {
		h.entry(did, "線形代数 II", naturalStart, naturalEnd, vecClassA)
	}

	h.Env.Clock.Advance(6 * time.Minute) // carol の位置だけ古くする
	h.SetLocation(alice, naturalLat, naturalLon)
	h.SetLocation(bob, naturalLat, naturalLon)

	for i := 0; i < 3; i++ {
		h.tick()
	}
	if len(h.naturalGroups()) != 0 {
		t.Fatal("位置が古い人を含めてグループができている")
	}
}

func TestNaturalGroupJoinsExistingOpenGroup(t *testing.T) {
	h := newNaturalHarness(t)
	h.User(eve)
	h.SetLocation(eve, naturalLat, naturalLon)
	existing := h.Create(eve, groupsvc.CreateGroupInput{Name: "既存の授業", Lat: naturalLat, Lon: naturalLon, RadiusM: 200})
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

	h.User(dave)
	h.SetLocation(dave, naturalLat, naturalLon)
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
	if err := h.Svc.Leave(context.Background(), alice, id); err != nil {
		t.Fatalf("Leave: %v", err)
	}
	h.tick()

	if h.activeMember(id, alice) {
		t.Fatal("退出した人が自動で戻っている")
	}
	m, err := spacemodel.GetSpaceMember(context.Background(), h.Env.DB, id, alice)
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

	h.Env.Clock.Advance(2 * time.Hour) // 10:30 の猶予を過ぎ、位置も古くなる
	h.tick()

	if state := h.SpaceState(id); state != "archived" {
		t.Fatalf("state = %q, want archived", state)
	}
	if _, err := h.Env.Redis.ZScore(context.Background(), kv.GeoGroupsKey(), id).Result(); !errors.Is(err, redis.Nil) {
		t.Fatalf("geo:groups から外れていない (err=%v)", err)
	}
	if len(h.broker.EventsFor("space.archived", "")) != 1 {
		t.Fatalf("space.archived = %d, want 1", len(h.broker.EventsFor("space.archived", "")))
	}
}

func TestNaturalGroupBlockedNameFallback(t *testing.T) {
	h := newNaturalHarness(t)
	h.ngs.Moderator = &testutil.BlockingModerator{Words: []string{"blockme"}}
	h.User(alice)
	h.User(bob)
	h.User(carol)
	for _, did := range []string{alice, bob, carol} {
		h.SetLocation(did, naturalLat, naturalLon)
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
	h.User(alice)
	h.User(bob)
	h.User(carol)
	for _, did := range []string{alice, bob, carol} {
		h.SetLocation(did, naturalLat, naturalLon)
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
	h.User(alice)
	h.User(bob)
	h.User(carol)
	h.SetLocation(alice, naturalLat, naturalLon)
	h.SetLocation(bob, naturalLat, naturalLon)
	h.SetLocation(carol, naturalLat, naturalLon)
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
