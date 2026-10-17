package fcm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"

	"github.com/golang-jwt/jwt/v5"

	"github.com/applemango/chain/internal/clock"
)

// serviceAccount は Google のサービスアカウント JSON のうち必要なフィールドだけを読む。
type serviceAccount struct {
	ProjectID  string `json:"project_id"`
	ClientMail string `json:"client_email"`
	PrivateKey string `json:"private_key"`
}

// NewClient reads a Google service-account JSON file (fields project_id, client_email, private_key)
// and returns a Client with default TokenURL/BaseURL/HTTP. Returns an error if the file cannot be read/parsed.
func NewClient(ctx context.Context, credPath string, clk clock.Clock) (*Client, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(credPath)
	if err != nil {
		return nil, fmt.Errorf("fcm: read credentials: %w", err)
	}
	var sa serviceAccount
	if err := json.Unmarshal(raw, &sa); err != nil {
		return nil, fmt.Errorf("fcm: parse credentials: %w", err)
	}
	if sa.ProjectID == "" || sa.ClientMail == "" || sa.PrivateKey == "" {
		return nil, errors.New("fcm: credentials missing project_id, client_email or private_key")
	}
	key, err := jwt.ParseRSAPrivateKeyFromPEM([]byte(sa.PrivateKey))
	if err != nil {
		return nil, fmt.Errorf("fcm: parse private key: %w", err)
	}
	return &Client{
		ProjectID:  sa.ProjectID,
		Email:      sa.ClientMail,
		PrivateKey: key,
		TokenURL:   defaultTokenURL,
		BaseURL:    defaultBaseURL,
		HTTP:       &http.Client{},
		Clock:      clk,
	}, nil
}
