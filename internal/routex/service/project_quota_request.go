package service

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/id"
	"github.com/miclle/routex/pkg/limits"
	"github.com/miclle/routex/pkg/pricing"
	"github.com/miclle/routex/pkg/secret"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
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

type projectRequestAccess struct{ Model, Quota bool }

func (access projectRequestAccess) allows(kind string) bool {
	return (kind == "" || kind == entity.ProjectRequestModelAccess) && access.Model || kind == entity.ProjectRequestQuota && access.Quota
}

func exactProjectRequestManager(tx *gorm.DB, actorID, projectID string) (bool, error) {
	var rows []entity.ProjectManager
	err := tx.Where(database.ExactText(tx, clause.Column{Name: "user_id"}, actorID)).
		Where(database.ExactText(tx, clause.Column{Name: "project_id"}, projectID)).Limit(1).Find(&rows).Error
	if err != nil {
		return false, err
	}
	return len(rows) == 1 && rows[0].UserID == actorID && rows[0].ProjectID == projectID, nil
}

func projectRequestReadAccess(tx *gorm.DB, actorID, projectID string) (projectRequestAccess, error) {
	var access projectRequestAccess
	actor, err := exactEnabledActor(tx, actorID)
	if err != nil {
		return access, err
	}
	manager, err := exactProjectRequestManager(tx, actorID, projectID)
	if err != nil {
		return access, err
	}
	all, err := exactGovernancePermission(tx, actor, "projects.read_all")
	if err != nil {
		return access, err
	}
	if manager || all {
		return projectRequestAccess{Model: true, Quota: true}, nil
	}
	access.Model, err = exactGovernancePermission(tx, actor, "projects.models.write")
	if err != nil {
		return access, err
	}
	access.Quota, err = exactGovernancePermission(tx, actor, "projects.limits.write")
	if err != nil {
		return access, err
	}
	if !access.Model && !access.Quota {
		return access, apperrors.ErrNotFound
	}
	return access, nil
}

func projectRequestKindScope(tx *gorm.DB, access projectRequestAccess) *gorm.DB {
	if access.Model && access.Quota {
		return tx.Where(clause.Or(
			database.ExactText(tx, clause.Column{Name: "kind"}, entity.ProjectRequestModelAccess),
			database.ExactText(tx, clause.Column{Name: "kind"}, entity.ProjectRequestQuota),
			database.ExactText(tx, clause.Column{Name: "kind"}, ""),
		))
	}
	if access.Quota {
		return tx.Where(database.ExactText(tx, clause.Column{Name: "kind"}, entity.ProjectRequestQuota))
	}
	return tx.Where(clause.Or(
		database.ExactText(tx, clause.Column{Name: "kind"}, entity.ProjectRequestModelAccess),
		database.ExactText(tx, clause.Column{Name: "kind"}, ""),
	))
}

func readProjectQuotaPolicy(tx *gorm.DB, projectID string, lock bool) (entity.ResourceLimit, limits.Policy, error) {
	row := entity.ResourceLimit{ScopeKind: "project", ScopeID: projectID, ETag: "0"}
	query := tx.Where(database.ExactText(tx, clause.Column{Name: "scope_kind"}, "project")).Where(database.ExactText(tx, clause.Column{Name: "scope_id"}, projectID))
	if lock {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	err := query.First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		err = nil
	}
	if err != nil {
		return row, limits.Policy{}, err
	}
	if row.ScopeKind != "project" || row.ScopeID != projectID {
		return row, limits.Policy{}, catalogConflict
	}
	policy, err := policyFromRow(row)
	return row, policy, err
}

func projectQuotaReviewETag(projectID string, row entity.ResourceLimit, policy limits.Policy, currency entity.PricingSetting, request *entity.ProjectModelRequest) (string, error) {
	value := struct {
		ProjectID, PolicyETag, PricingETag, Currency                      string
		Policy                                                            limits.Policy
		RequestID, RequestHash, BaselinePolicyETag, RequestedJSON, Status string
	}{
		ProjectID:   projectID,
		PolicyETag:  row.ETag,
		PricingETag: currency.ETag,
		Currency:    currency.PlatformCurrency,
		Policy:      policy,
	}
	if request != nil {
		value.RequestID, value.RequestHash, value.BaselinePolicyETag, value.RequestedJSON, value.Status = request.ID, request.RequestHash, request.BaselinePolicyETag, request.RequestedJSON, request.Status
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return secret.SHA256Hex(string(raw)), nil
}

func readProjectQuotaContext(tx *gorm.DB, projectID string, request *entity.ProjectModelRequest) (*ProjectQuotaRequestContext, entity.ResourceLimit, limits.Policy, error) {
	row, policy, err := readProjectQuotaPolicy(tx, projectID, false)
	if err != nil {
		return nil, row, policy, err
	}
	var currency entity.PricingSetting
	if err := tx.First(&currency, 1).Error; err != nil {
		return nil, row, policy, err
	}
	tag, err := projectQuotaReviewETag(projectID, row, policy, currency, request)
	return &ProjectQuotaRequestContext{
		ProjectID:        projectID,
		ReviewETag:       tag,
		PolicyETag:       row.ETag,
		CurrentQuota:     projectQuotaValues(policy),
		PlatformCurrency: currency.PlatformCurrency,
	}, row, policy, err
}

func (s *Service) GetProjectQuotaRequestContext(ctx context.Context, actorID, projectID string) (*ProjectQuotaRequestContext, error) {
	var result *ProjectQuotaRequestContext
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := exactEnabledActor(tx, actorID); err != nil {
			return err
		}
		manager, err := exactProjectRequestManager(tx, actorID, projectID)
		if err != nil {
			return err
		}
		if !manager {
			return apperrors.ErrForbidden
		}
		var project entity.Project
		if err := tx.Where(database.ExactText(tx, clause.Column{Name: "id"}, projectID)).First(&project).Error; err != nil {
			return err
		}
		if project.ID != projectID {
			return apperrors.ErrNotFound
		}
		if project.Status != entity.ResourceActive {
			return catalogConflict
		}
		result, _, _, err = readProjectQuotaContext(tx, projectID, nil)
		return err
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return result, catalogError(err)
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

func approvedProjectQuotaPolicy(row *entity.ProjectModelRequest) (limits.Policy, error) {
	var policy limits.Policy
	if row.ApprovedPolicyETag == "" || json.Unmarshal([]byte(row.ApprovedPolicyJSON), &policy) != nil {
		return policy, apperrors.ErrInternal
	}
	normalized, err := limits.Normalize(policy)
	if err != nil {
		return policy, apperrors.ErrInternal
	}
	return normalized, nil
}

func (s *Service) createProjectQuotaRequest(ctx context.Context, actorID, projectID string, input ProjectRequestInput) (*ProjectRequestRecord, error) {
	if input.Quota == nil || len(input.ModelIDs) != 0 || !safeCallID.MatchString(input.RequestID) || !projectQuotaReviewValidator(input.ReviewETag) {
		return nil, apperrors.ErrBadRequest
	}
	patch, err := normalizeProjectQuotaPatch(*input.Quota)
	if err != nil {
		return nil, err
	}
	input.Reason = strings.TrimSpace(input.Reason)
	if input.Reason == "" || !validResourceDescription(input.Reason) {
		return nil, apperrors.ErrBadRequest
	}
	raw, err := json.Marshal(struct {
		ActorID, ProjectID, RequestID, ReviewETag, Reason string
		Quota                                             ProjectQuotaPatch
	}{actorID, projectID, input.RequestID, input.ReviewETag, input.Reason, patch})
	if err != nil {
		return nil, err
	}
	digest := secret.SHA256Hex(string(raw))
	var result *ProjectRequestRecord
	err = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		if _, err := exactEnabledActor(tx, actorID); err != nil {
			return err
		}
		manager, err := exactProjectRequestManager(tx, actorID, projectID)
		if err != nil {
			return err
		}
		if !manager {
			return apperrors.ErrForbidden
		}
		var existing entity.ProjectModelRequest
		err = tx.Where(database.ExactText(tx, clause.Column{Name: "request_id"}, input.RequestID)).First(&existing).Error
		if err == nil {
			if existing.RequestID != input.RequestID || existing.ProjectID != projectID || existing.ApplicantUserID != actorID || existing.Kind != entity.ProjectRequestQuota || existing.RequestHash != digest {
				return catalogConflict
			}
			result, err = projectRequestRecord(&existing)
			return err
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var project entity.Project
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(database.ExactText(tx, clause.Column{Name: "id"}, projectID)).First(&project).Error; err != nil {
			return err
		}
		if project.ID != projectID {
			return apperrors.ErrNotFound
		}
		if project.Status != entity.ResourceActive {
			return catalogConflict
		}
		context, row, _, err := readProjectQuotaContext(tx, projectID, nil)
		if err != nil {
			return err
		}
		if context.ReviewETag != input.ReviewETag || patch.MoneyMonth != nil && patch.Currency != context.PlatformCurrency {
			return catalogConflict
		}
		baseline, err := json.Marshal(projectQuotaBaseline{ProjectQuotaValues: *context.CurrentQuota, PlatformCurrency: context.PlatformCurrency})
		if err != nil {
			return err
		}
		requested, err := json.Marshal(patch)
		if err != nil {
			return err
		}
		recordID, err := id.NewPrefixed("pmr")
		if err != nil {
			return err
		}
		saved := entity.ProjectModelRequest{
			ID:                 recordID,
			Kind:               entity.ProjectRequestQuota,
			RequestID:          input.RequestID,
			RequestHash:        digest,
			ProjectID:          projectID,
			ApplicantUserID:    actorID,
			BaselineJSON:       string(baseline),
			RequestedJSON:      string(requested),
			BaselinePolicyETag: row.ETag,
			Reason:             input.Reason,
			Status:             entity.ProjectRequestPending,
		}
		if err := tx.Create(&saved).Error; err != nil {
			return err
		}
		if err := appendAudit(tx, actorID, "project.request.create", "project_request", saved.ID); err != nil {
			return err
		}
		result, err = projectRequestRecord(&saved)
		return err
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	return result, catalogError(err)
}

func (s *Service) GetProjectRequest(ctx context.Context, actorID, projectID, requestID string) (*ProjectRequestRecord, error) {
	var result *ProjectRequestRecord
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		access, err := projectRequestReadAccess(tx, actorID, projectID)
		if err != nil {
			return err
		}
		var project entity.Project
		if err := tx.Where(database.ExactText(tx, clause.Column{Name: "id"}, projectID)).First(&project).Error; err != nil {
			return err
		}
		var row entity.ProjectModelRequest
		if err := projectRequestKindScope(tx, access).Where(database.ExactText(tx, clause.Column{Name: "project_id"}, projectID)).Where(database.ExactText(tx, clause.Column{Name: "id"}, requestID)).First(&row).Error; err != nil {
			return err
		}
		if row.ID != requestID || row.ProjectID != projectID || project.ID != projectID || !access.allows(row.Kind) {
			return apperrors.ErrNotFound
		}
		result, err = projectRequestRecord(&row)
		if err != nil {
			return err
		}
		if row.Kind == entity.ProjectRequestQuota {
			context, current, policy, err := readProjectQuotaContext(tx, projectID, &row)
			if err != nil {
				return err
			}
			result.CurrentQuota, result.CurrentPolicyETag, result.PlatformCurrency = context.CurrentQuota, context.PolicyETag, context.PlatformCurrency
			if row.Status == entity.ProjectRequestPending {
				result.ApprovalReviewETag = context.ReviewETag
			}
			return s.projectQuotaApplication(&row, result, current, policy, project.Status)
		}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return result, catalogError(err)
}

func (s *Service) projectQuotaApplication(row *entity.ProjectModelRequest, result *ProjectRequestRecord, current entity.ResourceLimit, policy limits.Policy, projectStatus string) error {
	if row.Status != entity.ProjectRequestApproved {
		return nil
	}
	applied := false
	result.RuntimeApplied = &applied
	result.ApplicationStatus = "pending"
	approved, err := approvedProjectQuotaPolicy(row)
	if err != nil {
		return err
	}
	if current.ETag != row.ApprovedPolicyETag || !reflect.DeepEqual(policy, approved) {
		result.ApplicationStatus = "superseded"
		return nil
	}
	if s.runtime == nil || projectStatus != entity.ResourceActive {
		return nil
	}
	auth := s.runtime.auth.Load()
	account := limitAccount("project", row.ProjectID)
	if auth == nil || !time.Now().Before(auth.ValidUntil) || auth.Quota == nil || auth.Quota.Revisions[account] != row.ApprovedPolicyETag || runtimeDenied(&s.runtime.deniedProjects, row.ProjectID) || runtimeDenied(&s.runtime.deniedLimits, account) || runtimeDenied(&s.runtime.deniedLimits, "quota_settings") {
		return nil
	}
	if approved.MoneyMonth != nil && (auth.Quota.Currency != approved.Currency || result.PlatformCurrency != approved.Currency) {
		return nil
	}
	published, err := limits.Normalize(auth.LimitPolicies[account])
	if err != nil || !reflect.DeepEqual(published, approved) {
		return nil
	}
	applied = true
	result.ApplicationStatus = "applied"
	return nil
}

func projectQuotaDecisionHash(actorID, projectID, requestID string, input ProjectRequestDecision) (string, error) {
	raw, err := json.Marshal(struct{ ActorID, ProjectID, RequestID, Action, Reason, ReviewETag string }{actorID, projectID, requestID, input.Action, input.Reason, input.ReviewETag})
	if err != nil {
		return "", err
	}
	return secret.SHA256Hex(string(raw)), nil
}

func projectQuotaReviewValidator(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, char := range value {
		if !strings.ContainsRune("0123456789abcdef", char) {
			return false
		}
	}
	return true
}

func (s *Service) decideProjectQuotaRequest(ctx context.Context, actorID, projectID, requestID string, input ProjectRequestDecision) (*ProjectRequestRecord, error) {
	input.Reason = strings.TrimSpace(input.Reason)
	status, err := projectRequestDecisionStatus(input)
	if err != nil || !validResourceDescription(input.Reason) {
		return nil, apperrors.ErrBadRequest
	}
	if input.ReviewETag != "" && !projectQuotaReviewValidator(input.ReviewETag) || input.Action == "approve" && (input.ReviewETag == "" || input.Reason == "") {
		return nil, apperrors.ErrBadRequest
	}
	digest, err := projectQuotaDecisionHash(actorID, projectID, requestID, input)
	if err != nil {
		return nil, err
	}
	s.limitMu.Lock()
	defer s.limitMu.Unlock()
	var result *ProjectRequestRecord
	var saved entity.ProjectModelRequest
	fresh := false
	err = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		actor, err := exactEnabledActor(tx, actorID)
		if err != nil {
			return err
		}
		if input.Action != "withdraw" {
			allowed, err := exactGovernancePermission(tx, actor, "projects.limits.write")
			if err != nil {
				return err
			}
			if !allowed {
				return apperrors.ErrForbidden
			}
		}
		var project entity.Project
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(database.ExactText(tx, clause.Column{Name: "id"}, projectID)).First(&project).Error; err != nil {
			return err
		}
		var row entity.ProjectModelRequest
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(database.ExactText(tx, clause.Column{Name: "project_id"}, projectID)).Where(database.ExactText(tx, clause.Column{Name: "id"}, requestID)).First(&row).Error; err != nil {
			return err
		}
		if row.Kind != entity.ProjectRequestQuota || row.ID != requestID || row.ProjectID != projectID || project.ID != projectID {
			return apperrors.ErrNotFound
		}
		if input.Action == "withdraw" && row.ApplicantUserID != actorID || input.Action != "withdraw" && row.ApplicantUserID == actorID {
			return apperrors.ErrForbidden
		}
		if row.Status != entity.ProjectRequestPending {
			if row.Status != status || row.DecisionActorID != actorID || row.DecisionReason != input.Reason || row.DecisionRequestHash != digest || row.DecisionReviewETag != input.ReviewETag {
				return catalogConflict
			}
			saved = row
			return nil
		}
		if input.Action == "approve" {
			if project.Status != entity.ResourceActive {
				return catalogConflict
			}
			if _, err := exactEnabledActor(tx, row.ApplicantUserID); err != nil {
				if errors.Is(err, apperrors.ErrUnauthorized) {
					return catalogConflict
				}
				return err
			}
			manager, err := exactProjectRequestManager(tx, row.ApplicantUserID, projectID)
			if err != nil {
				return err
			}
			if !manager {
				return catalogConflict
			}
			current, before, err := readProjectQuotaPolicy(tx, projectID, true)
			if err != nil {
				return err
			}
			currency, err := lockPricing(tx)
			if err != nil {
				return err
			}
			review, err := projectQuotaReviewETag(projectID, current, before, currency, &row)
			if err != nil {
				return err
			}
			if review != input.ReviewETag {
				return catalogConflict
			}
			var patch ProjectQuotaPatch
			if json.Unmarshal([]byte(row.RequestedJSON), &patch) != nil {
				return apperrors.ErrInternal
			}
			if patch.MoneyMonth != nil && patch.Currency != currency.PlatformCurrency {
				return catalogConflict
			}
			approved := before
			if patch.TokensMonth != nil {
				approved.TokensMonth = patch.TokensMonth
			}
			if patch.MoneyMonth != nil {
				approved.MoneyMonth, approved.Currency = patch.MoneyMonth, patch.Currency
			}
			approved, err = limits.Normalize(approved)
			if err != nil {
				return apperrors.ErrBadRequest
			}
			if approved.MoneyMonth != nil && approved.Currency != currency.PlatformCurrency {
				return catalogConflict
			}
			auditReason := input.Reason
			policyRow, err := persistResourceLimitPolicy(tx, actorID, resolvedLimitTarget{kind: "project", id: projectID}, current, before, approved, auditReason)
			if err != nil {
				return err
			}
			full, err := json.Marshal(approved)
			if err != nil {
				return err
			}
			row.ApprovedPolicyETag, row.ApprovedPolicyJSON = policyRow.ETag, string(full)
			fresh = true
		}
		now := time.Now().UTC()
		row.Status, row.DecisionActorID, row.DecisionReason, row.DecidedAt = status, actorID, input.Reason, &now
		row.DecisionReviewETag, row.DecisionRequestHash = input.ReviewETag, digest
		if err := tx.Save(&row).Error; err != nil {
			return err
		}
		if err := appendAudit(tx, actorID, "project.request."+input.Action, "project_request", row.ID); err != nil {
			return err
		}
		saved = row
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, catalogError(err)
	}
	if fresh {
		s.denyLimitScope("project", projectID)
		if err := s.RefreshRuntime(ctx); err != nil {
			return nil, err
		}
	} else if saved.Status == entity.ProjectRequestApproved {
		// Historical retry can reconcile publication only while the exact approved
		// policy remains current. It never writes or restores a historical patch.
		current, _, err := readProjectQuotaPolicy(s.authDB(ctx), projectID, false)
		if err != nil {
			return nil, catalogError(err)
		}
		if current.ETag == saved.ApprovedPolicyETag {
			if err := s.RefreshRuntime(ctx); err != nil {
				return nil, err
			}
		}
	}
	result, err = s.GetProjectRequest(ctx, actorID, projectID, requestID)
	if err != nil {
		// Withdrawal remains available to the enabled applicant after losing the
		// manager relationship; returning its receipt does not restore read access.
		if input.Action == "withdraw" && errors.Is(err, apperrors.ErrNotFound) {
			return projectRequestRecord(&saved)
		}
		return nil, err
	}
	return result, nil
}
