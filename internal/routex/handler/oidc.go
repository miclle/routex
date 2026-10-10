package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/fox-gonic/fox"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
)

func oidcReviewETag(c *fox.Context) (string, error) {
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
func oidcNoQuery(c *fox.Context) error {
	if c.Request.URL.RawQuery != "" || c.Request.URL.ForceQuery || c.Request.ContentLength > 64<<10 {
		return apperrors.ErrBadRequest
	}
	return nil
}
func (ctrl *Ctrl) PublicOIDC(c *fox.Context) (*service.OIDCPublic, error) {
	if captureOIDCInputs(c.Request).invalid {
		return nil, apperrors.ErrBadRequest
	}
	return ctrl.service.PublicOIDC(c.Request.Context())
}
func oidcSetCookie(c *fox.Context, start *service.OIDCStart) {
	http.SetCookie(c.Writer, &http.Cookie{Name: oidcCookie, Value: start.Cookie, Path: oidcCookiePath, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: 300, Expires: start.ExpiresAt})
}
func oidcClearCookie(c *fox.Context) {
	http.SetCookie(c.Writer, &http.Cookie{Name: oidcCookie, Value: "", Path: oidcCookiePath, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: -1})
}
func (ctrl *Ctrl) optionalOIDCSession(c *fox.Context) (*service.Authentication, error) {
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
func (ctrl *Ctrl) StartOIDCLogin(c *fox.Context) (*service.OIDCStart, error) {
	if captureOIDCInputs(c.Request).invalid {
		return nil, apperrors.ErrBadRequest
	}
	if _, err := decodeMFARequest[struct{}](c); err != nil {
		return nil, err
	}
	auth, err := ctrl.optionalOIDCSession(c)
	if err != nil {
		return nil, err
	}
	if auth != nil {
		return nil, apperrors.ErrForbidden
	}
	result, err := ctrl.service.StartOIDCLogin(c.Request.Context())
	if err == nil {
		oidcSetCookie(c, result)
	}
	return result, err
}
func (ctrl *Ctrl) OIDCCallback(c *fox.Context) {
	in := captureOIDCInputs(c.Request)
	c.Header("Referrer-Policy", "no-referrer")
	if in.invalid || in.cookie == "" || ctrl.service.ReceiveOIDCCallback(c.Request.Context(), in.cookie, in.state, in.code, in.remoteError, in.issuer) != nil {
		oidcClearCookie(c)
	}
	c.Redirect(http.StatusSeeOther, "/auth/oidc/complete")
}
func (ctrl *Ctrl) CompleteOIDC(c *fox.Context) error {
	in := captureOIDCInputs(c.Request)
	if in.invalid || in.cookie == "" {
		return apperrors.ErrUnauthorized
	}
	if _, err := decodeMFARequest[struct{}](c); err != nil {
		return err
	}
	auth, err := ctrl.optionalOIDCSession(c)
	if err != nil {
		return err
	}
	if auth != nil && !auth.CheckCSRF(c.Request.Header.Get("X-CSRF-Token")) {
		return apperrors.ErrForbidden
	}
	result, err := ctrl.service.CompleteOIDC(c.Request.Context(), auth, in.cookie)
	oidcClearCookie(c)
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
func (ctrl *Ctrl) GetOIDCProvider(c *fox.Context) (*service.OIDCProviderView, error) {
	if err := oidcNoQuery(c); err != nil {
		return nil, err
	}
	result, err := ctrl.service.GetOIDCProvider(c.Request.Context(), currentAuthentication(c).User.ID)
	if err == nil {
		c.Header("ETag", strconv.Quote(result.ReviewETag))
	}
	return result, err
}
func (ctrl *Ctrl) SaveOIDCProvider(c *fox.Context) (*service.OIDCProviderView, error) {
	if err := oidcNoQuery(c); err != nil {
		return nil, err
	}
	etag, err := oidcReviewETag(c)
	if err != nil {
		return nil, err
	}
	in, err := decodeMFARequest[service.OIDCProviderInput](c)
	if err != nil {
		return nil, err
	}
	result, err := ctrl.service.SaveOIDCProvider(c.Request.Context(), currentAuthentication(c).User.ID, etag, *in)
	if err == nil {
		c.Header("ETag", strconv.Quote(result.ReviewETag))
	}
	return result, err
}
func (ctrl *Ctrl) SetOIDCEnabled(c *fox.Context) (*service.OIDCProviderView, error) {
	if err := oidcNoQuery(c); err != nil {
		return nil, err
	}
	etag, err := oidcReviewETag(c)
	if err != nil {
		return nil, err
	}
	in, err := decodeMFARequest[service.OIDCStatusInput](c)
	if err != nil {
		return nil, err
	}
	result, err := ctrl.service.SetOIDCEnabled(c.Request.Context(), currentAuthentication(c).User.ID, etag, *in)
	if err == nil {
		c.Header("ETag", strconv.Quote(result.ReviewETag))
	}
	return result, err
}
func (ctrl *Ctrl) AccountOIDC(c *fox.Context) (*service.OIDCAccountView, error) {
	if err := oidcNoQuery(c); err != nil {
		return nil, err
	}
	result, err := ctrl.service.AccountOIDC(c.Request.Context(), currentAuthentication(c))
	if err == nil {
		c.Header("ETag", strconv.Quote(result.ReviewETag))
	}
	return result, err
}
func (ctrl *Ctrl) StartOIDCBinding(c *fox.Context) (*service.OIDCStart, error) {
	return ctrl.startOIDCIdentity(c, false)
}
func (ctrl *Ctrl) StartOIDCVerification(c *fox.Context) (*service.OIDCStart, error) {
	return ctrl.startOIDCIdentity(c, true)
}
func (ctrl *Ctrl) startOIDCIdentity(c *fox.Context, verify bool) (*service.OIDCStart, error) {
	if err := oidcNoQuery(c); err != nil {
		return nil, err
	}
	etag, err := oidcReviewETag(c)
	if err != nil {
		return nil, err
	}
	in, err := decodeMFARequest[service.OIDCIdentityInput](c)
	if err != nil {
		return nil, err
	}
	var result *service.OIDCStart
	if verify {
		result, err = ctrl.service.StartOIDCVerification(c.Request.Context(), currentAuthentication(c), etag, *in)
	} else {
		result, err = ctrl.service.StartOIDCBinding(c.Request.Context(), currentAuthentication(c), etag, *in)
	}
	if err == nil {
		oidcSetCookie(c, result)
	}
	return result, err
}
func (ctrl *Ctrl) UnlinkOIDC(c *fox.Context) (*service.OIDCAccountView, error) {
	if err := oidcNoQuery(c); err != nil {
		return nil, err
	}
	etag, err := oidcReviewETag(c)
	if err != nil {
		return nil, err
	}
	in, err := decodeMFARequest[service.OIDCIdentityInput](c)
	if err != nil {
		return nil, err
	}
	result, err := ctrl.service.UnlinkOIDC(c.Request.Context(), currentAuthentication(c), etag, *in)
	if err == nil {
		c.Header("ETag", strconv.Quote(result.ReviewETag))
		if currentAuthentication(c).Session.PrimaryMethod == "oidc" {
			http.SetCookie(c.Writer, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", HttpOnly: true, Secure: c.Request.TLS != nil, SameSite: http.SameSiteStrictMode, MaxAge: -1})
		}
	}
	return result, err
}

// Keep strict review parsing local to this 64-hex contract; Connection reviews
// have two independently bound digests and cannot be reused for identity writes.
