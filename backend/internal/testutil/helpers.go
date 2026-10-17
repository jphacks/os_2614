package testutil

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/applemango/chain/internal/httpx"
	"github.com/applemango/chain/internal/model"
	"github.com/applemango/chain/internal/service/access"
	core "github.com/applemango/chain/internal/service/core"
	groupsvc "github.com/applemango/chain/internal/service/group"
	"github.com/applemango/chain/internal/service/moderation"
	"github.com/applemango/chain/internal/service/proximity"
)

// 複数の機能のテストが共有する小さな道具。SQL を直接書いて前提を整えるものと、
// エラーの形を確かめるものを置く。

// Exec は SQL を実行し、失敗したらテストを止める。
func Exec(t *testing.T, env *Env, query string, args ...any) {
	t.Helper()
	if _, err := env.DB.ExecContext(context.Background(), query, args...); err != nil {
		t.Fatalf("exec %q: %v", query, err)
	}
}

// MkSpace は spaces の行を 1 つ作る。
func MkSpace(t *testing.T, env *Env, id, typ, state string) {
	t.Helper()
	now := env.Clock.Now().UnixMilli()
	Exec(t, env, `INSERT INTO spaces (id, type, authority, ref, name, state, created_by, created_at)
		VALUES (?, ?, 'did:web:chain.test', ?, ?, ?, ?, ?)`,
		id, typ, "at://did:web:chain.test/space/"+typ+"/"+id, "space "+id, state, "did:plc:member", now)
}

// MkGroup は groups の行を 1 つ作る。
func MkGroup(t *testing.T, env *Env, id string, persistent bool, schedule *string) {
	t.Helper()
	Exec(t, env, `INSERT INTO groups (space_id, description, visibility, origin, persistent, center_lat, center_lon, radius_m, schedule_json)
		VALUES (?, '', 'open', 'explicit', ?, 35.0, 135.0, 40, ?)`, id, persistent, schedule)
}

// MkMember は space_members の行を 1 つ作る。
func MkMember(t *testing.T, env *Env, id, did string, canRead, canWrite, left bool) {
	t.Helper()
	now := env.Clock.Now().UnixMilli()
	var leftAt any
	if left {
		leftAt = now
	}
	Exec(t, env, `INSERT INTO space_members (space_id, did, can_read, can_write, role, joined_at, left_at)
		VALUES (?, ?, ?, ?, 'member', ?, ?)`, id, did, canRead, canWrite, now, leftAt)
}

var jstZone = time.FixedZone("JST", 9*60*60)

// JSTTime は日本時間の壁時計で時刻を作る。
func JSTTime(y int, mo time.Month, d, h, mi int) time.Time {
	return time.Date(y, mo, d, h, mi, 0, 0, jstZone)
}

// InsertEncounter は encounters の行を 1 つ作る。user_a / user_b は昇順にそろえる。
func InsertEncounter(t *testing.T, env *Env, id, a, b string, at time.Time, visibility string) {
	t.Helper()
	aa, bb := a, b
	if aa > bb {
		aa, bb = bb, aa
	}
	if _, err := env.DB.ExecContext(context.Background(), `
		INSERT INTO encounters (id, user_a, user_b, established_at, first_seen_at, last_seen_at, visibility)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, aa, bb, at.UnixMilli(), at.UnixMilli(), at.UnixMilli(), visibility); err != nil {
		t.Fatalf("insert encounter %s: %v", id, err)
	}
}

// WantCode は err が httpx.Error で code を持つことを確かめる。
func WantCode(t *testing.T, err error, code string) {
	t.Helper()
	var e *httpx.Error
	if !errors.As(err, &e) {
		t.Fatalf("err = %v, want *httpx.Error code %q", err, code)
	}
	if e.Code != code {
		t.Fatalf("code = %q, want %q", e.Code, code)
	}
}

// WantHTTPError は err が httpx.Error で code を持つことを確かめる。
func WantHTTPError(t *testing.T, err error, code string) {
	t.Helper()
	var e *httpx.Error
	if !errors.As(err, &e) {
		t.Fatalf("err = %v, want *httpx.Error with code %q", err, code)
	}
	if e.Code != code {
		t.Fatalf("err code = %q, want %q", e.Code, code)
	}
}

// SessionID は did の直近のアプリセッションの id を返す。
func SessionID(t *testing.T, env *Env, did string) string {
	t.Helper()
	var sid string
	if err := env.DB.QueryRowContext(context.Background(),
		"SELECT id FROM app_sessions WHERE did = ? ORDER BY created_at DESC, id DESC LIMIT 1", did).Scan(&sid); err != nil {
		t.Fatalf("select sid: %v", err)
	}
	return sid
}

// GroupHarness はグループ（09）のテストの土台。natural のテストもこれを使う。
type GroupHarness struct {
	T      *testing.T
	Env    *Env
	Svc    *groupsvc.GroupService
	Access *access.AccessService
}

// NewGroupHarness は group のテスト環境を作る。
func NewGroupHarness(t *testing.T) *GroupHarness {
	t.Helper()
	env := NewEnv(t)
	zones := &proximity.GPSZonePresence{Deps: env.Deps}
	acc := &access.AccessService{Deps: env.Deps, Zones: zones}
	return &GroupHarness{
		T:      t,
		Env:    env,
		Access: acc,
		Svc:    &groupsvc.GroupService{Deps: env.Deps, Zones: zones, Access: acc, Moderator: moderation.ModerationNoop{}},
	}
}

// User はユーザーを 1 人作る。
func (h *GroupHarness) User(did string) {
	h.T.Helper()
	h.Env.CreateUser(h.T, did, did[8:]+".test")
}

// SetLocation は位置を Redis に直接入れる。
func (h *GroupHarness) SetLocation(did string, lat, lon float64) {
	h.T.Helper()
	loc := model.Location{DID: did, Lat: lat, Lon: lon, AccuracyM: 5, RecordedAt: h.Env.Clock.Now(), Source: "gps"}
	if err := model.SaveLocation(context.Background(), h.Env.Redis, loc, core.LocationTTL); err != nil {
		h.T.Fatalf("SaveLocation(%s): %v", did, err)
	}
}

// Create はグループを作る。
func (h *GroupHarness) Create(did string, in groupsvc.CreateGroupInput) *groupsvc.GroupDetail {
	h.T.Helper()
	d, err := h.Svc.Create(context.Background(), did, in)
	if err != nil {
		h.T.Fatalf("Create(%s): %v", did, err)
	}
	return d
}

// ParentOf はグループの親 space を返す。
func (h *GroupHarness) ParentOf(spaceID string) *string {
	h.T.Helper()
	var parent *string
	if err := h.Env.DB.QueryRowContext(context.Background(),
		"SELECT parent_space_id FROM groups WHERE space_id = ?", spaceID).Scan(&parent); err != nil {
		h.T.Fatalf("parent of %s: %v", spaceID, err)
	}
	return parent
}

// MemberRole はメンバーの role を返す。
func (h *GroupHarness) MemberRole(spaceID, did string) string {
	h.T.Helper()
	var role string
	if err := h.Env.DB.QueryRowContext(context.Background(),
		"SELECT role FROM space_members WHERE space_id = ? AND did = ?", spaceID, did).Scan(&role); err != nil {
		h.T.Fatalf("role of %s in %s: %v", did, spaceID, err)
	}
	return role
}

// SpaceState は space の state を返す。
func (h *GroupHarness) SpaceState(spaceID string) string {
	h.T.Helper()
	var state string
	if err := h.Env.DB.QueryRowContext(context.Background(),
		"SELECT state FROM spaces WHERE id = ?", spaceID).Scan(&state); err != nil {
		h.T.Fatalf("state of %s: %v", spaceID, err)
	}
	return state
}
