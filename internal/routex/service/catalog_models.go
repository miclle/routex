package service

import (
	"context"
	"database/sql"
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
	// ConfiguredReady is optional authorized stored configuration, not runtime health.
	ConfiguredReady *bool
	Model           entity.Model
	Name            string
	Names           []entity.ModelName
	Bindings        []BindingCatalog
	GrantedUserIDs  []string
}

type BindingCatalog struct {
	Supply *ModelRoutingSupply

	Binding      entity.ModelProviderBinding
	ProviderID   string
	ConnectionID string
	UpstreamName string
	Protocol     string
	Ready        bool
	// Weight configuration depends on verified credential coverage even when
	// the Provider Model is temporarily disabled.
	credentialReady bool
}

func modelBindingCatalog(binding entity.ModelProviderBinding, model entity.ProviderModel, connection entity.ProviderConnection, credentialReady bool) BindingCatalog {
	return BindingCatalog{
		Binding: binding, ProviderID: connection.ProviderID, ConnectionID: connection.ID,
		UpstreamName: model.UpstreamName, Protocol: connection.Protocol,
		Ready: !model.Disabled && credentialReady, credentialReady: credentialReady,
	}
}

type ModelWeight struct {
	BindingID string
	Weight    int
}

type VisibleModel struct {
	Protocols         []string            `gorm:"-"`
	InputCapabilities map[string][]string `gorm:"-"`
	ID                string
	Name              string
	Status            string
	Protocol          string
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
		result.Bindings = append(result.Bindings, modelBindingCatalog(binding, pm, connection, ready))
	}
	if err := db.Model(&entity.UserModelGrant{}).Where("model_id = ?", modelID).Order("user_id").Pluck("user_id", &result.GrantedUserIDs).Error; err != nil {
		return nil, err
	}
	return result, nil
}

func providerModelReady(db *gorm.DB, providerModelID, connectionID string) (bool, error) {
	var pm entity.ProviderModel
	var connection entity.ProviderConnection
	if err := personalExact(modelCreationDB(db), "id", providerModelID).Take(&pm).Error; err != nil {
		return false, err
	}
	if err := personalExact(modelCreationDB(db), "id", connectionID).Take(&connection).Error; err != nil {
		return false, err
	}
	if pm.ConnectionID != connectionID || !capabilityTransportCurrent(pm, connection) {
		return false, nil
	}
	credentials, covered, err := connectionCredentialCoverage(db, connectionID, []string{providerModelID})
	if err != nil {
		return false, err
	}
	enabled := 0
	for _, c := range credentials {
		if c.Enabled && verifiedTransportCurrent(c, connection) {
			enabled++
			if !covered[c.ID][providerModelID] {
				return false, nil
			}
		}
	}
	return enabled > 0, nil
}

func (s *Service) ListAdminModels(ctx context.Context, actorID string) ([]ModelCatalog, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	result := []ModelCatalog{}
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := exactCatalogPermission(tx, actorID, "models.read_all"); err != nil {
			return err
		}
		var models []entity.Model
		if err := tx.Order("created_at, id").Find(&models).Error; err != nil {
			return err
		}
		for _, model := range models {
			item, err := loadExactAdminModelCatalog(tx, model.ID)
			if err != nil {
				return err
			}
			result = append(result, *item)
		}
		return enrichModelConfiguredReadiness(tx, actorID, result)
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, catalogError(err)
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
	release := s.pinPersonalKeyMutation()
	defer release()
	db := s.authDB(ctx)
	err = db.Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		if err := exactCatalogPermission(modelCreationDB(tx), actorID, "models.write"); err != nil {
			return err
		}
		if _, err := memberModelsSubject(tx, actorID, true); err != nil {
			return err
		}
		var pm entity.ProviderModel
		if err := tx.First(&pm, "id = ?", providerModelID).Error; err != nil {
			return err
		}
		model := newRecordedModel(modelID, "active", time.Now())
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
		if err := advancePersonalGrantRevision(modelCreationDB(tx), actorID); err != nil {
			return err
		}
		if err := appendAudit(tx, actorID, "model.create", "model", modelID); err != nil {
			return err
		}
		return appendAudit(tx, actorID, "model.grant", "model", modelID)
	})
	release()
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
		if err := stampModelConfiguration(tx, modelID, time.Now()); err != nil {
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
		if err := lockGovernance(tx); err != nil {
			return err
		}
		if _, _, err := modelWeightAuthority(tx, actorID, false, true); err != nil {
			return err
		}
		observed, err := loadModelWeightState(tx, modelID, true, false)
		if err != nil {
			return err
		}
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
			if weight > 0 && !binding.credentialReady {
				return credentialNotReady
			}
			sums[binding.Protocol] += weight
		}
		for _, sum := range sums {
			if sum != 100 {
				return apperrors.ErrBadRequest
			}
		}
		changed := false
		for _, binding := range catalog.Bindings {
			if binding.Binding.Weight == requested[binding.Binding.ID] {
				continue
			}
			changed = true
			if err := tx.Model(&entity.ModelProviderBinding{}).Where("id = ?", binding.Binding.ID).Update("weight", requested[binding.Binding.ID]).Error; err != nil {
				return err
			}
		}
		if changed {
			after := append([]ModelWeightRow(nil), observed.Rows...)
			for i := range after {
				after[i].Weight = requested[after[i].BindingID]
			}
			now := time.Now().UTC().Truncate(time.Microsecond)
			beforeID, afterID, err := journalModelWeightChange(tx, actorID, observed, after, "legacy_editor", nil, nil, now)
			if err != nil {
				return err
			}
			if err := stampModelConfiguration(tx, modelID, now); err != nil {
				return err
			}
			return appendModelWeightAudit(tx, actorID, modelID, "model.weights.update", modelWeightAudit{SourceVersionID: beforeID, SavedVersionID: afterID, BeforeDigest: memberModelsDigest(observed.Rows), AfterDigest: memberModelsDigest(after), BindingCount: len(after), Effect: "changed"})
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
		if err := stampModelConfiguration(tx, modelID, time.Now()); err != nil {
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
	if len(userIDs) > 1000 || !validAdminModelTarget(modelID) {
		return nil, apperrors.ErrBadRequest
	}
	seen := map[string]bool{}
	for _, userID := range userIDs {
		if !safeTeamSessionID(userID) || seen[userID] {
			return nil, apperrors.ErrBadRequest
		}
		seen[userID] = true
	}
	release := s.pinPersonalKeyMutation()
	defer release()
	db := s.authDB(ctx)
	var removed []string
	err := db.Transaction(func(tx *gorm.DB) error {
		var err error
		removed, err = setCatalogModelGrants(tx, actorID, modelID, userIDs)
		return err
	})
	if err == nil {
		for _, userID := range removed {
			s.invalidatePersonalModelGrants(userID)
		}
	}
	release()
	if err := s.refreshAfterMutation(ctx, catalogError(err)); err != nil {
		return nil, err
	}
	result, err := loadModelCatalog(db, modelID)
	return result, catalogError(err)
}

func (s *Service) ListVisibleModels(ctx context.Context, userID string) ([]VisibleModel, error) {
	if _, err := registrationAdmittedUser(s.authDB(ctx), userID, false); err != nil {
		return nil, catalogError(err)
	}
	result := []VisibleModel{}
	err := s.authDB(ctx).Table("models m").Select("m.id, n.name, m.status").Joins("JOIN model_names n ON n.current_model_id = m.id").Joins("JOIN user_model_grants g ON g.model_id = m.id").Where("g.user_id = ? AND m.status = ?", userID, "active").Order("n.name").Scan(&result).Error
	if err != nil || len(result) == 0 {
		return result, catalogError(err)
	}
	ids := make([]string, len(result))
	for index := range result {
		ids[index] = result[index].ID
		result[index].Protocols = []string{}
		result[index].InputCapabilities = map[string][]string{}
	}
	metadata, err := s.gatewayModelMetadata(ctx, ids)
	if err != nil {
		return nil, catalogError(err)
	}
	for index := range result {
		item := metadata[result[index].ID]
		result[index].Protocols = item.Protocols
		result[index].InputCapabilities = item.InputCapabilities
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
