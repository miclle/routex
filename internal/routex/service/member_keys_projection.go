package service

import (
	"errors"
	"slices"
	"time"

	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/limits"
)

type MemberKeyLimitRecord struct {
	PlatformCurrency string               `json:"platform_currency"`
	QuotaRootID      string               `json:"quota_root_id"`
	Shared           bool                 `json:"shared_rotation_quota"`
	Stored           EffectiveLimitValues `json:"stored"`
	Effective        EffectiveLimitValues `json:"effective"`
	QuotaUsage       *MemberKeyQuotaUsage `json:"quota_usage"`
	RPMUsed          *string              `json:"rpm_used"`
	Active           *string              `json:"active"`
	Enforced         bool                 `json:"enforced"`
}

func (s *Service) memberKeyRecords(tx *gorm.DB, actor, subject entity.User, all []entity.APIKey, roots map[string]string, selected []entity.APIKey) ([]MemberKeyRecord, error) {
	permissions, err := memberKeyPermissions(tx, actor)
	if err != nil {
		return nil, err
	}
	canDisable := slices.Contains(permissions, "members.keys.disable")
	ids := make([]string, 0, len(selected))
	models := make(map[string][]string, len(selected))
	for _, key := range selected {
		ids = append(ids, key.ID)
		models[key.ID] = []string{}
	}
	if len(ids) == 0 {
		return []MemberKeyRecord{}, nil
	}
	var scopes []entity.APIKeyModel
	if err := tx.Where("key_id IN ?", ids).Order("key_id, model_id").Find(&scopes).Error; err != nil {
		return nil, err
	}
	for _, scope := range scopes {
		if !slices.Contains(ids, scope.KeyID) {
			return nil, apperrors.ErrNotFound
		}
		models[scope.KeyID] = append(models[scope.KeyID], scope.ModelID)
	}
	lastUse, complete, err := memberKeyLastUse(tx, subject.ID, ids)
	if err != nil {
		return nil, err
	}
	rootCounts := map[string]int{}
	for _, key := range all {
		rootCounts[roots[key.ID]]++
	}
	policies := map[string]*MemberKeyLimitRecord{}
	result := make([]MemberKeyRecord, 0, len(selected))
	now := time.Now()
	for _, key := range selected {
		root := roots[key.ID]
		policy := policies[root]
		if policy == nil {
			// The real administrative actor was authorized before resolving this
			// exact subject-owned ancestry. Never call an owner API as the subject.
			if err := validateMemberKeyPolicies(tx, root, subject.ID); err != nil {
				return nil, err
			}
			resolved := resolvedLimitTarget{kind: "key", id: root, parentKind: "user", parentID: subject.ID}
			record, err := s.resourceLimitRecord(tx, LimitTarget{Kind: "personal_key", ID: key.ID}, resolved)
			if err != nil {
				return nil, err
			}
			quota, err := memberKeyQuotaUsage(record.QuotaUsage)
			if err != nil {
				return nil, err
			}
			rpm, err := memberKeyCounter(record.RPMUsed)
			if err != nil {
				return nil, err
			}
			active, err := memberKeyCounter(record.Active)
			if err != nil {
				return nil, err
			}
			policy = &MemberKeyLimitRecord{PlatformCurrency: record.PlatformCurrency, QuotaRootID: root, Shared: rootCounts[root] > 1, Stored: effectiveQuotaValues(record.Stored, limits.Policy{}), Effective: record.Effective, QuotaUsage: quota, RPMUsed: rpm, Active: active, Enforced: record.Enforced}
			policies[root] = policy
		}
		coverage := "unknown"
		if complete {
			coverage = "no_recorded_use"
		}
		var latest *time.Time
		if recorded, exists := lastUse[key.ID]; exists {
			value := recorded.UTC()
			latest, coverage = &value, "recorded"
		}
		result = append(result, MemberKeyRecord{ID: key.ID, Name: key.Name, Status: key.Status, Expired: key.ExpiresAt != nil && !now.Before(*key.ExpiresAt), ModelIDs: models[key.ID], ExpiresAt: key.ExpiresAt, CreatedAt: key.CreatedAt, UpdatedAt: key.UpdatedAt, ETag: memberKeyETag(key, models[key.ID]), DisableEligible: canDisable && memberKeyDisableAllowed(actor, subject, key, now), LastUsedAt: latest, LastUseCoverage: coverage, Limits: policy})
	}
	return result, nil
}

func memberKeyLastUse(db *gorm.DB, subjectID string, ids []string) (map[string]time.Time, bool, error) {
	var facts []entity.CallRecord
	if err := db.Select("user_id", "project_id", "key_id", "started_at").Where("user_id = ? AND project_id = ? AND key_id IN ?", subjectID, "", ids).Order("started_at DESC, request_id DESC").Limit(memberKeyReadBound + 1).Find(&facts).Error; err != nil {
		return nil, false, err
	}
	return memberKeyLastUseFacts(facts, subjectID, ids), len(facts) <= memberKeyReadBound, nil
}

func memberKeyLastUseFacts(facts []entity.CallRecord, subjectID string, ids []string) map[string]time.Time {
	result := map[string]time.Time{}
	for _, fact := range facts {
		// SQL collation cannot promote another immutable identity's history.
		if fact.UserID != subjectID || fact.ProjectID != "" || !slices.Contains(ids, fact.KeyID) || fact.StartedAt.IsZero() {
			continue
		}
		if previous, exists := result[fact.KeyID]; !exists || fact.StartedAt.After(previous) {
			result[fact.KeyID] = fact.StartedAt
		}
	}
	return result
}

func validateMemberKeyPolicies(tx *gorm.DB, root, subjectID string) error {
	for _, scope := range [][2]string{{"key", root}, {"user", subjectID}} {
		var row entity.ResourceLimit
		err := tx.Where("scope_kind = ? AND scope_id = ?", scope[0], scope[1]).First(&row).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			continue
		}
		if err != nil {
			return err
		}
		if row.ScopeKind != scope[0] || row.ScopeID != scope[1] {
			return apperrors.ErrNotFound
		}
	}
	return nil
}
