package service

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/secret"
)

func (s *Service) consumeMFAProof(tx *gorm.DB, state *entity.UserMFA, proof MFAProof, now time.Time) (bool, error) {
	if (proof.Code == "") == (proof.RecoveryCode == "") {
		return false, nil
	}
	if proof.RecoveryCode != "" {
		digest, valid := mfaRecoveryDigest(state.UserID, state.Generation, proof.RecoveryCode)
		if !valid {
			return false, nil
		}
		result := tx.Model(&entity.MFARecoveryCode{}).Where("user_id = ? AND code_hash = ? AND used_at IS NULL", state.UserID, digest).Update("used_at", now)
		return result.RowsAffected == 1, result.Error
	}
	key, err := s.secrets.Open("mfa:"+state.UserID+":"+state.Generation, state.SecretCiphertext)
	if err != nil {
		return false, errMFAUnavailable
	}
	step, valid := mfaMatchTOTP(key, proof.Code, now, state.LastTOTPStep)
	if valid {
		state.LastTOTPStep = step
	}
	return valid, nil
}
func resetMFAFailures(state *entity.UserMFA) { state.FailedAttempts = 0; state.LockedUntil = nil }
func newMFARecoveryCodes(tx *gorm.DB, state entity.UserMFA) ([]string, error) {
	if err := tx.Where("user_id = ?", state.UserID).Delete(&entity.MFARecoveryCode{}).Error; err != nil {
		return nil, err
	}
	result := make([]string, 0, 10)
	for range 10 {
		code, err := newMFARecoveryCode()
		if err != nil {
			return nil, err
		}
		digest, _ := mfaRecoveryDigest(state.UserID, state.Generation, code)
		if err := tx.Create(&entity.MFARecoveryCode{UserID: state.UserID, CodeHash: digest}).Error; err != nil {
			return nil, err
		}
		result = append(result, code)
	}
	return result, nil
}
func rotateMFASession(tx *gorm.DB, user entity.User) (*Authentication, error) {
	if err := invalidateMFAChallenges(tx, user.ID); err != nil {
		return nil, err
	}
	if err := tx.Where("user_id = ?", user.ID).Delete(&entity.Session{}).Error; err != nil {
		return nil, err
	}
	return createSession(tx, user)
}
func mfaChallengeValid(challenge entity.MFAChallenge, user entity.User, state entity.UserMFA, now time.Time) bool {
	return challenge.ExpiresAt.After(now) && challenge.Attempts < mfaAttemptLimit && challenge.PasswordDigest == secret.SHA256Hex(user.PasswordHash) && challenge.Generation == state.Generation
}
func (s *Service) EnableMFA(ctx context.Context, auth *Authentication, password, token, code string) (*MFAMutation, error) {
	if auth == nil {
		return nil, apperrors.ErrUnauthorized
	}
	hash, err := s.mfaPassword(ctx, auth.User.ID, password)
	if err != nil {
		return nil, err
	}
	result := &MFAMutation{}
	rejected := false
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
		var challenge entity.MFAChallenge
		err = tx.Where("user_id = ? AND purpose = ? AND token_hash = ?", user.ID, "enrollment", secret.SHA256Hex(token)).First(&challenge).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return apperrors.ErrUnauthorized
		}
		if err != nil {
			return err
		}
		if len(token) != 43 || challenge.SessionID != auth.Session.ID || !mfaChallengeValid(challenge, user, state, now) {
			return apperrors.ErrUnauthorized
		}
		valid, err := s.consumeMFAProof(tx, &state, MFAProof{Code: code}, now)
		if err != nil {
			return err
		}
		if !valid {
			rejected = true
			challenge.Attempts++
			if err := tx.Save(&challenge).Error; err != nil {
				return err
			}
			return mfaFailure(tx, &state, now)
		}
		state.Enabled = true
		resetMFAFailures(&state)
		if err := tx.Save(&state).Error; err != nil {
			return err
		}
		result.RecoveryCodes, err = newMFARecoveryCodes(tx, state)
		if err != nil {
			return err
		}
		result.Authentication, err = rotateMFASession(tx, user)
		if err != nil {
			return err
		}
		return appendAudit(tx, user.ID, "account.mfa.enable", "user", user.ID)
	})
	if err != nil {
		return nil, keyServiceError(err)
	}
	if rejected {
		return nil, apperrors.ErrUnauthorized
	}
	return result, nil
}
func (s *Service) ChangeMFA(ctx context.Context, auth *Authentication, password string, proof MFAProof, disable bool) (*MFAMutation, error) {
	if auth == nil {
		return nil, apperrors.ErrUnauthorized
	}
	hash, err := s.mfaPassword(ctx, auth.User.ID, password)
	if err != nil {
		return nil, err
	}
	result := &MFAMutation{}
	rejected := false
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
		if !state.Enabled {
			return catalogConflict
		}
		if mfaLocked(&state, now) {
			return apperrors.ErrUnauthorized
		}
		valid, err := s.consumeMFAProof(tx, &state, proof, now)
		if err != nil {
			return err
		}
		if !valid {
			rejected = true
			return mfaFailure(tx, &state, now)
		}
		resetMFAFailures(&state)
		action := "account.mfa.recovery.regenerate"
		if disable {
			state.Enabled = false
			state.SecretCiphertext = ""
			state.Generation = ""
			state.LastTOTPStep = -1
			action = "account.mfa.disable"
			if err := tx.Where("user_id = ?", user.ID).Delete(&entity.MFARecoveryCode{}).Error; err != nil {
				return err
			}
		} else {
			result.RecoveryCodes, err = newMFARecoveryCodes(tx, state)
			if err != nil {
				return err
			}
		}
		if err := tx.Save(&state).Error; err != nil {
			return err
		}
		result.Authentication, err = rotateMFASession(tx, user)
		if err != nil {
			return err
		}
		return appendAudit(tx, user.ID, action, "user", user.ID)
	})
	if err != nil {
		return nil, keyServiceError(err)
	}
	if rejected {
		return nil, apperrors.ErrUnauthorized
	}
	return result, nil
}
func (s *Service) CompleteMFALogin(ctx context.Context, token string, proof MFAProof) (*Authentication, error) {
	if len(token) != 43 {
		return nil, apperrors.ErrUnauthorized
	}
	var before entity.MFAChallenge
	if err := s.authDB(ctx).Where("purpose = ? AND token_hash = ?", "login", secret.SHA256Hex(token)).First(&before).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, apperrors.ErrUnauthorized
		}
		return nil, keyServiceError(err)
	}
	var auth *Authentication
	rejected := false
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		user, err := mfaActiveUser(tx, before.UserID)
		if err != nil {
			return err
		}
		state, err := mfaState(tx, user.ID)
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		if !state.Enabled || mfaLocked(&state, now) {
			return apperrors.ErrUnauthorized
		}
		var challenge entity.MFAChallenge
		if err := tx.Where("user_id = ? AND purpose = ? AND token_hash = ?", user.ID, "login", secret.SHA256Hex(token)).First(&challenge).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return apperrors.ErrUnauthorized
			}
			return err
		}
		if !mfaChallengeValid(challenge, user, state, now) {
			return apperrors.ErrUnauthorized
		}
		valid, err := s.consumeMFAProof(tx, &state, proof, now)
		if err != nil {
			return err
		}
		if !valid {
			rejected = true
			challenge.Attempts++
			if err := tx.Save(&challenge).Error; err != nil {
				return err
			}
			return mfaFailure(tx, &state, now)
		}
		resetMFAFailures(&state)
		if err := tx.Save(&state).Error; err != nil {
			return err
		}
		if err := tx.Delete(&challenge).Error; err != nil {
			return err
		}
		auth, err = createSession(tx, user)
		if err != nil {
			return err
		}
		return appendAudit(tx, user.ID, "account.mfa.login", "user", user.ID)
	})
	if err != nil {
		return nil, keyServiceError(err)
	}
	if rejected {
		return nil, apperrors.ErrUnauthorized
	}
	return auth, nil
}
func issueMFALoginChallenge(tx *gorm.DB, user entity.User, state entity.UserMFA) (*MFALoginChallenge, error) {
	now := time.Now().UTC()
	if mfaLocked(&state, now) {
		return nil, apperrors.ErrUnauthorized
	}
	token, err := secret.RandomURLSafe(32)
	if err != nil {
		return nil, err
	}
	if err := tx.Where("user_id = ? AND purpose = ?", user.ID, "login").Delete(&entity.MFAChallenge{}).Error; err != nil {
		return nil, err
	}
	challenge := entity.MFAChallenge{UserID: user.ID, Purpose: "login", TokenHash: secret.SHA256Hex(token), PasswordDigest: secret.SHA256Hex(user.PasswordHash), Generation: state.Generation, ExpiresAt: now.Add(mfaChallengeLifetime)}
	if err := tx.Create(&challenge).Error; err != nil {
		return nil, err
	}
	return &MFALoginChallenge{MFARequired: true, ChallengeToken: token, ExpiresAt: challenge.ExpiresAt, Methods: []string{"totp", "recovery_code"}}, nil
}
