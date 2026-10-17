package worker

import (
	"context"
	"time"

	"github.com/applemango/chain/internal/model"
	"github.com/applemango/chain/internal/service"
)

// GroupVotes は期限を過ぎたグループの投票を 1 分ごとに締める（09 §8.6）。
type GroupVotes struct {
	service.Deps
	Votes *service.GroupVoteService
}

const (
	groupVotesLockName = "group-votes"
	groupVotesLockTTL  = time.Minute
)

// Tick は 1 回分の締め切りを行う。多重起動は lock: で防ぐ。
func (w *GroupVotes) Tick(ctx context.Context) error {
	got, err := model.AcquireLock(ctx, w.Redis, groupVotesLockName, groupVotesLockTTL)
	if err != nil {
		return err
	}
	if !got {
		return nil
	}
	defer func() { _ = model.ReleaseLock(context.WithoutCancel(ctx), w.Redis, groupVotesLockName) }()
	_, err = w.Votes.CloseDue(ctx)
	return err
}
