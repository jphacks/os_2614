package worker

import (
	"context"
	"time"

	"github.com/applemango/chain/internal/model"
	"github.com/applemango/chain/internal/service"
)

// PartyEngine は party エンジンを 5 秒ごとに回す（08 §4.1）。業務ロジックは service にある。
type PartyEngine struct {
	service.Deps
	Parties *service.PartyService
}

const (
	partyEngineLockName = "party-engine"
	partyEngineLockTTL  = 30 * time.Second
)

// Tick は 1 回分のエンジンを実行する。多重起動は lock: で防ぐ。
func (w *PartyEngine) Tick(ctx context.Context) error {
	got, err := model.AcquireLock(ctx, w.Redis, partyEngineLockName, partyEngineLockTTL)
	if err != nil {
		return err
	}
	if !got {
		return nil
	}
	defer func() { _ = model.ReleaseLock(context.WithoutCancel(ctx), w.Redis, partyEngineLockName) }()
	return w.Parties.Tick(ctx)
}
