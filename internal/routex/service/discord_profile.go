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

const discordProviderID = "discord"
const discordProfileID = "discord.oauth2.v1"
// This is the fixed identity namespace, not an OIDC issuer claim.
const discordIdentityIssuer = "https://discord.com"
const discordAuthorizationURL = "https://discord.com/oauth2/authorize"
const discordTokenURL = "https://discord.com/api/v10/oauth2/token"
const discordUserURL = "https://discord.com/api/v10/users/@me"

// Validate the string representation without reformatting or numeric coercion.
func discordCanonicalID(value string) bool {
	if len(value) == 0 || len(value) > 20 || value[0] == '0' {
		return false
	}
	for _, c := range []byte(value) {
		if c < '0' || c > '9' {
			return false
		}
	}
	return len(value) < 20 || value <= "18446744073709551615"
}

type discordProtocolAdapter struct{ client *oauthprotocol.Client }

func (a discordProtocolAdapter) AuthorizationURL(ctx context.Context, in namedIdentityAuthorization) (string, error) {
	return a.client.AuthorizationURL(ctx, oauthprotocol.Authorization{State: in.State, PKCEVerifier: in.PKCEVerifier})
}
func (a discordProtocolAdapter) Exchange(ctx context.Context, in namedIdentityCallback) (namedIdentityRemoteIdentity, error) {
	identity, err := a.client.Exchange(ctx, oauthprotocol.Callback{Code: in.Code, State: in.State, ExpectedState: in.ExpectedState, PKCEVerifier: in.PKCEVerifier})
	if err != nil {
		if err == oauthprotocol.ErrUnavailable {
			return namedIdentityRemoteIdentity{}, namedIdentityUnavailable
		}
		return namedIdentityRemoteIdentity{}, apperrors.ErrUnauthorized
	}
	if identity.Kind != oauthprotocol.SubjectString || !discordCanonicalID(identity.Subject) {
		return namedIdentityRemoteIdentity{}, apperrors.ErrUnauthorized
	}
	return namedIdentityRemoteIdentity{Kind: string(identity.Kind), Subject: identity.Subject}, nil
}

// The generic client is deliberately broader than this fixed request profile.
func discordEndpointPolicy(ctx context.Context, target *url.URL, clientID, callback string, allowPrivate bool) error {
	if ctx == nil || ctx.Err() != nil {
		return namedIdentityUnavailable
	}
	if target == nil || target.Scheme != "https" || target.User != nil || target.Fragment != "" || target.RawFragment != "" || target.Opaque != "" || target.RawPath != "" {
		return apperrors.ErrBadRequest
	}
	base := *target
	base.RawQuery = ""
	base.ForceQuery = false
	raw := base.String()
	if raw != discordAuthorizationURL && raw != discordTokenURL && raw != discordUserURL && raw != callback {
		return apperrors.ErrBadRequest
	}
	if target.RawQuery != "" || target.ForceQuery {
		if raw != discordAuthorizationURL {
			return apperrors.ErrBadRequest
		}
		q, err := url.ParseQuery(target.RawQuery)
		if err != nil || len(q) != 7 {
			return apperrors.ErrBadRequest
		}
		for _, key := range []string{"response_type", "client_id", "redirect_uri", "scope", "state", "code_challenge", "code_challenge_method"} {
			if len(q[key]) != 1 {
				return apperrors.ErrBadRequest
			}
		}
		if q.Get("response_type") != "code" || q.Get("client_id") != clientID || q.Get("redirect_uri") != callback || q.Get("scope") != "identify" || q.Get("code_challenge_method") != "S256" || !namedIdentityBrowserValue(q.Get("state")) || !namedIdentityBrowserValue(q.Get("code_challenge")) {
			return apperrors.ErrBadRequest
		}
	}
	_, err := upstream.ValidateBaseURL(raw, allowPrivate)
	return err
}

// Every instance is owned by one ceremony operation; no token is persisted here.
func (s *Service) discordProtocol(ctx context.Context, p entity.NamedIdentityProvider) (namedIdentityProtocolClient, func() error, error) {
	noop := func() error { return nil }
	if ctx == nil || ctx.Err() != nil || p.ID != discordProviderID || !namedIdentityProfile(p.ID, p.ProfileID, p.IdentityIssuer) || !discordCanonicalID(p.ClientID) || !namedIdentityURLFor(p.ID, p.CallbackURL, true) || p.AuthCiphertext == "" {
		return nil, noop, namedIdentityUnavailable
	}
	plaintext, err := s.openSecret(rootReference("named_identity_providers", p.ID, p.SecretGeneration), p.AuthCiphertext)
	if err != nil {
		return nil, noop, namedIdentityUnavailable
	}
	t, err := s.namedIdentityTransport(ctx)
	if err != nil {
		return nil, noop, namedIdentityUnavailable
	}
	client, err := oauthprotocol.New(ctx, oauthprotocol.Config{
		AuthorizationURL: discordAuthorizationURL, TokenURL: discordTokenURL, UserInfoURL: discordUserURL,
		RedirectURL: p.CallbackURL, ClientID: p.ClientID, ClientSecret: plaintext,
		ClientAuthMethod: oauthprotocol.ClientSecretBasic, Scopes: []string{"identify"}, SubjectPath: []string{"id"},
		Transport: discordRequestTransport{next: t.RoundTripper},
		EndpointPolicy: func(ctx context.Context, target *url.URL) error {
			return discordEndpointPolicy(ctx, target, p.ClientID, p.CallbackURL, s.allowPrivateUpstream)
		},
	})
	if err != nil {
		_ = t.Close()
		return nil, noop, namedIdentityUnavailable
	}
	var once sync.Once
	var closeErr error
	closeTransport := func() error { once.Do(func() { closeErr = t.Close() }); return closeErr }
	return discordProtocolAdapter{client: client}, closeTransport, nil
}

type discordRequestTransport struct{ next http.RoundTripper }

func (t discordRequestTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r == nil || r.URL == nil || r.Context().Err() != nil || r.URL.User != nil || r.URL.RawQuery != "" || r.URL.ForceQuery || r.URL.Fragment != "" || r.URL.RawFragment != "" || r.URL.Opaque != "" || r.URL.RawPath != "" {
		return nil, namedIdentityUnavailable
	}
	tokenRequest := r.Method == http.MethodPost && r.URL.String() == discordTokenURL
	userRequest := r.Method == http.MethodGet && r.URL.String() == discordUserURL
	if !tokenRequest && !userRequest {
		return nil, namedIdentityUnavailable
	}
	return t.next.RoundTrip(r)
}
