package party

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/applemango/chain/internal/db"
	"github.com/applemango/chain/internal/httpx"
	"github.com/applemango/chain/internal/model"
	friendmodel "github.com/applemango/chain/internal/model/friend"
	spacemodel "github.com/applemango/chain/internal/model/space"
	"github.com/applemango/chain/internal/realtime"
	"github.com/applemango/chain/internal/service/access"
	"github.com/applemango/chain/internal/service/content"
	"github.com/applemango/chain/internal/service/core"
	"github.com/applemango/chain/internal/service/moderation"
	"github.com/applemango/chain/internal/service/notify"
	"github.com/applemango/chain/internal/service/proximity"
	"github.com/applemango/chain/internal/storage"
	"github.com/applemango/chain/internal/utils"
)

// PartyService は 08 のフレンド近接・presence・party エンジン・API を持つ。
// エンジン（Tick）は 5 秒ごとに worker.PartyEngine から呼ばれる。
type PartyService struct {
	core.Deps
	Proximity proximity.PeerProximity
	Access    *access.AccessService
	Moderator moderation.Moderator
	Broker    realtime.Broker
	Signer    storage.Signer
	Beacon    *proximity.BeaconService
	Store     spacemodel.InstanceStore
}

const partyTitleSuffix = " の party"

// FriendBeaconToken は GET /party/friend-beacons の token 1 件（08 §2）。
type FriendBeaconToken struct {
	WindowStart time.Time
	Token       string
}

type FriendBeacon struct {
	DID    string
	Tokens []FriendBeaconToken
}

type FriendBeaconResult struct {
	ValidUntil time.Time
	Friends    []FriendBeacon
}

// PresenceObservation は POST /party/presence の 1 件（08 §2）。
type PresenceObservation struct {
	DID        string
	LastSeenAt time.Time
	RSSI       *int
}

// PartyMemberItem は GET /party/current のメンバー 1 件（08 §6）。
type PartyMemberItem struct {
	DID         string `json:"did"`
	Handle      string `json:"handle,omitempty"`
	DisplayName string `json:"displayName,omitempty"`
	AvatarURL   string `json:"avatarUrl,omitempty"`
	Present     bool   `json:"present"`
	Left        bool   `json:"left"`
}

// PartyDetail は GET /party/current の応答（08 §6）。
type PartyDetail struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Phase       string            `json:"phase"`
	Locked      bool              `json:"locked"`
	TentativeAt time.Time         `json:"tentativeAt"`
	ConfirmedAt *time.Time        `json:"confirmedAt,omitempty"`
	Members     []PartyMemberItem `json:"members"`
}

// MemoryCover は memory の表紙（13 §2）。
type MemoryCover struct {
	ThumbURL string `json:"thumbUrl"`
}

// MemoryItem は GET /memories の 1 件（13 §2）。
type MemoryItem struct {
	SpaceID     string       `json:"spaceId"`
	Type        string       `json:"type"`
	Name        string       `json:"name"`
	From        time.Time    `json:"from"`
	Until       time.Time    `json:"until"`
	MemberCount int          `json:"memberCount"`
	MediaCount  int          `json:"mediaCount"`
	Cover       *MemoryCover `json:"cover,omitempty"`
}

type MemoryPage struct {
	Items  []MemoryItem `json:"items"`
	Cursor string       `json:"cursor,omitempty"`
}

// FriendBeacons は、相手が friend_beacon_share = 1 かつ party_auto = 1 のフレンドの token を
// 現在から core.FriendBeaconHorizon 分だけ返す（08 §2）。未発行の窓はここで作る。
func (s *PartyService) FriendBeacons(ctx context.Context, me string) (*FriendBeaconResult, error) {
	now := s.Clock.Now()
	validUntil := time.Unix(core.WindowStart(now)+int64(core.FriendBeaconHorizon/time.Second), 0).UTC()

	friends, err := friendmodel.ListFriendships(ctx, s.DB, me)
	if err != nil {
		return nil, err
	}
	beacon := s.Beacon
	if beacon == nil {
		beacon = &proximity.BeaconService{Deps: s.Deps}
	}
	friendsOut := make([]FriendBeacon, 0, len(friends))
	for _, f := range friends {
		peer := f.UserA
		if peer == me {
			peer = f.UserB
		}
		if peer == me {
			continue
		}
		u, err := model.GetUser(ctx, s.DB, peer)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if u.DeletedAt != nil {
			continue
		}
		share, auto, err := s.beaconSettings(ctx, peer)
		if err != nil {
			return nil, err
		}
		if !share || !auto {
			continue
		}
		blocked, err := model.IsBlocked(ctx, s.DB, me, peer)
		if err != nil {
			return nil, err
		}
		if blocked {
			continue
		}
		_, tokens, err := beacon.Tokens(ctx, peer, int(core.FriendBeaconHorizon/time.Hour))
		if err != nil {
			return nil, err
		}
		fb := FriendBeacon{DID: peer, Tokens: make([]FriendBeaconToken, 0, len(tokens))}
		for _, t := range tokens {
			fb.Tokens = append(fb.Tokens, FriendBeaconToken{WindowStart: t.WindowStart, Token: t.Token})
		}
		friendsOut = append(friendsOut, fb)
	}
	return &FriendBeaconResult{ValidUntil: validUntil, Friends: friendsOut}, nil
}

func (s *PartyService) beaconSettings(ctx context.Context, did string) (share, auto bool, err error) {
	st, err := model.GetSettings(ctx, s.DB, did)
	if errors.Is(err, sql.ErrNoRows) {
		return true, true, nil
	}
	if err != nil {
		return false, false, err
	}
	return st.FriendBeaconShare, st.PartyAuto, nil
}

// Presence は観測を受け取り、フレンドでない DID とブロック関係を無視して
// proximity.PeerProximity.Observe と近接エッジの更新を行う（08 §2、§3）。
func (s *PartyService) Presence(ctx context.Context, me string, observations []PresenceObservation) error {
	now := s.Clock.Now()
	for _, o := range observations {
		if o.DID == "" || o.DID == me {
			continue
		}
		if _, err := friendmodel.GetFriendship(ctx, s.DB, me, o.DID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			return err
		}
		blocked, err := model.IsBlocked(ctx, s.DB, me, o.DID)
		if err != nil {
			return err
		}
		if blocked {
			continue
		}
		u, err := model.GetUser(ctx, s.DB, o.DID)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}
		if u.DeletedAt != nil {
			continue
		}
		t := o.LastSeenAt
		if t.IsZero() {
			t = now
		}
		if s.Proximity != nil {
			if err := s.Proximity.Observe(ctx, me, o.DID, t, t, o.RSSI); err != nil {
				return err
			}
		}
		if err := model.ObservePeerEdge(ctx, s.Redis, me, o.DID, t, core.EdgeGapReset); err != nil {
			return err
		}
	}
	return nil
}

// Current は所属中の party を返す。離脱済み・非メンバーなら nil（08 §6）。
func (s *PartyService) Current(ctx context.Context, me string) (*PartyDetail, error) {
	spaceID, ok, err := model.GetPartyOf(ctx, s.Redis, me)
	if err != nil || !ok {
		return nil, err
	}
	m, err := spacemodel.GetSpaceMember(ctx, s.DB, spaceID, me)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if m.LeftAt != nil {
		return nil, nil
	}
	return s.partyDetail(ctx, spaceID)
}

// Leave は明示的に退出させる。退出したメンバーはその party が終わるまで自動で再合流しない（08 §4.2）。
func (s *PartyService) Leave(ctx context.Context, me, id string) error {
	sp, err := spacemodel.GetSpace(ctx, s.DB, id)
	if errors.Is(err, sql.ErrNoRows) {
		return httpx.ErrNotFound
	}
	if err != nil {
		return err
	}
	if sp.Type != core.SpaceTypeParty {
		return httpx.ErrNotFound
	}
	p, err := model.GetParty(ctx, s.DB, id)
	if errors.Is(err, sql.ErrNoRows) {
		return httpx.ErrNotFound
	}
	if err != nil {
		return err
	}
	if sp.State != "active" || p.Phase == "ended" {
		return httpx.ErrGone
	}
	m, err := spacemodel.GetSpaceMember(ctx, s.DB, id, me)
	if errors.Is(err, sql.ErrNoRows) {
		return httpx.ErrNotFound
	}
	if err != nil {
		return err
	}
	if m.LeftAt != nil {
		return nil
	}
	now := s.Clock.Now()
	if err := model.LeavePartyMember(ctx, s.DB, id, me, now); err != nil {
		return err
	}
	if err := model.AddPartyLeaver(ctx, s.Redis, id, me); err != nil {
		return err
	}
	s.invalidateMember(ctx, id, me)
	s.publish(ctx, realtime.Event{Name: "member.left", Space: id, Data: map[string]string{"did": me}})
	if detail, err := s.partyDetail(ctx, id); err == nil && detail != nil {
		s.publishPartyUpdated(ctx, detail)
	}
	return nil
}

// Rename は confirmed のメンバーが party の名前を変える（08 §6）。moderation の対象。
func (s *PartyService) Rename(ctx context.Context, me, id, name string) (*PartyDetail, error) {
	name = strings.TrimSpace(name)
	fields := map[string]string{}
	if name == "" {
		fields["name"] = "必須です"
	} else if utf8.RuneCountInString(name) > 40 {
		fields["name"] = "40 文字までです"
	}
	if len(fields) > 0 {
		return nil, httpx.ErrInvalidRequest.With("fields", fields)
	}

	sp, err := spacemodel.GetSpace(ctx, s.DB, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, httpx.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if sp.Type != core.SpaceTypeParty {
		return nil, httpx.ErrNotFound
	}
	p, err := model.GetParty(ctx, s.DB, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, httpx.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if sp.State != "active" || p.Phase != "confirmed" {
		return nil, httpx.ErrForbidden
	}
	m, err := spacemodel.GetSpaceMember(ctx, s.DB, id, me)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, httpx.ErrForbidden
	}
	if err != nil {
		return nil, err
	}
	if m.LeftAt != nil || !m.CanWrite {
		return nil, httpx.ErrForbidden
	}

	if s.Moderator != nil {
		v, err := s.Moderator.Judge(ctx, moderation.Content{Kind: "party_name", Text: name})
		if err != nil {
			slog.WarnContext(ctx, "moderation error; treating as ok", "err", err)
		} else if v.Label == moderation.LabelBlock {
			s.auditModerationBlock(ctx, me)
			return nil, httpx.ErrModerationBlocked.With("categories", v.Categories)
		}
	}

	if err := spacemodel.UpdateSpaceName(ctx, s.DB, id, name); err != nil {
		return nil, err
	}
	detail, err := s.partyDetail(ctx, id)
	if err != nil {
		return nil, err
	}
	if detail != nil {
		s.publishPartyUpdated(ctx, detail)
	}
	return detail, nil
}

// SetLocked は固定（lock / unlock）を切り替える（08 §4.4）。confirmed の未離脱メンバーなら誰でもできる。
// 切り替えは authority 名義の system 投稿として残し、party.updated を配信する。
func (s *PartyService) SetLocked(ctx context.Context, me, id string, locked bool) (*PartyDetail, error) {
	sp, err := spacemodel.GetSpace(ctx, s.DB, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, httpx.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if sp.Type != core.SpaceTypeParty {
		return nil, httpx.ErrNotFound
	}
	p, err := model.GetParty(ctx, s.DB, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, httpx.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if sp.State != "active" || p.Phase != "confirmed" {
		return nil, httpx.ErrForbidden
	}
	m, err := spacemodel.GetSpaceMember(ctx, s.DB, id, me)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, httpx.ErrForbidden
	}
	if err != nil {
		return nil, err
	}
	if m.LeftAt != nil {
		return nil, httpx.ErrForbidden
	}
	if p.Locked == locked {
		return s.partyDetail(ctx, id)
	}

	now := s.Clock.Now()
	if err := model.UpdatePartyLocked(ctx, s.DB, id, locked); err != nil {
		return nil, err
	}
	if err := model.TouchPartyActivity(ctx, s.DB, id, now); err != nil {
		return nil, err
	}
	if err := s.postSystemLock(ctx, sp, me, locked, now); err != nil {
		return nil, err
	}
	detail, err := s.partyDetail(ctx, id)
	if err != nil {
		return nil, err
	}
	if detail != nil {
		s.publishPartyUpdated(ctx, detail)
	}
	return detail, nil
}

// postSystemLock は lock / unlock を system 投稿として Space に残す（08 §4.4）。
func (s *PartyService) postSystemLock(ctx context.Context, sp *spacemodel.Space, actor string, locked bool, now time.Time) error {
	if s.Store == nil {
		return nil
	}
	text := s.lockText(ctx, actor, locked)
	value := content.PostValue{Text: text, Format: "plain", System: true, CreatedAt: core.FormatDateTime(now)}
	rec, err := s.Store.CreateRecord(ctx, sp.SpaceRef(), syntax.DID(sp.Authority), syntax.NSID(core.CollectionPost), value)
	if err != nil {
		return err
	}
	item := content.NewSystemFeedItem(rec.URI, content.FeedSpaceOf(sp), text, sp.Authority, rec.CreatedAt)
	s.publish(ctx, realtime.Event{Name: "post.created", Space: sp.ID, Data: item})
	return nil
}

func (s *PartyService) lockText(ctx context.Context, did string, locked bool) string {
	name := did
	if sum, err := core.AuthorSummaryFor(ctx, s.DB, did); err == nil && sum != nil {
		if sum.DisplayName != "" {
			name = sum.DisplayName
		} else if sum.Handle != "" {
			name = sum.Handle
		}
	}
	if locked {
		return name + "さんが party を固定しました"
	}
	return name + "さんが party の固定を解除しました"
}

// partyConfirmedPayload は party.confirmed の payload（12 §3）。遷移先は Space。
type partyConfirmedPayload struct {
	Text    string `json:"text"`
	SpaceID string `json:"spaceId"`
}

// confirmedText は party.confirmed の本文を、自分以外のメンバーの名前から作る（12 §3）。
func (s *PartyService) confirmedText(ctx context.Context, self string, members []spacemodel.SpaceMember) string {
	for _, m := range members {
		if m.DID == self || m.LeftAt != nil {
			continue
		}
		if sum, err := core.AuthorSummaryFor(ctx, s.DB, m.DID); err == nil && sum != nil {
			name := sum.DisplayName
			if name == "" {
				name = sum.Handle
			}
			if name != "" {
				return name + "さんたちと party が始まりました"
			}
		}
	}
	return "party が始まりました"
}

// Memories は archived の Party とグループのうち自分が hidden_from_memory = 0 のものを返す（13 §2）。
func (s *PartyService) Memories(ctx context.Context, me, cursor string, limit int) (*MemoryPage, error) {
	beforeAt, beforeID, err := utils.DecodeCursor(cursor)
	if err != nil {
		return nil, httpx.ErrInvalidRequest.With("fields", map[string]string{"cursor": "カーソルが不正です"})
	}
	rows, err := spacemodel.ListMemories(ctx, s.DB, me, beforeAt, beforeID, limit+1)
	if err != nil {
		return nil, err
	}
	now := s.Clock.Now()
	page := &MemoryPage{Items: make([]MemoryItem, 0, limit)}
	for _, r := range rows {
		if len(page.Items) >= limit {
			break
		}
		item := MemoryItem{
			SpaceID: r.SpaceID, Type: memoryTypeName(r.Type), Name: r.Name,
			From: r.From, Until: r.Until,
			MemberCount: r.MemberCount, MediaCount: r.MediaCount,
		}
		if r.CoverMediaID != nil {
			item.Cover = &MemoryCover{
				ThumbURL: s.Signer.URL(s.Config.PublicURL, *r.CoverMediaID, storage.VariantThumb, now.Add(content.MediaURLTTL).Unix()),
			}
		}
		page.Items = append(page.Items, item)
	}
	if len(rows) > limit {
		last := rows[len(page.Items)-1]
		page.Cursor = utils.EncodeCursor(last.Until.UnixMilli(), last.SpaceID)
	}
	return page, nil
}

// HideMemory は自分の memory から Space を隠す（hidden_from_memory = 1、13 §2）。
// 自分がメンバーだった archived の Space でなければ 404。
func (s *PartyService) HideMemory(ctx context.Context, me, spaceID string) error {
	sp, err := spacemodel.GetSpace(ctx, s.DB, spaceID)
	if errors.Is(err, sql.ErrNoRows) {
		return httpx.ErrNotFound
	}
	if err != nil {
		return err
	}
	if sp.State != "archived" {
		return httpx.ErrNotFound
	}
	if _, err := spacemodel.GetSpaceMember(ctx, s.DB, spaceID, me); errors.Is(err, sql.ErrNoRows) {
		return httpx.ErrNotFound
	} else if err != nil {
		return err
	}
	return spacemodel.HideSpaceMemberFromMemory(ctx, s.DB, spaceID, me)
}

// memoryTypeName は DB の Space の type（NSID）を memory の type に変換する（13 §2）。
func memoryTypeName(nsid string) string {
	switch nsid {
	case core.SpaceTypeParty:
		return "party"
	case core.SpaceTypeGroup:
		return "group"
	default:
		return nsid
	}
}

// RebuildIndex は SQLite の party 所属から party:of:* を組み直す（08 §4、再起動時）。
func (s *PartyService) RebuildIndex(ctx context.Context) error {
	if err := model.ClearAllPartyOf(ctx, s.Redis); err != nil {
		return err
	}
	refs, err := model.ListActivePartyMembers(ctx, s.DB)
	if err != nil {
		return err
	}
	for _, r := range refs {
		if err := model.SetPartyOf(ctx, s.Redis, r.DID, r.SpaceID); err != nil {
			return err
		}
	}
	return nil
}

func (s *PartyService) partyDetail(ctx context.Context, spaceID string) (*PartyDetail, error) {
	sp, err := spacemodel.GetSpace(ctx, s.DB, spaceID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if sp.Type != core.SpaceTypeParty || sp.State != "active" {
		return nil, nil
	}
	p, err := model.GetParty(ctx, s.DB, spaceID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if p.Phase == "ended" {
		return nil, nil
	}
	members, err := spacemodel.ListSpaceMembers(ctx, s.DB, spaceID)
	if err != nil {
		return nil, err
	}
	present, err := s.presentSet(ctx, members)
	if err != nil {
		return nil, err
	}
	return s.buildDetail(ctx, sp, p, members, present), nil
}

func (s *PartyService) buildDetail(ctx context.Context, sp *spacemodel.Space, p *model.Party, members []spacemodel.SpaceMember, present map[string]bool) *PartyDetail {
	detail := &PartyDetail{
		ID: sp.ID, Name: sp.Name, Phase: p.Phase, Locked: p.Locked,
		TentativeAt: p.TentativeAt, ConfirmedAt: p.ConfirmedAt,
		Members: make([]PartyMemberItem, 0, len(members)),
	}
	for _, m := range members {
		item := PartyMemberItem{DID: m.DID, Present: present[m.DID], Left: m.LeftAt != nil}
		if sum, err := core.AuthorSummaryFor(ctx, s.DB, m.DID); err == nil && sum != nil {
			item.Handle = sum.Handle
			item.DisplayName = sum.DisplayName
			item.AvatarURL = sum.AvatarURL
		}
		detail.Members = append(detail.Members, item)
	}
	return detail
}

func (s *PartyService) presentSet(ctx context.Context, members []spacemodel.SpaceMember) (map[string]bool, error) {
	memberSet := make(map[string]bool, len(members))
	for _, m := range members {
		memberSet[m.DID] = true
	}
	edges, err := model.LivePeerEdges(ctx, s.Redis, s.Clock.Now(), core.EdgeAlive)
	if err != nil {
		return nil, err
	}
	present := map[string]bool{}
	for _, e := range edges {
		if memberSet[e.A] && memberSet[e.B] {
			present[e.A] = true
			present[e.B] = true
		}
	}
	return present, nil
}

func (s *PartyService) auditModerationBlock(ctx context.Context, did string) {
	err := model.InsertAuditLog(ctx, s.DB, model.AuditEntry{
		ID: s.IDs.NewID(), ActorDID: did, Action: "moderation.block", Target: "party_name", CreatedAt: s.Clock.Now(),
	})
	if err != nil {
		slog.WarnContext(ctx, "audit log failed", "err", err)
	}
}

// Tick は 08 §4.1〜§4.3 の party エンジン本体。worker から 5 秒ごとに呼ばれる。
func (s *PartyService) Tick(ctx context.Context) error {
	now := s.Clock.Now()
	edges, err := s.liveEdges(ctx, now)
	if err != nil {
		return err
	}

	stable := make([]partyEdge, 0, len(edges))
	stableEdges := make([]model.PeerEdge, 0, len(edges))
	for _, e := range edges {
		if now.Sub(e.Since) >= core.TentativeAfter {
			stable = append(stable, partyEdge{A: e.A, B: e.B})
			stableEdges = append(stableEdges, e)
		}
	}

	nodes := map[string]bool{}
	for _, e := range stable {
		nodes[e.A] = true
		nodes[e.B] = true
	}
	partyOf, err := model.GetPartyOfMany(ctx, s.Redis, didList(nodes))
	if err != nil {
		return err
	}
	for _, comp := range connectedComponents(stable) {
		if err := s.integrate(ctx, now, comp, stableEdges, partyOf); err != nil {
			return err
		}
	}
	return s.advance(ctx, now, edges)
}

func (s *PartyService) liveEdges(ctx context.Context, now time.Time) ([]model.PeerEdge, error) {
	edges, err := model.LivePeerEdges(ctx, s.Redis, now, core.EdgeAlive)
	if err != nil {
		return nil, err
	}
	if len(edges) == 0 {
		return nil, nil
	}
	dids := map[string]bool{}
	for _, e := range edges {
		dids[e.A] = true
		dids[e.B] = true
	}
	list := didList(dids)
	auto, err := model.PartyAutoMap(ctx, s.DB, list)
	if err != nil {
		return nil, err
	}
	deleted, err := model.DeletedDIDs(ctx, s.DB, list)
	if err != nil {
		return nil, err
	}
	blockCache := map[string]bool{}
	out := make([]model.PeerEdge, 0, len(edges))
	for _, e := range edges {
		if !auto[e.A] || !auto[e.B] || deleted[e.A] || deleted[e.B] {
			continue
		}
		blocked, ok := blockCache[pairKey(e.A, e.B)]
		if !ok {
			blocked, err = model.IsBlocked(ctx, s.DB, e.A, e.B)
			if err != nil {
				return nil, err
			}
			blockCache[pairKey(e.A, e.B)] = blocked
		}
		if blocked {
			continue
		}
		out = append(out, e)
	}
	return out, nil
}

func (s *PartyService) integrate(ctx context.Context, now time.Time, comp []string, stableEdges []model.PeerEdge, partyOf map[string]string) error {
	infos := map[string]*partyInfo{}
	order := []string{}
	for _, did := range comp {
		sp, ok := partyOf[did]
		if !ok {
			continue
		}
		info := infos[sp]
		if info == nil {
			p, err := model.GetParty(ctx, s.DB, sp)
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			if err != nil {
				return err
			}
			if p.Phase != "tentative" && p.Phase != "confirmed" {
				continue
			}
			info = &partyInfo{SpaceID: sp, Phase: p.Phase, TentativeAt: p.TentativeAt}
			infos[sp] = info
			order = append(order, sp)
		}
		info.Members = append(info.Members, did)
	}
	parties := make([]partyInfo, 0, len(order))
	for _, sp := range order {
		parties = append(parties, *infos[sp])
	}

	since := map[string]time.Time{}
	inComp := map[string]bool{}
	for _, did := range comp {
		inComp[did] = true
	}
	for _, e := range stableEdges {
		if inComp[e.A] && inComp[e.B] {
			since[pairKey(e.A, e.B)] = e.Since
		}
	}
	off := map[string]string{}
	for _, did := range comp {
		if sp, ok := partyOf[did]; ok {
			off[did] = sp
		}
	}

	plan := planIntegration(comp, parties, since, off)
	if plan.Create {
		if err := s.createParty(ctx, now, plan.CreateMembers); err != nil {
			return err
		}
	}
	for _, target := range sortedKeys(plan.Assign) {
		if err := s.addMembers(ctx, now, target, plan.Assign[target]); err != nil {
			return err
		}
	}
	for _, sp := range plan.Dissolve {
		if err := s.dissolveParty(ctx, now, sp, plan.MergeInto); err != nil {
			return err
		}
	}
	return nil
}

func (s *PartyService) createParty(ctx context.Context, now time.Time, members []string) error {
	if len(members) == 0 {
		return nil
	}
	members = append([]string(nil), members...)
	sort.Strings(members)
	if len(members) > core.MaxMembers {
		members = members[:core.MaxMembers]
	}
	id := s.IDs.NewID()
	authority := core.AuthorityFor(s.Config.PublicURL)
	space := spacemodel.Space{
		ID: id, Type: core.SpaceTypeParty, Authority: authority,
		Ref:        spacemodel.SpaceRef{Authority: authority, Type: core.SpaceTypeParty, ID: id}.String(),
		ReadPolicy: "managing-app", WritePolicy: "managing-app",
		Name: partyName(now), State: "active", CreatedBy: members[0], CreatedAt: now,
	}
	party := model.Party{SpaceID: id, Phase: "tentative", TentativeAt: now, LastActivityAt: now}
	err := db.Transaction(ctx, s.DB, func(tx *sql.Tx) error {
		if err := spacemodel.InsertSpace(ctx, tx, space); err != nil {
			return err
		}
		if err := model.InsertParty(ctx, tx, party); err != nil {
			return err
		}
		for _, did := range members {
			if err := spacemodel.InsertSpaceMember(ctx, tx, spacemodel.SpaceMember{
				SpaceID: id, DID: did, CanRead: true, CanWrite: true, Role: "member", JoinedAt: now,
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	for _, did := range members {
		if err := model.SetPartyOf(ctx, s.Redis, did, id); err != nil {
			return err
		}
		s.publish(ctx, realtime.Event{Name: "member.joined", Space: id, Data: map[string]string{"did": did}})
	}
	if detail, err := s.partyDetail(ctx, id); err == nil && detail != nil {
		s.publishPartyUpdated(ctx, detail)
	}
	return nil
}

func (s *PartyService) addMembers(ctx context.Context, now time.Time, spaceID string, dids []string) error {
	if len(dids) == 0 {
		return nil
	}
	dids = append([]string(nil), dids...)
	sort.Strings(dids)
	count, err := spacemodel.CountActiveMembers(ctx, s.DB, spaceID)
	if err != nil {
		return err
	}
	added := false
	for _, did := range dids {
		if count >= core.MaxMembers {
			break
		}
		existing, err := spacemodel.GetSpaceMember(ctx, s.DB, spaceID, did)
		switch {
		case err == nil && existing.LeftAt == nil:
			continue
		case err == nil:
			if err := spacemodel.RejoinSpaceMember(ctx, s.DB, spaceID, did, now); err != nil {
				return err
			}
		case errors.Is(err, sql.ErrNoRows):
			if err := spacemodel.InsertSpaceMember(ctx, s.DB, spacemodel.SpaceMember{
				SpaceID: spaceID, DID: did, CanRead: true, CanWrite: true, Role: "member", JoinedAt: now,
			}); err != nil {
				return err
			}
		default:
			return err
		}
		if err := model.SetPartyOf(ctx, s.Redis, did, spaceID); err != nil {
			return err
		}
		count++
		added = true
		s.publish(ctx, realtime.Event{Name: "member.joined", Space: spaceID, Data: map[string]string{"did": did}})
	}
	if !added {
		return nil
	}
	if err := model.TouchPartyActivity(ctx, s.DB, spaceID, now); err != nil {
		return err
	}
	s.invalidateSpace(ctx, spaceID)
	if detail, err := s.partyDetail(ctx, spaceID); err == nil && detail != nil {
		s.publishPartyUpdated(ctx, detail)
	}
	return nil
}

func (s *PartyService) dissolveParty(ctx context.Context, now time.Time, spaceID, mergeInto string) error {
	members, err := spacemodel.ListSpaceMembers(ctx, s.DB, spaceID)
	if err != nil {
		return err
	}
	if mergeInto != "" {
		dids := make([]string, 0, len(members))
		for _, m := range members {
			dids = append(dids, m.DID)
		}
		if err := s.addMembers(ctx, now, mergeInto, dids); err != nil {
			return err
		}
	} else {
		for _, m := range members {
			if err := model.ClearPartyOf(ctx, s.Redis, m.DID); err != nil {
				return err
			}
		}
	}
	if err := spacemodel.DeleteSpace(ctx, s.DB, spaceID); err != nil {
		return err
	}
	if err := model.DeletePartyPresence(ctx, s.Redis, spaceID); err != nil {
		return err
	}
	if err := model.DeletePartyLeavers(ctx, s.Redis, spaceID); err != nil {
		return err
	}
	s.invalidateSpace(ctx, spaceID)
	return nil
}

func (s *PartyService) advance(ctx context.Context, now time.Time, edges []model.PeerEdge) error {
	parties, err := model.ListActiveParties(ctx, s.DB)
	if err != nil {
		return err
	}
	for _, p := range parties {
		if err := s.advanceParty(ctx, now, p, edges); err != nil {
			return err
		}
	}
	return nil
}

func (s *PartyService) advanceParty(ctx context.Context, now time.Time, p model.Party, edges []model.PeerEdge) error {
	members, err := spacemodel.ListSpaceMembers(ctx, s.DB, p.SpaceID)
	if err != nil {
		return err
	}
	memberSet := make(map[string]bool, len(members))
	for _, m := range members {
		memberSet[m.DID] = true
	}
	present := map[string]bool{}
	for _, e := range edges {
		if memberSet[e.A] && memberSet[e.B] {
			present[e.A] = true
			present[e.B] = true
		}
	}
	presentActive := 0
	for _, m := range members {
		if m.LeftAt == nil && present[m.DID] {
			presentActive++
		}
	}

	switch p.Phase {
	case "tentative":
		return s.advanceTentative(ctx, now, p, members, present, presentActive)
	case "confirmed":
		return s.advanceConfirmed(ctx, now, p, members, present)
	default:
		return nil
	}
}

func (s *PartyService) advanceTentative(ctx context.Context, now time.Time, p model.Party, members []spacemodel.SpaceMember, present map[string]bool, presentActive int) error {
	if presentActive < 2 {
		if now.Sub(p.LastActivityAt) >= core.TentativeDissolve {
			return s.dissolveParty(ctx, now, p.SpaceID, "")
		}
		return nil
	}
	if now.Sub(p.TentativeAt) < core.ConfirmAfter {
		return model.TouchPartyActivity(ctx, s.DB, p.SpaceID, now)
	}
	confirmedAt := now
	var notes []notify.NotificationToSend
	err := db.Transaction(ctx, s.DB, func(tx *sql.Tx) error {
		if err := model.SetPartyPhase(ctx, tx, p.SpaceID, "confirmed", &confirmedAt, nil); err != nil {
			return err
		}
		for _, m := range members {
			if m.LeftAt != nil {
				continue
			}
			item, err := notify.EnqueueNotification(ctx, tx, s.Deps, m.DID, notify.NotificationPartyConfirmed, partyConfirmedPayload{
				Text: s.confirmedText(ctx, m.DID, members), SpaceID: p.SpaceID,
			}, now)
			if err != nil {
				return err
			}
			if item != nil {
				notes = append(notes, notify.NotificationToSend{DID: m.DID, Item: *item})
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	for _, m := range members {
		if m.LeftAt == nil && present[m.DID] {
			if err := model.SetPartyPresence(ctx, s.Redis, p.SpaceID, m.DID, now); err != nil {
				return err
			}
		}
	}
	if err := model.TouchPartyActivity(ctx, s.DB, p.SpaceID, now); err != nil {
		return err
	}
	s.invalidateSpace(ctx, p.SpaceID)
	for _, n := range notes {
		s.publish(ctx, realtime.Event{Name: "notification", User: n.DID, Data: n.Item})
	}
	if detail, err := s.partyDetail(ctx, p.SpaceID); err == nil && detail != nil {
		s.publishPartyUpdated(ctx, detail)
	}
	return nil
}

func (s *PartyService) advanceConfirmed(ctx context.Context, now time.Time, p model.Party, members []spacemodel.SpaceMember, present map[string]bool) error {
	pp, err := model.PartyPresence(ctx, s.Redis, p.SpaceID)
	if err != nil {
		return err
	}
	leavers, err := model.PartyLeavers(ctx, s.Redis, p.SpaceID)
	if err != nil {
		return err
	}

	active := map[string]bool{}
	for _, m := range members {
		active[m.DID] = m.LeftAt == nil
	}
	var events []realtime.Event
	changed := false

	for _, m := range members {
		if active[m.DID] {
			if present[m.DID] {
				if err := model.SetPartyPresence(ctx, s.Redis, p.SpaceID, m.DID, now); err != nil {
					return err
				}
				continue
			}
			if p.Locked {
				// locked の間は、在席しないことによる離脱を起こさない（08 §4.4）。
				continue
			}
			last, ok := pp[m.DID]
			if !ok {
				if err := model.SetPartyPresence(ctx, s.Redis, p.SpaceID, m.DID, now); err != nil {
					return err
				}
				continue
			}
			if now.Sub(last) >= core.LeaveAfter {
				if err := model.LeavePartyMember(ctx, s.DB, p.SpaceID, m.DID, now); err != nil {
					return err
				}
				active[m.DID] = false
				changed = true
				events = append(events, realtime.Event{Name: "member.left", Space: p.SpaceID, Data: map[string]string{"did": m.DID}})
			}
			continue
		}
		if present[m.DID] && !leavers[m.DID] {
			if err := spacemodel.RejoinSpaceMember(ctx, s.DB, p.SpaceID, m.DID, now); err != nil {
				return err
			}
			if err := model.SetPartyPresence(ctx, s.Redis, p.SpaceID, m.DID, now); err != nil {
				return err
			}
			active[m.DID] = true
			changed = true
			events = append(events, realtime.Event{Name: "member.joined", Space: p.SpaceID, Data: map[string]string{"did": m.DID}})
		}
	}

	activeCount := 0
	activeMembers := 0
	for did, isActive := range active {
		if isActive {
			activeMembers++
			if present[did] {
				activeCount++
			}
		}
	}

	if activeCount >= 2 {
		if err := model.TouchPartyActivity(ctx, s.DB, p.SpaceID, now); err != nil {
			return err
		}
	}

	if p.Locked {
		// locked: 72 時間の無活動、未離脱メンバーが 2 人未満、または core.MaxDuration で終了する（08 §4.2）。
		if now.Sub(p.TentativeAt) >= core.MaxDuration || activeMembers < 2 || now.Sub(p.LastActivityAt) >= core.LockedIdleEnd {
			return s.endParty(ctx, now, p, members, events)
		}
	} else if now.Sub(p.TentativeAt) >= core.MaxDuration || (activeCount < 2 && now.Sub(p.LastActivityAt) >= core.LeaveAfter) {
		return s.endParty(ctx, now, p, members, events)
	}
	if !changed {
		return nil
	}
	s.invalidateSpace(ctx, p.SpaceID)
	for _, e := range events {
		s.publish(ctx, e)
	}
	if detail, err := s.partyDetail(ctx, p.SpaceID); err == nil && detail != nil {
		s.publishPartyUpdated(ctx, detail)
	}
	return nil
}

func (s *PartyService) endParty(ctx context.Context, now time.Time, p model.Party, members []spacemodel.SpaceMember, events []realtime.Event) error {
	endedAt := now
	archivedAt := now
	err := db.Transaction(ctx, s.DB, func(tx *sql.Tx) error {
		if err := model.SetPartyPhase(ctx, tx, p.SpaceID, "ended", p.ConfirmedAt, &endedAt); err != nil {
			return err
		}
		return spacemodel.SetSpaceState(ctx, tx, p.SpaceID, "archived", &archivedAt)
	})
	if err != nil {
		return err
	}
	for _, m := range members {
		if err := model.ClearPartyOf(ctx, s.Redis, m.DID); err != nil {
			return err
		}
	}
	if err := model.DeletePartyPresence(ctx, s.Redis, p.SpaceID); err != nil {
		return err
	}
	if err := model.DeletePartyLeavers(ctx, s.Redis, p.SpaceID); err != nil {
		return err
	}
	s.invalidateSpace(ctx, p.SpaceID)
	for _, e := range events {
		s.publish(ctx, e)
	}
	s.publish(ctx, realtime.Event{Name: "space.archived", Space: p.SpaceID})
	detail := &PartyDetail{ID: p.SpaceID, Name: "", Phase: "ended", TentativeAt: p.TentativeAt, ConfirmedAt: p.ConfirmedAt, Members: []PartyMemberItem{}}
	if sp, err := spacemodel.GetSpace(ctx, s.DB, p.SpaceID); err == nil {
		detail.Name = sp.Name
	}
	s.publishPartyUpdated(ctx, detail)
	return nil
}

func (s *PartyService) publishPartyUpdated(ctx context.Context, detail *PartyDetail) {
	present := make([]string, 0, len(detail.Members))
	for _, m := range detail.Members {
		if m.Present {
			present = append(present, m.DID)
		}
	}
	sort.Strings(present)
	data := map[string]any{"phase": detail.Phase, "locked": detail.Locked, "present": present}
	s.publish(ctx, realtime.Event{Name: "party.updated", Space: detail.ID, Data: data})
	for _, m := range detail.Members {
		s.publish(ctx, realtime.Event{Name: "party.updated", User: m.DID, Data: data})
	}
}

func (s *PartyService) publish(ctx context.Context, e realtime.Event) {
	if s.Broker != nil {
		s.Broker.Publish(ctx, e)
	}
}

func (s *PartyService) invalidateSpace(ctx context.Context, spaceID string) {
	if s.Access != nil {
		_ = s.Access.InvalidateSpace(ctx, spaceID)
	}
}

func (s *PartyService) invalidateMember(ctx context.Context, spaceID, did string) {
	if s.Access != nil {
		_ = s.Access.Invalidate(ctx, spaceID, did)
	}
}

type partyEdge struct{ A, B string }

type partyInfo struct {
	SpaceID     string
	Phase       string
	TentativeAt time.Time
	Members     []string
}

type integrationPlan struct {
	Create        bool
	CreateMembers []string
	Assign        map[string][]string
	Dissolve      []string
	MergeInto     string
}

// planIntegration は 08 §4.3 の統合規則を決める純関数。since / off は成分内の安定エッジと所属 party。
func planIntegration(comp []string, parties []partyInfo, since map[string]time.Time, off map[string]string) integrationPlan {
	switch len(parties) {
	case 0:
		return integrationPlan{Create: true, CreateMembers: append([]string(nil), comp...)}
	case 1:
		return integrationPlan{Assign: map[string][]string{parties[0].SpaceID: unaffiliated(comp, off)}}
	}

	confirmed := filterPhase(parties, "confirmed")
	tentative := filterPhase(parties, "tentative")
	unaff := unaffiliated(comp, off)
	plan := integrationPlan{Assign: map[string][]string{}}

	if len(confirmed)+len(tentative) == 0 {
		return integrationPlan{Create: true, CreateMembers: append([]string(nil), comp...)}
	}
	if len(confirmed) == 0 {
		target := oldest(tentative)
		plan.MergeInto = target.SpaceID
		plan.Dissolve = spaceIDsExcept(tentative, target.SpaceID)
		plan.Assign[target.SpaceID] = unaff
		return plan
	}
	if len(tentative) == 0 {
		for _, u := range unaff {
			if t := oldestNeighborParty(u, comp, since, off); t != "" {
				plan.Assign[t] = append(plan.Assign[t], u)
			}
		}
		return plan
	}
	target := oldest(confirmed)
	plan.MergeInto = target.SpaceID
	plan.Dissolve = spaceIDs(tentative)
	plan.Assign[target.SpaceID] = unaff
	return plan
}

func filterPhase(parties []partyInfo, phase string) []partyInfo {
	out := make([]partyInfo, 0, len(parties))
	for _, p := range parties {
		if p.Phase == phase {
			out = append(out, p)
		}
	}
	return out
}

func unaffiliated(comp []string, off map[string]string) []string {
	out := make([]string, 0, len(comp))
	for _, did := range comp {
		if _, ok := off[did]; !ok {
			out = append(out, did)
		}
	}
	sort.Strings(out)
	return out
}

func oldest(parties []partyInfo) partyInfo {
	best := parties[0]
	for _, p := range parties[1:] {
		if p.TentativeAt.Before(best.TentativeAt) ||
			(p.TentativeAt.Equal(best.TentativeAt) && p.SpaceID < best.SpaceID) {
			best = p
		}
	}
	return best
}

func spaceIDs(parties []partyInfo) []string {
	out := make([]string, 0, len(parties))
	for _, p := range parties {
		out = append(out, p.SpaceID)
	}
	sort.Strings(out)
	return out
}

func spaceIDsExcept(parties []partyInfo, keep string) []string {
	out := make([]string, 0, len(parties))
	for _, p := range parties {
		if p.SpaceID != keep {
			out = append(out, p.SpaceID)
		}
	}
	sort.Strings(out)
	return out
}

func oldestNeighborParty(u string, comp []string, since map[string]time.Time, off map[string]string) string {
	best := ""
	var bestSince time.Time
	for _, v := range comp {
		if v == u {
			continue
		}
		target, ok := off[v]
		if !ok {
			continue
		}
		s, ok := since[pairKey(u, v)]
		if !ok {
			continue
		}
		if best == "" || s.Before(bestSince) {
			best, bestSince = target, s
		}
	}
	return best
}

// connectedComponents は無向グラフの連結成分を、ノードの昇順で返す純関数（08 §4.1 step2）。
func connectedComponents(edges []partyEdge) [][]string {
	adj := map[string]map[string]bool{}
	for _, e := range edges {
		if e.A == "" || e.B == "" {
			continue
		}
		if adj[e.A] == nil {
			adj[e.A] = map[string]bool{}
		}
		if adj[e.B] == nil {
			adj[e.B] = map[string]bool{}
		}
		adj[e.A][e.B] = true
		adj[e.B][e.A] = true
	}
	nodes := make([]string, 0, len(adj))
	for n := range adj {
		nodes = append(nodes, n)
	}
	sort.Strings(nodes)

	seen := map[string]bool{}
	comps := make([][]string, 0)
	for _, start := range nodes {
		if seen[start] {
			continue
		}
		comp := []string{}
		stack := []string{start}
		seen[start] = true
		for len(stack) > 0 {
			u := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			comp = append(comp, u)
			neighbors := make([]string, 0, len(adj[u]))
			for v := range adj[u] {
				neighbors = append(neighbors, v)
			}
			sort.Strings(neighbors)
			for _, v := range neighbors {
				if !seen[v] {
					seen[v] = true
					stack = append(stack, v)
				}
			}
		}
		sort.Strings(comp)
		comps = append(comps, comp)
	}
	sort.Slice(comps, func(i, j int) bool { return comps[i][0] < comps[j][0] })
	return comps
}

// partyName は「M/D HH:mm の party」を Asia/Tokyo で作る（08 §4.2）。
func partyName(t time.Time) string {
	l := t.In(core.Jst)
	return fmt.Sprintf("%d/%d %02d:%02d%s", int(l.Month()), l.Day(), l.Hour(), l.Minute(), partyTitleSuffix)
}

func pairKey(a, b string) string {
	if a > b {
		a, b = b, a
	}
	return a + "|" + b
}

func didList(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for d := range set {
		out = append(out, d)
	}
	sort.Strings(out)
	return out
}

func sortedKeys(m map[string][]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
