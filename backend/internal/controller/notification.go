package controller

import (
	"encoding/json"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/applemango/chain/internal/httpx"
	notifysvc "github.com/applemango/chain/internal/service/notify"
)

// NotificationController は通知の一覧と既読化（12 §1、14「memory・通知・その他」）。
type NotificationController struct {
	Notifications *notifysvc.NotificationService
}

func (h *NotificationController) Register(r fiber.Router) {
	r.Get("/notifications", h.list)
	r.Post("/notifications/read", h.read)
}

type notificationResponse struct {
	ID        string          `json:"id"`
	Type      string          `json:"type"`
	Data      json.RawMessage `json:"data"`
	CreatedAt time.Time       `json:"createdAt"`
	ReadAt    *time.Time      `json:"readAt,omitempty"`
}

type notificationsPageResponse struct {
	Items  []notificationResponse `json:"items"`
	Cursor string                 `json:"cursor,omitempty"`
}

func (h *NotificationController) list(c fiber.Ctx) error {
	limit := 0
	if raw := c.Query("limit"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil {
			return httpx.ErrInvalidRequest.With("fields", map[string]string{"limit": "整数で指定してください"})
		}
		limit = v
	}
	page, err := h.Notifications.List(c.Context(), httpx.DID(c), c.Query("cursor"), limit)
	if err != nil {
		return err
	}
	items := make([]notificationResponse, 0, len(page.Items))
	for _, n := range page.Items {
		items = append(items, notificationResponse{
			ID: n.ID, Type: n.Type, Data: n.Data, CreatedAt: n.CreatedAt, ReadAt: n.ReadAt,
		})
	}
	return c.JSON(notificationsPageResponse{Items: items, Cursor: page.Cursor})
}

type readNotificationsRequest struct {
	IDs []string `json:"ids"`
	All bool     `json:"all"`
}

func (r *readNotificationsRequest) Validate() map[string]string {
	if !r.All && len(r.IDs) == 0 {
		return map[string]string{"ids": "ids か all のどちらかが必要です"}
	}
	return nil
}

func (h *NotificationController) read(c fiber.Ctx) error {
	var req readNotificationsRequest
	if err := httpx.Bind(c, &req); err != nil {
		return err
	}
	updated, err := h.Notifications.MarkRead(c.Context(), httpx.DID(c), req.IDs, req.All)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"updated": updated})
}
