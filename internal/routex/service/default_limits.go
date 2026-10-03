package service

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"reflect"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/id"
	"github.com/miclle/routex/pkg/limits"
	"github.com/miclle/routex/pkg/secret"
)

type DefaultLimitRecord struct {
	Kind             string               `json:"kind"`
	RuleETag         string               `json:"rule_etag"`
	ETag             string               `json:"etag"`
	Policy           EffectiveLimitValues `json:"policy"`
	PlatformCurrency string               `json:"platform_currency"`
	Editable         bool                 `json:"editable"`
	UpdatedAt        time.Time            `json:"updated_at"`
}

type DefaultLimitInput struct {
	Policy EffectiveLimitValues `json:"policy"`
	Reason string               `json:"reason"`
}

func decodeDefaultLimitObject(raw []byte, fields []string) (map[string]json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return nil, apperrors.ErrBadRequest
	}
	allowed := make(map[string]bool, len(fields))
	for _, field := range fields {
		allowed[field] = true
	}
	result := make(map[string]json.RawMessage, len(fields))
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil, apperrors.ErrBadRequest
		}
		field, ok := token.(string)
		if !ok || !allowed[field] {
			return nil, apperrors.ErrBadRequest
		}
		if _, duplicate := result[field]; duplicate {
			return nil, apperrors.ErrBadRequest
		}
		var value json.RawMessage
		if decoder.Decode(&value) != nil {
			return nil, apperrors.ErrBadRequest
		}
		result[field] = value
	}
	if _, err := decoder.Token(); err != nil {
		return nil, apperrors.ErrBadRequest
	}
	if _, err := decoder.Token(); err != io.EOF || len(result) != len(fields) {
		return nil, apperrors.ErrBadRequest
	}
	return result, nil
}

func (input *DefaultLimitInput) UnmarshalJSON(raw []byte) error {
	fields, err := decodeDefaultLimitObject(raw, []string{"policy", "reason"})
	if err != nil {
		return err
	}
	policyFields := append(append([]string(nil), teamLimitFields...), "currency")
	policy, err := decodeDefaultLimitObject(fields["policy"], policyFields)
	if err != nil || bytes.Equal(bytes.TrimSpace(fields["reason"]), []byte("null")) || bytes.Equal(bytes.TrimSpace(policy["currency"]), []byte("null")) {
		return apperrors.ErrBadRequest
	}
	var value DefaultLimitInput
	if json.Unmarshal(fields["reason"], &value.Reason) != nil || json.Unmarshal(fields["policy"], &value.Policy) != nil {
		return apperrors.ErrBadRequest
	}
	*input = value
	return nil
}

func defaultLimitKind(kind string) bool { return kind == "user" || kind == "team" }

func defaultLimitPolicy(value EffectiveLimitValues) (limits.Policy, error) {
	policy := limits.Policy{Tokens5H: value.Tokens5H, Tokens7D: value.Tokens7D, TokensMonth: value.TokensMonth, TPM: value.TPM, MoneyMonth: value.MoneyMonth, Currency: value.Currency, RPM: value.RPM, Concurrency: value.Concurrency, IPMode: "none"}
	if value.MoneyMonth == nil && value.Currency != "" {
		return limits.Policy{}, apperrors.ErrBadRequest
	}
	normalized, err := limits.Normalize(policy)
	if err != nil {
		return limits.Policy{}, apperrors.ErrBadRequest
	}
	return normalized, nil
}

func readDefaultLimitRule(tx *gorm.DB, kind string, lock bool) (entity.DefaultLimitRule, limits.Policy, error) {
	var row entity.DefaultLimitRule
	query := tx.Where(database.ExactText(tx, clause.Column{Name: "kind"}, kind))
	if lock {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	if err := query.First(&row).Error; err != nil {
		return row, limits.Policy{}, err
	}
	if row.Kind != kind || !defaultLimitKind(row.Kind) || !teamSessionDigest.MatchString(row.RuleETag) {
		return row, limits.Policy{}, apperrors.ErrInternal
	}
	policy, err := defaultLimitPolicy(EffectiveLimitValues{Tokens5H: row.Tokens5H, Tokens7D: row.Tokens7D, TokensMonth: row.TokensMonth, TPM: row.TPM, MoneyMonth: row.MoneyMonth, Currency: row.Currency, RPM: row.RPM, Concurrency: row.Concurrency})
	return row, policy, err
}

func defaultLimitReviewETag(row entity.DefaultLimitRule, policy limits.Policy, pricing entity.PricingSetting) (string, error) {
	raw, err := json.Marshal(struct {
		Domain, Kind, Revision, PricingRevision, Currency string
		Policy                                            EffectiveLimitValues
	}{"routex.default-limits.review", row.Kind, row.RuleETag, pricing.ETag, pricing.PlatformCurrency, effectiveQuotaValues(policy, limits.Policy{})})
	if err != nil {
		return "", err
	}
	return secret.SHA256Hex(string(raw)), nil
}

func defaultLimitRecord(row entity.DefaultLimitRule, policy limits.Policy, pricing entity.PricingSetting, editable bool) (*DefaultLimitRecord, error) {
	etag, err := defaultLimitReviewETag(row, policy, pricing)
	if err != nil {
		return nil, err
	}
	return &DefaultLimitRecord{Kind: row.Kind, RuleETag: row.RuleETag, ETag: etag, Policy: effectiveQuotaValues(policy, limits.Policy{}), PlatformCurrency: pricing.PlatformCurrency, Editable: editable, UpdatedAt: row.UpdatedAt.UTC()}, nil
}

func defaultLimitAccess(tx *gorm.DB, actorID string, write bool) (bool, error) {
	actor, err := exactEnabledActor(tx, actorID)
	if err != nil {
		return false, err
	}
	editable, err := exactGovernancePermission(tx, actor, "limits.settings.write")
	if err != nil {
		return false, err
	}
	if editable {
		return true, nil
	}
	if !write {
		readable, err := exactGovernancePermission(tx, actor, "system.read")
		if err != nil || readable {
			return false, err
		}
	}
	return false, apperrors.ErrForbidden
}

func (s *Service) GetDefaultLimit(ctx context.Context, actorID, kind string) (*DefaultLimitRecord, error) {
	if !defaultLimitKind(kind) {
		return nil, apperrors.ErrBadRequest
	}
	var result *DefaultLimitRecord
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		editable, err := defaultLimitAccess(tx, actorID, false)
		if err != nil {
			return err
		}
		row, policy, err := readDefaultLimitRule(tx, kind, false)
		if err != nil {
			return err
		}
		var pricing entity.PricingSetting
		if err := tx.First(&pricing, 1).Error; err != nil {
			return err
		}
		result, err = defaultLimitRecord(row, policy, pricing, editable)
		return err
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return result, catalogError(err)
}

func (s *Service) SetDefaultLimit(ctx context.Context, actorID, kind, etag string, input DefaultLimitInput) (*DefaultLimitRecord, error) {
	policy, err := defaultLimitPolicy(input.Policy)
	reason := strings.TrimSpace(input.Reason)
	if err != nil || !defaultLimitKind(kind) || !teamSessionDigest.MatchString(etag) || !validCredentialMetadataReason(reason) {
		return nil, apperrors.ErrBadRequest
	}
	var result *DefaultLimitRecord
	err = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		if _, err := defaultLimitAccess(tx, actorID, true); err != nil {
			return err
		}
		pricing, err := lockPricing(tx)
		if err != nil {
			return err
		}
		row, before, err := readDefaultLimitRule(tx, kind, true)
		if err != nil {
			return err
		}
		review, err := defaultLimitReviewETag(row, before, pricing)
		if err != nil {
			return err
		}
		if review != etag {
			if row.PreviousETag == nil || *row.PreviousETag != etag || row.ActorID != actorID || row.Reason != reason || !reflect.DeepEqual(before, policy) {
				return errLimitConflict
			}
			result, err = defaultLimitRecord(row, before, pricing, true)
			return err
		}
		if policy.MoneyMonth != nil && policy.Currency != pricing.PlatformCurrency {
			return errLimitConflict
		}
		if reflect.DeepEqual(before, policy) {
			result, err = defaultLimitRecord(row, before, pricing, true)
			return err
		}
		revision, err := secret.RandomURLSafe(32)
		if err != nil {
			return err
		}
		row = entity.DefaultLimitRule{Kind: kind, Tokens5H: policy.Tokens5H, Tokens7D: policy.Tokens7D, TokensMonth: policy.TokensMonth, TPM: policy.TPM, MoneyMonth: policy.MoneyMonth, Currency: policy.Currency, RPM: policy.RPM, Concurrency: policy.Concurrency, RuleETag: secret.SHA256Hex(revision), PreviousETag: &etag, ActorID: actorID, Reason: reason}
		if err := tx.Save(&row).Error; err != nil {
			return err
		}
		if err := appendDefaultLimitAudit(tx, actorID, "limits.defaults.update", "default_limit", kind, map[string]any{"before": effectiveQuotaValues(before, limits.Policy{}), "after": effectiveQuotaValues(policy, limits.Policy{}), "rule_etag": row.RuleETag, "reason": reason}); err != nil {
			return err
		}
		result, err = defaultLimitRecord(row, policy, pricing, true)
		return err
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	return result, catalogError(err)
}

// Creation copies the template once. The runtime reads only the saved resource
// policy; subsequent template changes never reinterpret an existing account.
func applyCreationDefaultLimit(tx *gorm.DB, kind, scopeID, actorID string) error {
	if !defaultLimitKind(kind) {
		return apperrors.ErrBadRequest
	}
	row, policy, err := readDefaultLimitRule(tx, kind, false)
	if err != nil {
		return err
	}
	var pricing entity.PricingSetting
	if err := tx.First(&pricing, 1).Error; err != nil {
		return err
	}
	if policy.MoneyMonth != nil && policy.Currency != pricing.PlatformCurrency {
		return errLimitConflict
	}
	revision, err := id.NewPrefixed("lim")
	if err != nil {
		return err
	}
	policyRow := entity.ResourceLimit{ScopeKind: kind, ScopeID: scopeID, ETag: revision, PreviousETag: "0", ActorID: actorID, Reason: "Apply current creation default", Tokens5H: policy.Tokens5H, Tokens7D: policy.Tokens7D, TokensMonth: policy.TokensMonth, TPM: policy.TPM, MoneyMonth: policy.MoneyMonth, Currency: policy.Currency, RPM: policy.RPM, Concurrency: policy.Concurrency, IPMode: "none", IPRangesJSON: "[]", AppliedDefaultETag: &row.RuleETag}
	if err := tx.Create(&policyRow).Error; err != nil {
		return err
	}
	return appendDefaultLimitAudit(tx, actorID, "limits.default.apply", kind, scopeID, map[string]any{"after": effectiveQuotaValues(policy, limits.Policy{}), "default_rule_etag": row.RuleETag})
}

// appendDefaultLimitAudit stores only the bounded typed default-policy facts.
func appendDefaultLimitAudit(tx *gorm.DB, actor, action, kind, target string, details any) error {
	eventID, err := id.NewPrefixed("aud")
	if err != nil {
		return err
	}
	raw, err := json.Marshal(details)
	if err != nil {
		return err
	}
	encoded := string(raw)
	return tx.Create(&entity.AuditEvent{ID: eventID, ActorID: actor, Action: action, ResourceType: kind, ResourceID: target, DetailsJSON: &encoded}).Error
}
