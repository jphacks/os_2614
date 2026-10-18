package content

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"image"
	"io"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"

	_ "image/png"

	_ "golang.org/x/image/webp"

	"github.com/applemango/chain/internal/httpx"
	"github.com/applemango/chain/internal/model"
	spacemodel "github.com/applemango/chain/internal/model/space"
	"github.com/applemango/chain/internal/realtime"
	"github.com/applemango/chain/internal/service/access"
	"github.com/applemango/chain/internal/service/core"
	"github.com/applemango/chain/internal/storage"
)

// メディアのアップロード・再エンコード・共有（10 §4.4）。
// 再エンコードで EXIF（位置情報を含む）を落とす。

const (
	// MediaLongSide は本体の長辺、MediaThumbLongSide はサムネイルの長辺（10 §4.4）。
	MediaLongSide      = 2048
	MediaThumbLongSide = 400
)

type MediaService struct {
	core.Deps
	Store   spacemodel.InstanceStore
	Access  access.AccessChecker
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
	Created bool
}

func (s *MediaService) Upload(ctx context.Context, did string, r io.Reader, size int64, takenAt *time.Time) (*MediaUpload, error) {
	if size > core.MediaMaxBytes {
		return nil, httpx.ErrPayloadTooLarge
	}
	data, err := io.ReadAll(io.LimitReader(r, core.MediaMaxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > core.MediaMaxBytes {
		return nil, httpx.ErrPayloadTooLarge
	}

	img, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, core.InvalidMedia("画像として読めません")
	}
	switch format {
	case "jpeg", "png", "webp":
	default:
		return nil, core.InvalidMedia("JPEG / PNG / WebP のみ対応しています")
	}

	full := core.ResizeLongSide(img, MediaLongSide)
	thumb := core.ResizeLongSide(img, MediaThumbLongSide)
	fullBytes, err := core.EncodeJPEG(full)
	if err != nil {
		return nil, err
	}
	thumbBytes, err := core.EncodeJPEG(thumb)
	if err != nil {
		return nil, err
	}

	id := s.IDs.NewID()
	now := s.Clock.Now()
	if err := s.Storage.Save(id, now, fullBytes, thumbBytes); err != nil {
		return nil, err
	}
	sum := sha256.Sum256(fullBytes)
	m := spacemodel.Media{
		ID: id, OwnerDID: did, Mime: "image/jpeg",
		Width: full.Bounds().Dx(), Height: full.Bounds().Dy(),
		Bytes: len(fullBytes), SHA256: hex.EncodeToString(sum[:]),
		TakenAt: takenAt, CreatedAt: now,
	}
	if err := spacemodel.InsertMedia(ctx, s.DB, m); err != nil {
		return nil, err
	}
	return &MediaUpload{ID: id, Width: m.Width, Height: m.Height}, nil
}

// ShareToSpace は Party の写真共有（10 §4.4）。media record を作り、post.created と同じ形で media.created を配る。
func (s *MediaService) ShareToSpace(ctx context.Context, did, spaceID, mediaID string, takenAt *time.Time) (*SharedMedia, error) {
	ok, err := s.Access.Check(ctx, spaceID, syntax.DID(did), access.ModeWrite)
	if err != nil {
		if !errors.Is(err, httpx.ErrGone) || !s.archivedPartyPhotoAllowed(ctx, did, spaceID, mediaID, takenAt) {
			return nil, err
		}
		ok = true
	}
	if !ok {
		return nil, httpx.ErrForbidden
	}
	sp, err := spacemodel.GetSpace(ctx, s.DB, spaceID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, httpx.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	m, err := spacemodel.GetMedia(ctx, s.DB, mediaID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, core.InvalidMedia("使用できないメディアです")
	}
	if err != nil {
		return nil, err
	}
	if m.OwnerDID != did || m.DeletedAt != nil {
		return nil, core.InvalidMedia("使用できないメディアです")
	}
	effectiveTakenAt := takenAt
	if effectiveTakenAt == nil {
		effectiveTakenAt = m.TakenAt
	}
	if existing, findErr := spacemodel.FindSharedMedia(ctx, s.DB, sp.ID, did, m.SHA256); findErr == nil {
		return &SharedMedia{URI: existing.URI, SpaceID: sp.ID, MediaID: m.ID, Created: false}, nil
	} else if !errors.Is(findErr, sql.ErrNoRows) {
		return nil, findErr
	}
	if m.SpaceID != nil {
		return nil, core.InvalidMedia("使用できないメディアです")
	}

	now := s.Clock.Now()
	value := mediaValue{Media: postMediaRef{ID: m.ID, Alt: ""}, CreatedAt: core.FormatDateTime(now)}
	if effectiveTakenAt != nil {
		value.TakenAt = effectiveTakenAt.UTC().Format(time.RFC3339)
	}
	rec, err := s.Store.CreateRecord(ctx, sp.SpaceRef(), syntax.DID(did), syntax.NSID(core.CollectionMedia), value)
	if err != nil {
		return nil, err
	}
	attached, err := spacemodel.AttachMedia(ctx, s.DB, m.ID, did, sp.ID)
	if err != nil {
		return nil, err
	}
	if !attached {
		return nil, core.InvalidMedia("使用できないメディアです")
	}

	item := &FeedItem{
		Kind: "instance_media", URI: rec.URI, Space: FeedSpaceOf(sp), CreatedAt: rec.CreatedAt,
		authorDID: did, Moderation: &ModerationInfo{Label: "ok", Display: "show"},
	}
	if author, err := core.AuthorSummaryFor(ctx, s.DB, did); err == nil {
		item.Author = author
	}
	if mi, err := mediaItemFor(ctx, s.DB, s.Signer, s.Config.PublicURL, now, m.ID, ""); err == nil && mi != nil {
		item.Media = []MediaItem{*mi}
	}
	s.Broker.Publish(ctx, realtime.Event{Name: "media.created", Space: sp.ID, Data: item})
	return &SharedMedia{URI: rec.URI, SpaceID: sp.ID, MediaID: m.ID, Created: true}, nil
}

// archivedPartyPhotoAllowed は archived Party の写真共有だけに許す終了後 24 時間の例外（10 §5）を判定する。
// 通常の write access にはこの条件を混ぜず、写真共有の経路からだけ呼び出す。
func (s *MediaService) archivedPartyPhotoAllowed(ctx context.Context, did, spaceID, mediaID string, takenAt *time.Time) bool {
	sp, err := spacemodel.GetSpace(ctx, s.DB, spaceID)
	if err != nil || sp.State != "archived" || sp.Type != model.SpaceTypeParty {
		return false
	}
	party, err := model.GetParty(ctx, s.DB, spaceID)
	if err != nil || party.ConfirmedAt == nil || party.EndedAt == nil {
		return false
	}
	media, err := spacemodel.GetMedia(ctx, s.DB, mediaID)
	if err != nil || media.OwnerDID != did || media.DeletedAt != nil {
		return false
	}
	if takenAt == nil {
		takenAt = media.TakenAt
	}
	if takenAt == nil {
		return false
	}
	now := s.Clock.Now()
	if now.Before(*party.EndedAt) || now.Sub(*party.EndedAt) > 24*time.Hour {
		return false
	}
	if takenAt.Before(*party.ConfirmedAt) || takenAt.After(*party.EndedAt) {
		return false
	}
	member, err := spacemodel.GetSpaceMember(ctx, s.DB, spaceID, did)
	return err == nil && member != nil && member.LeftAt == nil
}

// Deliver は署名付き URL の配信（10 §4.4）。署名と期限だけを確認し、ファイルの場所を返す。
func (s *MediaService) Deliver(ctx context.Context, id string, v storage.Variant, exp int64, sig string) (string, error) {
	if v != storage.VariantFull && v != storage.VariantThumb {
		return "", httpx.ErrForbidden
	}
	if !s.Signer.Verify(id, v, exp, sig, s.Clock.Now()) {
		return "", httpx.ErrForbidden
	}
	m, err := spacemodel.GetMedia(ctx, s.DB, id)
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
