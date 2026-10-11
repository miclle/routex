package service

import (
	"encoding/json"
	apperrors "github.com/miclle/routex/internal/routex/errors"
)

type EmergencyLoginInput struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (v *EmergencyLoginInput) UnmarshalJSON(raw []byte) error {
	f, e := registrationStrictObject(raw, []string{"email", "password"})
	if e != nil {
		return e
	}
	var n EmergencyLoginInput
	if json.Unmarshal(f["email"], &n.Email) != nil || json.Unmarshal(f["password"], &n.Password) != nil {
		return apperrors.ErrBadRequest
	}
	*v = n
	return nil
}

type EmergencyProofInput struct {
	ChallengeToken string `json:"challenge_token"`
	Code           string `json:"code"`
	RecoveryCode   string `json:"recovery_code"`
}

func (v *EmergencyProofInput) UnmarshalJSON(raw []byte) error {
	field := "code"
	f, e := registrationStrictObject(raw, []string{"challenge_token", field})
	if e != nil {
		field = "recovery_code"
		f, e = registrationStrictObject(raw, []string{"challenge_token", field})
	}
	if e != nil {
		return e
	}
	var n EmergencyProofInput
	var factor string
	if json.Unmarshal(f["challenge_token"], &n.ChallengeToken) != nil || json.Unmarshal(f[field], &factor) != nil || !oidcBrowserValue(n.ChallengeToken) || factor == "" || len(factor) > 128 {
		return apperrors.ErrBadRequest
	}
	if field == "code" {
		n.Code = factor
	} else {
		n.RecoveryCode = factor
	}
	*v = n
	return nil
}
