// Package oidc verifies one OpenID Connect authorization-code ceremony.
// Callers own durable, single-use ceremony state and explicit account binding.
package oidc

import (
	"context"
	"crypto/subtle"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	coreoidc "github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

const (
	OperationTimeout         = 10 * time.Second
	MaxURLBytes              = 4096
	maxAuthorizationURLBytes = 8192
	maxDiscoveryBytes        = 64 << 10
	maxTokenBytes            = 64 << 10
	maxJWKSBytes             = 256 << 10
	maxIDTokenBytes          = 32 << 10
	maxKeys                  = 64
)

var (
	ErrConfig      = errors.New("invalid OpenID Connect configuration")
	ErrPolicy      = errors.New("OpenID Connect endpoint is not permitted")
	ErrState       = errors.New("invalid OpenID Connect ceremony proof")
	ErrProtocol    = errors.New("OpenID Connect verification failed")
	ErrUnavailable = errors.New("OpenID Connect operation unavailable")
)

// Config has no default network transport. Transport must honor request context
// cancellation through RoundTrip, body Read and Close. EndpointPolicy must apply
// the deployment's destination policy, without logging query or request secrets.
// ClientSecret authentication is explicitly client_secret_basic; no probing occurs.
type Config struct {
	google bool // only NewGoogle can select the fixed Google profile

	Issuer         string
	ClientID       string
	ClientSecret   string
	RedirectURL    string
	Transport      http.RoundTripper
	EndpointPolicy func(context.Context, *url.URL) error
}

// Client is an immutable discovery capture. Create a new Client after any
// configuration change; this package does not refresh metadata or cache tokens.
type Client struct {
	config           Config
	authorizationURL string
	tokenURL         string
	jwksURL          string
	algorithms       []string
}

type Authorization struct {
	State        string
	Nonce        string
	PKCEVerifier string
}

type Callback struct {
	Code          string
	State         string
	ExpectedState string
	ExpectedNonce string
	PKCEVerifier  string
}

// Identity contains only the verified exact OIDC binding identity. In particular,
// email, roles, tokens and provider claims are not account-linking authority.
type Identity struct {
	Issuer  string
	Subject string
}

type discovery struct {
	Issuer           string   `json:"issuer"`
	AuthorizationURL string   `json:"authorization_endpoint"`
	TokenURL         string   `json:"token_endpoint"`
	JWKSURL          string   `json:"jwks_uri"`
	Algorithms       []string `json:"id_token_signing_alg_values_supported"`
	ResponseTypes    []string `json:"response_types_supported"`
	AuthMethods      []string `json:"token_endpoint_auth_methods_supported"`
}

// Discover performs exactly one metadata request under a capped caller context.
// A caller may share one shorter outer deadline across Discover and Exchange.
func Discover(ctx context.Context, config Config) (*Client, error) {
	if ctx == nil || config.Transport == nil || config.EndpointPolicy == nil ||
		!boundedText(config.ClientID, 256) || !boundedText(config.ClientSecret, 4096) {
		return nil, ErrConfig
	}
	for _, raw := range []string{config.Issuer, config.RedirectURL} {
		if _, err := endpoint(raw); err != nil {
			return nil, ErrConfig
		}
	}
	ctx, cancel := context.WithTimeout(ctx, OperationTimeout)
	defer cancel()
	if err := admit(ctx, config, config.RedirectURL, false); err != nil {
		return nil, err
	}
	metadataURL := strings.TrimSuffix(config.Issuer, "/") + "/.well-known/openid-configuration"
	if _, err := endpoint(metadataURL); err != nil {
		return nil, ErrConfig
	}
	client := operationClient(config, map[string]requestBound{
		metadataURL: {method: http.MethodGet, bytes: maxDiscoveryBytes},
	})
	provider, err := coreoidc.NewProvider(coreoidc.ClientContext(ctx, client), config.Issuer)
	if err != nil {
		return nil, ErrProtocol
	}
	var metadata discovery
	if err := provider.Claims(&metadata); err != nil || metadata.Issuer != config.Issuer ||
		!contains(metadata.ResponseTypes, "code") ||
		(len(metadata.AuthMethods) > 0 && !contains(metadata.AuthMethods, "client_secret_basic")) {
		return nil, ErrProtocol
	}
	for _, raw := range []string{metadata.AuthorizationURL, metadata.TokenURL, metadata.JWKSURL} {
		if _, err := endpoint(raw); err != nil {
			return nil, ErrProtocol
		}
		if err := admit(ctx, config, raw, false); err != nil {
			return nil, err
		}
	}
	// Distinct endpoints prevent method/response-role ambiguity in the bounded transport.
	if metadata.TokenURL == metadata.JWKSURL || metadata.AuthorizationURL == metadata.TokenURL ||
		metadata.AuthorizationURL == metadata.JWKSURL {
		return nil, ErrProtocol
	}
	algorithms := approvedAlgorithms(metadata.Algorithms)
	if len(algorithms) == 0 || ctx.Err() != nil {
		return nil, ErrProtocol
	}
	return &Client{config: config, authorizationURL: metadata.AuthorizationURL,
		tokenURL: metadata.TokenURL, jwksURL: metadata.JWKSURL, algorithms: algorithms}, nil
}

// AuthorizationURL builds an S256-only code request. State, nonce and verifier
// must come from the caller's same durable, single-use ceremony, never the browser.
func (c *Client) AuthorizationURL(ctx context.Context, request Authorization) (string, error) {
	if c == nil || ctx == nil || !proof(request.State, 32) || !proof(request.Nonce, 32) ||
		!proof(request.PKCEVerifier, 43) {
		return "", ErrState
	}
	ctx, cancel := context.WithTimeout(ctx, OperationTimeout)
	defer cancel()
	raw := c.oauthConfig().AuthCodeURL(request.State,
		oauth2.SetAuthURLParam("nonce", request.Nonce), oauth2.S256ChallengeOption(request.PKCEVerifier))
	if len(raw) > maxAuthorizationURLBytes {
		return "", ErrProtocol
	}
	if err := admit(ctx, c.config, raw, true); err != nil {
		return "", err
	}
	return raw, nil
}

// Exchange does one token POST and one JWKS GET, never a retry or UserInfo read.
// Callers must atomically consume trusted ceremony state before invoking it.
func (c *Client) Exchange(ctx context.Context, callback Callback) (Identity, error) {
	if c == nil || ctx == nil || !proof(callback.State, 32) || !proof(callback.ExpectedState, 32) ||
		subtle.ConstantTimeCompare([]byte(callback.State), []byte(callback.ExpectedState)) != 1 ||
		!proof(callback.ExpectedNonce, 32) || !proof(callback.PKCEVerifier, 43) ||
		!boundedText(callback.Code, 4096) {
		return Identity{}, ErrState
	}
	ctx, cancel := context.WithTimeout(ctx, OperationTimeout)
	defer cancel()
	client := operationClient(c.config, map[string]requestBound{
		c.tokenURL: {method: http.MethodPost, bytes: maxTokenBytes},
		c.jwksURL:  {method: http.MethodGet, bytes: maxJWKSBytes},
	})
	token, err := c.oauthConfig().Exchange(coreoidc.ClientContext(ctx, client), callback.Code,
		oauth2.VerifierOption(callback.PKCEVerifier))
	if err != nil || ctx.Err() != nil {
		return Identity{}, ErrProtocol
	}
	raw, ok := token.Extra("id_token").(string)
	if !ok || len(raw) == 0 || len(raw) > maxIDTokenBytes {
		return Identity{}, ErrProtocol
	}
	keys, err := readKeys(ctx, client, c.jwksURL)
	if err != nil {
		return Identity{}, err
	}
	return c.verify(ctx, raw, callback.ExpectedNonce, keys, token.AccessToken)
}

func (c *Client) oauthConfig() *oauth2.Config {
	scopes := []string{"openid"}
	if c.config.google {
		scopes = []string{"openid", "profile"}
	}
	return &oauth2.Config{ClientID: c.config.ClientID, ClientSecret: c.config.ClientSecret,
		RedirectURL: c.config.RedirectURL, Scopes: scopes,
		Endpoint: oauth2.Endpoint{AuthURL: c.authorizationURL, TokenURL: c.tokenURL,
			AuthStyle: oauth2.AuthStyleInHeader}}
}

func boundedText(value string, max int) bool {
	if value == "" || len(value) > max {
		return false
	}
	for _, ch := range value {
		if ch < 0x20 || ch == 0x7f {
			return false
		}
	}
	return true
}

func proof(value string, min int) bool {
	if len(value) < min || len(value) > 128 {
		return false
	}
	for _, ch := range value {
		valid := ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' ||
			ch == '-' || ch == '.' || ch == '_' || ch == '~'
		if !valid {
			return false
		}
	}
	return true
}

func contains(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func approvedAlgorithms(advertised []string) []string {
	var result []string
	for _, algorithm := range []string{"RS256", "RS384", "RS512", "ES256", "ES384", "ES512"} {
		if contains(advertised, algorithm) {
			result = append(result, algorithm)
		}
	}
	return result
}
