package controller

import (
	"github.com/gofiber/fiber/v3"

	"github.com/applemango/chain/internal/service"
)

type HealthController struct {
	Health *service.HealthService
}

func (h *HealthController) Register(r fiber.Router) {
	r.Get("/health", h.get)
}

func (h *HealthController) get(c fiber.Ctx) error {
	s := h.Health.Check(c.Context())
	if !s.OK() {
		return c.Status(fiber.StatusServiceUnavailable).JSON(s)
	}
	return c.JSON(s)
}
