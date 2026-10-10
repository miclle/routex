package service

import (
	"errors"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
)

// OIDC revocation uses the existing bounded epoch fences. Disabling a provider
// must remain possible regardless of the number of retained external Sessions.
func (s *Service) invalidateRuntimeOIDCPolicy(revision string) {
	if s.runtime != nil && revision != "" {
		s.runtime.deniedOIDCPolicies.Store(revision, s.runtime.epoch.Add(1))
	}
}
func oidcRuntimeBindingKey(id string, birth *time.Time) string {
	if id == "" || birth == nil || birth.IsZero() {
		return ""
	}
	return oidcDigest("oidc.runtime.binding.v1", id, birth.UTC())
}
func (s *Service) invalidateRuntimeOIDCBinding(id string, birth time.Time) {
	if s.runtime != nil {
		if key := oidcRuntimeBindingKey(id, &birth); key != "" {
			s.runtime.deniedOIDCBindings.Store(key, s.runtime.epoch.Add(1))
		}
	}
}
func (s *Service) runtimeOIDCSessionDenied(session runtimeTeamSession) bool {
	return session.OIDCPolicyRevision != "" && runtimeDenied(&s.runtime.deniedOIDCPolicies, session.OIDCPolicyRevision) || session.OIDCBindingKey != "" && runtimeDenied(&s.runtime.deniedOIDCBindings, session.OIDCBindingKey)
}

func loadOIDCSessionRuntimeData(tx *gorm.DB, data *teamSessionRuntimeData) error {
	needed := false
	for _, row := range data.Sessions {
		if row.PrimaryMethod == "oidc" {
			needed = true
			break
		}
	}
	if !needed {
		return nil
	}
	var p entity.OIDCProvider
	err := tx.Session(&gorm.Session{NewDB: true}).Select("id", "config_revision", "policy_revision", "issuer", "enabled", "verified_config_revision", "verified_by", "verified_user_created_at", "verified_binding_id", "verified_binding_created_at").Take(&p, "id = ?", "oidc").Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	data.OIDCProvider = &p
	if !p.Enabled || !oidcVerified(p) {
		return nil
	}
	return tx.Session(&gorm.Session{NewDB: true}).Select("id", "user_id", "user_created_at", "config_revision", "subject", "subject_digest", "created_at").Where("config_revision = ?", p.ConfigRevision).Find(&data.OIDCBindings).Error
}

func oidcRuntimePrimary(row entity.Session, p *entity.OIDCProvider, bindings map[string]entity.OIDCBinding, users map[string]entity.User) bool {
	if row.PrimaryMethod == "" {
		return row.OIDCBindingID == "" && row.OIDCBindingCreatedAt == nil && row.OIDCConfigRevision == "" && row.OIDCPolicyRevision == "" && row.OIDCUserCreatedAt == nil
	}
	if row.PrimaryMethod != "oidc" || p == nil || p.ID != "oidc" || !p.Enabled || !oidcVerified(*p) || !validMemberRoleDigest(p.ConfigRevision) || !validMemberRoleDigest(p.PolicyRevision) || p.Issuer == "" || row.OIDCConfigRevision != p.ConfigRevision || row.OIDCPolicyRevision != p.PolicyRevision || row.OIDCBindingCreatedAt == nil || row.OIDCUserCreatedAt == nil {
		return false
	}
	b, ok := bindings[row.OIDCBindingID]
	u, exists := users[row.UserID]
	return ok && exists && b.ID == row.OIDCBindingID && b.UserID == row.UserID && b.UserCreatedAt.Equal(u.CreatedAt) && b.UserCreatedAt.Equal(*row.OIDCUserCreatedAt) && b.CreatedAt.Equal(*row.OIDCBindingCreatedAt) && !b.CreatedAt.IsZero() && b.ConfigRevision == p.ConfigRevision && b.Subject != "" && b.SubjectDigest == oidcSubjectDigest(p.Issuer, b.Subject)
}
