package service

import (
	"context"
	"database/sql"
	"errors"

	"github.com/applemango/chain/internal/db"
	"github.com/applemango/chain/internal/httpx"
	"github.com/applemango/chain/internal/model"
	"github.com/applemango/chain/internal/realtime"
	"github.com/applemango/chain/internal/service/core"
	friendsvc "github.com/applemango/chain/internal/service/friend"
)

// PairReconciler は組ごとに会ったフレンドの条件を確かめる（friend.Reconciler、07 §2.4）。
type PairReconciler interface {
	ReconcileTx(ctx context.Context, q db.Querier, a, b string) (*friendsvc.ReconcileResult, error)
	Publish(ctx context.Context, events []realtime.Event)
}

// BlockService はブロック（13 §4）。
type BlockService struct {
	core.Deps
	// Recon は nil でもよい（その場合は friendships と位置共有を直接消すだけで、friend.changed は送らない）。
	Recon PairReconciler
}

type BlockedUser struct {
	User    model.User
	Profile model.Profile
}

// Create はブロックし、フレンド関係・申請・位置共有を双方向で消し、audit_log に残す。
func (s *BlockService) Create(ctx context.Context, blocker, blocked string) error {
	if blocker == blocked {
		return httpx.ErrInvalidRequest.With("fields", map[string]string{"did": "自分はブロックできません"})
	}
	u, err := model.GetUser(ctx, s.DB, blocked)
	if errors.Is(err, sql.ErrNoRows) {
		return httpx.ErrNotFound
	}
	if err != nil {
		return err
	}
	if u.DeletedAt != nil {
		return httpx.ErrNotFound
	}

	now := s.Clock.Now()
	var res *friendsvc.ReconcileResult
	err = db.Transaction(ctx, s.DB, func(tx *sql.Tx) error {
		if err := model.CreateBlock(ctx, tx, blocker, blocked, now); err != nil {
			return err
		}
		// 会ったフレンドの条件にブロック無しがある（07 §2.4）ので、friendships と位置共有がここで消える。
		if s.Recon != nil {
			if res, err = s.Recon.ReconcileTx(ctx, tx, blocker, blocked); err != nil {
				return err
			}
		} else {
			a, b := core.OrderDIDs(blocker, blocked)
			if _, err := model.DeleteFriendship(ctx, tx, a, b); err != nil {
				return err
			}
			if _, err := model.DeleteLocationSharesBetween(ctx, tx, blocker, blocked); err != nil {
				return err
			}
		}
		return model.InsertAuditLog(ctx, tx, model.AuditEntry{
			ID: s.IDs.NewID(), ActorDID: blocker, Action: "block.create", Target: blocked, CreatedAt: now,
		})
	})
	if err == nil && res != nil {
		s.Recon.Publish(ctx, res.Events)
	}
	return err
}

// Delete はブロックを解除する。冪等（行がなくてもエラーにしない）。
// Bluesky のブロック（source = 'bsky'）は CHAIN からは解除できない（07 §2.5）。
func (s *BlockService) Delete(ctx context.Context, blocker, blocked string) error {
	now := s.Clock.Now()
	var res *friendsvc.ReconcileResult
	err := db.Transaction(ctx, s.DB, func(tx *sql.Tx) error {
		existing, err := model.GetBlock(ctx, tx, blocker, blocked)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if existing.Source == "bsky" {
			return httpx.ErrConflict.With("reason", "Bluesky のブロックは Bluesky のアプリで解除してください")
		}
		if _, err := model.DeleteBlock(ctx, tx, blocker, blocked); err != nil {
			return err
		}
		// 解除でブロックが無くなるので、相互フォローと会った裏付けが残っていれば会ったフレンドに戻る。
		if s.Recon != nil {
			if res, err = s.Recon.ReconcileTx(ctx, tx, blocker, blocked); err != nil {
				return err
			}
		}
		return model.InsertAuditLog(ctx, tx, model.AuditEntry{
			ID: s.IDs.NewID(), ActorDID: blocker, Action: "block.delete", Target: blocked, CreatedAt: now,
		})
	})
	if err == nil && res != nil {
		s.Recon.Publish(ctx, res.Events)
	}
	return err
}

func (s *BlockService) List(ctx context.Context, blocker string) ([]BlockedUser, error) {
	rows, err := model.ListBlocks(ctx, s.DB, blocker)
	if err != nil {
		return nil, err
	}
	out := make([]BlockedUser, 0, len(rows))
	for _, b := range rows {
		u, err := model.GetUser(ctx, s.DB, b.Blocked)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		item := BlockedUser{User: *u}
		p, err := model.GetProfile(ctx, s.DB, b.Blocked)
		switch {
		case err == nil:
			item.Profile = *p
		case !errors.Is(err, sql.ErrNoRows):
			return nil, err
		}
		out = append(out, item)
	}
	return out, nil
}
