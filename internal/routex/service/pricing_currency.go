package service

import (
	"context"
	"database/sql"

	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/pricing"
)

type PricingCurrencyPage struct {
	ETag               string     `json:"etag"`
	Currency           pricing.FX `json:"currency"`
	RequiredCurrencies []string   `json:"required_currencies"`
}

// GetPricingCurrency reads the complete finite currency requirements independently
// of price-catalogue pagination, under the same catalogue generation lock.
func (s *Service) GetPricingCurrency(ctx context.Context, actorID string) (*PricingCurrencyPage, error) {
	result := &PricingCurrencyPage{RequiredCurrencies: []string{}}
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := authorizeGovernance(tx, actorID, "prices.read"); err != nil {
			return err
		}
		setting, err := lockPricing(tx)
		if err != nil {
			return err
		}
		result.ETag = setting.ETag
		result.Currency, err = pricingFX(tx, setting)
		if err != nil {
			return err
		}
		return tx.Model(&entity.PriceRate{}).Where("enabled = ?", true).
			Distinct("currency").Order("currency").Pluck("currency", &result.RequiredCurrencies).Error
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	return result, pricingError(err)
}
