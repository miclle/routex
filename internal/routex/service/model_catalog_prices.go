package service

import (
	"context"
	"database/sql"
	"reflect"
	"slices"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/pricing"
	"gorm.io/gorm"
)

// Reauthorize after the independent metadata fallback has released its connection.
// Prices and exact current source identities are read in one fresh snapshot; an
// obsolete catalogue cannot acquire prices from a later actor or grant generation.
func (s *Service) readMemberCatalogPrices(ctx context.Context, originalActor entity.User, modelID string, original []MemberModelCatalogRecord) error {
	return s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		actor, err := registrationAdmittedUser(tx, originalActor.ID, false)
		if err != nil {
			return err
		}
		current, err := readMemberCatalogRecords(tx, actor.ID, modelID)
		if err != nil {
			return err
		}
		if !actor.CreatedAt.Equal(originalActor.CreatedAt) || !reflect.DeepEqual(current, original) {
			return runtimeUnavailable
		}
		allowed, err := exactGovernancePermissionForAdmittedActor(modelCreationDB(tx), actor, "prices.read")
		if err != nil {
			return err
		}
		data := &memberModelsData{PricesRead: allowed}
		// Unauthorized reads do not hydrate price, Credential or supplier metadata.
		if allowed && len(current) > 0 && s.runtime != nil {
			ids := make([]string, len(current))
			for i, item := range current {
				ids[i] = item.ID
			}
			if err := modelCreationDB(tx).Where(memberModelsExactIDs(tx, "id", ids)).Limit(memberCatalogModelLimit + 1).Find(&data.Models).Error; err != nil {
				return err
			}
			if len(data.Models) != len(current) {
				return runtimeUnavailable
			}
			for _, model := range data.Models {
				i := slices.IndexFunc(current, func(item MemberModelCatalogRecord) bool { return item.ID == model.ID })
				if i < 0 || model.Status != current[i].Status || !model.CreatedAt.Equal(current[i].CreatedAt) {
					return runtimeUnavailable
				}
			}
			if err := readMemberModelMetadata(tx, data, ids); err != nil {
				return err
			}
		}
		s.applyMemberCatalogPrices(original, data)
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
}

// Report only a complete coherent ready route set. This is a configured base
// price observation, never a selected route, quote or invocation guarantee.
func (s *Service) applyMemberCatalogPrices(items []MemberModelCatalogRecord, data *memberModelsData) {
	var auth *runtimeAuthorization
	var routes *runtimeRoutes
	if data.PricesRead && s.runtime != nil && s.runtime.mu.TryLock() {
		defer s.runtime.mu.Unlock()
		auth, routes = s.runtime.auth.Load(), s.runtime.routes.Load()
	}
	index := memberModelsIndex(data)
	for i := range items {
		var eligible []string
		if modelIndex := slices.IndexFunc(data.Models, func(model entity.Model) bool { return model.ID == items[i].ID }); modelIndex >= 0 {
			_, eligible, _ = s.memberModelProtocols(data.Models[modelIndex], index, auth, routes)
		}
		items[i].InputPrice = memberModelsPrice(pricing.Input, data.PricesRead, eligible, data)
		items[i].OutputPrice = memberModelsPrice(pricing.Output, data.PricesRead, eligible, data)
	}
	// A concurrent publisher or expiry must not leave any earlier row priced.
	if data.PricesRead && auth != nil && (s.runtime.auth.Load() != auth || s.runtime.routes.Load() != routes || !time.Now().Before(auth.ValidUntil)) {
		for i := range items {
			items[i].InputPrice = MemberModelPriceCell{State: "unavailable"}
			items[i].OutputPrice = MemberModelPriceCell{State: "unavailable"}
		}
	}
}
