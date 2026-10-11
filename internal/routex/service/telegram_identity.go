package service

import (
	"context"
	"encoding/json"
	apperrors "github.com/miclle/routex/internal/routex/errors"
)

type TelegramPublic = GitHubPublic
type TelegramProviderView = GitHubProviderView
type TelegramStatusInput = GitHubStatusInput
type TelegramAccountView = GitHubAccountView
type TelegramIdentityInput = GitHubIdentityInput
type TelegramProof = GitHubProof
type TelegramStart = GitHubStart
type TelegramCompletion = GitHubCompletion
type TelegramProviderInput GitHubProviderInput

func (in *TelegramProviderInput) UnmarshalJSON(raw []byte) error {
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
	if namedIdentityValidateConfigInputFor(telegramProviderID, &v) != nil {
		return apperrors.ErrBadRequest
	}
	*in = TelegramProviderInput(v)
	return nil
}
func (s *Service) PublicTelegram(ctx context.Context) (*TelegramPublic, error) {
	return s.publicNamedIdentity(ctx, telegramProviderID)
}
func (s *Service) GetTelegramProvider(ctx context.Context, actorID string) (*TelegramProviderView, error) {
	return s.getNamedIdentityProvider(ctx, actorID, telegramProviderID)
}
func (s *Service) SaveTelegramProvider(ctx context.Context, actorID, etag string, in TelegramProviderInput) (*TelegramProviderView, error) {
	return s.saveNamedIdentityProvider(ctx, actorID, etag, telegramProviderID, GitHubProviderInput(in))
}
func (s *Service) SetTelegramEnabled(ctx context.Context, actorID, etag string, in TelegramStatusInput) (*TelegramProviderView, error) {
	return s.setNamedIdentityEnabled(ctx, actorID, etag, telegramProviderID, in)
}
func (s *Service) AccountTelegram(ctx context.Context, a *Authentication) (*TelegramAccountView, error) {
	return s.accountNamedIdentity(ctx, a, telegramProviderID)
}
func (s *Service) UnlinkTelegram(ctx context.Context, a *Authentication, etag string, in TelegramIdentityInput) (*TelegramAccountView, error) {
	return s.unlinkNamedIdentity(ctx, a, etag, telegramProviderID, in)
}
func (s *Service) CompleteTelegram(ctx context.Context, a *Authentication, cookie string) (*TelegramCompletion, error) {
	return s.completeNamedIdentity(ctx, a, cookie, telegramProviderID)
}
func (s *Service) ReceiveTelegramCallback(ctx context.Context, cookie, state, code, remoteError string) error {
	return s.receiveNamedIdentityCallback(ctx, cookie, state, code, remoteError, "", telegramProviderID)
}
func (s *Service) StartTelegramLogin(ctx context.Context) (*TelegramStart, error) {
	return s.startNamedIdentity(ctx, nil, "", GitHubIdentityInput{}, "login", telegramProviderID)
}
func (s *Service) StartTelegramBinding(ctx context.Context, a *Authentication, etag string, in TelegramIdentityInput) (*TelegramStart, error) {
	return s.startNamedIdentity(ctx, a, etag, in, "bind", telegramProviderID)
}
func (s *Service) StartTelegramVerification(ctx context.Context, a *Authentication, etag string, in TelegramIdentityInput) (*TelegramStart, error) {
	return s.startNamedIdentity(ctx, a, etag, in, "verify", telegramProviderID)
}
