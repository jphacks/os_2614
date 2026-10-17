package worker

import (
	"context"
	"log/slog"
	"time"

	"github.com/applemango/chain/internal/model"
	"github.com/applemango/chain/internal/service"
	core "github.com/applemango/chain/internal/service/core"
)

// Janitor は期限切れデータの掃除を 10 分ごとに回す（04 §4）。業務ロジックは service にある。
type Janitor struct {
	core.Deps
	Janitor *service.Janitor
}

const (
	janitorLockName = "janitor"
	janitorLockTTL  = 5 * time.Minute
)

// Tick は 1 回分の掃除を行う。多重実行は lock: で防ぐ。
func (w *Janitor) Tick(ctx context.Context) error {
	got, err := model.AcquireLock(ctx, w.Redis, janitorLockName, janitorLockTTL)
	if err != nil {
		return err
	}
	if !got {
		return nil
	}
	defer func() { _ = model.ReleaseLock(context.WithoutCancel(ctx), w.Redis, janitorLockName) }()

	res, err := w.Janitor.Sweep(ctx)
	if err != nil {
		return err
	}
	if res.Total() > 0 {
		slog.InfoContext(ctx, "janitor swept", "count", res.Total())
	}
	return nil
}
