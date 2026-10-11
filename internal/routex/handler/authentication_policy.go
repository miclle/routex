package handler

import (
	"github.com/fox-gonic/fox"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
	"net/http"
	"strconv"
)

func (ctrl *Ctrl) PublicAuthenticationPolicy(c *fox.Context) (*service.AuthenticationPolicyPublic, error) {
	if _, ok := authenticationPolicyRequest(c.Request); !ok {
		return nil, apperrors.ErrBadRequest
	}
	return ctrl.service.PublicAuthenticationPolicy(c.Request.Context())
}
func (ctrl *Ctrl) GetAuthenticationPolicy(c *fox.Context) (*service.AuthenticationPolicyView, error) {
	in, ok := authenticationPolicyRequest(c.Request)
	if !ok {
		return nil, apperrors.ErrBadRequest
	}
	out, e := ctrl.service.GetAuthenticationPolicy(c.Request.Context(), currentAuthentication(c), in.candidate)
	if e == nil {
		c.Header("ETag", strconv.Quote(out.ReviewETag))
	}
	return out, e
}
func (ctrl *Ctrl) SetAuthenticationPolicy(c *fox.Context) (*service.AuthenticationPolicyResult, error) {
	if _, ok := authenticationPolicyRequest(c.Request); !ok {
		return nil, apperrors.ErrBadRequest
	}
	tag, e := memberMetadataETag(c)
	if e != nil {
		return nil, e
	}
	var in service.AuthenticationPolicyInput
	if e = registrationApprovalBody(c, &in); e != nil {
		return nil, e
	}
	out, e := ctrl.service.SetAuthenticationPolicy(c.Request.Context(), currentAuthentication(c), tag, in)
	if e == nil {
		c.Header("ETag", strconv.Quote(out.ReviewETag))
		if out.ReauthenticationRequired {
			expireAuthenticationCookie(c)
		}
	}
	return out, e
}
func (ctrl *Ctrl) BeginEmergencyLogin(c *fox.Context) error {
	if _, ok := authenticationPolicyRequest(c.Request); !ok {
		return apperrors.ErrBadRequest
	}
	in, e := decodeMFARequest[service.EmergencyLoginInput](c)
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
	challenge, e := ctrl.service.BeginEmergencyLogin(c.Request.Context(), in.Email, in.Password)
	if e != nil {
		return e
	}
	c.JSON(http.StatusAccepted, challenge)
	return nil
}
func (ctrl *Ctrl) CompleteEmergencyLogin(c *fox.Context) (*SessionResponse, error) {
	if _, ok := authenticationPolicyRequest(c.Request); !ok {
		return nil, apperrors.ErrBadRequest
	}
	in, e := decodeMFARequest[service.EmergencyProofInput](c)
	if e != nil {
		return nil, e
	}
	a, e := ctrl.optionalOAuthSession(c)
	if e != nil {
		return nil, e
	}
	if a != nil {
		return nil, apperrors.ErrForbidden
	}
	auth, e := ctrl.service.CompleteEmergencyLogin(c.Request.Context(), in.ChallengeToken, service.MFAProof{Code: in.Code, RecoveryCode: in.RecoveryCode})
	if e != nil {
		return nil, e
	}
	setSessionCookie(c, auth)
	return sessionResponse(auth), nil
}
func expireAuthenticationCookie(c *fox.Context) {
	http.SetCookie(c.Writer, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", HttpOnly: true, Secure: c.Request.TLS != nil, SameSite: http.SameSiteStrictMode, MaxAge: -1})
}

type ReauthenticationResponse struct {
	ReauthenticationRequired bool `json:"reauthentication_required"`
}
