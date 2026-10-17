package controller

import (
	"strconv"

	"github.com/gofiber/fiber/v3"

	"github.com/applemango/chain/internal/httpx"
	"github.com/applemango/chain/internal/service"
	"github.com/applemango/chain/internal/storage"
)

// MediaDeliveryController は署名付き URL の配信（10 §4.4）。認証は Bearer ではなく署名で行う。
type MediaDeliveryController struct {
	Media *service.MediaService
}

func (h *MediaDeliveryController) Register(r fiber.Router) {
	r.Get("/media/:id", h.serve)
}

func (h *MediaDeliveryController) serve(c fiber.Ctx) error {
	exp, err := strconv.ParseInt(c.Query("exp"), 10, 64)
	if err != nil {
		return httpx.ErrForbidden
	}
	path, err := h.Media.Deliver(c.Context(), c.Params("id"), storage.Variant(c.Query("v")), exp, c.Query("sig"))
	if err != nil {
		return err
	}
	c.Set(fiber.HeaderCacheControl, "private, max-age=3600")
	c.Set(fiber.HeaderContentType, "image/jpeg")
	return c.SendFile(path)
}
