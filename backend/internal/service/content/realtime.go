package content

import (
	"context"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/applemango/chain/internal/auth"
	"github.com/applemango/chain/internal/httpx"
	"github.com/applemango/chain/internal/service/access"
	"github.com/applemango/chain/internal/service/core"
)

// RealtimeService は WebSocket の接続認証と購読の判定（10 §7.1、§7.2）。
type RealtimeService struct {
	core.Deps
	Access   access.AccessChecker
	Sessions httpx.SessionChecker
}

// Authorize は auth メッセージの access JWT を検証し、DID とトークンの期限を返す。
func (s *RealtimeService) Authorize(ctx context.Context, token string) (string, time.Time, error) {
	claims, err := auth.ParseAccess(s.Config.JWTKey, token, s.Clock.Now())
	if err != nil {
		return "", time.Time{}, httpx.ErrUnauthenticated
	}
	active, err := s.Sessions.Active(ctx, claims.SessionID)
	if err != nil {
		return "", time.Time{}, err
	}
	if !active {
		return "", time.Time{}, httpx.ErrUnauthenticated
	}
	return claims.DID, claims.ExpiresAt, nil
}

// CheckRead は購読の read 判定（10 §7.2）。許可なら nil。
func (s *RealtimeService) CheckRead(ctx context.Context, spaceID, did string) error {
	ok, err := s.Access.Check(ctx, spaceID, syntax.DID(did), access.ModeRead)
	if err != nil {
		return err
	}
	if !ok {
		return httpx.ErrForbidden
	}
	return nil
}
