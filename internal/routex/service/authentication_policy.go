package service

import (
	"context"
	"encoding/base64"
	"errors"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/secret"
	"gorm.io/gorm"
)

// One admission time owns both deadlines. Bcrypt and remote work cannot renew
// the local transaction budget after returning.
func authenticationContexts(parent context.Context) (context.Context, context.Context, context.CancelFunc) {
	admitted := time.Now()
	outer, outerCancel := context.WithDeadline(parent, admitted.Add(10*time.Second))
	local, localCancel := context.WithDeadline(outer, admitted.Add(5*time.Second))
	return outer, local, func() { localCancel(); outerCancel() }
}
func authenticationPolicy(tx *gorm.DB) (entity.GovernanceSetting, error) {
	var p entity.GovernanceSetting
	if tx == nil {
		return p, apperrors.ErrUnauthorized
	}
	err := tx.Session(&gorm.Session{NewDB: true}).Take(&p, 1).Error
	if err != nil {
		return p, err
	}
	if !validMemberRoleDigest(p.AuthenticationPolicyRevision) || p.ForcedSSOEnabled && (p.EmergencyAdminID == "" || p.EmergencyAdminCreatedAt == nil || p.EmergencyAdminCreatedAt.IsZero()) || !p.ForcedSSOEnabled && (p.EmergencyAdminID != "" || p.EmergencyAdminCreatedAt != nil) {
		return p, apperrors.ErrUnauthorized
	}
	return p, nil
}
func authenticationMethodAllowed(p entity.GovernanceSetting, method string) bool {
	if method == "emergency" {
		return p.ForcedSSOEnabled
	}
	return !p.ForcedSSOEnabled || method == "oidc" || method == "ldap"
}
func authenticationLoginAllowed(tx *gorm.DB, method string) error {
	p, err := authenticationPolicy(tx)
	if err != nil {
		return err
	}
	if !authenticationMethodAllowed(p, method) {
		return apperrors.ErrUnauthorized
	}
	return tx.Statement.Context.Err()
}
func authenticationMFAGeneration(value string) bool {
	raw, err := base64.RawURLEncoding.Strict().DecodeString(value)
	return err == nil && len(raw) == 32 && base64.RawURLEncoding.EncodeToString(raw) == value
}
func emergencyProofEmpty(row entity.Session) bool {
	return row.EmergencyPolicyRevision == "" && row.EmergencyUserCreatedAt == nil && row.EmergencyMFAGeneration == "" && row.EmergencyPasswordDigest == "" && row.EmergencyFactorCompletedAt == nil
}
func authenticationProviderProofsEmpty(row entity.Session) bool {
	return primaryProofEmpty(row.OIDCBindingID, row.OIDCBindingCreatedAt, row.OIDCConfigRevision, row.OIDCPolicyRevision, row.OIDCUserCreatedAt) && primaryProofEmpty(row.OAuthBindingID, row.OAuthBindingCreatedAt, row.OAuthConfigRevision, row.OAuthPolicyRevision, row.OAuthUserCreatedAt) && primaryProofEmpty(row.LDAPBindingID, row.LDAPBindingCreatedAt, row.LDAPConfigRevision, row.LDAPPolicyRevision, row.LDAPUserCreatedAt) && primaryProofEmpty(row.SAMLBindingID, row.SAMLBindingCreatedAt, row.SAMLConfigRevision, row.SAMLPolicyRevision, row.SAMLUserCreatedAt) && namedIdentityProofEmpty(row)
}
func emergencyCurrent(p entity.GovernanceSetting, row entity.Session, u entity.User, m entity.UserMFA, now time.Time, challenge bool) bool {
	if !p.ForcedSSOEnabled || !validMemberRoleDigest(p.AuthenticationPolicyRevision) || row.PrimaryMethod != "emergency" || !authenticationProviderProofsEmpty(row) || row.UserID != p.EmergencyAdminID || row.UserID != u.ID || p.EmergencyAdminCreatedAt == nil || row.EmergencyUserCreatedAt == nil || !u.CreatedAt.Equal(*p.EmergencyAdminCreatedAt) || !u.CreatedAt.Equal(*row.EmergencyUserCreatedAt) || u.CreatedAt.IsZero() || u.Disabled || u.OffboardedAt != nil || u.Role != entity.RoleAdmin || row.EmergencyPolicyRevision != p.AuthenticationPolicyRevision || !m.Enabled || m.UserID != u.ID || !authenticationMFAGeneration(m.Generation) || row.EmergencyMFAGeneration != m.Generation || !validMemberRoleDigest(row.EmergencyPasswordDigest) || row.EmergencyPasswordDigest != secret.SHA256Hex(u.PasswordHash) {
		return false
	}
	if challenge {
		return row.EmergencyFactorCompletedAt == nil
	}
	return row.EmergencyFactorCompletedAt != nil && !row.EmergencyFactorCompletedAt.IsZero() && !row.EmergencyFactorCompletedAt.After(now) && !row.CreatedAt.IsZero() && !row.EmergencyFactorCompletedAt.After(row.CreatedAt) && row.CreatedAt.Before(row.ExpiresAt) && now.Before(row.ExpiresAt)
}
func emergencyValidatePrimary(tx *gorm.DB, row entity.Session, challenge bool) error {
	p, err := authenticationPolicy(tx)
	if err != nil {
		return err
	}
	u, err := registrationAdmittedUser(tx, row.UserID, false)
	if err != nil {
		return err
	}
	m, err := mfaState(tx, u.ID)
	if err != nil {
		return err
	}
	if !emergencyCurrent(p, row, u, m, time.Now().UTC(), challenge) {
		return apperrors.ErrUnauthorized
	}
	return tx.Statement.Context.Err()
}
func authenticationChallengeSession(c entity.MFAChallenge) entity.Session {
	return entity.Session{UserID: c.UserID, PrimaryMethod: c.PrimaryMethod, ExpiresAt: c.ExpiresAt, EmergencyPolicyRevision: c.EmergencyPolicyRevision, EmergencyUserCreatedAt: c.EmergencyUserCreatedAt, EmergencyMFAGeneration: c.EmergencyMFAGeneration, EmergencyPasswordDigest: c.EmergencyPasswordDigest, EmergencyFactorCompletedAt: c.EmergencyFactorCompletedAt, OIDCBindingID: c.OIDCBindingID, OIDCBindingCreatedAt: c.OIDCBindingCreatedAt, OIDCConfigRevision: c.OIDCConfigRevision, OIDCPolicyRevision: c.OIDCPolicyRevision, OIDCUserCreatedAt: c.OIDCUserCreatedAt, OAuthBindingID: c.OAuthBindingID, OAuthBindingCreatedAt: c.OAuthBindingCreatedAt, OAuthConfigRevision: c.OAuthConfigRevision, OAuthPolicyRevision: c.OAuthPolicyRevision, OAuthUserCreatedAt: c.OAuthUserCreatedAt, LDAPBindingID: c.LDAPBindingID, LDAPBindingCreatedAt: c.LDAPBindingCreatedAt, LDAPConfigRevision: c.LDAPConfigRevision, LDAPPolicyRevision: c.LDAPPolicyRevision, LDAPUserCreatedAt: c.LDAPUserCreatedAt, SAMLBindingID: c.SAMLBindingID, SAMLBindingCreatedAt: c.SAMLBindingCreatedAt, SAMLConfigRevision: c.SAMLConfigRevision, SAMLPolicyRevision: c.SAMLPolicyRevision, SAMLUserCreatedAt: c.SAMLUserCreatedAt, NamedIdentityProviderID: c.NamedIdentityProviderID, NamedIdentityProfileID: c.NamedIdentityProfileID, NamedIdentityBindingID: c.NamedIdentityBindingID, NamedIdentityBindingCreatedAt: c.NamedIdentityBindingCreatedAt, NamedIdentityConfigRevision: c.NamedIdentityConfigRevision, NamedIdentityPolicyRevision: c.NamedIdentityPolicyRevision, NamedIdentityUserCreatedAt: c.NamedIdentityUserCreatedAt}
}
func authenticationInstantEqual(a, b *time.Time) bool {
	return a == nil && b == nil || a != nil && b != nil && a.Equal(*b)
}

// Compare every persisted primary field; entity metadata is not a proof.
func authenticationSessionEqual(a, b entity.Session) bool {
	return a.ID == b.ID && a.UserID == b.UserID && a.TokenHash == b.TokenHash && a.CreatedAt.Equal(b.CreatedAt) && a.ExpiresAt.Equal(b.ExpiresAt) && a.PrimaryMethod == b.PrimaryMethod &&
		a.OIDCBindingID == b.OIDCBindingID && authenticationInstantEqual(a.OIDCBindingCreatedAt, b.OIDCBindingCreatedAt) && a.OIDCConfigRevision == b.OIDCConfigRevision && a.OIDCPolicyRevision == b.OIDCPolicyRevision && authenticationInstantEqual(a.OIDCUserCreatedAt, b.OIDCUserCreatedAt) &&
		a.OAuthBindingID == b.OAuthBindingID && authenticationInstantEqual(a.OAuthBindingCreatedAt, b.OAuthBindingCreatedAt) && a.OAuthConfigRevision == b.OAuthConfigRevision && a.OAuthPolicyRevision == b.OAuthPolicyRevision && authenticationInstantEqual(a.OAuthUserCreatedAt, b.OAuthUserCreatedAt) &&
		a.LDAPBindingID == b.LDAPBindingID && authenticationInstantEqual(a.LDAPBindingCreatedAt, b.LDAPBindingCreatedAt) && a.LDAPConfigRevision == b.LDAPConfigRevision && a.LDAPPolicyRevision == b.LDAPPolicyRevision && authenticationInstantEqual(a.LDAPUserCreatedAt, b.LDAPUserCreatedAt) &&
		a.SAMLBindingID == b.SAMLBindingID && authenticationInstantEqual(a.SAMLBindingCreatedAt, b.SAMLBindingCreatedAt) && a.SAMLConfigRevision == b.SAMLConfigRevision && a.SAMLPolicyRevision == b.SAMLPolicyRevision && authenticationInstantEqual(a.SAMLUserCreatedAt, b.SAMLUserCreatedAt) &&
		a.NamedIdentityProviderID == b.NamedIdentityProviderID && a.NamedIdentityProfileID == b.NamedIdentityProfileID && a.NamedIdentityBindingID == b.NamedIdentityBindingID && authenticationInstantEqual(a.NamedIdentityBindingCreatedAt, b.NamedIdentityBindingCreatedAt) && a.NamedIdentityConfigRevision == b.NamedIdentityConfigRevision && a.NamedIdentityPolicyRevision == b.NamedIdentityPolicyRevision && authenticationInstantEqual(a.NamedIdentityUserCreatedAt, b.NamedIdentityUserCreatedAt) &&
		a.EmergencyPolicyRevision == b.EmergencyPolicyRevision && authenticationInstantEqual(a.EmergencyUserCreatedAt, b.EmergencyUserCreatedAt) && a.EmergencyMFAGeneration == b.EmergencyMFAGeneration && a.EmergencyPasswordDigest == b.EmergencyPasswordDigest && authenticationInstantEqual(a.EmergencyFactorCompletedAt, b.EmergencyFactorCompletedAt)
}
func authenticationError(err error) error {
	if err == nil {
		return nil
	}
	var e *apperrors.Error
	if errors.As(err, &e) && (e.Code == 400 || e.Code == 401 || e.Code == 403 || e.Code == 409) {
		return e
	}
	return runtimeUnavailable
}
