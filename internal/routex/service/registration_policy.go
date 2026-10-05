package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"gorm.io/gorm"
	"time"
)

type RegistrationStatus struct {
	Enabled          bool `json:"enabled"`
	ApprovalRequired bool `json:"approval_required"`
}
type RegistrationPolicy struct {
	Enabled          bool   `json:"enabled"`
	ApprovalRequired bool   `json:"approval_required"`
	ReviewETag       string `json:"review_etag"`
}
type RegistrationPolicyInput struct {
	Enabled          bool   `json:"enabled"`
	ApprovalRequired bool   `json:"approval_required"`
	Reason           string `json:"reason"`
}
type RegistrationPolicyResult struct {
	Confirmation     string `json:"confirmation"`
	Enabled          bool   `json:"enabled"`
	ApprovalRequired bool   `json:"approval_required"`
	ReviewETag       string `json:"review_etag"`
}

func (in *RegistrationPolicyInput) UnmarshalJSON(raw []byte) error {
	f, err := registrationStrictObject(raw, []string{"enabled", "approval_required", "reason"})
	if err != nil {
		return err
	}
	var n RegistrationPolicyInput
	if json.Unmarshal(f["enabled"], &n.Enabled) != nil || json.Unmarshal(f["approval_required"], &n.ApprovalRequired) != nil || json.Unmarshal(f["reason"], &n.Reason) != nil || !validRegistrationReason(n.Reason) {
		return apperrors.ErrBadRequest
	}
	*in = n
	return nil
}
func registrationPolicyETag(v entity.GovernanceSetting) string {
	b, _ := json.Marshal([]any{"registration.policy.v1", v.RegistrationPolicyRevision, v.RegistrationEnabled, v.RegistrationApprovalRequired})
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
	initialized, err := s.Initialized(ctx)
	if err != nil {
		return nil, err
	}
	return &RegistrationStatus{setting.RegistrationEnabled && initialized, setting.RegistrationApprovalRequired}, nil
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
		result = &RegistrationPolicy{v.RegistrationEnabled, v.RegistrationApprovalRequired, registrationPolicyETag(v)}
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
		if v.RegistrationEnabled == in.Enabled && v.RegistrationApprovalRequired == in.ApprovalRequired {
			return nil
		}
		if reviewed && etag != registrationPolicyETag(v) {
			return catalogConflict
		}
		revision, err := newMemberRoleRevision()
		if err != nil {
			return err
		}
		if err := tx.Session(&gorm.Session{NewDB: true}).Model(&entity.GovernanceSetting{}).Where("id = ?", 1).UpdateColumns(map[string]any{"RegistrationEnabled": in.Enabled, "RegistrationApprovalRequired": in.ApprovalRequired, "RegistrationPolicyRevision": revision}).Error; err != nil {
			return err
		}
		return appendRegistrationPolicyAudit(tx, actorID, v, in)
	})
}
func (s *Service) SetRegistrationPolicy(ctx context.Context, actorID, etag string, in RegistrationPolicyInput) (*RegistrationPolicyResult, error) {
	if !validMemberRoleDigest(etag) || !validRegistrationReason(in.Reason) {
		return nil, apperrors.ErrBadRequest
	}
	if err := s.mutateRegistrationPolicy(ctx, actorID, etag, in, true); err != nil {
		return nil, registrationApprovalError(err)
	}
	v, err := s.GetRegistrationPolicy(ctx, actorID)
	if err != nil {
		return nil, err
	}
	if v.Enabled != in.Enabled || v.ApprovalRequired != in.ApprovalRequired {
		return nil, catalogConflict
	}
	return &RegistrationPolicyResult{"current_registration_policy", v.Enabled, v.ApprovalRequired, v.ReviewETag}, nil
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
