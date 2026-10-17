package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"image"
	"image/jpeg"
	"io"
	"math"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"golang.org/x/image/draw"

	_ "golang.org/x/image/webp"
	_ "image/png"

	"github.com/applemango/chain/internal/httpx"
	"github.com/applemango/chain/internal/model"
	"github.com/applemango/chain/internal/realtime"
	"github.com/applemango/chain/internal/storage"
)

// メディアのアップロード・再エンコード・共有（10 §4.4）。
// 再エンコードで EXIF（位置情報を含む）を落とす。

const (
	// MediaMaxBytes は受け付けるファイルの上限（10 §4.4）。超えたら 413。
	MediaMaxBytes = 15 << 20
	// MediaLongSide は本体の長辺、MediaThumbLongSide はサムネイルの長辺（10 §4.4）。
	MediaLongSide      = 2048
	MediaThumbLongSide = 400
	mediaJPEGQuality   = 85
)

type MediaService struct {
	Deps
	Store   model.InstanceStore
	Access  AccessChecker
	Broker  realtime.Broker
	Storage *storage.Store
	Signer  storage.Signer
}

type MediaUpload struct {
	ID     string `json:"id"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

type SharedMedia struct {
	URI     string
	SpaceID string
	MediaID string
}

func (s *MediaService) Upload(ctx context.Context, did string, r io.Reader, size int64, takenAt *time.Time) (*MediaUpload, error) {
	if size > MediaMaxBytes {
		return nil, httpx.ErrPayloadTooLarge
	}
	data, err := io.ReadAll(io.LimitReader(r, MediaMaxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > MediaMaxBytes {
		return nil, httpx.ErrPayloadTooLarge
	}

	img, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, invalidMedia("画像として読めません")
	}
	switch format {
	case "jpeg", "png", "webp":
	default:
		return nil, invalidMedia("JPEG / PNG / WebP のみ対応しています")
	}

	full := resizeLongSide(img, MediaLongSide)
	thumb := resizeLongSide(img, MediaThumbLongSide)
	fullBytes, err := encodeJPEG(full)
	if err != nil {
		return nil, err
	}
	thumbBytes, err := encodeJPEG(thumb)
	if err != nil {
		return nil, err
	}

	id := s.IDs.NewID()
	now := s.Clock.Now()
	if err := s.Storage.Save(id, now, fullBytes, thumbBytes); err != nil {
		return nil, err
	}
	sum := sha256.Sum256(fullBytes)
	m := model.Media{
		ID: id, OwnerDID: did, Mime: "image/jpeg",
		Width: full.Bounds().Dx(), Height: full.Bounds().Dy(),
		Bytes: len(fullBytes), SHA256: hex.EncodeToString(sum[:]),
		TakenAt: takenAt, CreatedAt: now,
	}
	if err := model.InsertMedia(ctx, s.DB, m); err != nil {
		return nil, err
	}
	return &MediaUpload{ID: id, Width: m.Width, Height: m.Height}, nil
}

// ShareToSpace は Party の写真共有（10 §4.4）。media record を作り、post.created と同じ形で media.created を配る。
func (s *MediaService) ShareToSpace(ctx context.Context, did, spaceID, mediaID string, takenAt *time.Time) (*SharedMedia, error) {
	ok, err := s.Access.Check(ctx, spaceID, syntax.DID(did), ModeWrite)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, httpx.ErrForbidden
	}
	sp, err := model.GetSpace(ctx, s.DB, spaceID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, httpx.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	m, err := model.GetMedia(ctx, s.DB, mediaID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, invalidMedia("使用できないメディアです")
	}
	if err != nil {
		return nil, err
	}
	if m.OwnerDID != did || m.SpaceID != nil || m.DeletedAt != nil {
		return nil, invalidMedia("使用できないメディアです")
	}

	now := s.Clock.Now()
	value := mediaValue{Media: postMediaRef{ID: m.ID, Alt: ""}, CreatedAt: formatDateTime(now)}
	if takenAt != nil {
		value.TakenAt = takenAt.UTC().Format(time.RFC3339)
	}
	rec, err := s.Store.CreateRecord(ctx, sp.SpaceRef(), syntax.DID(did), syntax.NSID(CollectionMedia), value)
	if err != nil {
		return nil, err
	}
	attached, err := model.AttachMedia(ctx, s.DB, m.ID, did, sp.ID)
	if err != nil {
		return nil, err
	}
	if !attached {
		return nil, invalidMedia("使用できないメディアです")
	}

	item := &FeedItem{
		Kind: "instance_media", URI: rec.URI, Space: feedSpaceOf(sp), CreatedAt: rec.CreatedAt,
		authorDID: did, Moderation: &ModerationInfo{Label: "ok", Display: "show"},
	}
	if author, err := authorSummaryFor(ctx, s.DB, did); err == nil {
		item.Author = author
	}
	if mi, err := mediaItemFor(ctx, s.DB, s.Signer, s.Config.PublicURL, now, m.ID, ""); err == nil && mi != nil {
		item.Media = []MediaItem{*mi}
	}
	s.Broker.Publish(ctx, realtime.Event{Name: "media.created", Space: sp.ID, Data: item})
	return &SharedMedia{URI: rec.URI, SpaceID: sp.ID, MediaID: m.ID}, nil
}

// Deliver は署名付き URL の配信（10 §4.4）。署名と期限だけを確認し、ファイルの場所を返す。
func (s *MediaService) Deliver(ctx context.Context, id string, v storage.Variant, exp int64, sig string) (string, error) {
	if v != storage.VariantFull && v != storage.VariantThumb {
		return "", httpx.ErrForbidden
	}
	if !s.Signer.Verify(id, v, exp, sig, s.Clock.Now()) {
		return "", httpx.ErrForbidden
	}
	m, err := model.GetMedia(ctx, s.DB, id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", httpx.ErrForbidden
	}
	if err != nil {
		return "", err
	}
	if m.DeletedAt != nil {
		return "", httpx.ErrForbidden
	}
	return s.Storage.Path(id, m.CreatedAt, v), nil
}

func invalidMedia(reason string) *httpx.Error {
	return httpx.ErrInvalidRequest.With("fields", map[string]string{"file": reason})
}

func resizeLongSide(img image.Image, maxSide int) image.Image {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	longest := w
	if h > longest {
		longest = h
	}
	if longest <= maxSide || longest == 0 {
		return img
	}
	scale := float64(maxSide) / float64(longest)
	nw := maxInt(1, int(math.Round(float64(w)*scale)))
	nh := maxInt(1, int(math.Round(float64(h)*scale)))
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, b, draw.Over, nil)
	return dst
}

func encodeJPEG(img image.Image) ([]byte, error) {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: mediaJPEGQuality}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
