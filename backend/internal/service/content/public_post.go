package content

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"strconv"
	"strings"

	"github.com/applemango/chain/internal/external/atproto"
	"github.com/applemango/chain/internal/httpx"
	"github.com/applemango/chain/internal/model"
	"github.com/applemango/chain/internal/realtime"
	"github.com/applemango/chain/internal/service/core"
	"github.com/applemango/chain/internal/service/moderation"
	"github.com/applemango/chain/internal/utils"
)

// 公開投稿（ユーザーの PDS に書く 17 §2.1・§2.2）。

const (
	// CollectionFeedPost は CHAIN の公開投稿の collection（17 §2.3）。
	CollectionFeedPost = "jp.i32.chain.feed.post"
	// CollectionBskyPost は Bluesky の投稿の collection。alsoBluesky のときだけ書く（17 §2.1）。
	CollectionBskyPost = "app.bsky.feed.post"

	// ContentKindPublicPost は公開投稿の判定種別（11 §5.5）。pin 質問は付けない。
	ContentKindPublicPost = "public_post"

	// MaxFeedPostGraphemes は公開投稿の本文の上限 grapheme（17 §2.1）。
	MaxFeedPostGraphemes = 1000
	// MaxFeedPostLangs は langs の上限（17 §2.1）。
	MaxFeedPostLangs = 3
)

// PublicPostService は公開投稿の作成・削除（17 §2.1・§2.2）。
type PublicPostService struct {
	core.Deps
	Writer    core.RecordWriter
	Moderator moderation.Moderator
	Broker    realtime.Broker
}

// CreatePublicPostInput は公開投稿の作成入力。
type CreatePublicPostInput struct {
	Text        string
	Langs       []string
	AlsoBluesky bool
}

// PublicPostResult は作成した公開投稿。BskyURI は alsoBluesky のときだけ入る。
type PublicPostResult struct {
	URI     string
	CID     string
	BskyURI string
}

// Create は公開投稿を作る。moderation を PDS に書く前に行い（block は 422）、書いた直後に索引に入れる。
func (s *PublicPostService) Create(ctx context.Context, did, sid string, in CreatePublicPostInput) (*PublicPostResult, error) {
	if err := validatePublicPost(in); err != nil {
		return nil, err
	}
	if s.Writer == nil {
		return nil, httpx.ErrUpstream.With("reason", "PDS への書き込みができません")
	}

	label, modState, pending, err := s.judge(ctx, in.Text)
	if err != nil {
		return nil, err
	}

	sess, err := model.GetAppSession(ctx, s.DB, sid)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, httpx.ErrATProtoSession
	}
	if err != nil {
		return nil, err
	}

	now := s.Clock.Now()
	feedRecord := map[string]any{
		"$type":     CollectionFeedPost,
		"text":      in.Text,
		"createdAt": core.FormatDateTime(now),
	}
	if len(in.Langs) > 0 {
		feedRecord["langs"] = in.Langs
	}

	var bskyURI, bskyCID string
	if in.AlsoBluesky {
		bskyRecord := map[string]any{
			"$type":     CollectionBskyPost,
			"text":      in.Text,
			"createdAt": core.FormatDateTime(now),
		}
		if len(in.Langs) > 0 {
			bskyRecord["langs"] = in.Langs
		}
		uri, cid, err := s.Writer.CreateRecord(ctx, did, sess.ATProtoSessionID, CollectionBskyPost, bskyRecord)
		if err != nil {
			return nil, publicPostWriteError(err)
		}
		bskyURI, bskyCID = uri, cid
		feedRecord["bsky"] = map[string]any{"uri": bskyURI, "cid": bskyCID}
	}

	uri, cid, err := s.Writer.CreateRecord(ctx, did, sess.ATProtoSessionID, CollectionFeedPost, feedRecord)
	if err != nil {
		if bskyURI != "" {
			if rerr := s.Writer.DeleteRecord(ctx, did, sess.ATProtoSessionID, CollectionBskyPost, rkeyOf(bskyURI)); rerr != nil && !errors.Is(rerr, atproto.ErrRecordNotFound) {
				slog.WarnContext(ctx, "bluesky rollback failed", "err", rerr)
			}
		}
		return nil, publicPostWriteError(err)
	}

	if _, err := model.UpsertPublicPost(ctx, s.DB, model.PublicPost{
		URI:       uri,
		CID:       cid,
		AuthorDID: did,
		Text:      in.Text,
		Langs:     in.Langs,
		BskyURI:   bskyURI,
		BskyCID:   bskyCID,
		CreatedAt: now,
		IndexedAt: now,
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
	publishHomeFeedUpdated(ctx, s.DB, s.Clock, s.Broker, did)
	return &PublicPostResult{URI: uri, CID: cid, BskyURI: bskyURI}, nil
}

// Delete は自分の公開投稿を消す（17 §2.2）。bsky があれば Bluesky 側も消す。
// 片方が既に PDS に無くても成功にする。索引に無い、または他人の rkey は 404。
func (s *PublicPostService) Delete(ctx context.Context, did, sid, rkey string) error {
	if s.Writer == nil {
		return httpx.ErrUpstream.With("reason", "PDS への書き込みができません")
	}
	uri := feedPostURI(did, rkey)
	pp, err := model.GetPublicPost(ctx, s.DB, uri)
	if errors.Is(err, sql.ErrNoRows) {
		return httpx.ErrNotFound
	}
	if err != nil {
		return err
	}

	sess, err := model.GetAppSession(ctx, s.DB, sid)
	if errors.Is(err, sql.ErrNoRows) {
		return httpx.ErrATProtoSession
	}
	if err != nil {
		return err
	}

	if err := s.Writer.DeleteRecord(ctx, did, sess.ATProtoSessionID, CollectionFeedPost, rkey); err != nil && !errors.Is(err, atproto.ErrRecordNotFound) {
		return publicPostWriteError(err)
	}
	if pp.BskyURI != "" {
		if err := s.Writer.DeleteRecord(ctx, did, sess.ATProtoSessionID, CollectionBskyPost, rkeyOf(pp.BskyURI)); err != nil && !errors.Is(err, atproto.ErrRecordNotFound) {
			return publicPostWriteError(err)
		}
	}
	return model.MarkPublicPostDeleted(ctx, s.DB, uri, s.Clock.Now())
}

// judge は moderation を同期で行い、索引に入れる mod_label / mod_state を返す（11 §3）。
// block は 422 のエラーを返す。エラー / タイムアウトは ok / pending として進める。
func (s *PublicPostService) judge(ctx context.Context, text string) (label, state string, pending bool, err error) {
	if s.Moderator == nil {
		return "ok", "done", false, nil
	}
	judgeCtx, cancel := context.WithTimeout(ctx, moderation.ModerationSyncTimeout)
	defer cancel()
	v, jerr := s.Moderator.Judge(judgeCtx, moderation.Content{Kind: ContentKindPublicPost, Text: text})
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

func validatePublicPost(in CreatePublicPostInput) error {
	fields := map[string]string{}
	if strings.TrimSpace(in.Text) == "" {
		fields["text"] = "本文が必要です"
	} else if utils.GraphemeCount(in.Text) > MaxFeedPostGraphemes {
		fields["text"] = "1000 文字までです"
	}
	if len(in.Langs) > MaxFeedPostLangs {
		fields["langs"] = "3 件までです"
	}
	for i, l := range in.Langs {
		if !utils.ValidLanguageTag(l) {
			fields["langs."+strconv.Itoa(i)] = "言語タグが不正です"
		}
	}
	if len(fields) > 0 {
		return httpx.ErrInvalidRequest.With("fields", fields)
	}
	return nil
}

// publicPostWriteError は Writer のエラーを API のエラーに写す（03 §2.2、17 §2.4）。
func publicPostWriteError(err error) error {
	switch {
	case errors.Is(err, atproto.ErrSessionExpired):
		return httpx.ErrATProtoSession
	case errors.Is(err, atproto.ErrReauthRequired):
		return httpx.ErrReauthRequired
	default:
		return httpx.ErrUpstream.With("reason", "PDS への書き込みに失敗しました")
	}
}

// HttpErrorCode は service のエラーから API の code を取り出す。公開投稿を instance 投稿と
// 同時に行うとき、失敗の code だけを応答に載せるために使う（10 §4.1）。
func HttpErrorCode(err error) string {
	var e *httpx.Error
	if errors.As(err, &e) {
		return e.Code
	}
	return "internal"
}

func feedPostURI(did, rkey string) string {
	return "at://" + did + "/" + CollectionFeedPost + "/" + rkey
}

func rkeyOf(uri string) string {
	if i := strings.LastIndex(uri, "/"); i >= 0 {
		return uri[i+1:]
	}
	return uri
}
