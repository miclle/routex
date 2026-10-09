package service

import (
	"context"
	"database/sql"
	"strings"

	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
)

func validAdminModelTarget(modelID string) bool {
	return strings.HasPrefix(modelID, "mdl_") && safeTeamSessionID(modelID)
}

func exactCatalogPermission(tx *gorm.DB, actorID, permission string) error {
	actor, err := exactEnabledActor(tx, actorID)
	if err != nil {
		return err
	}
	allowed, err := exactGovernancePermission(tx, actor, permission)
	if err != nil {
		return err
	}
	if !allowed {
		return apperrors.ErrForbidden
	}
	return nil
}

func (s *Service) GetAdminModel(ctx context.Context, actorID, modelID string) (*ModelCatalog, error) {
	if !validAdminModelTarget(modelID) {
		return nil, apperrors.ErrBadRequest
	}
	var result *ModelCatalog
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := exactCatalogPermission(tx, actorID, "models.read_all"); err != nil {
			return err
		}
		var err error
		result, err = loadExactAdminModelCatalog(tx, modelID)
		if err != nil {
			return err
		}
		user, err := exactEnabledActor(modelCreationDB(tx), actorID)
		if err != nil {
			return err
		}
		allowed, err := exactGovernancePermission(modelCreationDB(tx), user, "providers.read")
		if err != nil {
			return err
		}
		if allowed {
			return enrichRoutingSupplies(tx, result)
		}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return result, catalogError(err)
}

// Detail reads do not borrow the global list's target or relationship lookups.
// Exact predicates preserve identity under every supported database collation.
func loadExactAdminModelCatalog(tx *gorm.DB, modelID string) (*ModelCatalog, error) {
	result := &ModelCatalog{Names: []entity.ModelName{}, Bindings: []BindingCatalog{}, GrantedUserIDs: []string{}}
	if err := personalExact(tx, "id", modelID).Take(&result.Model).Error; err != nil {
		return nil, err
	}
	if result.Model.ID != modelID {
		return nil, apperrors.ErrNotFound
	}
	if err := personalExact(tx, "model_id", modelID).Order("created_at,name").Find(&result.Names).Error; err != nil {
		return nil, err
	}
	for _, name := range result.Names {
		if name.ModelID != modelID || name.CurrentModelID != nil && *name.CurrentModelID != modelID {
			return nil, apperrors.ErrNotFound
		}
		if name.CurrentModelID != nil {
			result.Name = name.Name
		}
	}
	var bindings []entity.ModelProviderBinding
	if err := personalExact(tx, "model_id", modelID).Order("created_at,id").Find(&bindings).Error; err != nil {
		return nil, err
	}
	for _, binding := range bindings {
		model, connection, err := loadExactPriceSubject(tx, binding.ProviderModelID)
		if err != nil {
			return nil, err
		}
		ready, err := exactProviderModelReady(tx, model.ID, connection.ID)
		if err != nil {
			return nil, err
		}
		result.Bindings = append(result.Bindings, modelBindingCatalog(binding, model, connection, ready))
	}
	if err := personalExact(tx.Model(&entity.UserModelGrant{}), "model_id", modelID).Order("user_id").Pluck("user_id", &result.GrantedUserIDs).Error; err != nil {
		return nil, err
	}
	return result, nil
}

func exactProviderModelReady(tx *gorm.DB, providerModelID, connectionID string) (bool, error) {
	return providerModelReady(tx, providerModelID, connectionID)
}
