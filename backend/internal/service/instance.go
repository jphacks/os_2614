package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/applemango/chain/internal/external/atproto"
	"github.com/applemango/chain/internal/httpx"
	"github.com/applemango/chain/internal/kv"
	"github.com/applemango/chain/internal/model"
	"github.com/applemango/chain/internal/realtime"
	"github.com/applemango/chain/internal/storage"
	"github.com/applemango/chain/internal/utils"
)

// instance（グループ / Party 内の投稿）の作成・一覧・削除（10 §4.1〜§4.3）。
// record の読み書きは InstanceStore 越しに行い、Spaces 統合時に差し替えられるようにする。

const (
	CollectionPost  = "jp.i32.chain.instance.post"
	CollectionMedia = "jp.i32.chain.instance.media"
	CollectionPin   = "jp.i32.chain.instance.pin"

	spaceTypeGroup = "jp.i32.chain.space.group"
	spaceTypeParty = "jp.i32.chain.space.party"

	mediaURLTTL = time.Hour
)

type InstanceService struct {
	Deps
	Store    model.InstanceStore
	Access   AccessChecker
	Assessor PostAssessor
	Broker   realtime.Broker
	Signer   storage.Signer
}

// MediaInput は投稿に付けるメディア（10 §4.1）。
type MediaInput struct {
	ID  string
	Alt string
}

type CreatePostInput struct {
	SpaceIDs   []string
	Text       string
	Format     string
	Media      []MediaInput
	AlsoPublic bool
}

type CreatedPost struct {
	URI     string `json:"uri"`
	SpaceID string `json:"spaceId"`
}

type CreatePostResult struct {
	Items []CreatedPost `json:"items"`
}

type AuthorSummary struct {
	DID         string `json:"did"`
	Handle      string `json:"handle"`
	DisplayName string `json:"displayName,omitempty"`
	AvatarURL   string `json:"avatarUrl,omitempty"`
}

type MediaItem struct {
	ID       string `json:"id"`
	Alt      string `json:"alt"`
	URL      string `json:"url"`
	ThumbURL string `json:"thumbUrl"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
}

type ModerationInfo struct {
	Label      string   `json:"label"`
	Display    string   `json:"display"`
	Categories []string `json:"categories,omitempty"`
}

// PostItem は §4.2 の 1 要素。post と media record を同じ形で返す。
type PostItem struct {
	Kind       string          `json:"kind"`
	URI        string          `json:"uri"`
	SpaceID    string          `json:"spaceId"`
	Author     *AuthorSummary  `json:"author"`
	Text       string          `json:"text,omitempty"`
	Format     string          `json:"format,omitempty"`
	Media      []MediaItem     `json:"media,omitempty"`
	Moderation *ModerationInfo `json:"moderation,omitempty"`
	Hidden     string          `json:"hidden,omitempty"`
	TakenAt    *time.Time      `json:"takenAt,omitempty"`
	CreatedAt  time.Time       `json:"createdAt"`
}

type PostPage struct {
	Items  []PostItem `json:"items"`
	Cursor string     `json:"cursor,omitempty"`
}

type postValue struct {
	Text      string         `json:"text"`
	Format    string         `json:"format"`
	Media     []postMediaRef `json:"media,omitempty"`
	CreatedAt string         `json:"createdAt"`
}

type postMediaRef struct {
	ID  string `json:"id"`
	Alt string `json:"alt"`
}

type mediaValue struct {
	Media     postMediaRef `json:"media"`
	TakenAt   string       `json:"takenAt,omitempty"`
	CreatedAt string       `json:"createdAt"`
}

func formatDateTime(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000Z") }

// CreatePosts は §4.1。spaceIds ごとに別の record を作り、moderation は 1 回だけ実行する。
func (s *InstanceService) CreatePosts(ctx context.Context, did string, in CreatePostInput) (*CreatePostResult, error) {
	settings, err := model.GetSettings(ctx, s.DB, did)
	if err != nil {
		return nil, err
	}
	if settings.ContentScanConsentVersion == nil || *settings.ContentScanConsentVersion != ContentScanConsentVersion {
		return nil, httpx.ErrConsentRequired
	}

	spaces := make([]*model.Space, 0, len(in.SpaceIDs))
	for _, id := range in.SpaceIDs {
		sp, err := model.GetSpace(ctx, s.DB, id)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, httpx.ErrNotFound.With("fields", map[string]string{"spaceIds": "存在しない Space が含まれています"})
		}
		if err != nil {
			return nil, err
		}
		ok, err := s.Access.Check(ctx, id, syntax.DID(did), ModeWrite)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, httpx.ErrForbidden
		}
		spaces = append(spaces, sp)
	}

	if len(in.Media) > 0 {
		if len(in.SpaceIDs) != 1 {
			return nil, httpx.ErrInvalidRequest.With("fields", map[string]string{"media": "メディアを付けられるのは 1 つの Space への投稿だけです"})
		}
		for _, m := range in.Media {
			if err := s.checkUsableMedia(ctx, did, m.ID); err != nil {
				return nil, err
			}
		}
	}

	label, modState, err := s.assess(ctx, did, in.Text)
	if err != nil {
		return nil, err
	}

	now := s.Clock.Now()
	refs := make([]postMediaRef, 0, len(in.Media))
	for _, m := range in.Media {
		refs = append(refs, postMediaRef{ID: m.ID, Alt: m.Alt})
	}
	value := postValue{Text: in.Text, Format: in.Format, Media: refs, CreatedAt: formatDateTime(now)}

	result := &CreatePostResult{Items: make([]CreatedPost, 0, len(spaces))}
	for _, sp := range spaces {
		rec, err := s.Store.CreateRecord(ctx, sp.SpaceRef(), syntax.DID(did), syntax.NSID(CollectionPost), value)
		if err != nil {
			return nil, err
		}
		if label != "ok" || modState != "done" {
			if err := model.SetRecordModeration(ctx, s.DB, rec.URI, label, modState); err != nil {
				return nil, err
			}
			rec.ModLabel, rec.ModState = label, modState
		}
		result.Items = append(result.Items, CreatedPost{URI: rec.URI, SpaceID: sp.ID})

		if len(in.Media) > 0 {
			if err := s.attachMedia(ctx, did, sp.ID, in.Media); err != nil {
				return nil, err
			}
		}
		if item, err := s.renderRecord(ctx, did, rec, settings.WarnDisplay, nil); err != nil {
			return nil, err
		} else if item != nil {
			s.Broker.Publish(ctx, realtime.Event{Name: "post.created", Space: sp.ID, Data: item})
		}
	}
	return result, nil
}

func (s *InstanceService) checkUsableMedia(ctx context.Context, did, id string) error {
	m, err := model.GetMedia(ctx, s.DB, id)
	if errors.Is(err, sql.ErrNoRows) {
		return httpx.ErrInvalidRequest.With("fields", map[string]string{"media": "使用できないメディアが含まれています"})
	}
	if err != nil {
		return err
	}
	if m.OwnerDID != did || m.SpaceID != nil || m.DeletedAt != nil {
		return httpx.ErrInvalidRequest.With("fields", map[string]string{"media": "使用できないメディアが含まれています"})
	}
	return nil
}

func (s *InstanceService) attachMedia(ctx context.Context, did, spaceID string, media []MediaInput) error {
	for _, m := range media {
		ok, err := model.AttachMedia(ctx, s.DB, m.ID, did, spaceID)
		if err != nil {
			return err
		}
		if !ok {
			return httpx.ErrInvalidRequest.With("fields", map[string]string{"media": "使用できないメディアが含まれています"})
		}
	}
	return nil
}

// assess は moderation を 1 回だけ実行し、保存する mod_label / mod_state を返す（11 §3）。
func (s *InstanceService) assess(ctx context.Context, did, text string) (label, state string, err error) {
	verdict, _, err := s.Assessor.Assess(ctx, Content{Kind: "instance_post", Text: text})
	if err != nil {
		slog.WarnContext(ctx, "moderation error; storing pending", "err", err)
		if perr := s.Redis.LPush(ctx, kv.ModerationQueueKey(), did).Err(); perr != nil {
			slog.WarnContext(ctx, "modq push failed", "err", perr)
		}
		return "ok", "pending", nil
	}
	switch verdict.Label {
	case LabelBlock:
		return "", "", httpx.ErrModerationBlocked.With("categories", verdict.Categories)
	case LabelWarn:
		return "warn", "done", nil
	default:
		return "ok", "done", nil
	}
}

// ListPosts は §4.2。新しい順に post と media record を混ぜて返し、ブロック関係と moderation を反映する。
func (s *InstanceService) ListPosts(ctx context.Context, viewer, spaceID, cursor string, limit int) (*PostPage, error) {
	ok, err := s.Access.Check(ctx, spaceID, syntax.DID(viewer), ModeRead)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, httpx.ErrForbidden
	}

	beforeAt, beforeURI, err := utils.DecodeCursor(cursor)
	if err != nil {
		return nil, httpx.ErrInvalidRequest.With("fields", map[string]string{"cursor": "カーソルが不正です"})
	}
	records, err := model.ListRecordsInSpace(ctx, s.DB, spaceID,
		[]string{CollectionPost, CollectionMedia}, beforeAt, beforeURI, limit+1)
	if err != nil {
		return nil, err
	}

	blocked, err := model.BlockedDIDs(ctx, s.DB, viewer)
	if err != nil {
		return nil, err
	}
	warnDisplay, err := s.warnDisplay(ctx, viewer)
	if err != nil {
		return nil, err
	}

	page := &PostPage{Items: make([]PostItem, 0, limit)}
	for _, rec := range records {
		if len(page.Items) >= limit {
			break
		}
		item, err := s.renderRecord(ctx, viewer, rec, warnDisplay, blocked)
		if err != nil {
			return nil, err
		}
		if item == nil {
			continue
		}
		page.Items = append(page.Items, *item)
		page.Cursor = utils.EncodeCursor(rec.CreatedAt.UnixMilli(), rec.URI)
	}
	if len(records) < limit+1 {
		page.Cursor = ""
	}
	return page, nil
}

func (s *InstanceService) warnDisplay(ctx context.Context, did string) (string, error) {
	st, err := model.GetSettings(ctx, s.DB, did)
	if errors.Is(err, sql.ErrNoRows) {
		return "blur", nil
	}
	if err != nil {
		return "", err
	}
	return st.WarnDisplay, nil
}

// DeletePost は投稿者本人の削除（10 §4.3）。論理削除して post.deleted を配信する。
func (s *InstanceService) DeletePost(ctx context.Context, did, spaceID, rkey string) error {
	ok, err := s.Access.Check(ctx, spaceID, syntax.DID(did), ModeRead)
	if err != nil {
		return err
	}
	if !ok {
		return httpx.ErrForbidden
	}
	sp, err := model.GetSpace(ctx, s.DB, spaceID)
	if errors.Is(err, sql.ErrNoRows) {
		return httpx.ErrNotFound
	}
	if err != nil {
		return err
	}
	uri := sp.SpaceRef().RecordURI(did, CollectionPost, rkey)
	rec, err := model.GetRecord(ctx, s.DB, uri)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && rec.DeletedAt != nil) {
		return httpx.ErrNotFound
	}
	if err != nil {
		return err
	}
	if _, err := model.SoftDeleteRecord(ctx, s.DB, uri, did, s.Clock.Now()); err != nil {
		return err
	}
	s.Broker.Publish(ctx, realtime.Event{Name: "post.deleted", Space: spaceID, Data: map[string]string{"uri": uri}})
	return nil
}

// ListInstances は GET /instances（13 §2）。
func (s *InstanceService) ListInstances(ctx context.Context, did string) ([]model.SpaceListItem, error) {
	return model.ListUserSpaces(ctx, s.DB, did)
}

func (s *InstanceService) renderRecord(ctx context.Context, viewer string, rec model.Record, warnDisplay string, blocked map[string]bool) (*PostItem, error) {
	if blocked[rec.AuthorDID] {
		return nil, nil
	}
	if rec.ModLabel == "block" && rec.AuthorDID != viewer {
		return nil, nil
	}
	if rec.ModLabel == "warn" && warnDisplay == "hide" && rec.AuthorDID != viewer {
		return nil, nil
	}

	author, err := authorSummaryFor(ctx, s.DB, rec.AuthorDID)
	if err != nil {
		return nil, err
	}
	item := &PostItem{URI: rec.URI, SpaceID: rec.SpaceID, Author: author, CreatedAt: rec.CreatedAt}

	switch rec.Collection {
	case CollectionPost:
		var v postValue
		if err := json.Unmarshal([]byte(rec.ValueJSON), &v); err != nil {
			return nil, err
		}
		item.Kind = "post"
		item.Text = v.Text
		item.Format = v.Format
		for _, ref := range v.Media {
			mi, err := mediaItemFor(ctx, s.DB, s.Signer, s.Config.PublicURL, s.Clock.Now(), ref.ID, ref.Alt)
			if err != nil {
				return nil, err
			}
			if mi != nil {
				item.Media = append(item.Media, *mi)
			}
		}
	default:
		var v mediaValue
		if err := json.Unmarshal([]byte(rec.ValueJSON), &v); err != nil {
			return nil, err
		}
		item.Kind = "media"
		if mi, err := mediaItemFor(ctx, s.DB, s.Signer, s.Config.PublicURL, s.Clock.Now(), v.Media.ID, v.Media.Alt); err != nil {
			return nil, err
		} else if mi != nil {
			item.Media = []MediaItem{*mi}
		}
		if v.TakenAt != "" {
			if t, err := time.Parse(time.RFC3339, v.TakenAt); err == nil {
				item.TakenAt = &t
			}
		}
	}

	switch rec.ModLabel {
	case "block":
		item.Hidden = "moderation"
		item.Moderation = &ModerationInfo{Label: "block", Display: "hide"}
	case "warn":
		item.Moderation = &ModerationInfo{Label: "warn", Display: warnDisplay}
	default:
		item.Moderation = &ModerationInfo{Label: "ok", Display: "show"}
	}
	return item, nil
}

func mediaItemFor(ctx context.Context, q *sql.DB, signer storage.Signer, publicURL string, now time.Time, id, alt string) (*MediaItem, error) {
	m, err := model.GetMedia(ctx, q, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if m.DeletedAt != nil {
		return nil, nil
	}
	exp := now.Add(mediaURLTTL).Unix()
	return &MediaItem{
		ID:       m.ID,
		Alt:      alt,
		URL:      signer.URL(publicURL, m.ID, storage.VariantFull, exp),
		ThumbURL: signer.URL(publicURL, m.ID, storage.VariantThumb, exp),
		Width:    m.Width,
		Height:   m.Height,
	}, nil
}

func authorSummaryFor(ctx context.Context, q *sql.DB, did string) (*AuthorSummary, error) {
	u, err := model.GetUser(ctx, q, did)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	summary := &AuthorSummary{DID: did}
	if u != nil {
		summary.Handle = u.Handle
	}
	p, err := model.GetProfile(ctx, q, did)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if p != nil {
		summary.DisplayName = p.DisplayName
		summary.AvatarURL = atproto.AvatarURL(did, p.AvatarCID)
	}
	return summary, nil
}
