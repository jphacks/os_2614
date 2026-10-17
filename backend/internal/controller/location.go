package controller

import (
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/applemango/chain/internal/httpx"
	"github.com/applemango/chain/internal/service"
)

type LocationController struct {
	Location *service.LocationService
}

func (h *LocationController) Register(r fiber.Router) {
	r.Put("/me/location", h.update)
}

type locationRequest struct {
	Lat        *float64   `json:"lat"`
	Lon        *float64   `json:"lon"`
	AccuracyM  *float64   `json:"accuracyM"`
	RecordedAt *time.Time `json:"recordedAt"`
	Source     string     `json:"source"`
	IsMock     bool       `json:"isMock"`
}

func (r *locationRequest) Validate() map[string]string {
	fields := map[string]string{}
	if r.Lat == nil {
		fields["lat"] = "必須です"
	}
	if r.Lon == nil {
		fields["lon"] = "必須です"
	}
	if r.RecordedAt == nil {
		fields["recordedAt"] = "必須です"
	}
	if len(fields) == 0 {
		return nil
	}
	return fields
}

func (h *LocationController) update(c fiber.Ctx) error {
	var req locationRequest
	if err := httpx.Bind(c, &req); err != nil {
		return err
	}
	accuracy := 0.0
	if req.AccuracyM != nil {
		accuracy = *req.AccuracyM
	}
	if _, err := h.Location.Update(c.Context(), httpx.DID(c), service.LocationInput{
		Lat: *req.Lat, Lon: *req.Lon, AccuracyM: accuracy,
		RecordedAt: *req.RecordedAt, Source: req.Source, IsMock: req.IsMock,
	}); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}
