package service

import (
	"context"
	"database/sql"
	"errors"

	"github.com/applemango/chain/internal/httpx"
	"github.com/applemango/chain/internal/model"
)

// UserService は他人のプロフィールの閲覧（13 §1.2）。
type UserService struct {
	Deps
}

type PublicUser struct {
	User    model.User
	Profile model.Profile
}

func (s *UserService) Get(ctx context.Context, viewer, target string) (*PublicUser, error) {
	u, err := model.GetUser(ctx, s.DB, target)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, httpx.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if u.DeletedAt != nil {
		return nil, httpx.ErrNotFound
	}

	if viewer != target {
		blocked, err := model.IsBlocked(ctx, s.DB, viewer, target)
		if err != nil {
			return nil, err
		}
		if blocked {
			return nil, httpx.ErrNotFound
		}
		related, err := model.HasRelationship(ctx, s.DB, viewer, target)
		if err != nil {
			return nil, err
		}
		if !related {
			return nil, httpx.ErrNotFound
		}
	}

	pub := &PublicUser{User: *u}
	p, err := model.GetProfile(ctx, s.DB, target)
	switch {
	case err == nil:
		pub.Profile = *p
	case !errors.Is(err, sql.ErrNoRows):
		return nil, err
	}
	return pub, nil
}
