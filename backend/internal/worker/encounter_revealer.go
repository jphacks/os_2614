package worker

import (
	"context"
	"log/slog"
	"time"

	"github.com/applemango/chain/internal/model"
	core "github.com/applemango/chain/internal/service/core"
	encountersvc "github.com/applemango/chain/internal/service/encounter"
)

// EncounterRevealer は公開時刻に達したすれ違いを公開する（06 §5.4、01 §2）。
type EncounterRevealer struct {
	core.Deps
	Encounters *encountersvc.EncounterService
}

const (
	revealerLockName = "encounter-reveal"
	revealerLockTTL  = 5 * time.Minute
)

// Tick は 1 回分の公開処理を行う。多重実行は lock: で防ぐ。
func (w *EncounterRevealer) Tick(ctx context.Context) error {
	got, err := model.AcquireLock(ctx, w.Redis, revealerLockName, revealerLockTTL)
	if err != nil {
		return err
	}
	if !got {
		return nil
	}
	defer func() { _ = model.ReleaseLock(context.WithoutCancel(ctx), w.Redis, revealerLockName) }()

	n, err := w.Encounters.RevealPending(ctx)
	if err != nil {
		return err
	}
	if n > 0 {
		slog.InfoContext(ctx, "encounters revealed", "count", n)
	}
	return nil
}
