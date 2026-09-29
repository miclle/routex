package service

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/eventqueue"
	"github.com/miclle/routex/pkg/limits"
)

func quotaTestString(value string) *string { return &value }
func quotaPayload(t *testing.T, raw string) map[string]json.RawMessage {
	t.Helper()
	var payload map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatal(err)
	}
	return payload
}

func TestGatewayNoWorkAttemptSettlement(t *testing.T) {
	fact := CallFact{PriceBasis: testPriceBasis(), Attempts: []CallAttempt{
		{WorkEvidence: "not_sent"},
		{WorkEvidence: "rejected_without_work"},
	}}
	settlement := quotaCallSettlement(fact)
	if settlement.Tokens == nil || *settlement.Tokens != 0 || settlement.Money == nil || *settlement.Money != "0" || settlement.Currency != fact.PriceBasis.Currency.PlatformCurrency {
		t.Fatalf("no-work settlement = %+v", settlement)
	}
	fact.Attempts[1].WorkEvidence = "unknown"
	settlement = quotaCallSettlement(fact)
	if settlement.Tokens != nil || settlement.Money != nil {
		t.Fatalf("unknown work settled as zero: %+v", settlement)
	}
	fact.Attempts = nil
	fact.NoWork = true
	settlement = quotaCallSettlement(fact)
	if settlement.Tokens == nil || *settlement.Tokens != 0 || settlement.Money == nil || *settlement.Money != "0" {
		t.Fatalf("admitted zero-attempt settlement = %+v", settlement)
	}

	zeroTokens, zeroMoney := int64(10), "12"
	result := &GatewayResult{
		Attempts:        []CallAttempt{{WorkEvidence: "not_sent"}},
		quotaBound:      eventqueue.QuotaBound{Tokens: &zeroTokens, Money: &zeroMoney, Currency: "USD"},
		AttemptID:       "",
		PriceBasis:      fact.PriceBasis,
		RouteStopReason: "no_candidates",
	}
	interrupted := gatewayInterruptedSettlement(result)
	if interrupted.Tokens == nil || *interrupted.Tokens != 0 || interrupted.Money == nil || *interrupted.Money != "0" || interrupted.Currency != "USD" {
		t.Fatalf("interrupted no-work settlement = %+v", interrupted)
	}
	result.AttemptID = "att_active"
	interrupted = gatewayInterruptedSettlement(result)
	if interrupted.Tokens != nil || interrupted.Money != nil {
		t.Fatalf("active attempt received a zero recovery settlement: %+v", interrupted)
	}
}
func TestQuotaBoundedChatRequiresNativeCapAndFiniteShape(t *testing.T) {
	for _, raw := range []string{
		`{"messages":[{"role":"user","content":"text"}],"max_completion_tokens":10,"n":1}`,
		`{"messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}],"max_completion_tokens":1,"response_format":{"type":"json_schema","json_schema":{"schema":{"properties":{"file_id":{"type":"string"}}}}}}`,
	} {
		if !inspectChatQuotaRequest(quotaPayload(t, raw)).Supported {
			t.Fatalf("bounded text rejected %s", raw)
		}
	}
	for _, raw := range []string{
		`{"messages":[],"max_tokens":10}`,
		`{"messages":[],"max_completion_tokens":10,"n":2}`,
		`{"messages":[],"max_completion_tokens":10,"prediction":{"content":"hello"}}`,
		`{"messages":[],"max_completion_tokens":10,"tools":[{"type":"web_search"}]}`,
		`{"messages":[{"content":[{"type":"image_url","image_url":"https://example.com"}]}],"max_completion_tokens":10}`,
		`{"messages":[],"max_completion_tokens":10,"service_tier":"priority"}`,
		`{"messages":[],"max_completion_tokens":10,"unknown_provider_billing_dimension":true}`,
		`{"messages":[],"max_completion_tokens":0}`,
	} {
		if inspectChatQuotaRequest(quotaPayload(t, raw)).Supported {
			t.Fatalf("unsupported bounded request accepted %s", raw)
		}
	}
}
func TestQuotaTokenAndMoneyEvidenceIndependent(t *testing.T) {
	result := &GatewayResult{ProviderModelID: "pmd_one", Protocol: entity.ProtocolOpenAIChat, quotaRequest: quotaRequest{Supported: true, MaxOutput: 10, CacheRead: true, CacheWrite: true}}
	data := &runtimeQuotaData{Bounds: map[string]entity.ReservationBound{"pmd_one": {Protocol: entity.ProtocolOpenAIChat, MaxInputTokens: 100, MaxOutputTokens: 20, ETag: "bnd_proven"}}}
	policies := []eventqueue.QuotaLimit{{Tokens5H: limitNumber(200)}}
	bound, err := prepareQuotaBound(result, policies, data)
	if err != nil || bound.Tokens == nil || *bound.Tokens != 110 || bound.Money != nil {
		t.Fatal("token capacity depends on money price", bound, err)
	}
	policies[0].MoneyMonth = quotaTestString("10")
	policies[0].Currency = "USD"
	if _, err := prepareQuotaBound(result, policies, data); err == nil {
		t.Fatal("unknown price treated as free")
	}
	result.PriceBasis = testPriceBasis()
	bound, err = prepareQuotaBound(result, policies, data)
	if err != nil || bound.Money == nil || bound.BasisDigest == "" || bound.PriceRevision == "" {
		t.Fatal("missing immutable money basis", bound, err)
	}
	delete(data.Bounds, "pmd_one")
	if _, err := prepareQuotaBound(result, policies, data); err == nil {
		t.Fatal("capacity inferred without attestation")
	}
	if bound, err := prepareQuotaBound(result, nil, data); err != nil || bound.Tokens != nil {
		t.Fatal("unconstrained traffic fabricated bound", bound, err)
	}
}

func TestQuotaAttachmentTokenAndMoneyPoliciesRemainIndependent(t *testing.T) {
	result := &GatewayResult{
		ProviderModelID:    "pmd_one",
		Protocol:           entity.ProtocolOpenAIChat,
		PricingUnsupported: true,
		quotaRequest:       quotaRequest{Supported: true, MaxOutput: 10, CacheRead: true, CacheWrite: true},
	}
	data := &runtimeQuotaData{Bounds: map[string]entity.ReservationBound{
		"pmd_one": {
			Protocol:        entity.ProtocolOpenAIChat,
			MaxInputTokens:  100,
			MaxOutputTokens: 20,
			ETag:            "bnd_proven",
		},
	}}
	for name, policy := range map[string]eventqueue.QuotaLimit{
		"tokens": {Tokens5H: limitNumber(200)},
		"tpm":    {TPM: limitNumber(200)},
	} {
		t.Run(name, func(t *testing.T) {
			bound, err := prepareQuotaBound(result, []eventqueue.QuotaLimit{policy}, data)
			if err != nil || bound.Tokens == nil || *bound.Tokens != 110 || bound.Money != nil || bound.Revision != "bnd_proven" {
				t.Fatalf("attachment token capacity was not reserved: %+v, %v", bound, err)
			}
		})
	}
	for name, policy := range map[string]eventqueue.QuotaLimit{
		"money": {
			MoneyMonth: quotaTestString("10"),
			Currency:   "USD",
		},
		"mixed": {
			Tokens5H:   limitNumber(200),
			MoneyMonth: quotaTestString("10"),
			Currency:   "USD",
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := prepareQuotaBound(result, []eventqueue.QuotaLimit{policy}, data)
			var gateway *GatewayError
			if !errors.As(err, &gateway) || gateway.Status != 503 || gateway.Code != "quota_price_unavailable" {
				t.Fatalf("unsupported attachment price did not fail closed: %v", err)
			}
		})
	}
}

func TestQuotaUnsupportedShapePrecedesAttachmentPriceFailure(t *testing.T) {
	result := &GatewayResult{
		ProviderModelID:    "pmd_one",
		Protocol:           entity.ProtocolOpenAIChat,
		PricingUnsupported: true,
		quotaRequest:       quotaRequest{},
	}
	for name, policy := range map[string]eventqueue.QuotaLimit{
		"money": {MoneyMonth: quotaTestString("10"), Currency: "USD"},
		"mixed": {Tokens5H: limitNumber(200), MoneyMonth: quotaTestString("10"), Currency: "USD"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := prepareQuotaBound(result, []eventqueue.QuotaLimit{policy}, &runtimeQuotaData{})
			var gateway *GatewayError
			if !errors.As(err, &gateway) || gateway.Status != 400 || gateway.Code != "quota_request_unsupported" {
				t.Fatalf("unsupported request error = %v", err)
			}
		})
	}
}

func TestQuotaKnownUsageSettlesCanceledRequestsIndependently(t *testing.T) {
	for _, status := range []string{"success", "error", "canceled"} {
		fact := CallFact{Status: status, UsageComplete: true, InputTokens: limitNumber(11), OutputTokens: limitNumber(7)}
		actual := quotaCallSettlement(fact)
		if actual.Tokens == nil || *actual.Tokens != 18 || actual.Money != nil {
			t.Fatal("status replaced evidence", status, actual)
		}
		fact.Pricing = &CallPricing{Status: "priced", Amount: quotaTestString("0"), Currency: quotaTestString("USD")}
		actual = quotaCallSettlement(fact)
		if actual.Money == nil || *actual.Money != "0" {
			t.Fatal("explicit zero lost")
		}
		fact.UsageComplete = false
		fact.Pricing = &CallPricing{Status: "not_final"}
		actual = quotaCallSettlement(fact)
		if actual.Tokens != nil || actual.Money != nil {
			t.Fatal("non-final invented usage")
		}
	}
}
func TestQuotaRuntimePolicyReductionPrecedesAdmission(t *testing.T) {
	svc, data, _ := runtimeFixture(t, "http://127.0.0.1/v1")
	queue, err := eventqueue.Open(filepath.Join(t.TempDir(), "quota.db"), 20, callQueuePayloadLimit)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = queue.Close() }()
	svc.recorder = &callRecorder{queue: queue}
	now := time.Now().UTC()
	if err := queue.EnableQuota("UTC", now.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	data.Quota.Created = map[string]time.Time{"user_usr_one": now, "key_key_one": now}
	data.Quota.Bounds = map[string]entity.ReservationBound{"pmd_one": {Protocol: entity.ProtocolOpenAIChat, MaxInputTokens: 10, MaxOutputTokens: 10, ETag: "bound_one"}}
	data.Limits = []entity.ResourceLimit{{ScopeKind: "user", ScopeID: "usr_one", ETag: "policy_one", Tokens5H: limitNumber(20)}}
	if err := compileRuntimeLimits(data); err != nil {
		t.Fatal(err)
	}
	svc.runtime.auth.Store(buildRuntimeAuthorization(data, now.Add(time.Minute)))
	result := func() *GatewayResult {
		return &GatewayResult{UserID: "usr_one", KeyID: "key_one", ModelID: "mdl_one", ModelName: "public-model", ProviderModelID: "pmd_one", quotaRequest: quotaRequest{Supported: true, MaxOutput: 10}}
	}
	if err := svc.admitLimitedGatewayCall(context.Background(), "first", result()); err != nil {
		t.Fatal(err)
	}
	if err := svc.admitLimitedGatewayCall(context.Background(), "second", result()); err == nil {
		t.Fatal("active hold overshot quota")
	}
	usage, err := queue.AccountQuotaUsage("user_usr_one", now)
	if err != nil || usage.Active.TokensHeld != 20 {
		t.Fatal(usage, err)
	}
	svc.denyLimitScope("user", "usr_one")
	if err := svc.admitLimitedGatewayCall(context.Background(), "denied", result()); !errors.Is(err, runtimeUnavailable) {
		t.Fatal("unpublished policy reused", err)
	}
	effective := effectiveQuotaValues(limits.Policy{Tokens5H: limitNumber(10)}, limits.Policy{Tokens5H: limitNumber(3)})
	if *effective.Tokens5H != 3 {
		t.Fatal("effective parent ignored")
	}
}
