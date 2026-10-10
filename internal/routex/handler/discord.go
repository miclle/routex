package handler

import (
	"net/http"
	"strconv"

	"github.com/fox-gonic/fox"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
)

func (ctrl *Ctrl) PublicDiscord(c *fox.Context) (*service.DiscordPublic, error) {
	if captureDiscordInputs(c.Request).invalid {
		return nil, apperrors.ErrBadRequest
	}
	return ctrl.service.PublicDiscord(c.Request.Context())
}
func discordSetCookie(c *fox.Context, start *service.DiscordStart) {
	http.SetCookie(c.Writer, &http.Cookie{Name: discordCookie, Value: start.Cookie, Path: discordCookiePath, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: 300, Expires: start.ExpiresAt})
}
func discordClearCookie(c *fox.Context) {
	http.SetCookie(c.Writer, &http.Cookie{Name: discordCookie, Value: "", Path: discordCookiePath, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: -1})
}
func (ctrl *Ctrl) StartDiscordLogin(c *fox.Context) (*service.DiscordStart, error) {
	if captureDiscordInputs(c.Request).invalid {
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
	result, err := ctrl.service.StartDiscordLogin(c.Request.Context())
	if err == nil {
		discordSetCookie(c, result)
	}
	return result, err
}
func (ctrl *Ctrl) DiscordCallback(c *fox.Context) {
	in := captureDiscordInputs(c.Request)
	c.Header("Referrer-Policy", "no-referrer")
	if in.invalid || in.cookie == "" || ctrl.service.ReceiveDiscordCallback(c.Request.Context(), in.cookie, in.state, in.code, in.remoteError) != nil {
		discordClearCookie(c)
	}
	c.Redirect(http.StatusSeeOther, "/auth/discord/complete")
}
func (ctrl *Ctrl) CompleteDiscord(c *fox.Context) error {
	in := captureDiscordInputs(c.Request)
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
	result, err := ctrl.service.CompleteDiscord(c.Request.Context(), auth, in.cookie)
	discordClearCookie(c)
	if err != nil {
		return err
	}
	return identityCompletionResponse(c, result.Kind, result.Authentication, result.Challenge)
}
func (ctrl *Ctrl) GetDiscordProvider(c *fox.Context) (*service.DiscordProviderView, error) {
	if err := discordNoQuery(c); err != nil {
		return nil, err
	}
	result, err := ctrl.service.GetDiscordProvider(c.Request.Context(), currentAuthentication(c).User.ID)
	if err == nil {
		c.Header("ETag", strconv.Quote(result.ReviewETag))
	}
	return result, err
}
func (ctrl *Ctrl) SaveDiscordProvider(c *fox.Context) (*service.DiscordProviderView, error) {
	if err := discordNoQuery(c); err != nil {
		return nil, err
	}
	etag, err := identityReviewETag(c)
	if err != nil {
		return nil, err
	}
	in, err := decodeMFARequest[service.DiscordProviderInput](c)
	if err != nil {
		return nil, err
	}
	result, err := ctrl.service.SaveDiscordProvider(c.Request.Context(), currentAuthentication(c).User.ID, etag, *in)
	if err == nil {
		c.Header("ETag", strconv.Quote(result.ReviewETag))
	}
	return result, err
}
func (ctrl *Ctrl) SetDiscordEnabled(c *fox.Context) (*service.DiscordProviderView, error) {
	if err := discordNoQuery(c); err != nil {
		return nil, err
	}
	etag, err := identityReviewETag(c)
	if err != nil {
		return nil, err
	}
	in, err := decodeMFARequest[service.DiscordStatusInput](c)
	if err != nil {
		return nil, err
	}
	result, err := ctrl.service.SetDiscordEnabled(c.Request.Context(), currentAuthentication(c).User.ID, etag, *in)
	if err == nil {
		c.Header("ETag", strconv.Quote(result.ReviewETag))
	}
	return result, err
}
func (ctrl *Ctrl) AccountDiscord(c *fox.Context) (*service.DiscordAccountView, error) {
	if err := discordNoQuery(c); err != nil {
		return nil, err
	}
	result, err := ctrl.service.AccountDiscord(c.Request.Context(), currentAuthentication(c))
	if err == nil {
		c.Header("ETag", strconv.Quote(result.ReviewETag))
	}
	return result, err
}
func (ctrl *Ctrl) StartDiscordBinding(c *fox.Context) (*service.DiscordStart, error) {
	return ctrl.startDiscordIdentity(c, false)
}
func (ctrl *Ctrl) StartDiscordVerification(c *fox.Context) (*service.DiscordStart, error) {
	return ctrl.startDiscordIdentity(c, true)
}
func (ctrl *Ctrl) startDiscordIdentity(c *fox.Context, verify bool) (*service.DiscordStart, error) {
	if err := discordNoQuery(c); err != nil {
		return nil, err
	}
	etag, err := identityReviewETag(c)
	if err != nil {
		return nil, err
	}
	in, err := decodeMFARequest[service.DiscordIdentityInput](c)
	if err != nil {
		return nil, err
	}
	var result *service.DiscordStart
	if verify {
		result, err = ctrl.service.StartDiscordVerification(c.Request.Context(), currentAuthentication(c), etag, *in)
	} else {
		result, err = ctrl.service.StartDiscordBinding(c.Request.Context(), currentAuthentication(c), etag, *in)
	}
	if err == nil {
		discordSetCookie(c, result)
	}
	return result, err
}
func (ctrl *Ctrl) UnlinkDiscord(c *fox.Context) (*service.DiscordAccountView, error) {
	if err := discordNoQuery(c); err != nil {
		return nil, err
	}
	etag, err := identityReviewETag(c)
	if err != nil {
		return nil, err
	}
	in, err := decodeMFARequest[service.DiscordIdentityInput](c)
	if err != nil {
		return nil, err
	}
	result, err := ctrl.service.UnlinkDiscord(c.Request.Context(), currentAuthentication(c), etag, *in)
	if err == nil {
		c.Header("ETag", strconv.Quote(result.ReviewETag))
		if currentAuthentication(c).Session.PrimaryMethod == "discord" {
			http.SetCookie(c.Writer, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", HttpOnly: true, Secure: c.Request.TLS != nil, SameSite: http.SameSiteStrictMode, MaxAge: -1})
		}
	}
	return result, err
}

// Abandon clears browser correlation only; it does not claim durable cancellation.
func (ctrl *Ctrl) AbandonDiscord(c *fox.Context) error {
	if captureDiscordInputs(c.Request).invalid {
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
	discordClearCookie(c)
	c.Status(http.StatusNoContent)
	return nil
}

func discordNoQuery(c *fox.Context) error {
	if captureDiscordInputs(c.Request).invalid {
		return apperrors.ErrBadRequest
	}
	return identityNoQuery(c)
}
