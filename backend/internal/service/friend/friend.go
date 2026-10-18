package friend

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/applemango/chain/internal/db"
	"github.com/applemango/chain/internal/external/atproto"
	"github.com/applemango/chain/internal/httpx"
	"github.com/applemango/chain/internal/idgen"
	"github.com/applemango/chain/internal/model"
	friendmodel "github.com/applemango/chain/internal/model/friend"
	"github.com/applemango/chain/internal/realtime"
	"github.com/applemango/chain/internal/service/core"
	"github.com/applemango/chain/internal/service/notify"
)

// FriendService はフレンド（Bluesky の相互フォロー）の申請・承認・取消、招待コード、一覧、削除（07 §2）。
// 申請はフォローを書くこと、承認はフォローを返すことで、申請の表は持たない。
type FriendService struct {
	core.Deps
	Broker realtime.Broker
	// Writer はユーザーの PDS に app.bsky.graph.follow を書く・消す。
	Writer core.RecordWriter
	// Lister は PDS の follow / block を listRecords で読む（認証なし）。nil なら、削除は索引にある record だけを消し、
	// ログイン時の読み直しはしない。
	Lister RecordLister
	// Recon は nil なら Deps と Broker から作る。
	Recon *Reconciler
}

// RecordLister は PDS の公開 record を collection ごとに読む。実装は *atproto.FirstLoadFetcher。
type RecordLister interface {
	ListRecords(ctx context.Context, pdsURL, did, collection string) ([]atproto.Record, error)
}

const (
	// MaxFriends は会ったフレンド数の上限（07 §1）。
	MaxFriends = 500
	// InviteTTL は招待コードの有効期限（07 §2.1）。
	InviteTTL = 10 * time.Minute

	inviteCodeLength = 8
	// crockfordAlphabet は Crockford Base32。I / L / O / U を含まない。
	crockfordAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
	friendInviteURL   = "chain://friend/"
)

// FriendItem は GET /friends の 1 件（07 §2.3）。
type FriendItem struct {
	core.UserSummary
	Since time.Time `json:"since"`
	// Precision は自分が相手に設定している共有精度。会ったフレンドでなければ off。
	Precision string `json:"precision"`
	// Met は会ったフレンドか（位置共有・beacon・Party が使える。07 §1）。
	Met bool `json:"met"`
	// Via は会った裏付けの経路（encounter / invite / space）。無ければ nil。
	Via *string `json:"via"`
}

// FriendRequestItem は申請 1 件（07 §2.2）。ID は follows.id。
type FriendRequestItem struct {
	ID        string           `json:"id"`
	Peer      core.UserSummary `json:"peer"`
	Via       string           `json:"via"`
	ViaRef    string           `json:"viaRef,omitempty"`
	Status    string           `json:"status"`
	CreatedAt time.Time        `json:"createdAt"`
}

// InviteResult は POST /friends/invites の応答（07 §2.1）。
type InviteResult struct {
	Code      string
	URL       string
	ExpiresAt time.Time
}

// CreateRequestInput は POST /friends/requests の入力（07 §2.2）。
type CreateRequestInput struct {
	DID    string
	Via    string
	ViaRef string
}

// friendNotificationPayload は notifications.payload_json の形（12 §3）。
type friendNotificationPayload struct {
	Text      string           `json:"text"`
	Actor     core.UserSummary `json:"actor"`
	RequestID string           `json:"requestId,omitempty"`
}

func (s *FriendService) reconciler() *Reconciler {
	if s.Recon != nil {
		return s.Recon
	}
	return &Reconciler{Deps: s.Deps, Broker: s.Broker}
}

// CreateInvite は 8 文字の招待コードを作り、それまでの未使用コードを失効させる（07 §2.1）。
// コードを作ることは、読み取った相手をフォローすることへの同意とみなす。
func (s *FriendService) CreateInvite(ctx context.Context, did string) (*InviteResult, error) {
	now := s.Clock.Now()
	expiresAt := now.Add(InviteTTL)

	var code string
	err := db.Transaction(ctx, s.DB, func(tx *sql.Tx) error {
		if err := friendmodel.DeleteUnusedFriendInvites(ctx, tx, did); err != nil {
			return err
		}
		// 40 ビットなので衝突は稀だが、起きたら別のコードで作り直す。
		for attempt := 0; attempt < 3; attempt++ {
			candidate := newInviteCode(s.IDs)
			created, err := friendmodel.CreateFriendInvite(ctx, tx, friendmodel.FriendInvite{
				CodeHash: hashInviteCode(candidate), DID: did, CreatedAt: now, ExpiresAt: expiresAt,
			})
			if err != nil {
				return err
			}
			if created {
				code = candidate
				return nil
			}
		}
		return httpx.ErrConflict.With("reason", "招待コードを作れませんでした。もう一度お試しください")
	})
	if err != nil {
		return nil, err
	}
	return &InviteResult{Code: displayInviteCode(code), URL: friendInviteURL + code, ExpiresAt: expiresAt}, nil
}

// AcceptInvite は招待コードを使う（07 §2.1）。読み取った人のフォローをその人のセッションで、作成者のフォローを
// 作成者のセッションで書く。作成者のセッションが使えなければ follow_pending にして次のログインで書く。
// 会ったフレンドになるのは、相互フォローがそろった時。
func (s *FriendService) AcceptInvite(ctx context.Context, me, sid, rawCode string) (*FriendItem, error) {
	code, ok := normalizeInviteCode(rawCode)
	if !ok {
		return nil, httpx.ErrGone
	}
	now := s.Clock.Now()
	codeHash := hashInviteCode(code)

	inv, err := friendmodel.GetFriendInvite(ctx, s.DB, codeHash)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, httpx.ErrGone
	}
	if err != nil {
		return nil, err
	}
	if inv.DID == me {
		return nil, httpx.ErrInvalidRequest.With("fields", map[string]string{"code": "自分のコードは使えません"})
	}
	if inv.UsedBy != "" || !now.Before(inv.ExpiresAt) {
		return nil, httpx.ErrGone
	}
	creator, err := model.GetUser(ctx, s.DB, inv.DID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, httpx.ErrGone
	}
	if err != nil {
		return nil, err
	}
	if creator.DeletedAt != nil {
		return nil, httpx.ErrGone
	}
	blocked, err := model.IsBlocked(ctx, s.DB, me, inv.DID)
	if err != nil {
		return nil, err
	}
	if blocked {
		return nil, httpx.ErrBlocked
	}
	if _, err := friendmodel.GetFriendship(ctx, s.DB, me, inv.DID); err == nil {
		return nil, httpx.ErrConflict.With("reason", "既にフレンドです")
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if err := s.checkFriendCap(ctx, s.DB, me, inv.DID); err != nil {
		return nil, err
	}

	// PDS への書き込みは、招待を使い切る前に行う（失敗したら招待は未使用のまま残り、やり直せる）。
	myURI, err := s.followAs(ctx, me, sid, inv.DID)
	if err != nil {
		return nil, err
	}
	creatorURI, creatorPending := s.followAsCreator(ctx, creator, me)

	var effects followEffects
	var friend *FriendItem
	err = db.Transaction(ctx, s.DB, func(tx *sql.Tx) error {
		used, err := friendmodel.UseFriendInvite(ctx, tx, codeHash, me, now)
		if err != nil {
			return err
		}
		if !used {
			return httpx.ErrGone
		}
		if creatorPending {
			if err := friendmodel.SetFriendInviteFollowPending(ctx, tx, codeHash, true); err != nil {
				return err
			}
		}
		for _, f := range []struct{ author, subject, uri string }{
			{me, inv.DID, myURI}, {inv.DID, me, creatorURI},
		} {
			if f.uri == "" {
				continue
			}
			if _, err := s.indexFollowN(ctx, tx, f.author, f.subject, f.uri, now, false, &effects); err != nil {
				return err
			}
		}
		if _, err := s.reconcileInto(ctx, tx, me, inv.DID, &effects); err != nil {
			return err
		}
		// 作成者には、読み取られたことを知らせる（相互フォローがまだでも）。
		acceptor, err := core.FetchUserSummary(ctx, tx, me)
		if err != nil {
			return err
		}
		if err := s.notify(ctx, tx, inv.DID, notify.NotificationFriendAccepted, friendNotificationPayload{
			Text:  friendAcceptedText(*acceptor),
			Actor: *acceptor,
		}, now, &effects.notes); err != nil {
			return err
		}
		effects.events = append(effects.events,
			realtime.Event{Name: notify.NotificationFriendAccepted, User: inv.DID, Data: acceptor})
		friend, err = s.friendItem(ctx, tx, me, inv.DID, now)
		return err
	})
	if err != nil {
		return nil, err
	}
	s.publish(ctx, effects.events, effects.notes)
	return friend, nil
}

// followAsCreator は招待の作成者のフォローを、作成者の ATProto セッションで書く。
// セッションが無い・失効・scope 不足のときは pending を返し、次のログインで書く（07 §2.1）。
func (s *FriendService) followAsCreator(ctx context.Context, creator *model.User, acceptor string) (uri string, pending bool) {
	if has, err := friendmodel.HasFollow(ctx, s.DB, creator.DID, acceptor); err == nil && has {
		return "", false
	}
	atsess, err := friendmodel.LatestATProtoSessionID(ctx, s.DB, creator.DID)
	if err != nil || atsess == "" {
		return "", true
	}
	uri, err = s.writeFollow(ctx, creator.DID, atsess, acceptor)
	if err != nil {
		slog.WarnContext(ctx, "invite: creator follow deferred to next login", "did", creator.DID, "err", err)
		return "", true
	}
	return uri, false
}

// CreateRequest は申請 = 相手を Bluesky でフォローすることで、自分のセッションで follow を書く（07 §2.2）。
func (s *FriendService) CreateRequest(ctx context.Context, me, sid string, in CreateRequestInput) (*FriendRequestItem, error) {
	if in.DID == me {
		return nil, httpx.ErrInvalidRequest.With("fields", map[string]string{"did": "自分には申請できません"})
	}
	if in.Via != "encounter" && in.Via != "space" {
		return nil, httpx.ErrInvalidRequest.With("fields", map[string]string{"via": "encounter / space のいずれかです"})
	}
	if in.ViaRef == "" {
		return nil, httpx.ErrInvalidRequest.With("fields", map[string]string{"viaRef": "必須です"})
	}

	target, err := model.GetUser(ctx, s.DB, in.DID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, httpx.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if target.DeletedAt != nil {
		return nil, httpx.ErrNotFound
	}
	blocked, err := model.IsBlocked(ctx, s.DB, me, in.DID)
	if err != nil {
		return nil, err
	}
	if blocked {
		return nil, httpx.ErrBlocked
	}
	if _, err := friendmodel.GetFriendship(ctx, s.DB, me, in.DID); err == nil {
		return nil, httpx.ErrConflict.With("reason", "既にフレンドです")
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if mine, err := friendmodel.HasFollow(ctx, s.DB, me, in.DID); err != nil {
		return nil, err
	} else if mine {
		return nil, httpx.ErrConflict.With("reason", "既に申請中です")
	}
	ok, err := s.canRequestVia(ctx, me, in.DID, in.Via, in.ViaRef)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, httpx.ErrForbidden
	}

	now := s.Clock.Now()
	uri, err := s.followAs(ctx, me, sid, in.DID)
	if err != nil {
		return nil, err
	}
	var effects followEffects
	var follow *friendmodel.Follow
	err = db.Transaction(ctx, s.DB, func(tx *sql.Tx) error {
		follow, err = s.indexFollow(ctx, tx, me, in.DID, uri, now, &effects)
		return err
	})
	if err != nil {
		return nil, err
	}
	s.publish(ctx, effects.events, effects.notes)
	summary, err := core.FetchUserSummary(ctx, s.DB, in.DID)
	if err != nil {
		return nil, err
	}
	status := "pending"
	if effects.mutual {
		status = "accepted"
	}
	return &FriendRequestItem{
		ID: follow.ID, Peer: *summary, Via: in.Via, ViaRef: in.ViaRef, Status: status, CreatedAt: follow.CreatedAt,
	}, nil
}

// Follow はプロフィールから相手を Bluesky でフォローする。プロフィールが見える相手（13 §1.2）だけに書ける。
// 書いたフォローは申請と同じ扱いで、相手からのフォローがあればフレンドになる（07 §2.2）。
func (s *FriendService) Follow(ctx context.Context, me, sid, peer string) error {
	if peer == me {
		return httpx.ErrInvalidRequest.With("fields", map[string]string{"did": "自分はフォローできません"})
	}
	target, err := model.GetUser(ctx, s.DB, peer)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && target.DeletedAt != nil) {
		return httpx.ErrNotFound
	}
	if err != nil {
		return err
	}
	blocked, err := model.IsBlocked(ctx, s.DB, me, peer)
	if err != nil {
		return err
	}
	if blocked {
		return httpx.ErrBlocked
	}
	if related, err := model.HasRelationship(ctx, s.DB, me, peer); err != nil {
		return err
	} else if !related {
		return httpx.ErrNotFound
	}

	uri, err := s.followAs(ctx, me, sid, peer)
	if err != nil {
		return err
	}
	if uri == "" {
		// 既にフォローしている。
		return nil
	}
	var effects followEffects
	err = db.Transaction(ctx, s.DB, func(tx *sql.Tx) error {
		_, err := s.indexFollow(ctx, tx, me, peer, uri, s.Clock.Now(), &effects)
		return err
	})
	if err != nil {
		return err
	}
	s.publish(ctx, effects.events, effects.notes)
	return nil
}

// AcceptRequest は受信者として申請を承認する = フォローを返す（07 §2.2）。{id} は follows.id。
func (s *FriendService) AcceptRequest(ctx context.Context, me, sid, id string) (*FriendItem, error) {
	f, err := friendmodel.GetFollowByID(ctx, s.DB, id)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && f.Subject != me) {
		return nil, httpx.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	blocked, err := model.IsBlocked(ctx, s.DB, me, f.Author)
	if err != nil {
		return nil, err
	}
	if blocked {
		return nil, httpx.ErrBlocked
	}
	if mine, err := friendmodel.HasFollow(ctx, s.DB, me, f.Author); err != nil {
		return nil, err
	} else if mine {
		return nil, httpx.ErrConflict.With("reason", "この申請は既に処理されています")
	}
	if via, _, err := s.reconciler().Evidence(ctx, s.DB, me, f.Author); err != nil {
		return nil, err
	} else if via == "" {
		return nil, httpx.ErrForbidden
	}

	now := s.Clock.Now()
	uri, err := s.followAs(ctx, me, sid, f.Author)
	if err != nil {
		return nil, err
	}
	var effects followEffects
	var friend *FriendItem
	err = db.Transaction(ctx, s.DB, func(tx *sql.Tx) error {
		if err := friendmodel.DeleteDismissal(ctx, tx, me, f.Author); err != nil {
			return err
		}
		if _, err := s.indexFollow(ctx, tx, me, f.Author, uri, now, &effects); err != nil {
			return err
		}
		friend, err = s.friendItem(ctx, tx, me, f.Author, now)
		return err
	})
	if err != nil {
		return nil, err
	}
	s.publish(ctx, effects.events, effects.notes)
	return friend, nil
}

// DeclineRequest は受信者として申請を拒否する。受信箱に出さなくするだけで、Bluesky には何も書かない（07 §2.2）。
func (s *FriendService) DeclineRequest(ctx context.Context, me, id string) error {
	f, err := friendmodel.GetFollowByID(ctx, s.DB, id)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && f.Subject != me) {
		return httpx.ErrNotFound
	}
	if err != nil {
		return err
	}
	return friendmodel.CreateDismissal(ctx, s.DB, me, f.Author, s.Clock.Now())
}

// CancelRequest は送信者として申請を取り消す = 自分のフォローを消す（07 §2.2）。
func (s *FriendService) CancelRequest(ctx context.Context, me, sid, id string) error {
	f, err := friendmodel.GetFollowByID(ctx, s.DB, id)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && f.Author != me) {
		return httpx.ErrNotFound
	}
	if err != nil {
		return err
	}
	if mutual, err := friendmodel.HasFollow(ctx, s.DB, f.Subject, me); err != nil {
		return err
	} else if mutual {
		return httpx.ErrConflict.With("reason", "既にフレンドです。フレンドの削除から外してください")
	}
	events, err := s.unfollow(ctx, me, sid, f.Subject)
	if err != nil {
		return err
	}
	s.publish(ctx, events, nil)
	return nil
}

// Delete はフレンド（相互フォロー）を外す = 自分のフォローを消す。Bluesky でもフォローが外れる（07 §2.3）。
// 相手には通知しない。会ったフレンドだった場合は位置共有も消える（07 §2.4）。
func (s *FriendService) Delete(ctx context.Context, me, sid, peer string) error {
	if peer == me {
		return httpx.ErrNotFound
	}
	if mine, err := friendmodel.HasFollow(ctx, s.DB, me, peer); err != nil {
		return err
	} else if !mine {
		return httpx.ErrNotFound
	}
	events, err := s.unfollow(ctx, me, sid, peer)
	if err != nil {
		return err
	}
	s.publish(ctx, events, nil)
	return nil
}

// unfollow は me の subject へのフォローを PDS と索引から消し、組を確かめ直す。コミット後に配るイベントを返す。
func (s *FriendService) unfollow(ctx context.Context, me, sid, subject string) ([]realtime.Event, error) {
	atsess, err := s.atprotoSession(ctx, sid)
	if err != nil {
		return nil, err
	}
	if err := s.deleteFollowRecords(ctx, me, atsess, subject); err != nil {
		return nil, err
	}
	var effects followEffects
	err = db.Transaction(ctx, s.DB, func(tx *sql.Tx) error {
		return s.removeFollow(ctx, tx, me, subject, &effects)
	})
	if err != nil {
		return nil, err
	}
	if len(effects.events) == 0 {
		// 会ったフレンドでなくても、一覧を更新させる。
		for _, pair := range [][2]string{{me, subject}, {subject, me}} {
			summary, err := core.FetchUserSummary(ctx, s.DB, pair[1])
			if err != nil {
				return nil, err
			}
			effects.events = append(effects.events, realtime.Event{Name: "friend.changed", User: pair[0], Data: summary})
		}
	}
	return effects.events, nil
}

// List は自分のフレンド（相互フォロー）の一覧を新しい順に返す（07 §2.3）。
func (s *FriendService) List(ctx context.Context, me string) ([]FriendItem, error) {
	rows, err := friendmodel.ListMutualFollows(ctx, s.DB, me)
	if err != nil {
		return nil, err
	}
	items := make([]FriendItem, 0, len(rows))
	for _, m := range rows {
		item, err := s.friendItem(ctx, s.DB, me, m.DID, m.Since)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, nil
}

// friendItem は me から見た peer の一覧の 1 件を作る。Since は since を使う（会ったフレンドなら friendships の時刻）。
func (s *FriendService) friendItem(ctx context.Context, q db.Querier, me, peer string, since time.Time) (*FriendItem, error) {
	summary, err := core.FetchUserSummary(ctx, q, peer)
	if err != nil {
		return nil, err
	}
	item := &FriendItem{UserSummary: *summary, Since: since, Precision: PrecisionOff}
	fr, err := friendmodel.GetFriendship(ctx, q, me, peer)
	switch {
	case err == nil:
		item.Met = true
		via := fr.Via
		item.Via = &via
		item.Precision, err = friendmodel.GetLocationPrecision(ctx, q, me, peer)
		if err != nil {
			return nil, err
		}
	case !errors.Is(err, sql.ErrNoRows):
		return nil, err
	}
	return item, nil
}

// ListRequests は受信箱 / 送信箱を返す。box は incoming / outgoing（07 §2.2）。
// どちらも §2 の関係（会った裏付けになりうる関係）がある相手だけ。
func (s *FriendService) ListRequests(ctx context.Context, me, box string) ([]FriendRequestItem, error) {
	if box != "incoming" && box != "outgoing" {
		return nil, httpx.ErrInvalidRequest.With("fields", map[string]string{"box": "incoming / outgoing のいずれかです"})
	}
	incoming := box == "incoming"
	var rows []friendmodel.Follow
	var err error
	if incoming {
		rows, err = friendmodel.ListIncomingFollows(ctx, s.DB, me)
	} else {
		rows, err = friendmodel.ListOutgoingFollows(ctx, s.DB, me)
	}
	if err != nil {
		return nil, err
	}
	items := make([]FriendRequestItem, 0, len(rows))
	for _, f := range rows {
		peerDID := f.Author
		if !incoming {
			peerDID = f.Subject
		}
		via, ref, err := s.reconciler().Evidence(ctx, s.DB, me, peerDID)
		if err != nil {
			return nil, err
		}
		if via == "" {
			continue
		}
		summary, err := core.FetchUserSummary(ctx, s.DB, peerDID)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		items = append(items, FriendRequestItem{
			ID: f.ID, Peer: *summary, Via: via, ViaRef: ref, Status: "pending", CreatedAt: f.CreatedAt,
		})
	}
	return items, nil
}

func (s *FriendService) canRequestVia(ctx context.Context, me, peer, via, viaRef string) (bool, error) {
	switch via {
	case "encounter":
		return friendmodel.HasRevealedEncounter(ctx, s.DB, viaRef, me, peer)
	case "space":
		return friendmodel.SharedSpace(ctx, s.DB, viaRef, me, peer)
	default:
		return false, nil
	}
}

func (s *FriendService) checkFriendCap(ctx context.Context, q db.Querier, dids ...string) error {
	for _, did := range dids {
		n, err := friendmodel.CountFriends(ctx, q, did)
		if err != nil {
			return err
		}
		if n >= MaxFriends {
			return httpx.ErrConflict.With("reason", "会ったフレンド数の上限（500 人）に達しています")
		}
	}
	return nil
}

// notify は通知を積み、コミット後に送る分を notes に足す。off なら何もしない。
func (s *FriendService) notify(ctx context.Context, q db.Querier, did, typ string, payload any, now time.Time, notes *[]notify.NotificationToSend) error {
	item, err := notify.EnqueueNotification(ctx, q, s.Deps, did, typ, payload, now)
	if err != nil {
		return err
	}
	if item != nil {
		*notes = append(*notes, notify.NotificationToSend{DID: did, Item: *item})
	}
	return nil
}

// publish はコミット後のイベントと通知を配る（10 §7.3）。
func (s *FriendService) publish(ctx context.Context, events []realtime.Event, notes []notify.NotificationToSend) {
	if s.Broker == nil {
		return
	}
	for _, e := range events {
		s.Broker.Publish(ctx, e)
	}
	for _, n := range notes {
		s.Broker.Publish(ctx, realtime.Event{Name: "notification", User: n.DID, Data: n.Item})
	}
}

func friendRequestText(actor core.UserSummary) string {
	return summaryName(actor) + " さんからフレンド申請"
}

func friendAcceptedText(actor core.UserSummary) string {
	return summaryName(actor) + " さんとフレンドになりました"
}

func summaryName(s core.UserSummary) string {
	if s.DisplayName != "" {
		return s.DisplayName
	}
	if s.Handle != "" {
		return s.Handle
	}
	return s.DID
}

// newInviteCode は 40 ビットの乱数を Crockford Base32 の 8 文字にする（07 §2.1）。
func newInviteCode(ids idgen.Generator) string {
	raw := ids.Bytes(5)
	out := make([]byte, inviteCodeLength)
	for i := 0; i < inviteCodeLength; i++ {
		bit := i * 5
		var v byte
		for j := 0; j < 5; j++ {
			p := bit + j
			shift := uint(7 - p%8)
			v = (v << 1) | ((raw[p/8] >> shift) & 1)
		}
		out[i] = crockfordAlphabet[v&0x1f]
	}
	return string(out)
}

// normalizeInviteCode は大文字小文字・ハイフン・空白を無視し、Crockford の
// 紛らわしい文字（I / L → 1、O → 0）を畳んで 8 文字のコードにする。
func normalizeInviteCode(raw string) (string, bool) {
	var b strings.Builder
	b.Grow(inviteCodeLength)
	for _, r := range strings.ToUpper(strings.TrimSpace(raw)) {
		switch r {
		case '-', ' ', '\t', '\n', '\r':
			continue
		case 'I', 'L':
			r = '1'
		case 'O':
			r = '0'
		}
		if !strings.ContainsRune(crockfordAlphabet, r) {
			return "", false
		}
		b.WriteRune(r)
	}
	code := b.String()
	if len(code) != inviteCodeLength {
		return "", false
	}
	return code, true
}

func hashInviteCode(code string) string {
	sum := sha256.Sum256([]byte(code))
	return hex.EncodeToString(sum[:])
}

func displayInviteCode(code string) string {
	if len(code) != inviteCodeLength {
		return code
	}
	return code[:4] + "-" + code[4:]
}
