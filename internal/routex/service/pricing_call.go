package service

import (
	"encoding/json"
	"errors"
	"regexp"
	"slices"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/pricing"
)

const callPricingSnapshotLimit = 16 << 10

// CallPricing is finalized before durable journal completion. Replay persists the
// receipt verbatim instead of consulting current rates or recalculating money.
type CallPricing struct {
	Status       string
	ETag         string
	Amount       *string
	Currency     *string
	SnapshotJSON *string
}
type callPricingSnapshot struct {
	Basis                 *CallPriceBasis `json:"basis"`
	UnsupportedDimensions []string        `json:"unsupported_dimensions,omitempty"`
	Quote                 *pricing.Quote  `json:"quote,omitempty"`
}

func finalizeCallPricing(fact *CallFact) {
	if fact.Pricing != nil {
		return
	}
	result := &CallPricing{Status: "not_captured"}
	fact.Pricing = result
	if fact.PriceBasis == nil {
		return
	}
	result.ETag = fact.PriceBasis.ETag
	snapshot := callPricingSnapshot{Basis: clonePriceBasis(fact.PriceBasis), UnsupportedDimensions: slices.Clone(fact.PricingDimensions)}
	switch {
	case fact.PricingUnsupported || fact.PriceBasis.Adapter != "routex_text_v1":
		result.Status = "unsupported"
	case !fact.UsageComplete:
		result.Status = "not_final"
	case fact.InputTokens == nil || fact.OutputTokens == nil || fact.CacheReadTokens == nil || fact.CacheWriteTokens == nil:
		result.Status = "unknown_usage"
	default:
		usage := pricing.Usage{InputTokens: *fact.InputTokens, OutputTokens: *fact.OutputTokens, CacheReadTokens: *fact.CacheReadTokens, CacheWriteTokens: *fact.CacheWriteTokens}
		if usage.CacheReadTokens > usage.InputTokens || usage.CacheWriteTokens > usage.InputTokens-usage.CacheReadTokens {
			result.Status = "invalid_usage"
			break
		}
		quote, err := pricing.Calculate(snapshot.Basis.Schedule, snapshot.Basis.Currency, usage)
		switch {
		case errors.Is(err, pricing.ErrUnpriced):
			result.Status = "missing_price"
		case err != nil:
			result.Status = "invalid_configuration"
		default:
			result.Status = "priced"
			result.Amount = &quote.Total
			result.Currency = &quote.Currency
			snapshot.Quote = quote
		}
	}
	raw, err := json.Marshal(snapshot)
	if err != nil || len(raw) > callPricingSnapshotLimit {
		result.Status = "invalid_configuration"
		result.Amount = nil
		result.Currency = nil
		return
	}
	encoded := string(raw)
	result.SnapshotJSON = &encoded
}

var chargeAmountPattern = regexp.MustCompile(`^(0|[1-9][0-9]{0,59})(\.[0-9]{1,18})?$`)

func validateCallPricing(fact CallFact) error {
	if len(fact.PricingDimensions) > 7 {
		return apperrors.ErrBadRequest
	}
	for _, dimension := range fact.PricingDimensions {
		switch dimension {
		case "request_non_text", "response_non_text", "request_service_tier", "response_service_tier", "cache_retention", "external_tool", "request_condition":
		default:
			return apperrors.ErrBadRequest
		}
	}

	if (fact.CacheReadTokens != nil && *fact.CacheReadTokens < 0) || (fact.CacheWriteTokens != nil && *fact.CacheWriteTokens < 0) {
		return apperrors.ErrBadRequest
	}
	if fact.PriceBasis != nil {
		if fact.PriceBasis.Schedule.ProviderModelID != fact.ProviderModelID || len(fact.PriceBasis.ETag) > 64 || len(fact.PriceBasis.Schedule.Rates) > 8 || len(fact.PriceBasis.Currency.Rates) > 7 {
			return apperrors.ErrBadRequest
		}
		if size := pricingBasisSize(fact.PriceBasis); size < 0 || size > callPricingSnapshotLimit {
			return apperrors.ErrBadRequest
		}
	}
	receipt := fact.Pricing
	if receipt == nil {
		return nil
	}
	switch receipt.Status {
	case "not_captured", "unsupported", "not_final", "unknown_usage", "invalid_usage", "missing_price", "invalid_configuration", "priced":
	default:
		return apperrors.ErrBadRequest
	}
	if len(receipt.ETag) > 64 {
		return apperrors.ErrBadRequest
	}
	if receipt.Status == "priced" {
		if receipt.Amount == nil || !chargeAmountPattern.MatchString(*receipt.Amount) || receipt.Currency == nil || !pricing.Currency(*receipt.Currency) || receipt.SnapshotJSON == nil {
			return apperrors.ErrBadRequest
		}
	} else if receipt.Amount != nil || receipt.Currency != nil {
		return apperrors.ErrBadRequest
	}
	if receipt.SnapshotJSON != nil && (len(*receipt.SnapshotJSON) > callPricingSnapshotLimit || !json.Valid([]byte(*receipt.SnapshotJSON))) {
		return apperrors.ErrBadRequest
	}
	return nil
}
func callPricingFields(fact CallFact) entity.CallPricingFields {
	result := entity.CallPricingFields{CacheReadTokens: fact.CacheReadTokens, CacheWriteTokens: fact.CacheWriteTokens, PricingStatus: "not_captured"}
	if fact.Pricing != nil {
		result.PricingStatus = fact.Pricing.Status
		result.PriceETag = fact.Pricing.ETag
		result.ChargeAmount = fact.Pricing.Amount
		result.ChargeCurrency = fact.Pricing.Currency
		result.PricingSnapshotJSON = fact.Pricing.SnapshotJSON
	}
	return result
}
