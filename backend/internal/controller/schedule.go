package controller

import (
	"fmt"

	"github.com/gofiber/fiber/v3"

	"github.com/applemango/chain/internal/httpx"
	naturalsvc "github.com/applemango/chain/internal/service/natural"
)

// ScheduleController は今日・明日の予定（09 §8.2、14「自分」）。
type ScheduleController struct {
	Schedule *naturalsvc.ScheduleService
}

func (h *ScheduleController) Register(r fiber.Router) {
	r.Put("/me/schedule-today", h.put)
	r.Get("/me/schedule-today", h.get)
	r.Delete("/me/schedule-today", h.delete)
}

type scheduleEntryRequest struct {
	Title string `json:"title"`
	Start string `json:"start"`
	End   string `json:"end"`
}

type schedulePutRequest struct {
	Date    string                 `json:"date"`
	Source  string                 `json:"source"`
	Entries []scheduleEntryRequest `json:"entries"`
}

type scheduleEntryResponse struct {
	Source string `json:"source"`
	Title  string `json:"title"`
	Start  string `json:"start"`
	End    string `json:"end"`
}

type scheduleResponse struct {
	Date    string                  `json:"date"`
	Entries []scheduleEntryResponse `json:"entries"`
}

func scheduleEntriesResponse(day *naturalsvc.ScheduleDay) scheduleResponse {
	out := make([]scheduleEntryResponse, 0, len(day.Entries))
	for _, e := range day.Entries {
		out = append(out, scheduleEntryResponse{
			Source: e.Source,
			Title:  e.Title,
			Start:  formatClock(e.StartMin),
			End:    formatClock(e.EndMin),
		})
	}
	return scheduleResponse{Date: day.Date, Entries: out}
}

// formatClock は 0:00 からの分を HH:MM にする。"24:00" もそのまま返す。
func formatClock(min int) string { return fmt.Sprintf("%02d:%02d", min/60, min%60) }

func (h *ScheduleController) put(c fiber.Ctx) error {
	var req schedulePutRequest
	if err := httpx.Bind(c, &req); err != nil {
		return err
	}
	inputs := make([]naturalsvc.ScheduleInput, len(req.Entries))
	for i, e := range req.Entries {
		inputs[i] = naturalsvc.ScheduleInput{Title: e.Title, Start: e.Start, End: e.End}
	}
	day, err := h.Schedule.ReplaceToday(c.Context(), httpx.DID(c), req.Date, req.Source, inputs)
	if err != nil {
		return err
	}
	return c.JSON(scheduleEntriesResponse(day))
}

func (h *ScheduleController) get(c fiber.Ctx) error {
	day, err := h.Schedule.List(c.Context(), httpx.DID(c), c.Query("date"))
	if err != nil {
		return err
	}
	return c.JSON(scheduleEntriesResponse(day))
}

func (h *ScheduleController) delete(c fiber.Ctx) error {
	if err := h.Schedule.Delete(c.Context(), httpx.DID(c), c.Query("date"), c.Query("source")); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}
