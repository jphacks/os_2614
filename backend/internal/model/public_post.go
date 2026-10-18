package model

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	"github.com/applemango/chain/internal/clock"
	"github.com/applemango/chain/internal/db"
)

// PublicPost は public_posts の行（17 §2）。ユーザーの PDS にある jp.i32.chain.feed.post の索引。
// 公開投稿の作成（read-after-write）、Jetstream の取り込み、フィードの 3 か所から使う。
type PublicPost struct {
	URI        string
	CID        string
	AuthorDID  string
	Text       string
	Langs      []string
	SelfLabels []string // labels（selfLabels）の val（19 §2.15.3）
	BskyURI    string   // Bluesky にも投稿したときの app.bsky.feed.post（17 §2.3）
	BskyCID    string
	CreatedAt  time.Time
	IndexedAt  time.Time
	DeletedAt  *time.Time
	ModLabel   string     // ok / warn / block
	ModState   string     // pending / done
	Audience   string     // public / nearby（17 §2）。nearby は今近くにいる人だけに配信する
	ExpiresAt  *time.Time // nearby の失効時刻（17 §2）。nil なら失効しない
}

const publicPostColumns = `uri, cid, author_did, text, langs, bsky_uri, bsky_cid,
	created_at, indexed_at, deleted_at, mod_label, mod_state, audience, expires_at, self_labels_json`

func scanPublicPost(scan db.Scan, p *PublicPost) error {
	var langs, bskyURI, bskyCID sql.NullString
	var selfLabels string
	var created, indexed int64
	var deleted, expires sql.NullInt64
	if err := scan(&p.URI, &p.CID, &p.AuthorDID, &p.Text, &langs, &bskyURI, &bskyCID,
		&created, &indexed, &deleted, &p.ModLabel, &p.ModState, &p.Audience, &expires, &selfLabels); err != nil {
		return err
	}
	if selfLabels != "" && selfLabels != "[]" {
		if err := json.Unmarshal([]byte(selfLabels), &p.SelfLabels); err != nil {
			return err
		}
	}
	if langs.Valid && langs.String != "" {
		if err := json.Unmarshal([]byte(langs.String), &p.Langs); err != nil {
			return err
		}
	}
	p.BskyURI, p.BskyCID = bskyURI.String, bskyCID.String
	p.CreatedAt = clock.FromUnixMilli(created)
	p.IndexedAt = clock.FromUnixMilli(indexed)
	if deleted.Valid {
		t := clock.FromUnixMilli(deleted.Int64)
		p.DeletedAt = &t
	}
	if expires.Valid {
		t := clock.FromUnixMilli(expires.Int64)
		p.ExpiresAt = &t
	}
	return nil
}

func GetPublicPost(ctx context.Context, q db.Querier, uri string) (*PublicPost, error) {
	return db.GetItem(ctx, q, "SELECT "+publicPostColumns+" FROM public_posts WHERE uri = ?", []any{uri}, scanPublicPost)
}

// UpsertPublicPost は uri をキーに作るか更新する。同じ uri・cid の 2 回目（read-after-write の後に
// Jetstream から同じ record が届いた場合）は何も変えない。cid が変われば本文を更新し、
// moderation をやり直すために mod_label を ok、mod_state を p.ModState にする。
// 戻り値は、行を作ったか本文を更新したか（moderation に回す必要があるか）。
func UpsertPublicPost(ctx context.Context, q db.Querier, p PublicPost) (bool, error) {
	var langs any
	if len(p.Langs) > 0 {
		b, err := json.Marshal(p.Langs)
		if err != nil {
			return false, err
		}
		langs = string(b)
	}
	selfLabels := "[]"
	if len(p.SelfLabels) > 0 {
		b, err := json.Marshal(p.SelfLabels)
		if err != nil {
			return false, err
		}
		selfLabels = string(b)
	}
	if p.ModLabel == "" {
		p.ModLabel = "ok"
	}
	if p.ModState == "" {
		p.ModState = "pending"
	}
	if p.Audience == "" {
		p.Audience = "public"
	}
	res, err := db.Execute(ctx, q, `
		INSERT INTO public_posts (uri, cid, author_did, text, langs, bsky_uri, bsky_cid,
		                          created_at, indexed_at, mod_label, mod_state, audience, expires_at, self_labels_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (uri) DO UPDATE SET
		  cid = excluded.cid, text = excluded.text, langs = excluded.langs,
		  bsky_uri = excluded.bsky_uri, bsky_cid = excluded.bsky_cid,
		  indexed_at = excluded.indexed_at, deleted_at = NULL,
		  mod_label = excluded.mod_label, mod_state = excluded.mod_state,
		  audience = excluded.audience, expires_at = excluded.expires_at,
		  self_labels_json = excluded.self_labels_json
		WHERE public_posts.cid != excluded.cid OR public_posts.deleted_at IS NOT NULL`,
		p.URI, p.CID, p.AuthorDID, p.Text, langs, Nullable(p.BskyURI), Nullable(p.BskyCID),
		clock.UnixMilli(p.CreatedAt), clock.UnixMilli(p.IndexedAt), p.ModLabel, p.ModState,
		p.Audience, NullableTime(p.ExpiresAt), selfLabels)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// MarkPublicPostDeleted は論理削除する（04 §4 で 30 日後に本文を消す）。
func MarkPublicPostDeleted(ctx context.Context, q db.Querier, uri string, now time.Time) error {
	_, err := db.Execute(ctx, q,
		"UPDATE public_posts SET deleted_at = ? WHERE uri = ? AND deleted_at IS NULL", clock.UnixMilli(now), uri)
	return err
}

// SetPublicPostModeration は moderation の結果を書く（11 §3）。
func SetPublicPostModeration(ctx context.Context, q db.Querier, uri, label, state string) error {
	_, err := db.Execute(ctx, q,
		"UPDATE public_posts SET mod_label = ?, mod_state = ? WHERE uri = ?", label, state, uri)
	return err
}

// HidePublicPostsByAuthor は Jetstream の account（active=false）で、その人の投稿を見せなくする（03 §5）。
func HidePublicPostsByAuthor(ctx context.Context, q db.Querier, did string, now time.Time) error {
	_, err := db.Execute(ctx, q,
		"UPDATE public_posts SET deleted_at = ? WHERE author_did = ? AND deleted_at IS NULL", clock.UnixMilli(now), did)
	return err
}

// ListPublicPostsByAuthors は authors の公開投稿を新しい順に返す（フィードの home / user source 用、17 §3）。
// before の (createdAt, uri) より古いものだけ。削除済みは除く。
// audience='public' に限定する: nearby 投稿は home / user フィードに混ざらない（17 §3）。
// block は除かない: 投稿者本人には hidden として返すため、表示の判定は service（FeedService）が行う（10 §4.2）。
func ListPublicPostsByAuthors(ctx context.Context, q db.Querier, authors []string, beforeAt int64, beforeURI string, limit int) ([]PublicPost, error) {
	if len(authors) == 0 {
		return []PublicPost{}, nil
	}
	args := make([]any, 0, len(authors)+4)
	for _, a := range authors {
		args = append(args, a)
	}
	where := "author_did IN (" + strings.TrimSuffix(strings.Repeat("?,", len(authors)), ",") + ")" +
		" AND audience = 'public' AND deleted_at IS NULL"
	if beforeAt > 0 {
		where += " AND (created_at < ? OR (created_at = ? AND uri < ?))"
		args = append(args, beforeAt, beforeAt, beforeURI)
	}
	args = append(args, limit)
	return db.GetItems(ctx, q,
		"SELECT "+publicPostColumns+" FROM public_posts WHERE "+where+" ORDER BY created_at DESC, uri DESC LIMIT ?",
		args, scanPublicPost)
}

// ListNearbyPublicPosts は authors（自分と今近くにいる人）の nearby 投稿を新しい順に返す（17 §3 の source=nearby）。
// audience='nearby' だけを対象にし、失効済み（expires_at <= now）は除く。paging は ListPublicPostsByAuthors と同じ。
func ListNearbyPublicPosts(ctx context.Context, q db.Querier, authors []string, now time.Time, beforeAt int64, beforeURI string, limit int) ([]PublicPost, error) {
	if len(authors) == 0 {
		return []PublicPost{}, nil
	}
	args := make([]any, 0, len(authors)+5)
	for _, a := range authors {
		args = append(args, a)
	}
	where := "author_did IN (" + strings.TrimSuffix(strings.Repeat("?,", len(authors)), ",") + ")" +
		" AND audience = 'nearby' AND deleted_at IS NULL" +
		" AND (expires_at IS NULL OR expires_at > ?)"
	args = append(args, clock.UnixMilli(now))
	if beforeAt > 0 {
		where += " AND (created_at < ? OR (created_at = ? AND uri < ?))"
		args = append(args, beforeAt, beforeAt, beforeURI)
	}
	args = append(args, limit)
	return db.GetItems(ctx, q,
		"SELECT "+publicPostColumns+" FROM public_posts WHERE "+where+" ORDER BY created_at DESC, uri DESC LIMIT ?",
		args, scanPublicPost)
}
