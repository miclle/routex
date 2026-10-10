package oidc

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"
)

const googleIssuer = "https://accounts.google.com"
const googleAuthorizationURL = "https://accounts.google.com/o/oauth2/v2/auth"
const googleTokenURL = "https://oauth2.googleapis.com/token"
const googleJWKSURL = "https://www.googleapis.com/oauth2/v3/certs"

// GoogleConfig supplies credentials and guarded operation-local transport only.
// The profile fixes endpoints, Basic, RS256, S256 and openid profile; no discovery,
// UserInfo, refresh, editable scopes, issuer override or background work exists.
type GoogleConfig struct {
	ClientID       string
	ClientSecret   string
	RedirectURL    string
	Transport      http.RoundTripper
	EndpointPolicy func(context.Context, *url.URL) error
}

// NewGoogle admits immutable fixed configuration without remote I/O. Enterprise
// Discover remains exact-issuer and does not inherit Google's documented alias.
func NewGoogle(ctx context.Context, in GoogleConfig) (*Client, error) {
	if ctx == nil || in.Transport == nil || in.EndpointPolicy == nil || !utf8.ValidString(in.ClientID) || !utf8.ValidString(in.ClientSecret) || !boundedText(in.ClientID, 256) || !boundedText(in.ClientSecret, 4096) {
		return nil, ErrConfig
	}
	if _, err := endpoint(in.RedirectURL); err != nil {
		return nil, ErrConfig
	}
	cfg := Config{Issuer: googleIssuer, ClientID: in.ClientID, ClientSecret: in.ClientSecret, RedirectURL: in.RedirectURL, Transport: in.Transport, EndpointPolicy: in.EndpointPolicy, google: true}
	for _, raw := range []string{googleIssuer, googleAuthorizationURL, googleTokenURL, googleJWKSURL, in.RedirectURL} {
		if err := admit(ctx, cfg, raw, false); err != nil {
			return nil, err
		}
	}
	return &Client{config: cfg, authorizationURL: googleAuthorizationURL, tokenURL: googleTokenURL, jwksURL: googleJWKSURL, algorithms: []string{"RS256"}}, nil
}

// This is an admission witness, never signature authority. The selected exact
// issuer is subsequently supplied to the maintained verifier, without skipping
// issuer checks; only verified identities are normalized to the fixed namespace.
func googleTokenIssuer(raw string) (string, error) {
	parts := strings.Split(raw, ".")
	if len(parts) != 3 || len(raw) > maxIDTokenBytes {
		return "", ErrProtocol
	}
	for _, part := range parts[:2] {
		decoded, err := base64.RawURLEncoding.DecodeString(part)
		if err != nil {
			return "", ErrProtocol
		}
		if _, err := googleJSONObject(decoded); err != nil {
			return "", ErrProtocol
		}
	}
	body, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", ErrProtocol
	}
	fields, err := googleJSONObject(body)
	if err != nil {
		return "", ErrProtocol
	}
	issuer, ok := fields["iss"].(string)
	if !ok || issuer != googleIssuer && issuer != "accounts.google.com" {
		return "", ErrProtocol
	}
	sub, ok := fields["sub"].(string)
	if !ok || !subject(sub) {
		return "", ErrProtocol
	}
	return issuer, nil
}
