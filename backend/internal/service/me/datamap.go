package me

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/url"
	"strings"

	"github.com/bluesky-social/indigo/atproto/auth/oauth"

	"github.com/applemango/chain/internal/auth"
	"github.com/applemango/chain/internal/httpx"
	"github.com/applemango/chain/internal/model"
	friendmodel "github.com/applemango/chain/internal/model/friend"
)

// データの置き場所の保存場所（DataMapRow.Storage）。
const (
	StoragePDS      = "pds"
	StorageChain    = "chain"
	StorageExternal = "external"
)

// DataMapRow は 19 §7.1 の表の 1 行。定義は固定で、文言は 03 §7.3・02 §8 とずらさない。
// Place の {pds_url} は PDS の URL に置き換えて返す。CountKeys は DataMap.Counts のキー。
type DataMapRow struct {
	ID        string
	Data      string
	Storage   string
	Place     string
	VisibleTo string
	Retention string
	Export    string
	CountKeys []string
}

var dataMapRows = []DataMapRow{
	{ID: "public_posts", Data: "公開投稿・プロフィール", Storage: StoragePDS, Place: "あなたの PDS（{pds_url}）", VisibleTo: "誰でも", Retention: "消すまで", Export: "PDS のエクスポート", CountKeys: []string{"publicPosts"}},
	{ID: "space_posts", Data: "グループ / Party の投稿・写真", Storage: StorageChain, Place: "CHAIN のサーバー（Spaces 統合後は、対応する PDS ならあなたの PDS）", VisibleTo: "その Space のメンバーと、CHAIN の運営者", Retention: "archive 後も残る", Export: "エクスポート機能（準備中）", CountKeys: []string{"spacePosts", "photos"}},
	{ID: "friends_follow", Data: "フレンド（フォロー）", Storage: StoragePDS, Place: "あなたの PDS（app.bsky.graph.follow）", VisibleTo: "誰でも", Retention: "消すまで", Export: "PDS のエクスポート", CountKeys: []string{"friends"}},
	{ID: "encounters", Data: "すれ違い・会った裏付け", Storage: StorageChain, Place: "CHAIN のサーバー", VisibleTo: "本人と相手（公開設定による）、運営者", Retention: "13 §5 の保存期間", Export: "エクスポート機能（準備中）", CountKeys: []string{"encounters"}},
	{ID: "location", Data: "位置", Storage: StorageChain, Place: "CHAIN のサーバー（一時的なもの。Redis）", VisibleTo: "位置共有の相手、運営者", Retention: "数分〜数時間で自動で消える", Export: "持ち出せない（保存していない）"},
	{ID: "moderation_text", Data: "投稿の本文（判定のため）", Storage: StorageExternal, Place: "外部（NanoGPT）", VisibleTo: "—", Retention: "外部の保存の扱いによる", Export: "—"},
	{ID: "schedule_titles", Data: "予定のタイトル", Storage: StorageChain, Place: "CHAIN のサーバー", VisibleTo: "運営者", Retention: "今日と明日の分だけ", Export: "エクスポート機能（準備中）"},
}

// DataMap は GET /me/data-map の中身。Scopes が nil なら OAuth セッションが失効している（または読めない）。
type DataMap struct {
	PDSURL  string
	PDSHost string
	Scopes  []string
	Counts  map[string]int
	Rows    []DataMapRow
}

// PDSHost は pds_url のホスト名だけを返す。解釈できなければ空文字。
func PDSHost(pdsURL string) string {
	u, err := url.Parse(pdsURL)
	if err != nil || u.Hostname() == "" {
		return ""
	}
	return u.Hostname()
}

func (s *MeService) DataMap(ctx context.Context, did, sid string) (*DataMap, error) {
	u, err := model.GetUser(ctx, s.DB, did)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, httpx.ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	c, err := model.CountDataMap(ctx, s.DB, did)
	if err != nil {
		return nil, err
	}
	friends, err := friendmodel.CountFriends(ctx, s.DB, did)
	if err != nil {
		return nil, err
	}

	rows := make([]DataMapRow, len(dataMapRows))
	for i, r := range dataMapRows {
		r.Place = strings.ReplaceAll(r.Place, "{pds_url}", u.PDSURL)
		rows[i] = r
	}
	return &DataMap{
		PDSURL:  u.PDSURL,
		PDSHost: PDSHost(u.PDSURL),
		Scopes:  s.sessionScopes(ctx, did, sid),
		Counts: map[string]int{
			"publicPosts": c.PublicPosts,
			"spacePosts":  c.SpacePosts,
			"photos":      c.Photos,
			"encounters":  c.Encounters,
			"friends":     friends,
		},
		Rows: rows,
	}, nil
}

// sessionScopes はこのログインの OAuth セッションに保存された scope を返す。
// 失効・読めない場合は nil（画面では「不明」と出す）。
func (s *MeService) sessionScopes(ctx context.Context, did, sid string) []string {
	app, err := model.GetAppSession(ctx, s.DB, sid)
	if err != nil {
		return nil
	}
	row, err := model.GetATProtoSession(ctx, s.DB, app.ATProtoSessionID)
	if err != nil || row.DID != did || row.RevokedAt != nil || len(row.DataEnc) == 0 {
		return nil
	}
	plain, err := auth.Decrypt(s.Config.SecretKey, row.DataEnc)
	if err != nil {
		slog.WarnContext(ctx, "decrypt oauth session for data-map", "err", err)
		return nil
	}
	var data oauth.ClientSessionData
	if err := json.Unmarshal(plain, &data); err != nil {
		slog.WarnContext(ctx, "decode oauth session for data-map", "err", err)
		return nil
	}
	if data.Scopes == nil {
		return []string{}
	}
	return data.Scopes
}
