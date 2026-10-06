package service

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"gorm.io/gorm"
)

const providerModelBindingsLimit = 10000

var providerModelBindingsOverflow = &apperrors.Error{Code: http.StatusUnprocessableEntity, Message: "Provider Model binding catalogue exceeds the supported read limit"}
var providerModelBindingsUnavailable = &apperrors.Error{Code: http.StatusServiceUnavailable, Message: "Provider Model binding catalogue unavailable"}

type ProviderBoundModel struct {
	ID   string  `json:"id"`
	Name *string `json:"name"`
}
type ProviderModelBindingRecord struct {
	ProviderModelID string               `json:"provider_model_id"`
	ConnectionID    string               `json:"connection_id"`
	BindingCount    int                  `json:"binding_count"`
	Models          []ProviderBoundModel `json:"models"`
}
type ProviderModelBindings struct {
	ProviderID string                       `json:"provider_id"`
	Items      []ProviderModelBindingRecord `json:"items"`
}

func providerBindingsID(value, prefix string) bool {
	return safeTeamSessionID(value) && strings.HasPrefix(value, prefix+"_")
}
func providerBindingsBound(length int) error {
	if length > providerModelBindingsLimit {
		return providerModelBindingsOverflow
	}
	return nil
}

// Ordinary indexed IN is a superset only: projection validates every returned
// identity against its exact requested set, including collation aliases.
func providerBindingsRows(tx *gorm.DB, column string, ids []string) *gorm.DB {
	return modelCreationDB(tx).Where(column+" IN ?", ids).Order("id").Limit(providerModelBindingsLimit + 1)
}

func projectProviderModelBindings(providerID string, connections []entity.ProviderConnection, supply []entity.ProviderModel, bindings []entity.ModelProviderBinding, models []entity.Model, names []entity.ModelName) (*ProviderModelBindings, error) {
	for _, n := range []int{len(connections), len(supply), len(bindings), len(models), len(names)} {
		if err := providerBindingsBound(n); err != nil {
			return nil, err
		}
	}
	connectionIDs := map[string]bool{}
	for _, c := range connections {
		if !providerBindingsID(c.ID, "con") || c.ProviderID != providerID || connectionIDs[c.ID] {
			return nil, providerModelBindingsUnavailable
		}
		connectionIDs[c.ID] = true
	}
	result := &ProviderModelBindings{ProviderID: providerID, Items: []ProviderModelBindingRecord{}}
	records := map[string]ProviderModelBindingRecord{}
	for _, pm := range supply {
		if !providerBindingsID(pm.ID, "pmd") || !connectionIDs[pm.ConnectionID] {
			return nil, providerModelBindingsUnavailable
		}
		if _, duplicate := records[pm.ID]; duplicate {
			return nil, providerModelBindingsUnavailable
		}
		records[pm.ID] = ProviderModelBindingRecord{ProviderModelID: pm.ID, ConnectionID: pm.ConnectionID, Models: []ProviderBoundModel{}}
	}
	wanted := map[string]bool{}
	bindingIDs := map[string]bool{}
	pairs := map[[2]string]bool{}
	for _, b := range bindings {
		if _, exists := records[b.ProviderModelID]; !exists {
			return nil, providerModelBindingsUnavailable
		}
		pair := [2]string{b.ProviderModelID, b.ModelID}
		if !providerBindingsID(b.ID, "bnd") || !validAdminModelTarget(b.ModelID) || b.Weight < 0 || b.Weight > 100 || bindingIDs[b.ID] || pairs[pair] {
			return nil, providerModelBindingsUnavailable
		}
		bindingIDs[b.ID] = true
		pairs[pair] = true
		wanted[b.ModelID] = true
	}
	found := map[string]bool{}
	for _, m := range models {
		if !wanted[m.ID] || found[m.ID] {
			return nil, providerModelBindingsUnavailable
		}
		found[m.ID] = true
	}
	if len(found) != len(wanted) {
		return nil, providerModelBindingsUnavailable
	}
	currentNames := map[string]*string{}
	for _, n := range names {
		if n.CurrentModelID == nil || *n.CurrentModelID != n.ModelID || !found[n.ModelID] {
			return nil, providerModelBindingsUnavailable
		}
		if _, duplicate := currentNames[n.ModelID]; duplicate {
			return nil, providerModelBindingsUnavailable
		}
		var name *string
		if publicModelName.MatchString(n.Name) {
			value := n.Name
			name = &value
		}
		currentNames[n.ModelID] = name
	}
	for _, b := range bindings {
		r := records[b.ProviderModelID]
		r.Models = append(r.Models, ProviderBoundModel{ID: b.ModelID, Name: currentNames[b.ModelID]})
		records[b.ProviderModelID] = r
	}
	for _, r := range records {
		slices.SortFunc(r.Models, func(a, b ProviderBoundModel) int { return strings.Compare(a.ID, b.ID) })
		r.BindingCount = len(r.Models)
		result.Items = append(result.Items, r)
	}
	slices.SortFunc(result.Items, func(a, b ProviderModelBindingRecord) int {
		return strings.Compare(a.ProviderModelID, b.ProviderModelID)
	})
	return result, nil
}

func loadProviderModelBindings(tx *gorm.DB, providerID string) (*ProviderModelBindings, error) {
	var provider entity.Provider
	q := modelCreationDB(tx).Where("id = ?", providerID)
	if err := personalExact(q, "id", providerID).Select("id").Take(&provider).Error; err != nil {
		return nil, err
	}
	if provider.ID != providerID {
		return nil, apperrors.ErrNotFound
	}
	var connections []entity.ProviderConnection
	// Select ownership candidates without hiding aliases: every returned row must
	// subsequently prove its exact parent and canonical child identity.
	if err := modelCreationDB(tx).Select("id", "provider_id").Where("provider_id = ?", providerID).Order("id").Limit(providerModelBindingsLimit + 1).Find(&connections).Error; err != nil {
		return nil, err
	}
	if err := providerBindingsBound(len(connections)); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(connections))
	for _, c := range connections {
		if c.ProviderID != providerID || !providerBindingsID(c.ID, "con") {
			return nil, providerModelBindingsUnavailable
		}
		ids = append(ids, c.ID)
	}
	var supply []entity.ProviderModel
	if len(ids) > 0 {
		if err := providerBindingsRows(tx, "connection_id", ids).Select("id", "connection_id").Find(&supply).Error; err != nil {
			return nil, err
		}
	}
	if err := providerBindingsBound(len(supply)); err != nil {
		return nil, err
	}
	ids = ids[:0]
	for _, pm := range supply {
		ids = append(ids, pm.ID)
	}
	var bindings []entity.ModelProviderBinding
	if len(ids) > 0 {
		if err := providerBindingsRows(tx, "provider_model_id", ids).Select("id", "model_id", "provider_model_id", "weight").Find(&bindings).Error; err != nil {
			return nil, err
		}
	}
	if err := providerBindingsBound(len(bindings)); err != nil {
		return nil, err
	}
	ids = ids[:0]
	for _, b := range bindings {
		ids = append(ids, b.ModelID)
	}
	slices.Sort(ids)
	ids = slices.Compact(ids)
	var models []entity.Model
	var names []entity.ModelName
	if len(ids) > 0 {
		if err := providerBindingsRows(tx, "id", ids).Select("id").Find(&models).Error; err != nil {
			return nil, err
		}
		if err := modelCreationDB(tx).Select("name", "model_id", "current_model_id").Where("current_model_id IN ?", ids).Order("current_model_id,name").Limit(providerModelBindingsLimit + 1).Find(&names).Error; err != nil {
			return nil, err
		}
	}
	return projectProviderModelBindings(providerID, connections, supply, bindings, models, names)
}

func (s *Service) GetProviderModelBindings(ctx context.Context, actorID, providerID string) (*ProviderModelBindings, error) {
	if !providerBindingsID(actorID, "usr") {
		return nil, apperrors.ErrUnauthorized
	}
	if !providerBindingsID(providerID, "prv") {
		return nil, apperrors.ErrBadRequest
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var result *ProviderModelBindings
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		actor, err := exactEnabledActor(modelCreationDB(tx), actorID)
		if err != nil {
			return err
		}
		if actor.Role != entity.RoleAdmin && actor.Role != entity.RoleMember {
			return apperrors.ErrUnauthorized
		}
		for _, permission := range []string{"providers.read", "models.read_all"} {
			allowed, err := exactGovernancePermissionForAdmittedActor(modelCreationDB(tx), actor, permission)
			if err != nil {
				return err
			}
			if !allowed {
				return apperrors.ErrForbidden
			}
		}
		result, err = loadProviderModelBindings(tx, providerID)
		return err
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		var app *apperrors.Error
		if errors.As(err, &app) {
			return nil, app
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, apperrors.ErrNotFound
		}
		return nil, providerModelBindingsUnavailable
	}
	return result, nil
}
