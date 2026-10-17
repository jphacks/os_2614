package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/applemango/chain/internal/db"
	"github.com/applemango/chain/internal/httpx"
	"github.com/applemango/chain/internal/model"
	"github.com/applemango/chain/internal/utils"
)

// 12 §3 の通知の種類のうち、S4 で作るもの。本文の組み立ては送信側（friend）が行う。
const (
	NotificationFriendRequest       = "friend.request"
	NotificationFriendAccepted      = "friend.accepted"
	NotificationPartyConfirmed      = "party.confirmed"
	NotificationModerationHidden    = "moderation.hidden"
	NotificationGroupNearby         = "group.nearby"
	NotificationGroupAutoJoined     = "group.auto_joined"
	NotificationGroupVotePassed     = "group.vote_passed"
	NotificationGroupInvited        = "group.invited"
	NotificationGroupInviteApproval = "group.invite_approval"
)

// moderationHiddenPayload は moderation.hidden の payload（12 §3）。遷移先は Space。
type moderationHiddenPayload struct {
	Text    string `json:"text"`
	SpaceID string `json:"spaceId"`
	URI     string `json:"uri"`
}

// NotificationItem は GET /notifications の 1 件（12 §1）。data は業務処理が積んだ payload。
type NotificationItem struct {
	ID        string          `json:"id"`
	Type      string          `json:"type"`
	Data      json.RawMessage `json:"data"`
	CreatedAt time.Time       `json:"createdAt"`
	ReadAt    *time.Time      `json:"readAt,omitempty"`
}

type NotificationPage struct {
	Items  []NotificationItem `json:"items"`
	Cursor string             `json:"cursor,omitempty"`
}

// NotificationService は通知の一覧と既読化（12 §1）。
type NotificationService struct {
	Deps
}

func (s *NotificationService) List(ctx context.Context, did, cursor string, limit int) (*NotificationPage, error) {
	if limit <= 0 {
		limit = defaultPageLimit
	}
	if limit > maxPageLimit {
		limit = maxPageLimit
	}
	cursorAt, cursorID, err := utils.DecodeCursor(cursor)
	if err != nil {
		return nil, httpx.ErrInvalidRequest.With("fields", map[string]string{"cursor": "カーソルが不正です"})
	}
	rows, err := model.ListNotifications(ctx, s.DB, did, time.UnixMilli(cursorAt).UTC(), cursorID, limit+1)
	if err != nil {
		return nil, err
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	page := &NotificationPage{Items: make([]NotificationItem, 0, len(rows))}
	for _, n := range rows {
		page.Items = append(page.Items, notificationItem(n))
	}
	if hasMore {
		last := rows[len(rows)-1]
		page.Cursor = utils.EncodeCursor(last.CreatedAt.UnixMilli(), last.ID)
	}
	return page, nil
}

// MarkRead は ids か all で既読にする。返すのは更新した件数。
func (s *NotificationService) MarkRead(ctx context.Context, did string, ids []string, all bool) (int64, error) {
	now := s.Clock.Now()
	if all {
		return model.MarkAllNotificationsRead(ctx, s.DB, did, now)
	}
	return model.MarkNotificationsRead(ctx, s.DB, did, ids, now)
}

func notificationItem(n model.Notification) NotificationItem {
	data := json.RawMessage(n.PayloadJSON)
	if len(data) == 0 {
		data = json.RawMessage(`{}`)
	}
	return NotificationItem{ID: n.ID, Type: n.Type, Data: data, CreatedAt: n.CreatedAt, ReadAt: n.ReadAt}
}

// notificationToSend は業務処理のコミット後に WebSocket へ流す通知（10 §7.3、12 §4）。
type notificationToSend struct {
	DID  string
	Item NotificationItem
}

// enqueueNotification は業務処理と同じトランザクションで notifications と
// notification_outbox(queued) に積む（12 §4）。宛先の notify_json で off なら何もしない。
// 積んだときだけ item を返す。
func enqueueNotification(ctx context.Context, q db.Querier, deps Deps, did, typ string, payload any, now time.Time) (*NotificationItem, error) {
	enabled, err := notificationEnabled(ctx, q, did, typ, now)
	if err != nil {
		return nil, err
	}
	if !enabled {
		return nil, nil
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	n := model.Notification{
		ID:          deps.IDs.NewID(),
		DID:         did,
		Type:        typ,
		PayloadJSON: string(b),
		CreatedAt:   now,
	}
	if err := model.CreateNotification(ctx, q, n); err != nil {
		return nil, err
	}
	if err := model.EnqueueNotification(ctx, q, deps.IDs.NewID(), n.ID, now); err != nil {
		return nil, err
	}
	item := notificationItem(n)
	return &item, nil
}

// notificationEnabled は 12 §3 の設定を見る。notify_json は既定から変えた種類だけを持つ。
func notificationEnabled(ctx context.Context, q db.Querier, did, typ string, now time.Time) (bool, error) {
	if err := model.EnsureSettings(ctx, q, did, now); err != nil {
		return false, err
	}
	st, err := model.GetSettings(ctx, q, did)
	if errors.Is(err, sql.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if st.NotifyJSON == "" {
		return true, nil
	}
	prefs := map[string]bool{}
	if err := json.Unmarshal([]byte(st.NotifyJSON), &prefs); err != nil {
		return false, err
	}
	if v, ok := prefs[typ]; ok {
		return v, nil
	}
	return true, nil
}
