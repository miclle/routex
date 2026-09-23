package service

import (
	"context"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

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
	ID, Name, Description, Status, CreatorID string
	CreatedAt                                time.Time
	Members                                  []ResourcePerson `gorm:"-"`
	Managers                                 []ResourcePerson `gorm:"-"`
	ModelIDs                                 []string         `gorm:"-"`
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
	permissions, err := permissionsFor(db, actorID)
	return slices.Contains(permissions, string(kind)+"."+action), err
}
func resourceManager(db *gorm.DB, actorID, projectID string) (bool, error) {
	var count int64
	err := db.Model(&entity.ProjectManager{}).Where("project_id = ? AND user_id = ?", projectID, actorID).Count(&count).Error
	return count > 0, err
}
func resourceAccess(db *gorm.DB, actorID string, kind ResourceKind, resourceID string) error {
	permissions, err := permissionsFor(db, actorID)
	if err != nil {
		return err
	}
	for _, action := range []string{"read_all", "write", "models.write"} {
		if slices.Contains(permissions, string(kind)+"."+action) {
			return nil
		}
	}
	var count int64
	if kind == TeamResource {
		err = db.Model(&entity.TeamMembership{}).Where("team_id = ? AND user_id = ? AND status = ?", resourceID, actorID, entity.ResourceActive).Count(&count).Error
	} else {
		err = db.Model(&entity.ProjectManager{}).Where("project_id = ? AND user_id = ?", resourceID, actorID).Count(&count).Error
	}
	if err != nil {
		return err
	}
	if count == 0 {
		return apperrors.ErrNotFound
	}
	return nil
}
func resourceRecord(db *gorm.DB, kind ResourceKind, resourceID string, lock bool) (*ResourceRecord, error) {
	result := &ResourceRecord{ModelIDs: []string{}}
	query := db.Table(string(kind)).Where("id = ?", resourceID)
	if lock {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	if err := query.Take(result).Error; err != nil {
		return nil, err
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
			if err := activeResourceUsers(tx, []string{actorID}); err != nil {
				return err
			}
			project := entity.Project{ID: resourceID, Name: name, Description: description, Status: entity.ResourceActive, CreatorID: actorID}
			if err := tx.Create(&project).Error; err != nil {
				return err
			}
			relationID, err := id.NewPrefixed("pmg")
			if err != nil {
				return err
			}
			if err := tx.Create(&entity.ProjectManager{ID: relationID, ProjectID: resourceID, UserID: actorID}).Error; err != nil {
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
		if current.Status == entity.ResourceArchived {
			return catalogConflict
		}
		if input.Status != nil && *input.Status == entity.ResourceArchived && current.Status != entity.ResourceDisabled {
			return catalogConflict
		}
		updates := map[string]any{"updated_at": time.Now().UTC()}
		if input.Name != nil {
			updates["name"] = *input.Name
		}
		if input.Description != nil {
			updates["description"] = *input.Description
		}
		if input.Status != nil {
			updates["status"] = *input.Status
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
	return result, catalogError(err)
}
