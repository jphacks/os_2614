// Package httpx は controller 共通のエラー形式・レスポンス・ミドルウェアを持つ。
// 形式は specification/backend/02-conventions.md §3。
package httpx

import (
	"errors"
	"log/slog"

	"github.com/gofiber/fiber/v3"
)

// Error は API が返すエラー。service はこれを返し、ErrorHandler が JSON にする。
type Error struct {
	Status  int            `json:"-"`
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

func (e *Error) With(key string, v any) *Error {
	cp := *e
	cp.Details = map[string]any{}
	for k, x := range e.Details {
		cp.Details[k] = x
	}
	cp.Details[key] = v
	return &cp
}

func NewError(status int, code, message string) *Error {
	return &Error{Status: status, Code: code, Message: message}
}

// 02 §3 の code 一覧。
var (
	ErrInvalidRequest    = NewError(fiber.StatusBadRequest, "invalid_request", "リクエストが不正です")
	ErrUnauthenticated   = NewError(fiber.StatusUnauthorized, "unauthenticated", "認証が必要です")
	ErrATProtoSession    = NewError(fiber.StatusUnauthorized, "atproto_session_expired", "再ログインが必要です")
	ErrForbidden         = NewError(fiber.StatusForbidden, "forbidden", "権限がありません")
	ErrNotInGeofence     = NewError(fiber.StatusForbidden, "not_in_geofence", "エリアの外です")
	ErrLocationStale     = NewError(fiber.StatusForbidden, "location_stale", "位置情報が古いか、ありません")
	ErrBlocked           = NewError(fiber.StatusForbidden, "blocked", "ブロック関係にあります")
	ErrConsentRequired   = NewError(fiber.StatusForbidden, "consent_required", "投稿内容の自動判定への同意が必要です")
	ErrNotFound          = NewError(fiber.StatusNotFound, "not_found", "見つかりません")
	ErrConflict          = NewError(fiber.StatusConflict, "conflict", "状態が競合しています")
	ErrGone              = NewError(fiber.StatusGone, "gone", "期限切れか、終了しています")
	ErrPayloadTooLarge   = NewError(fiber.StatusRequestEntityTooLarge, "payload_too_large", "サイズが大きすぎます")
	ErrModerationBlocked = NewError(fiber.StatusUnprocessableEntity, "moderation_blocked", "投稿できない内容が含まれています")
	ErrRateLimited       = NewError(fiber.StatusTooManyRequests, "rate_limited", "リクエストが多すぎます")
	ErrUpstream          = NewError(fiber.StatusBadGateway, "upstream_error", "外部サービスでエラーが発生しました")
	errInternal          = NewError(fiber.StatusInternalServerError, "internal", "サーバでエラーが発生しました")
)

// ErrorHandler は fiber.Config.ErrorHandler に渡す。
func ErrorHandler(c fiber.Ctx, err error) error {
	var e *Error
	var fe *fiber.Error
	switch {
	case errors.As(err, &e):
	case errors.As(err, &fe):
		e = NewError(fe.Code, fiberCode(fe.Code), fe.Message)
	default:
		slog.ErrorContext(c.Context(), "unhandled error", "err", err, "route", c.Route().Path)
		e = errInternal
	}
	return c.Status(e.Status).JSON(fiber.Map{"error": e})
}

func fiberCode(status int) string {
	switch status {
	case fiber.StatusNotFound:
		return "not_found"
	case fiber.StatusMethodNotAllowed:
		return "method_not_allowed"
	case fiber.StatusRequestEntityTooLarge:
		return "payload_too_large"
	default:
		if status >= 500 {
			return "internal"
		}
		return "invalid_request"
	}
}
