package handler

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/fox-gonic/fox"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
)

func samlPrivate(c *fox.Context) {
	c.Header("Cache-Control", "private, no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Referrer-Policy", "no-referrer")
	c.Next()
}
func samlSetCookie(c *fox.Context, name, value string, expires time.Time) bool {
	remaining := time.Until(expires)
	if remaining <= 0 {
		return false
	}
	http.SetCookie(c.Writer, &http.Cookie{Name: name, Value: value, Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode, Expires: expires, MaxAge: int(remaining / time.Second)})
	return true
}
func samlClearCookie(c *fox.Context, name string) {
	http.SetCookie(c.Writer, &http.Cookie{Name: name, Value: "", Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: -1})
}
func samlClearCookies(c *fox.Context) {
	samlClearCookie(c, samlStartCookie)
	samlClearCookie(c, samlDeliveryCookie)
}
func (ctrl *Ctrl) PublicSAML(c *fox.Context) (*service.SAMLPublic, error) {
	if captureSAMLInputs(c.Request).invalid {
		return nil, apperrors.ErrBadRequest
	}
	return ctrl.service.PublicSAML(c.Request.Context())
}
func (ctrl *Ctrl) StartSAMLLogin(c *fox.Context) (*service.SAMLStart, error) {
	if captureSAMLInputs(c.Request).invalid {
		return nil, apperrors.ErrBadRequest
	}
	if _, err := decodeMFARequest[struct{}](c); err != nil {
		return nil, err
	}
	a, err := ctrl.optionalOAuthSession(c)
	if err != nil {
		return nil, err
	}
	if a != nil {
		return nil, apperrors.ErrForbidden
	}
	out, err := ctrl.service.StartSAMLLogin(c.Request.Context())
	if err != nil {
		return nil, err
	}
	if !samlSetCookie(c, samlStartCookie, out.Cookie, out.ExpiresAt) {
		return nil, apperrors.ErrUnauthorized
	}
	samlClearCookie(c, samlDeliveryCookie)
	return out, nil
}
func (ctrl *Ctrl) SAMLACS(c *fox.Context) {
	in := captureSAMLInputs(c.Request)
	if !in.invalid {
		out, err := ctrl.service.ReceiveSAMLAssertion(c.Request.Context(), in.relay, in.response)
		if err == nil && out != nil {
			samlSetCookie(c, samlDeliveryCookie, out.Cookie, out.ExpiresAt)
		}
	}
	// Staging has no Session or linking authority, including on malformed input.
	c.Redirect(http.StatusSeeOther, "/auth/saml/complete")
}
func (ctrl *Ctrl) CompleteSAML(c *fox.Context) error {
	in := captureSAMLInputs(c.Request)
	if in.invalid || in.start == "" || in.delivery == "" {
		return apperrors.ErrUnauthorized
	}
	if _, err := decodeMFARequest[struct{}](c); err != nil {
		return err
	}
	a, err := ctrl.optionalOAuthSession(c)
	if err != nil {
		return err
	}
	if a != nil && !a.CheckCSRF(c.Request.Header.Get("X-CSRF-Token")) {
		return apperrors.ErrForbidden
	}
	out, err := ctrl.service.CompleteSAML(c.Request.Context(), a, in.start, in.delivery)
	samlClearCookies(c)
	if err != nil {
		return err
	}
	switch out.Kind {
	case "session":
		if out.Authentication == nil {
			return apperrors.ErrInternal
		}
		setSessionCookie(c, out.Authentication)
		c.JSON(http.StatusOK, sessionResponse(out.Authentication))
	case "challenge":
		if out.Challenge == nil {
			return apperrors.ErrInternal
		}
		c.JSON(http.StatusAccepted, out.Challenge)
	case "bound", "verified":
		c.JSON(http.StatusOK, struct {
			Kind string `json:"kind"`
		}{out.Kind})
	default:
		return apperrors.ErrInternal
	}
	return nil
}
func (ctrl *Ctrl) GetSAML(c *fox.Context) (*service.SAMLProviderView, error) {
	if captureSAMLInputs(c.Request).invalid {
		return nil, apperrors.ErrBadRequest
	}
	out, err := ctrl.service.GetSAML(c.Request.Context(), currentAuthentication(c).User.ID)
	if err == nil {
		c.Header("ETag", strconv.Quote(out.ReviewETag))
	}
	return out, err
}
func (ctrl *Ctrl) SaveSAML(c *fox.Context) (*service.SAMLProviderView, error) {
	if captureSAMLInputs(c.Request).invalid {
		return nil, apperrors.ErrBadRequest
	}
	tag, err := oauthReviewETag(c)
	if err != nil {
		return nil, err
	}
	in, err := decodeMFARequest[service.SAMLProviderInput](c)
	if err != nil {
		return nil, err
	}
	out, err := ctrl.service.SaveSAML(c.Request.Context(), currentAuthentication(c).User.ID, tag, *in)
	if err == nil {
		c.Header("ETag", strconv.Quote(out.ReviewETag))
		ctrl.samlClearRevokedCookie(c)
	}
	return out, err
}
func (ctrl *Ctrl) SetSAMLStatus(c *fox.Context) (*service.SAMLProviderView, error) {
	if captureSAMLInputs(c.Request).invalid {
		return nil, apperrors.ErrBadRequest
	}
	tag, err := oauthReviewETag(c)
	if err != nil {
		return nil, err
	}
	in, err := decodeMFARequest[service.SAMLStatusInput](c)
	if err != nil {
		return nil, err
	}
	out, err := ctrl.service.SetSAMLStatus(c.Request.Context(), currentAuthentication(c).User.ID, tag, *in)
	if err == nil {
		c.Header("ETag", strconv.Quote(out.ReviewETag))
		ctrl.samlClearRevokedCookie(c)
	}
	return out, err
}
func (ctrl *Ctrl) AccountSAML(c *fox.Context) (*service.SAMLAccountView, error) {
	if captureSAMLInputs(c.Request).invalid {
		return nil, apperrors.ErrBadRequest
	}
	out, err := ctrl.service.AccountSAML(c.Request.Context(), currentAuthentication(c))
	if err == nil {
		c.Header("ETag", strconv.Quote(out.ReviewETag))
	}
	return out, err
}
func (ctrl *Ctrl) StartSAMLBinding(c *fox.Context) (*service.SAMLStart, error) {
	return ctrl.startSAMLIdentity(c, false)
}
func (ctrl *Ctrl) StartSAMLVerification(c *fox.Context) (*service.SAMLStart, error) {
	return ctrl.startSAMLIdentity(c, true)
}
func (ctrl *Ctrl) startSAMLIdentity(c *fox.Context, verify bool) (*service.SAMLStart, error) {
	if captureSAMLInputs(c.Request).invalid {
		return nil, apperrors.ErrBadRequest
	}
	tag, err := oauthReviewETag(c)
	if err != nil {
		return nil, err
	}
	in, err := decodeMFARequest[service.SAMLIdentityInput](c)
	if err != nil {
		return nil, err
	}
	var out *service.SAMLStart
	if verify {
		out, err = ctrl.service.StartSAMLVerification(c.Request.Context(), currentAuthentication(c), tag, *in)
	} else {
		out, err = ctrl.service.StartSAMLBinding(c.Request.Context(), currentAuthentication(c), tag, *in)
	}
	if err != nil {
		return nil, err
	}
	if !samlSetCookie(c, samlStartCookie, out.Cookie, out.ExpiresAt) {
		return nil, apperrors.ErrUnauthorized
	}
	samlClearCookie(c, samlDeliveryCookie)
	return out, nil
}
func (ctrl *Ctrl) UnlinkSAML(c *fox.Context) (*service.SAMLAccountView, error) {
	if captureSAMLInputs(c.Request).invalid {
		return nil, apperrors.ErrBadRequest
	}
	tag, err := oauthReviewETag(c)
	if err != nil {
		return nil, err
	}
	in, err := decodeMFARequest[service.SAMLIdentityInput](c)
	if err != nil {
		return nil, err
	}
	out, err := ctrl.service.UnlinkSAML(c.Request.Context(), currentAuthentication(c), tag, *in)
	if err == nil {
		c.Header("ETag", strconv.Quote(out.ReviewETag))
		ctrl.samlClearRevokedCookie(c)
	}
	return out, err
}
func (ctrl *Ctrl) samlClearRevokedCookie(c *fox.Context) {
	a := currentAuthentication(c)
	if a.Session.PrimaryMethod != "saml" {
		return
	}
	if _, err := ctrl.service.Authenticate(c.Request.Context(), a.Token); errors.Is(err, apperrors.ErrUnauthorized) {
		http.SetCookie(c.Writer, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", HttpOnly: true, Secure: c.Request.TLS != nil, SameSite: http.SameSiteStrictMode, MaxAge: -1})
	}
}

// AbandonSAML removes only this browser's transient correlation proofs. Retained
// ceremonies expire normally; no binding, Session or historical receipt changes.
func (ctrl *Ctrl) AbandonSAML(c *fox.Context) error {
	if _, err := decodeMFARequest[struct{}](c); err != nil {
		return err
	}
	a, err := ctrl.optionalOAuthSession(c)
	if err != nil {
		return err
	}
	if a != nil && !a.CheckCSRF(c.Request.Header.Get("X-CSRF-Token")) {
		return apperrors.ErrForbidden
	}
	samlClearCookies(c)
	c.Status(http.StatusNoContent)
	return nil
}
