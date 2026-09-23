package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"maps"
	"slices"
	"strings"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/pricing"
	"gorm.io/gorm"
)

// CallPriceBasis contains no secrets or request content. Runtime publication owns
// the source; every admitted request receives a detached immutable copy.
type CallPriceBasis struct {
	Adapter  string           `json:"adapter"`
	ETag     string           `json:"etag"`
	Schedule pricing.Schedule `json:"schedule"`
	Currency pricing.FX       `json:"currency"`
}
type runtimePricingData struct {
	Setting entity.PricingSetting
	FX      []entity.PricingExchangeRate
	Prices  []entity.ModelPrice
	Rates   []entity.PriceRate
}

func loadRuntimePricing(tx *gorm.DB) (*runtimePricingData, error) {
	data := &runtimePricingData{}
	if err := tx.First(&data.Setting, 1).Error; err != nil {
		return nil, err
	}
	for _, target := range []any{&data.FX, &data.Prices, &data.Rates} {
		if err := tx.Find(target).Error; err != nil {
			return nil, err
		}
	}
	slices.SortFunc(data.FX, func(a, b entity.PricingExchangeRate) int { return strings.Compare(a.Currency, b.Currency) })
	slices.SortFunc(data.Prices, func(a, b entity.ModelPrice) int { return strings.Compare(a.ID, b.ID) })
	slices.SortFunc(data.Rates, func(a, b entity.PriceRate) int { return strings.Compare(a.ID, b.ID) })
	return data, nil
}
func runtimePriceBasis(data *runtimePricingData, providerModelID, protocol string) *CallPriceBasis {
	if data == nil {
		return nil
	}
	basis := &CallPriceBasis{Adapter: "routex_text_v1", ETag: data.Setting.ETag, Schedule: pricing.Schedule{ProviderModelID: providerModelID, Protocol: protocol, Rates: []pricing.Rate{}}, Currency: pricing.FX{PlatformCurrency: data.Setting.PlatformCurrency, Rates: map[string]string{data.Setting.PlatformCurrency: "1"}}}
	for _, fx := range data.FX {
		basis.Currency.Rates[fx.Currency] = fx.Rate
	}
	for _, price := range data.Prices {
		if price.ProviderModelID == providerModelID {
			basis.Schedule.PriceID = price.ID
			basis.Schedule.ContextThreshold = price.ContextThreshold
			break
		}
	}
	for _, rate := range data.Rates {
		if rate.ModelPriceID == basis.Schedule.PriceID {
			basis.Schedule.Rates = append(basis.Schedule.Rates, pricing.Rate{ID: rate.ID, Metric: rate.Metric, Tier: rate.Tier, Unit: rate.Unit, Currency: rate.Currency, Amount: rate.Amount, Enabled: rate.Enabled})
		}
	}
	return basis
}
func clonePriceBasis(basis *CallPriceBasis) *CallPriceBasis {
	if basis == nil {
		return nil
	}
	copy := *basis
	copy.Schedule.Rates = slices.Clone(basis.Schedule.Rates)
	copy.Currency.Rates = maps.Clone(basis.Currency.Rates)
	return &copy
}
func (s *Service) capturePriceBasis(ctx context.Context, providerModelID string) (*CallPriceBasis, error) {
	var result *CallPriceBasis
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		data, err := loadRuntimePricing(tx)
		if err != nil {
			return err
		}
		result = runtimePriceBasis(data, providerModelID, entity.ProtocolOpenAIChat)
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return result, err
}
func pricingBasisSize(basis *CallPriceBasis) int {
	raw, err := json.Marshal(basis)
	if err != nil {
		return -1
	}
	return len(raw)
}
