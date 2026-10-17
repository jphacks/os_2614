package party

import (
	"strconv"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/applemango/chain/internal/httpx"
	partysvc "github.com/applemango/chain/internal/service/party"
)

// PartyController は 08 §6 の API を持つ。
type PartyController struct {
	Parties *partysvc.PartyService
}

func (h *PartyController) Register(r fiber.Router) {
	r.Get("/party/friend-beacons", h.friendBeacons)
	r.Post("/party/presence", h.presence)
	r.Get("/party/current", h.current)
	r.Post("/parties/:id/leave", h.leave)
	r.Post("/parties/:id/lock", h.lock)
	r.Post("/parties/:id/unlock", h.unlock)
	r.Patch("/parties/:id", h.rename)
}

type friendBeaconTokenResponse struct {
	WindowStart string `json:"windowStart"`
	Token       string `json:"token"`
}

type friendBeaconResponse struct {
	DID    string                      `json:"did"`
	Tokens []friendBeaconTokenResponse `json:"tokens"`
}

type friendBeaconsResponse struct {
	ValidUntil time.Time              `json:"validUntil"`
	Friends    []friendBeaconResponse `json:"friends"`
}

func (h *PartyController) friendBeacons(c fiber.Ctx) error {
	res, err := h.Parties.FriendBeacons(c.Context(), httpx.DID(c))
	if err != nil {
		return err
	}
	resp := friendBeaconsResponse{
		ValidUntil: res.ValidUntil,
		Friends:    make([]friendBeaconResponse, 0, len(res.Friends)),
	}
	for _, f := range res.Friends {
		item := friendBeaconResponse{DID: f.DID, Tokens: make([]friendBeaconTokenResponse, 0, len(f.Tokens))}
		for _, t := range f.Tokens {
			item.Tokens = append(item.Tokens, friendBeaconTokenResponse{
				WindowStart: t.WindowStart.Format(time.RFC3339),
				Token:       t.Token,
			})
		}
		resp.Friends = append(resp.Friends, item)
	}
	return c.JSON(resp)
}

type presenceObservationRequest struct {
	DID        string `json:"did"`
	LastSeenAt string `json:"lastSeenAt"`
	RSSI       *int   `json:"rssi"`
}

type presenceRequest struct {
	Observations []presenceObservationRequest `json:"observations"`
}

func (r *presenceRequest) Validate() map[string]string {
	fields := map[string]string{}
	for i, o := range r.Observations {
		if o.DID == "" {
			fields["observations."+strconv.Itoa(i)+".did"] = "did が必要です"
		}
		if _, err := time.Parse(time.RFC3339, o.LastSeenAt); err != nil {
			fields["observations."+strconv.Itoa(i)+".lastSeenAt"] = "RFC 3339 の時刻を指定してください"
		}
	}
	return fields
}

func (h *PartyController) presence(c fiber.Ctx) error {
	var req presenceRequest
	if err := httpx.Bind(c, &req); err != nil {
		return err
	}
	obs := make([]partysvc.PresenceObservation, 0, len(req.Observations))
	for _, o := range req.Observations {
		t, _ := time.Parse(time.RFC3339, o.LastSeenAt)
		obs = append(obs, partysvc.PresenceObservation{DID: o.DID, LastSeenAt: t, RSSI: o.RSSI})
	}
	if err := h.Parties.Presence(c.Context(), httpx.DID(c), obs); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *PartyController) current(c fiber.Ctx) error {
	detail, err := h.Parties.Current(c.Context(), httpx.DID(c))
	if err != nil {
		return err
	}
	if detail == nil {
		return c.JSON(nil)
	}
	return c.JSON(detail)
}

func (h *PartyController) leave(c fiber.Ctx) error {
	if err := h.Parties.Leave(c.Context(), httpx.DID(c), c.Params("id")); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *PartyController) lock(c fiber.Ctx) error {
	detail, err := h.Parties.SetLocked(c.Context(), httpx.DID(c), c.Params("id"), true)
	if err != nil {
		return err
	}
	return c.JSON(detail)
}

func (h *PartyController) unlock(c fiber.Ctx) error {
	detail, err := h.Parties.SetLocked(c.Context(), httpx.DID(c), c.Params("id"), false)
	if err != nil {
		return err
	}
	return c.JSON(detail)
}

type renamePartyRequest struct {
	Name string `json:"name"`
}

func (h *PartyController) rename(c fiber.Ctx) error {
	var req renamePartyRequest
	if err := httpx.Bind(c, &req); err != nil {
		return err
	}
	detail, err := h.Parties.Rename(c.Context(), httpx.DID(c), c.Params("id"), req.Name)
	if err != nil {
		return err
	}
	return c.JSON(detail)
}
