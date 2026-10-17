package worker

import (
	"context"
	"time"

	"github.com/applemango/chain/internal/model"
	core "github.com/applemango/chain/internal/service/core"
	naturalsvc "github.com/applemango/chain/internal/service/natural"
)

// EmbedBackfill は embedding の無い（または古い）予定を 1 分ごとに計算し直す（09 §8.3）。
// 業務ロジックは service にある。
type EmbedBackfill struct {
	core.Deps
	Schedule *naturalsvc.ScheduleService
}

const (
	embedBackfillLockName = "embed-backfill"
	embedBackfillLockTTL  = time.Minute
)

// Tick は 1 回分の再計算を行う。Embedder が無ければ何もしない。多重実行は lock: で防ぐ。
func (w *EmbedBackfill) Tick(ctx context.Context) error {
	if w.Schedule == nil || w.Schedule.Embedder == nil {
		return nil
	}
	got, err := model.AcquireLock(ctx, w.Redis, embedBackfillLockName, embedBackfillLockTTL)
	if err != nil {
		return err
	}
	if !got {
		return nil
	}
	defer func() { _ = model.ReleaseLock(context.WithoutCancel(ctx), w.Redis, embedBackfillLockName) }()

	_, err = w.Schedule.BackfillEmbeddings(ctx)
	return err
}
