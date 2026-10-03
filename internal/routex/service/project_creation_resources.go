package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/id"
	"github.com/miclle/routex/pkg/limits"
)

type ProjectCreationReceiptRecord struct {
	CreationID        string    `json:"creation_id"`
	ProjectID         string    `json:"project_id"`
	CreatedAt         time.Time `json:"created_at"`
	InitialRequestIDs []string  `json:"initial_request_ids"`
}
type ProjectCreationResult struct {
	Project           *ResourceRecord              `json:"project"`
	Receipt           ProjectCreationReceiptRecord `json:"receipt"`
	Committed         bool                         `json:"committed"`
	RuntimeApplied    bool                         `json:"runtime_applied"`
	ApplicationStatus string                       `json:"application_status"`
}
type ProjectCreationManagerSnapshot struct{ ID, UserID string }

// ProjectCreationSnapshot records the original application boundary, not a
// desired configuration to restore when a historical receipt is reconciled.
type ProjectCreationSnapshot struct {
	ProjectID, Name, Description, CreatorID string
	Reason                                  string
	Managers                                []ProjectCreationManagerSnapshot
	ModelIDs                                []string
	PolicyETag                              string
	Policy                                  limits.Policy
	InitialRequestIDs                       []string
}

func projectCreationHash(actorID string, input ProjectCreationInput) string {
	return personalHash(struct {
		Actor      string
		Input      ProjectCreationInput
		ReviewETag string
	}{actorID, input, input.ReviewETag})
}

func projectCreationReceiptMatches(row entity.ProjectCreationReceipt, actorID, hash string, input ProjectCreationInput) bool {
	return row.CreationID == input.CreationID && row.ActorID == actorID && row.RequestHash == hash && row.ReviewETag == input.ReviewETag
}

func readProjectCreationSnapshot(row entity.ProjectCreationReceipt) (ProjectCreationSnapshot, error) {
	var snapshot ProjectCreationSnapshot
	if json.Unmarshal([]byte(row.SnapshotJSON), &snapshot) != nil || snapshot.ProjectID != row.ProjectID || snapshot.CreatorID != row.ActorID || !safeTeamSessionID(row.ProjectID) || !credentialReplacementRequestID.MatchString(row.CreationID) || !personalModelETag(row.RequestHash) || !personalModelETag(row.ReviewETag) || row.CreatedAt.IsZero() || len(snapshot.Managers) == 0 || len(snapshot.Managers) > 1000 || len(snapshot.ModelIDs) > 1000 || len(snapshot.InitialRequestIDs) > 3 || snapshot.PolicyETag == "" {
		return snapshot, apperrors.ErrInternal
	}
	if !strings.HasPrefix(snapshot.ProjectID, "prj_") || !validCatalogLabel(snapshot.Name) || !validResourceDescription(snapshot.Description) || !validResourceDescription(snapshot.Reason) || !safeTeamSessionID(snapshot.CreatorID) || !slices.IsSorted(snapshot.ModelIDs) {
		return snapshot, apperrors.ErrInternal
	}
	for _, pair := range []struct {
		Values []string
		Prefix string
	}{{snapshot.ModelIDs, "mdl_"}, {snapshot.InitialRequestIDs, "pmr_"}} {
		seen := map[string]bool{}
		for _, value := range pair.Values {
			if !strings.HasPrefix(value, pair.Prefix) || !safeTeamSessionID(value) || seen[value] {
				return snapshot, apperrors.ErrInternal
			}
			seen[value] = true
		}
	}
	users, relationships := map[string]bool{}, map[string]bool{}
	for _, manager := range snapshot.Managers {
		if !strings.HasPrefix(manager.ID, "pmg_") || !safeTeamSessionID(manager.ID) || !strings.HasPrefix(manager.UserID, "usr_") || !safeTeamSessionID(manager.UserID) || users[manager.UserID] || relationships[manager.ID] {
			return snapshot, apperrors.ErrInternal
		}
		users[manager.UserID], relationships[manager.ID] = true, true
	}
	normalized, err := limits.Normalize(snapshot.Policy)
	if err != nil || !reflect.DeepEqual(normalized, snapshot.Policy) {
		return snapshot, apperrors.ErrInternal
	}
	return snapshot, nil
}

func projectCreationDirectAllowed(context ProjectCreationContext, resources *ProjectInitialResources) bool {
	return resources == nil || (len(resources.ModelIDs) == 0 || context.CanSetModels) && (!resources.hasLimits() || context.CanSetLimits)
}

func exactProjectCreationRecord(tx *gorm.DB, projectID string) (*ResourceRecord, error) {
	result := &ResourceRecord{Managers: []ResourcePerson{}, ModelIDs: []string{}}
	if err := tx.Table("projects").Where(database.ExactText(tx, clause.Column{Name: "id"}, projectID)).Take(result).Error; err != nil {
		return nil, err
	}
	if result.ID != projectID {
		return nil, apperrors.ErrNotFound
	}
	if err := tx.Table("project_managers AS m").Select("m.id,m.user_id,u.name,u.email").
		Joins("JOIN users AS u ON ?", database.ExactTextColumns(tx, clause.Column{Table: "u", Name: "id"}, clause.Column{Table: "m", Name: "user_id"})).
		Where(database.ExactText(tx, clause.Column{Table: "m", Name: "project_id"}, projectID)).Order("m.id").Scan(&result.Managers).Error; err != nil {
		return nil, err
	}
	if err := personalExact(tx.Model(&entity.ProjectModelGrant{}), "project_id", projectID).Order("model_id").Pluck("model_id", &result.ModelIDs).Error; err != nil {
		return nil, err
	}
	// Persisted legacy identifiers may contain case distinctions. Canonicalize
	// set order in Go instead of relying on a database's default text collation.
	slices.Sort(result.ModelIDs)
	return result, nil
}

func projectCreationConfigurationMatches(tx *gorm.DB, receipt entity.ProjectCreationReceipt, current *ResourceRecord) (bool, error) {
	snapshot, err := readProjectCreationSnapshot(receipt)
	if err != nil {
		return false, err
	}
	if current == nil || current.ID != snapshot.ProjectID || current.Status != entity.ResourceActive || current.CreatorID != snapshot.CreatorID || !slices.Equal(current.ModelIDs, snapshot.ModelIDs) {
		return false, nil
	}
	managers := make([]ProjectCreationManagerSnapshot, 0, len(current.Managers))
	for _, manager := range current.Managers {
		managers = append(managers, ProjectCreationManagerSnapshot{ID: manager.ID, UserID: manager.UserID})
	}
	if !reflect.DeepEqual(managers, snapshot.Managers) {
		return false, nil
	}
	for _, manager := range managers {
		if _, err := exactEnabledActor(tx, manager.UserID); err != nil {
			if errors.Is(err, apperrors.ErrUnauthorized) {
				return false, nil
			}
			return false, err
		}
	}
	for _, modelID := range snapshot.ModelIDs {
		var model entity.Model
		if err := personalExact(tx, "id", modelID).Take(&model).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return false, nil
			}
			return false, err
		}
		if model.ID != modelID || model.Status != entity.ResourceActive {
			return false, nil
		}
		var name entity.ModelName
		if err := personalExact(personalExact(tx, "model_id", modelID), "current_model_id", modelID).Take(&name).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return false, nil
			}
			return false, err
		}
	}
	row, policy, err := readProjectQuotaPolicy(tx, receipt.ProjectID, false)
	if err != nil {
		return false, err
	}
	if row.ETag != snapshot.PolicyETag || !reflect.DeepEqual(policy, snapshot.Policy) {
		return false, nil
	}
	if snapshot.Policy.MoneyMonth != nil {
		var setting entity.PricingSetting
		if err := tx.First(&setting, 1).Error; err != nil {
			return false, err
		}
		if setting.PlatformCurrency != snapshot.Policy.Currency {
			return false, nil
		}
	}
	return true, nil
}

func projectCreationCurrent(tx *gorm.DB, actorID, projectID string) (*ResourceRecord, error) {
	if err := resourceAccess(tx, actorID, ProjectResource, projectID); err != nil {
		if errors.Is(err, apperrors.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	result, err := exactProjectCreationRecord(tx, projectID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return result, err
}

func (s *Service) CreateProjectWithInitialResources(ctx context.Context, actorID string, input ProjectCreationInput) (*ProjectCreationResult, error) {
	input, err := normalizeProjectCreationResources(input)
	if err != nil {
		return nil, err
	}
	if input.InitialRequest != nil && (input.ManagerIDs == nil || !slices.Contains(*input.ManagerIDs, actorID)) {
		return nil, apperrors.ErrBadRequest
	}
	hash := projectCreationHash(actorID, input)
	var receipt entity.ProjectCreationReceipt
	var current *ResourceRecord
	var matches bool
	s.limitMu.Lock()
	defer s.limitMu.Unlock()
	err = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		actor, err := exactEnabledActor(tx, actorID)
		if err != nil {
			return err
		}
		err = personalExact(tx, "creation_id", input.CreationID).Take(&receipt).Error
		if err == nil {
			if !projectCreationReceiptMatches(receipt, actor.ID, hash, input) {
				return catalogConflict
			}
			current, err = projectCreationCurrent(tx, actor.ID, receipt.ProjectID)
			if err != nil {
				return err
			}
			matches, err = projectCreationConfigurationMatches(tx, receipt, current)
			return err
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		context, err := projectCreationContext(tx, actor)
		if err != nil {
			return err
		}
		if !projectCreationDirectAllowed(*context, input.InitialResources) {
			return apperrors.ErrForbidden
		}
		if context.ReviewETag != input.ReviewETag {
			return catalogConflict
		}
		selected := input.InitialResources
		if input.InitialRequest != nil {
			selected = input.InitialRequest
		}
		if selected != nil && selected.MoneyMonth != nil && selected.Currency != context.PlatformCurrency {
			return catalogConflict
		}
		projectID, err := id.NewPrefixed("prj")
		if err != nil {
			return err
		}
		if err := createProjectWithManagers(tx, actor.ID, projectID, input.Name, input.Description, input.ManagerIDs); err != nil {
			return err
		}
		if selected != nil {
			if err := exactProjectCreationModels(tx, selected.ModelIDs); err != nil {
				return err
			}
		}
		policyRow, before, err := readProjectQuotaPolicy(tx, projectID, true)
		if err != nil {
			return err
		}
		policy := before
		if input.InitialResources != nil {
			for _, modelID := range input.InitialResources.ModelIDs {
				if err := tx.Create(&entity.ProjectModelGrant{ProjectID: projectID, ModelID: modelID}).Error; err != nil {
					return err
				}
			}
			if input.InitialResources.hasLimits() {
				policy, err = input.InitialResources.policy()
				if err != nil {
					return err
				}
				policyRow, err = persistResourceLimitPolicy(tx, actor.ID, resolvedLimitTarget{kind: "project", id: projectID}, policyRow, before, policy, input.InitialResources.Reason)
				if err != nil {
					return err
				}
			}
		}
		children := []string{}
		if input.InitialRequest != nil {
			children, err = createProjectInitialRequests(tx, actor.ID, projectID, input.CreationID, *input.InitialRequest, policyRow, before)
			if err != nil {
				return err
			}
		}
		if err := appendAudit(tx, actor.ID, "resource.create", string(ProjectResource), projectID); err != nil {
			return err
		}
		created, err := exactProjectCreationRecord(tx, projectID)
		if err != nil {
			return err
		}
		snapshot := ProjectCreationSnapshot{ProjectID: projectID, Name: created.Name, Description: created.Description, CreatorID: actor.ID, Managers: []ProjectCreationManagerSnapshot{}, ModelIDs: slices.Clone(created.ModelIDs), PolicyETag: policyRow.ETag, Policy: policy, InitialRequestIDs: children}
		if selected != nil {
			snapshot.Reason = selected.Reason
		}
		for _, manager := range created.Managers {
			snapshot.Managers = append(snapshot.Managers, ProjectCreationManagerSnapshot{ID: manager.ID, UserID: manager.UserID})
		}
		raw, err := json.Marshal(snapshot)
		if err != nil {
			return err
		}
		receipt = entity.ProjectCreationReceipt{CreationID: input.CreationID, ActorID: actor.ID, ProjectID: projectID, RequestHash: hash, ReviewETag: input.ReviewETag, SnapshotJSON: string(raw)}
		if err := tx.Create(&receipt).Error; err != nil {
			return err
		}
		// The first response and every retry expose the same persisted timestamp,
		// including each driver's microsecond precision.
		if err := personalExact(tx, "creation_id", input.CreationID).Take(&receipt).Error; err != nil {
			return err
		}
		if err := appendProjectCreationAudit(tx, actor.ID, receipt); err != nil {
			return err
		}
		current, err = projectCreationCurrent(tx, actor.ID, projectID)
		if err != nil {
			return err
		}
		matches, err = projectCreationConfigurationMatches(tx, receipt, current)
		return err
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, catalogError(err)
	}
	snapshot, err := readProjectCreationSnapshot(receipt)
	if err != nil {
		return nil, err
	}
	result := &ProjectCreationResult{Project: current, Receipt: ProjectCreationReceiptRecord{CreationID: receipt.CreationID, ProjectID: receipt.ProjectID, CreatedAt: receipt.CreatedAt.UTC(), InitialRequestIDs: slices.Clone(snapshot.InitialRequestIDs)}, Committed: true, ApplicationStatus: "unavailable"}
	if current == nil {
		return result, nil
	}
	if !matches {
		result.ApplicationStatus = "superseded"
		return result, nil
	}
	// Publication failure leaves the committed immutable receipt reviewable.
	if s.RefreshRuntime(ctx) != nil {
		result.ApplicationStatus = "pending"
		return result, nil
	}
	result.RuntimeApplied, result.ApplicationStatus = s.projectCreationApplication(receipt, current)
	return result, nil
}

func projectInitialRequestInputs(creationID string, resources ProjectInitialResources) []ProjectRequestInput {
	inputs := []ProjectRequestInput{}
	if len(resources.ModelIDs) > 0 {
		inputs = append(inputs, ProjectRequestInput{Kind: entity.ProjectRequestModelAccess, RequestID: creationID + "_models", ModelIDs: resources.ModelIDs, Reason: resources.Reason})
	}
	if resources.hasQuota() {
		inputs = append(inputs, ProjectRequestInput{Kind: entity.ProjectRequestQuota, RequestID: creationID + "_quota", Quota: &ProjectQuotaPatch{TokensMonth: resources.TokensMonth, MoneyMonth: resources.MoneyMonth, Currency: resources.Currency}, Reason: resources.Reason})
	}
	if resources.hasRates() {
		inputs = append(inputs, ProjectRequestInput{Kind: entity.ProjectRequestRateLimit, RequestID: creationID + "_rates", RateLimit: &ProjectRateLimitPatch{RPM: resources.RPM, TPM: resources.TPM, Concurrency: resources.Concurrency}, Reason: resources.Reason})
	}
	return inputs
}

func createProjectInitialRequests(tx *gorm.DB, actorID, projectID, creationID string, resources ProjectInitialResources, row entity.ResourceLimit, baseline limits.Policy) ([]string, error) {
	inputs := projectInitialRequestInputs(creationID, resources)
	var currency entity.PricingSetting
	if err := tx.First(&currency, 1).Error; err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(inputs))
	for _, input := range inputs {
		requestID, err := id.NewPrefixed("pmr")
		if err != nil {
			return nil, err
		}
		var baselineJSON, requestedJSON []byte
		var hash string
		if input.Kind == entity.ProjectRequestModelAccess {
			baselineJSON = []byte("[]")
			requestedJSON, err = json.Marshal(input.ModelIDs)
			if err != nil {
				return nil, err
			}
			hash, err = projectModelRequestHash(actorID, projectID, input)
		} else {
			input.ReviewETag, err = projectQuotaReviewETag(projectID, row, baseline, currency, nil)
			if err != nil {
				return nil, err
			}
			baselineJSON, err = projectLimitBaseline(input.Kind, baseline, currency.PlatformCurrency)
			if err != nil {
				return nil, err
			}
			requestedJSON, hash, err = projectLimitCreationIntent(actorID, projectID, input)
		}
		if err != nil {
			return nil, err
		}
		request := entity.ProjectModelRequest{ID: requestID, RequestID: input.RequestID, RequestHash: hash, ProjectID: projectID, ApplicantUserID: actorID, Kind: input.Kind, BaselinePolicyETag: row.ETag, BaselineJSON: string(baselineJSON), RequestedJSON: string(requestedJSON), Reason: resources.Reason, Status: entity.ProjectRequestPending}
		if err := tx.Create(&request).Error; err != nil {
			return nil, err
		}
		if err := appendAudit(tx, actorID, "project.request.create", string(ProjectResource), projectID); err != nil {
			return nil, err
		}
		ids = append(ids, requestID)
	}
	return ids, nil
}
