package service

import (
	"context"
	"errors"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/secret"
)

const mfaChallengeLifetime = 5 * time.Minute
const mfaAttemptLimit = 5

var errMFARequired = &apperrors.Error{Code: 401, Message: "two-step verification required"}
var errMFAUnavailable = &apperrors.Error{Code: 503, Message: "two-step verification temporarily unavailable"}

type MFAStatus struct {
	Enabled                bool  `json:"enabled"`
	EnrollmentAvailable    bool  `json:"enrollment_available"`
	EnrollmentPending      bool  `json:"enrollment_pending"`
	RecoveryCodesRemaining int64 `json:"recovery_codes_remaining"`
}
type MFAEnrollment struct {
	Secret          string    `json:"secret"`
	ProvisioningURI string    `json:"otpauth_uri"`
	EnrollmentToken string    `json:"enrollment_token"`
	ExpiresAt       time.Time `json:"expires_at"`
}
type MFALoginChallenge struct {
	MFARequired    bool      `json:"mfa_required"`
	ChallengeToken string    `json:"challenge_token"`
	ExpiresAt      time.Time `json:"expires_at"`
	Methods        []string  `json:"methods"`
}
type MFAMutation struct {
	Authentication *Authentication
	RecoveryCodes  []string
}
type MFAProof struct{ Code, RecoveryCode string }

// Call while holding the user's row lock. Password and account lifecycle changes
// delete challenges in their own transaction, so reactivation cannot revive one.
func invalidateMFAChallenges(tx *gorm.DB, userID string) error {
	return tx.Where("user_id = ?", userID).Delete(&entity.MFAChallenge{}).Error
}
func mfaState(tx *gorm.DB, userID string) (entity.UserMFA, error) {
	result := entity.UserMFA{UserID: userID, LastTOTPStep: -1}
	err := tx.First(&result, "user_id = ?", userID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return entity.UserMFA{UserID: userID, LastTOTPStep: -1}, nil
	}
	return result, err
}
func mfaLocked(state *entity.UserMFA, now time.Time) bool {
	if state.LockedUntil == nil {
		return false
	}
	if state.LockedUntil.After(now) {
		return true
	}
	state.LockedUntil = nil
	state.FailedAttempts = 0
	return false
}
func mfaFailure(tx *gorm.DB, state *entity.UserMFA, now time.Time) error {
	state.FailedAttempts++
	if state.FailedAttempts >= mfaAttemptLimit {
		until := now.Add(15 * time.Minute)
		state.LockedUntil = &until
	}
	return tx.Save(state).Error
}
func mfaActiveUser(tx *gorm.DB, userID string) (entity.User, error) {
	var user entity.User
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", userID).First(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) || user.Disabled {
		return user, apperrors.ErrUnauthorized
	}
	return user, err
}
func mfaSession(tx *gorm.DB, auth *Authentication, now time.Time) error {
	if auth == nil {
		return apperrors.ErrUnauthorized
	}
	var session entity.Session
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND user_id = ? AND token_hash = ? AND expires_at > ?", auth.Session.ID, auth.User.ID, secret.SHA256Hex(auth.Token), now).First(&session).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return apperrors.ErrUnauthorized
	}
	return err
}
func (s *Service) mfaPassword(ctx context.Context, userID, password string) (string, error) {
	if !validPassword(password) {
		return "", apperrors.ErrUnauthorized
	}
	var user entity.User
	if err := s.authDB(ctx).Where("id = ? AND disabled = ?", userID, false).First(&user).Error; err != nil {
		return "", keyServiceError(err)
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) != nil {
		return "", apperrors.ErrUnauthorized
	}
	return user.PasswordHash, nil
}
func (s *Service) AccountMFA(ctx context.Context, userID string) (*MFAStatus, error) {
	result := &MFAStatus{EnrollmentAvailable: s.secrets != nil}
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := mfaActiveUser(tx, userID); err != nil {
			return err
		}
		state, err := mfaState(tx, userID)
		if err != nil {
			return err
		}
		result.Enabled = state.Enabled
		var count int64
		if err := tx.Model(&entity.MFAChallenge{}).Where("user_id = ? AND purpose = ? AND expires_at > ?", userID, "enrollment", time.Now().UTC()).Count(&count).Error; err != nil {
			return err
		}
		result.EnrollmentPending = count != 0
		return tx.Model(&entity.MFARecoveryCode{}).Where("user_id = ? AND used_at IS NULL", userID).Count(&result.RecoveryCodesRemaining).Error
	})
	return result, keyServiceError(err)
}
func (s *Service) BeginMFAEnrollment(ctx context.Context, auth *Authentication, password string) (*MFAEnrollment, error) {
	if auth == nil {
		return nil, apperrors.ErrUnauthorized
	}
	hash, err := s.mfaPassword(ctx, auth.User.ID, password)
	if err != nil {
		return nil, err
	}
	if s.secrets == nil {
		return nil, errMFAUnavailable
	}
	result := &MFAEnrollment{}
	err = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		user, err := mfaActiveUser(tx, auth.User.ID)
		if err != nil {
			return err
		}
		if user.PasswordHash != hash {
			return apperrors.ErrUnauthorized
		}
		now := time.Now().UTC()
		if err := mfaSession(tx, auth, now); err != nil {
			return err
		}
		state, err := mfaState(tx, user.ID)
		if err != nil {
			return err
		}
		if state.Enabled {
			return catalogConflict
		}
		if mfaLocked(&state, now) {
			return apperrors.ErrUnauthorized
		}
		key, err := newMFASecret()
		if err != nil {
			return err
		}
		generation, err := secret.RandomURLSafe(32)
		if err != nil {
			return err
		}
		token, err := secret.RandomURLSafe(32)
		if err != nil {
			return err
		}
		ciphertext, err := s.secrets.Seal("mfa:"+user.ID+":"+generation, key)
		if err != nil {
			return errMFAUnavailable
		}
		state.Generation = generation
		state.SecretCiphertext = ciphertext
		state.LastTOTPStep = -1
		if err := tx.Save(&state).Error; err != nil {
			return err
		}
		if err := invalidateMFAChallenges(tx, user.ID); err != nil {
			return err
		}
		challenge := entity.MFAChallenge{UserID: user.ID, Purpose: "enrollment", TokenHash: secret.SHA256Hex(token), PasswordDigest: secret.SHA256Hex(hash), Generation: generation, SessionID: auth.Session.ID, ExpiresAt: now.Add(mfaChallengeLifetime)}
		if err := tx.Create(&challenge).Error; err != nil {
			return err
		}
		*result = MFAEnrollment{Secret: key, ProvisioningURI: mfaProvisioningURI(user.Email, key), EnrollmentToken: token, ExpiresAt: challenge.ExpiresAt}
		return nil
	})
	return result, keyServiceError(err)
}
func (s *Service) CancelMFAEnrollment(ctx context.Context, auth *Authentication) error {
	if auth == nil {
		return apperrors.ErrUnauthorized
	}
	return keyServiceError(s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := mfaActiveUser(tx, auth.User.ID); err != nil {
			return err
		}
		if err := mfaSession(tx, auth, time.Now().UTC()); err != nil {
			return err
		}
		state, err := mfaState(tx, auth.User.ID)
		if err != nil {
			return err
		}
		if state.Enabled {
			return catalogConflict
		}
		state.SecretCiphertext = ""
		state.Generation = ""
		if err := tx.Save(&state).Error; err != nil {
			return err
		}
		return invalidateMFAChallenges(tx, auth.User.ID)
	}))
}
