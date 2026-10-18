//go:build integration

package content_test

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
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/applemango/chain/internal/storage"
	"github.com/applemango/chain/internal/testutil"
)

// メディアのアップロードと配信（10 §4.4）の API テスト。

func uploadFile(t *testing.T, app *fiber.App, token, filename string, data []byte, withEXIF bool) (int, []byte) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := part.Write(data); err != nil {
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
	return resp.StatusCode, body
}

func jpegBytes(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 32, 24))
	for y := 0; y < 24; y++ {
		for x := 0; x < 32; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 8), G: uint8(y * 10), B: 128, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatalf("jpeg.Encode: %v", err)
	}
	return buf.Bytes()
}

// withEXIFApp1 は JPEG の SOI の直後に APP1(Exif) セグメントを挿入する。
func withEXIFApp1(t *testing.T, plain []byte) []byte {
	t.Helper()
	payload := []byte("Exif\x00\x00II*\x00\x08\x00\x00\x00\x00\x00")
	segLen := len(payload) + 2
	seg := []byte{0xFF, 0xE1, byte(segLen >> 8), byte(segLen & 0xFF)}
	seg = append(seg, payload...)

	out := make([]byte, 0, len(plain)+len(seg))
	out = append(out, plain[:2]...) // SOI
	out = append(out, seg...)
	out = append(out, plain[2:]...)
	return out
}

func uploadJPEG(t *testing.T, app *fiber.App, token string) string {
	t.Helper()
	status, body := uploadFile(t, app, token, "photo.jpg", jpegBytes(t), false)
	if status != http.StatusCreated {
		t.Fatalf("upload: status = %d, want 201 (body=%s)", status, body)
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

func shareMedia(t *testing.T, app *fiber.App, token, spaceID, mediaID string, takenAt *time.Time) (int, []byte) {
	t.Helper()
	body := map[string]any{"mediaId": mediaID}
	if takenAt != nil {
		body["takenAt"] = takenAt.UTC().Format(time.RFC3339)
	}
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal share body: %v", err)
	}
	return testutil.Request(t, app, http.MethodPost, "/api/v1/instances/"+spaceID+"/media", string(b), token)
}

func archivedPartyForMedia(t *testing.T, env *testutil.Env, spaceID, did string, confirmed, ended time.Time, leftAt *time.Time) {
	t.Helper()
	testutil.Space(t, env, spaceID, "jp.i32.chain.space.party", "archived", did)
	testutil.Exec(t, env, `INSERT INTO parties (space_id, phase, confirmed_at, ended_at, tentative_at, last_activity_at)
		VALUES (?, 'ended', ?, ?, ?, ?)`, spaceID, confirmed.UnixMilli(), ended.UnixMilli(), confirmed.UnixMilli(), ended.UnixMilli())
	testutil.Exec(t, env, `INSERT INTO space_members (space_id, did, can_read, can_write, role, joined_at, left_at)
		VALUES (?, ?, 1, 0, 'member', ?, ?)`, spaceID, did, confirmed.UnixMilli(), func() any {
		if leftAt == nil {
			return nil
		}
		return leftAt.UnixMilli()
	}())
}

func insertUnattachedMedia(t *testing.T, env *testutil.Env, id, owner, sha string) {
	t.Helper()
	testutil.Exec(t, env, `INSERT INTO media (id, owner_did, mime, width, height, bytes, sha256, created_at)
		VALUES (?, ?, 'image/jpeg', 32, 24, 100, ?, ?)`, id, owner, sha, env.Clock.Now().UnixMilli())
}

func TestShareMediaDeduplicatesAndAllowsArchivedPartyWindow(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)
	const alice = "did:plc:alice"
	env.CreateUser(t, alice, "alice.test")
	token := env.Login(t, alice)

	testutil.Space(t, env, "group-active", "jp.i32.chain.space.group", "active", alice)
	testutil.Group(t, env, "group-active", true)
	testutil.Member(t, env, "group-active", alice, true)
	mediaID := uploadJPEG(t, app, token)
	status, body := shareMedia(t, app, token, "group-active", mediaID, nil)
	if status != http.StatusCreated {
		t.Fatalf("new share: status = %d, want 201 (body=%s)", status, body)
	}
	var first struct {
		URI string `json:"uri"`
	}
	testutil.MustJSONBody(t, body, &first)
	status, body = shareMedia(t, app, token, "group-active", mediaID, nil)
	if status != http.StatusOK {
		t.Fatalf("duplicate share: status = %d, want 200 (body=%s)", status, body)
	}
	var duplicate struct {
		URI string `json:"uri"`
	}
	testutil.MustJSONBody(t, body, &duplicate)
	if duplicate.URI != first.URI {
		t.Fatalf("duplicate uri = %q, want %q", duplicate.URI, first.URI)
	}

	now := env.Clock.Now()
	cases := []struct {
		name       string
		age        time.Duration
		left       bool
		taken      time.Time
		wantStatus int
	}{
		{name: "within 24 hours", age: time.Hour, taken: now.Add(-90 * time.Minute), wantStatus: http.StatusCreated},
		{name: "over 24 hours", age: 25 * time.Hour, taken: now.Add(-25 * time.Hour), wantStatus: http.StatusGone},
		{name: "left before ending", age: time.Hour, left: true, taken: now.Add(-90 * time.Minute), wantStatus: http.StatusGone},
		{name: "taken outside party", age: time.Hour, taken: now.Add(-4 * time.Hour), wantStatus: http.StatusGone},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spaceID := "party-media-" + string(rune('a'+i))
			ended := now.Add(-tc.age)
			confirmed := ended.Add(-2 * time.Hour)
			var leftAt *time.Time
			if tc.left {
				left := ended.Add(-time.Minute)
				leftAt = &left
			}
			archivedPartyForMedia(t, env, spaceID, alice, confirmed, ended, leftAt)
			mediaID := "uploaded-" + string(rune('a'+i))
			insertUnattachedMedia(t, env, mediaID, alice, "sha-"+string(rune('a'+i)))
			status, body := shareMedia(t, app, token, spaceID, mediaID, &tc.taken)
			if status != tc.wantStatus {
				t.Fatalf("status = %d, want %d (body=%s)", status, tc.wantStatus, body)
			}
			if tc.wantStatus == http.StatusGone {
				testutil.WantErrorCode(t, body, "gone")
			}
		})
	}

	testutil.Space(t, env, "group-archived-media", "jp.i32.chain.space.group", "archived", alice)
	testutil.Group(t, env, "group-archived-media", true)
	testutil.Member(t, env, "group-archived-media", alice, false)
	insertUnattachedMedia(t, env, "group-archived-upload", alice, "sha-group")
	status, body = shareMedia(t, app, token, "group-archived-media", "group-archived-upload", nil)
	if status != http.StatusGone {
		t.Fatalf("archived group: status = %d, want 410 (body=%s)", status, body)
	}
	testutil.WantErrorCode(t, body, "gone")
}

func TestShareMediaRejectsForeignMediaBeforeValidation(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)
	const alice = "did:plc:alice"
	const bob = "did:plc:bob"
	env.CreateUser(t, alice, "alice.test")
	env.CreateUser(t, bob, "bob.test")
	aliceToken := env.Login(t, alice)
	bobToken := env.Login(t, bob)
	testutil.Space(t, env, "group-foreign-media", "jp.i32.chain.space.group", "active", alice)
	testutil.Group(t, env, "group-foreign-media", true)
	testutil.Member(t, env, "group-foreign-media", alice, true)
	mediaID := uploadJPEG(t, app, aliceToken)

	status, body := shareMedia(t, app, bobToken, "group-foreign-media", mediaID, nil)
	if status != http.StatusForbidden {
		t.Fatalf("foreign media from non-member: status = %d, want 403 (body=%s)", status, body)
	}
	testutil.WantErrorCode(t, body, "forbidden")
}

func TestMediaUploadStripsEXIF(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)
	env.CreateUser(t, "did:plc:alice", "alice.test")
	token := env.Login(t, "did:plc:alice")

	original := withEXIFApp1(t, jpegBytes(t))
	if !bytes.Contains(original, []byte("Exif")) {
		t.Fatal("test fixture does not contain EXIF")
	}

	status, body := uploadFile(t, app, token, "photo.jpg", original, true)
	if status != http.StatusCreated {
		t.Fatalf("upload: status = %d, want 201 (body=%s)", status, body)
	}
	var up struct {
		ID     string `json:"id"`
		Width  int    `json:"width"`
		Height int    `json:"height"`
	}
	testutil.MustJSONBody(t, body, &up)
	if up.Width != 32 || up.Height != 24 {
		t.Fatalf("size = %dx%d, want 32x24", up.Width, up.Height)
	}

	path := filepath.Join(env.Config.MediaDir, "2026", "10", up.ID+".jpg")
	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read saved file %s: %v", path, err)
	}
	if bytes.Contains(saved, []byte("Exif")) {
		t.Fatal("saved file still contains EXIF")
	}
	// APP1 マーカー（0xFFE1）も残っていないこと。
	if bytes.Contains(saved, []byte{0xFF, 0xE1}) {
		t.Fatal("saved file still contains an APP1 segment")
	}
}

func TestMediaSignedURL(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)
	env.CreateUser(t, "did:plc:alice", "alice.test")
	token := env.Login(t, "did:plc:alice")

	id := uploadJPEG(t, app, token)
	signer := storage.NewSigner(env.Config.JWTKey)
	exp := env.Clock.Now().Add(time.Hour).Unix()

	valid := signer.URL("", id, storage.VariantFull, exp)
	if status, body := testutil.Request(t, app, http.MethodGet, valid, "", ""); status != http.StatusOK {
		t.Fatalf("valid signed url: status = %d, want 200 (body=%s)", status, body)
	}

	// 署名の改ざん。
	tampered := signer.URL("", id, storage.VariantFull, exp)
	tampered = tampered[:len(tampered)-4] + "AAAA"
	if status, body := testutil.Request(t, app, http.MethodGet, tampered, "", ""); status != http.StatusForbidden {
		t.Fatalf("tampered sig: status = %d, want 403 (body=%s)", status, body)
	}

	// 期限切れ。
	expired := signer.URL("", id, storage.VariantFull, env.Clock.Now().Add(-time.Minute).Unix())
	if status, body := testutil.Request(t, app, http.MethodGet, expired, "", ""); status != http.StatusForbidden {
		t.Fatalf("expired sig: status = %d, want 403 (body=%s)", status, body)
	}
}

func TestMediaUploadTooLarge(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)
	env.CreateUser(t, "did:plc:alice", "alice.test")
	token := env.Login(t, "did:plc:alice")

	big := bytes.Repeat([]byte{0}, (15<<20)+1)
	status, body := uploadFile(t, app, token, "big.bin", big, false)
	if status != http.StatusRequestEntityTooLarge {
		t.Fatalf("too large: status = %d, want 413 (body=%s)", status, body)
	}
}

func TestMediaUploadUnsupportedFormat(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)
	env.CreateUser(t, "did:plc:alice", "alice.test")
	token := env.Login(t, "did:plc:alice")

	gif := []byte("GIF89a\x01\x00\x01\x00\x00\x00\x00;")
	status, body := uploadFile(t, app, token, "anim.gif", gif, false)
	if status != http.StatusBadRequest {
		t.Fatalf("unsupported format: status = %d, want 400 (body=%s)", status, body)
	}
	testutil.WantErrorCode(t, body, "invalid_request")
}
