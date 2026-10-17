package service

import (
	"context"
	"database/sql"
	"errors"

	"github.com/applemango/chain/internal/db"
	"github.com/applemango/chain/internal/httpx"
	"github.com/applemango/chain/internal/model"
	"github.com/applemango/chain/internal/service/core"
)

// BlockService はブロック（13 §4）。
type BlockService struct {
	core.Deps
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
	return db.Transaction(ctx, s.DB, func(tx *sql.Tx) error {
		if err := model.CreateBlock(ctx, tx, blocker, blocked, now); err != nil {
			return err
		}
		a, b := core.OrderDIDs(blocker, blocked)
		if _, err := model.DeleteFriendship(ctx, tx, a, b); err != nil {
			return err
		}
		if _, err := model.DeleteFriendRequestsBetween(ctx, tx, blocker, blocked); err != nil {
			return err
		}
		if _, err := model.DeleteLocationSharesBetween(ctx, tx, blocker, blocked); err != nil {
			return err
		}
		return model.InsertAuditLog(ctx, tx, model.AuditEntry{
			ID: s.IDs.NewID(), ActorDID: blocker, Action: "block.create", Target: blocked, CreatedAt: now,
		})
	})
}

// Delete はブロックを解除する。冪等（行がなくてもエラーにしない）。
func (s *BlockService) Delete(ctx context.Context, blocker, blocked string) error {
	now := s.Clock.Now()
	return db.Transaction(ctx, s.DB, func(tx *sql.Tx) error {
		deleted, err := model.DeleteBlock(ctx, tx, blocker, blocked)
		if err != nil {
			return err
		}
		if !deleted {
			return nil
		}
		return model.InsertAuditLog(ctx, tx, model.AuditEntry{
			ID: s.IDs.NewID(), ActorDID: blocker, Action: "block.delete", Target: blocked, CreatedAt: now,
		})
	})
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
