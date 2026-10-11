package oidc

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"
)

const telegramIssuer = "https://oauth.telegram.org"
const telegramAuthorizationURL = "https://oauth.telegram.org/auth"
const telegramTokenURL = "https://oauth.telegram.org/token"
const telegramJWKSURL = "https://oauth.telegram.org/.well-known/jwks.json"

// TelegramConfig supplies credentials and guarded operation-local transport only.
// The initial profile fixes official endpoints, Basic, RS256, S256 and openid.
// Configure the Telegram application to use its default RS256 signing algorithm.
// No algorithm downgrade, discovery,
// UserInfo, refresh, editable scopes, issuer override or background work exists.
type TelegramConfig struct {
	ClientID       string
	ClientSecret   string
	RedirectURL    string
	Transport      http.RoundTripper
	EndpointPolicy func(context.Context, *url.URL) error
}

// NewTelegram admits immutable fixed configuration without remote I/O.
// The exact issuer and opaque string subject are never normalized or aliased.
func NewTelegram(ctx context.Context, in TelegramConfig) (*Client, error) {
	if ctx == nil || in.Transport == nil || in.EndpointPolicy == nil || !utf8.ValidString(in.ClientID) || !utf8.ValidString(in.ClientSecret) || !boundedText(in.ClientID, 256) || !boundedText(in.ClientSecret, 4096) {
		return nil, ErrConfig
	}
	if _, err := endpoint(in.RedirectURL); err != nil {
		return nil, ErrConfig
	}
	cfg := Config{Issuer: telegramIssuer, ClientID: in.ClientID, ClientSecret: in.ClientSecret, RedirectURL: in.RedirectURL, Transport: in.Transport, EndpointPolicy: in.EndpointPolicy, telegram: true}
	for _, raw := range []string{telegramIssuer, telegramAuthorizationURL, telegramTokenURL, telegramJWKSURL, in.RedirectURL} {
		if err := admit(ctx, cfg, raw, false); err != nil {
			return nil, err
		}
	}
	return &Client{config: cfg, authorizationURL: telegramAuthorizationURL, tokenURL: telegramTokenURL, jwksURL: telegramJWKSURL, algorithms: []string{"RS256"}}, nil
}

// The maintained verifier still owns signature and claims authority. Reject
// duplicate JSON fields before it parses the exact Telegram identity.
func telegramTokenShape(raw string) error {
	parts := strings.Split(raw, ".")
	if len(parts) != 3 || len(raw) > maxIDTokenBytes {
		return ErrProtocol
	}
	for _, part := range parts[:2] {
		decoded, err := base64.RawURLEncoding.DecodeString(part)
		if err != nil {
			return ErrProtocol
		}
		if _, err := googleJSONObject(decoded); err != nil {
			return ErrProtocol
		}
	}
	return nil
}
