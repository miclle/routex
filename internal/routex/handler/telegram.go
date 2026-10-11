package handler

import (
	"net/http"
	"strconv"

	"github.com/fox-gonic/fox"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
)

func (ctrl *Ctrl) PublicTelegram(c *fox.Context) (*service.TelegramPublic, error) {
	if captureTelegramInputs(c.Request).invalid {
		return nil, apperrors.ErrBadRequest
	}
	return ctrl.service.PublicTelegram(c.Request.Context())
}
func telegramSetCookie(c *fox.Context, start *service.TelegramStart) {
	http.SetCookie(c.Writer, &http.Cookie{Name: telegramCookie, Value: start.Cookie, Path: telegramCookiePath, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: 300, Expires: start.ExpiresAt})
}
func telegramClearCookie(c *fox.Context) {
	http.SetCookie(c.Writer, &http.Cookie{Name: telegramCookie, Value: "", Path: telegramCookiePath, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: -1})
}
func (ctrl *Ctrl) StartTelegramLogin(c *fox.Context) (*service.TelegramStart, error) {
	if captureTelegramInputs(c.Request).invalid {
		return nil, apperrors.ErrBadRequest
	}
	if _, err := decodeMFARequest[struct{}](c); err != nil {
		return nil, err
	}
	auth, err := ctrl.optionalIdentitySession(c)
	if err != nil {
		return nil, err
	}
	if auth != nil {
		return nil, apperrors.ErrForbidden
	}
	result, err := ctrl.service.StartTelegramLogin(c.Request.Context())
	if err == nil {
		telegramSetCookie(c, result)
	}
	return result, err
}
func (ctrl *Ctrl) TelegramCallback(c *fox.Context) {
	in := captureTelegramInputs(c.Request)
	c.Header("Referrer-Policy", "no-referrer")
	if in.invalid || in.cookie == "" || ctrl.service.ReceiveTelegramCallback(c.Request.Context(), in.cookie, in.state, in.code, in.remoteError) != nil {
		telegramClearCookie(c)
	}
	c.Redirect(http.StatusSeeOther, "/auth/telegram/complete")
}
func (ctrl *Ctrl) CompleteTelegram(c *fox.Context) error {
	in := captureTelegramInputs(c.Request)
	if in.invalid || in.cookie == "" {
		return apperrors.ErrUnauthorized
	}
	if _, err := decodeMFARequest[struct{}](c); err != nil {
		return err
	}
	auth, err := ctrl.optionalIdentitySession(c)
	if err != nil {
		return err
	}
	if auth != nil && !auth.CheckCSRF(c.Request.Header.Get("X-CSRF-Token")) {
		return apperrors.ErrForbidden
	}
	result, err := ctrl.service.CompleteTelegram(c.Request.Context(), auth, in.cookie)
	telegramClearCookie(c)
	if err != nil {
		return err
	}
	return identityCompletionResponse(c, result.Kind, result.Authentication, result.Challenge)
}
func (ctrl *Ctrl) GetTelegramProvider(c *fox.Context) (*service.TelegramProviderView, error) {
	if err := telegramNoQuery(c); err != nil {
		return nil, err
	}
	result, err := ctrl.service.GetTelegramProvider(c.Request.Context(), currentAuthentication(c).User.ID)
	if err == nil {
		c.Header("ETag", strconv.Quote(result.ReviewETag))
	}
	return result, err
}
func (ctrl *Ctrl) SaveTelegramProvider(c *fox.Context) (*service.TelegramProviderView, error) {
	if err := telegramNoQuery(c); err != nil {
		return nil, err
	}
	etag, err := identityReviewETag(c)
	if err != nil {
		return nil, err
	}
	in, err := decodeMFARequest[service.TelegramProviderInput](c)
	if err != nil {
		return nil, err
	}
	result, err := ctrl.service.SaveTelegramProvider(c.Request.Context(), currentAuthentication(c).User.ID, etag, *in)
	if err == nil {
		c.Header("ETag", strconv.Quote(result.ReviewETag))
	}
	return result, err
}
func (ctrl *Ctrl) SetTelegramEnabled(c *fox.Context) (*service.TelegramProviderView, error) {
	if err := telegramNoQuery(c); err != nil {
		return nil, err
	}
	etag, err := identityReviewETag(c)
	if err != nil {
		return nil, err
	}
	in, err := decodeMFARequest[service.TelegramStatusInput](c)
	if err != nil {
		return nil, err
	}
	result, err := ctrl.service.SetTelegramEnabled(c.Request.Context(), currentAuthentication(c).User.ID, etag, *in)
	if err == nil {
		c.Header("ETag", strconv.Quote(result.ReviewETag))
	}
	return result, err
}
func (ctrl *Ctrl) AccountTelegram(c *fox.Context) (*service.TelegramAccountView, error) {
	if err := telegramNoQuery(c); err != nil {
		return nil, err
	}
	result, err := ctrl.service.AccountTelegram(c.Request.Context(), currentAuthentication(c))
	if err == nil {
		c.Header("ETag", strconv.Quote(result.ReviewETag))
	}
	return result, err
}
func (ctrl *Ctrl) StartTelegramBinding(c *fox.Context) (*service.TelegramStart, error) {
	return ctrl.startTelegramIdentity(c, false)
}
func (ctrl *Ctrl) StartTelegramVerification(c *fox.Context) (*service.TelegramStart, error) {
	return ctrl.startTelegramIdentity(c, true)
}
func (ctrl *Ctrl) startTelegramIdentity(c *fox.Context, verify bool) (*service.TelegramStart, error) {
	if err := telegramNoQuery(c); err != nil {
		return nil, err
	}
	etag, err := identityReviewETag(c)
	if err != nil {
		return nil, err
	}
	in, err := decodeMFARequest[service.TelegramIdentityInput](c)
	if err != nil {
		return nil, err
	}
	var result *service.TelegramStart
	if verify {
		result, err = ctrl.service.StartTelegramVerification(c.Request.Context(), currentAuthentication(c), etag, *in)
	} else {
		result, err = ctrl.service.StartTelegramBinding(c.Request.Context(), currentAuthentication(c), etag, *in)
	}
	if err == nil {
		telegramSetCookie(c, result)
	}
	return result, err
}
func (ctrl *Ctrl) UnlinkTelegram(c *fox.Context) (*service.TelegramAccountView, error) {
	if err := telegramNoQuery(c); err != nil {
		return nil, err
	}
	etag, err := identityReviewETag(c)
	if err != nil {
		return nil, err
	}
	in, err := decodeMFARequest[service.TelegramIdentityInput](c)
	if err != nil {
		return nil, err
	}
	result, err := ctrl.service.UnlinkTelegram(c.Request.Context(), currentAuthentication(c), etag, *in)
	if err == nil {
		c.Header("ETag", strconv.Quote(result.ReviewETag))
		if currentAuthentication(c).Session.PrimaryMethod == "telegram" {
			http.SetCookie(c.Writer, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", HttpOnly: true, Secure: c.Request.TLS != nil, SameSite: http.SameSiteStrictMode, MaxAge: -1})
		}
	}
	return result, err
}

// Abandon clears browser correlation only; it does not claim durable cancellation.
func (ctrl *Ctrl) AbandonTelegram(c *fox.Context) error {
	if captureTelegramInputs(c.Request).invalid {
		return apperrors.ErrBadRequest
	}
	if _, err := decodeMFARequest[struct{}](c); err != nil {
		return err
	}
	auth, err := ctrl.optionalIdentitySession(c)
	if err != nil {
		return err
	}
	if auth != nil && !auth.CheckCSRF(c.Request.Header.Get("X-CSRF-Token")) {
		return apperrors.ErrForbidden
	}
	telegramClearCookie(c)
	c.Status(http.StatusNoContent)
	return nil
}

func telegramNoQuery(c *fox.Context) error {
	if captureTelegramInputs(c.Request).invalid {
		return apperrors.ErrBadRequest
	}
	return identityNoQuery(c)
}
