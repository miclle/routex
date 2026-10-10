package service

import (
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/secret"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func ldapPrimary(tx *gorm.DB, method, bindingID string, bindingBirth *time.Time, config, policy, userID string, userBirth *time.Time) error {
	if method == "" {
		if bindingID != "" || bindingBirth != nil || config != "" || policy != "" || userBirth != nil {
			return apperrors.ErrUnauthorized
		}
		return nil
	}
	if method != "ldap" || bindingID == "" || bindingBirth == nil || bindingBirth.IsZero() || userBirth == nil || userBirth.IsZero() || !validMemberRoleDigest(config) || !validMemberRoleDigest(policy) {
		return apperrors.ErrUnauthorized
	}
	p, e := ldapProvider(tx)
	if e != nil {
		return e
	}
	if !p.Enabled || !ldapVerified(p) || p.ConfigRevision != config || p.PolicyRevision != policy {
		return apperrors.ErrUnauthorized
	}
	b, e := ldapBindingByID(tx, bindingID)
	if e != nil {
		return e
	}
	if b.ProviderID != p.ID || b.IdentityAttribute != p.IdentityAttribute || !ldapStoredSubject(b) || b.UserID != userID || !b.CreatedAt.Equal(*bindingBirth) || !b.UserCreatedAt.Equal(*userBirth) || b.ConfigRevision != config || b.SubjectDigest != ldapSubjectDigest(b.IdentityAttribute, b.Subject) {
		return apperrors.ErrUnauthorized
	}
	u, e := registrationAdmittedUser(tx, userID, false)
	if e != nil {
		return e
	}
	if !u.CreatedAt.Equal(*userBirth) {
		return apperrors.ErrUnauthorized
	}
	return nil
}
func ldapValidatePrimary(tx *gorm.DB, s entity.Session) error {
	if !primaryProofEmpty(s.OIDCBindingID, s.OIDCBindingCreatedAt, s.OIDCConfigRevision, s.OIDCPolicyRevision, s.OIDCUserCreatedAt) || !primaryProofEmpty(s.OAuthBindingID, s.OAuthBindingCreatedAt, s.OAuthConfigRevision, s.OAuthPolicyRevision, s.OAuthUserCreatedAt) {
		return apperrors.ErrUnauthorized
	}
	return ldapPrimary(tx, s.PrimaryMethod, s.LDAPBindingID, s.LDAPBindingCreatedAt, s.LDAPConfigRevision, s.LDAPPolicyRevision, s.UserID, s.LDAPUserCreatedAt)
}
func ldapStampSession(tx *gorm.DB, a *Authentication, p entity.LDAPProvider, b entity.LDAPBinding) error {
	a.Session.PrimaryMethod = "ldap"
	a.Session.LDAPBindingID = b.ID
	a.Session.LDAPBindingCreatedAt = &b.CreatedAt
	a.Session.LDAPConfigRevision = p.ConfigRevision
	a.Session.LDAPPolicyRevision = p.PolicyRevision
	a.Session.LDAPUserCreatedAt = &b.UserCreatedAt
	return tx.Save(&a.Session).Error
}
func ldapStampChallenge(tx *gorm.DB, u entity.User, p entity.LDAPProvider, b entity.LDAPBinding, issued *MFALoginChallenge) error {
	var row entity.MFAChallenge
	e := tx.Session(&gorm.Session{NewDB: true}).Where(database.ExactText(tx, clause.Column{Name: "user_id"}, u.ID)).Where("purpose = ? AND token_hash = ?", "login", secret.SHA256Hex(issued.ChallengeToken)).Take(&row).Error
	if e != nil {
		return e
	}
	if row.UserID != u.ID || row.Purpose != "login" || row.TokenHash != secret.SHA256Hex(issued.ChallengeToken) {
		return apperrors.ErrUnauthorized
	}
	row.PrimaryMethod = "ldap"
	row.LDAPBindingID = b.ID
	row.LDAPBindingCreatedAt = &b.CreatedAt
	row.LDAPConfigRevision = p.ConfigRevision
	row.LDAPPolicyRevision = p.PolicyRevision
	row.LDAPUserCreatedAt = &u.CreatedAt
	return tx.Save(&row).Error
}
