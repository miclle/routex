package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"math/big"
	"sort"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/eventqueue"
	"github.com/miclle/routex/pkg/pricing"
	"github.com/miclle/routex/pkg/routeattempt"
)

// preflightGatewayQuota checks the durable journal before attachment storage
// reads without consuming RPM, creating a lease/fact, or holding quota. Final
// admission repeats the policy read and every check in one atomic reservation.
func (s *Service) preflightGatewayQuota(ctx context.Context, requestID string, result *GatewayResult) error {
	s.limitMu.RLock()
	defer s.limitMu.RUnlock()
	limits, err := s.gatewayLimits(ctx, result)
	if err != nil {
		return err
	}
	policies, data, err := s.gatewayQuotaPolicies(ctx, result, limits)
	if err != nil {
		return err
	}
	bound, err := prepareQuotaBound(result, policies, data)
	if err != nil {
		return err
	}
	if s.recorder == nil {
		for _, policy := range policies {
			if policy.Tokens5H != nil || policy.Tokens7D != nil || policy.TokensMonth != nil || policy.TPM != nil || policy.MoneyMonth != nil || policy.RPM != nil || policy.Concurrency != nil {
				return callQueueUnavailable
			}
		}
		return nil
	}
	if err := s.recorder.queue.PreflightWithQuota(requestID, policies, bound, data.Setting.TimeZone, time.Now().UTC()); err != nil {
		return quotaGatewayError(err)
	}
	return nil
}

// quotaRequest retains only finite classification and output capacity. It never
// stores messages, schemas, user metadata, or other native request content.
type quotaRequest struct {
	Supported             bool
	MaxOutput             int64
	CacheRead, CacheWrite bool
}

func inspectQuotaRequest(protocol string, payload map[string]json.RawMessage, plans ...*gatewayAttachmentPlan) quotaRequest {
	if len(plans) == 1 && plans[0] != nil && len(plans[0].Occurrences) > 0 {
		if detached, ok := quotaAttachmentPayload(protocol, plans[0]); ok {
			payload = detached
		}
	}
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
	tokenConstrained, moneyConstrained := false, false
	for _, policy := range policies {
		hasTokens := policy.Tokens5H != nil || policy.Tokens7D != nil || policy.TokensMonth != nil || policy.TPM != nil
		tokenConstrained = tokenConstrained || hasTokens
		moneyConstrained = moneyConstrained || policy.MoneyMonth != nil
	}
	constrained := tokenConstrained || moneyConstrained
	empty := eventqueue.QuotaBound{}
	if !result.quotaRequest.Supported {
		if constrained {
			return empty, gatewayError(400, "quota_request_unsupported", "The request cannot be bounded under the active resource policy.")
		}
		return empty, nil
	}
	if result.PricingUnsupported && moneyConstrained {
		return empty, gatewayError(503, "quota_price_unavailable", "A complete reservation price is unavailable.")
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
	if !result.PricingUnsupported && result.PriceBasis != nil &&
		(result.PriceBasis.Adapter == pricing.TextAdapter || result.PriceBasis.Adapter == pricing.MultimodalAdapter) &&
		result.PriceBasis.Schedule.ProviderModelID == result.ProviderModelID && result.PriceBasis.Schedule.Protocol == result.NativeProtocol() {
		quote, err := pricing.ReserveBound(result.PriceBasis.Schedule, result.PriceBasis.Currency, pricing.Capacity{Input: capacity.MaxInputTokens, Output: result.quotaRequest.MaxOutput, CacheRead: result.quotaRequest.CacheRead, CacheWrite: result.quotaRequest.CacheWrite, ImageInputs: result.ImageInputs, PDFInputs: result.PDFInputs})
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
	if fact.NoWork || len(fact.Attempts) > 0 && callAttemptsProveNoWork(fact.Attempts) {
		zeroTokens, zeroMoney := int64(0), "0"
		actual.Tokens = &zeroTokens
		if fact.PriceBasis != nil && fact.PriceBasis.Currency.PlatformCurrency != "" {
			actual.Money = &zeroMoney
			actual.Currency = fact.PriceBasis.Currency.PlatformCurrency
		}
		return actual
	}
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

func callAttemptsProveNoWork(attempts []CallAttempt) bool {
	for _, attempt := range attempts {
		if attempt.WorkEvidence != string(routeattempt.NotSent) && attempt.WorkEvidence != string(routeattempt.RejectedWithoutWork) {
			return false
		}
	}
	return true
}

type gatewayAttemptBoundEvidence struct {
	TargetID       string `json:"target_id"`
	ProviderModel  string `json:"provider_model_id"`
	BoundRevision  string `json:"bound_revision,omitempty"`
	InputTokens    int64  `json:"input_tokens,omitempty"`
	OutputTokens   int64  `json:"output_tokens,omitempty"`
	PriceRevision  string `json:"price_revision,omitempty"`
	PriceDigest    string `json:"price_digest,omitempty"`
	ReservedAmount string `json:"reserved_amount,omitempty"`
	PriceCurrency  string `json:"price_currency,omitempty"`
}

// prepareGatewayAttemptQuotaBound filters a detached plan to candidates that
// are currently eligible and have complete evidence for every active quota
// dimension. The returned conservative bound is admitted once for the logical
// request; terminal settlement still uses the actual route's immutable basis.
func (s *Service) prepareGatewayAttemptQuotaBound(ctx context.Context, plan *gatewayAttemptPlan, result *GatewayResult, policies []eventqueue.QuotaLimit, data *runtimeQuotaData) (*gatewayAttemptPlan, eventqueue.QuotaBound, error) {
	empty := eventqueue.QuotaBound{}
	if plan == nil || result == nil || data == nil {
		return nil, empty, runtimeUnavailable
	}
	tokenConstrained, moneyConstrained := gatewayQuotaDimensions(policies)
	if (tokenConstrained || moneyConstrained) && !result.quotaRequest.Supported {
		return nil, empty, gatewayError(400, "quota_request_unsupported", "The request cannot be bounded under the active resource policy.")
	}
	if moneyConstrained && result.PricingUnsupported {
		return nil, empty, gatewayError(503, "quota_price_unavailable", "A complete reservation price is unavailable.")
	}

	retained := []gatewayAttemptCandidate{}
	evidenceByTarget := map[string]gatewayAttemptBoundEvidence{}
	maxTokens := int64(0)
	maxMoney := new(big.Rat)
	maxMoneyText := ""
	currency := ""
	eligibleCount := 0
	for _, candidate := range plan.Candidates() {
		eligible, err := s.gatewayAttemptEligible(ctx, plan, candidate.attempt)
		if err != nil {
			return nil, empty, err
		}
		if !eligible {
			continue
		}
		eligibleCount++
		evidence, exists := evidenceByTarget[candidate.attempt.TargetID]
		if !exists {
			var usable bool
			evidence, usable = gatewayCandidateBoundEvidence(candidate, result, data, tokenConstrained, moneyConstrained)
			if !usable {
				continue
			}
			evidenceByTarget[candidate.attempt.TargetID] = evidence
			if tokenConstrained {
				total := evidence.InputTokens + evidence.OutputTokens
				if total > maxTokens {
					maxTokens = total
				}
			}
			if moneyConstrained {
				amount, ok := new(big.Rat).SetString(evidence.ReservedAmount)
				if !ok {
					return nil, empty, gatewayError(503, "quota_price_unavailable", "A complete reservation price is unavailable.")
				}
				if currency == "" {
					currency = data.Currency
					if currency == "" {
						currency = evidence.PriceCurrency
					}
				}
				if evidence.PriceCurrency != currency {
					delete(evidenceByTarget, candidate.attempt.TargetID)
					continue
				}
				if maxMoneyText == "" || amount.Cmp(maxMoney) > 0 {
					maxMoney.Set(amount)
					maxMoneyText = evidence.ReservedAmount
				}
			}
		}
		retained = append(retained, candidate)
	}
	if len(retained) == 0 {
		if eligibleCount == 0 {
			return nil, empty, gatewayError(503, "upstream_unavailable", "No usable upstream is available.")
		}
		if moneyConstrained {
			return nil, empty, gatewayError(503, "quota_price_unavailable", "A complete reservation price is unavailable.")
		}
		return nil, empty, gatewayError(503, "quota_bound_unavailable", "A verified request capacity is unavailable.")
	}
	filtered, err := plan.filtered(retained)
	if err != nil {
		return nil, empty, runtimeUnavailable
	}
	evidence := make([]gatewayAttemptBoundEvidence, 0, len(evidenceByTarget))
	for _, item := range evidenceByTarget {
		evidence = append(evidence, item)
	}
	sort.Slice(evidence, func(i, j int) bool { return evidence[i].TargetID < evidence[j].TargetID })
	bound := eventqueue.QuotaBound{}
	if tokenConstrained || moneyConstrained {
		bound.Revision = gatewayAttemptEvidenceDigest(evidence, "bound")
	}
	if tokenConstrained {
		bound.Tokens = &maxTokens
	}
	if moneyConstrained {
		bound.Money = &maxMoneyText
		bound.Currency = currency
		bound.PriceRevision = gatewayAttemptEvidenceDigest(evidence, "price")
		bound.BasisDigest = gatewayAttemptEvidenceDigest(evidence, "basis")
	}
	return filtered, bound, nil
}

func gatewayQuotaDimensions(policies []eventqueue.QuotaLimit) (bool, bool) {
	tokens, money := false, false
	for _, policy := range policies {
		tokens = tokens || policy.Tokens5H != nil || policy.Tokens7D != nil || policy.TokensMonth != nil || policy.TPM != nil
		money = money || policy.MoneyMonth != nil
	}
	return tokens, money
}

func gatewayCandidateBoundEvidence(candidate gatewayAttemptCandidate, result *GatewayResult, data *runtimeQuotaData, tokenConstrained, moneyConstrained bool) (gatewayAttemptBoundEvidence, bool) {
	evidence := gatewayAttemptBoundEvidence{TargetID: candidate.attempt.TargetID, ProviderModel: candidate.route.ProviderModelID}
	if !tokenConstrained && !moneyConstrained {
		return evidence, true
	}
	capacity, exists := data.Bounds[candidate.route.ProviderModelID]
	if !exists || capacity.Protocol != result.NativeProtocol() || capacity.ETag == "" || capacity.MaxInputTokens <= 0 || capacity.MaxOutputTokens <= 0 || result.quotaRequest.MaxOutput > capacity.MaxOutputTokens || result.quotaRequest.MaxOutput > math.MaxInt64-capacity.MaxInputTokens {
		return evidence, false
	}
	evidence.BoundRevision = capacity.ETag
	evidence.InputTokens = capacity.MaxInputTokens
	evidence.OutputTokens = result.quotaRequest.MaxOutput
	if !moneyConstrained {
		return evidence, true
	}
	basis := candidate.route.PriceBasis
	if basis == nil || basis.ETag == "" || (basis.Adapter != pricing.TextAdapter && basis.Adapter != pricing.MultimodalAdapter) || basis.Schedule.ProviderModelID != candidate.route.ProviderModelID || basis.Schedule.Protocol != result.NativeProtocol() {
		return evidence, false
	}
	quote, err := pricing.ReserveBound(basis.Schedule, basis.Currency, pricing.Capacity{Input: capacity.MaxInputTokens, Output: result.quotaRequest.MaxOutput, CacheRead: result.quotaRequest.CacheRead, CacheWrite: result.quotaRequest.CacheWrite, ImageInputs: result.ImageInputs, PDFInputs: result.PDFInputs})
	if err != nil {
		return evidence, false
	}
	raw, err := json.Marshal(basis)
	if err != nil {
		return evidence, false
	}
	digest := sha256.Sum256(raw)
	evidence.PriceRevision = basis.ETag
	evidence.PriceDigest = hex.EncodeToString(digest[:])
	evidence.ReservedAmount = quote.Amount
	evidence.PriceCurrency = quote.Currency
	return evidence, true
}

func gatewayAttemptEvidenceDigest(evidence []gatewayAttemptBoundEvidence, kind string) string {
	type digestEvidence struct {
		TargetID, ProviderModel, Revision, Digest, Amount, Currency string
		InputTokens, OutputTokens                                   int64
	}
	selected := make([]digestEvidence, 0, len(evidence))
	for _, item := range evidence {
		row := digestEvidence{TargetID: item.TargetID, ProviderModel: item.ProviderModel}
		switch kind {
		case "bound":
			row.Revision, row.InputTokens, row.OutputTokens = item.BoundRevision, item.InputTokens, item.OutputTokens
		case "price":
			row.Revision, row.Amount, row.Currency = item.PriceRevision, item.ReservedAmount, item.PriceCurrency
		case "basis":
			row.Digest = item.PriceDigest
		}
		selected = append(selected, row)
	}
	raw, _ := json.Marshal(selected)
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}
