package controller

import (
	"strconv"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/applemango/chain/internal/httpx"
	"github.com/applemango/chain/internal/service"
)

type BeaconController struct {
	Beacon *service.BeaconService
}

func (h *BeaconController) Register(r fiber.Router) {
	r.Get("/beacon/tokens", h.tokens)
}

type beaconTokensResponse struct {
	WindowSec int                   `json:"windowSec"`
	Tokens    []beaconTokenResponse `json:"tokens"`
}

type beaconTokenResponse struct {
	WindowStart string `json:"windowStart"`
	Token       string `json:"token"`
}

func (h *BeaconController) tokens(c fiber.Ctx) error {
	hours := 24
	if raw := c.Query("hours"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil {
			return httpx.ErrInvalidRequest.With("fields", map[string]string{"hours": "整数で指定してください"})
		}
		hours = v
	}

	windowSec, tokens, err := h.Beacon.Tokens(c.Context(), httpx.DID(c), hours)
	if err != nil {
		return err
	}
	resp := beaconTokensResponse{WindowSec: windowSec, Tokens: make([]beaconTokenResponse, 0, len(tokens))}
	for _, t := range tokens {
		resp.Tokens = append(resp.Tokens, beaconTokenResponse{
			WindowStart: t.WindowStart.Format(time.RFC3339),
			Token:       t.Token,
		})
	}
	return c.JSON(resp)
}
