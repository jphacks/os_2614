package party

import (
	"github.com/gofiber/fiber/v3"

	"github.com/applemango/chain/internal/controller"
	"github.com/applemango/chain/internal/httpx"
	partysvc "github.com/applemango/chain/internal/service/party"
)

// MemoryController は GET /memories と DELETE /memories/{spaceId}（13 §2）。
type MemoryController struct {
	Parties *partysvc.PartyService
}

func (h *MemoryController) Register(r fiber.Router) {
	r.Get("/memories", h.list)
	r.Delete("/memories/:spaceId", h.hide)
}

func (h *MemoryController) list(c fiber.Ctx) error {
	page, err := h.Parties.Memories(c.Context(), httpx.DID(c), c.Query("cursor"), controller.PageLimit(c))
	if err != nil {
		return err
	}
	return c.JSON(page)
}

func (h *MemoryController) hide(c fiber.Ctx) error {
	if err := h.Parties.HideMemory(c.Context(), httpx.DID(c), c.Params("spaceId")); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}
