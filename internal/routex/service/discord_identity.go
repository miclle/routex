package service

import (
	"context"
	"encoding/json"
	apperrors "github.com/miclle/routex/internal/routex/errors"
)

type DiscordPublic = GitHubPublic
type DiscordProviderView = GitHubProviderView
type DiscordStatusInput = GitHubStatusInput
type DiscordAccountView = GitHubAccountView
type DiscordIdentityInput = GitHubIdentityInput
type DiscordProof = GitHubProof
type DiscordStart = GitHubStart
type DiscordCompletion = GitHubCompletion
type DiscordProviderInput GitHubProviderInput

func (in *DiscordProviderInput) UnmarshalJSON(raw []byte) error {
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
	if namedIdentityValidateConfigInputFor(discordProviderID, &v) != nil {
		return apperrors.ErrBadRequest
	}
	*in = DiscordProviderInput(v)
	return nil
}
func (s *Service) PublicDiscord(ctx context.Context) (*DiscordPublic, error) {
	return s.publicNamedIdentity(ctx, discordProviderID)
}
func (s *Service) GetDiscordProvider(ctx context.Context, actorID string) (*DiscordProviderView, error) {
	return s.getNamedIdentityProvider(ctx, actorID, discordProviderID)
}
func (s *Service) SaveDiscordProvider(ctx context.Context, actorID, etag string, in DiscordProviderInput) (*DiscordProviderView, error) {
	return s.saveNamedIdentityProvider(ctx, actorID, etag, discordProviderID, GitHubProviderInput(in))
}
func (s *Service) SetDiscordEnabled(ctx context.Context, actorID, etag string, in DiscordStatusInput) (*DiscordProviderView, error) {
	return s.setNamedIdentityEnabled(ctx, actorID, etag, discordProviderID, in)
}
func (s *Service) AccountDiscord(ctx context.Context, a *Authentication) (*DiscordAccountView, error) {
	return s.accountNamedIdentity(ctx, a, discordProviderID)
}
func (s *Service) UnlinkDiscord(ctx context.Context, a *Authentication, etag string, in DiscordIdentityInput) (*DiscordAccountView, error) {
	return s.unlinkNamedIdentity(ctx, a, etag, discordProviderID, in)
}
func (s *Service) CompleteDiscord(ctx context.Context, a *Authentication, cookie string) (*DiscordCompletion, error) {
	return s.completeNamedIdentity(ctx, a, cookie, discordProviderID)
}
func (s *Service) ReceiveDiscordCallback(ctx context.Context, cookie, state, code, remoteError string) error {
	return s.receiveNamedIdentityCallback(ctx, cookie, state, code, remoteError, "", discordProviderID)
}
func (s *Service) StartDiscordLogin(ctx context.Context) (*DiscordStart, error) {
	return s.startNamedIdentity(ctx, nil, "", GitHubIdentityInput{}, "login", discordProviderID)
}
func (s *Service) StartDiscordBinding(ctx context.Context, a *Authentication, etag string, in DiscordIdentityInput) (*DiscordStart, error) {
	return s.startNamedIdentity(ctx, a, etag, in, "bind", discordProviderID)
}
func (s *Service) StartDiscordVerification(ctx context.Context, a *Authentication, etag string, in DiscordIdentityInput) (*DiscordStart, error) {
	return s.startNamedIdentity(ctx, a, etag, in, "verify", discordProviderID)
}
