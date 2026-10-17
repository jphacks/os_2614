package httpx

import (
	"github.com/gofiber/fiber/v3"
)

// Validator は Bind 後に呼ばれる入力検証。問題のある項目と理由を返す。
type Validator interface {
	Validate() map[string]string
}

// Bind は JSON 本文を v に読み、Validator を実装していれば検証する。
// 失敗したら 400 invalid_request（details.fields に項目ごとの理由）を返す。
func Bind(c fiber.Ctx, v any) error {
	if err := c.Bind().JSON(v); err != nil {
		return ErrInvalidRequest.With("reason", "JSON を読めません")
	}
	if val, ok := v.(Validator); ok {
		if fields := val.Validate(); len(fields) > 0 {
			return ErrInvalidRequest.With("fields", fields)
		}
	}
	return nil
}

// Page は一覧のレスポンス形式（02 §4）。
type Page[T any] struct {
	Items  []T    `json:"items"`
	Cursor string `json:"cursor,omitempty"`
}
