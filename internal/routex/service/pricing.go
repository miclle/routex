package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"slices"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/id"
	"github.com/miclle/routex/pkg/pricing"
	"github.com/miclle/routex/pkg/secret"
)

var pricingStale = &apperrors.Error{Code: http.StatusConflict, Message: "price catalogue changed; reload before editing"}
var pricingMissing = &apperrors.Error{Code: http.StatusUnprocessableEntity, Message: "required price or exchange rate is not configured"}

type PriceRecord struct {
	ID               string         `json:"id"`
	ProviderID       string         `json:"provider_id"`
	ProviderModelID  string         `json:"provider_model_id"`
	UpstreamName     string         `json:"upstream_name"`
	Protocol         string         `json:"protocol"`
	ContextThreshold int64          `json:"context_threshold"`
	UpdateSource     string         `json:"update_source"`
	FollowRepository bool           `json:"follow_repository"`
	Rates            []pricing.Rate `json:"rates"`
}
type PricePage struct {
	ETag       string        `json:"etag"`
	Currency   pricing.FX    `json:"currency"`
	Items      []PriceRecord `json:"items"`
	NextCursor string        `json:"next_cursor"`
}
type PriceInput struct {
	ProviderModelID  string         `json:"provider_model_id"`
	ContextThreshold *int64         `json:"context_threshold"`
	Rates            []pricing.Rate `json:"rates"`
}
type PriceFilter struct {
	ProviderModelID, Cursor string
	Limit                   int
}
type PriceQuote struct {
	ETag  string         `json:"etag"`
	Quote *pricing.Quote `json:"quote"`
}

func pricingError(err error) error {
	if errors.Is(err, pricing.ErrInvalid) {
		return apperrors.ErrBadRequest
	}
	if errors.Is(err, pricing.ErrUnpriced) {
		return pricingMissing
	}
	return catalogError(err)
}
func lockPricing(tx *gorm.DB) (entity.PricingSetting, error) {
	var setting entity.PricingSetting
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&setting, 1).Error
	return setting, err
}
func pricingFX(tx *gorm.DB, setting entity.PricingSetting) (pricing.FX, error) {
	result := pricing.FX{PlatformCurrency: setting.PlatformCurrency, Rates: map[string]string{setting.PlatformCurrency: "1"}}
	var rows []entity.PricingExchangeRate
	if err := tx.Order("currency").Find(&rows).Error; err != nil {
		return result, err
	}
	for _, row := range rows {
		result.Rates[row.Currency] = row.Rate
	}
	return result, nil
}
func loadPrice(tx *gorm.DB, modelPrice entity.ModelPrice) (PriceRecord, error) {
	var model entity.ProviderModel
	var connection entity.ProviderConnection
	if err := tx.First(&model, "id = ?", modelPrice.ProviderModelID).Error; err != nil {
		return PriceRecord{}, err
	}
	if err := tx.First(&connection, "id = ?", model.ConnectionID).Error; err != nil {
		return PriceRecord{}, err
	}
	result := PriceRecord{ID: modelPrice.ID, ProviderID: connection.ProviderID, ProviderModelID: model.ID, UpstreamName: model.UpstreamName, Protocol: connection.Protocol, ContextThreshold: modelPrice.ContextThreshold, UpdateSource: modelPrice.UpdateSource, FollowRepository: modelPrice.FollowRepository, Rates: []pricing.Rate{}}
	var rows []entity.PriceRate
	if err := tx.Where("model_price_id = ?", modelPrice.ID).Order("metric, tier").Find(&rows).Error; err != nil {
		return result, err
	}
	for _, row := range rows {
		result.Rates = append(result.Rates, pricing.Rate{ID: row.ID, Metric: row.Metric, Tier: row.Tier, Unit: row.Unit, Currency: row.Currency, Amount: row.Amount, Enabled: row.Enabled})
	}
	return result, nil
}
func priceSchedule(record PriceRecord) pricing.Schedule {
	return pricing.Schedule{PriceID: record.ID, ProviderModelID: record.ProviderModelID, Protocol: record.Protocol, ContextThreshold: record.ContextThreshold, Rates: record.Rates}
}

func (s *Service) ListPrices(ctx context.Context, actorID string, filter PriceFilter) (*PricePage, error) {
	if filter.Limit == 0 {
		filter.Limit = 50
	}
	if filter.Limit < 1 || filter.Limit > 100 {
		return nil, apperrors.ErrBadRequest
	}
	result := &PricePage{Items: []PriceRecord{}}
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
		query := tx.Order("id").Limit(filter.Limit + 1)
		if filter.ProviderModelID != "" {
			query = query.Where("provider_model_id = ?", filter.ProviderModelID)
		}
		if filter.Cursor != "" {
			query = query.Where("id > ?", filter.Cursor)
		}
		var rows []entity.ModelPrice
		if err = query.Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) > filter.Limit {
			rows = rows[:filter.Limit]
			result.NextCursor = rows[len(rows)-1].ID
		}
		for _, row := range rows {
			item, err := loadPrice(tx, row)
			if err != nil {
				return err
			}
			result.Items = append(result.Items, item)
		}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	return result, pricingError(err)
}
func (s *Service) GetPrice(ctx context.Context, actorID, providerModelID string) (*PricePage, error) {
	result, err := s.ListPrices(ctx, actorID, PriceFilter{ProviderModelID: providerModelID, Limit: 1})
	if err != nil {
		return nil, err
	}
	if len(result.Items) == 0 {
		return nil, apperrors.ErrNotFound
	}
	return result, nil
}
func (s *Service) QuotePrice(ctx context.Context, actorID, providerModelID string, usage pricing.Usage) (*PriceQuote, error) {
	page, err := s.GetPrice(ctx, actorID, providerModelID)
	if err != nil {
		if errors.Is(err, apperrors.ErrNotFound) {
			return nil, pricingMissing
		}
		return nil, err
	}
	quote, err := pricing.Calculate(priceSchedule(page.Items[0]), page.Currency, usage)
	return &PriceQuote{ETag: page.ETag, Quote: quote}, pricingError(err)
}

// appendPricingAudit accepts normalized catalogue values only, never arbitrary
// request payloads. The bounded batch (20 models, 8 rates each) limits volume.
func appendPricingAudit(tx *gorm.DB, actorID, action, resourceID string, before, after any) error {
	details, err := json.Marshal(struct {
		Source string `json:"source"`
		Before any    `json:"before"`
		After  any    `json:"after"`
	}{"api", before, after})
	if err != nil {
		return err
	}
	if len(details) > 60*1024 {
		return apperrors.ErrBadRequest
	}
	auditID, err := id.NewPrefixed("aud")
	if err != nil {
		return err
	}
	value := string(details)
	return tx.Create(&entity.AuditEvent{ID: auditID, ActorID: actorID, Action: action, ResourceType: "pricing", ResourceID: resourceID, DetailsJSON: &value}).Error
}
func updatePricingETag(tx *gorm.DB, setting *entity.PricingSetting) error {
	etag, err := secret.RandomURLSafe(24)
	if err != nil {
		return err
	}
	setting.ETag = etag
	return tx.Model(&entity.PricingSetting{}).Where("id = ?", 1).Update("ETag", etag).Error
}
func validateEnabledPriceFX(tx *gorm.DB, fx pricing.FX) error {
	var currencies []string
	if err := tx.Model(&entity.PriceRate{}).Where("enabled = ?", true).Distinct("currency").Pluck("currency", &currencies).Error; err != nil {
		return err
	}
	for _, currency := range currencies {
		if currency != fx.PlatformCurrency {
			if _, ok := fx.Rates[currency]; !ok {
				return pricing.ErrUnpriced
			}
		}
	}
	return nil
}
func (s *Service) WritePrices(ctx context.Context, actorID, etag string, items []PriceInput) (*PricePage, error) {
	if etag == "" || len(items) == 0 || len(items) > 20 {
		return nil, apperrors.ErrBadRequest
	}
	seen := map[string]bool{}
	for _, item := range items {
		if item.ProviderModelID == "" || seen[item.ProviderModelID] || len(item.Rates) > 8 || len(item.Rates) == 0 {
			return nil, apperrors.ErrBadRequest
		}
		seen[item.ProviderModelID] = true
		for _, rate := range item.Rates {
			if rate.ID != "" || pricing.ValidateRate(rate) != nil {
				return nil, apperrors.ErrBadRequest
			}
		}
	}
	result := &PricePage{Items: []PriceRecord{}}
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		if err := authorizeGovernance(tx, actorID, "prices.write"); err != nil {
			return err
		}
		setting, err := lockPricing(tx)
		if err != nil {
			return err
		}
		if setting.ETag != etag {
			return pricingStale
		}
		fx, err := pricingFX(tx, setting)
		if err != nil {
			return err
		}
		before := []PriceRecord{}
		for _, item := range items {
			old, updated, err := writePrice(tx, item)
			if err != nil {
				return err
			}
			if old != nil {
				before = append(before, *old)
			}
			result.Items = append(result.Items, *updated)
		}
		if err = validateEnabledPriceFX(tx, fx); err != nil {
			return err
		}
		if err = updatePricingETag(tx, &setting); err != nil {
			return err
		}
		result.ETag = setting.ETag
		result.Currency = fx
		return appendPricingAudit(tx, actorID, "prices.update", "pricing_catalogue", struct {
			ETag  string        `json:"etag"`
			Items []PriceRecord `json:"items"`
		}{etag, before}, struct {
			ETag  string        `json:"etag"`
			Items []PriceRecord `json:"items"`
		}{setting.ETag, result.Items})
	})
	return result, pricingError(err)
}
func writePrice(tx *gorm.DB, input PriceInput) (*PriceRecord, *PriceRecord, error) {
	var providerModel entity.ProviderModel
	var connection entity.ProviderConnection
	if err := tx.First(&providerModel, "id = ?", input.ProviderModelID).Error; err != nil {
		return nil, nil, err
	}
	if err := tx.First(&connection, "id = ?", providerModel.ConnectionID).Error; err != nil {
		return nil, nil, err
	}
	if connection.Protocol != entity.ProtocolOpenAIChat {
		return nil, nil, apperrors.ErrBadRequest
	}
	var model entity.ModelPrice
	var before *PriceRecord
	err := tx.First(&model, "provider_model_id = ?", input.ProviderModelID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		model.ID, err = id.NewPrefixed("prc")
		if err != nil {
			return nil, nil, err
		}
		model.ProviderModelID = input.ProviderModelID
	} else if err != nil {
		return nil, nil, err
	} else {
		record, err := loadPrice(tx, model)
		if err != nil {
			return nil, nil, err
		}
		before = &record
	}
	if input.ContextThreshold != nil {
		model.ContextThreshold = *input.ContextThreshold
	}
	model.UpdateSource = "api"
	model.FollowRepository = false
	if err = tx.Save(&model).Error; err != nil {
		return nil, nil, err
	}
	submitted := map[string]bool{}
	for _, inputRate := range input.Rates {
		key := inputRate.Metric + "/" + inputRate.Tier
		if submitted[key] {
			return nil, nil, apperrors.ErrBadRequest
		}
		submitted[key] = true
		var rate entity.PriceRate
		err = tx.Where("model_price_id = ? AND metric = ? AND tier = ?", model.ID, inputRate.Metric, inputRate.Tier).First(&rate).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			rate.ID, err = id.NewPrefixed("rat")
			if err != nil {
				return nil, nil, err
			}
			rate.ModelPriceID = model.ID
		} else if err != nil {
			return nil, nil, err
		}
		rate.Metric = inputRate.Metric
		rate.Tier = inputRate.Tier
		rate.Unit = inputRate.Unit
		rate.Currency = inputRate.Currency
		rate.Amount, _ = pricing.Decimal(inputRate.Amount)
		rate.Enabled = inputRate.Enabled
		if err = tx.Save(&rate).Error; err != nil {
			return nil, nil, err
		}
	}
	result, err := loadPrice(tx, model)
	if err != nil {
		return nil, nil, err
	}
	if err = pricing.ValidateSchedule(priceSchedule(result)); err != nil {
		return nil, nil, err
	}
	return before, &result, nil
}

// WritePricingCurrency replaces the finite exchange-rate set atomically. All
// enabled catalogue currencies must remain convertible to the new platform unit.
func (s *Service) WritePricingCurrency(ctx context.Context, actorID, etag string, fx pricing.FX) (*PricePage, error) {
	if etag == "" || pricing.ValidateFX(fx) != nil {
		return nil, apperrors.ErrBadRequest
	}
	normalized := pricing.FX{PlatformCurrency: fx.PlatformCurrency, Rates: map[string]string{fx.PlatformCurrency: "1"}}
	for currency, rate := range fx.Rates {
		normalized.Rates[currency], _ = pricing.Decimal(rate)
	}
	result := &PricePage{Items: []PriceRecord{}, Currency: normalized}
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		if err := authorizeGovernance(tx, actorID, "prices.write"); err != nil {
			return err
		}
		setting, err := lockPricing(tx)
		if err != nil {
			return err
		}
		if setting.ETag != etag {
			return pricingStale
		}
		before, err := pricingFX(tx, setting)
		if err != nil {
			return err
		}
		if err = validateEnabledPriceFX(tx, normalized); err != nil {
			return err
		}
		if err = tx.Where("1 = 1").Delete(&entity.PricingExchangeRate{}).Error; err != nil {
			return err
		}
		currencies := make([]string, 0, len(normalized.Rates))
		for currency := range normalized.Rates {
			currencies = append(currencies, currency)
		}
		slices.Sort(currencies)
		for _, currency := range currencies {
			if err = tx.Create(&entity.PricingExchangeRate{Currency: currency, Rate: normalized.Rates[currency]}).Error; err != nil {
				return err
			}
		}
		if err = tx.Model(&entity.PricingSetting{}).Where("id = ?", 1).Update("platform_currency", normalized.PlatformCurrency).Error; err != nil {
			return err
		}
		if err = updatePricingETag(tx, &setting); err != nil {
			return err
		}
		result.ETag = setting.ETag
		return appendPricingAudit(tx, actorID, "prices.currency.update", "pricing_catalogue", struct {
			ETag     string     `json:"etag"`
			Currency pricing.FX `json:"currency"`
		}{etag, before}, struct {
			ETag     string     `json:"etag"`
			Currency pricing.FX `json:"currency"`
		}{setting.ETag, normalized})
	})
	return result, pricingError(err)
}
