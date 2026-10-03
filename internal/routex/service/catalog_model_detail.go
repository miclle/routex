package service

import (
	"context"
	"database/sql"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/miclle/routex/internal/routex/database"
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
		return err
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
		result.Bindings = append(result.Bindings, BindingCatalog{Binding: binding, ProviderID: connection.ProviderID, ConnectionID: connection.ID, UpstreamName: model.UpstreamName, Protocol: connection.Protocol, Ready: ready})
	}
	if err := personalExact(tx.Model(&entity.UserModelGrant{}), "model_id", modelID).Order("user_id").Pluck("user_id", &result.GrantedUserIDs).Error; err != nil {
		return nil, err
	}
	return result, nil
}

func exactProviderModelReady(tx *gorm.DB, providerModelID, connectionID string) (bool, error) {
	var enabled, covered int64
	if err := personalExact(personalExact(tx.Model(&entity.ProviderCredential{}), "connection_id", connectionID), "verification_status", "verified").Where("enabled = ?", true).Count(&enabled).Error; err != nil {
		return false, err
	}
	if err := tx.Table("provider_credentials AS c").
		Joins("JOIN credential_model_accesses AS a ON ?", database.ExactTextColumns(tx, clause.Column{Table: "a", Name: "credential_id"}, clause.Column{Table: "c", Name: "id"})).
		Where(database.ExactText(tx, clause.Column{Table: "c", Name: "connection_id"}, connectionID)).
		Where(database.ExactText(tx, clause.Column{Table: "a", Name: "provider_model_id"}, providerModelID)).
		Where(database.ExactText(tx, clause.Column{Table: "c", Name: "verification_status"}, "verified")).Where("c.enabled = ?", true).Count(&covered).Error; err != nil {
		return false, err
	}
	return enabled > 0 && enabled == covered, nil
}
