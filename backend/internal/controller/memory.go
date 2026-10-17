package controller

import (
	"github.com/gofiber/fiber/v3"

	"github.com/applemango/chain/internal/httpx"
	"github.com/applemango/chain/internal/service"
)

// MemoryController は GET /memories（13 §2）。P0 は archived の Party だけを返す。
type MemoryController struct {
	Parties *service.PartyService
}

func (h *MemoryController) Register(r fiber.Router) {
	r.Get("/memories", h.list)
}

func (h *MemoryController) list(c fiber.Ctx) error {
	page, err := h.Parties.Memories(c.Context(), httpx.DID(c), c.Query("cursor"), pageLimit(c))
	if err != nil {
		return err
	}
	return c.JSON(page)
}
