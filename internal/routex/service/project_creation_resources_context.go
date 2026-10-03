package service

import (
	"context"
	"database/sql"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
)

type ProjectCreationContext struct {
	ReviewETag          string `json:"review_etag"`
	PlatformCurrency    string `json:"platform_currency"`
	CanSetModels        bool   `json:"can_set_models"`
	CanSetLimits        bool   `json:"can_set_limits"`
	CanRequestResources bool   `json:"can_request_resources"`
}

func projectCreationContext(tx *gorm.DB, actor entity.User) (*ProjectCreationContext, error) {
	models, err := exactGovernancePermission(tx, actor, "projects.models.write")
	if err != nil {
		return nil, err
	}
	policy, err := exactGovernancePermission(tx, actor, "projects.limits.write")
	if err != nil {
		return nil, err
	}
	platform, err := exactGovernancePermission(tx, actor, "projects.write")
	if err != nil {
		return nil, err
	}
	var currency entity.PricingSetting
	if err := tx.First(&currency, 1).Error; err != nil {
		return nil, err
	}
	tag := personalHash(struct {
		ActorID, Role, PricingETag, Currency string
		ActorUpdatedAt                       time.Time
		Models, Limits, Platform             bool
	}{actor.ID, actor.Role, currency.ETag, currency.PlatformCurrency, actor.UpdatedAt.UTC(), models, policy, platform})
	return &ProjectCreationContext{ReviewETag: tag, PlatformCurrency: currency.PlatformCurrency, CanSetModels: models, CanSetLimits: policy, CanRequestResources: true}, nil
}

func (s *Service) GetProjectCreationContext(ctx context.Context, actorID string) (*ProjectCreationContext, error) {
	var result *ProjectCreationContext
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		actor, err := exactEnabledActor(tx, actorID)
		if err != nil {
			return err
		}
		result, err = projectCreationContext(tx, actor)
		return err
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return result, catalogError(err)
}

type ProjectCreationModel struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type ProjectCreationModelPage struct {
	Items      []ProjectCreationModel `json:"items"`
	NextCursor *string                `json:"next_cursor"`
}
type ProjectCreationModelFilter struct {
	Query, Cursor string
	Limit         int
}

func normalizeProjectCreationModelFilter(input ProjectCreationModelFilter) (ProjectCreationModelFilter, string, error) {
	pattern, err := candidatePattern(input.Query)
	if err != nil {
		return input, "", err
	}
	if input.Limit == 0 {
		input.Limit = 50
	}
	if input.Limit < 1 || input.Limit > 50 || input.Cursor != "" && (!safeTeamSessionID(input.Cursor) || len(input.Cursor) < 5 || input.Cursor[:4] != "mdl_") {
		return input, "", apperrors.ErrBadRequest
	}
	return input, pattern, nil
}

func (s *Service) ListProjectCreationModels(ctx context.Context, actorID string, filter ProjectCreationModelFilter) (*ProjectCreationModelPage, error) {
	filter, pattern, err := normalizeProjectCreationModelFilter(filter)
	if err != nil {
		return nil, err
	}
	result := &ProjectCreationModelPage{Items: []ProjectCreationModel{}}
	err = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := exactEnabledActor(tx, actorID); err != nil {
			return err
		}
		query := tx.Table("models AS m").Select("m.id,n.name").
			Joins("JOIN model_names AS n ON ?", database.ExactTextColumns(tx, clause.Column{Table: "n", Name: "current_model_id"}, clause.Column{Table: "m", Name: "id"})).
			Where(database.ExactTextColumns(tx, clause.Column{Table: "n", Name: "model_id"}, clause.Column{Table: "m", Name: "id"})).
			Where(database.ExactText(tx, clause.Column{Table: "m", Name: "status"}, entity.ResourceActive)).
			Where("LOWER(n.name) LIKE ? ESCAPE '!'", pattern)
		if filter.Cursor != "" {
			query = query.Where("m.id > ?", filter.Cursor)
		}
		if err := query.Order("m.id").Limit(filter.Limit + 1).Scan(&result.Items).Error; err != nil {
			return err
		}
		if len(result.Items) > filter.Limit {
			cursor := result.Items[filter.Limit-1].ID
			result.NextCursor = &cursor
			result.Items = result.Items[:filter.Limit]
		}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return result, catalogError(err)
}

// Creation requires a live canonical Model and its current public name; legacy
// request validation remains unchanged for already-created Projects.
func exactProjectCreationModels(tx *gorm.DB, ids []string) error {
	for _, modelID := range ids {
		var model entity.Model
		if err := personalExact(tx, "id", modelID).Clauses(clause.Locking{Strength: "UPDATE"}).First(&model).Error; err != nil {
			return err
		}
		if model.ID != modelID || model.Status != entity.ResourceActive {
			return catalogConflict
		}
		var name entity.ModelName
		if err := personalExact(personalExact(tx, "model_id", modelID), "current_model_id", modelID).First(&name).Error; err != nil {
			return err
		}
		if name.ModelID != modelID || name.CurrentModelID == nil || *name.CurrentModelID != modelID {
			return catalogConflict
		}
	}
	return nil
}
