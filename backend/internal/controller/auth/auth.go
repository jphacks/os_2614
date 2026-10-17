package auth

import (
	"net/url"
	"strings"
	"time"

	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/gofiber/fiber/v3"

	"github.com/applemango/chain/internal/config"
	"github.com/applemango/chain/internal/external/atproto"
	"github.com/applemango/chain/internal/httpx"
	authsvc "github.com/applemango/chain/internal/service/auth"
)

// ログインの流れは specification/backend/03-auth-atproto.md §2。

const returnCookie = "chain_return"

// OAuthController は /api/v1 の外に置く OAuth の入口（client metadata、ログイン、コールバック）。
type OAuthController struct {
	Config *config.Config
	App    *oauth.ClientApp
	Auth   *authsvc.AuthService
}

func (h *OAuthController) Register(r fiber.Router) {
	r.Get("/oauth-client-metadata.json", h.metadata)
	r.Get("/oauth/jwks.json", h.jwks)
	r.Get("/auth/login", h.login)
	r.Get("/auth/callback", h.callback)
	if h.Config.IsDev() {
		r.Get("/auth/dev/complete", h.devComplete)
	}
}

func (h *OAuthController) metadata(c fiber.Ctx) error {
	return c.JSON(atproto.ClientMetadata(h.Config, h.App))
}

func (h *OAuthController) jwks(c fiber.Ctx) error {
	return c.JSON(h.App.Config.PublicJWKS())
}

// login は認可画面へ 302 する。dev では ?return=dev で、完了後に開発用の画面へ戻す（03 §2.1.2）。
func (h *OAuthController) login(c fiber.Ctx) error {
	ret := ""
	if h.Config.IsDev() && c.Query("return") == "dev" {
		ret = "dev"
	}
	c.Cookie(&fiber.Cookie{
		Name: returnCookie, Value: ret, Path: "/auth", HTTPOnly: true, SameSite: "Lax",
		MaxAge: int((10 * time.Minute).Seconds()),
	})

	u, err := h.Auth.StartLogin(c.Context(), c.Query("handle"))
	if err != nil {
		return err
	}
	return c.Redirect().Status(fiber.StatusFound).To(u)
}

func (h *OAuthController) callback(c fiber.Ctx) error {
	dev := c.Cookies(returnCookie) == "dev"
	c.ClearCookie(returnCookie)

	params := url.Values{}
	for k, v := range c.Queries() {
		params.Set(k, v)
	}
	code, err := h.Auth.CompleteLogin(c.Context(), params)
	if dev {
		if err != nil {
			return err
		}
		return c.Redirect().Status(fiber.StatusFound).To("/auth/dev/complete?code=" + url.QueryEscape(code))
	}

	// アプリへは成功でも失敗でも deep link で戻す。失敗時は error に code を入れる。
	q := url.Values{}
	if err != nil {
		if e, ok := err.(*httpx.Error); ok {
			q.Set("error", e.Code)
		} else {
			return err
		}
	} else {
		q.Set("code", code)
	}
	sep := "?"
	if strings.Contains(h.Config.AppRedirect, "?") {
		sep = "&"
	}
	return c.Redirect().Status(fiber.StatusFound).To(h.Config.AppRedirect + sep + q.Encode())
}

// devComplete は PC のブラウザでログインを試すための画面。トークンをそのまま JSON で返す。
func (h *OAuthController) devComplete(c fiber.Ctx) error {
	tokens, err := h.Auth.Exchange(c.Context(), c.Query("code"), "dev-browser")
	if err != nil {
		return err
	}
	return c.JSON(tokens)
}

// AuthController は /api/v1/auth の、認証不要の操作。
type AuthController struct {
	Auth *authsvc.AuthService
}

func (h *AuthController) Register(r fiber.Router) {
	r.Post("/auth/exchange", h.exchange)
	r.Post("/auth/refresh", h.refresh)
}

type exchangeRequest struct {
	Code       string `json:"code"`
	DeviceName string `json:"deviceName"`
}

func (r *exchangeRequest) Validate() map[string]string {
	f := map[string]string{}
	if r.Code == "" {
		f["code"] = "必須です"
	}
	if len(r.DeviceName) > 100 {
		f["deviceName"] = "100 文字までです"
	}
	return f
}

func (h *AuthController) exchange(c fiber.Ctx) error {
	var req exchangeRequest
	if err := httpx.Bind(c, &req); err != nil {
		return err
	}
	tokens, err := h.Auth.Exchange(c.Context(), req.Code, req.DeviceName)
	if err != nil {
		return err
	}
	return c.JSON(tokens)
}

type refreshRequest struct {
	RefreshToken string `json:"refreshToken"`
}

func (r *refreshRequest) Validate() map[string]string {
	if r.RefreshToken == "" {
		return map[string]string{"refreshToken": "必須です"}
	}
	return nil
}

func (h *AuthController) refresh(c fiber.Ctx) error {
	var req refreshRequest
	if err := httpx.Bind(c, &req); err != nil {
		return err
	}
	tokens, err := h.Auth.Refresh(c.Context(), req.RefreshToken)
	if err != nil {
		return err
	}
	return c.JSON(tokens)
}

// LogoutController は認証が要る /api/v1/auth/logout。
type LogoutController struct {
	Auth *authsvc.AuthService
}

func (h *LogoutController) Register(r fiber.Router) {
	r.Post("/auth/logout", h.logout)
}

func (h *LogoutController) logout(c fiber.Ctx) error {
	if err := h.Auth.Logout(c.Context(), httpx.DID(c), httpx.SessionID(c)); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}
