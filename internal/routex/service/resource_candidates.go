package service

import (
	"context"
	"database/sql"
	"github.com/miclle/routex/internal/routex/database"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"strings"
	"unicode/utf8"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
)

// ResourceCandidate exposes only fields needed to select an eligible resource.
// It does not grant member-directory or model-administration access.
type ResourceCandidate struct{ ID, Name, Email string }

func candidatePattern(query string) (string, error) {
	if len(query) > 200 || !utf8.ValidString(query) {
		return "", apperrors.ErrBadRequest
	}
	escaped := strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(strings.ToLower(strings.TrimSpace(query)))
	return "%" + escaped + "%", nil
}

func (s *Service) ResourceMemberCandidates(ctx context.Context, actorID string, kind ResourceKind, projectID, query string) ([]ResourceCandidate, error) {
	pattern, err := candidatePattern(query)
	if err != nil || !resourceKindValid(kind) {
		return nil, apperrors.ErrBadRequest
	}
	db := s.authDB(ctx)
	allowed, err := resourcePermission(db, actorID, kind, "write")
	if err != nil {
		return nil, catalogError(err)
	}
	if kind == ProjectResource {
		if !allowed {
			allowed, err = resourceManager(db, actorID, projectID)
			if err != nil {
				return nil, catalogError(err)
			}
		}
		if !allowed {
			return nil, apperrors.ErrNotFound
		}
		var project entity.Project
		if err := db.Where(database.ExactText(db, clause.Column{Name: "id"}, projectID)).First(&project).Error; err != nil {
			return nil, catalogError(err)
		}
		if project.ID != projectID {
			return nil, apperrors.ErrNotFound
		}
		if project.Status == entity.ResourceArchived {
			return nil, catalogConflict
		}
	} else if !allowed {
		return nil, apperrors.ErrForbidden
	}
	items := []ResourceCandidate{}
	err = db.Model(&entity.User{}).Select("id", "name", "email").Where("disabled = ?", false).
		Where("LOWER(name) LIKE ? ESCAPE '!' OR LOWER(email) LIKE ? ESCAPE '!'", pattern, pattern).
		Order("id").Limit(50).Scan(&items).Error
	return items, catalogError(err)
}

func (s *Service) ResourceModelCandidates(ctx context.Context, actorID string, kind ResourceKind, query string) ([]ResourceCandidate, error) {
	pattern, err := candidatePattern(query)
	if err != nil || !resourceKindValid(kind) {
		return nil, apperrors.ErrBadRequest
	}
	db := s.authDB(ctx)
	allowed, err := resourcePermission(db, actorID, kind, "models.write")
	if err != nil {
		return nil, catalogError(err)
	}
	if !allowed {
		return nil, apperrors.ErrForbidden
	}
	items := []ResourceCandidate{}
	err = db.Table("models m").Select("m.id,n.name").Joins("JOIN model_names n ON n.current_model_id = m.id").
		Where("m.status = ? AND LOWER(n.name) LIKE ? ESCAPE '!'", entity.ResourceActive, pattern).
		Order("m.id").Limit(50).Scan(&items).Error
	return items, catalogError(err)
}

func (s *Service) ScopedTeamMemberCandidates(ctx context.Context, actorID, teamID, query string) ([]ResourceCandidate, error) {
	pattern, err := candidatePattern(query)
	if err != nil {
		return nil, err
	}
	items := []ResourceCandidate{}
	err = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		allowed, err := teamTargetActionAllowed(tx, actorID, teamID, "teams.write")
		if err != nil {
			return err
		}
		if !allowed {
			return apperrors.ErrForbidden
		}
		var team entity.Team
		if err := quotaExact(tx, "id", teamID).First(&team).Error; err != nil {
			return err
		}
		if team.Status == entity.ResourceArchived {
			return catalogConflict
		}
		return tx.Model(&entity.User{}).Select("id", "name", "email").Where("disabled = ? AND offboarded_at IS NULL", false).Where("LOWER(name) LIKE ? ESCAPE '!' OR LOWER(email) LIKE ? ESCAPE '!'", pattern, pattern).Order("id").Limit(50).Scan(&items).Error
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return items, catalogError(err)
}
func (s *Service) ScopedTeamModelCandidates(ctx context.Context, actorID, teamID, query string) ([]ResourceCandidate, error) {
	pattern, err := candidatePattern(query)
	if err != nil {
		return nil, err
	}
	items := []ResourceCandidate{}
	err = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		allowed, err := teamTargetActionAllowed(tx, actorID, teamID, "teams.models.write")
		if err != nil {
			return err
		}
		if !allowed {
			return apperrors.ErrForbidden
		}
		var team entity.Team
		if err := quotaExact(tx, "id", teamID).First(&team).Error; err != nil {
			return err
		}
		if team.Status == entity.ResourceArchived {
			return catalogConflict
		}
		return tx.Table("models AS m").Select("m.id,n.name").Joins("JOIN model_names AS n ON ?", database.ExactTextColumns(tx, clause.Column{Table: "n", Name: "current_model_id"}, clause.Column{Table: "m", Name: "id"})).Where(database.ExactTextColumns(tx, clause.Column{Table: "n", Name: "model_id"}, clause.Column{Table: "m", Name: "id"})).Where(database.ExactText(tx, clause.Column{Table: "m", Name: "status"}, entity.ResourceActive)).Where("LOWER(n.name) LIKE ? ESCAPE '!'", pattern).Order("m.id").Limit(50).Scan(&items).Error
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return items, catalogError(err)
}

// ProjectCreationManagerCandidates is a purpose-specific picker, not a grant of
// global member-directory authority or access to existing Projects.
func (s *Service) ProjectCreationManagerCandidates(ctx context.Context, actorID, query string) ([]ResourceCandidate, error) {
	pattern, err := candidatePattern(query)
	if err != nil {
		return nil, err
	}
	items := []ResourceCandidate{}
	err = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := exactEnabledActor(tx, actorID); err != nil {
			return err
		}
		return tx.Model(&entity.User{}).Select("id", "name", "email").
			Where("disabled = ? AND offboarded_at IS NULL", false).
			Where("LOWER(name) LIKE ? ESCAPE '!' OR LOWER(email) LIKE ? ESCAPE '!'", pattern, pattern).
			Order("id").Limit(50).Scan(&items).Error
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return items, catalogError(err)
}
