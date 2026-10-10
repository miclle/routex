package service

import (
	"context"
	"net/http"
	"net/url"
	"sync"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	oauthprotocol "github.com/miclle/routex/pkg/oauth"
	"github.com/miclle/routex/pkg/upstream"
)

const githubProviderID = "github"
const githubProfileID = "github.com.oauth-app.v1"
const githubIdentityIssuer = "https://github.com"

// NamedIdentityTransportFactory is an operation-local constructor dependency.
// It is not configurable through YAML, CLI, HTTP, or operator policy. Injected
// transports must honor context/body closure and must not redirect or replay.
type NamedIdentityTransportFactory func(context.Context) (NamedIdentityTransport, error)
type NamedIdentityTransport struct {
	RoundTripper http.RoundTripper
	Close        func() error
}

func WithNamedIdentityTransportFactory(factory NamedIdentityTransportFactory) Option {
	return func(s *Service) { s.namedIdentityTransportFactory = factory }
}
func namedIdentityProfile(provider, profile, issuer string) bool {
	d, ok := namedIdentityDescriptor(provider)
	return ok && profile == d.profile && issuer == d.issuer
}
func (s *Service) namedIdentityTransport(ctx context.Context) (NamedIdentityTransport, error) {
	if ctx == nil || ctx.Err() != nil {
		return NamedIdentityTransport{}, namedIdentityUnavailable
	}
	if s.namedIdentityTransportFactory != nil {
		t, err := s.namedIdentityTransportFactory(ctx)
		if err != nil || t.RoundTripper == nil || t.Close == nil {
			if t.Close != nil {
				_ = t.Close()
			}
			return NamedIdentityTransport{}, namedIdentityUnavailable
		}
		return t, nil
	}
	c := upstream.NewNonReplayingClient(s.allowPrivateUpstream)
	return NamedIdentityTransport{RoundTripper: c.Transport, Close: func() error { c.CloseIdleConnections(); return nil }}, nil
}

type namedIdentityRequestTransport struct{ next http.RoundTripper }

func (t namedIdentityRequestTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r == nil || r.URL == nil || r.Context().Err() != nil || r.URL.Scheme != "https" || r.URL.User != nil || r.URL.Fragment != "" || r.URL.RawFragment != "" || r.URL.RawQuery != "" || r.URL.ForceQuery || r.URL.Opaque != "" {
		return nil, namedIdentityUnavailable
	}
	tokenRequest := r.Method == http.MethodPost && r.URL.Host == "github.com" && r.URL.Path == "/login/oauth/access_token"
	userRequest := r.Method == http.MethodGet && r.URL.Host == "api.github.com" && r.URL.Path == "/user"
	if r.URL.RawPath != "" || !tokenRequest && !userRequest {
		return nil, namedIdentityUnavailable
	}
	copyRequest := r.Clone(r.Context())
	copyRequest.Header = r.Header.Clone()
	copyRequest.Header.Set("User-Agent", "RouteX")
	return t.next.RoundTrip(copyRequest)
}
func (s *Service) namedIdentityProtocol(ctx context.Context, p entity.NamedIdentityProvider) (*oauthprotocol.Client, func() error, error) {
	noop := func() error { return nil }
	if p.ID != githubProviderID || !namedIdentityProfile(p.ID, p.ProfileID, p.IdentityIssuer) || p.AuthCiphertext == "" {
		return nil, noop, namedIdentityUnavailable
	}
	plaintext, err := s.openSecret(rootReference("named_identity_providers", p.ID, p.SecretGeneration), p.AuthCiphertext)
	if err != nil {
		return nil, noop, namedIdentityUnavailable
	}
	t, err := s.namedIdentityTransport(ctx)
	if err != nil {
		return nil, noop, err
	}
	c, err := oauthprotocol.New(ctx, oauthprotocol.Config{
		AuthorizationURL: "https://github.com/login/oauth/authorize", TokenURL: "https://github.com/login/oauth/access_token", UserInfoURL: "https://api.github.com/user",
		RedirectURL: p.CallbackURL, ClientID: p.ClientID, ClientSecret: plaintext, ClientAuthMethod: oauthprotocol.ClientSecretPost, Scopes: []string{}, SubjectPath: []string{"id"},
		Transport: namedIdentityRequestTransport{next: t.RoundTripper},
		EndpointPolicy: func(c context.Context, u *url.URL) error {
			if c.Err() != nil {
				return c.Err()
			}
			if u == nil || u.Scheme != "https" || u.User != nil || u.Fragment != "" || u.RawFragment != "" || u.Opaque != "" || u.RawPath != "" {
				return apperrors.ErrBadRequest
			}
			raw := *u
			raw.RawQuery = ""
			raw.ForceQuery = false
			target := raw.String()
			if target != "https://github.com/login/oauth/authorize" && target != "https://github.com/login/oauth/access_token" && target != "https://api.github.com/user" && target != p.CallbackURL {
				return apperrors.ErrBadRequest
			}
			if u.RawQuery != "" || u.ForceQuery {
				if target != "https://github.com/login/oauth/authorize" {
					return apperrors.ErrBadRequest
				}
				q, err := url.ParseQuery(u.RawQuery)
				if err != nil || len(q) != 6 {
					return apperrors.ErrBadRequest
				}
				for _, key := range []string{"response_type", "client_id", "redirect_uri", "state", "code_challenge", "code_challenge_method"} {
					if len(q[key]) != 1 {
						return apperrors.ErrBadRequest
					}
				}
				if q.Get("response_type") != "code" || q.Get("client_id") != p.ClientID || q.Get("redirect_uri") != p.CallbackURL || q.Get("code_challenge_method") != "S256" {
					return apperrors.ErrBadRequest
				}
			}
			_, err := upstream.ValidateBaseURL(target, s.allowPrivateUpstream)
			return err
		},
	})
	if err != nil {
		_ = t.Close()
		return nil, noop, namedIdentityUnavailable
	}
	var once sync.Once
	var closeErr error
	closeTransport := func() error { once.Do(func() { closeErr = t.Close() }); return closeErr }
	return c, closeTransport, nil
}
