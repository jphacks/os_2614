//go:build integration

package content_test

import (
	"bytes"
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
