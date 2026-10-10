package service

import (
	"errors"
	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/secret"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

func identityCurrentSession(tx *gorm.DB, a *Authentication, u entity.User, lock bool) error {
	if a == nil || a.User.ID != u.ID || !a.User.CreatedAt.Equal(u.CreatedAt) || a.Session.ID == "" || a.Session.CreatedAt.IsZero() {
		return apperrors.ErrUnauthorized
	}
	var row entity.Session
	q := tx.Session(&gorm.Session{NewDB: true}).Where(database.ExactText(tx, clause.Column{Name: "id"}, a.Session.ID))
	if lock {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	e := q.Take(&row).Error
	if errors.Is(e, gorm.ErrRecordNotFound) {
		return apperrors.ErrUnauthorized
	}
	if e != nil {
		return e
	}
	if row.ID != a.Session.ID || row.UserID != u.ID || !row.CreatedAt.Equal(a.Session.CreatedAt) || row.TokenHash != secret.SHA256Hex(a.Token) || !row.ExpiresAt.After(time.Now().UTC()) {
		return apperrors.ErrUnauthorized
	}
	return primaryValidateSession(tx, row)
}
func (s *Service) identityLocalProof(tx *gorm.DB, a *Authentication, passwordHash string, proof MFAProof) (entity.User, entity.UserMFA, bool, error) {
	if a == nil {
		return entity.User{}, entity.UserMFA{}, false, apperrors.ErrUnauthorized
	}
	u, e := registrationAdmittedUser(tx, a.User.ID, true)
	if e != nil {
		return u, entity.UserMFA{}, false, e
	}
	if u.PasswordHash != passwordHash {
		return u, entity.UserMFA{}, false, apperrors.ErrUnauthorized
	}
	if e = identityCurrentSession(tx, a, u, true); e != nil {
		return u, entity.UserMFA{}, false, e
	}
	m, e := mfaState(tx, u.ID)
	if e != nil {
		return u, m, false, e
	}
	now := time.Now().UTC()
	if !m.Enabled {
		if proof.Code != "" || proof.RecoveryCode != "" {
			return u, m, false, apperrors.ErrBadRequest
		}
		return u, m, false, nil
	}
	if (proof.Code == "") == (proof.RecoveryCode == "") {
		return u, m, false, apperrors.ErrBadRequest
	}
	if mfaLocked(&m, now) {
		return u, m, false, apperrors.ErrUnauthorized
	}
	valid, e := s.consumeMFAProof(tx, &m, MFAProof(proof), now)
	if e != nil {
		return u, m, false, e
	}
	if !valid {
		return u, m, true, mfaFailure(tx, &m, now)
	}
	resetMFAFailures(&m)
	e = tx.Save(&m).Error
	return u, m, false, e
}
