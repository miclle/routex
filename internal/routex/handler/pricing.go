package handler

import (
	"github.com/fox-gonic/fox"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/pricing"
)

type PriceListRequest struct {
	ProviderModelID string `query:"provider_model_id"`
	Cursor          string `query:"cursor"`
	Limit           int    `query:"limit"`
}
type PricePath struct {
	ProviderModelID string `uri:"provider_model_id"`
}
type PriceRateInput struct {
	Metric   string `json:"metric"`
	Tier     string `json:"tier"`
	Unit     string `json:"unit"`
	Currency string `json:"currency"`
	Amount   string `json:"amount"`
	Enabled  *bool  `json:"enabled"`
}
type PriceWriteItem struct {
	ProviderModelID  string           `json:"provider_model_id"`
	ContextThreshold *int64           `json:"context_threshold"`
	Rates            []PriceRateInput `json:"rates"`
}
type WritePricesRequest struct {
	ETag  string           `json:"etag"`
	Items []PriceWriteItem `json:"items"`
}
type WritePricingCurrencyRequest struct {
	ETag     string     `json:"etag"`
	Currency pricing.FX `json:"currency"`
}
type QuotePriceRequest struct {
	ProviderModelID string `json:"provider_model_id"`
	Usage           struct {
		InputTokens      *int64 `json:"input_tokens"`
		OutputTokens     *int64 `json:"output_tokens"`
		CacheReadTokens  *int64 `json:"cache_read_tokens"`
		CacheWriteTokens *int64 `json:"cache_write_tokens"`
	} `json:"usage"`
}

func (ctrl *Ctrl) ListPrices(c *fox.Context, request PriceListRequest) (*service.PricePage, error) {
	return ctrl.service.ListPrices(c.Request.Context(), currentAuthentication(c).User.ID, service.PriceFilter{ProviderModelID: request.ProviderModelID, Cursor: request.Cursor, Limit: request.Limit})
}
func (ctrl *Ctrl) GetPrice(c *fox.Context, request PricePath) (*service.PricePage, error) {
	return ctrl.service.GetPrice(c.Request.Context(), currentAuthentication(c).User.ID, request.ProviderModelID)
}
func (ctrl *Ctrl) WritePrices(c *fox.Context) (*service.PricePage, error) {
	var request WritePricesRequest
	if err := decodeStrictRequest(c, &request); err != nil {
		return nil, err
	}
	items := make([]service.PriceInput, 0, len(request.Items))
	for _, item := range request.Items {
		input := service.PriceInput{ProviderModelID: item.ProviderModelID, ContextThreshold: item.ContextThreshold, Rates: []pricing.Rate{}}
		for _, rate := range item.Rates {
			if rate.Enabled == nil {
				return nil, apperrors.ErrBadRequest
			}
			input.Rates = append(input.Rates, pricing.Rate{Metric: rate.Metric, Tier: rate.Tier, Unit: rate.Unit, Currency: rate.Currency, Amount: rate.Amount, Enabled: *rate.Enabled})
		}
		items = append(items, input)
	}
	return ctrl.service.WritePrices(c.Request.Context(), currentAuthentication(c).User.ID, request.ETag, items)
}
func (ctrl *Ctrl) WritePricingCurrency(c *fox.Context) (*service.PricePage, error) {
	var request WritePricingCurrencyRequest
	if err := decodeStrictRequest(c, &request); err != nil {
		return nil, err
	}
	return ctrl.service.WritePricingCurrency(c.Request.Context(), currentAuthentication(c).User.ID, request.ETag, request.Currency)
}
func (ctrl *Ctrl) QuotePrice(c *fox.Context) (*service.PriceQuote, error) {
	var request QuotePriceRequest
	if err := decodeStrictRequest(c, &request); err != nil {
		return nil, err
	}
	usage := request.Usage
	if request.ProviderModelID == "" || usage.InputTokens == nil || usage.OutputTokens == nil || usage.CacheReadTokens == nil || usage.CacheWriteTokens == nil {
		return nil, apperrors.ErrBadRequest
	}
	return ctrl.service.QuotePrice(c.Request.Context(), currentAuthentication(c).User.ID, request.ProviderModelID, pricing.Usage{InputTokens: *usage.InputTokens, OutputTokens: *usage.OutputTokens, CacheReadTokens: *usage.CacheReadTokens, CacheWriteTokens: *usage.CacheWriteTokens})
}
