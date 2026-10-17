package encounter

import (
	"strconv"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/applemango/chain/internal/httpx"
	core "github.com/applemango/chain/internal/service/core"
	encountersvc "github.com/applemango/chain/internal/service/encounter"
)

type EncounterController struct {
	Encounters *encountersvc.EncounterService
}

func (h *EncounterController) Register(r fiber.Router) {
	r.Post("/encounters/sightings", h.sightings)
	r.Get("/encounters", h.list)
	r.Get("/encounters/summary", h.summary)
}

type sightingsRequest struct {
	Items []sightingItem `json:"items"`
}

type sightingItem struct {
	Token       string    `json:"token"`
	FirstSeenAt time.Time `json:"firstSeenAt"`
	LastSeenAt  time.Time `json:"lastSeenAt"`
	MaxRssi     *int      `json:"maxRssi"`
	Count       int       `json:"count"`
}

func (r *sightingsRequest) Validate() map[string]string {
	if len(r.Items) > core.SightingMax {
		return map[string]string{"items": "1 リクエストは 500 件までです"}
	}
	return nil
}

func (h *EncounterController) sightings(c fiber.Ctx) error {
	var req sightingsRequest
	if err := httpx.Bind(c, &req); err != nil {
		return err
	}
	items := make([]encountersvc.SightingInput, 0, len(req.Items))
	for _, it := range req.Items {
		items = append(items, encountersvc.SightingInput{
			Token: it.Token, FirstSeenAt: it.FirstSeenAt, LastSeenAt: it.LastSeenAt,
			MaxRssi: it.MaxRssi, Count: it.Count,
		})
	}
	res, err := h.Encounters.RecordSightings(c.Context(), httpx.DID(c), items)
	if err != nil {
		return err
	}
	return c.JSON(res)
}

func (h *EncounterController) list(c fiber.Ctx) error {
	limit := 0
	if raw := c.Query("limit"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil {
			return httpx.ErrInvalidRequest.With("fields", map[string]string{"limit": "整数で指定してください"})
		}
		limit = v
	}
	page, err := h.Encounters.List(c.Context(), httpx.DID(c), c.Query("cursor"), limit)
	if err != nil {
		return err
	}
	return c.JSON(page)
}

func (h *EncounterController) summary(c fiber.Ctx) error {
	sum, err := h.Encounters.Summary(c.Context(), httpx.DID(c))
	if err != nil {
		return err
	}
	return c.JSON(sum)
}
