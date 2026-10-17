package controller

import (
	"strings"

	"github.com/gofiber/fiber/v3"

	"github.com/applemango/chain/internal/httpx"
	"github.com/applemango/chain/internal/service"
)

// DeviceController は端末の登録・削除（12 §2、14「自分」）。
type DeviceController struct {
	Devices *service.DeviceService
}

func (h *DeviceController) Register(r fiber.Router) {
	r.Put("/me/devices", h.put)
	r.Delete("/me/devices/:fcmToken", h.delete)
}

type deviceRequest struct {
	FCMToken string `json:"fcmToken"`
	Platform string `json:"platform"`
}

func (r *deviceRequest) Validate() map[string]string {
	f := map[string]string{}
	if strings.TrimSpace(r.FCMToken) == "" {
		f["fcmToken"] = "必須です"
	}
	switch r.Platform {
	case "", "android", "ios":
	default:
		f["platform"] = "android / ios のいずれかです"
	}
	if len(f) == 0 {
		return nil
	}
	return f
}

func (h *DeviceController) put(c fiber.Ctx) error {
	var req deviceRequest
	if err := httpx.Bind(c, &req); err != nil {
		return err
	}
	if err := h.Devices.RegisterDevice(c.Context(), httpx.DID(c), httpx.SessionID(c), req.FCMToken, req.Platform); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *DeviceController) delete(c fiber.Ctx) error {
	if err := h.Devices.UnregisterDevice(c.Context(), httpx.DID(c), c.Params("fcmToken")); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}
