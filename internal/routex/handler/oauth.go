package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/fox-gonic/fox"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
)

func oauthReviewETag(c *fox.Context) (string, error) {
	headers := c.Request.Header.Values("If-Match")
	if len(headers) != 1 {
		return "", apperrors.ErrBadRequest
	}
	value := headers[0]
	if len(value) != 66 || value[0] != '"' || value[65] != '"' {
		return "", apperrors.ErrBadRequest
	}
	value = value[1:65]
	for _, ch := range value {
		if (ch < '0' || ch > '9') && (ch < 'a' || ch > 'f') {
			return "", apperrors.ErrBadRequest
		}
	}
	return value, nil
}
func oauthNoQuery(c *fox.Context) error {
	if c.Request.URL.RawQuery != "" || c.Request.URL.ForceQuery || c.Request.ContentLength > 64<<10 {
		return apperrors.ErrBadRequest
	}
	return nil
}
func (ctrl *Ctrl) PublicOAuth(c *fox.Context) (*service.OAuthPublic, error) {
	if captureOAuthInputs(c.Request).invalid {
		return nil, apperrors.ErrBadRequest
	}
	return ctrl.service.PublicOAuth(c.Request.Context())
}
func oauthSetCookie(c *fox.Context, start *service.OAuthStart) {
	http.SetCookie(c.Writer, &http.Cookie{Name: oauthCookie, Value: start.Cookie, Path: oauthCookiePath, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: 300, Expires: start.ExpiresAt})
}
func oauthClearCookie(c *fox.Context) {
	http.SetCookie(c.Writer, &http.Cookie{Name: oauthCookie, Value: "", Path: oauthCookiePath, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: -1})
}
func (ctrl *Ctrl) optionalOAuthSession(c *fox.Context) (*service.Authentication, error) {
	cookie, err := c.Request.Cookie(sessionCookie)
	if errors.Is(err, http.ErrNoCookie) {
		return nil, nil
	}
	if err != nil {
		return nil, apperrors.ErrUnauthorized
	}
	auth, err := ctrl.service.Authenticate(c.Request.Context(), cookie.Value)
	if errors.Is(err, apperrors.ErrUnauthorized) {
		return nil, nil
	}
	return auth, err
}
func (ctrl *Ctrl) StartOAuthLogin(c *fox.Context) (*service.OAuthStart, error) {
	if captureOAuthInputs(c.Request).invalid {
		return nil, apperrors.ErrBadRequest
	}
	if _, err := decodeMFARequest[struct{}](c); err != nil {
		return nil, err
	}
	auth, err := ctrl.optionalOAuthSession(c)
	if err != nil {
		return nil, err
	}
	if auth != nil {
		return nil, apperrors.ErrForbidden
	}
	result, err := ctrl.service.StartOAuthLogin(c.Request.Context())
	if err == nil {
		oauthSetCookie(c, result)
	}
	return result, err
}
func (ctrl *Ctrl) OAuthCallback(c *fox.Context) {
	in := captureOAuthInputs(c.Request)
	c.Header("Referrer-Policy", "no-referrer")
	if in.invalid || in.cookie == "" || ctrl.service.ReceiveOAuthCallback(c.Request.Context(), in.cookie, in.state, in.code, in.remoteError) != nil {
		oauthClearCookie(c)
	}
	c.Redirect(http.StatusSeeOther, "/auth/oauth/complete")
}
func (ctrl *Ctrl) CompleteOAuth(c *fox.Context) error {
	in := captureOAuthInputs(c.Request)
	if in.invalid || in.cookie == "" {
		return apperrors.ErrUnauthorized
	}
	if _, err := decodeMFARequest[struct{}](c); err != nil {
		return err
	}
	auth, err := ctrl.optionalOAuthSession(c)
	if err != nil {
		return err
	}
	if auth != nil && !auth.CheckCSRF(c.Request.Header.Get("X-CSRF-Token")) {
		return apperrors.ErrForbidden
	}
	result, err := ctrl.service.CompleteOAuth(c.Request.Context(), auth, in.cookie)
	oauthClearCookie(c)
	if err != nil {
		return err
	}
	switch result.Kind {
	case "session":
		if result.Authentication == nil {
			return apperrors.ErrInternal
		}
		setSessionCookie(c, result.Authentication)
		c.JSON(http.StatusOK, sessionResponse(result.Authentication))
	case "challenge":
		if result.Challenge == nil {
			return apperrors.ErrInternal
		}
		c.JSON(http.StatusAccepted, result.Challenge)
	case "bound", "verified":
		c.JSON(http.StatusOK, struct {
			Kind string `json:"kind"`
		}{result.Kind})
	default:
		return apperrors.ErrInternal
	}
	return nil
}
func (ctrl *Ctrl) GetOAuthProvider(c *fox.Context) (*service.OAuthProviderView, error) {
	if err := oauthNoQuery(c); err != nil {
		return nil, err
	}
	result, err := ctrl.service.GetOAuthProvider(c.Request.Context(), currentAuthentication(c).User.ID)
	if err == nil {
		c.Header("ETag", strconv.Quote(result.ReviewETag))
	}
	return result, err
}
func (ctrl *Ctrl) SaveOAuthProvider(c *fox.Context) (*service.OAuthProviderView, error) {
	if err := oauthNoQuery(c); err != nil {
		return nil, err
	}
	etag, err := oauthReviewETag(c)
	if err != nil {
		return nil, err
	}
	in, err := decodeMFARequest[service.OAuthProviderInput](c)
	if err != nil {
		return nil, err
	}
	result, err := ctrl.service.SaveOAuthProvider(c.Request.Context(), currentAuthentication(c).User.ID, etag, *in)
	if err == nil {
		c.Header("ETag", strconv.Quote(result.ReviewETag))
	}
	return result, err
}
func (ctrl *Ctrl) SetOAuthEnabled(c *fox.Context) (*service.OAuthProviderView, error) {
	if err := oauthNoQuery(c); err != nil {
		return nil, err
	}
	etag, err := oauthReviewETag(c)
	if err != nil {
		return nil, err
	}
	in, err := decodeMFARequest[service.OAuthStatusInput](c)
	if err != nil {
		return nil, err
	}
	result, err := ctrl.service.SetOAuthEnabled(c.Request.Context(), currentAuthentication(c).User.ID, etag, *in)
	if err == nil {
		c.Header("ETag", strconv.Quote(result.ReviewETag))
	}
	return result, err
}
func (ctrl *Ctrl) AccountOAuth(c *fox.Context) (*service.OAuthAccountView, error) {
	if err := oauthNoQuery(c); err != nil {
		return nil, err
	}
	result, err := ctrl.service.AccountOAuth(c.Request.Context(), currentAuthentication(c))
	if err == nil {
		c.Header("ETag", strconv.Quote(result.ReviewETag))
	}
	return result, err
}
func (ctrl *Ctrl) StartOAuthBinding(c *fox.Context) (*service.OAuthStart, error) {
	return ctrl.startOAuthIdentity(c, false)
}
func (ctrl *Ctrl) StartOAuthVerification(c *fox.Context) (*service.OAuthStart, error) {
	return ctrl.startOAuthIdentity(c, true)
}
func (ctrl *Ctrl) startOAuthIdentity(c *fox.Context, verify bool) (*service.OAuthStart, error) {
	if err := oauthNoQuery(c); err != nil {
		return nil, err
	}
	etag, err := oauthReviewETag(c)
	if err != nil {
		return nil, err
	}
	in, err := decodeMFARequest[service.OAuthIdentityInput](c)
	if err != nil {
		return nil, err
	}
	var result *service.OAuthStart
	if verify {
		result, err = ctrl.service.StartOAuthVerification(c.Request.Context(), currentAuthentication(c), etag, *in)
	} else {
		result, err = ctrl.service.StartOAuthBinding(c.Request.Context(), currentAuthentication(c), etag, *in)
	}
	if err == nil {
		oauthSetCookie(c, result)
	}
	return result, err
}
func (ctrl *Ctrl) UnlinkOAuth(c *fox.Context) (*service.OAuthAccountView, error) {
	if err := oauthNoQuery(c); err != nil {
		return nil, err
	}
	etag, err := oauthReviewETag(c)
	if err != nil {
		return nil, err
	}
	in, err := decodeMFARequest[service.OAuthIdentityInput](c)
	if err != nil {
		return nil, err
	}
	result, err := ctrl.service.UnlinkOAuth(c.Request.Context(), currentAuthentication(c), etag, *in)
	if err == nil {
		c.Header("ETag", strconv.Quote(result.ReviewETag))
		if currentAuthentication(c).Session.PrimaryMethod == "oauth" {
			http.SetCookie(c.Writer, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", HttpOnly: true, Secure: c.Request.TLS != nil, SameSite: http.SameSiteStrictMode, MaxAge: -1})
		}
	}
	return result, err
}

// Keep strict review parsing local to this 64-hex contract; Connection reviews
// have two independently bound digests and cannot be reused for identity writes.
