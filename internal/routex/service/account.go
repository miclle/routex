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
func (s *Service) ChangePassword(ctx context.Context, userID, currentPassword, newPassword string) (*Authentication, error) {
	if !validPassword(currentPassword) || !validPassword(newPassword) || currentPassword == newPassword {
		return nil, apperrors.ErrBadRequest
	}
	var before entity.User
	if err := s.authDB(ctx).Where("id = ? AND disabled = ?", userID, false).First(&before).Error; err != nil {
		return nil, apperrors.ErrUnauthorized
	}
	if bcrypt.CompareHashAndPassword([]byte(before.PasswordHash), []byte(currentPassword)) != nil {
		return nil, apperrors.ErrBadRequest
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return nil, apperrors.ErrInternal
	}
	var auth *Authentication
	err = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		var user entity.User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&user, "id = ?", userID).Error; err != nil {
			return err
		}
		if user.Disabled || user.PasswordHash != before.PasswordHash {
			return apperrors.ErrUnauthorized
		}
		if err := tx.Model(&user).Update("password_hash", string(hash)).Error; err != nil {
			return err
		}
		user.PasswordHash = string(hash)
		if err := invalidateMFAChallenges(tx, userID); err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", userID).Delete(&entity.Session{}).Error; err != nil {
			return err
		}
		var err error
		auth, err = createSession(tx, user)
		if err != nil {
			return err
		}
		return appendAudit(tx, userID, "account.password.change", "user", userID)
	})
	return auth, keyServiceError(err)
}

func (s *Service) ListAccountSessions(ctx context.Context, userID string) ([]entity.Session, error) {
	sessions := []entity.Session{}
	err := s.authDB(ctx).Select("id", "user_id", "created_at", "expires_at").Where("user_id = ? AND expires_at > ?", userID, time.Now().UTC()).Order("created_at DESC, id DESC").Find(&sessions).Error
	return sessions, keyServiceError(err)
}

func (s *Service) RevokeAccountSession(ctx context.Context, userID, sessionID string) error {
	return keyServiceError(s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		var session entity.Session
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND user_id = ?", sessionID, userID).First(&session).Error; err != nil {
			return catalogError(err)
		}
		if err := tx.Delete(&session).Error; err != nil {
			return err
		}
		return appendAudit(tx, userID, "account.session.revoke", "session", sessionID)
	}))
}
