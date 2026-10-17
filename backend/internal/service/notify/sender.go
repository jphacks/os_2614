package notify

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/applemango/chain/internal/external/fcm"
	"github.com/applemango/chain/internal/model"
	"github.com/applemango/chain/internal/service/core"
)

// NotifySender は due な outbox を FCM で送る（12 §4）。業務処理は送信しない。
type NotifySender struct {
	core.Deps
	Sender fcm.Sender
}

const (
	// NotifyBatch は 1 回に取り出す outbox の上限（12 §4）。
	NotifyBatch = 100
	// NotifyMaxAttempts は failed にするまでの失敗回数（12 §4）。
	NotifyMaxAttempts = 5
	notifyBackoffBase = 10 * time.Second
)

// SendDue は due な通知を宛先ユーザーの全 devices に送る。戻り値は完了した（sent にした）件数。
func (s *NotifySender) SendDue(ctx context.Context) (int, error) {
	now := s.Clock.Now()
	due, err := model.ListDueNotifications(ctx, s.DB, now, NotifyBatch)
	if err != nil {
		return 0, err
	}
	sent := 0
	for _, d := range due {
		devices, err := model.ListDevicesByDID(ctx, s.DB, d.DID)
		if err != nil {
			return sent, err
		}
		msg := buildMessage(d)
		var sendErr error
		for _, dev := range devices {
			msg.Token = dev.FCMToken
			if err := s.Sender.Send(ctx, msg); err != nil {
				if errors.Is(err, fcm.ErrUnregistered) {
					if derr := model.DeleteDeviceByToken(ctx, s.DB, dev.FCMToken); derr != nil {
						return sent, derr
					}
					continue
				}
				sendErr = err
			}
		}
		switch {
		case sendErr != nil:
			attempts := d.Attempts + 1
			if attempts >= NotifyMaxAttempts {
				if err := model.FailNotification(ctx, s.DB, d.OutboxID, attempts); err != nil {
					return sent, err
				}
				continue
			}
			if err := model.RetryNotification(ctx, s.DB, d.OutboxID, attempts, now.Add(notifyBackoff(attempts))); err != nil {
				return sent, err
			}
		default:
			if err := model.MarkNotificationSent(ctx, s.DB, d.OutboxID); err != nil {
				return sent, err
			}
			sent++
		}
	}
	return sent, nil
}

// notificationPayload は通知の payload_json から、FCM に載せる項目だけを読む。
type notificationPayload struct {
	Text      string `json:"text"`
	TargetID  string `json:"targetId"`
	SpaceID   string `json:"spaceId"`
	RequestID string `json:"requestId"`
}

// buildMessage は 12 §4 のメッセージを組み立てる。notification（title, body）と
// data（type と遷移先の ID）を両方入れる。
func buildMessage(d model.DueNotification) fcm.Message {
	var p notificationPayload
	_ = json.Unmarshal([]byte(d.PayloadJSON), &p)

	data := map[string]string{"type": d.Type}
	switch {
	case p.TargetID != "":
		data["targetId"] = p.TargetID
	case p.SpaceID != "":
		data["targetId"] = p.SpaceID
	case p.RequestID != "":
		data["targetId"] = p.RequestID
	}
	if p.SpaceID != "" {
		data["spaceId"] = p.SpaceID
	}

	return fcm.Message{
		Title:       notificationTitle(d.Type),
		Body:        p.Text,
		Data:        data,
		ChannelID:   notificationChannel(d.Type),
		CollapseKey: notificationCollapseKey(d.Type, p.SpaceID),
	}
}

// notificationBackoff は 12 §4 の next_attempt_at = now + 2^attempts × 10 秒。
func notifyBackoff(attempts int) time.Duration {
	if attempts < 1 {
		attempts = 1
	}
	if attempts > 10 {
		attempts = 10
	}
	return time.Duration(1<<attempts) * notifyBackoffBase
}

// notificationTitle は 12 §3 の kind ごとのタイトル。
func notificationTitle(typ string) string {
	switch typ {
	case NotificationFriendRequest:
		return "フレンド申請"
	case NotificationFriendAccepted:
		return "フレンド承認"
	case "party.confirmed":
		return "Party"
	case "group.nearby":
		return "近くのグループ"
	case "group.invited":
		return "グループへの招待"
	case "group.suggested":
		return "グループの候補"
	case "encounter.daily", "encounter.weekly", "encounter.monthly":
		return "すれ違い"
	case NotificationModerationHidden:
		return "投稿のお知らせ"
	default:
		return "CHAIN"
	}
}

// notificationChannel は Android の通知チャンネル。すれ違いは digest、それ以外は social（12 §3）。
func notificationChannel(typ string) string {
	if strings.HasPrefix(typ, "encounter.") {
		return "digest"
	}
	return "social"
}

// notificationCollapseKey はダイジェストは kind ごと、group.nearby は Space ごと（12 §4）。
func notificationCollapseKey(typ, spaceID string) string {
	switch {
	case typ == "group.nearby" && spaceID != "":
		return "group.nearby:" + spaceID
	case strings.HasPrefix(typ, "encounter."):
		return typ
	default:
		return ""
	}
}
