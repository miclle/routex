package service

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/id"
	"github.com/miclle/routex/pkg/limits"
)

var errLimitConflict = &apperrors.Error{Code: 409, Message: "resource limit policy changed"}

type LimitTarget struct{ Kind, ID, ProjectID, TeamID string }
type LimitInput struct {
	limits.Policy
	Reason string `json:"reason"`
}
type EffectiveLimitValues struct {
	Tokens5H    *int64  `json:"tokens_5h"`
	Tokens7D    *int64  `json:"tokens_7d"`
	TokensMonth *int64  `json:"tokens_month"`
	TPM         *int64  `json:"tpm"`
	MoneyMonth  *string `json:"money_month"`
	Currency    string  `json:"currency"`
	RPM         *int64  `json:"rpm"`
	Concurrency *int64  `json:"concurrency"`
}
type LimitRecord struct {
	TeamID           string               `json:"team_id,omitempty"`
	EditableFields   []string             `json:"editable_fields,omitempty"`
	PlatformCurrency string               `json:"platform_currency"`
	QuotaUsage       *QuotaUsageRecord    `json:"quota_usage"`
	Kind             string               `json:"kind"`
	ID               string               `json:"id"`
	AccountID        string               `json:"account_id"`
	ETag             string               `json:"etag"`
	Stored           limits.Policy        `json:"stored"`
	Effective        EffectiveLimitValues `json:"effective"`
	IPPolicies       []limits.Policy      `json:"ip_policies"`
	ParentETag       string               `json:"parent_etag,omitempty"`
	RPMUsed          *int64               `json:"rpm_used"`
	Active           *int64               `json:"active"`
	Enforced         bool                 `json:"enforced"`
}
type resolvedLimitTarget struct{ kind, id, parentKind, parentID, teamID, userID string }

func policyFromRow(row entity.ResourceLimit) (limits.Policy, error) {
	policy := limits.Policy{Tokens5H: row.Tokens5H, Tokens7D: row.Tokens7D, TokensMonth: row.TokensMonth, TPM: row.TPM, MoneyMonth: row.MoneyMonth, Currency: row.Currency, RPM: row.RPM, Concurrency: row.Concurrency, IPMode: row.IPMode}
	if row.IPRangesJSON != "" {
		if json.Unmarshal([]byte(row.IPRangesJSON), &policy.IPRanges) != nil {
			return policy, limits.ErrInvalid
		}
	}
	return limits.Normalize(policy)
}
func readLimitPolicy(db *gorm.DB, kind, scopeID string) (entity.ResourceLimit, limits.Policy, error) {
	row := entity.ResourceLimit{ScopeKind: kind, ScopeID: scopeID, ETag: "0"}
	err := db.First(&row, "scope_kind = ? AND scope_id = ?", kind, scopeID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		err = nil
	}
	if err != nil {
		return row, limits.Policy{}, err
	}
	policy, err := policyFromRow(row)
	return row, policy, err
}
func resolveLimitTarget(db *gorm.DB, actor string, target LimitTarget, write bool) (resolvedLimitTarget, error) {
	if target.Kind == "team" || target.Kind == "team_member" {
		userID := ""
		if target.Kind == "team_member" {
			userID = target.ID
		}
		current, err := loadTeamLimitContext(db, actor, target.TeamID, userID, write)
		if err != nil {
			return resolvedLimitTarget{}, err
		}
		return current.Resolved, nil
	}

	permissions, err := permissionsFor(db, actor)
	if err != nil {
		return resolvedLimitTarget{}, err
	}
	can := func(value string) bool { return slices.Contains(permissions, value) }
	resolved := resolvedLimitTarget{kind: target.Kind, id: target.ID}
	switch target.Kind {
	case "user":
		if (write && !can("limits.users.write")) || (!write && actor != target.ID && !can("members.read") && !can("limits.users.write")) {
			return resolved, apperrors.ErrForbidden
		}
		var user entity.User
		if err := db.First(&user, "id = ?", target.ID).Error; err != nil {
			return resolved, err
		}
		if write && user.Disabled {
			return resolved, errLimitConflict
		}
	case "project":
		if write {
			if !can("projects.limits.write") {
				return resolved, apperrors.ErrForbidden
			}
		} else if !can("projects.read_all") && !can("projects.limits.write") {
			ok, err := resourceManager(db, actor, target.ID)
			if err != nil {
				return resolved, err
			}
			if !ok {
				return resolved, apperrors.ErrNotFound
			}
		}
		var project entity.Project
		if err := db.First(&project, "id = ?", target.ID).Error; err != nil {
			return resolved, err
		}
		if write && project.Status != entity.ResourceActive {
			return resolved, errLimitConflict
		}
	case "personal_key":
		var key entity.APIKey
		if err := db.First(&key, "id = ? AND user_id = ?", target.ID, actor).Error; err != nil {
			return resolved, err
		}
		if write && (key.Status == entity.KeyRevoked || key.Status == entity.KeyPending) {
			return resolved, errLimitConflict
		}
		var keys []entity.APIKey
		if err := db.Where("user_id = ?", actor).Find(&keys).Error; err != nil {
			return resolved, err
		}
		roots, err := personalLimitRoots(keys)
		if err != nil {
			return resolved, err
		}
		resolved = resolvedLimitTarget{kind: "key", id: roots[key.ID], parentKind: "user", parentID: actor}
	case "project_key":
		if err := projectKeyAccess(db, actor, target.ProjectID); err != nil {
			return resolved, err
		}
		var key entity.ProjectKey
		if err := db.First(&key, "id = ? AND project_id = ?", target.ID, target.ProjectID).Error; err != nil {
			return resolved, err
		}
		if write && (key.Status == entity.KeyRevoked || key.Status == entity.KeyPending) {
			return resolved, errLimitConflict
		}
		var project entity.Project
		if err := db.First(&project, "id = ?", target.ProjectID).Error; err != nil {
			return resolved, err
		}
		if write && project.Status != entity.ResourceActive {
			return resolved, errLimitConflict
		}
		var keys []entity.ProjectKey
		if err := db.Where("project_id = ?", target.ProjectID).Find(&keys).Error; err != nil {
			return resolved, err
		}
		roots, err := projectLimitRoots(keys)
		if err != nil {
			return resolved, err
		}
		resolved = resolvedLimitTarget{kind: "key", id: roots[key.ID], parentKind: "project", parentID: target.ProjectID}
	default:
		return resolved, apperrors.ErrBadRequest
	}
	return resolved, nil
}
func (s *Service) GetResourceLimit(ctx context.Context, actor string, target LimitTarget) (*LimitRecord, error) {
	var result *LimitRecord
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		resolved, err := resolveLimitTarget(tx, actor, target, false)
		if err != nil {
			return err
		}
		result, err = s.resourceLimitRecord(tx, target, resolved)
		return err
	})
	return result, catalogError(err)
}
func (s *Service) resourceLimitRecord(db *gorm.DB, target LimitTarget, resolved resolvedLimitTarget) (*LimitRecord, error) {
	read := readLimitPolicy
	if resolved.kind == "user" {
		read = readDefaultResourceLimitPolicy
	}
	if resolved.kind == "team" || resolved.kind == "team_member" {
		read = readTeamLimitPolicy
	}
	row, stored, err := read(db, resolved.kind, resolved.id)
	if err != nil {
		return nil, err
	}
	result := &LimitRecord{Kind: target.Kind, ID: target.ID, AccountID: limitAccount(resolved.kind, resolved.id), ETag: row.ETag, Stored: stored, Effective: effectiveQuotaValues(stored, limits.Policy{}), IPPolicies: []limits.Policy{stored}, Enforced: s.recorder != nil && s.runtime != nil && s.RuntimeStatus().Ready}
	// Resource readers need the denomination for a new budget, even when the
	// local policy is unlimited. Do not expose the protected price catalogue.
	var pricingSetting entity.PricingSetting
	if err := db.Select("platform_currency").First(&pricingSetting, 1).Error; err != nil {
		return nil, err
	}
	result.PlatformCurrency = pricingSetting.PlatformCurrency
	if resolved.parentKind != "" {
		parentRow, parent, err := read(db, resolved.parentKind, resolved.parentID)
		if err != nil {
			return nil, err
		}
		result.ParentETag = parentRow.ETag
		result.Effective = effectiveQuotaValues(stored, parent)
		result.IPPolicies = []limits.Policy{parent, stored}
	}
	if result.Enforced {
		auth := s.runtime.auth.Load()
		if auth == nil {
			result.Enforced = false
		} else {
			accounts := []string{result.AccountID}
			policies := []limits.Policy{stored}
			if resolved.parentKind != "" {
				accounts = append(accounts, limitAccount(resolved.parentKind, resolved.parentID))
				policies = append(policies, result.IPPolicies[0])
			}
			for index, account := range accounts {
				published, err := limits.Normalize(auth.LimitPolicies[account])
				if err != nil || runtimeDenied(&s.runtime.deniedLimits, account) || !reflect.DeepEqual(published, policies[index]) {
					result.Enforced = false
				}
			}
		}
	}
	if s.recorder != nil {
		rpm, active, err := s.recorder.queue.AccountUsage(result.AccountID, time.Now())
		if err != nil {
			return nil, err
		}
		result.RPMUsed = &rpm
		result.Active = &active
		quota, err := s.resourceQuotaUsage(db, resolved)
		if err != nil {
			return nil, err
		}
		result.QuotaUsage = quota
	}
	return result, nil
}
func (s *Service) SetResourceLimit(ctx context.Context, actor string, target LimitTarget, etag string, input LimitInput) (*LimitRecord, error) {
	policy, err := limits.Normalize(input.Policy)
	reason := strings.TrimSpace(input.Reason)
	if err != nil || etag == "" || len(reason) == 0 || len(reason) > 2000 {
		return nil, apperrors.ErrBadRequest
	}
	// Serialize publication against the final admission check, not the earlier
	// authentication lookup. A response cannot acknowledge a reduction while an
	// admission can still reserve under the former policy.
	s.limitMu.Lock()
	defer s.limitMu.Unlock()
	var resolved resolvedLimitTarget
	err = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		var err error
		resolved, err = resolveLimitTarget(tx, actor, target, true)
		if err != nil {
			return err
		}
		row, before, err := readLimitPolicy(tx, resolved.kind, resolved.id)
		if err != nil {
			return err
		}
		if row.ETag != etag {
			if row.AppliedDefaultETag == nil && row.DefaultResetETag == nil && row.PreviousETag == etag && row.ActorID == actor && row.Reason == reason && reflect.DeepEqual(before, policy) {
				return nil
			}
			return errLimitConflict
		}
		if policy.MoneyMonth != nil {
			var setting entity.PricingSetting
			if err := tx.First(&setting, 1).Error; err != nil {
				return err
			}
			if policy.Currency != setting.PlatformCurrency {
				return apperrors.ErrBadRequest
			}
		}
		if resolved.parentKind != "" {
			_, parent, err := readLimitPolicy(tx, resolved.parentKind, resolved.parentID)
			if err != nil {
				return err
			}
			if !limits.Narrower(parent, policy) {
				return apperrors.ErrBadRequest
			}
		}
		_, err = persistResourceLimitPolicy(tx, actor, resolved, row, before, policy, reason)
		return err
	})
	if err != nil {
		return nil, catalogError(err)
	}
	s.denyLimitScope(resolved.kind, resolved.id)
	if err := s.RefreshRuntime(ctx); err != nil {
		return nil, err
	}
	return s.GetResourceLimit(ctx, actor, target)
}
func limitAccount(kind, scopeID string) string { return kind + "_" + scopeID }

// persistResourceLimitPolicy saves one normalized full policy and its audit.
// Callers own admission/governance locks and validate the current revision.
func persistResourceLimitPolicy(tx *gorm.DB, actor string, resolved resolvedLimitTarget, row entity.ResourceLimit, before, policy limits.Policy, reason string) (entity.ResourceLimit, error) {
	return persistResourceLimitPolicyWithDefault(tx, actor, resolved, row, before, policy, reason, nil)
}

type defaultLimitProvenance struct{ RuleETag, ReviewETag string }

func persistResourceLimitPolicyWithDefault(tx *gorm.DB, actor string, resolved resolvedLimitTarget, row entity.ResourceLimit, before, policy limits.Policy, reason string, provenance *defaultLimitProvenance) (entity.ResourceLimit, error) {
	revision, err := id.NewPrefixed("lim")
	if err != nil {
		return row, err
	}
	ranges, err := json.Marshal(policy.IPRanges)
	if err != nil {
		return row, err
	}
	row = entity.ResourceLimit{ScopeKind: resolved.kind, ScopeID: resolved.id, ETag: revision, PreviousETag: row.ETag, ActorID: actor, Reason: reason, Tokens5H: policy.Tokens5H, Tokens7D: policy.Tokens7D, TokensMonth: policy.TokensMonth, TPM: policy.TPM, MoneyMonth: policy.MoneyMonth, Currency: policy.Currency, RPM: policy.RPM, Concurrency: policy.Concurrency, IPMode: policy.IPMode, IPRangesJSON: string(ranges)}
	if provenance != nil {
		row.AppliedDefaultETag = &provenance.RuleETag
		row.DefaultResetETag = &provenance.ReviewETag
	}
	if err := tx.Save(&row).Error; err != nil {
		return row, err
	}
	if provenance != nil {
		return row, appendDefaultLimitAudit(tx, actor, "limits.default.reset", resolved.kind, resolved.id, map[string]any{"before": before, "after": policy, "default_rule_etag": provenance.RuleETag, "reset_review_etag": provenance.ReviewETag, "reason": reason})
	}
	auditID, err := id.NewPrefixed("aud")
	if err != nil {
		return row, err
	}
	details, err := json.Marshal(struct {
		TeamID        string `json:"team_id,omitempty"`
		Before, After limits.Policy
		Reason, ETag  string
	}{resolved.teamID, before, policy, reason, revision})
	if err != nil {
		return row, err
	}
	encoded := string(details)
	resourceID := resolved.id
	if resolved.kind == "team_member" {
		resourceID = resolved.userID
	}
	return row, tx.Create(&entity.AuditEvent{ID: auditID, ActorID: actor, Action: "limits.update", ResourceType: resolved.kind, ResourceID: resourceID, DetailsJSON: &encoded}).Error
}
