package service

import (
	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"gorm.io/gorm"
	"time"
)

// A missing or mixed method proof never degrades to local authentication.
func primaryProofEmpty(id string, birth *time.Time, config, policy string, userBirth *time.Time) bool {
	return id == "" && birth == nil && config == "" && policy == "" && userBirth == nil
}
func primaryValidateSession(tx *gorm.DB, row entity.Session) error {
	oidcEmpty := primaryProofEmpty(row.OIDCBindingID, row.OIDCBindingCreatedAt, row.OIDCConfigRevision, row.OIDCPolicyRevision, row.OIDCUserCreatedAt)
	oauthEmpty := primaryProofEmpty(row.OAuthBindingID, row.OAuthBindingCreatedAt, row.OAuthConfigRevision, row.OAuthPolicyRevision, row.OAuthUserCreatedAt)
	ldapEmpty := primaryProofEmpty(row.LDAPBindingID, row.LDAPBindingCreatedAt, row.LDAPConfigRevision, row.LDAPPolicyRevision, row.LDAPUserCreatedAt)
	switch row.PrimaryMethod {
	case "":
		if !oidcEmpty || !oauthEmpty || !ldapEmpty {
			return apperrors.ErrUnauthorized
		}
		return nil
	case "oidc":
		if !oauthEmpty || !ldapEmpty {
			return apperrors.ErrUnauthorized
		}
		return oidcValidatePrimary(tx, row)
	case "oauth":
		if !oidcEmpty || !ldapEmpty {
			return apperrors.ErrUnauthorized
		}
		return oauthValidatePrimary(tx, row)
	case "ldap":
		if !oidcEmpty || !oauthEmpty {
			return apperrors.ErrUnauthorized
		}
		return ldapValidatePrimary(tx, row)
	default:
		return apperrors.ErrUnauthorized
	}
}
func primaryValidateChallenge(tx *gorm.DB, row entity.MFAChallenge) error {
	return primaryValidateSession(tx, entity.Session{UserID: row.UserID, PrimaryMethod: row.PrimaryMethod, OIDCBindingID: row.OIDCBindingID, OIDCBindingCreatedAt: row.OIDCBindingCreatedAt, OIDCConfigRevision: row.OIDCConfigRevision, OIDCPolicyRevision: row.OIDCPolicyRevision, OIDCUserCreatedAt: row.OIDCUserCreatedAt, OAuthBindingID: row.OAuthBindingID, OAuthBindingCreatedAt: row.OAuthBindingCreatedAt, OAuthConfigRevision: row.OAuthConfigRevision, OAuthPolicyRevision: row.OAuthPolicyRevision, OAuthUserCreatedAt: row.OAuthUserCreatedAt, LDAPBindingID: row.LDAPBindingID, LDAPBindingCreatedAt: row.LDAPBindingCreatedAt, LDAPConfigRevision: row.LDAPConfigRevision, LDAPPolicyRevision: row.LDAPPolicyRevision, LDAPUserCreatedAt: row.LDAPUserCreatedAt})
}
func (s *Service) primaryValidateSession(tx *gorm.DB, row entity.Session) error {
	return primaryValidateSession(tx, row)
}
func (s *Service) primaryValidateChallenge(tx *gorm.DB, row entity.MFAChallenge) error {
	return primaryValidateChallenge(tx, row)
}
