package service

import (
	"encoding/json"
	"reflect"
	"regexp"
	"strings"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/limits"
)

var defaultResetAuditReview = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Default-rule facts expose only template fields, never arbitrary audit JSON or IP.
type defaultLimitAuditPolicy struct {
	Tokens5H    *int64  `json:"tokens_5h"`
	Tokens7D    *int64  `json:"tokens_7d"`
	TokensMonth *int64  `json:"tokens_month"`
	TPM         *int64  `json:"tpm"`
	MoneyMonth  *string `json:"money_month"`
	Currency    string  `json:"currency"`
	RPM         *int64  `json:"rpm"`
	Concurrency *int64  `json:"concurrency"`
}

func (p defaultLimitAuditPolicy) valid() bool {
	_, err := limits.Normalize(limits.Policy{Tokens5H: p.Tokens5H, Tokens7D: p.Tokens7D,
		TokensMonth: p.TokensMonth, TPM: p.TPM, MoneyMonth: p.MoneyMonth,
		Currency: p.Currency, RPM: p.RPM, Concurrency: p.Concurrency})
	return err == nil
}

func validDefaultAuditRevision(value string) bool {
	return defaultResetAuditReview.MatchString(value)
}

func defaultLimitAuditProjection(row entity.AuditEvent) (any, bool) {
	if row.DetailsJSON == nil {
		return nil, false
	}
	switch row.Action {
	case "limits.defaults.update":
		var detail struct {
			Before   *defaultLimitAuditPolicy `json:"before"`
			After    *defaultLimitAuditPolicy `json:"after"`
			RuleETag string                   `json:"rule_etag"`
			Reason   string                   `json:"reason"`
		}
		if row.ResourceType != "default_limit" || (row.ResourceID != "user" && row.ResourceID != "team") ||
			json.Unmarshal([]byte(*row.DetailsJSON), &detail) != nil || detail.Before == nil || detail.After == nil ||
			!defaultAuditHasPolicy(*row.DetailsJSON, "before", false) || !defaultAuditHasPolicy(*row.DetailsJSON, "after", false) ||
			!detail.Before.valid() || !detail.After.valid() || !validDefaultAuditRevision(detail.RuleETag) ||
			strings.TrimSpace(detail.Reason) != detail.Reason || !validCredentialMetadataReason(detail.Reason) {
			return nil, false
		}
		return detail, true
	case "limits.default.apply":
		var detail struct {
			After           *defaultLimitAuditPolicy `json:"after"`
			DefaultRuleETag string                   `json:"default_rule_etag"`
		}
		if !validDefaultAuditTarget(row) || json.Unmarshal([]byte(*row.DetailsJSON), &detail) != nil ||
			detail.After == nil || !defaultAuditHasPolicy(*row.DetailsJSON, "after", false) || !detail.After.valid() || !validDefaultAuditRevision(detail.DefaultRuleETag) {
			return nil, false
		}
		return detail, true
	case "limits.default.reset":
		var detail struct {
			Before          *limits.Policy `json:"before"`
			After           *limits.Policy `json:"after"`
			DefaultRuleETag string         `json:"default_rule_etag"`
			ResetReviewETag string         `json:"reset_review_etag"`
			Reason          string         `json:"reason"`
		}
		if !validDefaultAuditTarget(row) || json.Unmarshal([]byte(*row.DetailsJSON), &detail) != nil ||
			detail.Before == nil || detail.After == nil || !defaultAuditHasPolicy(*row.DetailsJSON, "before", true) || !defaultAuditHasPolicy(*row.DetailsJSON, "after", true) || !validDefaultAuditRevision(detail.DefaultRuleETag) ||
			!defaultResetAuditReview.MatchString(detail.ResetReviewETag) ||
			strings.TrimSpace(detail.Reason) != detail.Reason || !validCredentialMetadataReason(detail.Reason) {
			return nil, false
		}
		before, beforeErr := limits.Normalize(*detail.Before)
		after, afterErr := limits.Normalize(*detail.After)
		if beforeErr != nil || afterErr != nil || !validMonthlyBehaviorAudit(row.ResourceType, before) || after.TokensMonthBehavior != "" || after.MoneyMonthBehavior != "" || before.IPMode != after.IPMode || !reflect.DeepEqual(before.IPRanges, after.IPRanges) ||
			row.ResourceType == "team" && (before.IPMode != "none" || after.IPMode != "none") {
			return nil, false
		}
		return detail, true
	}
	return nil, false
}

func validDefaultAuditTarget(row entity.AuditEvent) bool {
	return safeTeamSessionID(row.ResourceID) &&
		(row.ResourceType == "user" && strings.HasPrefix(row.ResourceID, "usr_") ||
			row.ResourceType == "team" && strings.HasPrefix(row.ResourceID, "tea_"))
}

// Missing nullable fields are unknown facts, not an assertion of unlimited policy.
func defaultAuditHasPolicy(raw, field string, includeIP bool) bool {
	var envelope map[string]json.RawMessage
	if json.Unmarshal([]byte(raw), &envelope) != nil {
		return false
	}
	var policy map[string]json.RawMessage
	if json.Unmarshal(envelope[field], &policy) != nil {
		return false
	}
	fields := []string{"tokens_5h", "tokens_7d", "tokens_month", "tpm", "money_month", "currency", "rpm", "concurrency"}
	if includeIP {
		fields = append(fields, "ip_mode", "ip_ranges")
	}
	for _, name := range fields {
		if _, found := policy[name]; !found {
			return false
		}
	}
	return true
}
