package service

import (
	"errors"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
)

// OAuth revocation uses the existing bounded epoch fences. Disabling a provider
// must remain possible regardless of the number of retained external Sessions.
func (s *Service) invalidateRuntimeOAuthPolicy(revision string) {
	if s.runtime != nil && revision != "" {
		s.runtime.deniedOAuthPolicies.Store(revision, s.runtime.epoch.Add(1))
	}
}
func oauthRuntimeBindingKey(id string, birth *time.Time) string {
	if id == "" || birth == nil || birth.IsZero() {
		return ""
	}
	return oauthDigest("oauth.runtime.binding.v1", id, birth.UTC())
}
func (s *Service) invalidateRuntimeOAuthBinding(id string, birth time.Time) {
	if s.runtime != nil {
		if key := oauthRuntimeBindingKey(id, &birth); key != "" {
			s.runtime.deniedOAuthBindings.Store(key, s.runtime.epoch.Add(1))
		}
	}
}
func (s *Service) runtimeOAuthSessionDenied(session runtimeTeamSession) bool {
	return session.OAuthPolicyRevision != "" && runtimeDenied(&s.runtime.deniedOAuthPolicies, session.OAuthPolicyRevision) || session.OAuthBindingKey != "" && runtimeDenied(&s.runtime.deniedOAuthBindings, session.OAuthBindingKey)
}

func loadOAuthSessionRuntimeData(tx *gorm.DB, data *teamSessionRuntimeData) error {
	needed := false
	for _, row := range data.Sessions {
		if row.PrimaryMethod == "oauth" {
			needed = true
			break
		}
	}
	if !needed {
		return nil
	}
	var p entity.OAuthProvider
	err := tx.Session(&gorm.Session{NewDB: true}).Select("id", "config_revision", "policy_revision", "authorization_url", "token_url", "user_info_url", "enabled", "verified_config_revision", "verified_by", "verified_user_created_at", "verified_binding_id", "verified_binding_created_at").Take(&p, "id = ?", "oauth").Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	data.OAuthProvider = &p
	if !p.Enabled || !oauthVerified(p) {
		return nil
	}
	return tx.Session(&gorm.Session{NewDB: true}).Select("id", "provider_id", "subject_kind", "user_id", "user_created_at", "config_revision", "subject", "subject_digest", "created_at").Where("config_revision = ?", p.ConfigRevision).Find(&data.OAuthBindings).Error
}

func oauthRuntimePrimary(row entity.Session, p *entity.OAuthProvider, bindings map[string]entity.OAuthBinding, users map[string]entity.User) bool {
	if !namedIdentityProofEmpty(row) {
		return false
	}
	if row.PrimaryMethod == "" {
		return row.OAuthBindingID == "" && row.OAuthBindingCreatedAt == nil && row.OAuthConfigRevision == "" && row.OAuthPolicyRevision == "" && row.OAuthUserCreatedAt == nil
	}
	if row.PrimaryMethod != "oauth" || p == nil || p.ID != "oauth" || !p.Enabled || !oauthVerified(*p) || !validMemberRoleDigest(p.ConfigRevision) || !validMemberRoleDigest(p.PolicyRevision) || (p.AuthorizationURL == "" || p.TokenURL == "" || p.UserInfoURL == "") || row.OAuthConfigRevision != p.ConfigRevision || row.OAuthPolicyRevision != p.PolicyRevision || row.OAuthBindingCreatedAt == nil || row.OAuthUserCreatedAt == nil {
		return false
	}
	b, ok := bindings[row.OAuthBindingID]
	u, exists := users[row.UserID]
	return ok && exists && b.ID == row.OAuthBindingID && b.UserID == row.UserID && b.UserCreatedAt.Equal(u.CreatedAt) && b.UserCreatedAt.Equal(*row.OAuthUserCreatedAt) && b.CreatedAt.Equal(*row.OAuthBindingCreatedAt) && !b.CreatedAt.IsZero() && b.ConfigRevision == p.ConfigRevision && b.ProviderID == p.ID && oauthSubject(b.SubjectKind, b.Subject) && b.SubjectDigest == oauthSubjectDigest(p.ID, b.SubjectKind, b.Subject)
}

func primaryRuntimeSession(row entity.Session, op *entity.OIDCProvider, ob map[string]entity.OIDCBinding, p *entity.OAuthProvider, b map[string]entity.OAuthBinding, users map[string]entity.User) bool {
	if !namedIdentityProofEmpty(row) {
		return false
	}
	oe := primaryProofEmpty(row.OIDCBindingID, row.OIDCBindingCreatedAt, row.OIDCConfigRevision, row.OIDCPolicyRevision, row.OIDCUserCreatedAt)
	ae := primaryProofEmpty(row.OAuthBindingID, row.OAuthBindingCreatedAt, row.OAuthConfigRevision, row.OAuthPolicyRevision, row.OAuthUserCreatedAt)
	le := primaryProofEmpty(row.LDAPBindingID, row.LDAPBindingCreatedAt, row.LDAPConfigRevision, row.LDAPPolicyRevision, row.LDAPUserCreatedAt)
	if !le || !primaryProofEmpty(row.SAMLBindingID, row.SAMLBindingCreatedAt, row.SAMLConfigRevision, row.SAMLPolicyRevision, row.SAMLUserCreatedAt) {
		return false
	}
	switch row.PrimaryMethod {
	case "":
		return oe && ae
	case "oidc":
		return ae && oidcRuntimePrimary(row, op, ob, users)
	case "oauth":
		return oe && oauthRuntimePrimary(row, p, b, users)
	default:
		return false
	}
}

// The old dispatcher remains available to existing callers and rejects LDAP
// proof without its complete current publication input.
func primaryRuntimeSessionWithLDAP(row entity.Session, op *entity.OIDCProvider, ob map[string]entity.OIDCBinding, p *entity.OAuthProvider, b map[string]entity.OAuthBinding, lp *entity.LDAPProvider, lb map[string]entity.LDAPBinding, users map[string]entity.User) bool {
	if !namedIdentityProofEmpty(row) {
		return false
	}
	if !primaryProofEmpty(row.SAMLBindingID, row.SAMLBindingCreatedAt, row.SAMLConfigRevision, row.SAMLPolicyRevision, row.SAMLUserCreatedAt) {
		return false
	}
	if row.PrimaryMethod != "ldap" {
		return primaryRuntimeSession(row, op, ob, p, b, users)
	}
	if !primaryProofEmpty(row.OIDCBindingID, row.OIDCBindingCreatedAt, row.OIDCConfigRevision, row.OIDCPolicyRevision, row.OIDCUserCreatedAt) || !primaryProofEmpty(row.OAuthBindingID, row.OAuthBindingCreatedAt, row.OAuthConfigRevision, row.OAuthPolicyRevision, row.OAuthUserCreatedAt) {
		return false
	}
	return ldapRuntimePrimary(row, lp, lb, users)
}
