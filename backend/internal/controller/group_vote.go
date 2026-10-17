package controller

import (
	"github.com/gofiber/fiber/v3"

	"github.com/applemango/chain/internal/httpx"
	"github.com/applemango/chain/internal/service"
)

type GroupVoteController struct {
	Votes *service.GroupVoteService
}

func (h *GroupVoteController) Register(r fiber.Router) {
	r.Post("/groups/:id/votes", h.propose)
	r.Get("/groups/:id/votes", h.list)
	r.Post("/groups/:id/votes/:voteId/ballot", h.ballot)
}

type proposeVoteRequest struct {
	Kind string `json:"kind"`
}

func (r *proposeVoteRequest) Validate() map[string]string {
	if r.Kind != "make_closed" && r.Kind != "make_persistent" {
		return map[string]string{"kind": "make_closed か make_persistent を指定してください"}
	}
	return nil
}

type castBallotRequest struct {
	Yes bool `json:"yes"`
}

func (h *GroupVoteController) propose(c fiber.Ctx) error {
	var req proposeVoteRequest
	if err := httpx.Bind(c, &req); err != nil {
		return err
	}
	item, err := h.Votes.Propose(c.Context(), httpx.DID(c), c.Params("id"), req.Kind)
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(item)
}

func (h *GroupVoteController) ballot(c fiber.Ctx) error {
	var req castBallotRequest
	if err := httpx.Bind(c, &req); err != nil {
		return err
	}
	item, err := h.Votes.CastBallot(c.Context(), httpx.DID(c), c.Params("id"), c.Params("voteId"), req.Yes)
	if err != nil {
		return err
	}
	return c.JSON(item)
}

func (h *GroupVoteController) list(c fiber.Ctx) error {
	res, err := h.Votes.List(c.Context(), httpx.DID(c), c.Params("id"))
	if err != nil {
		return err
	}
	return c.JSON(res)
}
