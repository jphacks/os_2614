package controller

import (
	"github.com/gofiber/fiber/v3"

	"github.com/applemango/chain/internal/httpx"
	"github.com/applemango/chain/internal/service"
)

// AccountController はアカウント削除（13 §5）。
type AccountController struct {
	Account *service.AccountService
}

func (h *AccountController) Register(r fiber.Router) {
	r.Delete("/me", h.delete)
}

func (h *AccountController) delete(c fiber.Ctx) error {
	deletePds := false
	switch c.Query("deletePdsRecords") {
	case "", "false":
	case "true":
		deletePds = true
	default:
		return httpx.ErrInvalidRequest.With("fields", map[string]string{"deletePdsRecords": "true か false を指定してください"})
	}
	if err := h.Account.Delete(c.Context(), httpx.DID(c), deletePds); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}
