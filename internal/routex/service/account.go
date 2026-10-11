package service

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
)

func (s *Service) UpdateProfile(ctx context.Context, userID, name string) (*entity.User, error) {
	name = strings.TrimSpace(name)
	if name == "" || !utf8.ValidString(name) || utf8.RuneCountInString(name) > 100 {
		return nil, apperrors.ErrBadRequest
	}
	var user entity.User
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND disabled = ?", userID, false).First(&user).Error; err != nil {
			return err
		}
		if err := requireRegistrationAdmission(tx.Session(&gorm.Session{}), user); err != nil {
			return err
		}
		user.Name = name
		if err := tx.Model(&user).Update("name", name).Error; err != nil {
			return err
		}
		return appendAudit(tx, userID, "account.profile.update", "user", userID)
	})
	return &user, catalogError(err)
}

// ChangePassword rotates the browser session and revokes every prior session.
// Lock and recheck the hash to prevent concurrent changes using an old password.
// The trusted compatibility path is local-only and cannot bypass enforcement.
func (s *Service) ChangePassword(ctx context.Context, userID, currentPassword, newPassword string) (*Authentication, error) {
	result, e := s.changePassword(ctx, nil, userID, currentPassword, newPassword)
	if e != nil {
		return nil, e
	}
	return result.Authentication, nil
}
func (s *Service) ChangePasswordAuthenticated(ctx context.Context, original *Authentication, currentPassword, newPassword string) (*PasswordMutation, error) {
	if original == nil {
		return nil, apperrors.ErrUnauthorized
	}
	return s.changePassword(ctx, original, original.User.ID, currentPassword, newPassword)
}
func (s *Service) changePassword(parent context.Context, original *Authentication, userID, currentPassword, newPassword string) (*PasswordMutation, error) {
	_, ctx, cancel := authenticationContexts(parent)
	defer cancel()
	if !validPassword(currentPassword) || !validPassword(newPassword) || currentPassword == newPassword {
		return nil, apperrors.ErrBadRequest
	}
	before, e := registrationAdmittedUser(s.authDB(ctx), userID, false)
	if e != nil {
		return nil, keyServiceError(e)
	}
	if bcrypt.CompareHashAndPassword([]byte(before.PasswordHash), []byte(currentPassword)) != nil {
		return nil, apperrors.ErrBadRequest
	}
	hash, e := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if e != nil {
		return nil, apperrors.ErrInternal
	}
	if ctx.Err() != nil {
		return nil, authenticationError(ctx.Err())
	}
	result := &PasswordMutation{}
	release := s.pinPersonalKeyMutation()
	defer release()
	e = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if e := lockGovernance(tx); e != nil {
			return e
		}
		user, e := registrationAdmittedUser(tx, userID, true)
		if e != nil {
			return e
		}
		if !user.CreatedAt.Equal(before.CreatedAt) || user.PasswordHash != before.PasswordHash {
			return apperrors.ErrUnauthorized
		}
		if original == nil {
			if e := authenticationLoginAllowed(tx, ""); e != nil {
				return e
			}
		} else if e := mfaSession(tx, original, time.Now().UTC()); e != nil {
			return e
		}
		if e = tx.Model(&user).Update("password_hash", string(hash)).Error; e != nil {
			return e
		}
		user.PasswordHash = string(hash)
		if original == nil {
			if e = invalidateMFAChallenges(tx, userID); e != nil {
				return e
			}
			if e = tx.Where("user_id = ?", userID).Delete(&entity.Session{}).Error; e != nil {
				return e
			}
			result.Authentication, e = createSession(tx, user)
		} else {
			result.Authentication, e = rotateAuthenticatedMFASession(tx, user, original)
		}
		if e != nil {
			return e
		}
		result.ReauthenticationRequired = result.Authentication == nil
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return appendAudit(tx, userID, "account.password.change", "user", userID)
	})
	if e != nil {
		return nil, keyServiceError(e)
	}
	s.invalidateRuntimeSessionUser(userID)
	release()
	s.publishSessionMutation(ctx)
	return result, nil
}

func (s *Service) ListAccountSessions(ctx context.Context, userID string) ([]entity.Session, error) {
	if _, err := registrationAdmittedUser(s.authDB(ctx), userID, false); err != nil {
		return nil, keyServiceError(err)
	}
	sessions := []entity.Session{}
	err := s.authDB(ctx).Select("id", "user_id", "created_at", "expires_at").Where("user_id = ? AND expires_at > ?", userID, time.Now().UTC()).Order("created_at DESC, id DESC").Find(&sessions).Error
	return sessions, keyServiceError(err)
}

func (s *Service) RevokeAccountSession(ctx context.Context, userID, sessionID string) error {
	if _, err := registrationAdmittedUser(s.authDB(ctx), userID, false); err != nil {
		return keyServiceError(err)
	}
	var revokedID string
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		var session entity.Session
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND user_id = ?", sessionID, userID).First(&session).Error; err != nil {
			return catalogError(err)
		}
		revokedID = session.ID
		if err := tx.Delete(&session).Error; err != nil {
			return err
		}
		return appendAudit(tx, userID, "account.session.revoke", "session", sessionID)
	})
	if err == nil {
		s.invalidateRuntimeSession(revokedID)
		s.publishSessionMutation(ctx)
	}
	return keyServiceError(err)
}
