package service

import (
	"context"
	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	oidcprotocol "github.com/miclle/routex/pkg/oidc"
	"github.com/miclle/routex/pkg/upstream"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"unicode"
)

const telegramProviderID = "telegram"
const telegramProfileID = "telegram.oidc.v1"
const telegramIdentityIssuer = "https://oauth.telegram.org"

type telegramProtocolAdapter struct{ client *oidcprotocol.Client }

func (a telegramProtocolAdapter) AuthorizationURL(ctx context.Context, in namedIdentityAuthorization) (string, error) {
	return a.client.AuthorizationURL(ctx, oidcprotocol.Authorization{State: in.State, Nonce: in.Nonce, PKCEVerifier: in.PKCEVerifier})
}
func (a telegramProtocolAdapter) Exchange(ctx context.Context, in namedIdentityCallback) (namedIdentityRemoteIdentity, error) {
	v, e := a.client.Exchange(ctx, oidcprotocol.Callback{Code: in.Code, State: in.State, ExpectedState: in.ExpectedState, ExpectedNonce: in.ExpectedNonce, PKCEVerifier: in.PKCEVerifier})
	if e != nil {
		if e == oidcprotocol.ErrUnavailable {
			return namedIdentityRemoteIdentity{}, namedIdentityUnavailable
		}
		return namedIdentityRemoteIdentity{}, apperrors.ErrUnauthorized
	}
	if v.Issuer != telegramIdentityIssuer || !namedIdentityProfileSubject(telegramProviderID, "string", v.Subject) {
		return namedIdentityRemoteIdentity{}, apperrors.ErrUnauthorized
	}
	return namedIdentityRemoteIdentity{Kind: "string", Subject: v.Subject}, nil
}
func (s *Service) telegramProtocol(ctx context.Context, p entity.NamedIdentityProvider) (namedIdentityProtocolClient, func() error, error) {
	noop := func() error { return nil }
	if ctx == nil || ctx.Err() != nil || p.ID != telegramProviderID || !namedIdentityProfile(p.ID, p.ProfileID, p.IdentityIssuer) || !namedIdentityURLFor(p.ID, p.CallbackURL, true) || !namedIdentityText(p.ClientID, 256) || strings.ContainsFunc(p.ClientID, unicode.IsSpace) || p.AuthCiphertext == "" {
		return nil, noop, namedIdentityUnavailable
	}
	plaintext, e := s.openSecret(rootReference("named_identity_providers", p.ID, p.SecretGeneration), p.AuthCiphertext)
	if e != nil {
		return nil, noop, namedIdentityUnavailable
	}
	t, e := s.namedIdentityTransport(ctx)
	if e != nil {
		return nil, noop, e
	}
	c, e := oidcprotocol.NewTelegram(ctx, oidcprotocol.TelegramConfig{ClientID: p.ClientID, ClientSecret: plaintext, RedirectURL: p.CallbackURL, Transport: telegramRequestTransport{t.RoundTripper}, EndpointPolicy: func(ctx context.Context, u *url.URL) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if u == nil || u.Scheme != "https" || u.User != nil || u.Fragment != "" || u.RawFragment != "" || u.Opaque != "" || u.RawPath != "" {
			return apperrors.ErrBadRequest
		}
		raw := *u
		raw.RawQuery = ""
		raw.ForceQuery = false
		target := raw.String()
		if target != telegramIdentityIssuer && target != "https://oauth.telegram.org/auth" && target != "https://oauth.telegram.org/token" && target != "https://oauth.telegram.org/.well-known/jwks.json" && target != p.CallbackURL {
			return apperrors.ErrBadRequest
		}
		if u.RawQuery != "" || u.ForceQuery {
			if target != "https://oauth.telegram.org/auth" {
				return apperrors.ErrBadRequest
			}
			q, e := url.ParseQuery(u.RawQuery)
			if e != nil || len(q) != 8 {
				return apperrors.ErrBadRequest
			}
			for _, key := range []string{"response_type", "client_id", "redirect_uri", "scope", "state", "nonce", "code_challenge", "code_challenge_method"} {
				if len(q[key]) != 1 {
					return apperrors.ErrBadRequest
				}
			}
			if q.Get("response_type") != "code" || q.Get("client_id") != p.ClientID || q.Get("redirect_uri") != p.CallbackURL || q.Get("scope") != "openid" || q.Get("code_challenge_method") != "S256" {
				return apperrors.ErrBadRequest
			}
		}
		_, e := upstream.ValidateBaseURL(target, s.allowPrivateUpstream)
		return e
	}})
	if e != nil {
		_ = t.Close()
		return nil, noop, namedIdentityUnavailable
	}
	var once sync.Once
	var closeErr error
	close := func() error { once.Do(func() { closeErr = t.Close() }); return closeErr }
	return telegramProtocolAdapter{c}, close, nil
}

// The injected dependency cannot change the profile's destination or request role.
type telegramRequestTransport struct{ next http.RoundTripper }

func (t telegramRequestTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r == nil || r.URL == nil || r.Context().Err() != nil || r.URL.User != nil || r.URL.RawQuery != "" || r.URL.ForceQuery || r.URL.Fragment != "" || r.URL.RawFragment != "" || r.URL.Opaque != "" || r.URL.RawPath != "" {
		return nil, namedIdentityUnavailable
	}
	tokenRequest := r.Method == http.MethodPost && r.URL.String() == "https://oauth.telegram.org/token"
	keysRequest := r.Method == http.MethodGet && r.URL.String() == "https://oauth.telegram.org/.well-known/jwks.json"
	if !tokenRequest && !keysRequest {
		return nil, namedIdentityUnavailable
	}
	return t.next.RoundTrip(r)
}
