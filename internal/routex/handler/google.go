package handler

import (
	"net/http"
	"strconv"

	"github.com/fox-gonic/fox"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
)

func (ctrl *Ctrl) PublicGoogle(c *fox.Context) (*service.GooglePublic, error) {
	if captureGoogleInputs(c.Request).invalid {
		return nil, apperrors.ErrBadRequest
	}
	return ctrl.service.PublicGoogle(c.Request.Context())
}
func googleSetCookie(c *fox.Context, start *service.GoogleStart) {
	http.SetCookie(c.Writer, &http.Cookie{Name: googleCookie, Value: start.Cookie, Path: googleCookiePath, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: 300, Expires: start.ExpiresAt})
}
func googleClearCookie(c *fox.Context) {
	http.SetCookie(c.Writer, &http.Cookie{Name: googleCookie, Value: "", Path: googleCookiePath, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: -1})
}
func (ctrl *Ctrl) StartGoogleLogin(c *fox.Context) (*service.GoogleStart, error) {
	if captureGoogleInputs(c.Request).invalid {
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
	result, err := ctrl.service.StartGoogleLogin(c.Request.Context())
	if err == nil {
		googleSetCookie(c, result)
	}
	return result, err
}
func (ctrl *Ctrl) GoogleCallback(c *fox.Context) {
	in := captureGoogleInputs(c.Request)
	c.Header("Referrer-Policy", "no-referrer")
	if in.invalid || in.cookie == "" || ctrl.service.ReceiveGoogleCallback(c.Request.Context(), in.cookie, in.state, in.code, in.remoteError, in.responseIssuer) != nil {
		googleClearCookie(c)
	}
	c.Redirect(http.StatusSeeOther, "/auth/google/complete")
}
func (ctrl *Ctrl) CompleteGoogle(c *fox.Context) error {
	in := captureGoogleInputs(c.Request)
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
	result, err := ctrl.service.CompleteGoogle(c.Request.Context(), auth, in.cookie)
	googleClearCookie(c)
	if err != nil {
		return err
	}
	return identityCompletionResponse(c, result.Kind, result.Authentication, result.Challenge)
}
func (ctrl *Ctrl) GetGoogleProvider(c *fox.Context) (*service.GoogleProviderView, error) {
	if err := googleNoQuery(c); err != nil {
		return nil, err
	}
	result, err := ctrl.service.GetGoogleProvider(c.Request.Context(), currentAuthentication(c).User.ID)
	if err == nil {
		c.Header("ETag", strconv.Quote(result.ReviewETag))
	}
	return result, err
}
func (ctrl *Ctrl) SaveGoogleProvider(c *fox.Context) (*service.GoogleProviderView, error) {
	if err := googleNoQuery(c); err != nil {
		return nil, err
	}
	etag, err := identityReviewETag(c)
	if err != nil {
		return nil, err
	}
	in, err := decodeMFARequest[service.GoogleProviderInput](c)
	if err != nil {
		return nil, err
	}
	result, err := ctrl.service.SaveGoogleProvider(c.Request.Context(), currentAuthentication(c).User.ID, etag, *in)
	if err == nil {
		c.Header("ETag", strconv.Quote(result.ReviewETag))
	}
	return result, err
}
func (ctrl *Ctrl) SetGoogleEnabled(c *fox.Context) (*service.GoogleProviderView, error) {
	if err := googleNoQuery(c); err != nil {
		return nil, err
	}
	etag, err := identityReviewETag(c)
	if err != nil {
		return nil, err
	}
	in, err := decodeMFARequest[service.GoogleStatusInput](c)
	if err != nil {
		return nil, err
	}
	result, err := ctrl.service.SetGoogleEnabled(c.Request.Context(), currentAuthentication(c).User.ID, etag, *in)
	if err == nil {
		c.Header("ETag", strconv.Quote(result.ReviewETag))
	}
	return result, err
}
func (ctrl *Ctrl) AccountGoogle(c *fox.Context) (*service.GoogleAccountView, error) {
	if err := googleNoQuery(c); err != nil {
		return nil, err
	}
	result, err := ctrl.service.AccountGoogle(c.Request.Context(), currentAuthentication(c))
	if err == nil {
		c.Header("ETag", strconv.Quote(result.ReviewETag))
	}
	return result, err
}
func (ctrl *Ctrl) StartGoogleBinding(c *fox.Context) (*service.GoogleStart, error) {
	return ctrl.startGoogleIdentity(c, false)
}
func (ctrl *Ctrl) StartGoogleVerification(c *fox.Context) (*service.GoogleStart, error) {
	return ctrl.startGoogleIdentity(c, true)
}
func (ctrl *Ctrl) startGoogleIdentity(c *fox.Context, verify bool) (*service.GoogleStart, error) {
	if err := googleNoQuery(c); err != nil {
		return nil, err
	}
	etag, err := identityReviewETag(c)
	if err != nil {
		return nil, err
	}
	in, err := decodeMFARequest[service.GoogleIdentityInput](c)
	if err != nil {
		return nil, err
	}
	var result *service.GoogleStart
	if verify {
		result, err = ctrl.service.StartGoogleVerification(c.Request.Context(), currentAuthentication(c), etag, *in)
	} else {
		result, err = ctrl.service.StartGoogleBinding(c.Request.Context(), currentAuthentication(c), etag, *in)
	}
	if err == nil {
		googleSetCookie(c, result)
	}
	return result, err
}
func (ctrl *Ctrl) UnlinkGoogle(c *fox.Context) (*service.GoogleAccountView, error) {
	if err := googleNoQuery(c); err != nil {
		return nil, err
	}
	etag, err := identityReviewETag(c)
	if err != nil {
		return nil, err
	}
	in, err := decodeMFARequest[service.GoogleIdentityInput](c)
	if err != nil {
		return nil, err
	}
	result, err := ctrl.service.UnlinkGoogle(c.Request.Context(), currentAuthentication(c), etag, *in)
	if err == nil {
		c.Header("ETag", strconv.Quote(result.ReviewETag))
		if currentAuthentication(c).Session.PrimaryMethod == "google" {
			http.SetCookie(c.Writer, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", HttpOnly: true, Secure: c.Request.TLS != nil, SameSite: http.SameSiteStrictMode, MaxAge: -1})
		}
	}
	return result, err
}

// Abandon clears browser correlation only; it does not claim durable cancellation.
func (ctrl *Ctrl) AbandonGoogle(c *fox.Context) error {
	if captureGoogleInputs(c.Request).invalid {
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
	googleClearCookie(c)
	c.Status(http.StatusNoContent)
	return nil
}

func googleNoQuery(c *fox.Context) error {
	if captureGoogleInputs(c.Request).invalid {
		return apperrors.ErrBadRequest
	}
	return identityNoQuery(c)
}
