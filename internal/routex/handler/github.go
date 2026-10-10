package handler

import (
	"net/http"
	"strconv"

	"github.com/fox-gonic/fox"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
)

func (ctrl *Ctrl) PublicGitHub(c *fox.Context) (*service.GitHubPublic, error) {
	if captureGitHubInputs(c.Request).invalid {
		return nil, apperrors.ErrBadRequest
	}
	return ctrl.service.PublicGitHub(c.Request.Context())
}
func githubSetCookie(c *fox.Context, start *service.GitHubStart) {
	http.SetCookie(c.Writer, &http.Cookie{Name: githubCookie, Value: start.Cookie, Path: githubCookiePath, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: 300, Expires: start.ExpiresAt})
}
func githubClearCookie(c *fox.Context) {
	http.SetCookie(c.Writer, &http.Cookie{Name: githubCookie, Value: "", Path: githubCookiePath, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: -1})
}
func (ctrl *Ctrl) StartGitHubLogin(c *fox.Context) (*service.GitHubStart, error) {
	if captureGitHubInputs(c.Request).invalid {
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
	result, err := ctrl.service.StartGitHubLogin(c.Request.Context())
	if err == nil {
		githubSetCookie(c, result)
	}
	return result, err
}
func (ctrl *Ctrl) GitHubCallback(c *fox.Context) {
	in := captureGitHubInputs(c.Request)
	c.Header("Referrer-Policy", "no-referrer")
	if in.invalid || in.cookie == "" || ctrl.service.ReceiveGitHubCallback(c.Request.Context(), in.cookie, in.state, in.code, in.remoteError) != nil {
		githubClearCookie(c)
	}
	c.Redirect(http.StatusSeeOther, "/auth/github/complete")
}
func (ctrl *Ctrl) CompleteGitHub(c *fox.Context) error {
	in := captureGitHubInputs(c.Request)
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
	result, err := ctrl.service.CompleteGitHub(c.Request.Context(), auth, in.cookie)
	githubClearCookie(c)
	if err != nil {
		return err
	}
	return identityCompletionResponse(c, result.Kind, result.Authentication, result.Challenge)
}
func (ctrl *Ctrl) GetGitHubProvider(c *fox.Context) (*service.GitHubProviderView, error) {
	if err := githubNoQuery(c); err != nil {
		return nil, err
	}
	result, err := ctrl.service.GetGitHubProvider(c.Request.Context(), currentAuthentication(c).User.ID)
	if err == nil {
		c.Header("ETag", strconv.Quote(result.ReviewETag))
	}
	return result, err
}
func (ctrl *Ctrl) SaveGitHubProvider(c *fox.Context) (*service.GitHubProviderView, error) {
	if err := githubNoQuery(c); err != nil {
		return nil, err
	}
	etag, err := identityReviewETag(c)
	if err != nil {
		return nil, err
	}
	in, err := decodeMFARequest[service.GitHubProviderInput](c)
	if err != nil {
		return nil, err
	}
	result, err := ctrl.service.SaveGitHubProvider(c.Request.Context(), currentAuthentication(c).User.ID, etag, *in)
	if err == nil {
		c.Header("ETag", strconv.Quote(result.ReviewETag))
	}
	return result, err
}
func (ctrl *Ctrl) SetGitHubEnabled(c *fox.Context) (*service.GitHubProviderView, error) {
	if err := githubNoQuery(c); err != nil {
		return nil, err
	}
	etag, err := identityReviewETag(c)
	if err != nil {
		return nil, err
	}
	in, err := decodeMFARequest[service.GitHubStatusInput](c)
	if err != nil {
		return nil, err
	}
	result, err := ctrl.service.SetGitHubEnabled(c.Request.Context(), currentAuthentication(c).User.ID, etag, *in)
	if err == nil {
		c.Header("ETag", strconv.Quote(result.ReviewETag))
	}
	return result, err
}
func (ctrl *Ctrl) AccountGitHub(c *fox.Context) (*service.GitHubAccountView, error) {
	if err := githubNoQuery(c); err != nil {
		return nil, err
	}
	result, err := ctrl.service.AccountGitHub(c.Request.Context(), currentAuthentication(c))
	if err == nil {
		c.Header("ETag", strconv.Quote(result.ReviewETag))
	}
	return result, err
}
func (ctrl *Ctrl) StartGitHubBinding(c *fox.Context) (*service.GitHubStart, error) {
	return ctrl.startGitHubIdentity(c, false)
}
func (ctrl *Ctrl) StartGitHubVerification(c *fox.Context) (*service.GitHubStart, error) {
	return ctrl.startGitHubIdentity(c, true)
}
func (ctrl *Ctrl) startGitHubIdentity(c *fox.Context, verify bool) (*service.GitHubStart, error) {
	if err := githubNoQuery(c); err != nil {
		return nil, err
	}
	etag, err := identityReviewETag(c)
	if err != nil {
		return nil, err
	}
	in, err := decodeMFARequest[service.GitHubIdentityInput](c)
	if err != nil {
		return nil, err
	}
	var result *service.GitHubStart
	if verify {
		result, err = ctrl.service.StartGitHubVerification(c.Request.Context(), currentAuthentication(c), etag, *in)
	} else {
		result, err = ctrl.service.StartGitHubBinding(c.Request.Context(), currentAuthentication(c), etag, *in)
	}
	if err == nil {
		githubSetCookie(c, result)
	}
	return result, err
}
func (ctrl *Ctrl) UnlinkGitHub(c *fox.Context) (*service.GitHubAccountView, error) {
	if err := githubNoQuery(c); err != nil {
		return nil, err
	}
	etag, err := identityReviewETag(c)
	if err != nil {
		return nil, err
	}
	in, err := decodeMFARequest[service.GitHubIdentityInput](c)
	if err != nil {
		return nil, err
	}
	result, err := ctrl.service.UnlinkGitHub(c.Request.Context(), currentAuthentication(c), etag, *in)
	if err == nil {
		c.Header("ETag", strconv.Quote(result.ReviewETag))
		if currentAuthentication(c).Session.PrimaryMethod == "github" {
			http.SetCookie(c.Writer, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", HttpOnly: true, Secure: c.Request.TLS != nil, SameSite: http.SameSiteStrictMode, MaxAge: -1})
		}
	}
	return result, err
}

// Abandon clears browser correlation only; it does not claim durable cancellation.
func (ctrl *Ctrl) AbandonGitHub(c *fox.Context) error {
	if captureGitHubInputs(c.Request).invalid {
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
	githubClearCookie(c)
	c.Status(http.StatusNoContent)
	return nil
}

func githubNoQuery(c *fox.Context) error {
	if captureGitHubInputs(c.Request).invalid {
		return apperrors.ErrBadRequest
	}
	return identityNoQuery(c)
}
