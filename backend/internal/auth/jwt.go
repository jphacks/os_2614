package auth

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// アクセストークンの仕様は specification/backend/03-auth-atproto.md §3。

const AccessTTL = 15 * time.Minute

type Claims struct {
	DID       string
	SessionID string
	ExpiresAt time.Time
}

type accessClaims struct {
	SessionID string `json:"sid"`
	jwt.RegisteredClaims
}

var ErrInvalidToken = errors.New("invalid access token")

func IssueAccess(key []byte, did, sid string, now time.Time) (string, error) {
	c := accessClaims{
		SessionID: sid,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   did,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(AccessTTL)),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, c).SignedString(key)
}

func ParseAccess(key []byte, token string, now time.Time) (Claims, error) {
	var c accessClaims
	_, err := jwt.ParseWithClaims(token, &c, func(*jwt.Token) (any, error) { return key, nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithTimeFunc(func() time.Time { return now }),
		jwt.WithExpirationRequired(),
	)
	if err != nil || c.Subject == "" || c.SessionID == "" {
		return Claims{}, ErrInvalidToken
	}
	return Claims{DID: c.Subject, SessionID: c.SessionID, ExpiresAt: c.ExpiresAt.Time}, nil
}
