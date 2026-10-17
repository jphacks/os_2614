package model

import (
	"context"
	"database/sql"
	"time"

	"github.com/applemango/chain/internal/clock"
	"github.com/applemango/chain/internal/db"
)

// Notification は notifications の行（04 §2.5、12 §4）。
type Notification struct {
	ID          string
	DID         string
	Type        string
	PayloadJSON string
	CreatedAt   time.Time
	ReadAt      *time.Time
}

const notificationColumns = `id, did, type, payload_json, created_at, read_at`

func scanNotification(scan db.Scan, n *Notification) error {
	var created int64
	var readAt sql.NullInt64
	if err := scan(&n.ID, &n.DID, &n.Type, &n.PayloadJSON, &created, &readAt); err != nil {
		return err
	}
	n.CreatedAt = clock.FromUnixMilli(created)
	if readAt.Valid {
		t := clock.FromUnixMilli(readAt.Int64)
		n.ReadAt = &t
	}
	return nil
}

func CreateNotification(ctx context.Context, q db.Querier, n Notification) error {
	_, err := db.Execute(ctx, q, `
		INSERT INTO notifications (id, did, type, payload_json, created_at, read_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		n.ID, n.DID, n.Type, n.PayloadJSON, clock.UnixMilli(n.CreatedAt), NullableTime(n.ReadAt))
	return err
}

// EnqueueNotification は notification_outbox に queued の行を積む（12 §4）。
func EnqueueNotification(ctx context.Context, q db.Querier, id, notificationID string, now time.Time) error {
	_, err := db.Execute(ctx, q, `
		INSERT INTO notification_outbox (id, notification_id, status, attempts, next_attempt_at)
		VALUES (?, ?, 'queued', 0, ?)`,
		id, notificationID, clock.UnixMilli(now))
	return err
}

// DueNotification は送信すべき outbox の 1 件（notification_outbox と notifications の結合）。
type DueNotification struct {
	OutboxID       string
	NotificationID string
	DID            string
	Type           string
	PayloadJSON    string
	Attempts       int
}

// ListDueNotifications は queued で next_attempt_at が now 以前のものを古い順に返す（12 §4）。
func ListDueNotifications(ctx context.Context, q db.Querier, now time.Time, limit int) ([]DueNotification, error) {
	return db.GetItems(ctx, q, `
		SELECT o.id, o.notification_id, n.did, n.type, n.payload_json, o.attempts
		FROM notification_outbox o
		JOIN notifications n ON n.id = o.notification_id
		WHERE o.status = 'queued' AND o.next_attempt_at <= ?
		ORDER BY o.next_attempt_at, o.id
		LIMIT ?`,
		[]any{clock.UnixMilli(now), limit},
		func(scan db.Scan, d *DueNotification) error {
			return scan(&d.OutboxID, &d.NotificationID, &d.DID, &d.Type, &d.PayloadJSON, &d.Attempts)
		})
}

// MarkNotificationSent は outbox を sent にする。
func MarkNotificationSent(ctx context.Context, q db.Querier, outboxID string) error {
	_, err := db.Execute(ctx, q, "UPDATE notification_outbox SET status = 'sent' WHERE id = ?", outboxID)
	return err
}

// RetryNotification は attempts と next_attempt_at を更新して queued のまま残す（12 §4）。
func RetryNotification(ctx context.Context, q db.Querier, outboxID string, attempts int, nextAt time.Time) error {
	_, err := db.Execute(ctx, q,
		"UPDATE notification_outbox SET status = 'queued', attempts = ?, next_attempt_at = ? WHERE id = ?",
		attempts, clock.UnixMilli(nextAt), outboxID)
	return err
}

// FailNotification は 5 回失敗した outbox を failed にする（12 §4）。
func FailNotification(ctx context.Context, q db.Querier, outboxID string, attempts int) error {
	_, err := db.Execute(ctx, q,
		"UPDATE notification_outbox SET status = 'failed', attempts = ? WHERE id = ?", attempts, outboxID)
	return err
}

// ListNotifications は did の通知を新しい順に返す。cursorID が空なら先頭から。
func ListNotifications(ctx context.Context, q db.Querier, did string, cursorAt time.Time, cursorID string, limit int) ([]Notification, error) {
	query := "SELECT " + notificationColumns + " FROM notifications WHERE did = ?"
	args := []any{did}
	if cursorID != "" {
		query += " AND (created_at < ? OR (created_at = ? AND id < ?))"
		ms := clock.UnixMilli(cursorAt)
		args = append(args, ms, ms, cursorID)
	}
	query += " ORDER BY created_at DESC, id DESC LIMIT ?"
	args = append(args, limit)
	return db.GetItems(ctx, q, query, args, scanNotification)
}

// MarkNotificationsRead は ids のうち did の未読のものを既読にする。更新した件数を返す。
func MarkNotificationsRead(ctx context.Context, q db.Querier, did string, ids []string, now time.Time) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	args := make([]any, 0, len(ids)+2)
	args = append(args, clock.UnixMilli(now), did)
	for _, id := range ids {
		args = append(args, id)
	}
	res, err := db.Execute(ctx, q,
		"UPDATE notifications SET read_at = ? WHERE did = ? AND read_at IS NULL AND id IN ("+
			Placeholders(len(ids))+")", args...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// MarkAllNotificationsRead は did の未読の通知をすべて既読にする。更新した件数を返す。
func MarkAllNotificationsRead(ctx context.Context, q db.Querier, did string, now time.Time) (int64, error) {
	res, err := db.Execute(ctx, q,
		"UPDATE notifications SET read_at = ? WHERE did = ? AND read_at IS NULL",
		clock.UnixMilli(now), did)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
