package service

import (
	"errors"
	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

func namedIdentityRuntimePolicyKey(revision string) string {
	return namedIdentityRuntimePolicyKeyFor(githubProviderID, revision)
}
func namedIdentityRuntimePolicyKeyFor(providerID, revision string) string {
	if !namedIdentityMethod(providerID) || revision == "" {
		return ""
	}
	return namedIdentityDigest("named-identity.runtime.policy.v1", providerID, namedIdentityProfileID(providerID), revision)
}
func namedIdentityRuntimeBindingKey(id string, birth *time.Time) string {
	return namedIdentityRuntimeBindingKeyFor(githubProviderID, id, birth)
}
func namedIdentityRuntimeBindingKeyFor(providerID, id string, birth *time.Time) string {
	if !namedIdentityMethod(providerID) || id == "" || birth == nil || birth.IsZero() {
		return ""
	}
	return namedIdentityDigest("named-identity.runtime.binding.v1", providerID, namedIdentityProfileID(providerID), id, birth.UTC())
}
func (s *Service) invalidateRuntimeNamedIdentityPolicy(providerID, revision string) {
	if s.runtime != nil {
		if key := namedIdentityRuntimePolicyKeyFor(providerID, revision); key != "" {
			s.runtime.deniedNamedIdentityPolicies.Store(key, s.runtime.epoch.Add(1))
		}
	}
}
func (s *Service) invalidateRuntimeNamedIdentityBinding(providerID, id string, birth time.Time) {
	if s.runtime != nil {
		if key := namedIdentityRuntimeBindingKeyFor(providerID, id, &birth); key != "" {
			s.runtime.deniedNamedIdentityBindings.Store(key, s.runtime.epoch.Add(1))
		}
	}
}
func (s *Service) runtimeNamedIdentitySessionDenied(row runtimeTeamSession) bool {
	return row.NamedIdentityPolicyKey != "" && runtimeDenied(&s.runtime.deniedNamedIdentityPolicies, row.NamedIdentityPolicyKey) || row.NamedIdentityBindingKey != "" && runtimeDenied(&s.runtime.deniedNamedIdentityBindings, row.NamedIdentityBindingKey)
}
func loadNamedIdentitySessionRuntimeData(tx *gorm.DB, data *teamSessionRuntimeData) error {
	data.NamedIdentityProviders = map[string]*entity.NamedIdentityProvider{}
	needed := map[string]bool{}
	for _, r := range data.Sessions {
		if namedIdentityMethod(r.PrimaryMethod) {
			needed[r.PrimaryMethod] = true
		}
	}
	for _, providerID := range []string{githubProviderID, googleProviderID, discordProviderID} {
		if !needed[providerID] {
			continue
		}
		var p entity.NamedIdentityProvider
		err := tx.Session(&gorm.Session{NewDB: true}).Select("id", "profile_id", "identity_issuer", "config_revision", "policy_revision", "enabled", "verified_config_revision", "verified_by", "verified_user_created_at", "verified_binding_id", "verified_binding_created_at").Where(database.ExactText(tx, clause.Column{Name: "id"}, providerID)).Take(&p).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			continue
		}
		if err != nil {
			return err
		}
		if p.ID != providerID || !namedIdentityProfile(p.ID, p.ProfileID, p.IdentityIssuer) {
			continue
		}
		data.NamedIdentityProviders[providerID] = &p
		if !p.Enabled || !namedIdentityVerified(p) {
			continue
		}
		var bindings []entity.NamedIdentityBinding
		if err = tx.Session(&gorm.Session{NewDB: true}).Select("id", "provider_id", "profile_id", "identity_issuer", "user_id", "user_created_at", "config_revision", "subject_kind", "subject", "subject_digest", "created_at").Where(database.ExactText(tx, clause.Column{Name: "provider_id"}, p.ID)).Where("config_revision = ?", p.ConfigRevision).Find(&bindings).Error; err != nil {
			return err
		}
		data.NamedIdentityBindings = append(data.NamedIdentityBindings, bindings...)
	}
	return nil
}
func namedIdentityRuntimePrimary(row entity.Session, p *entity.NamedIdentityProvider, bindings map[string]entity.NamedIdentityBinding, users map[string]entity.User) bool {
	if !namedIdentityMethod(row.PrimaryMethod) || row.NamedIdentityProviderID != row.PrimaryMethod || row.NamedIdentityProfileID != namedIdentityProfileID(row.PrimaryMethod) || p == nil || p.ID != row.PrimaryMethod || !namedIdentityProfile(p.ID, p.ProfileID, p.IdentityIssuer) || !p.Enabled || !namedIdentityVerified(*p) || !validMemberRoleDigest(p.ConfigRevision) || !validMemberRoleDigest(p.PolicyRevision) || row.NamedIdentityConfigRevision != p.ConfigRevision || row.NamedIdentityPolicyRevision != p.PolicyRevision || row.NamedIdentityBindingCreatedAt == nil || row.NamedIdentityUserCreatedAt == nil {
		return false
	}
	b, ok := bindings[row.NamedIdentityBindingID]
	u, exists := users[row.UserID]
	return ok && exists && b.ID == row.NamedIdentityBindingID && b.ProviderID == p.ID && namedIdentityProfile(b.ProviderID, b.ProfileID, b.IdentityIssuer) && b.UserID == row.UserID && !b.CreatedAt.IsZero() && b.CreatedAt.Equal(*row.NamedIdentityBindingCreatedAt) && b.UserCreatedAt.Equal(u.CreatedAt) && b.UserCreatedAt.Equal(*row.NamedIdentityUserCreatedAt) && b.ConfigRevision == p.ConfigRevision && namedIdentityProfileSubject(p.ID, b.SubjectKind, b.Subject) && b.SubjectDigest == namedIdentitySubjectDigest(p.ID, b.SubjectKind, b.Subject)
}
func primaryRuntimeSessionWithNamedIdentity(row entity.Session, op *entity.OIDCProvider, ob map[string]entity.OIDCBinding, p *entity.OAuthProvider, b map[string]entity.OAuthBinding, lp *entity.LDAPProvider, lb map[string]entity.LDAPBinding, sp *entity.SAMLProvider, sb map[string]entity.SAMLBinding, np *entity.NamedIdentityProvider, nb map[string]entity.NamedIdentityBinding, users map[string]entity.User) bool {
	if !namedIdentityMethod(row.PrimaryMethod) {
		if !namedIdentityProofEmpty(row) {
			return false
		}
		return primaryRuntimeSessionWithSAML(row, op, ob, p, b, lp, lb, sp, sb, users)
	}
	if !primaryProofEmpty(row.OIDCBindingID, row.OIDCBindingCreatedAt, row.OIDCConfigRevision, row.OIDCPolicyRevision, row.OIDCUserCreatedAt) || !primaryProofEmpty(row.OAuthBindingID, row.OAuthBindingCreatedAt, row.OAuthConfigRevision, row.OAuthPolicyRevision, row.OAuthUserCreatedAt) || !primaryProofEmpty(row.LDAPBindingID, row.LDAPBindingCreatedAt, row.LDAPConfigRevision, row.LDAPPolicyRevision, row.LDAPUserCreatedAt) || !primaryProofEmpty(row.SAMLBindingID, row.SAMLBindingCreatedAt, row.SAMLConfigRevision, row.SAMLPolicyRevision, row.SAMLUserCreatedAt) {
		return false
	}
	return namedIdentityRuntimePrimary(row, np, nb, users)
}

func (s *Service) invalidateRuntimeGitHubPolicy(revision string) {
	s.invalidateRuntimeNamedIdentityPolicy(githubProviderID, revision)
}
func (s *Service) invalidateRuntimeGitHubBinding(id string, birth time.Time) {
	s.invalidateRuntimeNamedIdentityBinding(githubProviderID, id, birth)
}
