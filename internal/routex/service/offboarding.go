package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"sort"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/id"
)

type OffboardingProjectAssignment struct {
	ProjectID      string   `json:"project_id"`
	ManagerUserIDs []string `json:"manager_user_ids"`
}
type OffboardingTeamAssignment struct {
	TeamID           string   `json:"team_id"`
	OwnerUserIDs     []string `json:"owner_user_ids"`
	AddMemberUserIDs []string `json:"add_member_user_ids"`
}
type OffboardingAssignments struct {
	Projects []OffboardingProjectAssignment `json:"project_assignments"`
	Teams    []OffboardingTeamAssignment    `json:"team_assignments"`
}
type OffboardingPlanInput struct {
	RequestID        string    `json:"request_id"`
	InventoryVersion string    `json:"inventory_version"`
	PlannedAt        time.Time `json:"planned_at"`
	Reason           string    `json:"reason"`
	OffboardingAssignments
}
type OffboardingEmergencyInput struct {
	RequestID       string                      `json:"request_id"`
	CurrentPassword string                      `json:"-"`
	Reason          string                      `json:"reason"`
	Teams           []OffboardingTeamAssignment `json:"team_assignments"`
}

func normalizeOffboardingAssignments(input OffboardingAssignments, departing string) (OffboardingAssignments, error) {
	if len(input.Projects) > 1000 || len(input.Teams) > 1000 {
		return input, apperrors.ErrBadRequest
	}
	input.Projects = append([]OffboardingProjectAssignment{}, input.Projects...)
	input.Teams = append([]OffboardingTeamAssignment{}, input.Teams...)
	seen := map[string]bool{}
	for i := range input.Projects {
		item := &input.Projects[i]
		if item.ProjectID == "" || len(item.ProjectID) > 30 || seen[item.ProjectID] {
			return input, apperrors.ErrBadRequest
		}
		seen[item.ProjectID] = true
		ids, err := offboardingUserIDs(item.ManagerUserIDs, departing)
		if err != nil {
			return input, err
		}
		item.ManagerUserIDs = ids
	}
	seen = map[string]bool{}
	for i := range input.Teams {
		item := &input.Teams[i]
		if item.TeamID == "" || len(item.TeamID) > 30 || seen[item.TeamID] {
			return input, apperrors.ErrBadRequest
		}
		seen[item.TeamID] = true
		ids, err := offboardingUserIDs(item.OwnerUserIDs, departing)
		if err != nil {
			return input, err
		}
		item.OwnerUserIDs = ids
		ids, err = offboardingUserIDs(item.AddMemberUserIDs, departing)
		if err != nil {
			return input, err
		}
		item.AddMemberUserIDs = ids
		for _, added := range ids {
			if !slices.Contains(item.OwnerUserIDs, added) {
				return input, apperrors.ErrBadRequest
			}
		}
	}
	sort.Slice(input.Projects, func(i, j int) bool { return input.Projects[i].ProjectID < input.Projects[j].ProjectID })
	sort.Slice(input.Teams, func(i, j int) bool { return input.Teams[i].TeamID < input.Teams[j].TeamID })
	return input, nil
}
func offboardingUserIDs(ids []string, departing string) ([]string, error) {
	if len(ids) > 1000 {
		return nil, apperrors.ErrBadRequest
	}
	result := append([]string{}, ids...)
	slices.Sort(result)
	for i, id := range result {
		if id == "" || len(id) > 30 || id == departing || (i > 0 && result[i-1] == id) {
			return nil, apperrors.ErrBadRequest
		}
	}
	return result, nil
}
func offboardingDigest(value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}
func offboardingActor(tx *gorm.DB, actorID, userID string, emergency bool) error {
	if err := authorizeGovernance(tx, actorID, "members.write"); err != nil {
		return err
	}
	var actor, target entity.User
	if err := tx.Select("id", "role", "disabled").First(&actor, "id = ?", actorID).Error; err != nil {
		return err
	}
	if err := tx.Select("id", "role", "disabled").First(&target, "id = ?", userID).Error; err != nil {
		return err
	}
	if emergency && actor.Role != entity.RoleAdmin {
		return apperrors.ErrForbidden
	}
	if target.Role == entity.RoleAdmin && actor.Role != entity.RoleAdmin {
		return apperrors.ErrForbidden
	}
	return nil
}
func validateOffboardingTarget(inventory *OffboardingInventory, actorID string) error {
	if inventory.Disabled || inventory.LastAdministrator {
		return catalogConflict
	}
	if inventory.UserID == actorID {
		return apperrors.ErrForbidden
	}
	return nil
}
func lockOffboardingUsers(tx *gorm.DB, actorID, userID string, assignments OffboardingAssignments) error {
	ids := []string{actorID, userID}
	for _, a := range assignments.Projects {
		ids = append(ids, a.ManagerUserIDs...)
	}
	for _, a := range assignments.Teams {
		ids = append(ids, a.OwnerUserIDs...)
		ids = append(ids, a.AddMemberUserIDs...)
	}
	slices.Sort(ids)
	ids = slices.Compact(ids)
	var users []entity.User
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").Where("id IN ?", ids).Order("id").Find(&users).Error; err != nil {
		return err
	}
	if len(users) != len(ids) {
		return apperrors.ErrBadRequest
	}
	return nil
}
func replayOffboardingCase(tx *gorm.DB, requestID, requestHash, actorID, userID, mode string) (*entity.OffboardingCase, error) {
	var row entity.OffboardingCase
	err := tx.First(&row, "request_id = ?", requestID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if row.RequestHash != requestHash || row.ActorID != actorID || row.UserID != userID || row.Mode != mode {
		return nil, catalogConflict
	}
	return &row, nil
}
func newOffboardingCase(tx *gorm.DB, actorID, userID, requestID, requestHash, mode, reason string, plannedAt *time.Time, inventory *OffboardingInventory, assignments OffboardingAssignments) (*entity.OffboardingCase, error) {
	caseID, err := id.NewPrefixed("off")
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(inventory)
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(assignments)
	if err != nil {
		return nil, err
	}
	row := &entity.OffboardingCase{ID: caseID, RequestID: requestID, RequestHash: requestHash, UserID: userID, ActorID: actorID, Mode: mode, Status: "ready_to_complete", Reason: reason, PlannedAt: plannedAt, InventoryVersion: inventory.InventoryVersion, InventoryJSON: string(raw), AssignmentsJSON: string(encoded)}
	if err := tx.Create(row).Error; err != nil {
		return nil, err
	}
	return row, nil
}

func (s *Service) CreateOffboardingPlan(ctx context.Context, actorID, userID string, input OffboardingPlanInput) (*OffboardingCaseRecord, error) {
	input.Reason = strings.TrimSpace(input.Reason)
	input.PlannedAt = input.PlannedAt.UTC().Truncate(time.Microsecond)
	if !safeCallID.MatchString(input.RequestID) || len(input.InventoryVersion) != 64 || input.PlannedAt.IsZero() || input.Reason == "" || !validResourceDescription(input.Reason) {
		return nil, apperrors.ErrBadRequest
	}
	assignments, err := normalizeOffboardingAssignments(input.OffboardingAssignments, userID)
	if err != nil {
		return nil, err
	}
	input.OffboardingAssignments = assignments
	digest, err := offboardingDigest(input)
	if err != nil {
		return nil, apperrors.ErrInternal
	}
	var result *entity.OffboardingCase
	err = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		if err := offboardingActor(tx, actorID, userID, false); err != nil {
			return err
		}
		previous, err := replayOffboardingCase(tx, input.RequestID, digest, actorID, userID, "planned")
		if err != nil {
			return err
		}
		if previous != nil {
			result = previous
			return nil
		}
		if err := lockOffboardingUsers(tx, actorID, userID, assignments); err != nil {
			return err
		}
		inventory, err := loadOffboardingInventory(tx, userID)
		if err != nil {
			return err
		}
		if err := validateOffboardingTarget(inventory, actorID); err != nil {
			return err
		}
		if inventory.InventoryVersion != input.InventoryVersion {
			return catalogConflict
		}
		if err := validateOffboardingAssignments(tx, inventory, assignments); err != nil {
			return err
		}
		result, err = newOffboardingCase(tx, actorID, userID, input.RequestID, digest, "planned", input.Reason, &input.PlannedAt, inventory, assignments)
		if err != nil {
			return err
		}
		return appendAudit(tx, actorID, "offboarding.plan", "offboarding_case", result.ID)
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, catalogError(err)
	}
	resultRecord, err := offboardingCaseRecord(*result)
	return resultRecord, catalogError(err)
}

func (s *Service) CompleteOffboarding(ctx context.Context, actorID, userID, caseID string) (*OffboardingCaseRecord, error) {
	var result entity.OffboardingCase
	var affected []string
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		if err := offboardingActor(tx, actorID, userID, false); err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&result, "id = ? AND user_id = ?", caseID, userID).Error; err != nil {
			return err
		}
		if result.Status == "completed" {
			return nil
		}
		if result.Mode != "planned" || result.Status != "ready_to_complete" {
			return catalogConflict
		}
		var assignments OffboardingAssignments
		if err := json.Unmarshal([]byte(result.AssignmentsJSON), &assignments); err != nil {
			return err
		}
		if err := lockOffboardingUsers(tx, actorID, userID, assignments); err != nil {
			return err
		}
		inventory, err := loadOffboardingInventory(tx, userID)
		if err != nil {
			return err
		}
		if err := validateOffboardingTarget(inventory, actorID); err != nil {
			return err
		}
		if inventory.InventoryVersion != result.InventoryVersion {
			return catalogConflict
		}
		if err := validateOffboardingAssignments(tx, inventory, assignments); err != nil {
			return err
		}
		affected, err = applyOffboarding(tx, actorID, inventory, assignments, &result)
		return err
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	return s.finishOffboardingMutation(ctx, userID, result, affected, err)
}

func (s *Service) EmergencyOffboarding(ctx context.Context, actorID, userID string, input OffboardingEmergencyInput) (*OffboardingCaseRecord, error) {
	input.Reason = strings.TrimSpace(input.Reason)
	if !safeCallID.MatchString(input.RequestID) || input.Reason == "" || !validResourceDescription(input.Reason) || !validPassword(input.CurrentPassword) {
		return nil, apperrors.ErrBadRequest
	}
	assignments, err := normalizeOffboardingAssignments(OffboardingAssignments{Teams: input.Teams}, userID)
	if err != nil {
		return nil, err
	}
	input.Teams = assignments.Teams
	// Reauthentication occurs before taking the shared governance lock. The hash
	// is checked again under the actor row lock, so a concurrent password change
	// invalidates the proof. Password material never enters the request digest.
	var actor entity.User
	if err := s.authDB(ctx).First(&actor, "id = ? AND disabled = ? AND role = ?", actorID, false, entity.RoleAdmin).Error; err != nil {
		return nil, apperrors.ErrForbidden
	}
	if bcrypt.CompareHashAndPassword([]byte(actor.PasswordHash), []byte(input.CurrentPassword)) != nil {
		return nil, apperrors.ErrUnauthorized
	}
	digest, err := offboardingDigest(input)
	if err != nil {
		return nil, apperrors.ErrInternal
	}
	var result *entity.OffboardingCase
	var affected []string
	err = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		if err := offboardingActor(tx, actorID, userID, true); err != nil {
			return err
		}
		previous, err := replayOffboardingCase(tx, input.RequestID, digest, actorID, userID, "emergency")
		if err != nil {
			return err
		}
		inventory, err := loadOffboardingInventory(tx, userID)
		if err != nil {
			return err
		}
		if previous == nil {
			if err := validateOffboardingTarget(inventory, actorID); err != nil {
				return err
			}
			assignments = emergencyOffboardingAssignments(inventory, actorID, assignments)
		}
		if err := lockOffboardingUsers(tx, actorID, userID, assignments); err != nil {
			return err
		}
		var current entity.User
		if err := tx.First(&current, "id = ?", actorID).Error; err != nil {
			return err
		}
		if current.Disabled || current.PasswordHash != actor.PasswordHash {
			return apperrors.ErrUnauthorized
		}
		if previous != nil {
			result = previous
			return nil
		}
		inventory, err = loadOffboardingInventory(tx, userID)
		if err != nil {
			return err
		}
		if err := validateOffboardingAssignments(tx, inventory, assignments); err != nil {
			return err
		}
		result, err = newOffboardingCase(tx, actorID, userID, input.RequestID, digest, "emergency", input.Reason, nil, inventory, assignments)
		if err != nil {
			return err
		}
		affected, err = applyOffboarding(tx, actorID, inventory, assignments, result)
		return err
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, catalogError(err)
	}
	return s.finishOffboardingMutation(ctx, userID, *result, affected, nil)
}

func (s *Service) finishOffboardingMutation(ctx context.Context, userID string, row entity.OffboardingCase, projects []string, err error) (*OffboardingCaseRecord, error) {
	if err != nil {
		return nil, catalogError(err)
	}
	s.InvalidateRuntimeUser(userID)
	for _, projectID := range projects {
		s.InvalidateRuntimeProject(projectID)
	}
	if err := s.refreshAfterMutation(ctx, nil); err != nil {
		return nil, err
	}
	result, err := offboardingCaseRecord(row)
	return result, catalogError(err)
}
