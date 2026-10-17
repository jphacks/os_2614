package core

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/applemango/chain/internal/db"
	"github.com/applemango/chain/internal/external/atproto"
	"github.com/applemango/chain/internal/model"
)

// 複数の機能から使う共通の名前（第 2 段階）。
// ルートの service パッケージはこれらを別名で再公開し、既存の呼び出しをそのまま使えるようにする。

// Space の種類（10 §5）。
const (
	SpaceTypeGroup = "jp.i32.chain.space.group"
	SpaceTypeParty = "jp.i32.chain.space.party"
)

// record の collection 名。instance（10 §4）と profile（03 §6、13 §1.3）。
const (
	CollectionPost         = "jp.i32.chain.instance.post"
	CollectionMedia        = "jp.i32.chain.instance.media"
	CollectionPin          = "jp.i32.chain.instance.pin"
	CollectionActorProfile = "jp.i32.chain.actor.profile"
	CollectionBskyProfile  = "app.bsky.actor.profile"
)

// 一覧のページ上限の既定（10 §7.3、12 §1）。
const (
	DefaultPageLimit = 30
	MaxPageLimit     = 100
)

// ContentScanConsentVersion は現在の投稿内容の自動判定への同意の版（02 §8）。
const ContentScanConsentVersion = 1

// AuthorSummary は instance の投稿などに載せる作成者の要約（10 §4.1）。
type AuthorSummary struct {
	DID         string `json:"did"`
	Handle      string `json:"handle"`
	DisplayName string `json:"displayName,omitempty"`
	AvatarURL   string `json:"avatarUrl,omitempty"`
}

// AuthorSummaryFor は users と profiles から作成者の要約を作る。
func AuthorSummaryFor(ctx context.Context, q *sql.DB, did string) (*AuthorSummary, error) {
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

// UserSummary はイベントや通知に載せる相手の要約（10 §7.3）。
type UserSummary struct {
	DID         string `json:"did"`
	Handle      string `json:"handle"`
	DisplayName string `json:"displayName,omitempty"`
	AvatarURL   string `json:"avatarUrl,omitempty"`
}

// FetchUserSummary は users と profiles から、イベント・通知に載せる要約を作る。
func FetchUserSummary(ctx context.Context, q db.Querier, did string) (*UserSummary, error) {
	u, err := model.GetUser(ctx, q, did)
	if err != nil {
		return nil, err
	}
	s := &UserSummary{DID: u.DID, Handle: u.Handle}
	p, err := model.GetProfile(ctx, q, did)
	switch {
	case err == nil:
		s.DisplayName = p.DisplayName
		s.AvatarURL = atproto.AvatarURL(did, p.AvatarCID)
	case !errors.Is(err, sql.ErrNoRows):
		return nil, err
	}
	return s, nil
}

// FormatDateTime は 03 §3 の日時表記（ミリ秒まで）にする。
func FormatDateTime(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000Z") }

// AuthorityFor は did:web:{host}（03 §7、seed と同じ形）を作る。
func AuthorityFor(publicURL string) string {
	if u, err := url.Parse(publicURL); err == nil && u.Host != "" {
		return "did:web:" + u.Host
	}
	return "did:web:" + strings.TrimPrefix(strings.TrimPrefix(publicURL, "https://"), "http://")
}

// JstDateString は t の Asia/Tokyo の暦日を YYYY-MM-DD で返す。
func JstDateString(t time.Time) string { return t.In(Jst).Format("2006-01-02") }

// RecordWriter はユーザーの PDS に record を読み書きする（03 §4）。実装は atproto.Writer、テストでは fake。
type RecordWriter interface {
	PutRecord(ctx context.Context, did, atprotoSessionID, collection, rkey string, record any, swapRecord string) (uri, cid string, err error)
	GetRecord(ctx context.Context, did, atprotoSessionID, collection, rkey string) (value map[string]any, cid string, found bool, err error)
	UploadBlob(ctx context.Context, did, atprotoSessionID, mime string, data []byte) (*atproto.Blob, error)
	CreateRecord(ctx context.Context, did, atprotoSessionID, collection string, record any) (uri, cid string, err error)
	DeleteRecord(ctx context.Context, did, atprotoSessionID, collection, rkey string) error
}
