package controller

import (
	"github.com/gofiber/fiber/v3"

	"github.com/applemango/chain/internal/httpx"
	"github.com/applemango/chain/internal/service"
)

// FeedController は共通のフィード（17 §3.1）。
type FeedController struct {
	Feed *service.FeedService
}

func (h *FeedController) Register(r fiber.Router) {
	r.Get("/feed", h.list)
}

func (h *FeedController) list(c fiber.Ctx) error {
	page, err := h.Feed.List(c.Context(), httpx.DID(c), c.Query("source"), c.Query("cursor"), pageLimit(c))
	if err != nil {
		return err
	}
	return c.JSON(page)
}
