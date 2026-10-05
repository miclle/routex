package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"slices"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"gorm.io/gorm"
)

type RegistrationStatus struct {
	AllowedEmailDomains []string `json:"allowed_email_domains"`
	Enabled             bool     `json:"enabled"`
	ApprovalRequired    bool     `json:"approval_required"`
}
type RegistrationPolicy struct {
	AllowedEmailDomains []string `json:"allowed_email_domains"`
	Enabled             bool     `json:"enabled"`
	ApprovalRequired    bool     `json:"approval_required"`
	ReviewETag          string   `json:"review_etag"`
}
type RegistrationPolicyInput struct {
	AllowedEmailDomains []string `json:"allowed_email_domains"`
	Enabled             bool     `json:"enabled"`
	ApprovalRequired    bool     `json:"approval_required"`
	Reason              string   `json:"reason"`
}
type RegistrationPolicyResult struct {
	AllowedEmailDomains []string `json:"allowed_email_domains"`
	Confirmation        string   `json:"confirmation"`
	Enabled             bool     `json:"enabled"`
	ApprovalRequired    bool     `json:"approval_required"`
	ReviewETag          string   `json:"review_etag"`
}

func (in *RegistrationPolicyInput) UnmarshalJSON(raw []byte) error {
	f, err := registrationStrictObject(raw, []string{"enabled", "approval_required", "allowed_email_domains", "reason"})
	if err != nil {
		return err
	}
	var n RegistrationPolicyInput
	if json.Unmarshal(f["enabled"], &n.Enabled) != nil || json.Unmarshal(f["approval_required"], &n.ApprovalRequired) != nil || json.Unmarshal(f["allowed_email_domains"], &n.AllowedEmailDomains) != nil || json.Unmarshal(f["reason"], &n.Reason) != nil || !validRegistrationReason(n.Reason) {
		return apperrors.ErrBadRequest
	}
	domains, err := canonicalRegistrationDomains(n.AllowedEmailDomains)
	if err != nil {
		return err
	}
	n.AllowedEmailDomains = domains
	*in = n
	return nil
}
func registrationPolicyETag(v entity.GovernanceSetting) string {
	b, _ := json.Marshal([]any{"registration.policy.v2", v.RegistrationPolicyRevision, v.RegistrationEnabled, v.RegistrationApprovalRequired, v.RegistrationAllowedEmailDomains})
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func registrationPolicyAuthority(tx *gorm.DB, actorID string) error {
	if err := activePlatformAdmin(tx.Session(&gorm.Session{NewDB: true}), actorID); err != nil {
		return err
	}
	return authorizeGovernance(tx.Session(&gorm.Session{NewDB: true}), actorID, "registration.write")
}
func (s *Service) PublicRegistrationStatus(ctx context.Context) (*RegistrationStatus, error) {
	var setting entity.GovernanceSetting
	if err := s.authDB(ctx).Take(&setting, 1).Error; err != nil {
		return nil, catalogError(err)
	}
	domains, err := registrationStoredDomains(setting.RegistrationAllowedEmailDomains)
	if err != nil {
		return nil, err
	}
	initialized, err := s.Initialized(ctx)
	if err != nil {
		return nil, err
	}
	if !setting.RegistrationEnabled || !initialized {
		domains = []string{}
	}
	return &RegistrationStatus{AllowedEmailDomains: domains, Enabled: setting.RegistrationEnabled && initialized, ApprovalRequired: setting.RegistrationApprovalRequired}, nil
}
func (s *Service) GetRegistrationPolicy(ctx context.Context, actorID string) (*RegistrationPolicy, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var result *RegistrationPolicy
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := registrationPolicyAuthority(tx, actorID); err != nil {
			return err
		}
		var v entity.GovernanceSetting
		if err := tx.Session(&gorm.Session{NewDB: true}).Take(&v, 1).Error; err != nil {
			return err
		}
		if !validMemberRoleDigest(v.RegistrationPolicyRevision) {
			return registrationApprovalUnavailable
		}
		domains, err := registrationStoredDomains(v.RegistrationAllowedEmailDomains)
		if err != nil {
			return err
		}
		result = &RegistrationPolicy{AllowedEmailDomains: domains, Enabled: v.RegistrationEnabled, ApprovalRequired: v.RegistrationApprovalRequired, ReviewETag: registrationPolicyETag(v)}
		return ctx.Err()
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return result, registrationApprovalError(err)
}
func (s *Service) mutateRegistrationPolicy(ctx context.Context, actorID, etag string, in RegistrationPolicyInput, reviewed bool) error {
	return s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		if err := registrationPolicyAuthority(tx, actorID); err != nil {
			return err
		}
		var v entity.GovernanceSetting
		if err := tx.Session(&gorm.Session{NewDB: true}).Take(&v, 1).Error; err != nil {
			return err
		}
		if !validMemberRoleDigest(v.RegistrationPolicyRevision) {
			return registrationApprovalUnavailable
		}
		domains, err := registrationStoredDomains(v.RegistrationAllowedEmailDomains)
		if err != nil {
			return err
		}
		if v.RegistrationEnabled == in.Enabled && v.RegistrationApprovalRequired == in.ApprovalRequired && slices.Equal(domains, in.AllowedEmailDomains) {
			return nil
		}
		if reviewed && etag != registrationPolicyETag(v) {
			return catalogConflict
		}
		revision, err := newMemberRoleRevision()
		if err != nil {
			return err
		}
		if err := tx.Session(&gorm.Session{NewDB: true}).Model(&entity.GovernanceSetting{}).Where("id = ?", 1).UpdateColumns(map[string]any{"RegistrationEnabled": in.Enabled, "RegistrationApprovalRequired": in.ApprovalRequired, "RegistrationAllowedEmailDomains": registrationDomainsJSON(in.AllowedEmailDomains), "RegistrationPolicyRevision": revision}).Error; err != nil {
			return err
		}
		return appendRegistrationPolicyAudit(tx, actorID, v, in)
	})
}
func (s *Service) SetRegistrationPolicy(ctx context.Context, actorID, etag string, in RegistrationPolicyInput) (*RegistrationPolicyResult, error) {
	if !validMemberRoleDigest(etag) || !validRegistrationReason(in.Reason) {
		return nil, apperrors.ErrBadRequest
	}
	domains, err := canonicalRegistrationDomains(in.AllowedEmailDomains)
	if err != nil {
		return nil, err
	}
	in.AllowedEmailDomains = domains
	if err := s.mutateRegistrationPolicy(ctx, actorID, etag, in, true); err != nil {
		return nil, registrationApprovalError(err)
	}
	v, err := s.GetRegistrationPolicy(ctx, actorID)
	if err != nil {
		return nil, err
	}
	if v.Enabled != in.Enabled || v.ApprovalRequired != in.ApprovalRequired || !slices.Equal(v.AllowedEmailDomains, in.AllowedEmailDomains) {
		return nil, catalogConflict
	}
	return &RegistrationPolicyResult{AllowedEmailDomains: slices.Clone(v.AllowedEmailDomains), Confirmation: "current_registration_policy", Enabled: v.Enabled, ApprovalRequired: v.ApprovalRequired, ReviewETag: v.ReviewETag}, nil
}

// Trusted compatibility adapter shares serialization, preserves approval policy,
// and cannot create a parallel unreviewed HTTP writer.
func (s *Service) setRegistrationEnabledLegacy(ctx context.Context, actorID string, enabled bool) error {
	return registrationApprovalError(s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		if err := registrationPolicyAuthority(tx, actorID); err != nil {
			return err
		}
		var v entity.GovernanceSetting
		if err := tx.Session(&gorm.Session{NewDB: true}).Take(&v, 1).Error; err != nil {
			return err
		}
		if _, err := registrationStoredDomains(v.RegistrationAllowedEmailDomains); err != nil {
			return err
		}
		if v.RegistrationEnabled == enabled {
			return nil
		}
		revision, err := newMemberRoleRevision()
		if err != nil {
			return err
		}
		if err := tx.Session(&gorm.Session{NewDB: true}).Model(&entity.GovernanceSetting{}).Where("id = ?", 1).UpdateColumns(map[string]any{"RegistrationEnabled": enabled, "RegistrationPolicyRevision": revision}).Error; err != nil {
			return err
		}
		return appendAudit(tx, actorID, "registration.update", "installation", "1")
	}))
}
