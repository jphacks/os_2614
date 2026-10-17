//go:build integration

package controller_test

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/applemango/chain/internal/external/atproto"
	"github.com/applemango/chain/internal/testutil"
)

// CHAIN からのプロフィール編集の API（13 §1.3）。

const (
	profileColl = "app.bsky.actor.profile"
	profileRKey = "self"
)

type meProfileBody struct {
	DID         string `json:"did"`
	Handle      string `json:"handle"`
	DisplayName string `json:"displayName"`
	Description string `json:"description"`
	AvatarURL   string `json:"avatarUrl"`
	BannerURL   string `json:"bannerUrl"`
}

func uploadTo(t *testing.T, app *fiber.App, method, path, token, filename string, data []byte) (int, []byte) {
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

	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := app.Test(req, fiber.TestConfig{Timeout: 30 * time.Second})
	if err != nil {
		t.Fatalf("app.Test(%s %s): %v", method, path, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return resp.StatusCode, body
}

func smallJPEG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 64, 48))
	for y := 0; y < 48; y++ {
		for x := 0; x < 64; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 4), G: uint8(y * 5), B: 100, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatalf("jpeg.Encode: %v", err)
	}
	return buf.Bytes()
}

func TestMeProfilePatchPreservesUnknownFields(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)
	env.CreateUser(t, "did:plc:alice", "alice.test")
	alice := env.Login(t, "did:plc:alice")
	env.Writer.SetRecord(profileColl, profileRKey, map[string]any{
		"$type":       profileColl,
		"displayName": "Old",
		"description": "OldDesc",
		"pronouns":    "they/them",
	}, "cid-old")

	status, body := testutil.Request(t, app, http.MethodPatch, "/api/v1/me/profile", `{"displayName":"New"}`, alice)
	if status != http.StatusOK {
		t.Fatalf("patch: status = %d, want 200 (body=%s)", status, body)
	}
	var me meProfileBody
	testutil.MustJSONBody(t, body, &me)
	if me.DisplayName != "New" || me.Description != "OldDesc" {
		t.Fatalf("response = %+v", me)
	}
	if me.DID != "did:plc:alice" || me.Handle != "alice.test" {
		t.Fatalf("response identity = %+v", me)
	}

	calls := env.Writer.Calls()
	if len(calls) != 1 || calls[0].SwapRecord != "cid-old" {
		t.Fatalf("writer calls = %+v", calls)
	}
	rec, _, _ := env.Writer.Record(profileColl, profileRKey)
	if rec["pronouns"] != "they/them" {
		t.Fatalf("unknown field dropped: %+v", rec)
	}

	status, body = testutil.Request(t, app, http.MethodGet, "/api/v1/me", "", alice)
	if status != http.StatusOK {
		t.Fatalf("get me: status = %d (body=%s)", status, body)
	}
	var got meProfileBody
	testutil.MustJSONBody(t, body, &got)
	if got.DisplayName != "New" {
		t.Fatalf("GET /me displayName = %q, want New", got.DisplayName)
	}
}

func TestMeProfileEmptyClearsField(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)
	env.CreateUser(t, "did:plc:alice", "alice.test")
	alice := env.Login(t, "did:plc:alice")
	env.Writer.SetRecord(profileColl, profileRKey, map[string]any{
		"$type": profileColl, "displayName": "Old", "description": "Keep",
	}, "cid-old")

	status, body := testutil.Request(t, app, http.MethodPatch, "/api/v1/me/profile", `{"displayName":""}`, alice)
	if status != http.StatusOK {
		t.Fatalf("patch: status = %d, want 200 (body=%s)", status, body)
	}
	var me meProfileBody
	testutil.MustJSONBody(t, body, &me)
	if me.DisplayName != "" || me.Description != "Keep" {
		t.Fatalf("response = %+v", me)
	}
}

func TestMeProfileLimits(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)
	env.CreateUser(t, "did:plc:alice", "alice.test")
	alice := env.Login(t, "did:plc:alice")

	status, body := testutil.Request(t, app, http.MethodPatch, "/api/v1/me/profile", `{"displayName":"`+strings.Repeat("あ", 65)+`"}`, alice)
	if status != http.StatusBadRequest {
		t.Fatalf("65 graphemes: status = %d, want 400 (body=%s)", status, body)
	}
	if e := testutil.WantErrorCode(t, body, "invalid_request"); e.Error.Details.Fields["displayName"] == "" {
		t.Fatalf("details.fields.displayName is empty (body=%s)", body)
	}

	status, body = testutil.Request(t, app, http.MethodPatch, "/api/v1/me/profile", `{"description":"`+strings.Repeat("あ", 257)+`"}`, alice)
	if status != http.StatusBadRequest {
		t.Fatalf("257 graphemes: status = %d, want 400 (body=%s)", status, body)
	}
	if e := testutil.WantErrorCode(t, body, "invalid_request"); e.Error.Details.Fields["description"] == "" {
		t.Fatalf("details.fields.description is empty (body=%s)", body)
	}

	status, body = testutil.Request(t, app, http.MethodPatch, "/api/v1/me/profile", `{"displayName":"`+strings.Repeat("あ", 64)+`"}`, alice)
	if status != http.StatusOK {
		t.Fatalf("64 graphemes: status = %d, want 200 (body=%s)", status, body)
	}
}

func TestMeProfileModerationBlocked(t *testing.T) {
	env := testutil.NewEnv(t)
	env.Moderator = &testutil.FakeAssessor{BlockWords: []string{"誹謗"}}
	app := testutil.NewApp(env)
	env.CreateUser(t, "did:plc:alice", "alice.test")
	alice := env.Login(t, "did:plc:alice")

	status, body := testutil.Request(t, app, http.MethodPatch, "/api/v1/me/profile", `{"displayName":"誹謗する名前"}`, alice)
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("blocked: status = %d, want 422 (body=%s)", status, body)
	}
	testutil.WantErrorCode(t, body, "moderation_blocked")
	if len(env.Writer.Calls()) != 0 {
		t.Fatalf("wrote to PDS despite block: %+v", env.Writer.Calls())
	}
}

func TestMeProfileErrors(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)
	env.CreateUser(t, "did:plc:alice", "alice.test")
	alice := env.Login(t, "did:plc:alice")

	env.Writer.SetRecord(profileColl, profileRKey, map[string]any{"$type": profileColl, "displayName": "Old"}, "cid-old")
	env.Writer.SetSwapConflict(true)
	status, body := testutil.Request(t, app, http.MethodPatch, "/api/v1/me/profile", `{"displayName":"New"}`, alice)
	if status != http.StatusConflict {
		t.Fatalf("swap conflict: status = %d, want 409 (body=%s)", status, body)
	}
	testutil.WantErrorCode(t, body, "conflict")

	env.Writer.SetSwapConflict(false)
	env.Writer.SetError(atproto.ErrSessionExpired)
	status, body = testutil.Request(t, app, http.MethodPatch, "/api/v1/me/profile", `{"displayName":"New"}`, alice)
	if status != http.StatusUnauthorized {
		t.Fatalf("expired: status = %d, want 401 (body=%s)", status, body)
	}
	testutil.WantErrorCode(t, body, "atproto_session_expired")

	env.Writer.SetError(atproto.ErrReauthRequired)
	status, body = testutil.Request(t, app, http.MethodPatch, "/api/v1/me/profile", `{"displayName":"New"}`, alice)
	if status != http.StatusForbidden {
		t.Fatalf("reauth: status = %d, want 403 (body=%s)", status, body)
	}
	testutil.WantErrorCode(t, body, "atproto_reauth_required")

	env.Writer.SetError(errors.New("pds down"))
	status, body = testutil.Request(t, app, http.MethodPatch, "/api/v1/me/profile", `{"displayName":"New"}`, alice)
	if status != http.StatusBadGateway {
		t.Fatalf("upstream: status = %d, want 502 (body=%s)", status, body)
	}
	testutil.WantErrorCode(t, body, "upstream_error")
}

func TestMeAvatarUploadAndDelete(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)
	env.CreateUser(t, "did:plc:alice", "alice.test")
	alice := env.Login(t, "did:plc:alice")

	status, body := uploadTo(t, app, http.MethodPut, "/api/v1/me/avatar", alice, "icon.jpg", smallJPEG(t))
	if status != http.StatusOK {
		t.Fatalf("put avatar: status = %d, want 200 (body=%s)", status, body)
	}
	var me meProfileBody
	testutil.MustJSONBody(t, body, &me)
	if me.AvatarURL == "" {
		t.Fatalf("avatarUrl is empty (body=%s)", body)
	}
	if len(env.Writer.Uploads()) != 1 || env.Writer.Uploads()[0].Mime != "image/jpeg" {
		t.Fatalf("uploads = %+v", env.Writer.Uploads())
	}
	if _, _, ok := env.Writer.Record(profileColl, profileRKey); !ok {
		t.Fatal("avatar was not written to the record")
	}

	status, body = testutil.Request(t, app, http.MethodDelete, "/api/v1/me/avatar", "", alice)
	if status != http.StatusOK {
		t.Fatalf("delete avatar: status = %d, want 200 (body=%s)", status, body)
	}
	var cleared meProfileBody
	testutil.MustJSONBody(t, body, &cleared)
	if cleared.AvatarURL != "" {
		t.Fatalf("avatarUrl = %q, want empty", cleared.AvatarURL)
	}
	rec, _, _ := env.Writer.Record(profileColl, profileRKey)
	if _, ok := rec["avatar"]; ok {
		t.Fatalf("avatar still in record: %+v", rec)
	}
}

func TestMeAvatarUnsupportedFormat(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)
	env.CreateUser(t, "did:plc:alice", "alice.test")
	alice := env.Login(t, "did:plc:alice")

	status, body := uploadTo(t, app, http.MethodPut, "/api/v1/me/avatar", alice, "x.gif", []byte("GIF89a not an image"))
	if status != http.StatusBadRequest {
		t.Fatalf("unsupported: status = %d, want 400 (body=%s)", status, body)
	}
	if e := testutil.WantErrorCode(t, body, "invalid_request"); e.Error.Details.Fields["file"] == "" {
		t.Fatalf("details.fields.file is empty (body=%s)", body)
	}
}

func TestMeBannerUpload(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)
	env.CreateUser(t, "did:plc:alice", "alice.test")
	alice := env.Login(t, "did:plc:alice")

	status, body := uploadTo(t, app, http.MethodPut, "/api/v1/me/banner", alice, "banner.jpg", smallJPEG(t))
	if status != http.StatusOK {
		t.Fatalf("put banner: status = %d, want 200 (body=%s)", status, body)
	}
	var me meProfileBody
	testutil.MustJSONBody(t, body, &me)
	if me.BannerURL == "" {
		t.Fatalf("bannerUrl is empty (body=%s)", body)
	}
}
