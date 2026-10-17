package controller

import (
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/applemango/chain/internal/httpx"
	"github.com/applemango/chain/internal/service"
)

type MediaController struct {
	Media *service.MediaService
}

func (h *MediaController) Register(r fiber.Router) {
	r.Post("/media", h.upload)
	r.Post("/instances/:spaceId/media", h.share)
}

func (h *MediaController) upload(c fiber.Ctx) error {
	header, err := c.FormFile("file")
	if err != nil {
		return httpx.ErrInvalidRequest.With("fields", map[string]string{"file": "file が必要です"})
	}
	f, err := header.Open()
	if err != nil {
		return httpx.ErrInvalidRequest.With("fields", map[string]string{"file": "file を開けません"})
	}
	defer f.Close()

	takenAt, err := parseTakenAt(c.FormValue("takenAt"))
	if err != nil {
		return err
	}
	up, err := h.Media.Upload(c.Context(), httpx.DID(c), f, header.Size, takenAt)
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(up)
}

type shareMediaRequest struct {
	MediaID string  `json:"mediaId"`
	TakenAt *string `json:"takenAt"`
}

func (r *shareMediaRequest) Validate() map[string]string {
	f := map[string]string{}
	if r.MediaID == "" {
		f["mediaId"] = "必須です"
	}
	if r.TakenAt != nil {
		if _, err := parseTakenAt(*r.TakenAt); err != nil {
			f["takenAt"] = "RFC 3339 の時刻を指定してください"
		}
	}
	return f
}

func (h *MediaController) share(c fiber.Ctx) error {
	var req shareMediaRequest
	if err := httpx.Bind(c, &req); err != nil {
		return err
	}
	takenAt, _ := parseTakenAt(deref(req.TakenAt))
	shared, err := h.Media.ShareToSpace(c.Context(), httpx.DID(c), c.Params("spaceId"), req.MediaID, takenAt)
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"uri": shared.URI, "spaceId": shared.SpaceID, "mediaId": shared.MediaID,
	})
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func parseTakenAt(s string) (*time.Time, error) {
	if s == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return nil, httpx.ErrInvalidRequest.With("fields", map[string]string{"takenAt": "RFC 3339 の時刻を指定してください"})
	}
	return &t, nil
}
