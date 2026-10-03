package service

import (
	"context"
	"database/sql"

	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/pricing"
)

// The scoped price endpoint preserves PricePage while authorizing and loading
// exactly one live Provider Model, independently from Model directory access.
func (s *Service) getExactProviderModelPrice(ctx context.Context, actorID, providerModelID string) (*PricePage, error) {
	if !safeTeamSessionID(providerModelID) {
		return nil, apperrors.ErrBadRequest
	}
	result := &PricePage{Items: []PriceRecord{}}
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := exactCatalogPermission(tx, actorID, "prices.read"); err != nil {
			return err
		}
		model, connection, err := loadExactPriceSubject(tx, providerModelID)
		if err != nil {
			return err
		}
		// Writers lock this same generation row. Keep the scoped schedule and FX
		// coherent even on MySQL's READ COMMITTED isolation.
		setting, err := lockPricing(tx)
		if err != nil {
			return err
		}
		result.ETag = setting.ETag
		result.Currency, err = pricingFX(tx, setting)
		if err != nil {
			return err
		}
		var price entity.ModelPrice
		if err := personalExact(tx, "provider_model_id", providerModelID).Take(&price).Error; err != nil {
			return err
		}
		if price.ProviderModelID != model.ID {
			return apperrors.ErrNotFound
		}
		record := PriceRecord{ID: price.ID, ProviderID: connection.ProviderID, ProviderModelID: model.ID, UpstreamName: model.UpstreamName, Protocol: connection.Protocol, ContextThreshold: price.ContextThreshold, UpdateSource: price.UpdateSource, FollowRepository: price.FollowRepository, Rates: []pricing.Rate{}}
		var rates []entity.PriceRate
		if err := personalExact(tx, "model_price_id", price.ID).Order("metric,tier").Find(&rates).Error; err != nil {
			return err
		}
		for _, rate := range rates {
			record.Rates = append(record.Rates, pricing.Rate{ID: rate.ID, Metric: rate.Metric, Tier: rate.Tier, Unit: rate.Unit, Currency: rate.Currency, Amount: rate.Amount, Enabled: rate.Enabled})
		}
		result.Items = append(result.Items, record)
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	return result, pricingError(err)
}

func loadExactPriceSubject(tx *gorm.DB, providerModelID string) (entity.ProviderModel, entity.ProviderConnection, error) {
	var model entity.ProviderModel
	var connection entity.ProviderConnection
	if err := personalExact(tx, "id", providerModelID).Take(&model).Error; err != nil {
		return model, connection, err
	}
	if model.ID != providerModelID {
		return model, connection, apperrors.ErrNotFound
	}
	if err := personalExact(tx, "id", model.ConnectionID).Take(&connection).Error; err != nil {
		return model, connection, err
	}
	if connection.ID != model.ConnectionID {
		return model, connection, apperrors.ErrNotFound
	}
	var provider entity.Provider
	if err := personalExact(tx, "id", connection.ProviderID).Select("id").Take(&provider).Error; err != nil {
		return model, connection, err
	}
	if provider.ID != connection.ProviderID {
		return model, connection, apperrors.ErrNotFound
	}
	return model, connection, nil
}
