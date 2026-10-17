package atproto

import (
	"fmt"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/auth/oauth"

	"github.com/applemango/chain/internal/config"
)

// OAuth のクライアント方式は specification/backend/03-auth-atproto.md §2.1.1。

const (
	clientKeyID = "chain-1"
	userAgent   = "chain-backend"
)

func NewOAuthApp(cfg *config.Config, store oauth.ClientAuthStore) (*oauth.ClientApp, error) {
	var oc oauth.ClientConfig
	if cfg.OAuthLoopback() {
		oc = oauth.NewLocalhostConfig(LoopbackCallbackURL(cfg), cfg.OAuthScopes)
	} else {
		oc = oauth.NewPublicConfig(cfg.PublicURL+"/oauth-client-metadata.json", cfg.PublicURL+"/auth/callback", cfg.OAuthScopes)
		priv, err := atcrypto.ParsePrivateMultibase(cfg.OAuthPrivateKey)
		if err != nil {
			return nil, fmt.Errorf("CHAIN_OAUTH_PRIVATE_KEY: %w", err)
		}
		if err := oc.SetClientSecret(priv, clientKeyID); err != nil {
			return nil, fmt.Errorf("CHAIN_OAUTH_PRIVATE_KEY: %w", err)
		}
	}
	oc.UserAgent = userAgent
	return oauth.NewClientApp(&oc, store), nil
}

func LoopbackCallbackURL(cfg *config.Config) string {
	return "http://127.0.0.1:" + cfg.ListenPort() + "/auth/callback"
}

// ClientMetadata は /oauth-client-metadata.json で返す文書（03 §2.1）。
func ClientMetadata(cfg *config.Config, app *oauth.ClientApp) oauth.ClientMetadata {
	m := app.Config.ClientMetadata()
	name := "CHAIN"
	m.ClientName = &name
	if app.Config.IsConfidential() {
		jwks := cfg.PublicURL + "/oauth/jwks.json"
		m.JWKSURI = &jwks
	}
	return m
}
