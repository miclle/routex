package service

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/eventqueue"
	"github.com/miclle/routex/pkg/pricing"
	"github.com/miclle/routex/pkg/routeattempt"
)

func reservationManifestFixture(t *testing.T) (*Service, *gatewayAttemptPlan, *GatewayResult, *runtimeQuotaData, map[string]json.RawMessage) {
	t.Helper()
	svc, data := gatewayAttemptFixture(t)
	for index := range data.Bindings {
		data.Bindings[index].CreatedAt = time.Date(2026, 10, 2, 0, 0, index, 0, time.UTC)
	}
	routes, err := svc.buildRuntimeRoutes(data)
	if err != nil {
		t.Fatal(err)
	}
	for index := range routes["mdl_one"] {
		route := &routes["mdl_one"][index].Route
		basis := testPriceBasis()
		basis.Currency.Rates["EUR"] = "8.25" // Preserve unused FX constituents too.
		basis.ETag = "price_" + route.ProviderModelID
		basis.Schedule.ProviderModelID = route.ProviderModelID
		if index == 1 {
			basis.Schedule.Rates[0].Amount = "9"
		}
		route.PriceBasis = basis
	}
	svc.runtime.routes.Store(&runtimeRoutes{ID: "cfg_01arz3ndektsv4rrffq69g5fav", Models: routes})
	data.Quota.Bounds = map[string]entity.ReservationBound{}
	for index, pm := range data.ProviderModels {
		data.Quota.Bounds[pm.ID] = entity.ReservationBound{ProviderModelID: pm.ID, Protocol: entity.ProtocolOpenAIChat, TransportGeneration: "0", MaxInputTokens: int64(100 * (index + 1)), MaxOutputTokens: 50, ETag: "cap_" + pm.ID}
	}
	data.Quota.Currency = "CNY"
	payload := attachmentTestPayload(t, `{"model":"public-model","messages":[{"role":"user","content":"private-prompt-do-not-retain"}],"max_completion_tokens":10}`)
	plan, err := svc.gatewayAttemptPlan("mdl_one", entity.ProtocolOpenAIChat)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(plan.releaseSources)
	result := &GatewayResult{ModelID: "mdl_one", Protocol: entity.ProtocolOpenAIChat, quotaRequest: inspectQuotaRequest(entity.ProtocolOpenAIChat, payload)}
	result.admissionQuota = []eventqueue.QuotaLimit{{Tokens5H: limitNumber(10000), MoneyMonth: quotaTestString("100"), Currency: "CNY"}}
	plan, bound, err := svc.prepareGatewayAttemptQuotaBound(context.Background(), plan, result, result.admissionQuota, data.Quota)
	if err != nil {
		t.Fatal(err)
	}
	result.quotaBound = bound
	result.attemptEvidence = gatewayAttemptEvidence(plan, result, result.admissionQuota, data.Quota)
	result.admissionAllowed = cloneGatewayAttemptAllowed(plan.allowed)
	return svc, plan, result, data.Quota, payload
}

func TestGatewayReservationManifestCapturesHydratedBirthsAndOriginalMaximum(t *testing.T) {
	_, plan, result, data, payload := reservationManifestFixture(t)
	manifest, err := captureGatewayReservationManifest("req_capture", plan, result, data, payload)
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Candidates) != 2 || manifest.MaximumAttempts != 4 || manifest.MaximumHold.Tokens == nil || *manifest.MaximumHold.Tokens != 210 || !reflect.DeepEqual(manifest.MaximumHold, result.quotaBound) {
		t.Fatalf("original reservation changed: %+v", manifest)
	}
	for index, candidate := range manifest.Candidates {
		original := plan.candidates[index]
		if !candidate.Binding.Birth.Equal(original.route.BindingBirth) || !candidate.Credential.Identity.Birth.Equal(original.route.CredentialBirth) || candidate.Credential.Identity.Birth.IsZero() || candidate.Binding.Birth.IsZero() || !manifest.Model.Birth.Equal(original.route.ModelBirth) {
			t.Fatal("actual hydrated birth lost")
		}
		if candidate.Capacity.MaximumInput != data.Bounds[candidate.ProviderModel.ID].MaxInputTokens || candidate.Capacity.MaximumOutput != 50 || candidate.PriceBasis.Currency.Rates["EUR"] != "8.25" {
			t.Fatal("complete capacity/FX constituents lost")
		}
	}
	quote, err := pricing.ReserveBound(manifest.Candidates[1].PriceBasis.Schedule, manifest.Candidates[1].PriceBasis.Currency, pricing.Capacity{Input: 200, Output: 10, CacheRead: true, CacheWrite: true})
	if err != nil || quote.Amount != *manifest.MaximumHold.Money {
		t.Fatalf("existing conservative builder parity: %v", err)
	}
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"private-prompt-do-not-retain", "test-upstream-secret", "provider-one.example", "provider-two.example", "provider-model", "ciphertext", "vault", "source_key", "plaintext"} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("private value retained: %s", forbidden)
		}
	}
}

func TestGatewayReservationManifestIsDetachedAndCanonical(t *testing.T) {
	_, plan, result, data, payload := reservationManifestFixture(t)
	first, err := captureGatewayReservationManifest("req_capture", plan, result, data, payload)
	if err != nil {
		t.Fatal(err)
	}
	before, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	reversed := *plan
	reversed.candidates = slices.Clone(plan.candidates)
	slices.Reverse(reversed.candidates)
	second, err := captureGatewayReservationManifest("req_capture", &reversed, result, data, payload)
	if err != nil {
		t.Fatal(err)
	}
	after, err := json.Marshal(second)
	if err != nil || !slices.Equal(before, after) {
		t.Fatal("original candidate order changed canonical capture")
	}
	*result.quotaBound.Tokens = 1
	*result.quotaBound.Money = "0"
	plan.candidates[0].route.PriceBasis.Currency.Rates["EUR"] = "999"
	plan.candidates[0].route.PriceBasis.Schedule.Rates[0].Amount = "999"
	payload["messages"] = json.RawMessage(`[]`)
	still, err := json.Marshal(first)
	if err != nil || !slices.Equal(before, still) {
		t.Fatal("capture aliases mutable source data")
	}
}

func TestGatewayReservationManifestRejectsChangedCapturedEvidence(t *testing.T) {
	cases := []struct {
		name   string
		change func(*gatewayAttemptPlan, *GatewayResult, *runtimeQuotaData, map[string]json.RawMessage)
	}{
		{"unknown_binding_birth", func(p *gatewayAttemptPlan, _ *GatewayResult, _ *runtimeQuotaData, _ map[string]json.RawMessage) {
			p.candidates[0].route.BindingBirth = time.Time{}
		}},
		{"unknown_credential_birth", func(p *gatewayAttemptPlan, _ *GatewayResult, _ *runtimeQuotaData, _ map[string]json.RawMessage) {
			p.candidates[0].route.CredentialBirth = time.Time{}
		}},
		{"mixed_model_birth", func(p *gatewayAttemptPlan, _ *GatewayResult, _ *runtimeQuotaData, _ map[string]json.RawMessage) {
			p.candidates[0].route.ModelBirth = p.candidates[0].route.ModelBirth.Add(time.Second)
		}},
		{"wrong_model_parent", func(p *gatewayAttemptPlan, _ *GatewayResult, _ *runtimeQuotaData, _ map[string]json.RawMessage) {
			p.candidates[0].route.ModelID = "mdl_other"
		}},
		{"wrong_connection", func(p *gatewayAttemptPlan, _ *GatewayResult, _ *runtimeQuotaData, _ map[string]json.RawMessage) {
			p.candidates[0].attempt.ConnectionID = "con_other"
		}},
		{"snapshot_changed", func(p *gatewayAttemptPlan, _ *GatewayResult, _ *runtimeQuotaData, _ map[string]json.RawMessage) {
			p.candidates[0].route.SnapshotID = "cfg_other"
		}},
		{"drop_candidate", func(p *gatewayAttemptPlan, _ *GatewayResult, _ *runtimeQuotaData, _ map[string]json.RawMessage) {
			p.candidates = p.candidates[:1]
		}},
		{"extra_allowed_pair", func(_ *gatewayAttemptPlan, r *GatewayResult, _ *runtimeQuotaData, _ map[string]json.RawMessage) {
			r.admissionAllowed["bnd_extra\x00crd_extra"] = true
		}},
		{"duplicate_pair", func(p *gatewayAttemptPlan, _ *GatewayResult, _ *runtimeQuotaData, _ map[string]json.RawMessage) {
			p.candidates[1] = p.candidates[0]
		}},
		{"reduced_hold", func(_ *gatewayAttemptPlan, r *GatewayResult, _ *runtimeQuotaData, _ map[string]json.RawMessage) {
			*r.quotaBound.Tokens = 110
		}},
		{"reduced_money_hold", func(_ *gatewayAttemptPlan, r *GatewayResult, _ *runtimeQuotaData, _ map[string]json.RawMessage) {
			*r.quotaBound.Money = "0"
		}},
		{"original_output_changed", func(_ *gatewayAttemptPlan, _ *GatewayResult, _ *runtimeQuotaData, p map[string]json.RawMessage) {
			p["max_completion_tokens"] = json.RawMessage(`20`)
		}},
		{"capacity_changed", func(_ *gatewayAttemptPlan, _ *GatewayResult, d *runtimeQuotaData, _ map[string]json.RawMessage) {
			c := d.Bounds["pmd_one"]
			c.MaxInputTokens++
			d.Bounds["pmd_one"] = c
		}},
		{"stale_capacity_transport", func(_ *gatewayAttemptPlan, _ *GatewayResult, d *runtimeQuotaData, _ map[string]json.RawMessage) {
			c := d.Bounds["pmd_one"]
			c.TransportGeneration = "rev_01arz3ndektsv4rrffq69g5fav"
			d.Bounds["pmd_one"] = c
		}},
		{"price_changed", func(p *gatewayAttemptPlan, _ *GatewayResult, _ *runtimeQuotaData, _ map[string]json.RawMessage) {
			p.candidates[0].route.PriceBasis.Schedule.Rates[0].Amount = "99"
		}},
		{"unused_fx_changed", func(p *gatewayAttemptPlan, _ *GatewayResult, _ *runtimeQuotaData, _ map[string]json.RawMessage) {
			p.candidates[0].route.PriceBasis.Currency.Rates["EUR"] = "9"
		}},
		{"credential_disabled", func(p *gatewayAttemptPlan, _ *GatewayResult, _ *runtimeQuotaData, _ map[string]json.RawMessage) {
			p.candidates[0].reservationCredential.Enabled = false
		}},
		{"stale_verification", func(p *gatewayAttemptPlan, _ *GatewayResult, _ *runtimeQuotaData, _ map[string]json.RawMessage) {
			p.candidates[0].reservationCredential.VerifiedTransportGeneration = "rev_01arz3ndektsv4rrffq69g5fav"
		}},
		{"media_count_override", func(_ *gatewayAttemptPlan, r *GatewayResult, _ *runtimeQuotaData, _ map[string]json.RawMessage) {
			r.ImageInputs = 1
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, plan, result, data, payload := reservationManifestFixture(t)
			tc.change(plan, result, data, payload)
			if _, err := captureGatewayReservationManifest("req_capture", plan, result, data, payload); err == nil {
				t.Fatal("inconsistent capture admitted")
			}
		})
	}
}

func TestGatewayReservationNativeShapeUsesExistingProtocolClassifiers(t *testing.T) {
	for _, tc := range []struct {
		protocol, body, container, shape string
		cacheWrite                       bool
		options                          []gatewayNativeOptions
	}{
		{entity.ProtocolOpenAIChat, `{"messages":[{"role":"user","content":"secret"}],"max_completion_tokens":11}`, "messages", "array", true, nil},
		{entity.ProtocolOpenAIResponses, `{"input":"secret","max_output_tokens":11}`, "input", "string", true, nil},
		{entity.ProtocolAnthropicMessages, `{"messages":[{"role":"user","content":"secret"}],"max_tokens":11}`, "messages", "array", true, []gatewayNativeOptions{{Messages: MessagesHeaders{Version: "2023-06-01"}}}},
		{entity.ProtocolGeminiGenerateContent, `{"contents":[{"role":"user","parts":[{"text":"secret"}]}],"generationConfig":{"maxOutputTokens":11}}`, "contents", "array", false, nil},
	} {
		t.Run(tc.protocol, func(t *testing.T) {
			payload := attachmentTestPayload(t, tc.body)
			shape, request, err := gatewayReservationNativeShape(tc.protocol, payload, tc.options...)
			if err != nil || !request.Supported || !shape.Supported || shape.MaximumOutput != 11 || shape.InputContainer != tc.container || shape.InputShape != tc.shape || !shape.CacheRead || shape.CacheWrite != tc.cacheWrite || len(shape.Media) != 0 {
				t.Fatalf("native shape: %+v, %+v, %v", shape, request, err)
			}
		})
	}
	payload := attachmentTestPayload(t, `{"messages":[{"role":"user","content":"secret"}],"max_tokens":11}`)
	shape, request, err := gatewayReservationNativeShape(entity.ProtocolAnthropicMessages, payload, gatewayNativeOptions{Messages: MessagesHeaders{Version: "2023-06-01", Beta: "private-header-value"}})
	if err != nil || request.Supported || shape.MessagesHeadersSupported == nil || *shape.MessagesHeadersSupported {
		t.Fatal("unbounded native header condition hidden")
	}
	raw, err := json.Marshal(shape)
	if err != nil || strings.Contains(string(raw), "private-header-value") || strings.Contains(string(raw), "secret") {
		t.Fatal("native data retained")
	}
}

func TestGatewayReservationNativeMediaCountsOccurrencesWithoutObjectEvidence(t *testing.T) {
	body := `{"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"routex://attachments/` + testAttachmentImageID + `"}},{"type":"image_url","image_url":{"url":"routex://attachments/` + testAttachmentImageID + `"}},{"type":"file","file":{"file_data":"routex://attachments/` + testAttachmentPDFID + `","filename":"private.pdf"}}]}],"max_completion_tokens":11}`
	shape, request, err := gatewayReservationNativeShape(entity.ProtocolOpenAIChat, attachmentTestPayload(t, body))
	if err != nil || !request.Supported || !reflect.DeepEqual(shape.Media, []GatewayReservationMedia{{"image", "data_url"}, {"image", "data_url"}, {"pdf", "openai_file"}}) {
		t.Fatalf("forwarded occurrence shape: %+v, %v", shape, err)
	}
	raw, err := json.Marshal(shape)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{testAttachmentImageID, testAttachmentPDFID, "private.pdf", "routex://", "file_data", "messages/"} {
		if strings.Contains(string(raw), value) {
			t.Fatalf("attachment data retained: %s", value)
		}
	}
}

func TestGatewayReservationManifestPreservesUnknownUnconstrainedBound(t *testing.T) {
	_, plan, result, data, payload := reservationManifestFixture(t)
	result.admissionQuota = nil
	result.quotaBound = eventqueue.QuotaBound{}
	result.attemptEvidence = gatewayAttemptEvidence(plan, result, nil, data)
	manifest, err := captureGatewayReservationManifest("req_capture", plan, result, data, payload)
	if err != nil || manifest.MaximumHold.Tokens != nil || manifest.MaximumHold.Money != nil || manifest.Candidates[0].Capacity != nil || manifest.Candidates[0].PriceBasis != nil {
		t.Fatalf("unknown became zero/proven: %+v, %v", manifest, err)
	}
}

func TestGatewayReservationManifestRetainsAzureCoverageIdentityWithoutSourceData(t *testing.T) {
	_, plan, result, data, payload := reservationManifestFixture(t)
	candidate := &plan.candidates[0]
	candidate.route.Adapter = entity.AdapterAzureOpenAIClassic
	candidate.route.BaseURL = "https://test-resource.openai.azure.com"
	version := "2024-10-21"
	candidate.route.APIVersion = &version
	if _, err := captureGatewayReservationManifest("req_capture", plan, result, data, payload); err == nil {
		t.Fatal("Azure missing attestation captured")
	}
	candidate.reservationCredential.DeploymentIdentity = strings.Repeat("a", 64)
	manifest, err := captureGatewayReservationManifest("req_capture", plan, result, data, payload)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Candidates[0].Credential.DeploymentIdentity != strings.Repeat("a", 64) {
		t.Fatal("exact retained attestation identity lost")
	}
	version = "changed"
	if *manifest.Candidates[0].APIVersion != "2024-10-21" {
		t.Fatal("API version pointer aliases source")
	}
}

func TestGatewayReservationManifestEnforcesExistingPlanBounds(t *testing.T) {
	for _, tc := range []struct {
		targets, credentials int
		accepted             bool
	}{{128, 1, true}, {129, 1, false}, {1, 32, true}, {1, 33, false}} {
		t.Run(fmt.Sprintf("%d_targets_%d_credentials", tc.targets, tc.credentials), func(t *testing.T) {
			_, plan, result, data, payload := reservationManifestFixture(t)
			base := plan.candidates[0]
			plan.candidates, plan.allowed = nil, map[string]bool{}
			result.admissionQuota, result.quotaBound = nil, eventqueue.QuotaBound{}
			result.admissionAllowed, result.attemptEvidence = map[string]bool{}, map[string]gatewayAttemptBoundEvidence{}
			for target := 0; target < tc.targets; target++ {
				for credential := 0; credential < tc.credentials; credential++ {
					c := base
					c.route.BindingID, c.route.CredentialID = fmt.Sprintf("bnd_%d", target), fmt.Sprintf("crd_%d", credential)
					c.attempt = routeattempt.Attempt{TargetID: c.route.BindingID, ConnectionID: c.route.ConnectionID, CredentialID: c.route.CredentialID, Protocol: plan.protocol}
					c.reservationCredential.Identity.ID = c.route.CredentialID
					key := gatewayAttemptKey(c.attempt)
					plan.candidates = append(plan.candidates, c)
					plan.allowed[key], result.admissionAllowed[key] = true, true
					result.attemptEvidence[key] = gatewayAttemptBoundEvidence{TargetID: c.route.BindingID, ProviderModel: c.route.ProviderModelID}
				}
			}
			manifest, err := captureGatewayReservationManifest("req_capture", plan, result, data, payload)
			if (err == nil) != tc.accepted {
				t.Fatalf("capture accepted=%v, want %v: %v", err == nil, tc.accepted, err)
			}
			if tc.accepted && len(manifest.Candidates) != tc.targets*tc.credentials {
				t.Fatal("bounded complete plan truncated")
			}
		})
	}
}

func TestGatewayReservationManifestRejectsWholeOversizedBasis(t *testing.T) {
	svc, plan, result, data, payload := reservationManifestFixture(t)
	for index := range plan.candidates {
		basis := clonePriceBasis(plan.candidates[index].route.PriceBasis)
		// The arithmetic validator deliberately does not authorize catalogue IDs.
		// Oversized captured metadata must still fail the complete-basis byte cap.
		basis.Schedule.PriceID = strings.Repeat("p", callPricingSnapshotLimit)
		plan.candidates[index].route.PriceBasis = basis
	}
	current, bound, err := svc.prepareGatewayAttemptQuotaBound(context.Background(), plan, result, result.admissionQuota, data)
	if err != nil {
		t.Fatal(err)
	}
	result.quotaBound = bound
	result.attemptEvidence = gatewayAttemptEvidence(current, result, result.admissionQuota, data)
	originalBasis := clonePriceBasis(current.candidates[0].route.PriceBasis)
	if _, err := captureGatewayReservationManifest("req_capture", current, result, data, payload); err == nil {
		t.Fatal("oversized full basis captured")
	}
	if len(current.candidates) != 2 || !reflect.DeepEqual(current.candidates[0].route.PriceBasis, originalBasis) || pricingBasisSize(originalBasis) <= callPricingSnapshotLimit {
		t.Fatal("oversized rejection truncated original candidate/basis")
	}
}

func TestGatewayReservationNativeShapeKeepsUnsupportedConditionsUnknown(t *testing.T) {
	payload := attachmentTestPayload(t, `{"messages":[{"role":"user","content":"private"}],"max_completion_tokens":12,"service_tier":"private-tier-value","web_search_options":{"query":"private-question"}}`)
	shape, request, err := gatewayReservationNativeShape(entity.ProtocolOpenAIChat, payload)
	if err != nil || request.Supported || !slices.Contains(shape.PricingDimensions, "request_service_tier") || !slices.Contains(shape.PricingDimensions, "external_tool") {
		t.Fatalf("unsupported conditions hidden: %+v, %v", shape, err)
	}
	raw, err := json.Marshal(shape)
	if err != nil || strings.Contains(string(raw), "private") {
		t.Fatal("arbitrary condition value retained")
	}
}
