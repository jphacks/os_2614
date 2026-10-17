package friend

import (
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/gofiber/fiber/v3"

	"github.com/applemango/chain/internal/httpx"
	core "github.com/applemango/chain/internal/service/core"
	friendsvc "github.com/applemango/chain/internal/service/friend"
)

// FriendController はフレンドの 3 経路・申請・一覧・削除（07 §2）。
type FriendController struct {
	Friends *friendsvc.FriendService
}

func (h *FriendController) Register(r fiber.Router) {
	r.Post("/friends/invites", h.createInvite)
	r.Post("/friends/invites/:code/accept", h.acceptInvite)
	r.Post("/friends/requests", h.createRequest)
	r.Get("/friends/requests", h.listRequests)
	r.Post("/friends/requests/:id/accept", h.acceptRequest)
	r.Post("/friends/requests/:id/decline", h.declineRequest)
	r.Delete("/friends/requests/:id", h.cancelRequest)
	r.Get("/friends", h.list)
	r.Delete("/friends/:did", h.delete)
}

type userSummaryResponse struct {
	DID         string `json:"did"`
	Handle      string `json:"handle"`
	DisplayName string `json:"displayName,omitempty"`
	AvatarURL   string `json:"avatarUrl,omitempty"`
}

type friendResponse struct {
	DID         string    `json:"did"`
	Handle      string    `json:"handle"`
	DisplayName string    `json:"displayName,omitempty"`
	AvatarURL   string    `json:"avatarUrl,omitempty"`
	Since       time.Time `json:"since"`
	Precision   string    `json:"precision"`
}

type friendRequestResponse struct {
	ID        string              `json:"id"`
	Peer      userSummaryResponse `json:"peer"`
	Via       string              `json:"via"`
	ViaRef    string              `json:"viaRef,omitempty"`
	Status    string              `json:"status"`
	CreatedAt time.Time           `json:"createdAt"`
}

type friendInviteResponse struct {
	Code      string    `json:"code"`
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expiresAt"`
}

func userSummary(s core.UserSummary) userSummaryResponse {
	return userSummaryResponse{
		DID: s.DID, Handle: s.Handle, DisplayName: s.DisplayName, AvatarURL: s.AvatarURL,
	}
}

func friendResponseFrom(f friendsvc.FriendItem) friendResponse {
	return friendResponse{
		DID: f.DID, Handle: f.Handle, DisplayName: f.DisplayName, AvatarURL: f.AvatarURL,
		Since: f.Since, Precision: f.Precision,
	}
}

func friendRequestResponseFrom(r friendsvc.FriendRequestItem) friendRequestResponse {
	return friendRequestResponse{
		ID: r.ID, Peer: userSummary(r.Peer), Via: r.Via, ViaRef: r.ViaRef,
		Status: r.Status, CreatedAt: r.CreatedAt,
	}
}

func (h *FriendController) createInvite(c fiber.Ctx) error {
	inv, err := h.Friends.CreateInvite(c.Context(), httpx.DID(c))
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(friendInviteResponse{
		Code: inv.Code, URL: inv.URL, ExpiresAt: inv.ExpiresAt,
	})
}

func (h *FriendController) acceptInvite(c fiber.Ctx) error {
	friend, err := h.Friends.AcceptInvite(c.Context(), httpx.DID(c), c.Params("code"))
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"friend": friendResponseFrom(*friend)})
}

type friendRequestBody struct {
	DID    string `json:"did"`
	Via    string `json:"via"`
	ViaRef string `json:"viaRef"`
}

func (r *friendRequestBody) Validate() map[string]string {
	f := map[string]string{}
	if r.DID == "" {
		f["did"] = "必須です"
	} else if _, err := syntax.ParseDID(r.DID); err != nil {
		f["did"] = "DID の形式が不正です"
	}
	if r.Via != "encounter" && r.Via != "space" {
		f["via"] = "encounter / space のいずれかです"
	}
	if r.ViaRef == "" {
		f["viaRef"] = "必須です"
	}
	return f
}

func (h *FriendController) createRequest(c fiber.Ctx) error {
	var req friendRequestBody
	if err := httpx.Bind(c, &req); err != nil {
		return err
	}
	item, err := h.Friends.CreateRequest(c.Context(), httpx.DID(c), friendsvc.CreateRequestInput{
		DID: req.DID, Via: req.Via, ViaRef: req.ViaRef,
	})
	if err != nil {
		return err
	}
	status := fiber.StatusCreated
	if item.Status == "accepted" {
		status = fiber.StatusOK
	}
	return c.Status(status).JSON(friendRequestResponseFrom(*item))
}

func (h *FriendController) listRequests(c fiber.Ctx) error {
	box := c.Query("box")
	if box == "" {
		box = "incoming"
	}
	items, err := h.Friends.ListRequests(c.Context(), httpx.DID(c), box)
	if err != nil {
		return err
	}
	out := make([]friendRequestResponse, 0, len(items))
	for _, it := range items {
		out = append(out, friendRequestResponseFrom(it))
	}
	return c.JSON(httpx.Page[friendRequestResponse]{Items: out})
}

func (h *FriendController) acceptRequest(c fiber.Ctx) error {
	friend, err := h.Friends.AcceptRequest(c.Context(), httpx.DID(c), c.Params("id"))
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"friend": friendResponseFrom(*friend)})
}

func (h *FriendController) declineRequest(c fiber.Ctx) error {
	if err := h.Friends.DeclineRequest(c.Context(), httpx.DID(c), c.Params("id")); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *FriendController) cancelRequest(c fiber.Ctx) error {
	if err := h.Friends.CancelRequest(c.Context(), httpx.DID(c), c.Params("id")); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *FriendController) list(c fiber.Ctx) error {
	items, err := h.Friends.List(c.Context(), httpx.DID(c))
	if err != nil {
		return err
	}
	out := make([]friendResponse, 0, len(items))
	for _, it := range items {
		out = append(out, friendResponseFrom(it))
	}
	return c.JSON(httpx.Page[friendResponse]{Items: out})
}

func (h *FriendController) delete(c fiber.Ctx) error {
	did, err := syntax.ParseDID(c.Params("did"))
	if err != nil {
		return httpx.ErrInvalidRequest.With("fields", map[string]string{"did": "DID の形式が不正です"})
	}
	if err := h.Friends.Delete(c.Context(), httpx.DID(c), did.String()); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}
