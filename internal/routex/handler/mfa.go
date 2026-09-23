package handler

import (
	"net/http"

	"github.com/fox-gonic/fox"

	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
)

type MFAPasswordRequest struct {
	CurrentPassword string `json:"current_password"`
}
type MFAEnableRequest struct {
	CurrentPassword string `json:"current_password"`
	EnrollmentToken string `json:"enrollment_token"`
	Code            string `json:"code"`
}
type MFAChangeRequest struct {
	CurrentPassword string `json:"current_password"`
	Code            string `json:"code"`
	RecoveryCode    string `json:"recovery_code"`
}
type MFALoginRequest struct {
	ChallengeToken string `json:"challenge_token"`
	Code           string `json:"code"`
	RecoveryCode   string `json:"recovery_code"`
}
type MFARecoveryResponse struct {
	Session       *SessionResponse `json:"session"`
	RecoveryCodes []string         `json:"recovery_codes"`
}

func (ctrl *Ctrl) AccountMFA(c *fox.Context) (*service.MFAStatus, error) {
	return ctrl.service.AccountMFA(c.Request.Context(), currentAuthentication(c).User.ID)
}
func (ctrl *Ctrl) BeginMFAEnrollment(c *fox.Context) (*service.MFAEnrollment, error) {
	request, err := decodeMFARequest[MFAPasswordRequest](c)
	if err != nil {
		return nil, err
	}
	return ctrl.service.BeginMFAEnrollment(c.Request.Context(), currentAuthentication(c), request.CurrentPassword)
}
func (ctrl *Ctrl) CancelMFAEnrollment(c *fox.Context) error {
	if err := ctrl.service.CancelMFAEnrollment(c.Request.Context(), currentAuthentication(c)); err != nil {
		return err
	}
	c.Status(http.StatusNoContent)
	return nil
}
func (ctrl *Ctrl) EnableMFA(c *fox.Context) (*MFARecoveryResponse, error) {
	request, err := decodeMFARequest[MFAEnableRequest](c)
	if err != nil {
		return nil, err
	}
	result, err := ctrl.service.EnableMFA(c.Request.Context(), currentAuthentication(c), request.CurrentPassword, request.EnrollmentToken, request.Code)
	if err != nil {
		return nil, err
	}
	setSessionCookie(c, result.Authentication)
	return &MFARecoveryResponse{Session: sessionResponse(result.Authentication), RecoveryCodes: result.RecoveryCodes}, nil
}
func (ctrl *Ctrl) RegenerateMFARecoveryCodes(c *fox.Context) (*MFARecoveryResponse, error) {
	request, err := decodeMFARequest[MFAChangeRequest](c)
	if err != nil {
		return nil, err
	}
	result, err := ctrl.service.ChangeMFA(c.Request.Context(), currentAuthentication(c), request.CurrentPassword, service.MFAProof{Code: request.Code, RecoveryCode: request.RecoveryCode}, false)
	if err != nil {
		return nil, err
	}
	setSessionCookie(c, result.Authentication)
	return &MFARecoveryResponse{Session: sessionResponse(result.Authentication), RecoveryCodes: result.RecoveryCodes}, nil
}
func (ctrl *Ctrl) DisableMFA(c *fox.Context) (*SessionResponse, error) {
	request, err := decodeMFARequest[MFAChangeRequest](c)
	if err != nil {
		return nil, err
	}
	result, err := ctrl.service.ChangeMFA(c.Request.Context(), currentAuthentication(c), request.CurrentPassword, service.MFAProof{Code: request.Code, RecoveryCode: request.RecoveryCode}, true)
	if err != nil {
		return nil, err
	}
	setSessionCookie(c, result.Authentication)
	return sessionResponse(result.Authentication), nil
}
func (ctrl *Ctrl) CompleteMFALogin(c *fox.Context) (*SessionResponse, error) {
	request, err := decodeMFARequest[MFALoginRequest](c)
	if err != nil {
		return nil, err
	}
	auth, err := ctrl.service.CompleteMFALogin(c.Request.Context(), request.ChallengeToken, service.MFAProof{Code: request.Code, RecoveryCode: request.RecoveryCode})
	if err != nil {
		return nil, err
	}
	setSessionCookie(c, auth)
	return sessionResponse(auth), nil
}

// Sensitive MFA bodies must be one non-null object with only known fields.
// Decode before accessing authentication or invoking any proof transition.
func decodeMFARequest[T any](c *fox.Context) (*T, error) {
	var input *T
	if err := decodeStrictRequest(c, &input); err != nil {
		return nil, err
	}
	if input == nil {
		return nil, apperrors.ErrBadRequest
	}
	return input, nil
}
