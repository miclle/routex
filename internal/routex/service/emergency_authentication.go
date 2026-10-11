package service

import (
	"context"
	"errors"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/id"
	"github.com/miclle/routex/pkg/secret"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

func (s *Service) BeginEmergencyLogin(parent context.Context, email, password string) (*MFALoginChallenge, error) {
	_, local, cancel := authenticationContexts(parent)
	defer cancel()
	email, valid := normalizeEmail(email)
	if !valid || !validPassword(password) {
		return nil, apperrors.ErrUnauthorized
	}
	admittedPolicy, e := authenticationPolicy(s.authDB(local))
	if e != nil {
		return nil, authenticationError(e)
	}
	var before entity.User
	e = s.authDB(local).Where("email = ?", email).Take(&before).Error
	if e != nil && !errors.Is(e, gorm.ErrRecordNotFound) {
		return nil, authenticationError(e)
	}
	hash := before.PasswordHash
	if e != nil {
		hash = "$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy"
	}
	bad := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil
	if local.Err() != nil {
		return nil, authenticationError(local.Err())
	}
	if e != nil || bad {
		return nil, apperrors.ErrUnauthorized
	}
	var result *MFALoginChallenge
	e = s.authDB(local).Transaction(func(tx *gorm.DB) error {
		if e := lockGovernance(tx); e != nil {
			return e
		}
		p, e := authenticationPolicy(tx)
		if e != nil {
			return e
		}
		if p.ForcedSSOEnabled != admittedPolicy.ForcedSSOEnabled || p.AuthenticationPolicyRevision != admittedPolicy.AuthenticationPolicyRevision || p.EmergencyAdminID != admittedPolicy.EmergencyAdminID || !authenticationInstantEqual(p.EmergencyAdminCreatedAt, admittedPolicy.EmergencyAdminCreatedAt) {
			return apperrors.ErrUnauthorized
		}
		u, e := registrationAdmittedUser(tx, before.ID, true)
		if e != nil {
			return e
		}
		m, e := mfaState(tx, u.ID)
		if e != nil {
			return e
		}
		c := entity.MFAChallenge{UserID: u.ID, Purpose: "emergency", PrimaryMethod: "emergency", PasswordDigest: secret.SHA256Hex(u.PasswordHash), Generation: m.Generation, EmergencyPolicyRevision: p.AuthenticationPolicyRevision, EmergencyUserCreatedAt: &u.CreatedAt, EmergencyMFAGeneration: m.Generation, EmergencyPasswordDigest: secret.SHA256Hex(u.PasswordHash)}
		now := time.Now().UTC().Truncate(time.Microsecond)
		if u.PasswordHash != before.PasswordHash || !u.CreatedAt.Equal(before.CreatedAt) || mfaLocked(&m, now) || !emergencyCurrent(p, authenticationChallengeSession(c), u, m, now, true) {
			return apperrors.ErrUnauthorized
		}
		token, e := secret.RandomURLSafe(32)
		if e != nil {
			return e
		}
		c.TokenHash = secret.SHA256Hex(token)
		c.ExpiresAt = now.Add(mfaChallengeLifetime)
		if e = tx.Where("user_id = ? AND purpose = ?", u.ID, "emergency").Delete(&entity.MFAChallenge{}).Error; e != nil {
			return e
		}
		if local.Err() != nil {
			return local.Err()
		}
		if e = tx.Create(&c).Error; e != nil {
			return e
		}
		result = &MFALoginChallenge{MFARequired: true, ChallengeToken: token, ExpiresAt: c.ExpiresAt, Methods: []string{"totp", "recovery_code"}}
		return local.Err()
	})
	if e != nil {
		return nil, authenticationError(e)
	}
	return result, nil
}
func (s *Service) CompleteEmergencyLogin(parent context.Context, token string, proof MFAProof) (*Authentication, error) {
	_, local, cancel := authenticationContexts(parent)
	defer cancel()
	if !oidcBrowserValue(token) {
		return nil, apperrors.ErrUnauthorized
	}
	var before entity.MFAChallenge
	e := s.authDB(local).Where("purpose = ? AND token_hash = ?", "emergency", secret.SHA256Hex(token)).Take(&before).Error
	if errors.Is(e, gorm.ErrRecordNotFound) {
		return nil, apperrors.ErrUnauthorized
	}
	if e != nil {
		return nil, authenticationError(e)
	}
	var result *Authentication
	rejected := false
	e = s.authDB(local).Transaction(func(tx *gorm.DB) error {
		if e := lockGovernance(tx); e != nil {
			return e
		}
		p, e := authenticationPolicy(tx)
		if e != nil {
			return e
		}
		u, e := registrationAdmittedUser(tx, before.UserID, true)
		if e != nil {
			return e
		}
		m, e := mfaState(tx, u.ID)
		if e != nil {
			return e
		}
		var c entity.MFAChallenge
		if e = tx.Where("user_id = ? AND purpose = ? AND token_hash = ?", u.ID, "emergency", secret.SHA256Hex(token)).Take(&c).Error; e != nil {
			if errors.Is(e, gorm.ErrRecordNotFound) {
				return apperrors.ErrUnauthorized
			}
			return e
		}
		now := time.Now().UTC().Truncate(time.Microsecond)
		if mfaLocked(&m, now) || !mfaChallengeValid(c, u, m, now) || c.PasswordDigest != c.EmergencyPasswordDigest || c.Generation != c.EmergencyMFAGeneration || !emergencyCurrent(p, authenticationChallengeSession(c), u, m, now, true) {
			return apperrors.ErrUnauthorized
		}
		valid, e := s.consumeMFAProof(tx, &m, proof, now)
		if e != nil {
			return e
		}
		if !valid {
			rejected = true
			c.Attempts++
			if e = tx.Save(&c).Error; e != nil {
				return e
			}
			return mfaFailure(tx, &m, now)
		}
		resetMFAFailures(&m)
		if e = tx.Save(&m).Error; e != nil {
			return e
		}
		if e = tx.Delete(&c).Error; e != nil {
			return e
		}
		bearer, e := secret.RandomURLSafe(32)
		if e != nil {
			return e
		}
		sid, e := id.NewPrefixed("ses")
		if e != nil {
			return e
		}
		row := authenticationChallengeSession(c)
		row.ID = sid
		row.TokenHash = secret.SHA256Hex(bearer)
		row.CreatedAt = now
		row.ExpiresAt = now.Add(SessionLifetime)
		row.EmergencyFactorCompletedAt = &now
		if !emergencyCurrent(p, row, u, m, now, false) {
			return apperrors.ErrUnauthorized
		}
		if local.Err() != nil {
			return local.Err()
		}
		if e = tx.Create(&row).Error; e != nil {
			return e
		}
		result = &Authentication{User: u, Session: row, Token: bearer}
		if e = recordSuccessfulLogin(tx, &result.User); e != nil {
			return e
		}
		if e = appendAudit(tx, u.ID, "authentication.emergency.login", "user", u.ID); e != nil {
			return e
		}
		return local.Err()
	})
	if e != nil {
		return nil, authenticationError(e)
	}
	if rejected {
		return nil, apperrors.ErrUnauthorized
	}
	s.publishSessionMutation(local)
	return result, nil
}
