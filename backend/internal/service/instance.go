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
	"github.com/applemango/chain/internal/model"
	"github.com/applemango/chain/internal/realtime"
	"github.com/applemango/chain/internal/storage"
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
	Pins     *PinService
	Public   *PublicPostService
}

// MediaInput は投稿に付けるメディア（10 §4.1）。
type MediaInput struct {
	ID  string
	Alt string
}

// ReplyInput は返信先（10 §3）。root と parent は同じ Space の投稿の at-uri。
type ReplyInput struct {
	Root   string
	Parent string
}

type CreatePostInput struct {
	SpaceIDs    []string
	Text        string
	Format      string
	Media       []MediaInput
	Reply       *ReplyInput
	SessionID   string
	AlsoPublic  bool
	AlsoBluesky bool
}

type CreatedPost struct {
	URI     string `json:"uri"`
	SpaceID string `json:"spaceId"`
}

// PublicPostOutcome は instance 投稿と同時に作る公開投稿の結果（10 §4.1）。
// 失敗したときは Error に API の code を入れる。
type PublicPostOutcome struct {
	URI     string `json:"uri,omitempty"`
	CID     string `json:"cid,omitempty"`
	BskyURI string `json:"bskyUri,omitempty"`
	Error   string `json:"error,omitempty"`
}

type CreatePostResult struct {
	Items  []CreatedPost      `json:"items"`
	Public *PublicPostOutcome `json:"public,omitempty"`
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

// ReplyRef は投稿の返信先（10 §3）。FeedItem の reply に使う。
type ReplyRef struct {
	Root   string `json:"root"`
	Parent string `json:"parent"`
}

type postValue struct {
	Text      string         `json:"text"`
	Format    string         `json:"format"`
	Reply     *postReplyRef  `json:"reply,omitempty"`
	System    bool           `json:"system,omitempty"`
	Media     []postMediaRef `json:"media,omitempty"`
	CreatedAt string         `json:"createdAt"`
}

// postReplyRef は投稿の返信先（10 §3）。
type postReplyRef struct {
	Root   string `json:"root"`
	Parent string `json:"parent"`
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

	if in.Reply != nil {
		if len(in.SpaceIDs) != 1 {
			return nil, httpx.ErrInvalidRequest.With("fields", map[string]string{"reply": "返信できるのは 1 つの Space への投稿だけです"})
		}
		if err := s.checkReply(ctx, in.SpaceIDs[0], in.Reply); err != nil {
			return nil, err
		}
	}

	decision := s.assess(ctx, in.Text)
	if decision.Label == "" {
		return nil, decision.Cause
	}
	if decision.Pending {
		logModerationError(ctx, decision.Cause)
	}
	label, modState := decision.Label, decision.State

	now := s.Clock.Now()
	refs := make([]postMediaRef, 0, len(in.Media))
	for _, m := range in.Media {
		refs = append(refs, postMediaRef{ID: m.ID, Alt: m.Alt})
	}
	value := postValue{Text: in.Text, Format: in.Format, Media: refs, CreatedAt: formatDateTime(now)}
	if in.Reply != nil {
		value.Reply = &postReplyRef{Root: in.Reply.Root, Parent: in.Reply.Parent}
	}

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
		if decision.Pending {
			if err := model.EnqueueModerationJob(ctx, s.Redis, rec.URI); err != nil {
				slog.WarnContext(ctx, "modq push failed", "err", err)
			}
		}
		result.Items = append(result.Items, CreatedPost{URI: rec.URI, SpaceID: sp.ID})

		if len(in.Media) > 0 {
			if err := s.attachMedia(ctx, did, sp.ID, in.Media); err != nil {
				return nil, err
			}
		}
		if item, err := newInstanceItem(ctx, s.DB, sp, rec); err != nil {
			return nil, err
		} else if ok, err := decorateItem(ctx, s.DB, s.Config.PublicURL, s.Signer, s.Clock.Now(), did, settings.WarnDisplay, nil, item); err != nil {
			return nil, err
		} else if ok {
			s.Broker.Publish(ctx, realtime.Event{Name: "post.created", Space: sp.ID, Data: item})
		}
		if decision.Pin && s.Pins != nil {
			if err := s.Pins.CreateAuto(ctx, sp.ID, rec.URI); err != nil {
				slog.WarnContext(ctx, "auto pin failed", "uri", rec.URI, "err", err)
			}
		}
		if sp.Type == spaceTypeParty {
			// party では投稿も「活動」として数える（08 §4.2 の locked の無活動判定）。
			if err := model.TouchPartyActivity(ctx, s.DB, sp.ID, now); err != nil {
				return nil, err
			}
		}
	}
	if in.AlsoPublic {
		result.Public = s.createPublicPost(ctx, did, in)
	}
	return result, nil
}

// createPublicPost は instance 投稿と同時に公開投稿を作る（17 §2.1）。失敗しても instance の
// 投稿は成功させ、応答の public.error に code を入れる（10 §4.1）。
func (s *InstanceService) createPublicPost(ctx context.Context, did string, in CreatePostInput) *PublicPostOutcome {
	if s.Public == nil {
		return &PublicPostOutcome{Error: httpx.ErrUpstream.Code}
	}
	res, err := s.Public.Create(ctx, did, in.SessionID, CreatePublicPostInput{
		Text:        in.Text,
		AlsoBluesky: in.AlsoBluesky,
	})
	if err != nil {
		slog.WarnContext(ctx, "public post failed", "code", HttpErrorCode(err))
		return &PublicPostOutcome{Error: HttpErrorCode(err)}
	}
	return &PublicPostOutcome{URI: res.URI, CID: res.CID, BskyURI: res.BskyURI}
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

// moderationDecision は同期判定の結果。Label が空なら block（Cause に 422 が入る）。
type moderationDecision struct {
	Label      string
	State      string
	Categories []string
	Pin        bool
	Pending    bool
	Cause      error
}

// assess は moderation を 1 回だけ実行し、保存する mod_label / mod_state を返す（11 §3）。
// タイムアウトは同期判定の 2 秒（11 §5.1）。判定が取れないときは pending とし、呼び出し側が
// 作成した record の URI を modq に積む。
func (s *InstanceService) assess(ctx context.Context, text string) moderationDecision {
	assessCtx, cancel := context.WithTimeout(ctx, ModerationSyncTimeout)
	defer cancel()
	verdict, pin, err := s.Assessor.Assess(assessCtx, Content{Kind: ContentKindInstancePost, Text: text})
	if err != nil {
		return moderationDecision{Label: "ok", State: "pending", Pending: true, Cause: err}
	}
	switch verdict.Label {
	case LabelBlock:
		return moderationDecision{
			Categories: verdict.Categories,
			Cause:      httpx.ErrModerationBlocked.With("categories", verdict.Categories),
		}
	case LabelWarn:
		return moderationDecision{Label: "warn", State: "done", Pin: pin}
	default:
		return moderationDecision{Label: "ok", State: "done", Pin: pin}
	}
}

// checkReply は返信先の root / parent を検証する（10 §3）。どちらも同じ Space の
// 削除されていない投稿で、parent の root が指定された root と一致すること。
func (s *InstanceService) checkReply(ctx context.Context, spaceID string, reply *ReplyInput) error {
	invalid := httpx.ErrInvalidRequest.With("fields", map[string]string{"reply": "返信先が不正です"})
	if reply.Root == "" || reply.Parent == "" {
		return invalid
	}
	parent, err := s.postRecordForReply(ctx, spaceID, reply.Parent)
	if err != nil {
		return err
	}
	if _, err := s.postRecordForReply(ctx, spaceID, reply.Root); err != nil {
		return err
	}
	parentRoot := parent.URI
	var pv postValue
	if err := json.Unmarshal([]byte(parent.ValueJSON), &pv); err == nil && pv.Reply != nil {
		parentRoot = pv.Reply.Root
	}
	if parentRoot != reply.Root {
		return invalid
	}
	return nil
}

func (s *InstanceService) postRecordForReply(ctx context.Context, spaceID, uri string) (*model.Record, error) {
	invalid := httpx.ErrInvalidRequest.With("fields", map[string]string{"reply": "返信先が不正です"})
	rec, err := model.GetRecord(ctx, s.DB, uri)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, invalid
	}
	if err != nil {
		return nil, err
	}
	if rec.SpaceID != spaceID || rec.Collection != CollectionPost || rec.DeletedAt != nil {
		return nil, invalid
	}
	return rec, nil
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
	if s.Pins != nil {
		if err := s.Pins.DeleteBySubject(ctx, spaceID, uri); err != nil {
			slog.WarnContext(ctx, "pin cleanup failed", "uri", uri, "err", err)
		}
	}
	return nil
}

// ListInstances は GET /instances（13 §2）。
func (s *InstanceService) ListInstances(ctx context.Context, did string) ([]model.SpaceListItem, error) {
	return model.ListUserSpaces(ctx, s.DB, did)
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
