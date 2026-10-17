//go:build integration

package service_test

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"

	"github.com/applemango/chain/internal/external/atproto"
	"github.com/applemango/chain/internal/model"
	"github.com/applemango/chain/internal/service"
	"github.com/applemango/chain/internal/testutil"
)

// CHAIN からのプロフィール編集（13 §1.3）。

const (
	profileDID        = "did:plc:profile"
	bskyProfileColl   = "app.bsky.actor.profile"
	bskyProfileRKey   = "self"
	profileJPEGMaxLen = 1_000_000
)

func newProfileHarness(t *testing.T, moderator service.Moderator) (*testutil.Env, *service.MeService, string) {
	t.Helper()
	env := testutil.NewEnv(t)
	env.CreateUser(t, profileDID, "profile.test")
	env.Login(t, profileDID)
	me := &service.MeService{Deps: env.Deps, Moderator: moderator, Writer: env.Writer}
	return env, me, sidFor(t, env, profileDID)
}

func strptr(s string) *string { return &s }

func TestUpdateProfilePreservesUnknownFields(t *testing.T) {
	env, me, sid := newProfileHarness(t, service.ModerationNoop{})
	env.Writer.SetRecord(bskyProfileColl, bskyProfileRKey, map[string]any{
		"$type":       bskyProfileColl,
		"displayName": "Old",
		"description": "OldDesc",
		"pronouns":    "they/them",
		"website":     "https://example.com",
	}, "cid-old")

	res, err := me.UpdateProfile(context.Background(), profileDID, sid, service.ProfileUpdate{DisplayName: strptr("New")})
	if err != nil {
		t.Fatalf("UpdateProfile: %v", err)
	}

	calls := env.Writer.Calls()
	if len(calls) != 1 {
		t.Fatalf("writer calls = %d, want 1", len(calls))
	}
	c := calls[0]
	if c.Collection != bskyProfileColl || c.RKey != bskyProfileRKey {
		t.Fatalf("call = %+v", c)
	}
	if c.SwapRecord != "cid-old" {
		t.Fatalf("swapRecord = %q, want cid-old", c.SwapRecord)
	}
	rec, ok := c.Record.(map[string]any)
	if !ok {
		t.Fatalf("record type = %T", c.Record)
	}
	if rec["pronouns"] != "they/them" || rec["website"] != "https://example.com" {
		t.Fatalf("unknown fields were dropped: %+v", rec)
	}
	if rec["displayName"] != "New" || rec["description"] != "OldDesc" {
		t.Fatalf("record = %+v", rec)
	}

	prof, err := model.GetProfile(context.Background(), env.DB, profileDID)
	if err != nil {
		t.Fatalf("GetProfile: %v", err)
	}
	if prof.DisplayName != "New" || prof.Description != "OldDesc" {
		t.Fatalf("stored profile = %+v", prof)
	}
	if res.Profile.DisplayName != "New" {
		t.Fatalf("returned displayName = %q, want New", res.Profile.DisplayName)
	}
}

func TestUpdateProfileEmptyClearsField(t *testing.T) {
	env, me, sid := newProfileHarness(t, service.ModerationNoop{})
	env.Writer.SetRecord(bskyProfileColl, bskyProfileRKey, map[string]any{
		"$type":       bskyProfileColl,
		"displayName": "Old",
		"description": "OldDesc",
		"pronouns":    "they/them",
	}, "cid-old")

	if _, err := me.UpdateProfile(context.Background(), profileDID, sid, service.ProfileUpdate{DisplayName: strptr("")}); err != nil {
		t.Fatalf("UpdateProfile: %v", err)
	}

	c := env.Writer.Calls()[0]
	rec := c.Record.(map[string]any)
	if _, ok := rec["displayName"]; ok {
		t.Fatalf("displayName was not removed: %+v", rec)
	}
	if rec["pronouns"] != "they/them" || rec["description"] != "OldDesc" {
		t.Fatalf("other fields changed: %+v", rec)
	}

	prof, err := model.GetProfile(context.Background(), env.DB, profileDID)
	if err != nil {
		t.Fatalf("GetProfile: %v", err)
	}
	if prof.DisplayName != "" || prof.Description != "OldDesc" {
		t.Fatalf("stored profile = %+v", prof)
	}
}

func TestUpdateProfileCreatesWhenMissing(t *testing.T) {
	env, me, sid := newProfileHarness(t, service.ModerationNoop{})

	if _, err := me.UpdateProfile(context.Background(), profileDID, sid, service.ProfileUpdate{DisplayName: strptr("Fresh")}); err != nil {
		t.Fatalf("UpdateProfile: %v", err)
	}
	c := env.Writer.Calls()[0]
	if c.SwapRecord != "" {
		t.Fatalf("swapRecord = %q, want empty for a new record", c.SwapRecord)
	}
	rec := c.Record.(map[string]any)
	if rec["$type"] != bskyProfileColl || rec["displayName"] != "Fresh" {
		t.Fatalf("record = %+v", rec)
	}
}

func TestUpdateProfileModerationBlocked(t *testing.T) {
	env, me, sid := newProfileHarness(t, &testutil.FakeAssessor{BlockWords: []string{"誹謗"}})

	_, err := me.UpdateProfile(context.Background(), profileDID, sid, service.ProfileUpdate{DisplayName: strptr("誹謗する名前")})
	wantHTTPError(t, err, "moderation_blocked")
	if len(env.Writer.Calls()) != 0 {
		t.Fatalf("wrote to PDS despite block: %+v", env.Writer.Calls())
	}
	if len(env.Writer.Uploads()) != 0 {
		t.Fatal("uploaded a blob despite block")
	}
}

func TestUpdateProfileSwapConflict(t *testing.T) {
	env, me, sid := newProfileHarness(t, service.ModerationNoop{})
	env.Writer.SetRecord(bskyProfileColl, bskyProfileRKey, map[string]any{"$type": bskyProfileColl, "displayName": "Old"}, "cid-old")
	env.Writer.SetSwapConflict(true)

	_, err := me.UpdateProfile(context.Background(), profileDID, sid, service.ProfileUpdate{DisplayName: strptr("New")})
	wantHTTPError(t, err, "conflict")

	prof, err := model.GetProfile(context.Background(), env.DB, profileDID)
	if err != nil {
		t.Fatalf("GetProfile: %v", err)
	}
	if prof.DisplayName != "profile.test" {
		t.Fatalf("profile changed despite conflict: %+v", prof)
	}
}

func TestUpdateProfileWriterErrors(t *testing.T) {
	env, me, sid := newProfileHarness(t, service.ModerationNoop{})

	env.Writer.SetError(atproto.ErrSessionExpired)
	if _, err := me.UpdateProfile(context.Background(), profileDID, sid, service.ProfileUpdate{DisplayName: strptr("x")}); err == nil {
		t.Fatal("want error")
	} else {
		wantHTTPError(t, err, "atproto_session_expired")
	}

	env.Writer.SetError(atproto.ErrReauthRequired)
	if _, err := me.UpdateProfile(context.Background(), profileDID, sid, service.ProfileUpdate{DisplayName: strptr("x")}); err == nil {
		t.Fatal("want error")
	} else {
		wantHTTPError(t, err, "atproto_reauth_required")
	}

	env.Writer.SetError(errors.New("pds down"))
	if _, err := me.UpdateProfile(context.Background(), profileDID, sid, service.ProfileUpdate{DisplayName: strptr("x")}); err == nil {
		t.Fatal("want error")
	} else {
		wantHTTPError(t, err, "upstream_error")
	}
}

func TestUpdateAvatarResizesUploadsAndStoresCID(t *testing.T) {
	env, me, sid := newProfileHarness(t, service.ModerationNoop{})

	big := pngBytes(t, 2500, 1600)
	res, err := me.UpdateAvatar(context.Background(), profileDID, sid, bytes.NewReader(big), int64(len(big)))
	if err != nil {
		t.Fatalf("UpdateAvatar: %v", err)
	}

	uploads := env.Writer.Uploads()
	if len(uploads) != 1 {
		t.Fatalf("uploads = %d, want 1", len(uploads))
	}
	if uploads[0].Mime != "image/jpeg" {
		t.Fatalf("mime = %q, want image/jpeg", uploads[0].Mime)
	}
	if len(uploads[0].Data) > profileJPEGMaxLen {
		t.Fatalf("uploaded %d bytes, want <= %d", len(uploads[0].Data), profileJPEGMaxLen)
	}
	img, format, err := image.Decode(bytes.NewReader(uploads[0].Data))
	if err != nil || format != "jpeg" {
		t.Fatalf("decode upload: format=%q err=%v", format, err)
	}
	if b := img.Bounds(); b.Dx() > 1000 || b.Dy() > 1000 {
		t.Fatalf("avatar size = %dx%d, want within 1000x1000", b.Dx(), b.Dy())
	}

	rec, _, ok := env.Writer.Record(bskyProfileColl, bskyProfileRKey)
	if !ok {
		t.Fatal("record was not written")
	}
	blob, ok := rec["avatar"].(*atproto.Blob)
	if !ok {
		t.Fatalf("record avatar = %T", rec["avatar"])
	}
	if res.Profile.AvatarCID != blob.Ref.Link || blob.Ref.Link == "" {
		t.Fatalf("avatar cid = %q, blob = %q", res.Profile.AvatarCID, blob.Ref.Link)
	}
}

func TestUpdateBannerFitsLimitAndQuality(t *testing.T) {
	env, me, sid := newProfileHarness(t, service.ModerationNoop{})

	noisy := pngNoise(t, 3200, 1200)
	if _, err := me.UpdateBanner(context.Background(), profileDID, sid, bytes.NewReader(noisy), int64(len(noisy))); err != nil {
		t.Fatalf("UpdateBanner: %v", err)
	}

	uploads := env.Writer.Uploads()
	if len(uploads) != 1 {
		t.Fatalf("uploads = %d, want 1", len(uploads))
	}
	if len(uploads[0].Data) > profileJPEGMaxLen {
		t.Fatalf("uploaded %d bytes, want <= %d", len(uploads[0].Data), profileJPEGMaxLen)
	}
	img, _, err := image.Decode(bytes.NewReader(uploads[0].Data))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if b := img.Bounds(); b.Dx() > 3000 || b.Dy() > 1000 {
		t.Fatalf("banner size = %dx%d, want within 3000x1000", b.Dx(), b.Dy())
	}
}

func TestUpdateAvatarStripsEXIF(t *testing.T) {
	env, me, sid := newProfileHarness(t, service.ModerationNoop{})

	original := jpegWithEXIF(t)
	if !bytes.Contains(original, []byte("Exif")) {
		t.Fatal("fixture does not contain EXIF")
	}
	if _, err := me.UpdateAvatar(context.Background(), profileDID, sid, bytes.NewReader(original), int64(len(original))); err != nil {
		t.Fatalf("UpdateAvatar: %v", err)
	}
	if bytes.Contains(env.Writer.Uploads()[0].Data, []byte("Exif")) {
		t.Fatal("uploaded JPEG still contains EXIF")
	}
}

func TestUpdateAvatarUnsupportedFormat(t *testing.T) {
	_, me, sid := newProfileHarness(t, service.ModerationNoop{})

	_, err := me.UpdateAvatar(context.Background(), profileDID, sid, bytes.NewReader([]byte("GIF89a not an image")), 17)
	wantHTTPError(t, err, "invalid_request")
}

func TestDeleteAvatarRemovesFromRecord(t *testing.T) {
	env, me, sid := newProfileHarness(t, service.ModerationNoop{})
	env.Writer.SetRecord(bskyProfileColl, bskyProfileRKey, map[string]any{
		"$type":    bskyProfileColl,
		"avatar":   &atproto.Blob{Type: "blob", Ref: atproto.BlobLink{Link: "cid-avatar"}},
		"banner":   &atproto.Blob{Type: "blob", Ref: atproto.BlobLink{Link: "cid-banner"}},
		"pronouns": "they/them",
	}, "cid-old")

	res, err := me.DeleteAvatar(context.Background(), profileDID, sid)
	if err != nil {
		t.Fatalf("DeleteAvatar: %v", err)
	}
	rec, _, _ := env.Writer.Record(bskyProfileColl, bskyProfileRKey)
	if _, ok := rec["avatar"]; ok {
		t.Fatalf("avatar was not removed: %+v", rec)
	}
	if _, ok := rec["banner"]; !ok {
		t.Fatalf("banner should remain: %+v", rec)
	}
	if env.Writer.Calls()[0].SwapRecord != "cid-old" {
		t.Fatalf("swapRecord = %q, want cid-old", env.Writer.Calls()[0].SwapRecord)
	}
	if res.Profile.AvatarCID != "" {
		t.Fatalf("stored avatar cid = %q, want empty", res.Profile.AvatarCID)
	}
}

func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 128, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}
	return buf.Bytes()
}

func pngNoise(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	seed := uint32(1)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			seed = seed*1664525 + 1013904223
			img.Set(x, y, color.RGBA{R: uint8(seed >> 24), G: uint8(seed >> 16), B: uint8(seed >> 8), A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}
	return buf.Bytes()
}

func jpegWithEXIF(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 40, 30))
	for y := 0; y < 30; y++ {
		for x := 0; x < 40; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 6), G: uint8(y * 8), B: 64, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatalf("jpeg.Encode: %v", err)
	}
	plain := buf.Bytes()

	payload := []byte("Exif\x00\x00II*\x00\x08\x00\x00\x00\x00\x00")
	segLen := len(payload) + 2
	seg := []byte{0xFF, 0xE1, byte(segLen >> 8), byte(segLen & 0xFF)}
	seg = append(seg, payload...)

	out := append([]byte{}, plain[:2]...)
	out = append(out, seg...)
	return append(out, plain[2:]...)
}
