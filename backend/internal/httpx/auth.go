package httpx

import (
	"context"
	"strings"

	"github.com/gofiber/fiber/v3"

	"github.com/applemango/chain/internal/auth"
	"github.com/applemango/chain/internal/clock"
)

// SessionChecker はセッションが失効していないかを確かめる（03 §3）。
// 実装は service 層に置く。
type SessionChecker interface {
	Active(ctx context.Context, sid string) (bool, error)
}

type localsKey int

const (
	keyDID localsKey = iota
	keySID
)

// RequireAuth は Authorization: Bearer の access JWT を検証する。
func RequireAuth(jwtKey []byte, clk clock.Clock, sessions SessionChecker) fiber.Handler {
	return func(c fiber.Ctx) error {
		h := c.Get(fiber.HeaderAuthorization)
		token, ok := strings.CutPrefix(h, "Bearer ")
		if !ok || token == "" {
			return ErrUnauthenticated
		}
		claims, err := auth.ParseAccess(jwtKey, token, clk.Now())
		if err != nil {
			return ErrUnauthenticated
		}
		active, err := sessions.Active(c.Context(), claims.SessionID)
		if err != nil {
			return err
		}
		if !active {
			return ErrUnauthenticated
		}
		c.Locals(keyDID, claims.DID)
		c.Locals(keySID, claims.SessionID)
		return c.Next()
	}
}

// DID は RequireAuth を通ったリクエストの呼び出し元。
func DID(c fiber.Ctx) string {
	v, _ := c.Locals(keyDID).(string)
	return v
}

func SessionID(c fiber.Ctx) string {
	v, _ := c.Locals(keySID).(string)
	return v
}
