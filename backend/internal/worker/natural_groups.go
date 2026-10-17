package worker

import (
	"context"
	"time"

	"github.com/applemango/chain/internal/model"
	core "github.com/applemango/chain/internal/service/core"
	naturalsvc "github.com/applemango/chain/internal/service/natural"
)

// NaturalGroups は自動グループの照合を 5 分ごとに回す（09 §8.4）。業務ロジックは service にある。
type NaturalGroups struct {
	core.Deps
	Groups *naturalsvc.NaturalGroupService
}

const (
	naturalGroupsLockName = "natural-groups"
	naturalGroupsLockTTL  = 5 * time.Minute
)

// Tick は 1 回分の照合を行う。多重実行は lock:natural-groups で防ぐ。
func (w *NaturalGroups) Tick(ctx context.Context) error {
	got, err := model.AcquireLock(ctx, w.Redis, naturalGroupsLockName, naturalGroupsLockTTL)
	if err != nil {
		return err
	}
	if !got {
		return nil
	}
	defer func() { _ = model.ReleaseLock(context.WithoutCancel(ctx), w.Redis, naturalGroupsLockName) }()
	return w.Groups.Tick(ctx)
}
