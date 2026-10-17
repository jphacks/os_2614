package controller

import (
	"strconv"

	"github.com/gofiber/fiber/v3"
)

// PageLimit は 02 §4 の limit（既定 30、最大 100）。
// 複数の機能フォルダーのコントローラーが使うため、ルートに置いて公開する。
func PageLimit(c fiber.Ctx) int {
	n, err := strconv.Atoi(c.Query("limit"))
	if err != nil || n <= 0 {
		return 30
	}
	if n > 100 {
		return 100
	}
	return n
}
