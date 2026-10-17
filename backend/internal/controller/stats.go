package controller

import (
	"github.com/gofiber/fiber/v3"

	"github.com/applemango/chain/internal/httpx"
	"github.com/applemango/chain/internal/service"
)

// StatsController は個人の統計（13 §3、14「自分」）。
type StatsController struct {
	Stats *service.StatsService
}

func (h *StatsController) Register(r fiber.Router) {
	r.Get("/me/stats", h.get)
}

func (h *StatsController) get(c fiber.Ctx) error {
	st, err := h.Stats.Get(c.Context(), httpx.DID(c))
	if err != nil {
		return err
	}
	return c.JSON(st)
}
