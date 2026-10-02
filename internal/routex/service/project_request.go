package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/id"
	"github.com/miclle/routex/pkg/secret"
)

type ProjectRequestInput struct {
	Kind       string
	Quota      *ProjectQuotaPatch
	RateLimit  *ProjectRateLimitPatch
	ReviewETag string
	RequestID  string
	ModelIDs   []string
	Reason     string
}
type ProjectRequestDecision struct{ Action, Reason, ReviewETag string }
type ProjectRequestFilter struct {
	Status, Cursor string
	Limit          int
}
type ProjectRequestRecord struct {
	BaselineRateLimit  *ProjectRateLimitValues `json:"baseline_rate_limit,omitempty"`
	RequestedRateLimit *ProjectRateLimitPatch  `json:"requested_rate_limit,omitempty"`
	CurrentRateLimit   *ProjectRateLimitValues `json:"current_rate_limit,omitempty"`
	ApprovedRateLimit  *ProjectRateLimitValues `json:"approved_rate_limit,omitempty"`
	BaselineQuota      *ProjectQuotaValues     `json:"baseline_quota,omitempty"`
	RequestedQuota     *ProjectQuotaPatch      `json:"requested_quota,omitempty"`
	ApprovedQuota      *ProjectQuotaValues     `json:"approved_quota,omitempty"`
	BaselinePolicyETag string                  `json:"baseline_policy_etag,omitempty"`
	ApprovedPolicyETag string                  `json:"approved_policy_etag,omitempty"`
	CurrentQuota       *ProjectQuotaValues     `json:"current_quota,omitempty"`
	CurrentPolicyETag  string                  `json:"current_policy_etag,omitempty"`
	PlatformCurrency   string                  `json:"platform_currency,omitempty"`
	ApprovalReviewETag string                  `json:"approval_review_etag,omitempty"`
	RuntimeApplied     *bool                   `json:"runtime_applied,omitempty"`
	ApplicationStatus  string                  `json:"application_status,omitempty"`
	ID                 string                  `json:"id"`
	ProjectID          string                  `json:"project_id"`
	ApplicantUserID    string                  `json:"applicant_user_id"`
	Kind               string                  `json:"kind"`
	BaselineModelIDs   []string                `json:"baseline_model_ids"`
	RequestedModelIDs  []string                `json:"requested_model_ids"`
	Reason             string                  `json:"reason"`
	Status             string                  `json:"status"`
	DecisionActorID    string                  `json:"decision_actor_id,omitempty"`
	DecisionReason     string                  `json:"decision_reason,omitempty"`
	CreatedAt          time.Time               `json:"created_at"`
	DecidedAt          *time.Time              `json:"decided_at"`
}
type ProjectRequestPage struct {
	Items      []ProjectRequestRecord `json:"items"`
	NextCursor string                 `json:"next_cursor,omitempty"`
}

func projectRequestRecord(row *entity.ProjectModelRequest) (*ProjectRequestRecord, error) {
	kind := row.Kind
	if kind == "" {
		kind = entity.ProjectRequestModelAccess
	}
	result := &ProjectRequestRecord{ID: row.ID, ProjectID: row.ProjectID, ApplicantUserID: row.ApplicantUserID, Kind: kind, Reason: row.Reason, Status: row.Status, DecisionActorID: row.DecisionActorID, DecisionReason: row.DecisionReason, CreatedAt: row.CreatedAt, DecidedAt: row.DecidedAt}
	if kind == entity.ProjectRequestRateLimit {
		return projectRateLimitRequestRecord(row, result)
	}
	if kind == entity.ProjectRequestQuota {
		return projectQuotaRequestRecord(row, result)
	}
	if kind != entity.ProjectRequestModelAccess {
		return nil, apperrors.ErrInternal
	}
	if err := json.Unmarshal([]byte(row.BaselineJSON), &result.BaselineModelIDs); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(row.RequestedJSON), &result.RequestedModelIDs); err != nil {
		return nil, err
	}
	return result, nil
}
func normalizeProjectRequest(input ProjectRequestInput) (ProjectRequestInput, error) {
	input.Reason = strings.TrimSpace(input.Reason)
	if !safeCallID.MatchString(input.RequestID) || len(input.ModelIDs) == 0 || len(input.ModelIDs) > 1000 || input.Reason == "" || !validResourceDescription(input.Reason) {
		return input, apperrors.ErrBadRequest
	}
	input.ModelIDs = slices.Clone(input.ModelIDs)
	slices.Sort(input.ModelIDs)
	for i, value := range input.ModelIDs {
		if value == "" || len(value) > 30 || (i > 0 && value == input.ModelIDs[i-1]) {
			return input, apperrors.ErrBadRequest
		}
	}
	return input, nil
}
func projectRequestManager(db *gorm.DB, actorID, projectID string) error {
	if _, err := permissionsFor(db, actorID); err != nil {
		return err
	}
	manager, err := resourceManager(db, actorID, projectID)
	if err != nil {
		return err
	}
	if !manager {
		return apperrors.ErrForbidden
	}
	return nil
}
func activeProjectRequestModels(tx *gorm.DB, ids []string) error {
	var models []entity.Model
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id IN ?", ids).Order("id").Find(&models).Error; err != nil {
		return err
	}
	if len(models) != len(ids) {
		return catalogConflict
	}
	for _, model := range models {
		if model.Status != entity.ResourceActive {
			return catalogConflict
		}
	}
	return nil
}
func (s *Service) CreateProjectRequest(ctx context.Context, actorID, projectID string, input ProjectRequestInput) (*ProjectRequestRecord, error) {
	if isProjectLimitRequest(input.Kind) {
		return s.createProjectLimitRequest(ctx, actorID, projectID, input)
	}
	if input.Kind != "" && input.Kind != entity.ProjectRequestModelAccess || input.Quota != nil || input.RateLimit != nil {
		return nil, apperrors.ErrBadRequest
	}
	input, err := normalizeProjectRequest(input)
	if err != nil {
		return nil, err
	}
	digest, err := projectModelRequestHash(actorID, projectID, input)
	if err != nil {
		return nil, err
	}
	var result *ProjectRequestRecord
	err = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		if err := projectRequestManager(tx, actorID, projectID); err != nil {
			return err
		}
		var existing entity.ProjectModelRequest
		err := tx.Where("request_id = ?", input.RequestID).First(&existing).Error
		if err == nil {
			if existing.RequestHash != digest {
				return catalogConflict
			}
			result, err = projectRequestRecord(&existing)
			return err
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		project, err := resourceRecord(tx, ProjectResource, projectID, true)
		if err != nil {
			return err
		}
		if project.Status != entity.ResourceActive {
			return catalogConflict
		}
		for _, modelID := range input.ModelIDs {
			if slices.Contains(project.ModelIDs, modelID) {
				return apperrors.ErrBadRequest
			}
		}
		if err := activeProjectRequestModels(tx, input.ModelIDs); err != nil {
			return err
		}
		requestID, err := id.NewPrefixed("pmr")
		if err != nil {
			return err
		}
		baseline, err := json.Marshal(project.ModelIDs)
		if err != nil {
			return err
		}
		requested, err := json.Marshal(input.ModelIDs)
		if err != nil {
			return err
		}
		row := entity.ProjectModelRequest{ID: requestID, RequestID: input.RequestID, RequestHash: digest, ProjectID: projectID, ApplicantUserID: actorID, BaselineJSON: string(baseline), RequestedJSON: string(requested), Reason: input.Reason, Status: entity.ProjectRequestPending}
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		if err := appendAudit(tx, actorID, "project.request.create", "project_request", row.ID); err != nil {
			return err
		}
		result, err = projectRequestRecord(&row)
		return err
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	return result, catalogError(err)
}
func (s *Service) ListProjectRequests(ctx context.Context, actorID, projectID string, filter ProjectRequestFilter) (*ProjectRequestPage, error) {
	if filter.Limit == 0 {
		filter.Limit = 40
	}
	if filter.Limit < 1 || filter.Limit > 100 || len(filter.Cursor) > 30 || !slices.Contains([]string{"", entity.ProjectRequestPending, entity.ProjectRequestApproved, entity.ProjectRequestRejected, entity.ProjectRequestWithdrawn}, filter.Status) {
		return nil, apperrors.ErrBadRequest
	}
	result := &ProjectRequestPage{Items: []ProjectRequestRecord{}}
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		access, err := projectRequestReadAccess(tx, actorID, projectID)
		if err != nil {
			return err
		}
		var project entity.Project
		if err := tx.Where(database.ExactText(tx, clause.Column{Name: "id"}, projectID)).First(&project).Error; err != nil {
			return err
		}
		if project.ID != projectID {
			return apperrors.ErrNotFound
		}
		query := tx.Where(database.ExactText(tx, clause.Column{Name: "project_id"}, projectID))
		query = projectRequestKindScope(query, access)

		if filter.Status != "" {
			query = query.Where("status = ?", filter.Status)
		}
		if filter.Cursor != "" {
			query = query.Where("id < ?", filter.Cursor)
		}
		var rows []entity.ProjectModelRequest
		if err := query.Order("id DESC").Limit(filter.Limit + 1).Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) > filter.Limit {
			rows = rows[:filter.Limit]
			result.NextCursor = rows[len(rows)-1].ID
		}
		for _, row := range rows {
			if row.ProjectID != projectID || !access.allows(row.Kind) {
				return apperrors.ErrInternal
			}
			record, err := projectRequestRecord(&row)
			if err != nil {
				return err
			}
			result.Items = append(result.Items, *record)
		}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return result, catalogError(err)
}

func projectModelRequestHash(actorID, projectID string, input ProjectRequestInput) (string, error) {
	encoded, err := json.Marshal(struct {
		ActorID, ProjectID string
		Input              struct {
			RequestID string
			ModelIDs  []string
			Reason    string
		}
	}{actorID, projectID, struct {
		RequestID string
		ModelIDs  []string
		Reason    string
	}{input.RequestID, input.ModelIDs, input.Reason}})
	if err != nil {
		return "", err
	}
	return secret.SHA256Hex(string(encoded)), nil
}
