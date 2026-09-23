package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/eventqueue"
	"github.com/miclle/routex/pkg/pricing"
)

// quotaRequest retains only finite classification and output capacity. It never
// stores messages, schemas, user metadata, or other native request content.
type quotaRequest struct {
	Supported             bool
	MaxOutput             int64
	CacheRead, CacheWrite bool
}

func inspectQuotaRequest(protocol string, payload map[string]json.RawMessage) quotaRequest {
	switch protocol {
	case entity.ProtocolOpenAIChat:
		return inspectChatQuotaRequest(payload)
	case entity.ProtocolOpenAIResponses:
		return inspectResponsesQuotaRequest(payload)
	case entity.ProtocolAnthropicMessages:
		return inspectMessagesQuotaRequest(payload)
	case entity.ProtocolGeminiGenerateContent:
		return inspectGeminiQuotaRequest(payload)
	default:
		return quotaRequest{}
	}
}
func inspectChatQuotaRequest(payload map[string]json.RawMessage) quotaRequest {
	if !supportsTextPricing(payload) {
		return quotaRequest{}
	}
	allowed := map[string]bool{}
	for _, key := range []string{"model", "messages", "max_completion_tokens", "stream", "stream_options", "temperature", "top_p", "stop", "presence_penalty", "frequency_penalty", "seed", "logprobs", "top_logprobs", "metadata", "user", "safety_identifier", "prompt_cache_key", "service_tier", "n", "response_format", "reasoning_effort", "tools", "tool_choice", "parallel_tool_calls"} {
		allowed[key] = true
	}
	for key := range payload {
		if !allowed[key] {
			return quotaRequest{}
		}
	}
	if raw, exists := payload["n"]; exists {
		var count int64
		if json.Unmarshal(raw, &count) != nil || count != 1 {
			return quotaRequest{}
		}
	}
	maximum := usageCounter(payload["max_completion_tokens"])
	if maximum == nil || *maximum <= 0 {
		return quotaRequest{}
	}
	return quotaRequest{Supported: true, MaxOutput: *maximum, CacheRead: true, CacheWrite: true}
}
func prepareQuotaBound(result *GatewayResult, policies []eventqueue.QuotaLimit, data *runtimeQuotaData) (eventqueue.QuotaBound, error) {
	constrained, moneyConstrained := false, false
	for _, policy := range policies {
		hasTokens := policy.Tokens5H != nil || policy.Tokens7D != nil || policy.TokensMonth != nil || policy.TPM != nil
		moneyConstrained = moneyConstrained || policy.MoneyMonth != nil
		constrained = constrained || hasTokens || policy.MoneyMonth != nil
	}
	empty := eventqueue.QuotaBound{}
	if !result.quotaRequest.Supported || result.PricingUnsupported {
		if constrained {
			return empty, gatewayError(400, "quota_request_unsupported", "The request cannot be bounded under the active resource policy.")
		}
		return empty, nil
	}
	capacity, exists := data.Bounds[result.ProviderModelID]
	if !exists || capacity.Protocol != result.NativeProtocol() || capacity.ETag == "" || capacity.MaxInputTokens <= 0 || capacity.MaxOutputTokens <= 0 {
		if constrained {
			return empty, gatewayError(503, "quota_bound_unavailable", "A verified request capacity is unavailable.")
		}
		return empty, nil
	}
	if result.quotaRequest.MaxOutput > capacity.MaxOutputTokens || result.quotaRequest.MaxOutput > math.MaxInt64-capacity.MaxInputTokens {
		if constrained {
			return empty, gatewayError(400, "quota_request_unsupported", "The requested output exceeds the configured capacity.")
		}
		return empty, nil
	}
	total := capacity.MaxInputTokens + result.quotaRequest.MaxOutput
	bound := eventqueue.QuotaBound{Tokens: &total, Revision: capacity.ETag}
	if result.PriceBasis != nil && result.PriceBasis.Adapter == "routex_text_v1" && result.PriceBasis.Schedule.ProviderModelID == result.ProviderModelID && result.PriceBasis.Schedule.Protocol == result.NativeProtocol() {
		quote, err := pricing.ReserveBound(result.PriceBasis.Schedule, result.PriceBasis.Currency, pricing.Capacity{Input: capacity.MaxInputTokens, Output: result.quotaRequest.MaxOutput, CacheRead: result.quotaRequest.CacheRead, CacheWrite: result.quotaRequest.CacheWrite})
		if err == nil {
			bound.Money = &quote.Amount
			bound.Currency = quote.Currency
			bound.PriceRevision = result.PriceBasis.ETag
			raw, _ := json.Marshal(result.PriceBasis)
			digest := sha256.Sum256(raw)
			bound.BasisDigest = hex.EncodeToString(digest[:])
		}
	}
	if moneyConstrained && bound.Money == nil {
		return empty, gatewayError(503, "quota_price_unavailable", "A complete reservation price is unavailable.")
	}
	return bound, nil
}
func quotaGatewayError(err error) error {
	switch {
	case errors.Is(err, eventqueue.ErrQuotaTokens), errors.Is(err, eventqueue.ErrQuotaMoney):
		return gatewayError(429, "quota_exceeded", "The resource allowance has been reached.")
	case errors.Is(err, eventqueue.ErrQuotaCoverage):
		return gatewayError(503, "quota_history_incomplete", "The required quota window is not fully covered.")
	case errors.Is(err, eventqueue.ErrQuotaUnknown):
		return gatewayError(503, "quota_usage_unknown", "The required quota window contains unbounded usage.")
	case errors.Is(err, eventqueue.ErrQuotaCurrency):
		return gatewayError(503, "quota_currency_mismatch", "The monetary quota currency is incompatible.")
	case errors.Is(err, eventqueue.ErrQuotaBound):
		return gatewayError(503, "quota_bound_unavailable", "A verified request capacity is unavailable.")
	default:
		return gatewayLimitError(err)
	}
}
func quotaCallSettlement(fact CallFact) eventqueue.QuotaSettlement {
	actual := eventqueue.QuotaSettlement{}
	if fact.UsageComplete && fact.InputTokens != nil && fact.OutputTokens != nil && *fact.InputTokens >= 0 && *fact.OutputTokens >= 0 && *fact.InputTokens <= math.MaxInt64-*fact.OutputTokens {
		total := *fact.InputTokens + *fact.OutputTokens
		actual.Tokens = &total
	}
	if fact.Pricing != nil && fact.Pricing.Status == "priced" && fact.Pricing.Amount != nil && fact.Pricing.Currency != nil {
		actual.Money = fact.Pricing.Amount
		actual.Currency = *fact.Pricing.Currency
	}
	return actual
}
