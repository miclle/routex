package service

import (
	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/id"
	"github.com/miclle/routex/pkg/secret"
	"gorm.io/gorm"
	"time"
)

type PasswordMutation struct {
	Authentication           *Authentication
	ReauthenticationRequired bool
}

// The caller rechecks the exact original Session/primary proof under governance
// before changing security state. This helper then replaces or revokes it.
func rotateAuthenticatedMFASession(tx *gorm.DB, u entity.User, original *Authentication) (*Authentication, error) {
	if original == nil {
		return nil, apperrors.ErrUnauthorized
	}
	if e := invalidateMFAChallenges(tx, u.ID); e != nil {
		return nil, e
	}
	if e := tx.Where("user_id = ?", u.ID).Delete(&entity.Session{}).Error; e != nil {
		return nil, e
	}
	if original.Session.PrimaryMethod == "emergency" {
		return nil, nil
	}
	if original.Session.PrimaryMethod == "" {
		return createSession(tx, u)
	}
	if !time.Now().Before(original.Session.ExpiresAt) {
		return nil, apperrors.ErrUnauthorized
	}
	token, e := secret.RandomURLSafe(32)
	if e != nil {
		return nil, e
	}
	sid, e := id.NewPrefixed("ses")
	if e != nil {
		return nil, e
	}
	row := original.Session
	row.ID = sid
	row.TokenHash = secret.SHA256Hex(token)
	row.CreatedAt = time.Now().UTC().Truncate(time.Microsecond)
	if !row.CreatedAt.Before(row.ExpiresAt) {
		return nil, apperrors.ErrUnauthorized
	}
	if e := tx.Statement.Context.Err(); e != nil {
		return nil, e
	}
	if e := tx.Create(&row).Error; e != nil {
		return nil, e
	}
	return &Authentication{User: u, Session: row, Token: token}, nil
}
