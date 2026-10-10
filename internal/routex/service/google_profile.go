package service

import (
	"context"
	"net/http"
	"net/url"
	"sync"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	oauthprotocol "github.com/miclle/routex/pkg/oauth"
	oidcprotocol "github.com/miclle/routex/pkg/oidc"
	"github.com/miclle/routex/pkg/upstream"
)

const googleProviderID = "google"
const googleProfileID = "google.oidc.v1"
const googleIdentityIssuer = "https://accounts.google.com"

type namedIdentityFixedProfile struct{ profile, issuer, callbackPath string }

func namedIdentityDescriptor(providerID string) (namedIdentityFixedProfile, bool) {
	switch providerID {
	case githubProviderID:
		return namedIdentityFixedProfile{githubProfileID, githubIdentityIssuer, "/api/v1/auth/github/callback"}, true
	case googleProviderID:
		return namedIdentityFixedProfile{googleProfileID, googleIdentityIssuer, "/api/v1/auth/google/callback"}, true
	default:
		return namedIdentityFixedProfile{}, false
	}
}
func namedIdentityProfileID(providerID string) string {
	d, _ := namedIdentityDescriptor(providerID)
	return d.profile
}
func namedIdentityMethod(method string) bool { _, ok := namedIdentityDescriptor(method); return ok }
func namedIdentityProfileSubject(providerID, kind, value string) bool {
	if providerID == githubProviderID {
		return namedIdentitySubject(kind, value)
	}
	if providerID != googleProviderID || kind != "string" || len(value) == 0 || len(value) > 255 {
		return false
	}
	for _, c := range []byte(value) {
		if c < 0x20 || c > 0x7e {
			return false
		}
	}
	return true
}

type namedIdentityAuthorization struct{ State, Nonce, PKCEVerifier string }
type namedIdentityCallback struct{ Code, State, ExpectedState, ExpectedNonce, PKCEVerifier string }
type namedIdentityRemoteIdentity struct{ Kind, Subject string }
type namedIdentityProtocolClient interface {
	AuthorizationURL(context.Context, namedIdentityAuthorization) (string, error)
	Exchange(context.Context, namedIdentityCallback) (namedIdentityRemoteIdentity, error)
}
type githubProtocolAdapter struct{ client *oauthprotocol.Client }

func (a githubProtocolAdapter) AuthorizationURL(ctx context.Context, in namedIdentityAuthorization) (string, error) {
	return a.client.AuthorizationURL(ctx, oauthprotocol.Authorization{State: in.State, PKCEVerifier: in.PKCEVerifier})
}
func (a githubProtocolAdapter) Exchange(ctx context.Context, in namedIdentityCallback) (namedIdentityRemoteIdentity, error) {
	v, e := a.client.Exchange(ctx, oauthprotocol.Callback{Code: in.Code, State: in.State, ExpectedState: in.ExpectedState, PKCEVerifier: in.PKCEVerifier})
	if e != nil {
		if e == oauthprotocol.ErrUnavailable {
			return namedIdentityRemoteIdentity{}, namedIdentityUnavailable
		}
		return namedIdentityRemoteIdentity{}, apperrors.ErrUnauthorized
	}
	return namedIdentityRemoteIdentity{Kind: string(v.Kind), Subject: v.Subject}, nil
}

type googleProtocolAdapter struct{ client *oidcprotocol.Client }

func (a googleProtocolAdapter) AuthorizationURL(ctx context.Context, in namedIdentityAuthorization) (string, error) {
	return a.client.AuthorizationURL(ctx, oidcprotocol.Authorization{State: in.State, Nonce: in.Nonce, PKCEVerifier: in.PKCEVerifier})
}
func (a googleProtocolAdapter) Exchange(ctx context.Context, in namedIdentityCallback) (namedIdentityRemoteIdentity, error) {
	v, e := a.client.Exchange(ctx, oidcprotocol.Callback{Code: in.Code, State: in.State, ExpectedState: in.ExpectedState, ExpectedNonce: in.ExpectedNonce, PKCEVerifier: in.PKCEVerifier})
	if e != nil {
		if e == oidcprotocol.ErrUnavailable {
			return namedIdentityRemoteIdentity{}, namedIdentityUnavailable
		}
		return namedIdentityRemoteIdentity{}, apperrors.ErrUnauthorized
	}
	if v.Issuer != googleIdentityIssuer || !namedIdentityProfileSubject(googleProviderID, "string", v.Subject) {
		return namedIdentityRemoteIdentity{}, apperrors.ErrUnauthorized
	}
	return namedIdentityRemoteIdentity{Kind: "string", Subject: v.Subject}, nil
}
func (s *Service) namedIdentityProfileProtocol(ctx context.Context, p entity.NamedIdentityProvider) (namedIdentityProtocolClient, func() error, error) {
	if p.ID == githubProviderID {
		c, close, e := s.namedIdentityProtocol(ctx, p)
		return githubProtocolAdapter{c}, close, e
	}
	noop := func() error { return nil }
	if p.ID != googleProviderID || !namedIdentityProfile(p.ID, p.ProfileID, p.IdentityIssuer) || p.AuthCiphertext == "" {
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
	c, e := oidcprotocol.NewGoogle(ctx, oidcprotocol.GoogleConfig{ClientID: p.ClientID, ClientSecret: plaintext, RedirectURL: p.CallbackURL, Transport: googleRequestTransport{t.RoundTripper}, EndpointPolicy: func(ctx context.Context, u *url.URL) error {
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
		if target != googleIdentityIssuer && target != "https://accounts.google.com/o/oauth2/v2/auth" && target != "https://oauth2.googleapis.com/token" && target != "https://www.googleapis.com/oauth2/v3/certs" && target != p.CallbackURL {
			return apperrors.ErrBadRequest
		}
		if u.RawQuery != "" || u.ForceQuery {
			if target != "https://accounts.google.com/o/oauth2/v2/auth" {
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
			if q.Get("response_type") != "code" || q.Get("client_id") != p.ClientID || q.Get("redirect_uri") != p.CallbackURL || q.Get("scope") != "openid profile" || q.Get("code_challenge_method") != "S256" {
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
	return googleProtocolAdapter{c}, close, nil
}

// The injected dependency cannot change the profile's destination or request role.
type googleRequestTransport struct{ next http.RoundTripper }

func (t googleRequestTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r == nil || r.URL == nil || r.Context().Err() != nil || r.URL.User != nil || r.URL.RawQuery != "" || r.URL.ForceQuery || r.URL.Fragment != "" || r.URL.RawFragment != "" || r.URL.Opaque != "" || r.URL.RawPath != "" {
		return nil, namedIdentityUnavailable
	}
	tokenRequest := r.Method == http.MethodPost && r.URL.String() == "https://oauth2.googleapis.com/token"
	keysRequest := r.Method == http.MethodGet && r.URL.String() == "https://www.googleapis.com/oauth2/v3/certs"
	if !tokenRequest && !keysRequest {
		return nil, namedIdentityUnavailable
	}
	return t.next.RoundTrip(r)
}
