package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/applemango/chain/internal/clock"
	"github.com/applemango/chain/internal/httpx"
	"github.com/applemango/chain/internal/model"
	"github.com/applemango/chain/internal/storage"
	"github.com/applemango/chain/internal/utils"
)

// 共通のフィード（17 §3）。source の名前から取得元を組み立て、複数をマージし、
// ブロック・moderation（11 §4）・メディアの署名付き URL を一律にかける。

const (
	feedHome        = "home"
	feedGroups      = "groups"
	feedSpacePrefix = "space:"
	feedUserPrefix  = "user:"

	// encounterFeedWindow は home がすれ違いの相手を含める期間（17 §3.1 の「直近 30 日」）。
	encounterFeedWindow = 30 * 24 * time.Hour
)

// FeedCursor は (createdAt, uri)。すべての source が同じ形を使う（17 §3.3）。
// ゼロ値は先頭（before なし）を意味する。
type FeedCursor struct {
	At  time.Time
	URI string
}

// FeedSource は 1 種類の取得元。before より古いものを、新しい順に最大 limit 件返す（17 §3.3）。
type FeedSource interface {
	Fetch(ctx context.Context, viewer string, before FeedCursor, limit int) ([]FeedItem, error)
}

// FeedSpace は instance の要素に付く Space の情報（17 §3.2）。
type FeedSpace struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	Name string `json:"name"`
}

// BskyRef は公開投稿を Bluesky にも書いたときの対応（17 §2.3）。
type BskyRef struct {
	URI string `json:"uri"`
}

// FeedItem は 17 §3.2 の 1 要素。public_post / instance_post / instance_media を同じ形で表す。
type FeedItem struct {
	Kind       string          `json:"kind"`
	URI        string          `json:"uri"`
	Space      *FeedSpace      `json:"space"`
	Author     *AuthorSummary  `json:"author"`
	Text       string          `json:"text,omitempty"`
	Format     string          `json:"format,omitempty"`
	Reply      *ReplyRef       `json:"reply,omitempty"`
	System     bool            `json:"system,omitempty"`
	Media      []MediaItem     `json:"media,omitempty"`
	Bsky       *BskyRef        `json:"bsky,omitempty"`
	Moderation *ModerationInfo `json:"moderation,omitempty"`
	Hidden     string          `json:"hidden,omitempty"`
	TakenAt    *time.Time      `json:"takenAt,omitempty"`
	CreatedAt  time.Time       `json:"createdAt"`

	// 内部用。API には出さない。
	authorDID string // ブロック判定に使う
	modLabel  string // ok / warn / block
}

// FeedPage は 17 §3.1 のレスポンス。
type FeedPage struct {
	Items  []FeedItem `json:"items"`
	Cursor string     `json:"cursor,omitempty"`
}

// FeedService は source から要素を集めて 1 つのフィードにする。
type FeedService struct {
	Deps
	Access AccessChecker
	Signer storage.Signer
}

// List は source（カンマ区切りで複数可）からフィードを返す（17 §3.1）。
func (s *FeedService) List(ctx context.Context, viewer, source, cursor string, limit int) (*FeedPage, error) {
	sources, err := s.buildSources(source)
	if err != nil {
		return nil, err
	}
	return s.ListSources(ctx, viewer, sources, cursor, limit)
}

// ListSources は組み立て済みの source をマージして返す。複数 source のマージはここで行う。
func (s *FeedService) ListSources(ctx context.Context, viewer string, sources []FeedSource, cursor string, limit int) (*FeedPage, error) {
	if limit <= 0 {
		limit = 30
	}
	before, err := decodeFeedCursor(cursor)
	if err != nil {
		return nil, err
	}
	blocked, err := model.BlockedDIDs(ctx, s.DB, viewer)
	if err != nil {
		return nil, err
	}
	warnDisplay, err := warnDisplayFor(ctx, s.DB, viewer)
	if err != nil {
		return nil, err
	}
	now := s.Clock.Now()
	keep := func(it *FeedItem) (bool, error) {
		return decorateItem(ctx, s.DB, s.Config.PublicURL, s.Signer, now, viewer, warnDisplay, blocked, it)
	}
	items, next, hasMore, err := mergeSources(ctx, sources, viewer, before, limit, keep)
	if err != nil {
		return nil, err
	}
	page := &FeedPage{Items: items}
	if hasMore {
		page.Cursor = utils.EncodeCursor(next.At.UnixMilli(), next.URI)
	}
	return page, nil
}

// buildSources はカンマ区切りの source 名を FeedSource に組み立てる。1 つでも不正なら 400。
func (s *FeedService) buildSources(source string) ([]FeedSource, error) {
	parts := strings.Split(source, ",")
	out := make([]FeedSource, 0, len(parts))
	for _, p := range parts {
		src, err := s.buildSource(strings.TrimSpace(p))
		if err != nil {
			return nil, err
		}
		out = append(out, src)
	}
	return out, nil
}

func (s *FeedService) buildSource(name string) (FeedSource, error) {
	invalid := func() error {
		return httpx.ErrInvalidRequest.With("fields", map[string]string{"source": "source が不正です"})
	}
	switch {
	case name == feedHome:
		return &homeSource{f: s}, nil
	case name == feedGroups:
		return &groupsSource{f: s}, nil
	case strings.HasPrefix(name, feedSpacePrefix):
		id := strings.TrimPrefix(name, feedSpacePrefix)
		if id == "" {
			return nil, invalid()
		}
		return &spaceSource{f: s, spaceID: id}, nil
	case strings.HasPrefix(name, feedUserPrefix):
		did := strings.TrimPrefix(name, feedUserPrefix)
		if did == "" {
			return nil, invalid()
		}
		return &userSource{f: s, did: did}, nil
	default:
		return nil, invalid()
	}
}

func decodeFeedCursor(cursor string) (FeedCursor, error) {
	at, uri, err := utils.DecodeCursor(cursor)
	if err != nil {
		return FeedCursor{}, httpx.ErrInvalidRequest.With("fields", map[string]string{"cursor": "カーソルが不正です"})
	}
	if at == 0 && uri == "" {
		return FeedCursor{}, nil
	}
	return FeedCursor{At: clock.FromUnixMilli(at), URI: uri}, nil
}

// mergeSources は複数の source を createdAt（同じなら uri）の降順にマージし、keep が通ったものを
// 最大 limit 件返す。続きがあるときは hasMore を立て、next に最後の要素の (createdAt, uri) を返す。
func mergeSources(ctx context.Context, sources []FeedSource, viewer string, before FeedCursor, limit int,
	keep func(*FeedItem) (bool, error)) (items []FeedItem, next FeedCursor, hasMore bool, err error) {

	target := limit + 1 // 1 件多く取って「続きがあるか」を判断する
	pos := make([]FeedCursor, len(sources))
	for i := range pos {
		pos[i] = before
	}
	exhausted := make([]bool, len(sources))
	items = make([]FeedItem, 0, target)

	for len(items) < target {
		type cand struct {
			src  int
			item FeedItem
		}
		var cands []cand
		progressed := false
		for i := range sources {
			if exhausted[i] {
				continue
			}
			got, ferr := sources[i].Fetch(ctx, viewer, pos[i], target)
			if ferr != nil {
				return nil, FeedCursor{}, false, ferr
			}
			if len(got) < target {
				exhausted[i] = true
			}
			if len(got) > 0 {
				progressed = true
			}
			for _, it := range got {
				cands = append(cands, cand{src: i, item: it})
			}
		}
		if !progressed {
			break
		}
		sort.SliceStable(cands, func(i, j int) bool {
			return feedItemAfter(cands[i].item, cands[j].item)
		})
		for _, c := range cands {
			if len(items) >= target {
				break
			}
			// 表示しなかった要素の先へ読み進める（次のバッチで読み直さないため）。
			pos[c.src] = FeedCursor{At: c.item.CreatedAt, URI: c.item.URI}
			ok, ferr := keep(&c.item)
			if ferr != nil {
				return nil, FeedCursor{}, false, ferr
			}
			if !ok {
				continue
			}
			items = append(items, c.item)
		}
	}

	if len(items) > limit {
		hasMore = true
		items = items[:limit]
	}
	if len(items) > 0 {
		last := items[len(items)-1]
		next = FeedCursor{At: last.CreatedAt, URI: last.URI}
	}
	return items, next, hasMore, nil
}

// homeSource は自分・フレンド・直近 30 日に revealed な encounter がある相手の公開投稿（17 §3.1）。
type homeSource struct{ f *FeedService }

func (s *homeSource) Fetch(ctx context.Context, viewer string, before FeedCursor, limit int) ([]FeedItem, error) {
	authors, err := s.f.homeAuthors(ctx, viewer)
	if err != nil {
		return nil, err
	}
	posts, err := model.ListPublicPostsByAuthors(ctx, s.f.DB, authors, beforeMillis(before), before.URI, limit)
	if err != nil {
		return nil, err
	}
	return publicPostItems(posts), nil
}

func (s *FeedService) homeAuthors(ctx context.Context, viewer string) ([]string, error) {
	set := map[string]bool{viewer: true}
	friends, err := model.ListFriendships(ctx, s.DB, viewer)
	if err != nil {
		return nil, err
	}
	for _, fr := range friends {
		set[otherDID(fr.UserA, fr.UserB, viewer)] = true
	}
	since := s.Clock.Now().Add(-encounterFeedWindow)
	peers, err := model.ListRevealedEncounterPeers(ctx, s.DB, viewer, since)
	if err != nil {
		return nil, err
	}
	for _, p := range peers {
		set[p] = true
	}
	out := make([]string, 0, len(set))
	for did := range set {
		out = append(out, did)
	}
	sort.Strings(out)
	return out, nil
}

func otherDID(a, b, me string) string {
	if a == me {
		return b
	}
	return a
}

// groupsSource は自分が read できる active なグループすべての instance の投稿とメディア（17 §3.1）。
// Party は含めない。
type groupsSource struct{ f *FeedService }

func (s *groupsSource) Fetch(ctx context.Context, viewer string, before FeedCursor, limit int) ([]FeedItem, error) {
	spaces, err := model.ListUserSpaces(ctx, s.f.DB, viewer)
	if err != nil {
		return nil, err
	}
	items := make([]FeedItem, 0, limit)
	for _, item := range spaces {
		if item.Space.State != "active" || item.Space.Type != spaceTypeGroup {
			continue
		}
		sp := item.Space
		ok, err := s.f.Access.Check(ctx, sp.ID, syntax.DID(viewer), ModeRead)
		if err != nil || !ok {
			// read できないグループ（退出済み・エリア外・位置が古いなど）は対象から外す。
			continue
		}
		recs, err := model.ListRecordsInSpace(ctx, s.f.DB, sp.ID,
			[]string{CollectionPost, CollectionMedia}, beforeMillis(before), before.URI, limit)
		if err != nil {
			return nil, err
		}
		got, err := s.f.instanceItems(ctx, &sp, recs)
		if err != nil {
			return nil, err
		}
		items = append(items, got...)
	}
	sortFeedItems(items)
	if len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

// spaceSource は 1 つの Space の instance の投稿とメディア（17 §3.1）。今の GET /instances/{id}/posts と同じ中身。
type spaceSource struct {
	f       *FeedService
	spaceID string
}

func (s *spaceSource) Fetch(ctx context.Context, viewer string, before FeedCursor, limit int) ([]FeedItem, error) {
	sp, err := model.GetSpace(ctx, s.f.DB, s.spaceID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, httpx.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := s.f.checkRead(ctx, s.spaceID, viewer); err != nil {
		return nil, err
	}
	recs, err := model.ListRecordsInSpace(ctx, s.f.DB, s.spaceID,
		[]string{CollectionPost, CollectionMedia}, beforeMillis(before), before.URI, limit)
	if err != nil {
		return nil, err
	}
	return s.f.instanceItems(ctx, sp, recs)
}

// userSource は 13 §1.2 の関係がある相手（自分を含む）の公開投稿（17 §3.1）。関係がなければ 404。
type userSource struct {
	f   *FeedService
	did string
}

func (s *userSource) Fetch(ctx context.Context, viewer string, before FeedCursor, limit int) ([]FeedItem, error) {
	if err := s.f.checkUserSource(ctx, viewer, s.did); err != nil {
		return nil, err
	}
	posts, err := model.ListPublicPostsByAuthors(ctx, s.f.DB, []string{s.did}, beforeMillis(before), before.URI, limit)
	if err != nil {
		return nil, err
	}
	return publicPostItems(posts), nil
}

func (s *FeedService) checkUserSource(ctx context.Context, viewer, target string) error {
	u, err := model.GetUser(ctx, s.DB, target)
	if errors.Is(err, sql.ErrNoRows) {
		return httpx.ErrNotFound
	}
	if err != nil {
		return err
	}
	if u.DeletedAt != nil {
		return httpx.ErrNotFound
	}
	if viewer == target {
		return nil
	}
	blocked, err := model.IsBlocked(ctx, s.DB, viewer, target)
	if err != nil {
		return err
	}
	if blocked {
		return httpx.ErrNotFound
	}
	related, err := model.HasRelationship(ctx, s.DB, viewer, target)
	if err != nil {
		return err
	}
	if !related {
		return httpx.ErrNotFound
	}
	return nil
}

func (s *FeedService) checkRead(ctx context.Context, spaceID, viewer string) error {
	ok, err := s.Access.Check(ctx, spaceID, syntax.DID(viewer), ModeRead)
	if err != nil {
		return err
	}
	if !ok {
		return httpx.ErrForbidden
	}
	return nil
}

func (s *FeedService) instanceItems(ctx context.Context, sp *model.Space, recs []model.Record) ([]FeedItem, error) {
	items := make([]FeedItem, 0, len(recs))
	for _, rec := range recs {
		it, err := newInstanceItem(ctx, s.DB, sp, rec)
		if err != nil {
			return nil, err
		}
		items = append(items, *it)
	}
	return items, nil
}

// newInstanceItem は instance の record を、内容だけの FeedItem にする（ブロック・moderation・
// メディアの署名付き URL は decorateItem がかける）。
func newInstanceItem(ctx context.Context, q *sql.DB, sp *model.Space, rec model.Record) (*FeedItem, error) {
	author, err := authorSummaryFor(ctx, q, rec.AuthorDID)
	if err != nil {
		return nil, err
	}
	item := &FeedItem{
		URI:       rec.URI,
		Author:    author,
		CreatedAt: rec.CreatedAt,
		authorDID: rec.AuthorDID,
		modLabel:  rec.ModLabel,
	}
	if sp != nil {
		item.Space = feedSpaceOf(sp)
	}
	switch rec.Collection {
	case CollectionPost:
		var v postValue
		if err := json.Unmarshal([]byte(rec.ValueJSON), &v); err != nil {
			return nil, err
		}
		item.Kind = "instance_post"
		item.Text = v.Text
		item.Format = v.Format
		item.System = v.System
		if v.Reply != nil {
			item.Reply = &ReplyRef{Root: v.Reply.Root, Parent: v.Reply.Parent}
		}
		for _, ref := range v.Media {
			item.Media = append(item.Media, MediaItem{ID: ref.ID, Alt: ref.Alt})
		}
	default:
		var v mediaValue
		if err := json.Unmarshal([]byte(rec.ValueJSON), &v); err != nil {
			return nil, err
		}
		item.Kind = "instance_media"
		item.Media = []MediaItem{{ID: v.Media.ID, Alt: v.Media.Alt}}
		if v.TakenAt != "" {
			if t, err := time.Parse(time.RFC3339, v.TakenAt); err == nil {
				item.TakenAt = &t
			}
		}
	}
	return item, nil
}

// decorateItem は 17 §3.3 の一律処理をかける。表示しない場合は ok=false。
// ブロック関係（どちら向きでも）の相手を除き、moderation の表示指示を付け、メディアの URL を署名する。
func decorateItem(ctx context.Context, q *sql.DB, publicURL string, signer storage.Signer, now time.Time,
	viewer, warnDisplay string, blocked map[string]bool, item *FeedItem) (bool, error) {

	if item.authorDID != "" && blocked[item.authorDID] {
		return false, nil
	}
	switch item.modLabel {
	case "block":
		// block は投稿者本人にだけ hidden:"moderation" として返す（10 §4.2、11 §4）。
		if item.authorDID != viewer {
			return false, nil
		}
		item.Hidden = "moderation"
		item.Moderation = &ModerationInfo{Label: "block", Display: "hide"}
	case "warn":
		if warnDisplay == "hide" && item.authorDID != viewer {
			return false, nil
		}
		item.Moderation = &ModerationInfo{Label: "warn", Display: warnDisplay}
	default:
		item.Moderation = &ModerationInfo{Label: "ok", Display: "show"}
	}
	if item.Author == nil {
		author, err := authorSummaryFor(ctx, q, item.authorDID)
		if err != nil {
			return false, err
		}
		item.Author = author
	}
	if len(item.Media) > 0 {
		signed := make([]MediaItem, 0, len(item.Media))
		for _, ref := range item.Media {
			mi, err := mediaItemFor(ctx, q, signer, publicURL, now, ref.ID, ref.Alt)
			if err != nil {
				return false, err
			}
			if mi != nil {
				signed = append(signed, *mi)
			}
		}
		item.Media = signed
	}
	return true, nil
}

func publicPostItems(posts []model.PublicPost) []FeedItem {
	items := make([]FeedItem, 0, len(posts))
	for _, p := range posts {
		items = append(items, publicPostItem(p))
	}
	return items
}

func publicPostItem(p model.PublicPost) FeedItem {
	it := FeedItem{
		Kind:      "public_post",
		URI:       p.URI,
		Text:      p.Text,
		Format:    "plain",
		CreatedAt: p.CreatedAt,
		authorDID: p.AuthorDID,
		modLabel:  p.ModLabel,
	}
	if p.BskyURI != "" {
		it.Bsky = &BskyRef{URI: p.BskyURI}
	}
	return it
}

func feedSpaceOf(sp *model.Space) *FeedSpace {
	return &FeedSpace{ID: sp.ID, Type: feedSpaceType(sp.Type), Name: sp.Name}
}

func feedSpaceType(nsid string) string {
	switch nsid {
	case spaceTypeGroup:
		return "group"
	case spaceTypeParty:
		return "party"
	default:
		return nsid
	}
}

// warnDisplayFor は閲覧者の warn_display を返す。設定がなければ既定の blur（11 §1）。
func warnDisplayFor(ctx context.Context, q *sql.DB, did string) (string, error) {
	st, err := model.GetSettings(ctx, q, did)
	if errors.Is(err, sql.ErrNoRows) {
		return "blur", nil
	}
	if err != nil {
		return "", err
	}
	return st.WarnDisplay, nil
}

func beforeMillis(c FeedCursor) int64 {
	if c.At.IsZero() {
		return 0
	}
	return c.At.UnixMilli()
}

// feedItemAfter は a が降順で b より先に来るか（createdAt の新しい順、同じなら uri の大きい順）。
func feedItemAfter(a, b FeedItem) bool {
	if !a.CreatedAt.Equal(b.CreatedAt) {
		return a.CreatedAt.After(b.CreatedAt)
	}
	return a.URI > b.URI
}

func sortFeedItems(items []FeedItem) {
	sort.SliceStable(items, func(i, j int) bool { return feedItemAfter(items[i], items[j]) })
}
