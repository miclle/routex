package service

import (
	"context"
	"encoding/json"
	apperrors "github.com/miclle/routex/internal/routex/errors"
)

type GooglePublic = GitHubPublic
type GoogleProviderView = GitHubProviderView
type GoogleStatusInput = GitHubStatusInput
type GoogleAccountView = GitHubAccountView
type GoogleIdentityInput = GitHubIdentityInput
type GoogleProof = GitHubProof
type GoogleStart = GitHubStart
type GoogleCompletion = GitHubCompletion
type GoogleProviderInput GitHubProviderInput

func (in *GoogleProviderInput) UnmarshalJSON(raw []byte) error {
	f, e := registrationStrictObject(raw, []string{"name", "client_id", "callback_url", "secret_action", "client_secret", "reason"})
	if e != nil {
		return e
	}
	var v GitHubProviderInput
	for k, d := range map[string]*string{"name": &v.Name, "client_id": &v.ClientID, "callback_url": &v.CallbackURL, "secret_action": &v.SecretAction, "client_secret": &v.ClientSecret, "reason": &v.Reason} {
		if json.Unmarshal(f[k], d) != nil {
			return apperrors.ErrBadRequest
		}
	}
	if namedIdentityValidateConfigInputFor(googleProviderID, &v) != nil {
		return apperrors.ErrBadRequest
	}
	*in = GoogleProviderInput(v)
	return nil
}
func (s *Service) PublicGitHub(ctx context.Context) (*GitHubPublic, error) {
	return s.publicNamedIdentity(ctx, githubProviderID)
}
func (s *Service) GetGitHubProvider(ctx context.Context, actorID string) (*GitHubProviderView, error) {
	return s.getNamedIdentityProvider(ctx, actorID, githubProviderID)
}
func (s *Service) SaveGitHubProvider(ctx context.Context, actorID, etag string, in GitHubProviderInput) (*GitHubProviderView, error) {
	return s.saveNamedIdentityProvider(ctx, actorID, etag, githubProviderID, GitHubProviderInput(in))
}
func (s *Service) SetGitHubEnabled(ctx context.Context, actorID, etag string, in GitHubStatusInput) (*GitHubProviderView, error) {
	return s.setNamedIdentityEnabled(ctx, actorID, etag, githubProviderID, in)
}
func (s *Service) AccountGitHub(ctx context.Context, a *Authentication) (*GitHubAccountView, error) {
	return s.accountNamedIdentity(ctx, a, githubProviderID)
}
func (s *Service) UnlinkGitHub(ctx context.Context, a *Authentication, etag string, in GitHubIdentityInput) (*GitHubAccountView, error) {
	return s.unlinkNamedIdentity(ctx, a, etag, githubProviderID, in)
}
func (s *Service) CompleteGitHub(ctx context.Context, a *Authentication, cookie string) (*GitHubCompletion, error) {
	return s.completeNamedIdentity(ctx, a, cookie, githubProviderID)
}
func (s *Service) PublicGoogle(ctx context.Context) (*GooglePublic, error) {
	return s.publicNamedIdentity(ctx, googleProviderID)
}
func (s *Service) GetGoogleProvider(ctx context.Context, actorID string) (*GoogleProviderView, error) {
	return s.getNamedIdentityProvider(ctx, actorID, googleProviderID)
}
func (s *Service) SaveGoogleProvider(ctx context.Context, actorID, etag string, in GoogleProviderInput) (*GoogleProviderView, error) {
	return s.saveNamedIdentityProvider(ctx, actorID, etag, googleProviderID, GitHubProviderInput(in))
}
func (s *Service) SetGoogleEnabled(ctx context.Context, actorID, etag string, in GoogleStatusInput) (*GoogleProviderView, error) {
	return s.setNamedIdentityEnabled(ctx, actorID, etag, googleProviderID, in)
}
func (s *Service) AccountGoogle(ctx context.Context, a *Authentication) (*GoogleAccountView, error) {
	return s.accountNamedIdentity(ctx, a, googleProviderID)
}
func (s *Service) UnlinkGoogle(ctx context.Context, a *Authentication, etag string, in GoogleIdentityInput) (*GoogleAccountView, error) {
	return s.unlinkNamedIdentity(ctx, a, etag, googleProviderID, in)
}
func (s *Service) CompleteGoogle(ctx context.Context, a *Authentication, cookie string) (*GoogleCompletion, error) {
	return s.completeNamedIdentity(ctx, a, cookie, googleProviderID)
}
func (s *Service) ReceiveGitHubCallback(ctx context.Context, cookie, state, code, remoteError string) error {
	return s.receiveNamedIdentityCallback(ctx, cookie, state, code, remoteError, "", githubProviderID)
}
func (s *Service) ReceiveGoogleCallback(ctx context.Context, cookie, state, code, remoteError, responseIssuer string) error {
	return s.receiveNamedIdentityCallback(ctx, cookie, state, code, remoteError, responseIssuer, googleProviderID)
}
func (s *Service) StartGoogleLogin(ctx context.Context) (*GoogleStart, error) {
	return s.startNamedIdentity(ctx, nil, "", GitHubIdentityInput{}, "login", googleProviderID)
}
func (s *Service) StartGoogleBinding(ctx context.Context, a *Authentication, etag string, in GoogleIdentityInput) (*GoogleStart, error) {
	return s.startNamedIdentity(ctx, a, etag, in, "bind", googleProviderID)
}
func (s *Service) StartGoogleVerification(ctx context.Context, a *Authentication, etag string, in GoogleIdentityInput) (*GoogleStart, error) {
	return s.startNamedIdentity(ctx, a, etag, in, "verify", googleProviderID)
}
