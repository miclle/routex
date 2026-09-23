package service

import (
	"context"
	"regexp"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/id"
)

var publicModelName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$`)

type ModelCatalog struct {
	Model          entity.Model
	Name           string
	Names          []entity.ModelName
	Bindings       []BindingCatalog
	GrantedUserIDs []string
}

type BindingCatalog struct {
	Binding      entity.ModelProviderBinding
	ProviderID   string
	ConnectionID string
	UpstreamName string
	Protocol     string
	Ready        bool
}

type ModelWeight struct {
	BindingID string
	Weight    int
}

type VisibleModel struct {
	Protocols []string `gorm:"-"`
	ID        string
	Name      string
	Status    string
	Protocol  string
}

func loadModelCatalog(db *gorm.DB, modelID string) (*ModelCatalog, error) {
	result := &ModelCatalog{Names: []entity.ModelName{}, Bindings: []BindingCatalog{}, GrantedUserIDs: []string{}}
	if err := db.First(&result.Model, "id = ?", modelID).Error; err != nil {
		return nil, err
	}
	if err := db.Where("model_id = ?", modelID).Order("created_at, name").Find(&result.Names).Error; err != nil {
		return nil, err
	}
	for _, name := range result.Names {
		if name.CurrentModelID != nil {
			result.Name = name.Name
		}
	}
	var bindings []entity.ModelProviderBinding
	if err := db.Where("model_id = ?", modelID).Order("created_at, id").Find(&bindings).Error; err != nil {
		return nil, err
	}
	for _, binding := range bindings {
		var pm entity.ProviderModel
		if err := db.First(&pm, "id = ?", binding.ProviderModelID).Error; err != nil {
			return nil, err
		}
		var connection entity.ProviderConnection
		if err := db.First(&connection, "id = ?", pm.ConnectionID).Error; err != nil {
			return nil, err
		}
		ready, err := providerModelReady(db, pm.ID, connection.ID)
		if err != nil {
			return nil, err
		}
		result.Bindings = append(result.Bindings, BindingCatalog{Binding: binding, ProviderID: connection.ProviderID, ConnectionID: connection.ID, UpstreamName: pm.UpstreamName, Protocol: connection.Protocol, Ready: ready})
	}
	if err := db.Model(&entity.UserModelGrant{}).Where("model_id = ?", modelID).Order("user_id").Pluck("user_id", &result.GrantedUserIDs).Error; err != nil {
		return nil, err
	}
	return result, nil
}

func providerModelReady(db *gorm.DB, providerModelID, connectionID string) (bool, error) {
	var enabled, covered int64
	if err := db.Model(&entity.ProviderCredential{}).Where("connection_id = ? AND enabled = ? AND verification_status = ?", connectionID, true, "verified").Count(&enabled).Error; err != nil {
		return false, err
	}
	if err := db.Table("provider_credentials AS c").Joins("JOIN credential_model_accesses a ON a.credential_id = c.id").Where("c.connection_id = ? AND c.enabled = ? AND c.verification_status = ? AND a.provider_model_id = ?", connectionID, true, "verified", providerModelID).Count(&covered).Error; err != nil {
		return false, err
	}
	return enabled > 0 && enabled == covered, nil
}

func (s *Service) ListAdminModels(ctx context.Context) ([]ModelCatalog, error) {
	db := s.authDB(ctx)
	var models []entity.Model
	if err := db.Order("created_at, id").Find(&models).Error; err != nil {
		return nil, catalogError(err)
	}
	result := make([]ModelCatalog, 0, len(models))
	for _, model := range models {
		item, err := loadModelCatalog(db, model.ID)
		if err != nil {
			return nil, catalogError(err)
		}
		result = append(result, *item)
	}
	return result, nil
}

func (s *Service) CreateModel(ctx context.Context, actorID, name, providerModelID string) (*ModelCatalog, error) {
	if !publicModelName.MatchString(name) {
		return nil, apperrors.ErrBadRequest
	}
	modelID, err := id.NewPrefixed("mdl")
	if err != nil {
		return nil, apperrors.ErrInternal
	}
	bindingID, err := id.NewPrefixed("bnd")
	if err != nil {
		return nil, apperrors.ErrInternal
	}
	db := s.authDB(ctx)
	err = db.Transaction(func(tx *gorm.DB) error {
		var pm entity.ProviderModel
		if err := tx.First(&pm, "id = ?", providerModelID).Error; err != nil {
			return err
		}
		model := entity.Model{ID: modelID, Status: "active"}
		if err := tx.Create(&model).Error; err != nil {
			return err
		}
		if err := tx.Create(&entity.ModelName{Name: name, ModelID: modelID, CurrentModelID: &modelID}).Error; err != nil {
			return err
		}
		if err := tx.Create(&entity.ModelProviderBinding{ID: bindingID, ModelID: modelID, ProviderModelID: providerModelID}).Error; err != nil {
			return err
		}
		// This is an explicit persisted grant, not an implicit administrator bypass.
		if err := tx.Create(&entity.UserModelGrant{UserID: actorID, ModelID: modelID}).Error; err != nil {
			return err
		}
		if err := appendAudit(tx, actorID, "model.create", "model", modelID); err != nil {
			return err
		}
		return appendAudit(tx, actorID, "model.grant", "model", modelID)
	})
	if err := s.refreshAfterMutation(ctx, catalogError(err)); err != nil {
		return nil, err
	}
	result, err := loadModelCatalog(db, modelID)
	return result, catalogError(err)
}

func (s *Service) AddModelBinding(ctx context.Context, actorID, modelID, providerModelID string) (*ModelCatalog, error) {
	bindingID, err := id.NewPrefixed("bnd")
	if err != nil {
		return nil, apperrors.ErrInternal
	}
	db := s.authDB(ctx)
	err = db.Transaction(func(tx *gorm.DB) error {
		var model entity.Model
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&model, "id = ?", modelID).Error; err != nil {
			return err
		}
		var pm entity.ProviderModel
		if err := tx.First(&pm, "id = ?", providerModelID).Error; err != nil {
			return err
		}
		if err := tx.Create(&entity.ModelProviderBinding{ID: bindingID, ModelID: modelID, ProviderModelID: providerModelID}).Error; err != nil {
			return err
		}
		return appendAudit(tx, actorID, "model.binding.create", "model", modelID)
	})
	if err := s.refreshAfterMutation(ctx, catalogError(err)); err != nil {
		return nil, err
	}
	result, err := loadModelCatalog(db, modelID)
	return result, catalogError(err)
}

func (s *Service) SetModelWeights(ctx context.Context, actorID, modelID string, weights []ModelWeight) (*ModelCatalog, error) {
	if len(weights) == 0 || len(weights) > 1000 {
		return nil, apperrors.ErrBadRequest
	}
	requested := make(map[string]int, len(weights))
	for _, weight := range weights {
		if _, exists := requested[weight.BindingID]; exists || weight.Weight < 0 || weight.Weight > 100 {
			return nil, apperrors.ErrBadRequest
		}
		requested[weight.BindingID] = weight.Weight
	}
	db := s.authDB(ctx)
	err := db.Transaction(func(tx *gorm.DB) error {
		var model entity.Model
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&model, "id = ?", modelID).Error; err != nil {
			return err
		}
		// Connection locks serialize activation against credential verification
		// and enable/disable changes. Sort them to keep lock ordering stable.
		var connectionIDs []string
		if err := tx.Table("model_provider_bindings AS b").Distinct("p.connection_id").Joins("JOIN provider_models p ON p.id = b.provider_model_id").Where("b.model_id = ?", modelID).Pluck("p.connection_id", &connectionIDs).Error; err != nil {
			return err
		}
		if len(connectionIDs) > 0 {
			var connections []entity.ProviderConnection
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id IN ?", connectionIDs).Order("id").Find(&connections).Error; err != nil {
				return err
			}
		}
		catalog, err := loadModelCatalog(tx, modelID)
		if err != nil {
			return err
		}
		if len(catalog.Bindings) != len(requested) {
			return catalogConflict
		}
		sums := map[string]int{}
		for _, binding := range catalog.Bindings {
			weight, exists := requested[binding.Binding.ID]
			if !exists {
				return catalogConflict
			}
			if weight > 0 && !binding.Ready {
				return credentialNotReady
			}
			sums[binding.Protocol] += weight
		}
		for _, sum := range sums {
			if sum != 100 {
				return apperrors.ErrBadRequest
			}
		}
		for _, binding := range catalog.Bindings {
			if err := tx.Model(&entity.ModelProviderBinding{}).Where("id = ?", binding.Binding.ID).Update("weight", requested[binding.Binding.ID]).Error; err != nil {
				return err
			}
		}
		return appendAudit(tx, actorID, "model.weights.update", "model", modelID)
	})
	if err == nil {
		s.InvalidateRuntimeModel(modelID)
	}
	if err := s.refreshAfterMutation(ctx, catalogError(err)); err != nil {
		return nil, err
	}
	result, err := loadModelCatalog(db, modelID)
	return result, catalogError(err)
}

func (s *Service) RenameModel(ctx context.Context, actorID, modelID, name string, aliasExpiresAt *time.Time) (*ModelCatalog, error) {
	if !publicModelName.MatchString(name) || (aliasExpiresAt != nil && !aliasExpiresAt.After(time.Now())) {
		return nil, apperrors.ErrBadRequest
	}
	db := s.authDB(ctx)
	err := db.Transaction(func(tx *gorm.DB) error {
		var model entity.Model
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&model, "id = ?", modelID).Error; err != nil {
			return err
		}
		var current entity.ModelName
		if err := tx.First(&current, "current_model_id = ?", modelID).Error; err != nil {
			return err
		}
		if current.Name == name {
			return nil
		}
		expires := time.Now().UTC()
		if aliasExpiresAt != nil {
			expires = aliasExpiresAt.UTC()
		}
		if err := tx.Model(&current).Updates(map[string]any{"current_model_id": nil, "expires_at": expires}).Error; err != nil {
			return err
		}
		if err := tx.Create(&entity.ModelName{Name: name, ModelID: modelID, CurrentModelID: &modelID}).Error; err != nil {
			return err
		}
		return appendAudit(tx, actorID, "model.rename", "model", modelID)
	})
	if err == nil {
		s.InvalidateRuntimeModel(modelID)
	}
	if err := s.refreshAfterMutation(ctx, catalogError(err)); err != nil {
		return nil, err
	}
	result, err := loadModelCatalog(db, modelID)
	return result, catalogError(err)
}

func (s *Service) SetModelGrants(ctx context.Context, actorID, modelID string, userIDs []string) (*ModelCatalog, error) {
	if len(userIDs) > 1000 {
		return nil, apperrors.ErrBadRequest
	}
	seen := map[string]bool{}
	for _, userID := range userIDs {
		if userID == "" || seen[userID] {
			return nil, apperrors.ErrBadRequest
		}
		seen[userID] = true
	}
	db := s.authDB(ctx)
	err := db.Transaction(func(tx *gorm.DB) error {
		// Key issuance locks its owner before foreign-key checks on the model.
		// Grant insertion must use the same order to avoid an owner/model cycle.
		if len(userIDs) > 0 {
			var users []entity.User
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").Where("id IN ? AND disabled = ?", userIDs, false).Order("id").Find(&users).Error; err != nil {
				return err
			}
			if len(users) != len(userIDs) {
				return apperrors.ErrBadRequest
			}
		}
		var model entity.Model
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&model, "id = ?", modelID).Error; err != nil {
			return err
		}
		if err := tx.Where("model_id = ?", modelID).Delete(&entity.UserModelGrant{}).Error; err != nil {
			return err
		}
		for _, userID := range userIDs {
			if err := tx.Create(&entity.UserModelGrant{UserID: userID, ModelID: modelID}).Error; err != nil {
				return err
			}
		}
		return appendAudit(tx, actorID, "model.grants.update", "model", modelID)
	})
	if err == nil {
		s.InvalidateRuntimeModel(modelID)
	}
	if err := s.refreshAfterMutation(ctx, catalogError(err)); err != nil {
		return nil, err
	}
	result, err := loadModelCatalog(db, modelID)
	return result, catalogError(err)
}

func (s *Service) ListVisibleModels(ctx context.Context, userID string) ([]VisibleModel, error) {
	result := []VisibleModel{}
	err := s.authDB(ctx).Table("models m").Select("m.id, n.name, m.status").Joins("JOIN model_names n ON n.current_model_id = m.id").Joins("JOIN user_model_grants g ON g.model_id = m.id").Where("g.user_id = ? AND m.status = ?", userID, "active").Order("n.name").Scan(&result).Error
	if err != nil || len(result) == 0 {
		return result, catalogError(err)
	}
	ids := make([]string, len(result))
	for index := range result {
		ids[index] = result[index].ID
		result[index].Protocols = []string{}
	}
	protocols, err := s.gatewayProtocols(ctx, ids)
	if err != nil {
		return nil, catalogError(err)
	}
	for index := range result {
		result[index].Protocols = protocols[result[index].ID]
		if len(result[index].Protocols) > 0 {
			result[index].Protocol = result[index].Protocols[0]
		}
	}
	return result, nil
}

func (s *Service) ListModelGrantees(ctx context.Context) ([]entity.User, error) {
	result := []entity.User{}
	err := s.authDB(ctx).Select("id", "email", "name").Where("disabled = ?", false).Order("email").Find(&result).Error
	return result, catalogError(err)
}

// ResolveModelName resolves a current or unexpired compatibility name without
// granting access. Callers must still check user/Key permissions by stable ID.
func (s *Service) ResolveModelName(ctx context.Context, name string) (*entity.Model, error) {
	var model entity.Model
	err := s.authDB(ctx).Table("models").Select("models.*").Joins("JOIN model_names n ON n.model_id = models.id").Where("n.name = ? AND models.status = ? AND (n.current_model_id IS NOT NULL OR n.expires_at > ?)", name, "active", time.Now().UTC()).Take(&model).Error
	return &model, catalogError(err)
}
