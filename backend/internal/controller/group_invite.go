package controller

import (
	"github.com/gofiber/fiber/v3"

	"github.com/applemango/chain/internal/httpx"
	"github.com/applemango/chain/internal/service"
)

// GroupInviteController は closed グループの招待（09 §6、14「グループ」）。
type GroupInviteController struct {
	Invites *service.GroupInviteService
}

func (h *GroupInviteController) Register(r fiber.Router) {
	r.Post("/groups/:id/invites", h.create)
	r.Get("/groups/:id/invites", h.list)
	r.Post("/groups/:id/invites/:inviteId/approve", h.approve)
	r.Delete("/groups/:id/invites/:inviteId", h.cancel)
	r.Get("/group-invites", h.inbox)
	r.Post("/group-invites/:inviteId/accept", h.accept)
	r.Post("/group-invites/:inviteId/decline", h.decline)
}

type createInviteRequest struct {
	DID string `json:"did"`
}

func (r *createInviteRequest) Validate() map[string]string {
	if r.DID == "" {
		return map[string]string{"did": "必須です"}
	}
	return nil
}

func (h *GroupInviteController) create(c fiber.Ctx) error {
	var req createInviteRequest
	if err := httpx.Bind(c, &req); err != nil {
		return err
	}
	item, err := h.Invites.Create(c.Context(), httpx.DID(c), c.Params("id"), req.DID)
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(item)
}

func (h *GroupInviteController) approve(c fiber.Ctx) error {
	item, err := h.Invites.Approve(c.Context(), httpx.DID(c), c.Params("id"), c.Params("inviteId"))
	if err != nil {
		return err
	}
	return c.JSON(item)
}

func (h *GroupInviteController) list(c fiber.Ctx) error {
	res, err := h.Invites.List(c.Context(), httpx.DID(c), c.Params("id"))
	if err != nil {
		return err
	}
	return c.JSON(res)
}

func (h *GroupInviteController) cancel(c fiber.Ctx) error {
	if err := h.Invites.Cancel(c.Context(), httpx.DID(c), c.Params("id"), c.Params("inviteId")); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *GroupInviteController) inbox(c fiber.Ctx) error {
	res, err := h.Invites.Inbox(c.Context(), httpx.DID(c))
	if err != nil {
		return err
	}
	return c.JSON(res)
}

func (h *GroupInviteController) accept(c fiber.Ctx) error {
	if err := h.Invites.Accept(c.Context(), httpx.DID(c), c.Params("inviteId")); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *GroupInviteController) decline(c fiber.Ctx) error {
	if err := h.Invites.Decline(c.Context(), httpx.DID(c), c.Params("inviteId")); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}
