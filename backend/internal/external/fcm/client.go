package fcm

import (
	"context"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/applemango/chain/internal/clock"
)

const (
	defaultTokenURL = "https://oauth2.googleapis.com/token"
	defaultBaseURL  = "https://fcm.googleapis.com"

	// fcmScope は HTTP v1 の送信に必要な OAuth スコープ。
	fcmScope = "https://www.googleapis.com/auth/firebase.messaging"

	// jwtBearerGrant はサービスアカウントの JWT をトークンに交換する grant_type。
	jwtBearerGrant = "urn:ietf:params:oauth:grant-type:jwt-bearer"

	// refreshSkew は期限前に更新する余裕（12 §2、キャッシュは now + expires_in - 60s）。
	refreshSkew = 60 * time.Second

	// snippetLimit はエラーに含めるレスポンス本文の上限バイト数。
	snippetLimit = 512
)

// Message is one push. Token/Title/Body are never logged by this package.
type Message struct {
	Token       string
	Title       string            // FCM "notification.title"
	Body        string            // FCM "notification.body"
	Data        map[string]string // FCM "data" (must include "type" per 12 §4)
	ChannelID   string            // Android notification channel ("social" / "digest")
	CollapseKey string            // FCM "android.collapse_key"
}

// ErrUnregistered is returned (wrapped so errors.Is works) when FCM reports the token is no longer registered.
var ErrUnregistered = errors.New("fcm: token unregistered")

// Sender sends one message. Consumers depend on this interface; tests inject a fake.
type Sender interface {
	Send(ctx context.Context, msg Message) error
}

// LogSender logs instead of sending. Used when CHAIN_FCM_CREDENTIALS is empty.
type LogSender struct{}

// Send は FCM トークン・本文をログに出さず、種別とチャンネルだけを記録する（12 §7 / 02 §7）。
func (LogSender) Send(ctx context.Context, msg Message) error {
	slog.InfoContext(ctx, "fcm: log sender",
		"type", msg.Data["type"],
		"channel", msg.ChannelID,
		"collapse_key", msg.CollapseKey,
	)
	return nil
}

// Client is the FCM HTTP v1 client.
type Client struct {
	ProjectID  string
	Email      string
	PrivateKey *rsa.PrivateKey
	TokenURL   string       // default https://oauth2.googleapis.com/token
	BaseURL    string       // default https://fcm.googleapis.com
	HTTP       *http.Client // injectable (tests inject httptest client); no Timeout
	Clock      clock.Clock  // injected; never call time.Now directly

	mu        sync.Mutex
	token     string
	expiresAt time.Time
}

// Send obtains an access token (cached, refreshed with a 60s skew) and posts one message.
func (c *Client) Send(ctx context.Context, msg Message) error {
	token, err := c.accessToken(ctx)
	if err != nil {
		return err
	}

	payload, err := json.Marshal(sendRequest{Message: buildMessage(msg)})
	if err != nil {
		return fmt.Errorf("fcm: marshal message: %w", err)
	}

	endpoint := c.baseURL() + "/v1/projects/" + url.PathEscape(c.ProjectID) + "/messages:send"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(payload)))
	if err != nil {
		return fmt.Errorf("fcm: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("fcm: send: %w", err)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	case resp.StatusCode == http.StatusNotFound:
		// FCM は失効トークンに 404 NOT_FOUND / errorCode UNREGISTERED を返す（12 §2）。
		return fmt.Errorf("%w: status %d: %s", ErrUnregistered, resp.StatusCode, snippet(resp.Body))
	default:
		return fmt.Errorf("fcm: send: status %d: %s", resp.StatusCode, snippet(resp.Body))
	}
}

// accessToken はキャッシュを排他制御しつつ有効なトークンを返す。
func (c *Client) accessToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := c.now()
	if c.token != "" && now.Before(c.expiresAt) {
		return c.token, nil
	}
	token, expiresAt, err := c.fetchToken(ctx, now)
	if err != nil {
		return "", err
	}
	c.token = token
	c.expiresAt = expiresAt
	return token, nil
}

// fetchToken は RS256 の JWT を作り、OAuth トークンエンドポイントで交換する。
func (c *Client) fetchToken(ctx context.Context, now time.Time) (string, time.Time, error) {
	tokenURL := c.tokenURL()
	claims := jwt.MapClaims{
		"iss":   c.Email,
		"scope": fcmScope,
		"aud":   tokenURL,
		"iat":   now.Unix(),
		"exp":   now.Add(time.Hour).Unix(),
	}
	assertion, err := jwt.NewWithClaims(jwt.SigningMethodRS256, claims).SignedString(c.PrivateKey)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("fcm: sign assertion: %w", err)
	}

	form := url.Values{
		"grant_type": {jwtBearerGrant},
		"assertion":  {assertion},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", time.Time{}, fmt.Errorf("fcm: build token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("fcm: token request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", time.Time{}, fmt.Errorf("fcm: token: status %d: %s", resp.StatusCode, snippet(resp.Body))
	}
	var tr struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return "", time.Time{}, fmt.Errorf("fcm: decode token: %w", err)
	}
	if tr.AccessToken == "" {
		return "", time.Time{}, errors.New("fcm: token response missing access_token")
	}
	if tr.ExpiresIn <= 0 {
		tr.ExpiresIn = 3600
	}
	expiresAt := now.Add(time.Duration(tr.ExpiresIn)*time.Second - refreshSkew)
	return tr.AccessToken, expiresAt, nil
}

func (c *Client) now() time.Time {
	if c.Clock != nil {
		return c.Clock.Now()
	}
	return clock.Real{}.Now()
}

func (c *Client) tokenURL() string {
	if c.TokenURL != "" {
		return c.TokenURL
	}
	return defaultTokenURL
}

func (c *Client) baseURL() string {
	if c.BaseURL != "" {
		return c.BaseURL
	}
	return defaultBaseURL
}

func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

// snippet はエラーに含めるレスポンス本文を上限付きで返す。
func snippet(r io.Reader) string {
	b, _ := io.ReadAll(io.LimitReader(r, snippetLimit))
	return strings.TrimSpace(string(b))
}
