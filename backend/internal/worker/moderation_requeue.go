package worker

import (
	"context"
	"time"

	"github.com/applemango/chain/internal/model"
	"github.com/applemango/chain/internal/service/content"
	core "github.com/applemango/chain/internal/service/core"
)

// ModerationRequeue は modq を消費して pending の投稿を再判定する（11 §3）。
type ModerationRequeue struct {
	core.Deps
	Queue *content.ModerationQueueService
}

const (
	moderationRequeueLock    = "moderation-requeue"
	moderationRequeueLockTTL = 5 * time.Minute
)

// Tick は 1 回分の再判定を行う。多重実行は lock: で防ぐ。
func (w *ModerationRequeue) Tick(ctx context.Context) error {
	got, err := model.AcquireLock(ctx, w.Redis, moderationRequeueLock, moderationRequeueLockTTL)
	if err != nil {
		return err
	}
	if !got {
		return nil
	}
	defer func() { _ = model.ReleaseLock(context.WithoutCancel(ctx), w.Redis, moderationRequeueLock) }()

	_, err = w.Queue.ProcessPending(ctx)
	return err
}
