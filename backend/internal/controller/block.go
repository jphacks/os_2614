package controller

import (
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/gofiber/fiber/v3"

	"github.com/applemango/chain/internal/external/atproto"
	"github.com/applemango/chain/internal/httpx"
	"github.com/applemango/chain/internal/service"
)

type BlockController struct {
	Blocks *service.BlockService
}

func (h *BlockController) Register(r fiber.Router) {
	r.Post("/blocks", h.create)
	r.Delete("/blocks/:did", h.delete)
	r.Get("/blocks", h.list)
}

type blockRequest struct {
	DID string `json:"did"`
}

func (r *blockRequest) Validate() map[string]string {
	if r.DID == "" {
		return map[string]string{"did": "必須です"}
	}
	if _, err := syntax.ParseDID(r.DID); err != nil {
		return map[string]string{"did": "DID の形式が不正です"}
	}
	return nil
}

func (h *BlockController) create(c fiber.Ctx) error {
	var req blockRequest
	if err := httpx.Bind(c, &req); err != nil {
		return err
	}
	if err := h.Blocks.Create(c.Context(), httpx.DID(c), req.DID); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *BlockController) delete(c fiber.Ctx) error {
	did, err := syntax.ParseDID(c.Params("did"))
	if err != nil {
		return httpx.ErrInvalidRequest.With("fields", map[string]string{"did": "DID の形式が不正です"})
	}
	if err := h.Blocks.Delete(c.Context(), httpx.DID(c), did.String()); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

type blockedUserResponse struct {
	DID         string `json:"did"`
	Handle      string `json:"handle"`
	DisplayName string `json:"displayName,omitempty"`
	AvatarURL   string `json:"avatarUrl,omitempty"`
}

func (h *BlockController) list(c fiber.Ctx) error {
	blocks, err := h.Blocks.List(c.Context(), httpx.DID(c))
	if err != nil {
		return err
	}
	items := make([]blockedUserResponse, 0, len(blocks))
	for _, b := range blocks {
		items = append(items, blockedUserResponse{
			DID:         b.User.DID,
			Handle:      b.User.Handle,
			DisplayName: b.Profile.DisplayName,
			AvatarURL:   atproto.AvatarURL(b.User.DID, b.Profile.AvatarCID),
		})
	}
	return c.JSON(httpx.Page[blockedUserResponse]{Items: items})
}
