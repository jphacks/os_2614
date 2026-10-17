package worker

import (
	"context"
	"time"

	"github.com/applemango/chain/internal/model"
	core "github.com/applemango/chain/internal/service/core"
	"github.com/applemango/chain/internal/service/notify"
)

// Digest は 1 分ごとにすれ違いのダイジェストを積む（12 §5）。
type Digest struct {
	core.Deps
	Digest *notify.Digest
}

const (
	digestLockName = "digest"
	digestLockTTL  = 5 * time.Minute
)

// Tick は 1 回分のダイジェストを行う。多重実行は lock:digest で防ぐ（12 §5）。
func (w *Digest) Tick(ctx context.Context) error {
	got, err := model.AcquireLock(ctx, w.Redis, digestLockName, digestLockTTL)
	if err != nil {
		return err
	}
	if !got {
		return nil
	}
	defer func() { _ = model.ReleaseLock(context.WithoutCancel(ctx), w.Redis, digestLockName) }()

	return w.Digest.Run(ctx, w.Clock.Now())
}
