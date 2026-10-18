package content

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/applemango/chain/internal/external/atproto"
	"github.com/applemango/chain/internal/model"
	"github.com/applemango/chain/internal/realtime"
	"github.com/applemango/chain/internal/service/core"
)

// 公開投稿の取り込み（specification/backend/03-auth-atproto.md §5、17-feed.md §2.5）。
// Jetstream のイベントと初回の読み込みを、public_posts / profiles / users に反映する。

const (
	// firstLoadLimit は初回の読み込みで読む件数（17 §2.5）。
	firstLoadLimit = 100
	// FirstLoadTimeout は初回の読み込みのタイムアウト（17 §2.5）。ログインを遅らせない。
	FirstLoadTimeout = 30 * time.Second
	// JetstreamCursorInterval はカーソルを保存する間隔（03 §5）。
	JetstreamCursorInterval = 5 * time.Second
)

// PublicPostLister は初回の読み込みで listRecords を呼ぶ（17 §2.5）。テストでは fake に差し替える。
type PublicPostLister interface {
	ListFeedPosts(ctx context.Context, pdsURL, did string, limit int) ([]atproto.FeedPost, error)
}

// PublicPostBackfiller は初回ログイン時に公開投稿を取り込む。*JetstreamService が実装する（17 §2.5）。
type PublicPostBackfiller interface {
	BackfillPublicPosts(ctx context.Context, did, pdsURL string) error
}

// JetstreamService は Jetstream のイベントを DB に反映し、カーソルを保存する（03 §5）。
type JetstreamService struct {
	core.Deps
	Posts  PublicPostLister
	Broker realtime.Broker
}

var _ PublicPostBackfiller = (*JetstreamService)(nil)

// Handle は 1 イベントを処理する（03 §5 の表）。users に無い DID・削除済みの DID は捨てる。
func (s *JetstreamService) Handle(ctx context.Context, ev atproto.JetstreamEvent) error {
	if ev.DID == "" {
		return nil
	}
	u, err := model.GetUser(ctx, s.DB, ev.DID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if u.DeletedAt != nil {
		return nil
	}

	switch ev.Kind {
	case atproto.JetstreamKindIdentity:
		if ev.Identity == nil || ev.Identity.Handle == "" {
			return nil
		}
		return model.SetUserHandle(ctx, s.DB, ev.DID, ev.Identity.Handle)
	case atproto.JetstreamKindAccount:
		if ev.Account != nil && !ev.Account.Active {
			return model.HidePublicPostsByAuthor(ctx, s.DB, ev.DID, s.Clock.Now())
		}
		return nil
	case atproto.JetstreamKindCommit:
		return s.handleCommit(ctx, ev.DID, ev.Commit)
	default:
		return nil
	}
}

func (s *JetstreamService) handleCommit(ctx context.Context, did string, c *atproto.JetstreamCommit) error {
	if c == nil {
		return nil
	}
	switch c.Collection {
	case CollectionFeedPost:
		switch c.Operation {
		case "create", "update":
			changed, err := s.ingestFeedPost(ctx, did, c.RKey, c.CID, c.Record)
			if changed {
				publishHomeFeedUpdated(ctx, s.DB, s.Clock, s.Broker, did)
			}
			return err
		case "delete":
			return model.MarkPublicPostDeleted(ctx, s.DB, atproto.FeedPostURI(did, c.RKey), s.Clock.Now())
		}
	case core.CollectionBskyProfile:
		return s.updateBskyProfile(ctx, did, c.Record)
	case core.CollectionActorProfile:
		return s.updateChainProfile(ctx, did, c.Record)
	}
	return nil
}

// ingestFeedPost は feed.post の record を検証して public_posts に入れ、変わったら moderation に積む。
// 変わったかどうかを返す。feed.updated の配信は呼び出し側が行う（初回の読み込みでは 1 回にまとめるため）。
func (s *JetstreamService) ingestFeedPost(ctx context.Context, did, rkey, cid string, raw json.RawMessage) (bool, error) {
	if rkey == "" {
		return false, nil
	}
	rec, err := atproto.ParseFeedPost(raw)
	if err != nil {
		slog.WarnContext(ctx, "jetstream: dropping invalid feed.post record", "did", did, "err", err)
		return false, nil
	}
	p := model.PublicPost{
		URI: atproto.FeedPostURI(did, rkey), CID: cid, AuthorDID: did,
		Text: rec.Text, Langs: rec.Langs, CreatedAt: rec.CreatedAt, IndexedAt: s.Clock.Now(),
	}
	if rec.Bsky != nil {
		p.BskyURI, p.BskyCID = rec.Bsky.URI, rec.Bsky.CID
	}
	changed, err := model.UpsertPublicPost(ctx, s.DB, p)
	if err != nil {
		return false, err
	}
	if changed {
		return true, model.EnqueueModerationJob(ctx, s.Redis, p.URI)
	}
	return false, nil
}

// updateBskyProfile は app.bsky.actor.profile を profiles に反映する（13 §1.1）。status は消さない。
func (s *JetstreamService) updateBskyProfile(ctx context.Context, did string, raw json.RawMessage) error {
	rec, err := atproto.ParseBskyProfile(raw)
	if err != nil {
		slog.WarnContext(ctx, "jetstream: dropping invalid bsky profile record", "did", did, "err", err)
		return nil
	}
	p, err := s.currentProfile(ctx, did)
	if err != nil {
		return err
	}
	p.DisplayName, p.Description = rec.DisplayName, rec.Description
	p.AvatarCID, p.BannerCID = rec.AvatarCID, rec.BannerCID
	p.UpdatedAt = s.Clock.Now()
	return model.UpsertProfile(ctx, s.DB, *p)
}

// updateChainProfile は jp.i32.chain.actor.profile の status を profiles に反映する（03 §6、13 §1.1）。
func (s *JetstreamService) updateChainProfile(ctx context.Context, did string, raw json.RawMessage) error {
	status, err := atproto.ParseChainProfile(raw)
	if err != nil {
		slog.WarnContext(ctx, "jetstream: dropping invalid chain profile record", "did", did, "err", err)
		return nil
	}
	p, err := s.currentProfile(ctx, did)
	if err != nil {
		return err
	}
	p.Status = status
	p.UpdatedAt = s.Clock.Now()
	return model.UpsertProfile(ctx, s.DB, *p)
}

// currentProfile は既存の profiles を読む。無ければ空の行を返し、他の項目を消さないようにする。
func (s *JetstreamService) currentProfile(ctx context.Context, did string) (*model.Profile, error) {
	p, err := model.GetProfile(ctx, s.DB, did)
	if errors.Is(err, sql.ErrNoRows) {
		return &model.Profile{DID: did}, nil
	}
	if err != nil {
		return nil, err
	}
	return p, nil
}

// Cursor は保存済みの cursor_us を返す（03 §5）。
func (s *JetstreamService) Cursor(ctx context.Context) (int64, error) {
	return model.GetJetstreamCursor(ctx, s.DB)
}

// SaveCursor は cursor_us を保存する（03 §5）。
func (s *JetstreamService) SaveCursor(ctx context.Context, cursorUS int64) error {
	return model.SetJetstreamCursor(ctx, s.DB, cursorUS)
}

// BackfillPublicPosts は初回ログイン時に PDS の公開投稿を新しい順に読み、取り込む（17 §2.5）。
func (s *JetstreamService) BackfillPublicPosts(ctx context.Context, did, pdsURL string) error {
	if s.Posts == nil {
		return nil
	}
	posts, err := s.Posts.ListFeedPosts(ctx, pdsURL, did, firstLoadLimit)
	if err != nil {
		return err
	}
	anyChanged := false
	for _, p := range posts {
		rkey := atproto.RKeyFromURI(p.URI)
		changed, err := s.ingestFeedPost(ctx, did, rkey, p.CID, p.Value)
		anyChanged = anyChanged || changed
		if err != nil {
			return err
		}
	}
	if anyChanged {
		publishHomeFeedUpdated(ctx, s.DB, s.Clock, s.Broker, did)
	}
	return nil
}
