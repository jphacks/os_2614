package utils

import (
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
)

// Cursor は一覧のページングに使う不透明な文字列（02 §4）。
// (sortKey, id) を base64url にして返す。
func EncodeCursor(sortKey int64, id string) string {
	raw := strconv.FormatInt(sortKey, 10) + "|" + id
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

var ErrBadCursor = errors.New("bad cursor")

// DecodeCursor は EncodeCursor の逆。空文字はゼロ値（先頭）として扱う。
func DecodeCursor(s string) (int64, string, error) {
	if s == "" {
		return 0, "", nil
	}
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return 0, "", ErrBadCursor
	}
	key, id, ok := strings.Cut(string(b), "|")
	if !ok {
		return 0, "", ErrBadCursor
	}
	n, err := strconv.ParseInt(key, 10, 64)
	if err != nil {
		return 0, "", ErrBadCursor
	}
	return n, id, nil
}
