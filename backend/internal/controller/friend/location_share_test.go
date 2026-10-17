//go:build integration

package friend_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/applemango/chain/internal/model"
	core "github.com/applemango/chain/internal/service/core"
	"github.com/applemango/chain/internal/testutil"
)

type areaItemResponse struct {
	ID        string    `json:"id"`
	Label     string    `json:"label"`
	Lat       float64   `json:"lat"`
	Lon       float64   `json:"lon"`
	RadiusM   int       `json:"radiusM"`
	CreatedAt time.Time `json:"createdAt"`
}

type areaListResponse struct {
	Items []areaItemResponse `json:"items"`
}

type friendLocationItem struct {
	DID       string   `json:"did"`
	Precision string   `json:"precision"`
	Lat       *float64 `json:"lat"`
	Lon       *float64 `json:"lon"`
	AccuracyM *float64 `json:"accuracyM"`
	Area      *struct {
		Label string `json:"label"`
	} `json:"area"`
	Outside        bool       `json:"outside"`
	Direction      string     `json:"direction"`
	DistanceBucket string     `json:"distanceBucket"`
	ViewerStale    bool       `json:"viewerStale"`
	UpdatedAt      *time.Time `json:"updatedAt"`
	Stale          bool       `json:"stale"`
}

type friendLocationsResponse struct {
	Items []friendLocationItem `json:"items"`
}

// beFriend は inviter の招待コードを accepter が使ってフレンドにする。
func beFriend(t *testing.T, app *fiber.App, inviterToken, accepterToken string) {
	t.Helper()
	inv := createInvite(t, app, inviterToken)
	status, body := testutil.Request(t, app, http.MethodPost, "/api/v1/friends/invites/"+inv.Code+"/accept", "", accepterToken)
	if status != http.StatusOK {
		t.Fatalf("accept invite: status = %d, want 200 (body=%s)", status, body)
	}
}

func putLocation(t *testing.T, app *fiber.App, token string, lat, lon, acc float64, at time.Time) {
	t.Helper()
	body := fmt.Sprintf(`{"lat":%v,"lon":%v,"accuracyM":%v,"recordedAt":%q,"source":"gps"}`,
		lat, lon, acc, at.Format(time.RFC3339Nano))
	status, resp := testutil.Request(t, app, http.MethodPut, "/api/v1/me/location", body, token)
	if status != http.StatusNoContent {
		t.Fatalf("put location: status = %d, want 204 (body=%s)", status, resp)
	}
}

func setPrecision(t *testing.T, app *fiber.App, token, did, precision string, areasOnly bool) (int, []byte) {
	t.Helper()
	body := fmt.Sprintf(`{"precision":%q,"areasOnly":%v}`, precision, areasOnly)
	return testutil.Request(t, app, http.MethodPut, "/api/v1/friends/"+did+"/location-share", body, token)
}

func getFriendLocations(t *testing.T, app *fiber.App, token string) friendLocationsResponse {
	t.Helper()
	status, body := testutil.Request(t, app, http.MethodGet, "/api/v1/friends/locations", "", token)
	if status != http.StatusOK {
		t.Fatalf("get friend locations: status = %d, want 200 (body=%s)", status, body)
	}
	var out friendLocationsResponse
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decode friend locations: %v (body=%s)", err, body)
	}
	return out
}

func findLocation(t *testing.T, items []friendLocationItem, did string) friendLocationItem {
	t.Helper()
	for _, it := range items {
		if it.DID == did {
			return it
		}
	}
	t.Fatalf("location for %s not found in %+v", did, items)
	return friendLocationItem{}
}

func locationSharePrecision(t *testing.T, env *testutil.Env, owner, viewer string) (string, bool) {
	t.Helper()
	var p string
	err := env.DB.QueryRowContext(context.Background(),
		"SELECT precision FROM location_shares WHERE owner = ? AND viewer = ?", owner, viewer).Scan(&p)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false
	}
	if err != nil {
		t.Fatalf("query location_shares: %v", err)
	}
	return p, true
}

func locationShareRow(t *testing.T, env *testutil.Env, owner, viewer string) (string, bool) {
	t.Helper()
	var p string
	var only bool
	err := env.DB.QueryRowContext(context.Background(),
		"SELECT precision, areas_only FROM location_shares WHERE owner = ? AND viewer = ?", owner, viewer).Scan(&p, &only)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false
	}
	if err != nil {
		t.Fatalf("query location_shares: %v", err)
	}
	return p, only
}

func createArea(t *testing.T, app *fiber.App, token, label string, lat, lon float64, radiusM int) areaItemResponse {
	t.Helper()
	body := fmt.Sprintf(`{"label":%q,"lat":%v,"lon":%v,"radiusM":%d}`, label, lat, lon, radiusM)
	status, resp := testutil.Request(t, app, http.MethodPost, "/api/v1/me/areas", body, token)
	if status != http.StatusCreated {
		t.Fatalf("create area: status = %d, want 201 (body=%s)", status, resp)
	}
	var a areaItemResponse
	if err := json.Unmarshal(resp, &a); err != nil {
		t.Fatalf("decode area: %v (body=%s)", err, resp)
	}
	return a
}

func TestFriendLocationsPrecision(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)
	env.CreateUser(t, "did:plc:alice", "alice.test")
	env.CreateUser(t, "did:plc:bob", "bob.test")
	aliceToken := env.Login(t, "did:plc:alice")
	bobToken := env.Login(t, "did:plc:bob")
	beFriend(t, app, aliceToken, bobToken)

	const atLat, atLon = 35.0261345, 135.7808949
	// 分単位に丸められることを確かめるため、記録時刻を 30 秒ずらす。
	putLocation(t, app, bobToken, atLat, atLon, 12, env.Clock.Now().Add(30*time.Second))

	// exact: 小数 5 桁に丸めた緯度経度と精度。
	if status, body := setPrecision(t, app, bobToken, "did:plc:alice", "exact", false); status != http.StatusNoContent {
		t.Fatalf("set exact: status = %d (body=%s)", status, body)
	}
	item := findLocation(t, getFriendLocations(t, app, aliceToken).Items, "did:plc:bob")
	if item.Precision != "exact" || item.Lat == nil || item.Lon == nil || item.AccuracyM == nil {
		t.Fatalf("exact item = %+v", item)
	}
	if diff(*item.Lat, 35.02613) > 1e-6 || diff(*item.Lon, 135.78089) > 1e-6 {
		t.Fatalf("exact lat/lon = %v,%v, want 35.02613,135.78089", *item.Lat, *item.Lon)
	}
	if *item.AccuracyM != 12 {
		t.Fatalf("accuracyM = %v, want 12", *item.AccuracyM)
	}
	if item.UpdatedAt == nil || item.UpdatedAt.Second() != 0 || item.UpdatedAt.Nanosecond() != 0 {
		t.Fatalf("updatedAt = %v, want a whole minute", item.UpdatedAt)
	}
	if !item.UpdatedAt.Equal(env.Clock.Now().Truncate(time.Minute)) {
		t.Fatalf("updatedAt = %v, want %v", item.UpdatedAt, env.Clock.Now().Truncate(time.Minute))
	}

	// approx: geohash 6 桁のセル中心。
	if status, body := setPrecision(t, app, bobToken, "did:plc:alice", "approx", false); status != http.StatusNoContent {
		t.Fatalf("set approx: status = %d (body=%s)", status, body)
	}
	item = findLocation(t, getFriendLocations(t, app, aliceToken).Items, "did:plc:bob")
	wantLat, wantLon := core.GeohashCellCenter(atLat, atLon, 6)
	if item.Precision != "approx" || item.Lat == nil || item.Lon == nil {
		t.Fatalf("approx item = %+v", item)
	}
	if *item.Lat != wantLat || *item.Lon != wantLon {
		t.Fatalf("approx lat/lon = %v,%v, want %v,%v", *item.Lat, *item.Lon, wantLat, wantLon)
	}

	// off: 何も返さない。
	if status, body := setPrecision(t, app, bobToken, "did:plc:alice", "off", false); status != http.StatusNoContent {
		t.Fatalf("set off: status = %d (body=%s)", status, body)
	}
	item = findLocation(t, getFriendLocations(t, app, aliceToken).Items, "did:plc:bob")
	if item.Precision != "off" || item.Lat != nil || item.Lon != nil || item.Area != nil {
		t.Fatalf("off item = %+v", item)
	}

	// area: 今いるエリアの label。複数なら最も小さいエリア。
	if status, body := setPrecision(t, app, bobToken, "did:plc:alice", "area", false); status != http.StatusNoContent {
		t.Fatalf("set area: status = %d (body=%s)", status, body)
	}
	createArea(t, app, bobToken, "大学", atLat, atLon, 500)
	item = findLocation(t, getFriendLocations(t, app, aliceToken).Items, "did:plc:bob")
	if item.Precision != "area" || item.Area == nil || item.Area.Label != "大学" || item.Outside {
		t.Fatalf("area item = %+v", item)
	}
	createArea(t, app, bobToken, "図書館", atLat, atLon, 100)
	item = findLocation(t, getFriendLocations(t, app, aliceToken).Items, "did:plc:bob")
	if item.Area == nil || item.Area.Label != "図書館" {
		t.Fatalf("nested area item = %+v", item)
	}

	// どのエリアにもいなければ area: null, outside: true。
	putLocation(t, app, bobToken, 0, 0, 12, env.Clock.Now().Add(30*time.Second))
	item = findLocation(t, getFriendLocations(t, app, aliceToken).Items, "did:plc:bob")
	if item.Precision != "area" || item.Area != nil || !item.Outside {
		t.Fatalf("outside item = %+v", item)
	}
}

func TestFriendLocationsStaleAndPaused(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)
	env.CreateUser(t, "did:plc:alice", "alice.test")
	env.CreateUser(t, "did:plc:bob", "bob.test")
	aliceToken := env.Login(t, "did:plc:alice")
	bobToken := env.Login(t, "did:plc:bob")
	beFriend(t, app, aliceToken, bobToken)

	if status, body := setPrecision(t, app, bobToken, "did:plc:alice", "exact", false); status != http.StatusNoContent {
		t.Fatalf("set exact: status = %d (body=%s)", status, body)
	}

	// 位置がなければ stale。
	item := findLocation(t, getFriendLocations(t, app, aliceToken).Items, "did:plc:bob")
	if !item.Stale || item.Lat != nil {
		t.Fatalf("no-location item = %+v", item)
	}

	putLocation(t, app, bobToken, 35.0, 135.0, 10, env.Clock.Now())
	item = findLocation(t, getFriendLocations(t, app, aliceToken).Items, "did:plc:bob")
	if item.Stale || item.Lat == nil {
		t.Fatalf("fresh item = %+v", item)
	}

	// 30 分以上前の位置は stale（検証を通さず Redis に直接古い時刻を書く）。
	old := model.Location{DID: "did:plc:bob", Lat: 35.0, Lon: 135.0, AccuracyM: 10, RecordedAt: env.Clock.Now().Add(-31 * time.Minute), Source: "gps"}
	if err := model.SaveLocation(context.Background(), env.Redis, old, time.Hour); err != nil {
		t.Fatalf("save old location: %v", err)
	}
	item = findLocation(t, getFriendLocations(t, app, aliceToken).Items, "did:plc:bob")
	if !item.Stale || item.Lat != nil {
		t.Fatalf("stale item = %+v", item)
	}

	// 相手が一時停止すると off として見える。
	if status, body := testutil.Request(t, app, http.MethodPut, "/api/v1/me/location-sharing", `{"paused":true}`, bobToken); status != http.StatusNoContent {
		t.Fatalf("pause: status = %d (body=%s)", status, body)
	}
	item = findLocation(t, getFriendLocations(t, app, aliceToken).Items, "did:plc:bob")
	if item.Precision != "off" || item.Lat != nil || item.Stale {
		t.Fatalf("paused item = %+v", item)
	}
}

// 相手が自分に設定した精度が使われる（自分が相手に設定した精度ではない）。
func TestFriendLocationsUsesPeerPrecision(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)
	env.CreateUser(t, "did:plc:alice", "alice.test")
	env.CreateUser(t, "did:plc:bob", "bob.test")
	aliceToken := env.Login(t, "did:plc:alice")
	bobToken := env.Login(t, "did:plc:bob")
	beFriend(t, app, aliceToken, bobToken)

	// alice は bob に exact を見せるが、bob は alice に off を見せる。
	if status, body := setPrecision(t, app, aliceToken, "did:plc:bob", "exact", false); status != http.StatusNoContent {
		t.Fatalf("alice set exact: status = %d (body=%s)", status, body)
	}
	if status, body := setPrecision(t, app, bobToken, "did:plc:alice", "off", false); status != http.StatusNoContent {
		t.Fatalf("bob set off: status = %d (body=%s)", status, body)
	}
	putLocation(t, app, bobToken, 35.0, 135.0, 10, env.Clock.Now())

	item := findLocation(t, getFriendLocations(t, app, aliceToken).Items, "did:plc:bob")
	if item.Precision != "off" || item.Lat != nil {
		t.Fatalf("item = %+v, want off (bob の設定)", item)
	}
}

func TestFriendLocationsExcludesBlocked(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)
	env.CreateUser(t, "did:plc:alice", "alice.test")
	env.CreateUser(t, "did:plc:bob", "bob.test")
	aliceToken := env.Login(t, "did:plc:alice")
	bobToken := env.Login(t, "did:plc:bob")
	beFriend(t, app, aliceToken, bobToken)
	putLocation(t, app, bobToken, 35.0, 135.0, 10, env.Clock.Now())

	if status, body := testutil.Request(t, app, http.MethodPost, "/api/v1/blocks", `{"did":"did:plc:bob"}`, aliceToken); status != http.StatusNoContent {
		t.Fatalf("block: status = %d (body=%s)", status, body)
	}
	items := getFriendLocations(t, app, aliceToken).Items
	for _, it := range items {
		if it.DID == "did:plc:bob" {
			t.Fatalf("blocked peer still listed: %+v", items)
		}
	}
}

func TestLocationShareSetPrecisionErrors(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)
	env.CreateUser(t, "did:plc:alice", "alice.test")
	env.CreateUser(t, "did:plc:bob", "bob.test")
	env.CreateUser(t, "did:plc:carol", "carol.test")
	aliceToken := env.Login(t, "did:plc:alice")
	bobToken := env.Login(t, "did:plc:bob")
	beFriend(t, app, aliceToken, bobToken)

	// フレンドでない相手は 404。
	status, body := setPrecision(t, app, aliceToken, "did:plc:carol", "area", false)
	if status != http.StatusNotFound {
		t.Fatalf("non-friend: status = %d, want 404 (body=%s)", status, body)
	}
	// 自分自身も 404。
	status, body = setPrecision(t, app, aliceToken, "did:plc:alice", "area", false)
	if status != http.StatusNotFound {
		t.Fatalf("self: status = %d, want 404 (body=%s)", status, body)
	}
	// direction と areasOnly は受け付ける（07 §4.1）。
	status, body = setPrecision(t, app, aliceToken, "did:plc:bob", "direction", true)
	if status != http.StatusNoContent {
		t.Fatalf("direction+areasOnly: status = %d, want 204 (body=%s)", status, body)
	}
	if p, only := locationShareRow(t, env, "did:plc:alice", "did:plc:bob"); p != "direction" || !only {
		t.Fatalf("saved row = %q,%v, want direction,true", p, only)
	}
	// 未知の精度も 400。
	status, body = setPrecision(t, app, aliceToken, "did:plc:bob", "bogus", false)
	if status != http.StatusBadRequest {
		t.Fatalf("bogus: status = %d, want 400 (body=%s)", status, body)
	}
	// フレンドには設定できる。
	if status, body := setPrecision(t, app, aliceToken, "did:plc:bob", "exact", false); status != http.StatusNoContent {
		t.Fatalf("friend: status = %d (body=%s)", status, body)
	}
}

func TestFriendshipCreatesDefaultLocationShares(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)
	env.CreateUser(t, "did:plc:alice", "alice.test")
	env.CreateUser(t, "did:plc:bob", "bob.test")
	env.CreateUser(t, "did:plc:carol", "carol.test")
	aliceToken := env.Login(t, "did:plc:alice")
	bobToken := env.Login(t, "did:plc:bob")
	carolToken := env.Login(t, "did:plc:carol")
	beFriend(t, app, aliceToken, bobToken)

	// 双方向に既定 area の行ができる。
	if p, ok := locationSharePrecision(t, env, "did:plc:alice", "did:plc:bob"); !ok || p != "area" {
		t.Fatalf("alice→bob = %q,%v, want area,true", p, ok)
	}
	if p, ok := locationSharePrecision(t, env, "did:plc:bob", "did:plc:alice"); !ok || p != "area" {
		t.Fatalf("bob→alice = %q,%v, want area,true", p, ok)
	}

	// フレンド削除で双方向の行が消える。
	if status, body := testutil.Request(t, app, http.MethodDelete, "/api/v1/friends/did:plc:bob", "", aliceToken); status != http.StatusNoContent {
		t.Fatalf("delete friend: status = %d (body=%s)", status, body)
	}
	if _, ok := locationSharePrecision(t, env, "did:plc:alice", "did:plc:bob"); ok {
		t.Fatal("alice→bob row remains after unfriend")
	}
	if _, ok := locationSharePrecision(t, env, "did:plc:bob", "did:plc:alice"); ok {
		t.Fatal("bob→alice row remains after unfriend")
	}

	// ブロックでも消える。
	beFriend(t, app, aliceToken, carolToken)
	if status, body := testutil.Request(t, app, http.MethodPost, "/api/v1/blocks", `{"did":"did:plc:carol"}`, aliceToken); status != http.StatusNoContent {
		t.Fatalf("block: status = %d (body=%s)", status, body)
	}
	if _, ok := locationSharePrecision(t, env, "did:plc:alice", "did:plc:carol"); ok {
		t.Fatal("alice→carol row remains after block")
	}
	if _, ok := locationSharePrecision(t, env, "did:plc:carol", "did:plc:alice"); ok {
		t.Fatal("carol→alice row remains after block")
	}
}

func TestAreasAPI(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)
	env.CreateUser(t, "did:plc:alice", "alice.test")
	env.CreateUser(t, "did:plc:bob", "bob.test")
	aliceToken := env.Login(t, "did:plc:alice")
	bobToken := env.Login(t, "did:plc:bob")

	// 作成と一覧。
	a := createArea(t, app, aliceToken, "大学", 35.0, 135.0, 500)
	if a.Label != "大学" || a.RadiusM != 500 {
		t.Fatalf("area = %+v", a)
	}
	status, body := testutil.Request(t, app, http.MethodGet, "/api/v1/me/areas", "", aliceToken)
	if status != http.StatusOK {
		t.Fatalf("list areas: status = %d (body=%s)", status, body)
	}
	var list areaListResponse
	if err := json.Unmarshal(body, &list); err != nil {
		t.Fatalf("decode areas: %v (body=%s)", err, body)
	}
	if len(list.Items) != 1 || list.Items[0].ID != a.ID {
		t.Fatalf("areas = %+v", list.Items)
	}

	// PATCH。
	status, body = testutil.Request(t, app, http.MethodPatch, "/api/v1/me/areas/"+a.ID, `{"label":"駅","radiusM":300}`, aliceToken)
	if status != http.StatusOK {
		t.Fatalf("patch area: status = %d (body=%s)", status, body)
	}
	var patched areaItemResponse
	if err := json.Unmarshal(body, &patched); err != nil {
		t.Fatalf("decode patched: %v", err)
	}
	if patched.Label != "駅" || patched.RadiusM != 300 {
		t.Fatalf("patched = %+v", patched)
	}

	// 他人のエリアは 404。
	if status, body := testutil.Request(t, app, http.MethodPatch, "/api/v1/me/areas/"+a.ID, `{"label":"x"}`, bobToken); status != http.StatusNotFound {
		t.Fatalf("patch other's area: status = %d, want 404 (body=%s)", status, body)
	}
	if status, body := testutil.Request(t, app, http.MethodDelete, "/api/v1/me/areas/"+a.ID, "", bobToken); status != http.StatusNotFound {
		t.Fatalf("delete other's area: status = %d, want 404 (body=%s)", status, body)
	}

	// 検証: 半径と label。
	for _, tc := range []struct {
		name string
		body string
	}{
		{"radius too small", `{"label":"x","lat":35,"lon":135,"radiusM":49}`},
		{"radius too large", `{"label":"x","lat":35,"lon":135,"radiusM":5001}`},
		{"label too long", fmt.Sprintf(`{"label":%q,"lat":35,"lon":135,"radiusM":500}`, strings.Repeat("あ", 21))},
		{"label empty", `{"label":"","lat":35,"lon":135,"radiusM":500}`},
	} {
		if status, body := testutil.Request(t, app, http.MethodPost, "/api/v1/me/areas", tc.body, aliceToken); status != http.StatusBadRequest {
			t.Fatalf("%s: status = %d, want 400 (body=%s)", tc.name, status, body)
		}
	}

	// 上限 10 個。
	for i := 0; i < 9; i++ {
		createArea(t, app, aliceToken, fmt.Sprintf("area-%d", i), 35.0, 135.0, 50)
	}
	status, body = testutil.Request(t, app, http.MethodPost, "/api/v1/me/areas",
		`{"label":"over","lat":35,"lon":135,"radiusM":500}`, aliceToken)
	if status != http.StatusConflict {
		t.Fatalf("over limit: status = %d, want 409 (body=%s)", status, body)
	}

	// 削除。
	if status, body := testutil.Request(t, app, http.MethodDelete, "/api/v1/me/areas/"+a.ID, "", aliceToken); status != http.StatusNoContent {
		t.Fatalf("delete area: status = %d (body=%s)", status, body)
	}
}

func diff(a, b float64) float64 {
	if a > b {
		return a - b
	}
	return b - a
}

// direction は見る側から相手への真方位と距離帯を返し、座標は返さない（07 §4.1）。
func TestFriendLocationsDirection(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)
	env.CreateUser(t, "did:plc:alice", "alice.test")
	env.CreateUser(t, "did:plc:bob", "bob.test")
	aliceToken := env.Login(t, "did:plc:alice")
	bobToken := env.Login(t, "did:plc:bob")
	beFriend(t, app, aliceToken, bobToken)

	if status, body := setPrecision(t, app, bobToken, "did:plc:alice", "direction", false); status != http.StatusNoContent {
		t.Fatalf("set direction: status = %d (body=%s)", status, body)
	}
	putLocation(t, app, bobToken, 35.0, 135.0, 10, env.Clock.Now())

	// 見る側（alice）の位置がなければ viewerStale。方角と距離は返さない。
	item := findLocation(t, getFriendLocations(t, app, aliceToken).Items, "did:plc:bob")
	if item.Precision != "direction" || !item.ViewerStale || item.Direction != "" || item.DistanceBucket != "" {
		t.Fatalf("viewerStale item = %+v", item)
	}

	// alice を bob の北 55 m に置く。方角は見る側から相手へなので南、距離帯は <100m。
	putLocation(t, app, aliceToken, 35.0005, 135.0, 10, env.Clock.Now())
	item = findLocation(t, getFriendLocations(t, app, aliceToken).Items, "did:plc:bob")
	if item.ViewerStale || item.Direction != "S" || item.DistanceBucket != "<100m" {
		t.Fatalf("direction item = %+v, want direction S, <100m", item)
	}
	if item.Lat != nil || item.Lon != nil {
		t.Fatalf("direction must not return coordinates: %+v", item)
	}

	// 見る側の位置が 2 分より古ければ viewerStale（検証を通さず Redis に直接書く）。
	old := model.Location{DID: "did:plc:alice", Lat: 35.0005, Lon: 135.0, AccuracyM: 10, RecordedAt: env.Clock.Now().Add(-3 * time.Minute), Source: "gps"}
	if err := model.SaveLocation(context.Background(), env.Redis, old, time.Hour); err != nil {
		t.Fatalf("save old viewer location: %v", err)
	}
	item = findLocation(t, getFriendLocations(t, app, aliceToken).Items, "did:plc:bob")
	if !item.ViewerStale || item.Direction != "" || item.DistanceBucket != "" {
		t.Fatalf("stale viewer item = %+v", item)
	}
}

// areasOnly は登録エリアの中にいるときだけ設定した精度で見せ、外では off と同じ（07 §4.1）。
func TestFriendLocationsAreasOnly(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)
	env.CreateUser(t, "did:plc:alice", "alice.test")
	env.CreateUser(t, "did:plc:bob", "bob.test")
	aliceToken := env.Login(t, "did:plc:alice")
	bobToken := env.Login(t, "did:plc:bob")
	beFriend(t, app, aliceToken, bobToken)
	createArea(t, app, bobToken, "大学", 35.0, 135.0, 500)

	// exact + areasOnly: 中は exact、外は off。
	if status, body := setPrecision(t, app, bobToken, "did:plc:alice", "exact", true); status != http.StatusNoContent {
		t.Fatalf("set exact+areasOnly: status = %d (body=%s)", status, body)
	}
	putLocation(t, app, bobToken, 35.0, 135.0, 10, env.Clock.Now())
	item := findLocation(t, getFriendLocations(t, app, aliceToken).Items, "did:plc:bob")
	if item.Precision != "exact" || item.Lat == nil {
		t.Fatalf("inside item = %+v, want exact", item)
	}
	putLocation(t, app, bobToken, 0, 0, 10, env.Clock.Now())
	item = findLocation(t, getFriendLocations(t, app, aliceToken).Items, "did:plc:bob")
	if item.Precision != "off" || item.Lat != nil {
		t.Fatalf("outside item = %+v, want off", item)
	}

	// area + areasOnly: 中は label、外は off。
	if status, body := setPrecision(t, app, bobToken, "did:plc:alice", "area", true); status != http.StatusNoContent {
		t.Fatalf("set area+areasOnly: status = %d (body=%s)", status, body)
	}
	putLocation(t, app, bobToken, 35.0, 135.0, 10, env.Clock.Now())
	item = findLocation(t, getFriendLocations(t, app, aliceToken).Items, "did:plc:bob")
	if item.Precision != "area" || item.Area == nil || item.Area.Label != "大学" || item.Outside {
		t.Fatalf("inside area item = %+v", item)
	}
	putLocation(t, app, bobToken, 0, 0, 10, env.Clock.Now())
	item = findLocation(t, getFriendLocations(t, app, aliceToken).Items, "did:plc:bob")
	if item.Precision != "off" || item.Area != nil {
		t.Fatalf("outside area item = %+v, want off", item)
	}
}

// direction と areasOnly の組み合わせ（07 §4.1）。
func TestFriendLocationsAreasOnlyDirection(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)
	env.CreateUser(t, "did:plc:alice", "alice.test")
	env.CreateUser(t, "did:plc:bob", "bob.test")
	aliceToken := env.Login(t, "did:plc:alice")
	bobToken := env.Login(t, "did:plc:bob")
	beFriend(t, app, aliceToken, bobToken)
	createArea(t, app, bobToken, "大学", 35.0, 135.0, 500)
	if status, body := setPrecision(t, app, bobToken, "did:plc:alice", "direction", true); status != http.StatusNoContent {
		t.Fatalf("set direction+areasOnly: status = %d (body=%s)", status, body)
	}
	putLocation(t, app, aliceToken, 35.0005, 135.0, 10, env.Clock.Now())

	// bob はエリア内 → direction。
	putLocation(t, app, bobToken, 35.0, 135.0, 10, env.Clock.Now())
	item := findLocation(t, getFriendLocations(t, app, aliceToken).Items, "did:plc:bob")
	if item.Precision != "direction" || item.Direction != "S" || item.DistanceBucket != "<100m" {
		t.Fatalf("inside item = %+v, want direction S, <100m", item)
	}

	// bob はエリア外 → off。
	putLocation(t, app, bobToken, 0, 0, 10, env.Clock.Now())
	item = findLocation(t, getFriendLocations(t, app, aliceToken).Items, "did:plc:bob")
	if item.Precision != "off" || item.Direction != "" {
		t.Fatalf("outside item = %+v, want off", item)
	}
}
