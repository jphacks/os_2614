package controller

import (
	"github.com/gofiber/fiber/v3"

	"github.com/applemango/chain/internal/httpx"
	"github.com/applemango/chain/internal/service"
)

type GroupController struct {
	Groups *service.GroupService
}

func (h *GroupController) Register(r fiber.Router) {
	r.Get("/groups/nearby", h.nearby)
	r.Post("/groups", h.create)
	r.Get("/groups/:id", h.get)
	r.Patch("/groups/:id", h.update)
	r.Post("/groups/:id/join", h.join)
	r.Post("/groups/:id/leave", h.leave)
	r.Get("/groups/:id/members", h.members)
}

type geofenceRequest struct {
	Lat     float64 `json:"lat"`
	Lon     float64 `json:"lon"`
	RadiusM int     `json:"radiusM"`
}

type createGroupRequest struct {
	Name         string                 `json:"name"`
	Description  string                 `json:"description"`
	Visibility   string                 `json:"visibility"`
	Persistent   bool                   `json:"persistent"`
	Geofence     *geofenceRequest       `json:"geofence"`
	Schedule     *service.GroupSchedule `json:"schedule"`
	InviteQuorum int                    `json:"inviteQuorum"`
	Force        bool                   `json:"force"`
}

func (r *createGroupRequest) Validate() map[string]string {
	if r.Geofence == nil {
		return map[string]string{"geofence": "必須です"}
	}
	return nil
}

func (h *GroupController) create(c fiber.Ctx) error {
	var req createGroupRequest
	if err := httpx.Bind(c, &req); err != nil {
		return err
	}
	detail, err := h.Groups.Create(c.Context(), httpx.DID(c), service.CreateGroupInput{
		Name: req.Name, Description: req.Description, Visibility: req.Visibility,
		Persistent: req.Persistent, Lat: req.Geofence.Lat, Lon: req.Geofence.Lon,
		RadiusM: req.Geofence.RadiusM, Schedule: req.Schedule,
		InviteQuorum: req.InviteQuorum, Force: req.Force,
	})
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(detail)
}

func (h *GroupController) get(c fiber.Ctx) error {
	detail, err := h.Groups.Get(c.Context(), httpx.DID(c), c.Params("id"))
	if err != nil {
		return err
	}
	return c.JSON(detail)
}

type updateGroupRequest struct {
	Name         *string                `json:"name"`
	Description  *string                `json:"description"`
	Schedule     *service.GroupSchedule `json:"schedule"`
	InviteQuorum *int                   `json:"inviteQuorum"`
	Geofence     *geofenceRequest       `json:"geofence"`
}

func (h *GroupController) update(c fiber.Ctx) error {
	var req updateGroupRequest
	if err := httpx.Bind(c, &req); err != nil {
		return err
	}
	if req.Geofence != nil {
		return httpx.ErrInvalidRequest.With("fields", map[string]string{"geofence": "変更できません。作り直してください"})
	}
	detail, err := h.Groups.Update(c.Context(), httpx.DID(c), c.Params("id"), service.UpdateGroupInput{
		Name: req.Name, Description: req.Description, Schedule: req.Schedule, InviteQuorum: req.InviteQuorum,
	})
	if err != nil {
		return err
	}
	return c.JSON(detail)
}

func (h *GroupController) nearby(c fiber.Ctx) error {
	res, err := h.Groups.Nearby(c.Context(), httpx.DID(c))
	if err != nil {
		return err
	}
	return c.JSON(res)
}

func (h *GroupController) join(c fiber.Ctx) error {
	if err := h.Groups.Join(c.Context(), httpx.DID(c), c.Params("id")); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *GroupController) leave(c fiber.Ctx) error {
	if err := h.Groups.Leave(c.Context(), httpx.DID(c), c.Params("id")); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *GroupController) members(c fiber.Ctx) error {
	items, err := h.Groups.Members(c.Context(), httpx.DID(c), c.Params("id"))
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"items": items})
}
