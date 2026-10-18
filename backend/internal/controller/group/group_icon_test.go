//go:build integration

package group_test

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/applemango/chain/internal/service/content"
	"github.com/applemango/chain/internal/storage"
	"github.com/applemango/chain/internal/testutil"
)

// グループのアイコン（09 §2、§4、§5）の結合テスト。
// 実装は変えず、iconMediaId の受け入れ条件と iconUrl の出し分けを確かめる。

// uploadIcon は POST /media で JPEG を 1 枚上げ、その id を返す。
func uploadIcon(t *testing.T, app *fiber.App, token string) string {
	t.Helper()

	img := image.NewRGBA(image.Rect(0, 0, 32, 24))
	for y := 0; y < 24; y++ {
		for x := 0; x < 32; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 8), G: uint8(y * 10), B: 128, A: 255})
		}
	}
	var jpegData bytes.Buffer
	if err := jpeg.Encode(&jpegData, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatalf("jpeg.Encode: %v", err)
	}

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile("file", "icon.jpg")
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := part.Write(jpegData.Bytes()); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/media", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := app.Test(req, fiber.TestConfig{Timeout: 30 * time.Second})
	if err != nil {
		t.Fatalf("app.Test(upload): %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read upload body: %v", err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("upload icon: status = %d, want 201 (body=%s)", resp.StatusCode, body)
	}
	var up struct {
		ID string `json:"id"`
	}
	testutil.MustJSONBody(t, body, &up)
	if up.ID == "" {
		t.Fatalf("upload response has no id (body=%s)", body)
	}
	return up.ID
}

// wantThumbIconURL は iconUrl が mediaID のサムネイル variant の署名付き URL かを確かめる。
func wantThumbIconURL(t *testing.T, env *testutil.Env, mediaID, got string) {
	t.Helper()
	if got == "" {
		t.Fatalf("iconUrl が空 (media=%s)", mediaID)
	}
	u, err := url.Parse(got)
	if err != nil {
		t.Fatalf("iconUrl を parse できない %q: %v", got, err)
	}
	if u.Path != "/media/"+mediaID {
		t.Fatalf("iconUrl の path = %q, want /media/%s", u.Path, mediaID)
	}
	q := u.Query()
	if q.Get("v") != string(storage.VariantThumb) {
		t.Fatalf("iconUrl の v = %q, want %q (iconUrl=%s)", q.Get("v"), storage.VariantThumb, got)
	}
	exp, err := strconv.ParseInt(q.Get("exp"), 10, 64)
	if err != nil {
		t.Fatalf("iconUrl の exp = %q: %v", q.Get("exp"), err)
	}
	wantExp := env.Clock.Now().Add(content.MediaURLTTL).Unix()
	if exp != wantExp {
		t.Fatalf("iconUrl の exp = %d, want %d", exp, wantExp)
	}
	signer := storage.NewSigner(env.Config.JWTKey)
	if !signer.Verify(mediaID, storage.VariantThumb, exp, q.Get("sig"), env.Clock.Now()) {
		t.Fatalf("iconUrl の署名が無効: %s", got)
	}
}

// iconJSON は iconUrl キーの有無を見るための最小のデコード先。
type iconJSON struct {
	ID      string `json:"id"`
	IconURL string `json:"iconUrl"`
}

func TestGroupIconOnCreateAndDetail(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)
	env.CreateUser(t, "did:plc:alice", "alice.test")
	alice := env.Login(t, "did:plc:alice")
	setGroupLocation(t, env, "did:plc:alice", groupLat, groupLon)

	mediaID := uploadIcon(t, app, alice)

	status, body := createGroup(t, app, alice,
		`{"name":"アイコン付き","geofence":{"lat":35.0,"lon":135.0,"radiusM":40},"iconMediaId":"`+mediaID+`"}`)
	if status != http.StatusCreated {
		t.Fatalf("create: status = %d, want 201 (body=%s)", status, body)
	}
	var created iconJSON
	testutil.MustJSONBody(t, body, &created)
	if created.ID == "" {
		t.Fatalf("created group has no id (body=%s)", body)
	}
	wantThumbIconURL(t, env, mediaID, created.IconURL)

	status, body = testutil.Request(t, app, http.MethodGet, "/api/v1/groups/"+created.ID, "", alice)
	if status != http.StatusOK {
		t.Fatalf("detail: status = %d, want 200 (body=%s)", status, body)
	}
	var detail iconJSON
	testutil.MustJSONBody(t, body, &detail)
	wantThumbIconURL(t, env, mediaID, detail.IconURL)
	if detail.IconURL != created.IconURL {
		t.Fatalf("detail iconUrl = %q, want %q", detail.IconURL, created.IconURL)
	}
}

func TestGroupIconOnNearbyInside(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)
	env.CreateUser(t, "did:plc:alice", "alice.test")
	alice := env.Login(t, "did:plc:alice")
	setGroupLocation(t, env, "did:plc:alice", groupLat, groupLon)

	mediaID := uploadIcon(t, app, alice)
	status, body := createGroup(t, app, alice,
		`{"name":"アイコン付き","geofence":{"lat":35.0,"lon":135.0,"radiusM":40},"iconMediaId":"`+mediaID+`"}`)
	if status != http.StatusCreated {
		t.Fatalf("create: status = %d, want 201 (body=%s)", status, body)
	}
	id := decodeGroup(t, body).ID

	env.CreateUser(t, "did:plc:bob", "bob.test")
	bob := env.Login(t, "did:plc:bob")
	setGroupLocation(t, env, "did:plc:bob", groupLat, groupLon)

	status, body = testutil.Request(t, app, http.MethodGet, "/api/v1/groups/nearby", "", bob)
	if status != http.StatusOK {
		t.Fatalf("nearby: status = %d, want 200 (body=%s)", status, body)
	}
	var nearby struct {
		Inside []struct {
			ID      string `json:"id"`
			IconURL string `json:"iconUrl"`
		} `json:"inside"`
	}
	testutil.MustJSONBody(t, body, &nearby)
	if len(nearby.Inside) != 1 || nearby.Inside[0].ID != id {
		t.Fatalf("nearby inside = %+v, want the group", nearby.Inside)
	}
	wantThumbIconURL(t, env, mediaID, nearby.Inside[0].IconURL)
}

func TestGroupIconOmittedWhenAbsent(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)
	env.CreateUser(t, "did:plc:alice", "alice.test")
	alice := env.Login(t, "did:plc:alice")
	setGroupLocation(t, env, "did:plc:alice", groupLat, groupLon)

	status, body := createGroup(t, app, alice,
		`{"name":"アイコンなし","geofence":{"lat":35.0,"lon":135.0,"radiusM":40}}`)
	if status != http.StatusCreated {
		t.Fatalf("create: status = %d, want 201 (body=%s)", status, body)
	}
	var raw map[string]json.RawMessage
	testutil.MustJSONBody(t, body, &raw)
	if _, ok := raw["iconUrl"]; ok {
		t.Fatalf("アイコンなしなのに iconUrl がある: %s", body)
	}
	id := decodeGroup(t, body).ID

	status, body = testutil.Request(t, app, http.MethodGet, "/api/v1/groups/"+id, "", alice)
	if status != http.StatusOK {
		t.Fatalf("detail: status = %d, want 200 (body=%s)", status, body)
	}
	testutil.MustJSONBody(t, body, &raw)
	if _, ok := raw["iconUrl"]; ok {
		t.Fatalf("アイコンなしなのに詳細に iconUrl がある: %s", body)
	}
}

// wantIconRejected は iconMediaId が 400 fields.iconMediaId で弾かれることを確かめる。
func wantIconRejected(t *testing.T, body []byte, status int) {
	t.Helper()
	if status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body=%s)", status, body)
	}
	e := testutil.WantErrorCode(t, body, "invalid_request")
	if e.Error.Details.Fields["iconMediaId"] == "" {
		t.Fatalf("details.fields.iconMediaId が空 (body=%s)", body)
	}
}

func TestGroupIconRejectsOthersMedia(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)
	env.CreateUser(t, "did:plc:alice", "alice.test")
	alice := env.Login(t, "did:plc:alice")
	env.CreateUser(t, "did:plc:bob", "bob.test")
	bob := env.Login(t, "did:plc:bob")
	setGroupLocation(t, env, "did:plc:alice", groupLat, groupLon)

	// bob が上げた media を alice が使う。
	bobMedia := uploadIcon(t, app, bob)

	status, body := createGroup(t, app, alice,
		`{"name":"よその画像","geofence":{"lat":35.0,"lon":135.0,"radiusM":40},"iconMediaId":"`+bobMedia+`"}`)
	wantIconRejected(t, body, status)

	// グループは作られていないので、同じ名前で作り直せる。
	status, body = createGroup(t, app, alice,
		`{"name":"よその画像","geofence":{"lat":35.0,"lon":135.0,"radiusM":40}}`)
	if status != http.StatusCreated {
		t.Fatalf("recreate: status = %d, want 201 (body=%s)", status, body)
	}
}

func TestGroupIconRejectsUnknownMedia(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)
	env.CreateUser(t, "did:plc:alice", "alice.test")
	alice := env.Login(t, "did:plc:alice")
	setGroupLocation(t, env, "did:plc:alice", groupLat, groupLon)

	status, body := createGroup(t, app, alice,
		`{"name":"存在しない画像","geofence":{"lat":35.0,"lon":135.0,"radiusM":40},"iconMediaId":"0192-does-not-exist"}`)
	wantIconRejected(t, body, status)

	// グループは作られていないので、同じ名前で作り直せる。
	status, body = createGroup(t, app, alice,
		`{"name":"存在しない画像","geofence":{"lat":35.0,"lon":135.0,"radiusM":40}}`)
	if status != http.StatusCreated {
		t.Fatalf("recreate: status = %d, want 201 (body=%s)", status, body)
	}
}

func TestGroupIconRejectsAlreadyAttachedMedia(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)
	env.CreateUser(t, "did:plc:alice", "alice.test")
	alice := env.Login(t, "did:plc:alice")

	// 1 つ目のグループをアイコン付きで作る（media が添付される）。
	setGroupLocation(t, env, "did:plc:alice", groupLat, groupLon)
	mediaID := uploadIcon(t, app, alice)
	status, body := createGroup(t, app, alice,
		`{"name":"一つ目","geofence":{"lat":35.0,"lon":135.0,"radiusM":40},"iconMediaId":"`+mediaID+`"}`)
	if status != http.StatusCreated {
		t.Fatalf("first create: status = %d, want 201 (body=%s)", status, body)
	}

	// 別の場所で、同じ media を 2 つ目のアイコンに使う → 400。
	lat2, lon2 := groupLat+0.01, groupLon
	setGroupLocation(t, env, "did:plc:alice", lat2, lon2)
	status, body = createGroup(t, app, alice,
		`{"name":"二つ目","geofence":{"lat":35.01,"lon":135.0,"radiusM":40},"iconMediaId":"`+mediaID+`"}`)
	wantIconRejected(t, body, status)

	// グループは作られていないので、同じ場所・同じ名前でアイコンなしなら作り直せる。
	// （作られていれば IoU 1・schedule 重なりで 409 になるはず。）
	status, body = createGroup(t, app, alice,
		`{"name":"二つ目","geofence":{"lat":35.01,"lon":135.0,"radiusM":40}}`)
	if status != http.StatusCreated {
		t.Fatalf("recreate: status = %d, want 201 (body=%s)", status, body)
	}
}
