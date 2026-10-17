package controller

import (
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/gofiber/fiber/v3"

	"github.com/applemango/chain/internal/external/atproto"
	"github.com/applemango/chain/internal/httpx"
	"github.com/applemango/chain/internal/service"
)

type UserController struct {
	User *service.UserService
}

func (h *UserController) Register(r fiber.Router) {
	r.Get("/users/:did", h.get)
}

type userResponse struct {
	DID         string `json:"did"`
	Handle      string `json:"handle"`
	DisplayName string `json:"displayName,omitempty"`
	Description string `json:"description,omitempty"`
	AvatarURL   string `json:"avatarUrl,omitempty"`
	BannerURL   string `json:"bannerUrl,omitempty"`
	Status      string `json:"status,omitempty"`
}

func (h *UserController) get(c fiber.Ctx) error {
	did, err := syntax.ParseDID(c.Params("did"))
	if err != nil {
		return httpx.ErrInvalidRequest.With("fields", map[string]string{"did": "DID の形式が不正です"})
	}
	p, err := h.User.Get(c.Context(), httpx.DID(c), did.String())
	if err != nil {
		return err
	}
	return c.JSON(userResponse{
		DID:         p.User.DID,
		Handle:      p.User.Handle,
		DisplayName: p.Profile.DisplayName,
		Description: p.Profile.Description,
		AvatarURL:   atproto.AvatarURL(p.User.DID, p.Profile.AvatarCID),
		BannerURL:   atproto.BannerURL(p.User.DID, p.Profile.BannerCID),
		Status:      p.Profile.Status,
	})
}
