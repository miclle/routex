package service

import (
	"errors"
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// LDAP revocation uses the existing bounded epoch fences. Disabling a provider
// must remain possible regardless of the number of retained external Sessions.
func (s *Service) invalidateRuntimeLDAPPolicy(revision string) {
	if s.runtime != nil && revision != "" {
		s.runtime.deniedLDAPPolicies.Store(revision, s.runtime.epoch.Add(1))
	}
}
func ldapRuntimeBindingKey(id string, birth *time.Time) string {
	if id == "" || birth == nil || birth.IsZero() {
		return ""
	}
	return ldapDigest("ldap.runtime.binding.v1", id, birth.UTC())
}
func (s *Service) invalidateRuntimeLDAPBinding(id string, birth time.Time) {
	if s.runtime != nil {
		if key := ldapRuntimeBindingKey(id, &birth); key != "" {
			s.runtime.deniedLDAPBindings.Store(key, s.runtime.epoch.Add(1))
		}
	}
}
func (s *Service) runtimeLDAPSessionDenied(session runtimeTeamSession) bool {
	return session.LDAPPolicyRevision != "" && runtimeDenied(&s.runtime.deniedLDAPPolicies, session.LDAPPolicyRevision) || session.LDAPBindingKey != "" && runtimeDenied(&s.runtime.deniedLDAPBindings, session.LDAPBindingKey)
}

func loadLDAPSessionRuntimeData(tx *gorm.DB, data *teamSessionRuntimeData) error {
	needed := false
	for _, row := range data.Sessions {
		if row.PrimaryMethod == "ldap" {
			needed = true
			break
		}
	}
	if !needed {
		return nil
	}
	var p entity.LDAPProvider
	err := tx.Session(&gorm.Session{NewDB: true}).Select("id", "config_revision", "policy_revision", "endpoint", "identity_attribute", "enabled", "verified_config_revision", "verified_by", "verified_user_created_at", "verified_binding_id", "verified_binding_created_at").Where(database.ExactText(tx, clause.Column{Name: "id"}, "ldap")).Take(&p).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	data.LDAPProvider = &p
	if !p.Enabled || !ldapVerified(p) {
		return nil
	}
	return tx.Session(&gorm.Session{NewDB: true}).Select("id", "provider_id", "identity_attribute", "user_id", "user_created_at", "config_revision", "subject", "subject_digest", "created_at").Where("config_revision = ?", p.ConfigRevision).Find(&data.LDAPBindings).Error
}

func ldapRuntimePrimary(row entity.Session, p *entity.LDAPProvider, bindings map[string]entity.LDAPBinding, users map[string]entity.User) bool {
	if row.PrimaryMethod == "" {
		return row.LDAPBindingID == "" && row.LDAPBindingCreatedAt == nil && row.LDAPConfigRevision == "" && row.LDAPPolicyRevision == "" && row.LDAPUserCreatedAt == nil
	}
	if row.PrimaryMethod != "ldap" || p == nil || p.ID != "ldap" || !p.Enabled || !ldapVerified(*p) || !validMemberRoleDigest(p.ConfigRevision) || !validMemberRoleDigest(p.PolicyRevision) || (p.Endpoint == "" || (p.IdentityAttribute != "entryUUID" && p.IdentityAttribute != "objectGUID")) || row.LDAPConfigRevision != p.ConfigRevision || row.LDAPPolicyRevision != p.PolicyRevision || row.LDAPBindingCreatedAt == nil || row.LDAPUserCreatedAt == nil {
		return false
	}
	b, ok := bindings[row.LDAPBindingID]
	u, exists := users[row.UserID]
	return ok && exists && b.ID == row.LDAPBindingID && b.UserID == row.UserID && b.UserCreatedAt.Equal(u.CreatedAt) && b.UserCreatedAt.Equal(*row.LDAPUserCreatedAt) && b.CreatedAt.Equal(*row.LDAPBindingCreatedAt) && !b.CreatedAt.IsZero() && b.ConfigRevision == p.ConfigRevision && b.ProviderID == p.ID && b.IdentityAttribute == p.IdentityAttribute && ldapStoredSubject(b) && b.SubjectDigest == ldapSubjectDigest(b.IdentityAttribute, b.Subject)
}
