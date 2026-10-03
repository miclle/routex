package service

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/id"
)

type ResourceKind string

const (
	TeamResource    ResourceKind = "teams"
	ProjectResource ResourceKind = "projects"
)

type ResourcePerson struct{ ID, UserID, Name, Email, Role, Status string }
type ResourceRecord struct {
	RequestWorkspaceOnly                     bool `gorm:"-"`
	ResourceLimitWorkspaceOnly               bool `gorm:"-"`
	ID, Name, Description, Status, CreatorID string
	CreatedAt                                time.Time
	Members                                  []ResourcePerson   `gorm:"-"`
	Managers                                 []ResourcePerson   `gorm:"-"`
	ModelIDs                                 []string           `gorm:"-"`
	KeyCount                                 *int64             `gorm:"-"`
	ProjectListLimits                        *ProjectListLimits `gorm:"-"`
}
type ResourceFilter struct {
	Query, Status, Cursor string
	Limit                 int
}
type ResourcePage struct {
	Items      []ResourceRecord
	NextCursor string
}
type ResourceUpdate struct{ Name, Description, Status *string }
type TeamMemberInput struct{ UserID, Role, Status string }

func resourceKindValid(kind ResourceKind) bool {
	return kind == TeamResource || kind == ProjectResource
}
func validResourceDescription(value string) bool {
	return utf8.ValidString(value) && utf8.RuneCountInString(value) <= 2000 && !strings.ContainsFunc(value, func(r rune) bool { return unicode.IsControl(r) && r != '\n' && r != '\t' })
}
func resourcePermission(db *gorm.DB, actorID string, kind ResourceKind, action string) (bool, error) {
	if kind == ProjectResource {
		actor, err := exactEnabledActor(db, actorID)
		if err != nil {
			return false, err
		}
		return exactGovernancePermission(db, actor, "projects."+action)
	}
	permissions, err := permissionsFor(db, actorID)
	return slices.Contains(permissions, string(kind)+"."+action), err
}
func resourceManager(db *gorm.DB, actorID, projectID string) (bool, error) {
	if _, err := exactEnabledActor(db, actorID); err != nil {
		return false, err
	}
	return exactProjectRequestManager(db, actorID, projectID)
}
func resourceAccess(db *gorm.DB, actorID string, kind ResourceKind, resourceID string) error {
	if kind == TeamResource {
		_, err := teamRoleTargetAccess(db, actorID, resourceID, false)
		return err
	}
	for _, action := range []string{"read_all", "write", "models.write"} {
		allowed, err := resourcePermission(db, actorID, kind, action)
		if err != nil {
			return err
		}
		if allowed {
			return nil
		}
	}
	manager, err := resourceManager(db, actorID, resourceID)
	if err != nil {
		return err
	}
	if !manager {
		return apperrors.ErrNotFound
	}
	return nil
}
func resourceRecord(db *gorm.DB, kind ResourceKind, resourceID string, lock bool) (*ResourceRecord, error) {
	result := &ResourceRecord{ModelIDs: []string{}}
	query := db.Table(string(kind)).Where("id = ?", resourceID)
	if kind == ProjectResource {
		query = db.Table(string(kind)).Where(database.ExactText(db, clause.Column{Name: "id"}, resourceID))
	}
	if lock {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	if err := query.Take(result).Error; err != nil {
		return nil, err
	}
	if kind == ProjectResource && result.ID != resourceID {
		return nil, apperrors.ErrNotFound
	}
	if kind == TeamResource {
		result.Members = []ResourcePerson{}
		if err := db.Table("team_memberships m").Select("m.id,m.user_id,u.name,u.email,m.role,m.status").Joins("JOIN users u ON u.id = m.user_id").Where("m.team_id = ?", resourceID).Order("m.id").Scan(&result.Members).Error; err != nil {
			return nil, err
		}
		if err := db.Model(&entity.TeamModelGrant{}).Where("team_id = ?", resourceID).Order("model_id").Pluck("model_id", &result.ModelIDs).Error; err != nil {
			return nil, err
		}
	} else {
		result.Managers = []ResourcePerson{}
		if err := db.Table("project_managers m").Select("m.id,m.user_id,u.name,u.email").Joins("JOIN users u ON u.id = m.user_id").Where("m.project_id = ?", resourceID).Order("m.id").Scan(&result.Managers).Error; err != nil {
			return nil, err
		}
		if err := db.Model(&entity.ProjectModelGrant{}).Where("project_id = ?", resourceID).Order("model_id").Pluck("model_id", &result.ModelIDs).Error; err != nil {
			return nil, err
		}
	}
	return result, nil
}
func (s *Service) GetResource(ctx context.Context, actorID string, kind ResourceKind, resourceID string) (*ResourceRecord, error) {
	if !resourceKindValid(kind) {
		return nil, apperrors.ErrBadRequest
	}
	db := s.authDB(ctx)
	if err := resourceAccess(db, actorID, kind, resourceID); err != nil {

		if kind == TeamResource && errors.Is(err, apperrors.ErrNotFound) {
			actor, actorErr := exactEnabledActor(db, actorID)
			if actorErr != nil {
				return nil, catalogError(actorErr)
			}
			fields, permissionErr := teamLimitEditableFields(db, actor, "team")
			if permissionErr != nil {
				return nil, catalogError(permissionErr)
			}
			if len(fields) > 0 {
				minimal := &ResourceRecord{ResourceLimitWorkspaceOnly: true}
				queryErr := db.Model(&entity.Team{}).Select("id", "name", "description", "status").Where(database.ExactText(db, clause.Column{Name: "id"}, resourceID)).Take(minimal).Error
				if queryErr != nil {
					return nil, catalogError(queryErr)
				}
				if minimal.ID != resourceID {
					return nil, apperrors.ErrNotFound
				}
				return minimal, nil
			}
		}
		if kind == ProjectResource && errors.Is(err, apperrors.ErrNotFound) {
			actor, actorErr := exactEnabledActor(db, actorID)
			if actorErr != nil {
				return nil, catalogError(actorErr)
			}
			allowed, permissionErr := exactGovernancePermission(db, actor, "projects.limits.write")
			if permissionErr != nil {
				return nil, catalogError(permissionErr)
			}
			if allowed {
				minimal := &ResourceRecord{RequestWorkspaceOnly: true, ModelIDs: []string{}, Managers: []ResourcePerson{}}
				queryErr := db.Model(&entity.Project{}).Select("id", "name", "description", "status").Where(database.ExactText(db, clause.Column{Name: "id"}, resourceID)).Take(minimal).Error
				if queryErr != nil {
					return nil, catalogError(queryErr)
				}
				if minimal.ID != resourceID {
					return nil, apperrors.ErrNotFound
				}
				return minimal, nil
			}
		}
		return nil, catalogError(err)
	}
	result, err := resourceRecord(db, kind, resourceID, false)
	return result, catalogError(err)
}
func (s *Service) ListResources(ctx context.Context, actorID string, kind ResourceKind, all bool, filter ResourceFilter) (*ResourcePage, error) {
	if !resourceKindValid(kind) {
		return nil, apperrors.ErrBadRequest
	}
	if filter.Limit == 0 {
		filter.Limit = 40
	}
	if filter.Limit < 1 || filter.Limit > 100 || len(filter.Cursor) > 30 || len(filter.Query) > 200 || !utf8.ValidString(filter.Query) || (filter.Status != "" && filter.Status != entity.ResourceActive && filter.Status != entity.ResourceDisabled && filter.Status != entity.ResourceArchived) {
		return nil, apperrors.ErrBadRequest
	}
	if kind == ProjectResource {
		return s.listProjects(ctx, actorID, all, filter)
	}
	db := s.authDB(ctx)
	allowed, err := resourcePermission(db, actorID, kind, "read_all")
	if err != nil {
		return nil, catalogError(err)
	}
	if all && !allowed {
		return nil, apperrors.ErrForbidden
	}
	query := db.Table(string(kind))
	// Personal lists remain scoped even for platform administrators.
	if !all {
		if kind == TeamResource {
			query = query.Where("id IN (?)", db.Model(&entity.TeamMembership{}).Select("team_id").Where("user_id = ? AND status = ?", actorID, entity.ResourceActive))
		} else {
			query = query.Where("id IN (?)", db.Model(&entity.ProjectManager{}).Select("project_id").Where("user_id = ?", actorID))
		}
	}
	if filter.Query != "" {
		escaped := strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(strings.ToLower(filter.Query))
		query = query.Where("LOWER(name) LIKE ? ESCAPE '!'", "%"+escaped+"%")
	}
	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	}
	if filter.Cursor != "" {
		query = query.Where("id > ?", filter.Cursor)
	}
	var ids []string
	if err := query.Order("id").Limit(filter.Limit+1).Pluck("id", &ids).Error; err != nil {
		return nil, catalogError(err)
	}
	page := &ResourcePage{Items: []ResourceRecord{}}
	if len(ids) > filter.Limit {
		ids = ids[:filter.Limit]
		page.NextCursor = ids[len(ids)-1]
	}
	for _, resourceID := range ids {
		record, err := resourceRecord(db, kind, resourceID, false)
		if err != nil {
			return nil, catalogError(err)
		}
		page.Items = append(page.Items, *record)
	}
	return page, nil
}
func activeResourceUsers(tx *gorm.DB, userIDs []string) error {
	if len(userIDs) == 0 || len(userIDs) > 1000 {
		return apperrors.ErrBadRequest
	}
	seen := map[string]bool{}
	for _, userID := range userIDs {
		if userID == "" || seen[userID] {
			return apperrors.ErrBadRequest
		}
		seen[userID] = true
	}
	var count int64
	if err := tx.Model(&entity.User{}).Where("id IN ? AND disabled = ?", userIDs, false).Count(&count).Error; err != nil {
		return err
	}
	if count != int64(len(userIDs)) {
		return apperrors.ErrBadRequest
	}
	return nil
}

func projectIdentityQuery(query *gorm.DB, ids []string) *gorm.DB {
	predicates := make([]clause.Expression, 0, len(ids))
	for _, value := range ids {
		predicates = append(predicates, database.ExactText(query, clause.Column{Name: "id"}, value))
	}
	return query.Where(clause.Or(predicates...))
}

func projectSelectionMatches(selectedIDs, submittedIDs []string) bool {
	if len(selectedIDs) != len(submittedIDs) {
		return false
	}
	selected := make(map[string]bool, len(selectedIDs))
	for _, value := range selectedIDs {
		if value == "" || selected[value] {
			return false
		}
		selected[value] = true
	}
	for _, value := range submittedIDs {
		if !selected[value] {
			return false
		}
		delete(selected, value)
	}
	return len(selected) == 0
}

func projectManagerIdentities(rows []entity.ProjectManager, projectID string, selectedUserIDs []string) map[string]string {
	selected := make(map[string]bool, len(selectedUserIDs))
	for _, userID := range selectedUserIDs {
		selected[userID] = true
	}
	identities := make(map[string]string, len(rows))
	for _, manager := range rows {
		if manager.ProjectID == projectID && selected[manager.UserID] {
			identities[manager.UserID] = manager.ID
		}
	}
	return identities
}

func activeProjectResourceUsers(tx *gorm.DB, userIDs []string) error {
	if len(userIDs) == 0 || len(userIDs) > 1000 {
		return apperrors.ErrBadRequest
	}
	var rows []entity.User
	if err := projectIdentityQuery(tx.Model(&entity.User{}), userIDs).Select("id", "disabled", "offboarded_at").
		Where("disabled = ? AND offboarded_at IS NULL", false).Find(&rows).Error; err != nil {
		return err
	}
	selected := make([]string, 0, len(rows))
	for _, row := range rows {
		if row.Disabled || row.OffboardedAt != nil {
			return apperrors.ErrBadRequest
		}
		selected = append(selected, row.ID)
	}
	if !projectSelectionMatches(selected, userIDs) {
		return apperrors.ErrBadRequest
	}
	return nil
}
func (s *Service) CreateResource(ctx context.Context, actorID string, kind ResourceKind, name, description string, owners []string) (*ResourceRecord, error) {
	name = strings.TrimSpace(name)
	if !resourceKindValid(kind) || !validCatalogLabel(name) || !validResourceDescription(description) {
		return nil, apperrors.ErrBadRequest
	}
	prefix := "tea"
	if kind == ProjectResource {
		prefix = "prj"
	}
	resourceID, err := id.NewPrefixed(prefix)
	if err != nil {
		return nil, apperrors.ErrInternal
	}
	var result *ResourceRecord
	err = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		if kind == TeamResource {
			if err := authorizeGovernance(tx, actorID, "teams.write"); err != nil {
				return err
			}
			if err := activeResourceUsers(tx, owners); err != nil {
				return err
			}
			team := entity.Team{ID: resourceID, Name: name, Description: description, Status: entity.ResourceActive}
			if err := tx.Create(&team).Error; err != nil {
				return err
			}
			if err := applyCreationDefaultLimit(tx, "team", team.ID, actorID); err != nil {
				return err
			}
			for _, owner := range owners {
				relationID, err := id.NewPrefixed("tmm")
				if err != nil {
					return err
				}
				if err := tx.Create(&entity.TeamMembership{ID: relationID, TeamID: resourceID, UserID: owner, Role: entity.TeamOwner, Status: entity.ResourceActive}).Error; err != nil {
					return err
				}
			}
		} else {
			if err := createProjectWithManagers(tx, actorID, resourceID, name, description, nil); err != nil {
				return err
			}
		}
		if err := appendAudit(tx, actorID, "resource.create", string(kind), resourceID); err != nil {
			return err
		}
		var err error
		result, err = resourceRecord(tx, kind, resourceID, false)
		return err
	})
	return result, catalogError(err)
}
func (s *Service) UpdateResource(ctx context.Context, actorID string, kind ResourceKind, resourceID string, input ResourceUpdate) (*ResourceRecord, error) {
	if !resourceKindValid(kind) || (input.Name == nil && input.Description == nil && input.Status == nil) {
		return nil, apperrors.ErrBadRequest
	}
	if input.Name != nil {
		value := strings.TrimSpace(*input.Name)
		input.Name = &value
		if !validCatalogLabel(value) {
			return nil, apperrors.ErrBadRequest
		}
	}
	if input.Description != nil && !validResourceDescription(*input.Description) {
		return nil, apperrors.ErrBadRequest
	}
	if input.Status != nil && *input.Status != entity.ResourceActive && *input.Status != entity.ResourceDisabled && *input.Status != entity.ResourceArchived {
		return nil, apperrors.ErrBadRequest
	}
	var result *ResourceRecord
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		allowed, err := resourcePermission(tx, actorID, kind, "write")
		if kind == TeamResource {
			actor, actorErr := exactEnabledActor(tx, actorID)
			if actorErr != nil {
				return actorErr
			}
			allowed, err = exactGovernancePermission(tx, actor, "teams.write")
			// Inherited Team actions manage metadata, never Team lifecycle.
			if err == nil && !allowed && input.Status == nil {
				allowed, err = teamTargetActionAllowed(tx, actorID, resourceID, "teams.write")
			}
		}
		if err != nil {
			return err
		}
		if !allowed && kind == ProjectResource && input.Status == nil {
			allowed, err = resourceManager(tx, actorID, resourceID)
			if err != nil {
				return err
			}
		}
		if !allowed {
			return apperrors.ErrForbidden
		}
		current, err := resourceRecord(tx, kind, resourceID, true)
		if err != nil {
			return err
		}
		if kind == TeamResource && current.ID != resourceID {
			return apperrors.ErrNotFound
		}
		if current.Status == entity.ResourceArchived {
			return catalogConflict
		}
		if input.Status != nil && *input.Status == entity.ResourceArchived && current.Status != entity.ResourceDisabled {
			return catalogConflict
		}
		updates := map[string]any{"updated_at": time.Now().UTC()}
		if kind == TeamResource {
			var saved struct{ UpdatedAt time.Time }
			// resourceRecord already locked this Team. Read its persisted generation
			// without adding internal lifecycle timestamps to the public record.
			if err := quotaExact(tx.Model(&entity.Team{}), "id", current.ID).Select("updated_at").Take(&saved).Error; err != nil {
				return err
			}
			updates["updated_at"] = teamRoleAssignmentGeneration(saved.UpdatedAt, time.Now())
		}
		if input.Name != nil {
			updates["name"] = *input.Name
		}
		if input.Description != nil {
			updates["description"] = *input.Description
		}
		if input.Status != nil {
			updates["status"] = *input.Status
			if kind == TeamResource && *input.Status != entity.ResourceActive {
				if err := CancelTeamModelRequestsForTeam(tx, actorID, current.ID, "team_unavailable"); err != nil {
					return err
				}
				if err := cancelTeamQuotaRequestsForTeam(tx, current.ID, "team_unavailable"); err != nil {
					return err
				}
			}
		}
		if err := tx.Table(string(kind)).Where("id = ?", resourceID).Updates(updates).Error; err != nil {
			return err
		}
		if err := appendAudit(tx, actorID, "resource.update", string(kind), resourceID); err != nil {
			return err
		}
		result, err = resourceRecord(tx, kind, resourceID, false)
		return err
	})
	if kind == ProjectResource {
		if err == nil && input.Status != nil && *input.Status != entity.ResourceActive {
			s.InvalidateRuntimeProject(resourceID)
		}
		return result, s.refreshAfterMutation(ctx, catalogError(err))
	}
	if err == nil {
		s.invalidateRuntimeTeam(result.ID)
	}
	return result, s.refreshAfterMutation(ctx, catalogError(err))
}
