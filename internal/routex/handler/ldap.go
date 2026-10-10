package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/fox-gonic/fox"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
)

func ldapPrivate(c *fox.Context) {
	c.Header("Cache-Control", "private, no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Next()
}
func (ctrl *Ctrl) PublicLDAP(c *fox.Context) (*service.LDAPPublic, error) {
	if e := oauthNoQuery(c); e != nil {
		return nil, e
	}
	return ctrl.service.PublicLDAP(c.Request.Context())
}
func (ctrl *Ctrl) LoginLDAP(c *fox.Context) error {
	if e := oauthNoQuery(c); e != nil {
		return e
	}
	in, e := decodeMFARequest[service.LDAPLoginInput](c)
	if e != nil {
		return e
	}
	a, e := ctrl.optionalOAuthSession(c)
	if e != nil {
		return e
	}
	if a != nil {
		return apperrors.ErrForbidden
	}
	out, e := ctrl.service.LoginLDAP(c.Request.Context(), *in)
	if e != nil {
		return e
	}
	if out.Challenge != nil {
		c.JSON(http.StatusAccepted, out.Challenge)
		return nil
	}
	if out.Authentication == nil {
		return apperrors.ErrInternal
	}
	setSessionCookie(c, out.Authentication)
	c.JSON(http.StatusOK, sessionResponse(out.Authentication))
	return nil
}
func (ctrl *Ctrl) GetLDAPProvider(c *fox.Context) (*service.LDAPProviderView, error) {
	if e := oauthNoQuery(c); e != nil {
		return nil, e
	}
	v, e := ctrl.service.GetLDAPProvider(c.Request.Context(), currentAuthentication(c).User.ID)
	if e == nil {
		c.Header("ETag", strconv.Quote(v.ReviewETag))
	}
	return v, e
}
func (ctrl *Ctrl) SaveLDAPProvider(c *fox.Context) (*service.LDAPProviderView, error) {
	if e := oauthNoQuery(c); e != nil {
		return nil, e
	}
	tag, e := oauthReviewETag(c)
	if e != nil {
		return nil, e
	}
	in, e := decodeMFARequest[service.LDAPProviderInput](c)
	if e != nil {
		return nil, e
	}
	v, e := ctrl.service.SaveLDAPProvider(c.Request.Context(), currentAuthentication(c).User.ID, tag, *in)
	if e == nil {
		c.Header("ETag", strconv.Quote(v.ReviewETag))
		ctrl.ldapClearRevokedCookie(c)
	}
	return v, e
}
func (ctrl *Ctrl) SetLDAPEnabled(c *fox.Context) (*service.LDAPProviderView, error) {
	if e := oauthNoQuery(c); e != nil {
		return nil, e
	}
	tag, e := oauthReviewETag(c)
	if e != nil {
		return nil, e
	}
	in, e := decodeMFARequest[service.LDAPStatusInput](c)
	if e != nil {
		return nil, e
	}
	v, e := ctrl.service.SetLDAPEnabled(c.Request.Context(), currentAuthentication(c).User.ID, tag, *in)
	if e == nil {
		c.Header("ETag", strconv.Quote(v.ReviewETag))
		ctrl.ldapClearRevokedCookie(c)
	}
	return v, e
}
func (ctrl *Ctrl) AccountLDAP(c *fox.Context) (*service.LDAPAccountView, error) {
	if e := oauthNoQuery(c); e != nil {
		return nil, e
	}
	v, e := ctrl.service.AccountLDAP(c.Request.Context(), currentAuthentication(c))
	if e == nil {
		c.Header("ETag", strconv.Quote(v.ReviewETag))
	}
	return v, e
}
func (ctrl *Ctrl) VerifyLDAP(c *fox.Context) (*service.LDAPResult, error) {
	return ctrl.ldapBind(c, true)
}
func (ctrl *Ctrl) BindLDAP(c *fox.Context) (*service.LDAPResult, error) {
	return ctrl.ldapBind(c, false)
}
func (ctrl *Ctrl) ldapBind(c *fox.Context, verify bool) (*service.LDAPResult, error) {
	if e := oauthNoQuery(c); e != nil {
		return nil, e
	}
	tag, e := oauthReviewETag(c)
	if e != nil {
		return nil, e
	}
	in, e := decodeMFARequest[service.LDAPBindingInput](c)
	if e != nil {
		return nil, e
	}
	if verify {
		return ctrl.service.VerifyLDAP(c.Request.Context(), currentAuthentication(c), tag, *in)
	}
	return ctrl.service.BindLDAP(c.Request.Context(), currentAuthentication(c), tag, *in)
}
func (ctrl *Ctrl) UnlinkLDAP(c *fox.Context) (*service.LDAPAccountView, error) {
	if e := oauthNoQuery(c); e != nil {
		return nil, e
	}
	tag, e := oauthReviewETag(c)
	if e != nil {
		return nil, e
	}
	in, e := decodeMFARequest[service.LDAPIdentityInput](c)
	if e != nil {
		return nil, e
	}
	v, e := ctrl.service.UnlinkLDAP(c.Request.Context(), currentAuthentication(c), tag, *in)
	if e == nil {
		c.Header("ETag", strconv.Quote(v.ReviewETag))
		ctrl.ldapClearRevokedCookie(c)
	}
	return v, e
}
func (ctrl *Ctrl) ldapClearRevokedCookie(c *fox.Context) {
	a := currentAuthentication(c)
	if a.Session.PrimaryMethod != "ldap" {
		return
	}
	// Name-only/no-op writes retain a live primary Session. Clear only after an
	// authoritative read proves it was revoked by this successful operation.
	_, e := ctrl.service.Authenticate(c.Request.Context(), a.Token)
	if errors.Is(e, apperrors.ErrUnauthorized) {
		http.SetCookie(c.Writer, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", HttpOnly: true, Secure: c.Request.TLS != nil, SameSite: http.SameSiteStrictMode, MaxAge: -1})
	}
}
