// Package oauth implements an operation-local authorization-code and JSON
// userinfo flow. It does not discover providers or establish application users.
package oauth

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	OperationTimeout         = 10 * time.Second
	MaxURLBytes              = 4096
	maxAuthorizationURLBytes = 8192
	maxTokenBytes            = 64 << 10
	maxProfileBytes          = 256 << 10
	maxSubjectBytes          = 256
)

var (
	ErrConfig      = errors.New("OAuth configuration rejected")
	ErrPolicy      = errors.New("OAuth endpoint policy rejected")
	ErrState       = errors.New("OAuth callback state rejected")
	ErrProtocol    = errors.New("OAuth response rejected")
	ErrUnavailable = errors.New("OAuth operation unavailable")
)

type ClientAuthMethod string

const (
	ClientSecretBasic ClientAuthMethod = "client_secret_basic"
	ClientSecretPost  ClientAuthMethod = "client_secret_post"
)

type SubjectKind string

const (
	SubjectString  SubjectKind = "string"
	SubjectInteger SubjectKind = "integer"
)

// Config contains explicit endpoints and immutable operation settings. Transport
// and EndpointPolicy must honor context cancellation, including body reads and
// closes. Transport must not redirect or replay requests. The caller owns its
// transport and any idle-connection cleanup; this package starts no workers.
type Config struct {
	AuthorizationURL string
	TokenURL         string
	UserInfoURL      string
	RedirectURL      string
	ClientID         string
	ClientSecret     string
	ClientAuthMethod ClientAuthMethod
	Scopes           []string
	// SubjectPath consists of exact object keys, not array indexes or expressions.
	SubjectPath    []string
	Transport      http.RoundTripper
	EndpointPolicy func(context.Context, *url.URL) error
}

type Client struct{ config Config }

type Authorization struct{ State, PKCEVerifier string }
type Callback struct{ Code, State, ExpectedState, PKCEVerifier string }

// Identity is only a verified resource response's configured stable identifier.
// Kind is part of the identity: string "1" and integer 1 must never alias.
// The caller supplies the provider namespace and all application authorization.
type Identity struct {
	Kind    SubjectKind
	Subject string
}

// New validates and captures a configuration without making HTTP requests.
func New(ctx context.Context, config Config) (*Client, error) {
	if ctx == nil || config.Transport == nil || config.EndpointPolicy == nil ||
		!text(config.ClientID, 256) || !text(config.ClientSecret, 4096) ||
		(config.ClientAuthMethod != ClientSecretBasic && config.ClientAuthMethod != ClientSecretPost) ||
		len(config.Scopes) > 16 || len(config.SubjectPath) == 0 || len(config.SubjectPath) > 16 {
		return nil, ErrConfig
	}
	for _, raw := range []string{config.AuthorizationURL, config.TokenURL, config.UserInfoURL, config.RedirectURL} {
		if _, err := endpoint(raw); err != nil {
			return nil, ErrConfig
		}
	}
	seen, total := make(map[string]bool), 0
	for _, scope := range config.Scopes {
		if len(scope) == 0 || len(scope) > 128 || seen[scope] {
			return nil, ErrConfig
		}
		for _, c := range []byte(scope) {
			if c < 0x21 || c > 0x7e || c == '"' || c == '\\' {
				return nil, ErrConfig
			}
		}
		seen[scope] = true
		total += len(scope)
	}
	if total > 1024 {
		return nil, ErrConfig
	}
	total = 0
	for _, segment := range config.SubjectPath {
		if !text(segment, 128) {
			return nil, ErrConfig
		}
		total += len(segment)
	}
	if total > 1024 {
		return nil, ErrConfig
	}
	config.Scopes = append([]string(nil), config.Scopes...)
	config.SubjectPath = append([]string(nil), config.SubjectPath...)
	operation, cancel := context.WithTimeout(ctx, OperationTimeout)
	defer cancel()
	if err := admitAll(operation, config); err != nil {
		return nil, err
	}
	return &Client{config: config}, nil
}

func (c *Client) AuthorizationURL(ctx context.Context, input Authorization) (string, error) {
	if c == nil || ctx == nil || !proof(input.State, 32, 128) || !proof(input.PKCEVerifier, 43, 128) {
		return "", ErrState
	}
	operation, cancel := context.WithTimeout(ctx, OperationTimeout)
	defer cancel()
	if err := admitAll(operation, c.config); err != nil {
		return "", err
	}
	target, err := endpoint(c.config.AuthorizationURL)
	if err != nil {
		return "", ErrConfig
	}
	challenge := sha256.Sum256([]byte(input.PKCEVerifier))
	values := url.Values{
		"response_type": {"code"}, "client_id": {c.config.ClientID},
		"redirect_uri": {c.config.RedirectURL}, "state": {input.State},
		"code_challenge":        {base64.RawURLEncoding.EncodeToString(challenge[:])},
		"code_challenge_method": {"S256"},
	}
	if len(c.config.Scopes) != 0 {
		values.Set("scope", strings.Join(c.config.Scopes, " "))
	}
	target.RawQuery = values.Encode()
	if len(target.String()) > maxAuthorizationURLBytes {
		return "", ErrConfig
	}
	if err := policy(operation, c.config, target); err != nil {
		return "", err
	}
	return target.String(), nil
}

// Exchange requires the caller to atomically consume its trusted ceremony before
// invocation. A token and a profile request share one deadline; no token is kept
// on Client or returned. A profile alone is not an application login authority.
func (c *Client) Exchange(ctx context.Context, input Callback) (Identity, error) {
	if c == nil || ctx == nil || !proof(input.State, 32, 128) || !proof(input.ExpectedState, 32, 128) ||
		subtle.ConstantTimeCompare([]byte(input.State), []byte(input.ExpectedState)) != 1 ||
		!proof(input.PKCEVerifier, 43, 128) || !text(input.Code, 4096) {
		return Identity{}, ErrState
	}
	operation, cancel := context.WithTimeout(ctx, OperationTimeout)
	defer cancel()
	if err := admitAll(operation, c.config); err != nil {
		return Identity{}, err
	}
	values := url.Values{"grant_type": {"authorization_code"}, "code": {input.Code},
		"redirect_uri": {c.config.RedirectURL}, "code_verifier": {input.PKCEVerifier}}
	if c.config.ClientAuthMethod == ClientSecretPost {
		values.Set("client_id", c.config.ClientID)
		values.Set("client_secret", c.config.ClientSecret)
	}
	request, err := http.NewRequestWithContext(operation, http.MethodPost, c.config.TokenURL, strings.NewReader(values.Encode()))
	if err != nil {
		return Identity{}, ErrConfig
	}
	request.GetBody = nil
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	if c.config.ClientAuthMethod == ClientSecretBasic {
		// RFC 6749 section 2.3.1 applies form encoding to each credential before Basic.
		request.SetBasicAuth(url.QueryEscape(c.config.ClientID), url.QueryEscape(c.config.ClientSecret))
	}
	body, err := response(operation, c.config, request, maxTokenBytes)
	if err != nil {
		return Identity{}, err
	}
	token, err := accessToken(body)
	clear(body)
	if err != nil {
		return Identity{}, err
	}
	profileRequest, err := http.NewRequestWithContext(operation, http.MethodGet, c.config.UserInfoURL, nil)
	if err != nil {
		return Identity{}, ErrConfig
	}
	profileRequest.Header.Set("Authorization", "Bearer "+token)
	profileRequest.Header.Set("Accept", "application/json")
	profile, err := response(operation, c.config, profileRequest, maxProfileBytes)
	if err != nil {
		return Identity{}, err
	}
	identity, err := subject(profile, c.config.SubjectPath)
	clear(profile)
	if operation.Err() != nil {
		return Identity{}, ErrUnavailable
	}
	return identity, err
}

func text(value string, bound int) bool {
	if len(value) == 0 || len(value) > bound || !utf8.ValidString(value) {
		return false
	}
	for _, c := range value {
		if c < 0x20 || c == 0x7f {
			return false
		}
	}
	return true
}

func proof(value string, minimum, maximum int) bool {
	if len(value) < minimum || len(value) > maximum {
		return false
	}
	for _, c := range []byte(value) {
		valid := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("-._~", rune(c))
		if !valid {
			return false
		}
	}
	return true
}
