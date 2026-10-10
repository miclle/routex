package service

import (
	"errors"
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// SAML revocation uses the existing bounded epoch fences. Disabling a provider
// must remain possible regardless of the number of retained external Sessions.
func (s *Service) invalidateRuntimeSAMLPolicy(revision string) {
	if s.runtime != nil && revision != "" {
		s.runtime.deniedSAMLPolicies.Store(revision, s.runtime.epoch.Add(1))
	}
}
func samlRuntimeBindingKey(id string, birth *time.Time) string {
	if id == "" || birth == nil || birth.IsZero() {
		return ""
	}
	return samlDigest("saml.runtime.binding.v1", id, birth.UTC())
}
func (s *Service) invalidateRuntimeSAMLBinding(id string, birth time.Time) {
	if s.runtime != nil {
		if key := samlRuntimeBindingKey(id, &birth); key != "" {
			s.runtime.deniedSAMLBindings.Store(key, s.runtime.epoch.Add(1))
		}
	}
}
func (s *Service) runtimeSAMLSessionDenied(session runtimeTeamSession) bool {
	return session.SAMLPolicyRevision != "" && runtimeDenied(&s.runtime.deniedSAMLPolicies, session.SAMLPolicyRevision) || session.SAMLBindingKey != "" && runtimeDenied(&s.runtime.deniedSAMLBindings, session.SAMLBindingKey)
}

func loadSAMLSessionRuntimeData(tx *gorm.DB, data *teamSessionRuntimeData) error {
	needed := false
	for _, row := range data.Sessions {
		if row.PrimaryMethod == "saml" {
			needed = true
			break
		}
	}
	if !needed {
		return nil
	}
	var p entity.SAMLProvider
	err := tx.Session(&gorm.Session{NewDB: true}).Select("id", "config_revision", "policy_revision", "IDPIssuer", "enabled", "verified_config_revision", "verified_by", "verified_user_created_at", "verified_binding_id", "verified_binding_created_at").Where(database.ExactText(tx, clause.Column{Name: "id"}, "saml")).Take(&p).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	data.SAMLProvider = &p
	if !p.Enabled || !samlVerified(p) {
		return nil
	}
	return tx.Session(&gorm.Session{NewDB: true}).Select("id", "provider_id", "issuer", "user_id", "user_created_at", "config_revision", "subject", "subject_digest", "created_at").Where("config_revision = ?", p.ConfigRevision).Find(&data.SAMLBindings).Error
}

func samlRuntimePrimary(row entity.Session, p *entity.SAMLProvider, bindings map[string]entity.SAMLBinding, users map[string]entity.User) bool {
	if !namedIdentityProofEmpty(row) {
		return false
	}
	if row.PrimaryMethod == "" {
		return row.SAMLBindingID == "" && row.SAMLBindingCreatedAt == nil && row.SAMLConfigRevision == "" && row.SAMLPolicyRevision == "" && row.SAMLUserCreatedAt == nil
	}
	if row.PrimaryMethod != "saml" || p == nil || p.ID != "saml" || !p.Enabled || !samlVerified(*p) || !validMemberRoleDigest(p.ConfigRevision) || !validMemberRoleDigest(p.PolicyRevision) || p.IDPIssuer == "" || row.SAMLConfigRevision != p.ConfigRevision || row.SAMLPolicyRevision != p.PolicyRevision || row.SAMLBindingCreatedAt == nil || row.SAMLUserCreatedAt == nil {
		return false
	}
	b, ok := bindings[row.SAMLBindingID]
	u, exists := users[row.UserID]
	return ok && exists && b.ID == row.SAMLBindingID && b.UserID == row.UserID && b.UserCreatedAt.Equal(u.CreatedAt) && b.UserCreatedAt.Equal(*row.SAMLUserCreatedAt) && b.CreatedAt.Equal(*row.SAMLBindingCreatedAt) && !b.CreatedAt.IsZero() && b.ConfigRevision == p.ConfigRevision && b.ProviderID == p.ID && b.Issuer == p.IDPIssuer && samlSubject(b.Subject) && b.SubjectDigest == samlSubjectDigest(p.IDPIssuer, b.Subject)
}

// Existing dispatchers keep rejecting a SAML proof without its complete input.
func primaryRuntimeSessionWithSAML(row entity.Session, op *entity.OIDCProvider, ob map[string]entity.OIDCBinding, p *entity.OAuthProvider, b map[string]entity.OAuthBinding, lp *entity.LDAPProvider, lb map[string]entity.LDAPBinding, sp *entity.SAMLProvider, sb map[string]entity.SAMLBinding, users map[string]entity.User) bool {
	if !namedIdentityProofEmpty(row) {
		return false
	}
	if row.PrimaryMethod != "saml" {
		return primaryRuntimeSessionWithLDAP(row, op, ob, p, b, lp, lb, users)
	}
	if !primaryProofEmpty(row.OIDCBindingID, row.OIDCBindingCreatedAt, row.OIDCConfigRevision, row.OIDCPolicyRevision, row.OIDCUserCreatedAt) || !primaryProofEmpty(row.OAuthBindingID, row.OAuthBindingCreatedAt, row.OAuthConfigRevision, row.OAuthPolicyRevision, row.OAuthUserCreatedAt) || !primaryProofEmpty(row.LDAPBindingID, row.LDAPBindingCreatedAt, row.LDAPConfigRevision, row.LDAPPolicyRevision, row.LDAPUserCreatedAt) {
		return false
	}
	return samlRuntimePrimary(row, sp, sb, users)
}
