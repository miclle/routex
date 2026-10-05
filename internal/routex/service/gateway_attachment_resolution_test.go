package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"database/sql/driver"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/eventqueue"
	"github.com/miclle/routex/pkg/pricing"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestGatewayProjectAttachmentQuotaAdmissionAndDispatch(t *testing.T) {
	for _, test := range gatewayAttachmentQuotaCases() {
		t.Run(test.name, func(t *testing.T) {
			var upstreamCalls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				upstreamCalls.Add(1)
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{}`)
			}))
			defer upstream.Close()

			svc, data, projectBearer, _ := projectRuntimeFixture(t)
			data.Connections[0].BaseURL = upstream.URL + "/v1"
			reads := configureGatewayAttachmentStorageScope(t, svc, entity.StorageOwnerProject, "prj_one")
			configureGatewayAttachmentQuota(t, svc, data, test.protocol, entity.ResourceLimit{ScopeKind: "project", ScopeID: "prj_one", ETag: "policy_project_attachment", Tokens5H: limitNumber(500)})

			requestID := "req_project_attachment_" + strings.ReplaceAll(test.name, " ", "_")
			result, err := test.invoke(svc, projectBearer, []byte(test.body), requestID)
			if err != nil {
				t.Fatal(err)
			}
			if err := result.Response.Body.Close(); err != nil {
				t.Fatal(err)
			}
			if upstreamCalls.Load() != 1 {
				t.Fatalf("upstream dispatches = %d, want 1", upstreamCalls.Load())
			}
			if reads(testAttachmentImageID) != 1 || reads(testAttachmentPDFID) != 1 {
				t.Fatalf("storage reads = image %d, PDF %d, want one per unique Project object", reads(testAttachmentImageID), reads(testAttachmentPDFID))
			}
			if result.ImageInputs != 2 || result.PDFInputs != 1 || result.PricingUnsupported {
				t.Fatalf("Project media pricing facts = image:%d PDF:%d unsupported:%t", result.ImageInputs, result.PDFInputs, result.PricingUnsupported)
			}
			receipt, err := svc.recorder.queue.QuotaReceipt(requestID)
			if err != nil || receipt.Bound.Tokens == nil || *receipt.Bound.Tokens != 110 {
				t.Fatalf("Project reservation = %+v, error = %v, want one 110-token admission", receipt, err)
			}
		})
	}
}

func TestGatewayProjectAttachmentRejectsPersonalObjectBeforeStorage(t *testing.T) {
	test := gatewayAttachmentQuotaCases()[0]
	var upstreamCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { upstreamCalls.Add(1) }))
	defer upstream.Close()
	svc, data, projectBearer, _ := projectRuntimeFixture(t)
	data.Connections[0].BaseURL = upstream.URL + "/v1"
	reads := configureGatewayAttachmentStorage(t, svc)
	configureGatewayAttachmentQuota(t, svc, data, test.protocol, entity.ResourceLimit{ScopeKind: "project", ScopeID: "prj_one", ETag: "policy_project_attachment", Tokens5H: limitNumber(500)})

	_, err := test.invoke(svc, projectBearer, []byte(test.body), "req_project_foreign_attachment")
	assertGatewayResolutionError(t, err, http.StatusNotFound, "attachment_not_found")
	if reads(testAttachmentImageID) != 0 || reads(testAttachmentPDFID) != 0 {
		t.Fatal("cross-scope rejection read attachment storage")
	}
	if upstreamCalls.Load() != 0 {
		t.Fatal("cross-scope rejection reached upstream")
	}
}

func TestGatewayProjectAttachmentMoneyPolicyRejectsBeforeStorage(t *testing.T) {
	test := gatewayAttachmentQuotaCases()[0]
	var upstreamCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { upstreamCalls.Add(1) }))
	defer upstream.Close()
	svc, data, projectBearer, _ := projectRuntimeFixture(t)
	data.Connections[0].BaseURL = upstream.URL + "/v1"
	reads := configureGatewayAttachmentStorageScope(t, svc, entity.StorageOwnerProject, "prj_one")
	configureGatewayAttachmentQuota(t, svc, data, test.protocol, entity.ResourceLimit{ScopeKind: "project", ScopeID: "prj_one", ETag: "policy_project_money", MoneyMonth: quotaTestString("20"), Currency: "USD"})

	_, err := test.invoke(svc, projectBearer, []byte(test.body), "req_project_attachment_money")
	assertGatewayResolutionError(t, err, http.StatusServiceUnavailable, "quota_price_unavailable")
	if reads(testAttachmentImageID) != 0 || reads(testAttachmentPDFID) != 0 {
		t.Fatal("Project monetary preflight rejection read attachment storage")
	}
	if upstreamCalls.Load() != 0 {
		t.Fatal("Project monetary preflight rejection reached upstream")
	}
}

func TestGatewayProjectAttachmentRechecksProjectAfterStorage(t *testing.T) {
	test := gatewayAttachmentQuotaCases()[0]
	var upstreamCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { upstreamCalls.Add(1) }))
	defer upstream.Close()
	svc, data, projectBearer, _ := projectRuntimeFixture(t)
	data.Connections[0].BaseURL = upstream.URL + "/v1"
	reads := configureGatewayAttachmentStorageScopeStatuses(t, svc, entity.StorageOwnerProject, "prj_one", []string{entity.ResourceActive, entity.ResourceDisabled})
	configureGatewayAttachmentQuota(t, svc, data, test.protocol, entity.ResourceLimit{ScopeKind: "project", ScopeID: "prj_one", ETag: "policy_project_lifecycle", Tokens5H: limitNumber(500)})

	_, err := test.invoke(svc, projectBearer, []byte(test.body), "req_project_attachment_disabled_during_read")
	assertGatewayResolutionError(t, err, http.StatusNotFound, "attachment_not_found")
	if reads(testAttachmentImageID) != 1 || reads(testAttachmentPDFID) != 0 {
		t.Fatalf("storage reads = image %d, PDF %d, want one read before Project lifecycle recheck", reads(testAttachmentImageID), reads(testAttachmentPDFID))
	}
	if upstreamCalls.Load() != 0 {
		t.Fatal("Project disabled during attachment read reached upstream")
	}
}

func TestGatewayAttachmentRequiresSelectedRouteCapability(t *testing.T) {
	var upstreamCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		upstreamCalls.Add(1)
	}))
	defer upstream.Close()
	svc, _, bearer := runtimeFixture(t, upstream.URL+"/v1")
	_, err := svc.GatewayChat(context.Background(), bearer, attachmentChatBody(testAttachmentImageID), "req_attachment_capability")
	assertGatewayResolutionError(t, err, http.StatusBadRequest, "attachment_type_unsupported")
	if upstreamCalls.Load() != 0 {
		t.Fatal("capability rejection reached upstream")
	}
}

func TestGatewayAttachmentQuotaAdmissionAndDispatch(t *testing.T) {
	for _, test := range gatewayAttachmentQuotaCases() {
		t.Run(test.name, func(t *testing.T) {
			for _, policy := range []struct {
				name  string
				limit entity.ResourceLimit
			}{
				{name: "token window", limit: entity.ResourceLimit{Tokens5H: limitNumber(500)}},
				{name: "TPM", limit: entity.ResourceLimit{TPM: limitNumber(500)}},
			} {
				t.Run(policy.name, func(t *testing.T) {
					assertGatewayAttachmentQuotaShape(t, test.protocol, test.body)
					var upstreamCalls atomic.Int32
					upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
						upstreamCalls.Add(1)
						w.Header().Set("Content-Type", "application/json")
						_, _ = io.WriteString(w, `{}`)
					}))
					defer upstream.Close()

					svc, data, bearer := runtimeFixture(t, upstream.URL+"/v1")
					reads := configureGatewayAttachmentStorage(t, svc)
					policy.limit.ScopeKind = "user"
					policy.limit.ScopeID = "usr_one"
					policy.limit.ETag = "policy_attachment"
					configureGatewayAttachmentQuota(t, svc, data, test.protocol, policy.limit)

					requestID := "req_attachment_" + strings.ReplaceAll(test.name+"_"+policy.name, " ", "_")
					result, err := test.invoke(svc, bearer, []byte(test.body), requestID)
					if err != nil {
						t.Fatal(err)
					}
					if err := result.Response.Body.Close(); err != nil {
						t.Fatal(err)
					}
					if upstreamCalls.Load() != 1 {
						t.Fatalf("upstream dispatches = %d, want 1", upstreamCalls.Load())
					}
					if reads(testAttachmentImageID) != 1 || reads(testAttachmentPDFID) != 1 {
						t.Fatalf("storage reads = image %d, PDF %d, want one per unique object", reads(testAttachmentImageID), reads(testAttachmentPDFID))
					}
					if result.ImageInputs != 2 || result.PDFInputs != 1 || result.PricingUnsupported {
						t.Fatalf("media pricing facts = image:%d PDF:%d unsupported:%t", result.ImageInputs, result.PDFInputs, result.PricingUnsupported)
					}
					receipt, err := svc.recorder.queue.QuotaReceipt(requestID)
					if err != nil || receipt.Bound.Tokens == nil || *receipt.Bound.Tokens != 110 {
						t.Fatalf("reservation = %+v, error = %v, want 110 tokens", receipt, err)
					}
				})
			}
		})
	}
}

func assertGatewayAttachmentQuotaShape(t *testing.T, protocol, body string) {
	t.Helper()
	payload := attachmentTestPayload(t, body)
	plan, err := planGatewayAttachments(protocol, payload)
	if err != nil {
		t.Fatal(err)
	}
	request := inspectQuotaRequest(protocol, payload, plan)
	if !request.Supported || request.MaxOutput != 10 {
		detached, detachedOK := quotaAttachmentPayload(protocol, plan)
		t.Fatalf("attachment quota shape = %+v, detached = %v (%t), occurrences = %+v", request, detached, detachedOK, plan.Occurrences)
	}
}

func TestGatewayAttachmentMoneyPolicyRejectsBeforeStorage(t *testing.T) {
	for _, test := range gatewayAttachmentQuotaCases() {
		t.Run(test.name, func(t *testing.T) {
			for _, mixed := range []bool{false, true} {
				name := "money only"
				if mixed {
					name = "token and money"
				}
				t.Run(name, func(t *testing.T) {
					var upstreamCalls atomic.Int32
					upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
						upstreamCalls.Add(1)
					}))
					defer upstream.Close()
					svc, data, bearer := runtimeFixture(t, upstream.URL+"/v1")
					reads := configureGatewayAttachmentStorage(t, svc)
					limit := entity.ResourceLimit{ScopeKind: "user", ScopeID: "usr_one", ETag: "policy_attachment_money", MoneyMonth: quotaTestString("20"), Currency: "USD"}
					if mixed {
						limit.Tokens5H = limitNumber(500)
					}
					configureGatewayAttachmentQuota(t, svc, data, test.protocol, limit)

					_, err := test.invoke(svc, bearer, []byte(test.body), "req_attachment_money")
					assertGatewayResolutionError(t, err, http.StatusServiceUnavailable, "quota_price_unavailable")
					if upstreamCalls.Load() != 0 {
						t.Fatal("monetary preflight rejection reached upstream")
					}
					if reads(testAttachmentImageID) != 0 || reads(testAttachmentPDFID) != 0 {
						t.Fatal("monetary preflight rejection read attachment storage")
					}
				})
			}
		})
	}
}

func TestGatewayAttachmentMoneyPricingAdmission(t *testing.T) {
	for _, test := range gatewayAttachmentQuotaCases() {
		t.Run(test.name, func(t *testing.T) {
			var upstreamCalls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				upstreamCalls.Add(1)
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{}`)
			}))
			defer upstream.Close()
			svc, data, bearer := runtimeFixture(t, upstream.URL+"/v1")
			reads := configureGatewayAttachmentStorage(t, svc)
			runtimePriceFixture(data)
			data.Pricing.Rates = append(data.Pricing.Rates,
				entity.PriceRate{ID: "ptr_image", ModelPriceID: "prc_one", Metric: pricing.ImageInput, Tier: pricing.Base, Unit: pricing.ImageUnit, Currency: "USD", Amount: "0", Enabled: true},
				entity.PriceRate{ID: "ptr_pdf", ModelPriceID: "prc_one", Metric: pricing.PDFInput, Tier: pricing.Base, Unit: pricing.PDFUnit, Currency: "USD", Amount: "0", Enabled: true},
			)
			configureGatewayAttachmentQuota(t, svc, data, test.protocol, entity.ResourceLimit{ScopeKind: "user", ScopeID: "usr_one", ETag: "policy_attachment_money", MoneyMonth: quotaTestString("20"), Currency: "CNY"})

			requestID := "req_attachment_money_" + strings.ReplaceAll(test.name, " ", "_")
			result, err := test.invoke(svc, bearer, []byte(test.body), requestID)
			if err != nil {
				t.Fatal(err)
			}
			if err := result.Response.Body.Close(); err != nil {
				t.Fatal(err)
			}
			if result.ImageInputs != 2 || result.PDFInputs != 1 || result.PriceBasis == nil || result.PriceBasis.Adapter != pricing.MultimodalAdapter {
				t.Fatalf("media pricing result = %+v", result)
			}
			if upstreamCalls.Load() != 1 || reads(testAttachmentImageID) != 1 || reads(testAttachmentPDFID) != 1 {
				t.Fatalf("dispatch/read counts = upstream:%d image:%d PDF:%d", upstreamCalls.Load(), reads(testAttachmentImageID), reads(testAttachmentPDFID))
			}
			receipt, err := svc.recorder.queue.QuotaReceipt(requestID)
			if err != nil || receipt.Bound.Money == nil || receipt.Bound.Currency != "CNY" || receipt.Bound.PriceRevision == "" || receipt.Bound.BasisDigest == "" {
				t.Fatalf("media money reservation = %+v, error = %v", receipt, err)
			}
		})
	}
}

func TestGatewayAttachmentMoneyPricingRequiresEveryMediaRateBeforeStorage(t *testing.T) {
	test := gatewayAttachmentQuotaCases()[0]
	for _, disabled := range []bool{false, true} {
		name := "missing PDF rate"
		if disabled {
			name = "disabled PDF rate"
		}
		t.Run(name, func(t *testing.T) {
			var upstreamCalls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				upstreamCalls.Add(1)
			}))
			defer upstream.Close()
			svc, data, bearer := runtimeFixture(t, upstream.URL+"/v1")
			reads := configureGatewayAttachmentStorage(t, svc)
			runtimePriceFixture(data)
			data.Pricing.Rates = append(data.Pricing.Rates,
				entity.PriceRate{ID: "ptr_image", ModelPriceID: "prc_one", Metric: pricing.ImageInput, Tier: pricing.Base, Unit: pricing.ImageUnit, Currency: "USD", Amount: "0", Enabled: true},
			)
			if disabled {
				data.Pricing.Rates = append(data.Pricing.Rates,
					entity.PriceRate{ID: "ptr_pdf", ModelPriceID: "prc_one", Metric: pricing.PDFInput, Tier: pricing.Base, Unit: pricing.PDFUnit, Currency: "USD", Amount: "0", Enabled: false},
				)
			}
			configureGatewayAttachmentQuota(t, svc, data, test.protocol, entity.ResourceLimit{ScopeKind: "user", ScopeID: "usr_one", ETag: "policy_attachment_money", MoneyMonth: quotaTestString("20"), Currency: "CNY"})

			_, err := test.invoke(svc, bearer, []byte(test.body), "req_attachment_missing_media_rate")
			assertGatewayResolutionError(t, err, http.StatusServiceUnavailable, "quota_price_unavailable")
			if upstreamCalls.Load() != 0 || reads(testAttachmentImageID) != 0 || reads(testAttachmentPDFID) != 0 {
				t.Fatalf("missing media rate reached side effects: upstream:%d image:%d PDF:%d", upstreamCalls.Load(), reads(testAttachmentImageID), reads(testAttachmentPDFID))
			}
		})
	}
}

func TestGatewayAttachmentDurablePreflightRejectsBeforeStorage(t *testing.T) {
	test := gatewayAttachmentQuotaCases()[0]
	for _, admission := range []struct {
		name  string
		limit entity.ResourceLimit
		seed  bool
		code  string
	}{
		{
			name:  "request exceeds token policy",
			limit: entity.ResourceLimit{Tokens5H: limitNumber(100)},
			code:  "quota_exceeded",
		},
		{
			name:  "existing token hold exhausts policy",
			limit: entity.ResourceLimit{Tokens5H: limitNumber(150)},
			seed:  true,
			code:  "quota_exceeded",
		},
		{
			name:  "RPM exhausted",
			limit: entity.ResourceLimit{RPM: limitNumber(1)},
			seed:  true,
			code:  "rate_limit_exceeded",
		},
		{
			name:  "concurrency exhausted",
			limit: entity.ResourceLimit{Concurrency: limitNumber(1)},
			seed:  true,
			code:  "concurrency_limit_exceeded",
		},
	} {
		t.Run(admission.name, func(t *testing.T) {
			var upstreamCalls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				upstreamCalls.Add(1)
			}))
			defer upstream.Close()
			svc, data, bearer := runtimeFixture(t, upstream.URL+"/v1")
			reads := configureGatewayAttachmentStorage(t, svc)
			admission.limit.ScopeKind = "user"
			admission.limit.ScopeID = "usr_one"
			admission.limit.ETag = "policy_preflight"
			configureGatewayAttachmentQuota(t, svc, data, test.protocol, admission.limit)
			if admission.seed {
				seedGatewayAttachmentQuotaHold(t, svc, test.protocol)
			}
			beforeDepth, err := svc.recorder.queue.Depth()
			if err != nil {
				t.Fatal(err)
			}

			_, err = test.invoke(svc, bearer, []byte(test.body), "req_attachment_preflight")
			assertGatewayResolutionError(t, err, http.StatusTooManyRequests, admission.code)
			if reads(testAttachmentImageID) != 0 || reads(testAttachmentPDFID) != 0 {
				t.Fatal("rejected durable preflight read attachment storage")
			}
			if upstreamCalls.Load() != 0 {
				t.Fatal("rejected durable preflight reached upstream")
			}
			afterDepth, err := svc.recorder.queue.Depth()
			if err != nil || afterDepth != beforeDepth {
				t.Fatalf("rejected durable preflight created a fact: before=%d after=%d err=%v", beforeDepth, afterDepth, err)
			}
		})
	}
}

func TestGatewayAttachmentDurablePreflightRequiresJournalBeforeStorage(t *testing.T) {
	test := gatewayAttachmentQuotaCases()[0]
	var upstreamCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		upstreamCalls.Add(1)
	}))
	defer upstream.Close()
	svc, data, bearer := runtimeFixture(t, upstream.URL+"/v1")
	data.Connections[0].Protocol = test.protocol
	data.ProviderModels[0].SupportsImageInput = true
	data.ProviderModels[0].SupportsPDFInput = true
	data.Limits = []entity.ResourceLimit{{ScopeKind: "user", ScopeID: "usr_one", ETag: "policy_preflight", Tokens5H: limitNumber(500)}}
	data.Quota.Bounds = map[string]entity.ReservationBound{
		"pmd_one": {ProviderModelID: "pmd_one", Protocol: test.protocol, MaxInputTokens: 100, MaxOutputTokens: 20, ETag: "bound_attachment"},
	}
	data.Quota.Created = map[string]time.Time{"user_usr_one": time.Now().UTC(), "key_key_one": time.Now().UTC()}
	if err := compileRuntimeLimits(data); err != nil {
		t.Fatal(err)
	}
	svc.runtime.auth.Store(buildRuntimeAuthorization(data, time.Now().Add(time.Minute)))
	routes, err := svc.buildRuntimeRoutes(data)
	if err != nil {
		t.Fatal(err)
	}
	svc.runtime.routes.Store(&runtimeRoutes{ID: "cfg_attachment", Models: routes, PublishedAt: time.Now()})

	_, err = test.invoke(svc, bearer, []byte(test.body), "req_attachment_no_journal")
	assertGatewayResolutionError(t, err, http.StatusServiceUnavailable, "event_buffer_unavailable")
	if upstreamCalls.Load() != 0 {
		t.Fatal("missing durable journal reached upstream")
	}
}

func TestGatewayAttachmentUnrecognizedInlineMediaRemainsUnsupported(t *testing.T) {
	tests := []struct {
		name, protocol, body string
		invoke               gatewayAttachmentInvoker
	}{
		{name: "OpenAI Chat", protocol: entity.ProtocolOpenAIChat, body: `{"model":"public-model","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://example.invalid/image.png"}}]}],"max_completion_tokens":10}`, invoke: invokeGatewayChat},
		{name: "OpenAI Responses", protocol: entity.ProtocolOpenAIResponses, body: `{"model":"public-model","input":[{"role":"user","content":[{"type":"input_image","image_url":"https://example.invalid/image.png"}]}],"max_output_tokens":10}`, invoke: invokeGatewayResponses},
		{name: "Anthropic Messages", protocol: entity.ProtocolAnthropicMessages, body: `{"model":"public-model","messages":[{"role":"user","content":[{"type":"image","source":{"type":"url","url":"https://example.invalid/image.png"}}]}],"max_tokens":10}`, invoke: invokeGatewayMessages},
		{name: "Gemini", protocol: entity.ProtocolGeminiGenerateContent, body: `{"contents":[{"role":"user","parts":[{"inlineData":{"mimeType":"image/png","data":"aW1hZ2U="}}]}],"generationConfig":{"maxOutputTokens":10}}`, invoke: invokeGatewayGemini},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var upstreamCalls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				upstreamCalls.Add(1)
			}))
			defer upstream.Close()
			svc, data, bearer := runtimeFixture(t, upstream.URL+"/v1")
			configureGatewayAttachmentQuota(t, svc, data, test.protocol, entity.ResourceLimit{ScopeKind: "user", ScopeID: "usr_one", ETag: "policy_inline", Tokens5H: limitNumber(500)})
			_, err := test.invoke(svc, bearer, []byte(test.body), "req_unrecognized_inline")
			assertGatewayResolutionError(t, err, http.StatusBadRequest, "quota_request_unsupported")
			if upstreamCalls.Load() != 0 {
				t.Fatal("unsupported inline media reached upstream")
			}
		})
	}
}

func attachmentChatBody(objectID string) []byte {
	return []byte(`{"model":"public-model","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"routex://attachments/` + objectID + `"}}]}]}`)
}

func assertGatewayResolutionError(t *testing.T, err error, status int, code string) {
	t.Helper()
	var gateway *GatewayError
	if !errors.As(err, &gateway) || gateway.Status != status || gateway.Code != code {
		t.Fatalf("gateway error = %#v, want status %d code %s", err, status, code)
	}
}

type gatewayAttachmentInvoker func(*Service, string, []byte, string) (*GatewayResult, error)

type gatewayAttachmentQuotaCase struct {
	name, protocol, body string
	invoke               gatewayAttachmentInvoker
}

func gatewayAttachmentQuotaCases() []gatewayAttachmentQuotaCase {
	image := gatewayAttachmentReferencePrefix + testAttachmentImageID
	pdf := gatewayAttachmentReferencePrefix + testAttachmentPDFID
	return []gatewayAttachmentQuotaCase{
		{
			name:     "OpenAI Chat",
			protocol: entity.ProtocolOpenAIChat,
			body:     `{"model":"public-model","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"` + image + `"}},{"type":"image_url","image_url":{"url":"` + image + `"}},{"type":"file","file":{"file_data":"` + pdf + `"}}]}],"max_completion_tokens":10}`,
			invoke:   invokeGatewayChat,
		},
		{
			name:     "OpenAI Responses",
			protocol: entity.ProtocolOpenAIResponses,
			body:     `{"model":"public-model","input":[{"role":"user","content":[{"type":"input_image","image_url":"` + image + `"},{"type":"input_image","image_url":"` + image + `"},{"type":"input_file","file_data":"` + pdf + `"}]}],"max_output_tokens":10}`,
			invoke:   invokeGatewayResponses,
		},
		{
			name:     "Anthropic Messages",
			protocol: entity.ProtocolAnthropicMessages,
			body:     `{"model":"public-model","messages":[{"role":"user","content":[{"type":"image","source":{"type":"base64","media_type":"image/png","data":"` + image + `"}},{"type":"image","source":{"type":"base64","media_type":"image/png","data":"` + image + `"}},{"type":"document","source":{"type":"base64","media_type":"application/pdf","data":"` + pdf + `"}}]}],"max_tokens":10}`,
			invoke:   invokeGatewayMessages,
		},
		{
			name:     "Gemini",
			protocol: entity.ProtocolGeminiGenerateContent,
			body:     `{"contents":[{"role":"user","parts":[{"inlineData":{"mimeType":"image/png","data":"` + image + `"}},{"inlineData":{"mimeType":"image/png","data":"` + image + `"}},{"inlineData":{"mimeType":"application/pdf","data":"` + pdf + `"}}]}],"generationConfig":{"maxOutputTokens":10}}`,
			invoke:   invokeGatewayGemini,
		},
	}
}

func invokeGatewayChat(svc *Service, bearer string, body []byte, requestID string) (*GatewayResult, error) {
	return svc.GatewayChat(context.Background(), bearer, body, requestID)
}

func invokeGatewayResponses(svc *Service, bearer string, body []byte, requestID string) (*GatewayResult, error) {
	return svc.GatewayResponses(context.Background(), bearer, body, requestID)
}

func invokeGatewayMessages(svc *Service, bearer string, body []byte, requestID string) (*GatewayResult, error) {
	return svc.GatewayMessages(context.Background(), bearer, body, requestID, MessagesHeaders{Version: "2023-06-01"})
}

func invokeGatewayGemini(svc *Service, bearer string, body []byte, requestID string) (*GatewayResult, error) {
	return svc.GatewayGemini(context.Background(), bearer, body, requestID, "public-model", false)
}

func seedGatewayAttachmentQuotaHold(t *testing.T, svc *Service, protocol string) {
	t.Helper()
	result := &GatewayResult{
		Protocol:           protocol,
		UserID:             "usr_one",
		KeyID:              "key_one",
		ModelID:            "mdl_one",
		ModelName:          "public-model",
		ProviderModelID:    "pmd_one",
		PricingUnsupported: true,
		quotaRequest:       quotaRequest{Supported: true, MaxOutput: 10, CacheRead: true, CacheWrite: protocol != entity.ProtocolGeminiGenerateContent},
	}
	limits, err := svc.gatewayLimits(context.Background(), result)
	if err != nil {
		t.Fatal(err)
	}
	policies, data, err := svc.gatewayQuotaPolicies(context.Background(), result, limits)
	if err != nil {
		t.Fatal(err)
	}
	bound, err := prepareQuotaBound(result, policies, data)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.recorder.queue.ReserveWithQuota("held_attachment", []byte("fallback"), policies, bound, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
}

func configureGatewayAttachmentQuota(t *testing.T, svc *Service, data *runtimeData, protocol string, limit entity.ResourceLimit) {
	t.Helper()
	now := time.Now().UTC()
	data.Connections[0].Protocol = protocol
	data.ProviderModels[0].SupportsImageInput = true
	data.ProviderModels[0].SupportsPDFInput = true
	data.Limits = []entity.ResourceLimit{limit}
	data.Quota.Bounds = map[string]entity.ReservationBound{
		"pmd_one": {ProviderModelID: "pmd_one", Protocol: protocol, MaxInputTokens: 100, MaxOutputTokens: 20, ETag: "bound_attachment"},
	}
	data.Quota.Created = map[string]time.Time{"user_usr_one": now, "project_prj_one": now, "key_key_one": now, "key_pky_one": now}
	if err := compileRuntimeLimits(data); err != nil {
		t.Fatal(err)
	}
	svc.runtime.auth.Store(buildRuntimeAuthorization(data, now.Add(time.Minute)))
	routes, err := svc.buildRuntimeRoutes(data)
	if err != nil {
		t.Fatal(err)
	}
	svc.runtime.routes.Store(&runtimeRoutes{ID: "cfg_attachment", Models: routes, PublishedAt: now})

	queue, err := eventqueue.Open(filepath.Join(t.TempDir(), "attachment-quota.db"), 20, callQueuePayloadLimit)
	if err != nil {
		t.Fatal(err)
	}
	if err := queue.EnableQuota("UTC", now.Add(-time.Minute)); err != nil {
		_ = queue.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = queue.Close() })
	svc.recorder = &callRecorder{queue: queue}
}

type attachmentStorageObject struct {
	row  entity.StorageObject
	data []byte
}

type attachmentStorageQueryFixture struct {
	objects        map[string]attachmentStorageObject
	revision       entity.StorageRevision
	userCreatedAt  time.Time
	projectStatus  []string
	projectQueries int
}

func configureGatewayAttachmentStorage(t *testing.T, svc *Service) func(string) int {
	return configureGatewayAttachmentStorageScope(t, svc, entity.StorageOwnerUser, "usr_one")
}

func configureGatewayAttachmentStorageScope(t *testing.T, svc *Service, ownerKind, ownerID string) func(string) int {
	return configureGatewayAttachmentStorageScopeStatuses(t, svc, ownerKind, ownerID, nil)
}

func configureGatewayAttachmentStorageScopeStatuses(t *testing.T, svc *Service, ownerKind, ownerID string, projectStatus []string) func(string) int {
	t.Helper()
	var mu sync.Mutex
	reads := map[string]int{}
	objects := map[string]attachmentStorageObject{}
	for _, input := range []struct {
		id, name, mime string
		data           []byte
	}{
		{id: testAttachmentImageID, name: "image.png", mime: "image/png", data: []byte("image")},
		{id: testAttachmentPDFID, name: "document.pdf", mime: "application/pdf", data: []byte("pdf")},
	} {
		digest := sha256.Sum256(input.data)
		objects[input.id] = attachmentStorageObject{
			row: entity.StorageObject{
				ID:            input.id,
				OwnerKind:     ownerKind,
				OwnerID:       ownerID,
				RevisionID:    "str_attachment",
				Purpose:       "attachment",
				State:         "ready",
				Name:          input.name,
				MIME:          input.mime,
				Size:          int64(len(input.data)),
				SHA256:        hex.EncodeToString(digest[:]),
				VersionID:     "version-" + input.id,
				NextCleanupAt: time.Now().Add(time.Hour),
				CreatedAt:     time.Now(),
			},
			data: input.data,
		}
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var object attachmentStorageObject
		found := false
		for objectID, candidate := range objects {
			if strings.HasSuffix(r.URL.Path, "/routex/"+objectID) {
				object = candidate
				found = true
				mu.Lock()
				reads[objectID]++
				mu.Unlock()
				break
			}
		}
		if !found || r.Method != http.MethodGet || !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 Credential=test-only-access/") {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.Header().Set("X-Amz-Version-Id", object.row.VersionID)
		w.Header().Set("X-Amz-Meta-Routex-Id", object.row.ID)
		_, _ = w.Write(object.data)
	}))
	t.Cleanup(server.Close)

	plaintext := `{"AccessKey":"test-only-access","SecretKey":"test-only-secret"}`
	ciphertext, err := svc.secrets.Seal("storage:str_attachment:generation-one", plaintext)
	if err != nil {
		t.Fatal(err)
	}
	revision := entity.StorageRevision{ID: "str_attachment", Endpoint: server.URL, Region: "us-east-1", Bucket: "routex-test", Prefix: "", SecretGeneration: "generation-one", AuthCiphertext: ciphertext}
	auth := svc.runtime.auth.Load()
	if auth == nil || auth.UserProofs["usr_one"].CreatedAt.IsZero() {
		t.Fatal("attachment fixture requires the recorded user birth")
	}
	fixture := &attachmentStorageQueryFixture{objects: objects, revision: revision, userCreatedAt: auth.UserProofs["usr_one"].CreatedAt, projectStatus: projectStatus}
	sqlDB := sql.OpenDB(attachmentStorageConnector{fixture: fixture})
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB, PreferSimpleProtocol: true, WithoutReturning: true}), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	svc.db = db
	svc.allowPrivateStorage = true
	return func(objectID string) int {
		mu.Lock()
		defer mu.Unlock()
		return reads[objectID]
	}
}

type attachmentStorageConnector struct {
	fixture *attachmentStorageQueryFixture
}

func (connector attachmentStorageConnector) Connect(context.Context) (driver.Conn, error) {
	return &attachmentStorageConnection{fixture: connector.fixture}, nil
}

func (connector attachmentStorageConnector) Driver() driver.Driver {
	return attachmentStorageDriver(connector)
}

type attachmentStorageDriver struct {
	fixture *attachmentStorageQueryFixture
}

func (database attachmentStorageDriver) Open(string) (driver.Conn, error) {
	return &attachmentStorageConnection{fixture: database.fixture}, nil
}

type attachmentStorageConnection struct {
	fixture *attachmentStorageQueryFixture
}

func (*attachmentStorageConnection) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepared statements are not supported")
}

func (*attachmentStorageConnection) Close() error { return nil }

func (*attachmentStorageConnection) Begin() (driver.Tx, error) {
	return nil, errors.New("transactions are not supported")
}

func (connection *attachmentStorageConnection) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	switch {
	case strings.Contains(query, `FROM "users"`):
		return &attachmentStorageRows{
			columns: []string{"id", "disabled", "created_at", "offboarded_at", "approval_application_id"},
			values:  [][]driver.Value{{"usr_one", false, connection.fixture.userCreatedAt, nil, nil}},
		}, nil
	case strings.Contains(query, `FROM "projects"`):
		now := time.Now()
		status := entity.ResourceActive
		if connection.fixture.projectQueries < len(connection.fixture.projectStatus) {
			status = connection.fixture.projectStatus[connection.fixture.projectQueries]
		}
		connection.fixture.projectQueries++
		if status != entity.ResourceActive && attachmentFixtureHasArgument(args, entity.ResourceActive) {
			return &attachmentStorageRows{columns: []string{"id"}}, nil
		}
		return &attachmentStorageRows{
			columns: []string{"id", "name", "description", "status", "creator_id", "created_at", "updated_at"},
			values:  [][]driver.Value{{"prj_one", "Project", "", status, "usr_creator", now, now}},
		}, nil
	case strings.Contains(query, `FROM project_managers m`):
		return &attachmentStorageRows{columns: []string{"count"}, values: [][]driver.Value{{int64(1)}}}, nil
	case strings.Contains(query, `FROM "storage_objects"`):
		var objectID string
		for _, arg := range args {
			value, ok := arg.Value.(string)
			if ok && strings.HasPrefix(value, "obj_") {
				objectID = value
				break
			}
		}
		object, ok := connection.fixture.objects[objectID]
		if ok && (!attachmentFixtureHasArgument(args, object.row.OwnerKind) || !attachmentFixtureHasArgument(args, object.row.OwnerID)) {
			ok = false
		}
		if !ok {
			return &attachmentStorageRows{columns: []string{"id"}}, nil
		}
		row := object.row
		return &attachmentStorageRows{
			columns: []string{"id", "owner_kind", "owner_id", "revision_id", "purpose", "state", "name", "mime", "size", "sha256", "version_id", "next_cleanup_at", "created_at"},
			values:  [][]driver.Value{{row.ID, row.OwnerKind, row.OwnerID, row.RevisionID, row.Purpose, row.State, row.Name, row.MIME, row.Size, row.SHA256, row.VersionID, row.NextCleanupAt, row.CreatedAt}},
		}, nil
	case strings.Contains(query, `FROM "storage_revisions"`):
		row := connection.fixture.revision
		return &attachmentStorageRows{
			columns: []string{"id", "endpoint", "region", "bucket", "prefix", "secret_generation", "auth_ciphertext"},
			values:  [][]driver.Value{{row.ID, row.Endpoint, row.Region, row.Bucket, row.Prefix, row.SecretGeneration, row.AuthCiphertext}},
		}, nil
	default:
		return nil, fmt.Errorf("unexpected attachment fixture query: %s", query)
	}
}

func attachmentFixtureHasArgument(args []driver.NamedValue, expected string) bool {
	for _, arg := range args {
		if value, ok := arg.Value.(string); ok && value == expected {
			return true
		}
	}
	return false
}

type attachmentStorageRows struct {
	columns []string
	values  [][]driver.Value
	index   int
}

func (rows *attachmentStorageRows) Columns() []string { return rows.columns }
func (*attachmentStorageRows) Close() error           { return nil }
func (rows *attachmentStorageRows) Next(values []driver.Value) error {
	if rows.index >= len(rows.values) {
		return io.EOF
	}
	copy(values, rows.values[rows.index])
	rows.index++
	return nil
}

var _ driver.QueryerContext = (*attachmentStorageConnection)(nil)
var _ driver.Connector = attachmentStorageConnector{}
