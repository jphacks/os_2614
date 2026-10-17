package model

import (
	"context"
	"database/sql"
	"time"

	"github.com/applemango/chain/internal/clock"
	"github.com/applemango/chain/internal/db"
)

// Device は devices の行（04 §2.1、12 §2）。FCM トークンは UNIQUE。
type Device struct {
	ID        string
	DID       string
	SessionID string
	FCMToken  string
	Platform  string
	UpdatedAt time.Time
}

const deviceColumns = `id, did, session_id, fcm_token, platform, updated_at`

func scanDevice(scan db.Scan, d *Device) error {
	var session sql.NullString
	var updated int64
	if err := scan(&d.ID, &d.DID, &session, &d.FCMToken, &d.Platform, &updated); err != nil {
		return err
	}
	d.SessionID = session.String
	d.UpdatedAt = clock.FromUnixMilli(updated)
	return nil
}

// UpsertDevice は FCM トークンを登録する。同じトークンが別ユーザーに紐付いていれば、
// その行を新しい持ち主へ付け替える（12 §2）。id / session_id は最新にする。
func UpsertDevice(ctx context.Context, q db.Querier, d Device) error {
	_, err := db.Execute(ctx, q, `
		INSERT INTO devices (id, did, session_id, fcm_token, platform, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (fcm_token) DO UPDATE SET
		  did = excluded.did,
		  session_id = excluded.session_id,
		  platform = excluded.platform,
		  updated_at = excluded.updated_at`,
		d.ID, d.DID, Nullable(d.SessionID), d.FCMToken, d.Platform, clock.UnixMilli(d.UpdatedAt))
	return err
}

// DeleteDevice は did が持つ fcm_token の行を消す（ログアウト、12 §2）。消したら true。
func DeleteDevice(ctx context.Context, q db.Querier, did, fcmToken string) (bool, error) {
	res, err := db.Execute(ctx, q, "DELETE FROM devices WHERE did = ? AND fcm_token = ?", did, fcmToken)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// DeleteDeviceByToken は FCM が UNREGISTERED を返したトークンを消す（12 §2）。
func DeleteDeviceByToken(ctx context.Context, q db.Querier, fcmToken string) error {
	_, err := db.Execute(ctx, q, "DELETE FROM devices WHERE fcm_token = ?", fcmToken)
	return err
}

// ListDevicesByDID は宛先ユーザーの全 devices を返す（12 §4）。
func ListDevicesByDID(ctx context.Context, q db.Querier, did string) ([]Device, error) {
	return db.GetItems(ctx, q,
		"SELECT "+deviceColumns+" FROM devices WHERE did = ? ORDER BY updated_at DESC",
		[]any{did}, scanDevice)
}
