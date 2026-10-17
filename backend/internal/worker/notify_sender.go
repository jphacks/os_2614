package worker

import (
	"context"
	"time"

	"github.com/applemango/chain/internal/model"
	core "github.com/applemango/chain/internal/service/core"
	"github.com/applemango/chain/internal/service/notify"
)

// NotifySender は due な通知を FCM で送る（12 §4）。
type NotifySender struct {
	core.Deps
	Notifier *notify.NotifySender
}

const (
	notifySenderLock    = "notify-send"
	notifySenderLockTTL = 30 * time.Second
)

// Tick は 1 回分の送信を行う。多重実行は lock: で防ぐ。
func (w *NotifySender) Tick(ctx context.Context) error {
	got, err := model.AcquireLock(ctx, w.Redis, notifySenderLock, notifySenderLockTTL)
	if err != nil {
		return err
	}
	if !got {
		return nil
	}
	defer func() { _ = model.ReleaseLock(context.WithoutCancel(ctx), w.Redis, notifySenderLock) }()

	_, err = w.Notifier.SendDue(ctx)
	return err
}
