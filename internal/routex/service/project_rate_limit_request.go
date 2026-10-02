package service

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/limits"
	"github.com/miclle/routex/pkg/secret"
)

type ProjectRateLimitValues struct {
	RPM         *int64 `json:"rpm"`
	TPM         *int64 `json:"tpm"`
	Concurrency *int64 `json:"concurrency"`
}

type ProjectRateLimitPatch struct {
	RPM         *int64 `json:"rpm,omitempty"`
	TPM         *int64 `json:"tpm,omitempty"`
	Concurrency *int64 `json:"concurrency,omitempty"`
}

func (patch *ProjectRateLimitPatch) UnmarshalJSON(raw []byte) error {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || len(fields) == 0 {
		return apperrors.ErrBadRequest
	}
	for key, value := range fields {
		if key != "rpm" && key != "tpm" && key != "concurrency" || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return apperrors.ErrBadRequest
		}
	}
	type plain ProjectRateLimitPatch
	var value plain
	if json.Unmarshal(raw, &value) != nil {
		return apperrors.ErrBadRequest
	}
	normalized, err := normalizeProjectRateLimitPatch(ProjectRateLimitPatch(value))
	if err != nil {
		return err
	}
	*patch = normalized
	return nil
}

func normalizeProjectRateLimitPatch(patch ProjectRateLimitPatch) (ProjectRateLimitPatch, error) {
	if patch.RPM == nil && patch.TPM == nil && patch.Concurrency == nil {
		return patch, apperrors.ErrBadRequest
	}
	for _, value := range []*int64{patch.RPM, patch.TPM, patch.Concurrency} {
		if value != nil && (*value < 0 || *value > limits.MaxInteger) {
			return patch, apperrors.ErrBadRequest
		}
	}
	return patch, nil
}

func projectRateLimitValues(policy limits.Policy) *ProjectRateLimitValues {
	return &ProjectRateLimitValues{RPM: policy.RPM, TPM: policy.TPM, Concurrency: policy.Concurrency}
}

type ProjectRequestLimitsContext struct {
	ProjectID        string                  `json:"project_id"`
	ReviewETag       string                  `json:"review_etag"`
	PolicyETag       string                  `json:"policy_etag"`
	CurrentQuota     *ProjectQuotaValues     `json:"current_quota"`
	CurrentRateLimit *ProjectRateLimitValues `json:"current_rate_limit"`
	PlatformCurrency string                  `json:"platform_currency"`
}

func (s *Service) GetProjectRequestLimitsContext(ctx context.Context, actorID, projectID string) (*ProjectRequestLimitsContext, error) {
	result, policy, err := s.getProjectRequestContext(ctx, actorID, projectID)
	if err != nil {
		return nil, err
	}
	return &ProjectRequestLimitsContext{ProjectID: result.ProjectID, ReviewETag: result.ReviewETag, PolicyETag: result.PolicyETag, CurrentQuota: result.CurrentQuota, CurrentRateLimit: projectRateLimitValues(policy), PlatformCurrency: result.PlatformCurrency}, nil
}

func projectRateLimitRequestRecord(row *entity.ProjectModelRequest, result *ProjectRequestRecord) (*ProjectRequestRecord, error) {
	var baseline ProjectRateLimitValues
	var patch ProjectRateLimitPatch
	if json.Unmarshal([]byte(row.BaselineJSON), &baseline) != nil || json.Unmarshal([]byte(row.RequestedJSON), &patch) != nil {
		return nil, apperrors.ErrInternal
	}
	result.BaselineRateLimit, result.RequestedRateLimit = &baseline, &patch
	result.BaselinePolicyETag = row.BaselinePolicyETag
	result.BaselineModelIDs, result.RequestedModelIDs = []string{}, []string{}
	if row.Status == entity.ProjectRequestApproved {
		policy, err := approvedProjectQuotaPolicy(row)
		if err != nil {
			return nil, err
		}
		result.ApprovedRateLimit = projectRateLimitValues(policy)
		result.ApprovedPolicyETag = row.ApprovedPolicyETag
	}
	return result, nil
}

func projectLimitBaseline(kind string, policy limits.Policy, currency string) ([]byte, error) {
	if kind == entity.ProjectRequestQuota {
		return json.Marshal(projectQuotaBaseline{ProjectQuotaValues: *projectQuotaValues(policy), PlatformCurrency: currency})
	}
	if kind == entity.ProjectRequestRateLimit {
		return json.Marshal(projectRateLimitValues(policy))
	}
	return nil, apperrors.ErrBadRequest
}

func projectLimitCreationIntent(actorID, projectID string, input ProjectRequestInput) ([]byte, string, error) {
	var requested, envelope []byte
	switch input.Kind {
	case entity.ProjectRequestQuota:
		if input.Quota == nil || input.RateLimit != nil {
			return nil, "", apperrors.ErrBadRequest
		}
		patch, err := normalizeProjectQuotaPatch(*input.Quota)
		if err != nil {
			return nil, "", err
		}
		requested, err = json.Marshal(patch)
		if err != nil {
			return nil, "", err
		}
		// This frozen envelope preserves released QUOTA retry identities.
		envelope, err = json.Marshal(struct {
			ActorID, ProjectID, RequestID, ReviewETag, Reason string
			Quota                                             ProjectQuotaPatch
		}{actorID, projectID, input.RequestID, input.ReviewETag, input.Reason, patch})
		if err != nil {
			return nil, "", err
		}
	case entity.ProjectRequestRateLimit:
		if input.RateLimit == nil || input.Quota != nil {
			return nil, "", apperrors.ErrBadRequest
		}
		patch, err := normalizeProjectRateLimitPatch(*input.RateLimit)
		if err != nil {
			return nil, "", err
		}
		requested, err = json.Marshal(patch)
		if err != nil {
			return nil, "", err
		}
		envelope, err = json.Marshal(struct {
			ActorID, ProjectID, RequestID, ReviewETag, Reason, Kind string
			RateLimit                                               ProjectRateLimitPatch
		}{actorID, projectID, input.RequestID, input.ReviewETag, input.Reason, input.Kind, patch})
		if err != nil {
			return nil, "", err
		}
	default:
		return nil, "", apperrors.ErrBadRequest
	}
	return requested, secret.SHA256Hex(string(envelope)), nil
}

func projectLimitDecisionHash(kind, actorID, projectID, requestID string, input ProjectRequestDecision) (string, error) {
	if kind == entity.ProjectRequestQuota {
		return projectQuotaDecisionHash(actorID, projectID, requestID, input)
	}
	raw, err := json.Marshal(struct{ ActorID, ProjectID, RequestID, Action, Reason, ReviewETag, Kind string }{actorID, projectID, requestID, input.Action, input.Reason, input.ReviewETag, kind})
	if err != nil {
		return "", err
	}
	return secret.SHA256Hex(string(raw)), nil
}

func applyProjectLimitPatch(kind, raw string, before limits.Policy, currency string) (limits.Policy, error) {
	approved := before
	switch kind {
	case entity.ProjectRequestQuota:
		var patch ProjectQuotaPatch
		if json.Unmarshal([]byte(raw), &patch) != nil {
			return approved, apperrors.ErrInternal
		}
		if patch.MoneyMonth != nil && patch.Currency != currency {
			return approved, catalogConflict
		}
		if patch.TokensMonth != nil {
			approved.TokensMonth = patch.TokensMonth
		}
		if patch.MoneyMonth != nil {
			approved.MoneyMonth, approved.Currency = patch.MoneyMonth, patch.Currency
		}
	case entity.ProjectRequestRateLimit:
		var patch ProjectRateLimitPatch
		if json.Unmarshal([]byte(raw), &patch) != nil {
			return approved, apperrors.ErrInternal
		}
		if patch.RPM != nil {
			approved.RPM = patch.RPM
		}
		if patch.TPM != nil {
			approved.TPM = patch.TPM
		}
		if patch.Concurrency != nil {
			approved.Concurrency = patch.Concurrency
		}
	default:
		return approved, apperrors.ErrBadRequest
	}
	normalized, err := limits.Normalize(approved)
	if err != nil {
		return approved, apperrors.ErrBadRequest
	}
	if normalized.MoneyMonth != nil && normalized.Currency != currency {
		return normalized, catalogConflict
	}
	return normalized, nil
}
