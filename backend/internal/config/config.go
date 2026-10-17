package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// 近接判定の調整値（05 §5、02 §6）。環境変数では上書きしない。
const (
	// PresenceGrace は参加後の閲覧・投稿の判定に使える位置の鮮度（09 §4）。
	PresenceGrace = 10 * time.Minute
	// LocationFresh は geofence 判定に使える位置の鮮度（参加・作成時、09 §2）。
	LocationFresh = 120 * time.Second
)

type Env string

const (
	EnvDev  Env = "dev"
	EnvProd Env = "prod"
)

// Config は環境変数から読む設定値。項目は specification/backend/02-conventions.md §6。
type Config struct {
	Env             Env
	Listen          string
	PublicURL       string
	AppRedirect     string
	DBPath          string
	RedisURL        string
	MediaDir        string
	SecretKey       []byte
	JWTKey          []byte
	OAuthPrivateKey string
	OAuthScopes     []string
	JetstreamURL    string
	NanoGPTURL      string
	NanoGPTKey      string
	JevModel        string
	FCMCredPath     string
	EmbedURL        string
	EmbedModel      string
	EmbedDim        int
	CORSOrigins     []string
}

func Load() (*Config, error) {
	c := &Config{
		Env:             Env(get("CHAIN_ENV", string(EnvDev))),
		Listen:          get("CHAIN_LISTEN", ":8080"),
		PublicURL:       strings.TrimRight(os.Getenv("CHAIN_PUBLIC_URL"), "/"),
		AppRedirect:     os.Getenv("CHAIN_APP_REDIRECT"),
		DBPath:          get("CHAIN_DB_PATH", "data/app.db"),
		RedisURL:        os.Getenv("CHAIN_REDIS_URL"),
		MediaDir:        get("CHAIN_MEDIA_DIR", "data/media"),
		OAuthPrivateKey: os.Getenv("CHAIN_OAUTH_PRIVATE_KEY"),
		OAuthScopes:     strings.Fields(get("CHAIN_OAUTH_SCOPES", "atproto repo:jp.i32.chain.feed.post repo:jp.i32.chain.actor.profile repo:app.bsky.feed.post?action=create&action=delete repo:app.bsky.actor.profile?action=create&action=update blob:image/*")),
		JetstreamURL:    get("CHAIN_JETSTREAM_URL", "wss://jetstream2.us-east.bsky.network/subscribe"),
		NanoGPTURL:      get("CHAIN_NANOGPT_URL", "https://api.nano-gpt.com/api/v1/decisions"),
		NanoGPTKey:      os.Getenv("CHAIN_NANOGPT_KEY"),
		JevModel:        get("CHAIN_JEV_MODEL", "typesafe/jev-1.13"),
		FCMCredPath:     os.Getenv("CHAIN_FCM_CREDENTIALS"),
		EmbedURL:        os.Getenv("CHAIN_EMBED_URL"),
		EmbedModel:      get("CHAIN_EMBED_MODEL", "embeddinggemma"),
		CORSOrigins:     split(get("CHAIN_CORS_ORIGINS", "https://localhost,capacitor://localhost,http://localhost:3000")),
	}

	var errs []error
	if c.Env != EnvDev && c.Env != EnvProd {
		errs = append(errs, fmt.Errorf("CHAIN_ENV must be dev or prod: %q", c.Env))
	}
	required := map[string]string{
		"CHAIN_PUBLIC_URL":   c.PublicURL,
		"CHAIN_APP_REDIRECT": c.AppRedirect,
		"CHAIN_REDIS_URL":    c.RedisURL,
	}
	for name, v := range required {
		if v == "" {
			errs = append(errs, fmt.Errorf("%s is required", name))
		}
	}

	var err error
	if c.SecretKey, err = key("CHAIN_SECRET_KEY", 32, 32); err != nil {
		errs = append(errs, err)
	}
	if c.JWTKey, err = key("CHAIN_JWT_KEY", 32, 0); err != nil {
		errs = append(errs, err)
	}
	if c.EmbedDim, err = positiveInt("CHAIN_EMBED_DIM", 256); err != nil {
		errs = append(errs, err)
	}
	// dev で鍵がなければ loopback クライアントになる（03 §2.1.1）。
	if c.Env == EnvProd && c.OAuthPrivateKey == "" {
		errs = append(errs, errors.New("CHAIN_OAUTH_PRIVATE_KEY is required in prod"))
	}

	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return c, nil
}

func (c *Config) IsDev() bool { return c.Env == EnvDev }

// OAuthLoopback は ATProto の localhost 開発用クライアントを使うか（03 §2.1.1）。
func (c *Config) OAuthLoopback() bool { return c.OAuthPrivateKey == "" && c.IsDev() }

// ListenPort は CHAIN_LISTEN（":8080" など）のポート部分。
func (c *Config) ListenPort() string {
	if i := strings.LastIndex(c.Listen, ":"); i >= 0 {
		return c.Listen[i+1:]
	}
	return c.Listen
}

func get(name, def string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return def
}

// positiveInt は正の整数の環境変数を読む。未設定なら def。
func positiveInt(name string, def int) (int, error) {
	v := os.Getenv(name)
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer: %q", name, v)
	}
	return n, nil
}

func split(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// key は base64 の鍵を読む。max が 0 なら上限なし。
func key(name string, min, max int) ([]byte, error) {
	v := os.Getenv(name)
	if v == "" {
		return nil, fmt.Errorf("%s is required", name)
	}
	b, err := base64.StdEncoding.DecodeString(v)
	if err != nil {
		return nil, fmt.Errorf("%s must be base64: %w", name, err)
	}
	if len(b) < min || (max > 0 && len(b) > max) {
		return nil, fmt.Errorf("%s has invalid length %d", name, len(b))
	}
	return b, nil
}
