package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/limits"
	"github.com/miclle/routex/pkg/secret"
)

type DefaultLimitResetContext struct {
	Kind               string              `json:"kind"`
	ID                 string              `json:"id"`
	ETag               string              `json:"etag"`
	Limit              *LimitRecord        `json:"limit"`
	DefaultRule        *DefaultLimitRecord `json:"default_rule"`
	AppliedDefaultETag *string             `json:"applied_default_etag"`
	Editable           bool                `json:"editable"`
}
type DefaultLimitResetResult struct {
	Kind               string       `json:"kind"`
	ID                 string       `json:"id"`
	Saved              bool         `json:"saved"`
	Limit              *LimitRecord `json:"limit"`
	AppliedDefaultETag string       `json:"applied_default_etag"`
	DefaultResetETag   string       `json:"default_reset_etag"`
	RuntimeApplied     bool         `json:"runtime_applied"`
}
type DefaultLimitResetInput struct {
	Reason string `json:"reason"`
}

func (input *DefaultLimitResetInput) UnmarshalJSON(raw []byte) error {
	fields, err := decodeDefaultLimitObject(raw, []string{"reason"})
	if err != nil {
		return err
	}
	var reason *string
	if json.Unmarshal(fields["reason"], &reason) != nil || reason == nil {
		return apperrors.ErrBadRequest
	}
	input.Reason = *reason
	return nil
}

type defaultLimitResetState struct {
	Target     LimitTarget
	Resolved   resolvedLimitTarget
	User       entity.User
	Team       *teamLimitContext
	Row        entity.ResourceLimit
	Stored     limits.Policy
	Rule       entity.DefaultLimitRule
	Policy     limits.Policy
	Pricing    entity.PricingSetting
	Editable   bool
	ReviewETag string
}

func defaultResetTarget(target LimitTarget) bool {
	return defaultLimitKind(target.Kind) && target.ID != "" && len(target.ID) <= 30 && target.ProjectID == "" && (target.Kind == "user" && target.TeamID == "" || target.Kind == "team" && (target.TeamID == "" || target.TeamID == target.ID))
}

func loadDefaultLimitReset(tx *gorm.DB, actorID string, target LimitTarget, write bool) (*defaultLimitResetState, error) {
	if !defaultResetTarget(target) {
		return nil, apperrors.ErrBadRequest
	}
	state := &defaultLimitResetState{Target: target, Resolved: resolvedLimitTarget{kind: target.Kind, id: target.ID}}
	if target.Kind == "team" {
		current, err := loadTeamLimitContext(tx, actorID, target.ID, "", write)
		if err != nil {
			return nil, err
		}
		if current.Team.Status != entity.ResourceActive {
			return nil, errLimitConflict
		}
		state.Team, state.Resolved, state.Row, state.Stored, state.Pricing = current, current.Resolved, current.Row, current.Stored, current.Pricing
		state.Editable = len(current.Editable) == len(teamResourceLimitFields)
		if write && !state.Editable {
			return nil, apperrors.ErrForbidden
		}
	} else {
		actor, err := exactEnabledActor(tx, actorID)
		if err != nil {
			return nil, err
		}
		state.Editable, err = exactGovernancePermission(tx, actor, "limits.users.write")
		if err != nil {
			return nil, err
		}
		if write && !state.Editable {
			return nil, apperrors.ErrForbidden
		}
		if !write && !state.Editable && actorID != target.ID {
			readable, err := exactGovernancePermission(tx, actor, "members.read")
			if err != nil {
				return nil, err
			}
			if !readable {
				return nil, apperrors.ErrForbidden
			}
		}
		query := tx.Where(database.ExactText(tx, clause.Column{Name: "id"}, target.ID))
		if write {
			query = query.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		if err := query.First(&state.User).Error; err != nil {
			return nil, err
		}
		if state.User.ID != target.ID {
			return nil, apperrors.ErrNotFound
		}
		if state.User.Disabled || state.User.OffboardedAt != nil {
			return nil, errLimitConflict
		}
		state.Row, state.Stored, err = readDefaultResourceLimitPolicy(tx, "user", target.ID)
		if err != nil {
			return nil, err
		}
		if err := tx.First(&state.Pricing, 1).Error; err != nil {
			return nil, err
		}
	}
	var err error
	state.Rule, state.Policy, err = readDefaultLimitRule(tx, target.Kind, write)
	if err != nil {
		return nil, err
	}
	state.ReviewETag, err = defaultLimitResetETag(state)
	return state, err
}

func defaultLimitResetETag(state *defaultLimitResetState) (string, error) {
	created, updated, status := state.User.CreatedAt, state.User.UpdatedAt, "enabled"
	if state.Team != nil {
		created, updated, status = state.Team.Team.CreatedAt, state.Team.Team.UpdatedAt, state.Team.Team.Status
	}
	raw, err := json.Marshal(struct {
		Domain, Kind, ID, Revision, RuleRevision, PricingRevision, Currency, Status string
		CreatedAt, UpdatedAt                                                        time.Time
		Before                                                                      limits.Policy
		Default                                                                     EffectiveLimitValues
	}{"routex.default-limits.reset", state.Target.Kind, state.Target.ID, state.Row.ETag, state.Rule.RuleETag, state.Pricing.ETag, state.Pricing.PlatformCurrency, status, created.UTC(), updated.UTC(), state.Stored, effectiveQuotaValues(state.Policy, limits.Policy{})})
	if err != nil {
		return "", err
	}
	return secret.SHA256Hex(string(raw)), nil
}

func defaultLimitResetPolicy(state *defaultLimitResetState) (limits.Policy, error) {
	policy := state.Policy
	if state.Target.Kind == "user" {
		policy.IPMode, policy.IPRanges = state.Stored.IPMode, append([]string(nil), state.Stored.IPRanges...)
	}
	if policy.MoneyMonth != nil && policy.Currency != state.Pricing.PlatformCurrency {
		return limits.Policy{}, errLimitConflict
	}
	return limits.Normalize(policy)
}

func (s *Service) defaultLimitResetRecord(tx *gorm.DB, state *defaultLimitResetState) (*LimitRecord, error) {
	var record *LimitRecord
	var err error
	if state.Team != nil {
		return s.teamResourceLimitRecord(tx, state.Team)
	}
	record, err = s.resourceLimitRecord(tx, state.Target, state.Resolved)
	if err != nil {
		return nil, err
	}
	record.Enforced = s.defaultUserLimitApplied(state)
	return record, nil
}
func (s *Service) defaultUserLimitApplied(state *defaultLimitResetState) bool {
	if s.runtime == nil || s.recorder == nil {
		return false
	}
	auth := s.runtime.auth.Load()
	if auth == nil || auth.Quota == nil || !time.Now().Before(auth.ValidUntil) || state.User.Disabled || state.User.OffboardedAt != nil || runtimeDenied(&s.runtime.deniedUsers, state.User.ID) {
		return false
	}
	account := limitAccount("user", state.User.ID)
	revision := auth.Quota.Revisions[account]
	if revision == "" {
		revision = "0"
	}
	policy, err := limits.Normalize(auth.LimitPolicies[account])
	return err == nil && revision == state.Row.ETag && reflect.DeepEqual(policy, state.Stored) && !runtimeDenied(&s.runtime.deniedLimits, account) && !runtimeDenied(&s.runtime.deniedLimits, "quota_settings") && (state.Stored.MoneyMonth == nil || state.Stored.Currency == auth.Quota.Currency && auth.Quota.Currency == state.Pricing.PlatformCurrency)
}
func (s *Service) GetDefaultLimitResetContext(ctx context.Context, actorID string, target LimitTarget) (*DefaultLimitResetContext, error) {
	var result *DefaultLimitResetContext
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		state, err := loadDefaultLimitReset(tx, actorID, target, false)
		if err != nil {
			return err
		}
		record, err := s.defaultLimitResetRecord(tx, state)
		if err != nil {
			return err
		}
		rule, err := defaultLimitRecord(state.Rule, state.Policy, state.Pricing, false)
		if err != nil {
			return err
		}
		result = &DefaultLimitResetContext{Kind: target.Kind, ID: target.ID, ETag: state.ReviewETag, Limit: record, DefaultRule: rule, AppliedDefaultETag: state.Row.AppliedDefaultETag, Editable: state.Editable}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return result, catalogError(err)
}

func defaultLimitResetMatches(row entity.ResourceLimit, actor, reason, review string) bool {
	return row.AppliedDefaultETag != nil && teamSessionDigest.MatchString(*row.AppliedDefaultETag) && row.DefaultResetETag != nil && *row.DefaultResetETag == review && row.ActorID == actor && row.Reason == reason
}

func (s *Service) ResetResourceLimitToDefault(ctx context.Context, actorID string, target LimitTarget, etag, reason string) (*DefaultLimitResetResult, error) {
	reason = strings.TrimSpace(reason)
	if !defaultResetTarget(target) || !teamSessionDigest.MatchString(etag) || !validCredentialMetadataReason(reason) {
		return nil, apperrors.ErrBadRequest
	}
	s.limitMu.Lock()
	defer s.limitMu.Unlock()
	var saved entity.ResourceLimit
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		state, err := loadDefaultLimitReset(tx, actorID, target, true)
		if err != nil {
			return err
		}
		// A known current reset reconciles its frozen copied revision, even when the
		// template has since changed. Ordinary writes clear this last-intent proof.
		if defaultLimitResetMatches(state.Row, actorID, reason, etag) {
			saved = state.Row
			return nil
		}
		if state.ReviewETag != etag {
			return errLimitConflict
		}
		policy, err := defaultLimitResetPolicy(state)
		if err != nil {
			return err
		}
		saved, err = persistResourceLimitPolicyWithDefault(tx, actorID, state.Resolved, state.Row, state.Stored, policy, reason, &defaultLimitProvenance{RuleETag: state.Rule.RuleETag, ReviewETag: etag})
		return err
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, catalogError(err)
	}
	s.denyLimitScope(target.Kind, target.ID)
	if err := s.RefreshRuntime(ctx); err != nil {
		return nil, err
	}
	var result *DefaultLimitResetResult
	err = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		state, err := loadDefaultLimitReset(tx, actorID, target, false)
		if err != nil {
			return err
		}
		if state.Row.ETag != saved.ETag || !defaultLimitResetMatches(state.Row, actorID, reason, etag) {
			return errLimitConflict
		}
		record, err := s.defaultLimitResetRecord(tx, state)
		if err != nil {
			return err
		}
		result = &DefaultLimitResetResult{Kind: target.Kind, ID: target.ID, Saved: true, Limit: record, AppliedDefaultETag: *saved.AppliedDefaultETag, DefaultResetETag: *saved.DefaultResetETag, RuntimeApplied: record.Enforced}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return result, catalogError(err)
}

func readDefaultResourceLimitPolicy(tx *gorm.DB, kind, scopeID string) (entity.ResourceLimit, limits.Policy, error) {
	row := entity.ResourceLimit{ScopeKind: kind, ScopeID: scopeID, ETag: "0"}
	err := tx.Where(database.ExactText(tx, clause.Column{Name: "scope_kind"}, kind)).Where(database.ExactText(tx, clause.Column{Name: "scope_id"}, scopeID)).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		err = nil
	}
	if err != nil {
		return row, limits.Policy{}, err
	}
	if row.ScopeKind != kind || row.ScopeID != scopeID {
		return row, limits.Policy{}, apperrors.ErrInternal
	}
	policy, err := policyFromRow(row)
	return row, policy, err
}
