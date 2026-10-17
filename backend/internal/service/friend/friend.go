package friend

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/applemango/chain/internal/db"
	"github.com/applemango/chain/internal/httpx"
	"github.com/applemango/chain/internal/idgen"
	"github.com/applemango/chain/internal/model"
	friendmodel "github.com/applemango/chain/internal/model/friend"
	"github.com/applemango/chain/internal/realtime"
	"github.com/applemango/chain/internal/service/core"
	"github.com/applemango/chain/internal/service/notify"
)

// FriendService はフレンドになる 3 経路（招待コード・すれ違い・Space）と、
// 申請の承認・拒否・取消、一覧、削除（07 §2）。
type FriendService struct {
	core.Deps
	Broker realtime.Broker
}

const (
	// MaxFriends はフレンド数の上限（07 §1）。
	MaxFriends = 500
	// InviteTTL は招待コードの有効期限（07 §2.1）。
	InviteTTL = 10 * time.Minute
	// FriendRequestCooldown は declined のあと同じ相手に再申請できない期間（07 §2.2）。
	FriendRequestCooldown = 7 * 24 * time.Hour

	inviteCodeLength = 8
	// crockfordAlphabet は Crockford Base32。I / L / O / U を含まない。
	crockfordAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
	friendInviteURL   = "chain://friend/"
)

// FriendItem は GET /friends の 1 件（07 §2.3）。
type FriendItem struct {
	core.UserSummary
	Since     time.Time `json:"since"`
	Precision string    `json:"precision"`
}

// FriendRequestItem は申請 1 件（07 §2.2）。
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

// CreateInvite は 8 文字の招待コードを作り、それまでの未使用コードを失効させる（07 §2.1）。
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

// AcceptInvite は招待コードを使い、作成者とフレンドになる（07 §2.1）。
func (s *FriendService) AcceptInvite(ctx context.Context, me, rawCode string) (*FriendItem, error) {
	code, ok := normalizeInviteCode(rawCode)
	if !ok {
		return nil, httpx.ErrGone
	}
	now := s.Clock.Now()

	var friend *FriendItem
	var events []realtime.Event
	var notes []notify.NotificationToSend

	err := db.Transaction(ctx, s.DB, func(tx *sql.Tx) error {
		inv, err := friendmodel.GetFriendInvite(ctx, tx, hashInviteCode(code))
		if errors.Is(err, sql.ErrNoRows) {
			return httpx.ErrGone
		}
		if err != nil {
			return err
		}
		if inv.DID == me {
			return httpx.ErrInvalidRequest.With("fields", map[string]string{"code": "自分のコードは使えません"})
		}
		if inv.UsedBy != "" || !now.Before(inv.ExpiresAt) {
			return httpx.ErrGone
		}

		creator, err := model.GetUser(ctx, tx, inv.DID)
		if errors.Is(err, sql.ErrNoRows) {
			return httpx.ErrGone
		}
		if err != nil {
			return err
		}
		if creator.DeletedAt != nil {
			return httpx.ErrGone
		}

		blocked, err := model.IsBlocked(ctx, tx, me, inv.DID)
		if err != nil {
			return err
		}
		if blocked {
			return httpx.ErrBlocked
		}
		if _, err := friendmodel.GetFriendship(ctx, tx, me, inv.DID); err == nil {
			return httpx.ErrConflict.With("reason", "既にフレンドです")
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err := s.checkFriendCap(ctx, tx, me, inv.DID); err != nil {
			return err
		}

		used, err := friendmodel.UseFriendInvite(ctx, tx, inv.CodeHash, me, now)
		if err != nil {
			return err
		}
		if !used {
			return httpx.ErrGone
		}

		a, b := core.OrderDIDs(me, inv.DID)
		if err := friendmodel.CreateFriendship(ctx, tx, a, b, "invite", now); err != nil {
			return err
		}
		if err := friendmodel.AcceptPendingFriendRequestsBetween(ctx, tx, me, inv.DID, now); err != nil {
			return err
		}

		creatorSummary, err := core.FetchUserSummary(ctx, tx, inv.DID)
		if err != nil {
			return err
		}
		acceptorSummary, err := core.FetchUserSummary(ctx, tx, me)
		if err != nil {
			return err
		}
		if err := s.notify(ctx, tx, inv.DID, notify.NotificationFriendAccepted, friendNotificationPayload{
			Text:  friendAcceptedText(*acceptorSummary),
			Actor: *acceptorSummary,
		}, now, &notes); err != nil {
			return err
		}
		events = append(events,
			realtime.Event{Name: notify.NotificationFriendAccepted, User: inv.DID, Data: acceptorSummary},
			realtime.Event{Name: notify.NotificationFriendAccepted, User: me, Data: creatorSummary},
		)

		precision, err := friendmodel.GetLocationPrecision(ctx, tx, me, inv.DID)
		if err != nil {
			return err
		}
		friend = &FriendItem{UserSummary: *creatorSummary, Since: now, Precision: precision}
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.publish(ctx, events, notes)
	return friend, nil
}

// CreateRequest はフレンド申請を送る。逆向きの pending があればその申請を承認する（07 §2.2）。
func (s *FriendService) CreateRequest(ctx context.Context, me string, in CreateRequestInput) (*FriendRequestItem, error) {
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

	// 相手から自分への pending があれば、新しい申請は作らずにそれを承認する。
	reverse, err := friendmodel.GetPendingFriendRequest(ctx, s.DB, in.DID, me)
	switch {
	case err == nil:
		friend, req, err := s.acceptRequest(ctx, reverse.ID, me)
		if err != nil {
			return nil, err
		}
		item := friendRequestItem(*req, friend.UserSummary, "accepted")
		return &item, nil
	case !errors.Is(err, sql.ErrNoRows):
		return nil, err
	}

	if _, err := friendmodel.GetPendingFriendRequest(ctx, s.DB, me, in.DID); err == nil {
		return nil, httpx.ErrConflict.With("reason", "既に申請中です")
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}

	now := s.Clock.Now()
	recent, err := friendmodel.HasRecentDecline(ctx, s.DB, me, in.DID, now.Add(-FriendRequestCooldown))
	if err != nil {
		return nil, err
	}
	if recent {
		return nil, httpx.ErrConflict.With("reason", "拒否されてから 7 日間は同じ相手に申請できません")
	}

	ok, err := s.canRequestVia(ctx, me, in.DID, in.Via, in.ViaRef)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, httpx.ErrForbidden
	}

	req := friendmodel.FriendRequest{
		ID: s.IDs.NewID(), FromDID: me, ToDID: in.DID,
		Via: in.Via, ViaRef: in.ViaRef, Status: "pending", CreatedAt: now,
	}
	var events []realtime.Event
	var notes []notify.NotificationToSend
	err = db.Transaction(ctx, s.DB, func(tx *sql.Tx) error {
		if err := friendmodel.CreateFriendRequest(ctx, tx, req); err != nil {
			return err
		}
		senderSummary, err := core.FetchUserSummary(ctx, tx, me)
		if err != nil {
			return err
		}
		recipientSummary, err := core.FetchUserSummary(ctx, tx, in.DID)
		if err != nil {
			return err
		}
		if err := s.notify(ctx, tx, in.DID, notify.NotificationFriendRequest, friendNotificationPayload{
			Text:      friendRequestText(*senderSummary),
			Actor:     *senderSummary,
			RequestID: req.ID,
		}, now, &notes); err != nil {
			return err
		}
		events = append(events,
			realtime.Event{Name: notify.NotificationFriendRequest, User: in.DID, Data: senderSummary},
			realtime.Event{Name: "friend.changed", User: me, Data: recipientSummary},
		)
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.publish(ctx, events, notes)
	summary, err := core.FetchUserSummary(ctx, s.DB, in.DID)
	if err != nil {
		return nil, err
	}
	item := friendRequestItem(req, *summary, "pending")
	return &item, nil
}

// AcceptRequest は受信者として申請を承認し、フレンドを作る（07 §2.2）。
func (s *FriendService) AcceptRequest(ctx context.Context, me, id string) (*FriendItem, error) {
	friend, _, err := s.acceptRequest(ctx, id, me)
	return friend, err
}

// acceptRequest は承認の本体。返すのは新しいフレンドと、承認した申請。
func (s *FriendService) acceptRequest(ctx context.Context, id, me string) (*FriendItem, *friendmodel.FriendRequest, error) {
	now := s.Clock.Now()
	var friend *FriendItem
	var accepted *friendmodel.FriendRequest
	var events []realtime.Event
	var notes []notify.NotificationToSend

	err := db.Transaction(ctx, s.DB, func(tx *sql.Tx) error {
		req, err := friendmodel.GetFriendRequest(ctx, tx, id)
		if errors.Is(err, sql.ErrNoRows) {
			return httpx.ErrNotFound
		}
		if err != nil {
			return err
		}
		if req.ToDID != me {
			return httpx.ErrNotFound
		}
		if req.Status != "pending" {
			return httpx.ErrConflict.With("reason", "この申請は既に処理されています")
		}
		if _, err := friendmodel.GetFriendship(ctx, tx, req.FromDID, req.ToDID); err == nil {
			return httpx.ErrConflict.With("reason", "既にフレンドです")
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err := s.checkFriendCap(ctx, tx, req.FromDID, req.ToDID); err != nil {
			return err
		}

		decided, err := friendmodel.DecideFriendRequest(ctx, tx, id, "accepted", now)
		if err != nil {
			return err
		}
		if !decided {
			return httpx.ErrConflict.With("reason", "この申請は既に処理されています")
		}
		a, b := core.OrderDIDs(req.FromDID, req.ToDID)
		if err := friendmodel.CreateFriendship(ctx, tx, a, b, req.Via, now); err != nil {
			return err
		}

		accepterSummary, err := core.FetchUserSummary(ctx, tx, me)
		if err != nil {
			return err
		}
		requesterSummary, err := core.FetchUserSummary(ctx, tx, req.FromDID)
		if err != nil {
			return err
		}
		if err := s.notify(ctx, tx, req.FromDID, notify.NotificationFriendAccepted, friendNotificationPayload{
			Text:  friendAcceptedText(*accepterSummary),
			Actor: *accepterSummary,
		}, now, &notes); err != nil {
			return err
		}
		events = append(events,
			realtime.Event{Name: notify.NotificationFriendAccepted, User: req.FromDID, Data: accepterSummary},
			realtime.Event{Name: notify.NotificationFriendAccepted, User: me, Data: requesterSummary},
		)

		precision, err := friendmodel.GetLocationPrecision(ctx, tx, me, req.FromDID)
		if err != nil {
			return err
		}
		friend = &FriendItem{UserSummary: *requesterSummary, Since: now, Precision: precision}
		cp := *req
		cp.Status = "accepted"
		cp.DecidedAt = &now
		accepted = &cp
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	s.publish(ctx, events, notes)
	return friend, accepted, nil
}

// DeclineRequest は受信者として申請を拒否する（07 §2.2）。
func (s *FriendService) DeclineRequest(ctx context.Context, me, id string) error {
	now := s.Clock.Now()
	var events []realtime.Event
	err := db.Transaction(ctx, s.DB, func(tx *sql.Tx) error {
		req, err := friendmodel.GetFriendRequest(ctx, tx, id)
		if errors.Is(err, sql.ErrNoRows) {
			return httpx.ErrNotFound
		}
		if err != nil {
			return err
		}
		if req.ToDID != me {
			return httpx.ErrNotFound
		}
		decided, err := friendmodel.DecideFriendRequest(ctx, tx, id, "declined", now)
		if err != nil {
			return err
		}
		if !decided {
			return httpx.ErrConflict.With("reason", "この申請は既に処理されています")
		}
		summary, err := core.FetchUserSummary(ctx, tx, me)
		if err != nil {
			return err
		}
		events = append(events, realtime.Event{Name: "friend.changed", User: req.FromDID, Data: summary})
		return nil
	})
	if err != nil {
		return err
	}
	s.publish(ctx, events, nil)
	return nil
}

// CancelRequest は送信者として申請を取り消す（07 §2.2）。
func (s *FriendService) CancelRequest(ctx context.Context, me, id string) error {
	now := s.Clock.Now()
	var events []realtime.Event
	err := db.Transaction(ctx, s.DB, func(tx *sql.Tx) error {
		req, err := friendmodel.GetFriendRequest(ctx, tx, id)
		if errors.Is(err, sql.ErrNoRows) {
			return httpx.ErrNotFound
		}
		if err != nil {
			return err
		}
		if req.FromDID != me {
			return httpx.ErrNotFound
		}
		canceled, err := friendmodel.DecideFriendRequest(ctx, tx, id, "canceled", now)
		if err != nil {
			return err
		}
		if !canceled {
			return httpx.ErrConflict.With("reason", "この申請は既に処理されています")
		}
		summary, err := core.FetchUserSummary(ctx, tx, me)
		if err != nil {
			return err
		}
		events = append(events, realtime.Event{Name: "friend.changed", User: req.ToDID, Data: summary})
		return nil
	})
	if err != nil {
		return err
	}
	s.publish(ctx, events, nil)
	return nil
}

// List は自分のフレンド一覧を新しい順に返す（07 §2.3）。
func (s *FriendService) List(ctx context.Context, me string) ([]FriendItem, error) {
	rows, err := friendmodel.ListFriendships(ctx, s.DB, me)
	if err != nil {
		return nil, err
	}
	items := make([]FriendItem, 0, len(rows))
	for _, f := range rows {
		peerDID := f.UserA
		if peerDID == me {
			peerDID = f.UserB
		}
		u, err := model.GetUser(ctx, s.DB, peerDID)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if u.DeletedAt != nil {
			continue
		}
		summary, err := core.FetchUserSummary(ctx, s.DB, peerDID)
		if err != nil {
			return nil, err
		}
		precision, err := friendmodel.GetLocationPrecision(ctx, s.DB, me, peerDID)
		if err != nil {
			return nil, err
		}
		items = append(items, FriendItem{UserSummary: *summary, Since: f.CreatedAt, Precision: precision})
	}
	return items, nil
}

// ListRequests は pending な申請を返す。box は incoming / outgoing（07 §2.3）。
func (s *FriendService) ListRequests(ctx context.Context, me, box string) ([]FriendRequestItem, error) {
	if box != "incoming" && box != "outgoing" {
		return nil, httpx.ErrInvalidRequest.With("fields", map[string]string{"box": "incoming / outgoing のいずれかです"})
	}
	incoming := box == "incoming"
	rows, err := friendmodel.ListPendingFriendRequests(ctx, s.DB, me, incoming)
	if err != nil {
		return nil, err
	}
	items := make([]FriendRequestItem, 0, len(rows))
	for _, r := range rows {
		peerDID := r.FromDID
		if !incoming {
			peerDID = r.ToDID
		}
		summary, err := core.FetchUserSummary(ctx, s.DB, peerDID)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		items = append(items, friendRequestItem(r, *summary, r.Status))
	}
	return items, nil
}

// Delete はフレンド関係と双方向の位置共有を消す。相手には通知しない（07 §2.3）。
func (s *FriendService) Delete(ctx context.Context, me, peer string) error {
	if peer == me {
		return httpx.ErrNotFound
	}
	a, b := core.OrderDIDs(me, peer)
	var events []realtime.Event
	err := db.Transaction(ctx, s.DB, func(tx *sql.Tx) error {
		deleted, err := model.DeleteFriendship(ctx, tx, a, b)
		if err != nil {
			return err
		}
		if !deleted {
			return httpx.ErrNotFound
		}
		if _, err := model.DeleteLocationSharesBetween(ctx, tx, me, peer); err != nil {
			return err
		}
		meSummary, err := core.FetchUserSummary(ctx, tx, me)
		if err != nil {
			return err
		}
		peerSummary, err := core.FetchUserSummary(ctx, tx, peer)
		if err != nil {
			return err
		}
		events = append(events,
			realtime.Event{Name: "friend.changed", User: peer, Data: meSummary},
			realtime.Event{Name: "friend.changed", User: me, Data: peerSummary},
		)
		return nil
	})
	if err != nil {
		return err
	}
	s.publish(ctx, events, nil)
	return nil
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
			return httpx.ErrConflict.With("reason", "フレンド数の上限（500 人）に達しています")
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

func friendRequestItem(r friendmodel.FriendRequest, peer core.UserSummary, status string) FriendRequestItem {
	return FriendRequestItem{
		ID:        r.ID,
		Peer:      peer,
		Via:       r.Via,
		ViaRef:    r.ViaRef,
		Status:    status,
		CreatedAt: r.CreatedAt,
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
