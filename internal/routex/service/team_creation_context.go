package service

import (
	"context"
	"database/sql"
	"slices"
	"strconv"
	"time"

	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/limits"
)

// Only independently editable groups appear; null means a stored unset value,
// never redaction. Preview integers remain decimal strings through browser JSON.
type TeamCreationContext struct {
	ReviewETag       string             `json:"review_etag"`
	DefaultRuleETag  string             `json:"default_rule_etag"`
	PlatformCurrency *string            `json:"platform_currency"`
	EditableFields   []string           `json:"editable_fields"`
	DefaultPolicy    map[string]*string `json:"default_policy"`
}

type teamCreationReview struct {
	Public  TeamCreationContext
	Actor   entity.User
	Pricing entity.PricingSetting
	Rule    entity.DefaultLimitRule
	Policy  limits.Policy
}

func teamCreationReviewHash(actor entity.User, proof runtimeAdmissionProof, rule entity.DefaultLimitRule, policy limits.Policy, pricing entity.PricingSetting, editable []string) string {
	proof.CreatedAt = proof.CreatedAt.UTC()
	proof.ApplicationCreatedAt = proof.ApplicationCreatedAt.UTC()
	return personalHash(struct {
		Domain, Actor, Role, IdentityRevision, DefaultRevision, PricingRevision, Currency string
		Born, Updated                                                                     time.Time
		Admission                                                                         runtimeAdmissionProof
		Editable                                                                          []string
		Policy                                                                            limits.Policy
	}{"routex.team-creation.context.v1", actor.ID, actor.Role, actor.MemberRoleRevision, rule.RuleETag, pricing.ETag, pricing.PlatformCurrency, actor.CreatedAt.UTC(), actor.UpdatedAt.UTC(), proof, slices.Clone(editable), policy})
}

func teamCreationPreview(policy limits.Policy, editable []string) map[string]*string {
	result := map[string]*string{}
	integers := map[string]*int64{"tokens_5h": policy.Tokens5H, "tokens_7d": policy.Tokens7D, "tokens_month": policy.TokensMonth, "rpm": policy.RPM, "tpm": policy.TPM, "concurrency": policy.Concurrency}
	for _, field := range editable {
		if field == "money_month" {
			result[field] = policy.MoneyMonth
			currency := policy.Currency
			result["currency"] = &currency
			continue
		}
		if value := integers[field]; value != nil {
			text := strconv.FormatInt(*value, 10)
			result[field] = &text
		} else {
			result[field] = nil
		}
	}
	return result
}

// The actor is a fresh complete admitted identity in this same transaction.
func loadTeamCreationReview(tx *gorm.DB, actor entity.User, lock bool) (*teamCreationReview, error) {
	allowed, err := exactGovernancePermissionForAdmittedActor(tx, actor, "teams.write")
	if err != nil {
		return nil, err
	}
	if !allowed {
		return nil, apperrors.ErrForbidden
	}
	editable := []string{}
	for _, permission := range []string{"teams.tokens.write", "teams.money.write", "teams.rates.write"} {
		allowed, err := exactGovernancePermissionForAdmittedActor(tx, actor, permission)
		if err != nil {
			return nil, err
		}
		if allowed {
			for _, field := range teamLimitFields {
				if teamLimitPermission(field) == permission {
					editable = append(editable, field)
				}
			}
		}
	}
	// Stable product field order, independent of permission-group enumeration.
	slices.SortFunc(editable, func(a, b string) int { return slices.Index(teamLimitFields, a) - slices.Index(teamLimitFields, b) })
	var pricing entity.PricingSetting
	if lock {
		pricing, err = lockPricing(tx)
	} else {
		err = tx.First(&pricing, 1).Error
	}
	if err != nil {
		return nil, err
	}
	rule, policy, err := readDefaultLimitRule(tx, "team", lock)
	if err != nil {
		return nil, err
	}
	apps, err := loadRegistrationApplications(tx, []entity.User{actor})
	if err != nil {
		return nil, err
	}
	_, proof := registrationAdmission(actor, apps)
	if !proof.Eligible {
		return nil, apperrors.ErrUnauthorized
	}
	public := TeamCreationContext{DefaultRuleETag: rule.RuleETag, EditableFields: editable, DefaultPolicy: teamCreationPreview(policy, editable)}
	if slices.Contains(editable, "money_month") {
		currency := pricing.PlatformCurrency
		public.PlatformCurrency = &currency
	}
	public.ReviewETag = teamCreationReviewHash(actor, proof, rule, policy, pricing, editable)
	return &teamCreationReview{Public: public, Actor: actor, Pricing: pricing, Rule: rule, Policy: policy}, nil
}

func (s *Service) GetTeamCreationContext(ctx context.Context, actorID string) (*TeamCreationContext, error) {
	var result *TeamCreationContext
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		actor, err := exactEnabledActor(tx, actorID)
		if err != nil {
			return err
		}
		review, err := loadTeamCreationReview(tx, actor, false)
		if err != nil {
			return err
		}
		result = &review.Public
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return result, catalogError(err)
}
