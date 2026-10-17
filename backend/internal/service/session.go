package service

import (
	"context"
	"database/sql"
	"errors"

	"github.com/applemango/chain/internal/model"
)

// SessionService はアプリセッションを扱う（03 §3）。ログイン・更新・ログアウトは 03 の実装で足す。
type SessionService struct {
	Deps
}

// Active は httpx.SessionChecker を満たす。Redis のキャッシュを見て、なければ SQLite を引く。
func (s *SessionService) Active(ctx context.Context, sid string) (bool, error) {
	active, found, err := model.GetSessionCache(ctx, s.Redis, sid)
	if err != nil {
		return false, err
	}
	if found {
		return active, nil
	}

	sess, err := model.GetAppSession(ctx, s.DB, sid)
	if errors.Is(err, sql.ErrNoRows) {
		active = false
	} else if err != nil {
		return false, err
	} else {
		active = sess.RevokedAt == nil && sess.ExpiresAt.After(s.Clock.Now())
	}
	if err := model.SetSessionCache(ctx, s.Redis, sid, active); err != nil {
		return false, err
	}
	return active, nil
}
