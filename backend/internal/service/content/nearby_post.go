package content

import (
	"context"
	"crypto/sha256"
	"encoding/base32"
	"log/slog"
	"strings"

	"github.com/applemango/chain/internal/httpx"
	"github.com/applemango/chain/internal/idgen"
	"github.com/applemango/chain/internal/model"
	"github.com/applemango/chain/internal/realtime"
	"github.com/applemango/chain/internal/service/core"
	"github.com/applemango/chain/internal/service/moderation"
	"github.com/applemango/chain/internal/service/proximity"
	"github.com/applemango/chain/internal/utils"
)

// nearby 投稿（17 §2）。今近くにいる人だけに届ける限定投稿。
// PDS には書かず、public_posts の索引にだけ入れる（限定データは SQLite のみ）。

const (
	// CollectionNearbyPost は nearby 投稿の合成 URI に使う collection（17 §2）。PDS には書かない。
	CollectionNearbyPost = "jp.i32.chain.feed.nearby"

	// ContentKindNearbyPost は nearby 投稿の判定種別（11 §5.5）。
	ContentKindNearbyPost = "nearby_post"

	// nearbyAudience は public_posts.audience の nearby 値（17 §2）。
	nearbyAudience = "nearby"
)

// NearbyPostService は nearby 投稿の作成（17 §2）。PDS に record を作らないので Writer を持たない。
type NearbyPostService struct {
	core.Deps
	Moderator moderation.Moderator
	Broker    realtime.Broker
	Proximity proximity.PeerProximity
	TID       idgen.TIDGenerator
}

// NearbyPostResult は作成した nearby 投稿。URI は合成の at-uri。
type NearbyPostResult struct {
	URI string
}

// Create は nearby 投稿を索引に入れる（17 §2）。同意 → moderation（block は 422）→
// PDS に書かず public_posts に audience='nearby' / expires_at 付きで保存 →
// feed.updated{source:"nearby"} を投稿者と今近くにいる相手へ publish する。
func (s *NearbyPostService) Create(ctx context.Context, did, text string) (*NearbyPostResult, error) {
	if err := validateNearbyPost(text); err != nil {
		return nil, err
	}

	settings, err := model.GetSettings(ctx, s.DB, did)
	if err != nil {
		return nil, err
	}
	if settings.ContentScanConsentVersion == nil || *settings.ContentScanConsentVersion != core.ContentScanConsentVersion {
		return nil, httpx.ErrConsentRequired
	}

	label, modState, pending, err := s.judge(ctx, text)
	if err != nil {
		return nil, err
	}

	// 固定の時計でも rkey が単調増加になるように、生成器は呼び出しをまたいで持ち回る。
	if s.TID == nil {
		s.TID = idgen.NewTIDGenerator(s.Clock, 0)
	}
	rkey := s.TID.Next().String()
	uri := "at://" + did + "/" + CollectionNearbyPost + "/" + rkey

	now := s.Clock.Now()
	expiry := now.Add(core.NearbyPostTTL)
	if _, err := model.UpsertPublicPost(ctx, s.DB, model.PublicPost{
		URI:       uri,
		CID:       syntheticCID(did, rkey, text),
		AuthorDID: did,
		Text:      text,
		CreatedAt: now,
		IndexedAt: now,
		Audience:  nearbyAudience,
		ExpiresAt: &expiry,
		ModLabel:  label,
		ModState:  modState,
	}); err != nil {
		return nil, err
	}
	if pending {
		if err := model.EnqueueModerationJob(ctx, s.Redis, uri); err != nil {
			slog.WarnContext(ctx, "modq push failed", "err", err)
		}
	}
	s.publishNearbyFeedUpdated(ctx, did)
	return &NearbyPostResult{URI: uri}, nil
}

// judge は moderation を同期で行い、索引に入れる mod_label / mod_state を返す（11 §3）。
// block は 422 のエラーを返す。エラー / タイムアウトは ok / pending として進める。
func (s *NearbyPostService) judge(ctx context.Context, text string) (label, state string, pending bool, err error) {
	if s.Moderator == nil {
		return "ok", "done", false, nil
	}
	judgeCtx, cancel := context.WithTimeout(ctx, moderation.ModerationSyncTimeout)
	defer cancel()
	v, jerr := s.Moderator.Judge(judgeCtx, moderation.Content{Kind: ContentKindNearbyPost, Text: text})
	if jerr != nil {
		moderation.LogModerationError(ctx, jerr)
		return "ok", "pending", true, nil
	}
	switch v.Label {
	case moderation.LabelBlock:
		return "", "", false, httpx.ErrModerationBlocked.With("categories", v.Categories)
	case moderation.LabelWarn:
		return "warn", "done", false, nil
	default:
		return "ok", "done", false, nil
	}
}

// publishNearbyFeedUpdated は nearby 投稿が索引に入ったことを、投稿者と今近くにいる相手へ知らせる（17 §3.4）。
// 中身は載せず、受けた側が GET /feed?source=nearby で取り直す。失敗は投稿を妨げない。
// 近接が使えないときは投稿者だけに届ける。
func (s *NearbyPostService) publishNearbyFeedUpdated(ctx context.Context, authorDID string) {
	if s.Broker == nil {
		return
	}
	if s.Proximity == nil {
		slog.WarnContext(ctx, "feed updated (nearby): proximity unavailable; notifying author only", "did", authorDID)
		s.Broker.Publish(ctx, realtime.Event{Name: "feed.updated", User: authorDID, Data: map[string]string{"source": "nearby"}})
		return
	}
	recipients, err := nearbyAuthors(ctx, s.Proximity, authorDID)
	if err != nil {
		slog.WarnContext(ctx, "feed updated (nearby): neighbors failed", "err", err)
		return
	}
	for _, did := range recipients {
		s.Broker.Publish(ctx, realtime.Event{Name: "feed.updated", User: did, Data: map[string]string{"source": "nearby"}})
	}
}

// validateNearbyPost は nearby 投稿の本文を検証する（17 §2）。公開投稿と同じ上限を使う。
func validateNearbyPost(text string) error {
	fields := map[string]string{}
	if strings.TrimSpace(text) == "" {
		fields["text"] = "本文が必要です"
	} else if utils.GraphemeCount(text) > MaxFeedPostGraphemes {
		fields["text"] = "1000 文字までです"
	}
	if len(fields) > 0 {
		return httpx.ErrInvalidRequest.With("fields", fields)
	}
	return nil
}

// syntheticCID は PDS に record が無い nearby 投稿の cid を作る。cid は NOT NULL なので、
// uri と本文から決まる安定した値を入れる。PDS の CID ではないので中身の検証には使えない。
func syntheticCID(did, rkey, text string) string {
	sum := sha256.Sum256([]byte(did + "|" + rkey + "|" + text))
	return "b" + strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(sum[:]))
}
