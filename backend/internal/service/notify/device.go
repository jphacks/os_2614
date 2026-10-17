package notify

import (
	"context"
	"strings"

	"github.com/applemango/chain/internal/httpx"
	"github.com/applemango/chain/internal/model"
	"github.com/applemango/chain/internal/service/core"
)

// DeviceService は端末（FCM トークン）の登録・削除（12 §2）。
type DeviceService struct {
	core.Deps
}

// RegisterDevice は FCM トークンを登録する。同じトークンが別ユーザーに紐付いていれば付け替える（12 §2）。
func (s *DeviceService) RegisterDevice(ctx context.Context, did, sessionID, fcmToken, platform string) error {
	token := strings.TrimSpace(fcmToken)
	if token == "" {
		return httpx.ErrInvalidRequest.With("fields", map[string]string{"fcmToken": "必須です"})
	}
	if platform == "" {
		platform = "android"
	}
	return model.UpsertDevice(ctx, s.DB, model.Device{
		ID: s.IDs.NewID(), DID: did, SessionID: sessionID,
		FCMToken: token, Platform: platform, UpdatedAt: s.Clock.Now(),
	})
}

// UnregisterDevice は自分の FCM トークンを消す（ログアウト、12 §2）。無くても成功とする。
func (s *DeviceService) UnregisterDevice(ctx context.Context, did, fcmToken string) error {
	if strings.TrimSpace(fcmToken) == "" {
		return httpx.ErrInvalidRequest.With("fields", map[string]string{"fcmToken": "必須です"})
	}
	_, err := model.DeleteDevice(ctx, s.DB, did, fcmToken)
	return err
}
