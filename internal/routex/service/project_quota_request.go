package service

import (
	"bytes"
	"encoding/json"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/limits"
	"github.com/miclle/routex/pkg/pricing"
	"github.com/miclle/routex/pkg/secret"
)

type ProjectQuotaValues struct {
	TokensMonth *int64  `json:"tokens_month"`
	MoneyMonth  *string `json:"money_month"`
	Currency    string  `json:"currency"`
}

type ProjectQuotaPatch struct {
	TokensMonth *int64  `json:"tokens_month,omitempty"`
	MoneyMonth  *string `json:"money_month,omitempty"`
	Currency    string  `json:"currency,omitempty"`
}

func (patch *ProjectQuotaPatch) UnmarshalJSON(raw []byte) error {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || len(fields) == 0 {
		return apperrors.ErrBadRequest
	}
	for key, value := range fields {
		if key != "tokens_month" && key != "money_month" && key != "currency" || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return apperrors.ErrBadRequest
		}
	}
	if fields["currency"] != nil && fields["money_month"] == nil {
		return apperrors.ErrBadRequest
	}
	type plain ProjectQuotaPatch
	var value plain
	if json.Unmarshal(raw, &value) != nil {
		return apperrors.ErrBadRequest
	}
	normalized, err := normalizeProjectQuotaPatch(ProjectQuotaPatch(value))
	if err != nil {
		return err
	}
	*patch = normalized
	return nil
}

func normalizeProjectQuotaPatch(patch ProjectQuotaPatch) (ProjectQuotaPatch, error) {
	if patch.TokensMonth == nil && patch.MoneyMonth == nil {
		return patch, apperrors.ErrBadRequest
	}
	if patch.TokensMonth != nil && (*patch.TokensMonth < 0 || *patch.TokensMonth > limits.MaxInteger) {
		return patch, apperrors.ErrBadRequest
	}
	if patch.MoneyMonth == nil {
		if patch.Currency != "" {
			return patch, apperrors.ErrBadRequest
		}
	} else {
		if !pricing.Currency(patch.Currency) {
			return patch, apperrors.ErrBadRequest
		}
		value, err := pricing.Decimal(*patch.MoneyMonth)
		if err != nil {
			return patch, apperrors.ErrBadRequest
		}
		patch.MoneyMonth = &value
	}
	return patch, nil
}

func projectQuotaValues(policy limits.Policy) *ProjectQuotaValues {
	return &ProjectQuotaValues{TokensMonth: policy.TokensMonth, MoneyMonth: policy.MoneyMonth, Currency: policy.Currency}
}

type projectQuotaBaseline struct {
	ProjectQuotaValues
	PlatformCurrency string `json:"platform_currency"`
}

type ProjectQuotaRequestContext struct {
	ProjectID        string              `json:"project_id"`
	ReviewETag       string              `json:"review_etag"`
	PolicyETag       string              `json:"policy_etag"`
	CurrentQuota     *ProjectQuotaValues `json:"current_quota"`
	PlatformCurrency string              `json:"platform_currency"`
}

func projectQuotaRequestRecord(row *entity.ProjectModelRequest, result *ProjectRequestRecord) (*ProjectRequestRecord, error) {
	var baseline projectQuotaBaseline
	var patch ProjectQuotaPatch
	if json.Unmarshal([]byte(row.BaselineJSON), &baseline) != nil || json.Unmarshal([]byte(row.RequestedJSON), &patch) != nil {
		return nil, apperrors.ErrInternal
	}
	result.BaselineQuota = &baseline.ProjectQuotaValues
	result.RequestedQuota = &patch
	result.BaselinePolicyETag = row.BaselinePolicyETag
	result.BaselineModelIDs, result.RequestedModelIDs = []string{}, []string{}
	if row.Status == entity.ProjectRequestApproved {
		policy, err := approvedProjectQuotaPolicy(row)
		if err != nil {
			return nil, err
		}
		result.ApprovedPolicyETag = row.ApprovedPolicyETag
		result.ApprovedQuota = projectQuotaValues(policy)
	}
	return result, nil
}

func projectQuotaDecisionHash(actorID, projectID, requestID string, input ProjectRequestDecision) (string, error) {
	raw, err := json.Marshal(struct{ ActorID, ProjectID, RequestID, Action, Reason, ReviewETag string }{actorID, projectID, requestID, input.Action, input.Reason, input.ReviewETag})
	if err != nil {
		return "", err
	}
	return secret.SHA256Hex(string(raw)), nil
}
