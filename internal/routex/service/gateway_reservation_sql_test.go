package service

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/eventqueue"
	"github.com/miclle/routex/pkg/pricing"
	"github.com/miclle/routex/pkg/routeattempt"
	"gorm.io/gorm"
)

func gatewayReservationSQLFixture(t *testing.T) (*GatewayReservationManifest, *gatewayReservationSQLFacts) {
	t.Helper()
	svc, plan, result, quota, payload := reservationManifestFixture(t)
	// Real SQL pricing has one setting revision and distinct stored row IDs.
	for i := range plan.candidates {
		basis := plan.candidates[i].route.PriceBasis
		basis.ETag = "pricing_capture"
		basis.Schedule.PriceID = "prc_" + plan.candidates[i].route.ProviderModelID
		for j := range basis.Schedule.Rates {
			basis.Schedule.Rates[j].ID = basis.Schedule.PriceID + "_" + basis.Schedule.Rates[j].ID
		}
		slices.SortFunc(basis.Schedule.Rates, func(a, b pricing.Rate) int { return compareReservationSQLID(a.ID, b.ID) })
	}
	current, bound, err := svc.prepareGatewayAttemptQuotaBound(t.Context(), plan, result, result.admissionQuota, quota)
	if err != nil {
		t.Fatal(err)
	}
	result.quotaBound = bound
	result.attemptEvidence = gatewayAttemptEvidence(current, result, result.admissionQuota, quota)
	m, err := captureGatewayReservationManifest("req_sql_capture", current, result, quota, payload)
	if err != nil {
		t.Fatal(err)
	}
	f := &gatewayReservationSQLFacts{model: entity.Model{ID: m.Model.ID, CreatedAt: m.Model.Birth, Status: entity.ResourceActive}, providers: map[string]entity.Provider{}, connections: map[string]entity.ProviderConnection{}, credentials: map[string]entity.ProviderCredential{}, models: map[string]entity.ProviderModel{}, bindings: map[string]entity.ModelProviderBinding{}, coverage: map[[2]string]string{}, access: map[[2]string]bool{}, quota: quota, pricing: &runtimePricingData{}, egress: &runtimeData{}}
	for _, candidate := range current.candidates {
		r := candidate.route
		f.providers[r.ProviderID] = entity.Provider{ID: r.ProviderID, CreatedAt: r.ProviderBirth, Enabled: r.ProviderEnabled, ETag: r.ProviderRevision}
		f.connections[r.ConnectionID] = entity.ProviderConnection{ID: r.ConnectionID, CreatedAt: r.ConnectionBirth, ProviderID: r.ProviderID, Enabled: r.ConnectionEnabled, ETag: r.ConnectionRevision, TransportGeneration: r.ConnectionTransportGeneration, Adapter: r.Adapter, APIVersion: r.APIVersion, BaseURL: r.BaseURL, Protocol: r.Protocol}
		f.credentials[r.CredentialID] = entity.ProviderCredential{ID: r.CredentialID, CreatedAt: r.CredentialBirth, ConnectionID: r.ConnectionID, Priority: candidate.priority, Enabled: true, VerificationStatus: "verified", VerifiedTransportGeneration: r.ConnectionTransportGeneration, CoverageRevision: candidate.reservationCredential.CoverageRevision, StorageSource: "inline"}
		f.models[r.ProviderModelID] = entity.ProviderModel{ID: r.ProviderModelID, CreatedAt: r.ProviderModelBirth, ConnectionID: r.ConnectionID, UpstreamName: r.UpstreamName, ETag: r.ProviderModelRevision, Disabled: r.Disabled, SupportsImageInput: r.SupportsImageInput, SupportsPDFInput: r.SupportsPDFInput, CapabilityTransportGeneration: r.CapabilityTransportGeneration}
		f.bindings[r.BindingID] = entity.ModelProviderBinding{ID: r.BindingID, CreatedAt: r.BindingBirth, ModelID: r.ModelID, ProviderModelID: r.ProviderModelID, Weight: r.Weight}
		f.access[[2]string{r.CredentialID, r.ProviderModelID}] = true
		basis := r.PriceBasis
		f.pricing.Setting = entity.PricingSetting{ID: 1, ETag: basis.ETag, PlatformCurrency: basis.Currency.PlatformCurrency}
		f.pricing.Prices = append(f.pricing.Prices, entity.ModelPrice{ID: basis.Schedule.PriceID, ProviderModelID: r.ProviderModelID, ContextThreshold: basis.Schedule.ContextThreshold})
		for _, rate := range basis.Schedule.Rates {
			f.pricing.Rates = append(f.pricing.Rates, entity.PriceRate{ID: rate.ID, ModelPriceID: basis.Schedule.PriceID, Metric: rate.Metric, Tier: rate.Tier, Unit: rate.Unit, Currency: rate.Currency, Amount: rate.Amount, Enabled: rate.Enabled})
		}
	}
	for currency, rate := range m.Candidates[0].PriceBasis.Currency.Rates {
		f.pricing.FX = append(f.pricing.FX, entity.PricingExchangeRate{Currency: currency, Rate: rate})
	}
	slices.SortFunc(f.pricing.Rates, func(a, b entity.PriceRate) int { return compareReservationSQLID(a.ID, b.ID) })
	return m, f
}

func TestGatewayReservationSQLDetachedReconstructionParity(t *testing.T) {
	m, f := gatewayReservationSQLFixture(t)
	all, err := gatewayReservationSQLSelection(m, nil)
	if err != nil || len(all) != 2 {
		t.Fatal("complete original capture", err)
	}
	for _, captured := range all {
		got, err := gatewayReservationSQLCurrent(m, captured, f)
		if err != nil || !reflect.DeepEqual(got, captured) {
			t.Fatal("current detached reconstruction changed capture", err)
		}
	}
}

func TestGatewayReservationSQLSelectedCandidateKeepsOriginalMaximum(t *testing.T) {
	m, f := gatewayReservationSQLFixture(t)
	before, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	c := m.Candidates[0]
	a := routeattempt.Attempt{Number: 2, TargetID: c.Binding.ID, ConnectionID: c.Connection.ID, CredentialID: c.Credential.Identity.ID, Protocol: m.Protocol}
	selected, err := gatewayReservationSQLSelection(m, &a)
	if err != nil || len(selected) != 1 {
		t.Fatal("original fallback selection", err)
	}
	other := m.Candidates[1]
	row := f.providers[other.Provider.ID]
	row.Enabled = false
	f.providers[row.ID] = row
	got, err := gatewayReservationSQLCurrent(m, selected[0], f)
	if err != nil || !reflect.DeepEqual(got, selected[0]) {
		t.Fatal("unselected current change affected selected read", err)
	}
	if _, err := gatewayReservationSQLCurrent(m, other, f); err == nil {
		t.Fatal("full Accept would admit disabled original route")
	}
	if m.MaximumHold.Tokens == nil || *m.MaximumHold.Tokens != 210 || c.BoundEvidence.InputTokens+c.BoundEvidence.OutputTokens >= *m.MaximumHold.Tokens {
		t.Fatal("fixture does not distinguish selected hold from original maximum")
	}
	after, err := json.Marshal(m)
	if err != nil || !slices.Equal(before, after) {
		t.Fatal("selected validation rewrote original capture/hold")
	}
	a.CredentialID = "crd_new"
	if _, err := gatewayReservationSQLSelection(m, &a); err == nil {
		t.Fatal("new current candidate introduced")
	}
}

func TestGatewayReservationSQLUnconstrainedUnknownStaysAbsent(t *testing.T) {
	m, f := gatewayReservationSQLFixture(t)
	m.MaximumHold = eventqueue.QuotaBound{}
	for i := range m.Candidates {
		c := &m.Candidates[i]
		c.Capacity, c.PriceBasis = nil, nil
		c.BoundEvidence = gatewayAttemptBoundEvidence{TargetID: c.Binding.ID, ProviderModel: c.ProviderModel.ID}
	}
	selected, err := gatewayReservationSQLSelection(m, nil)
	if err != nil {
		t.Fatal("unconstrained original capture", err)
	}
	for _, captured := range selected {
		current, err := gatewayReservationSQLCurrent(m, captured, f)
		if err != nil || !reflect.DeepEqual(current, captured) || current.Capacity != nil || current.PriceBasis != nil {
			t.Fatal("unconstrained absence became zero/current capacity proof", err)
		}
	}
}

func TestGatewayReservationSQLRejectsCapturedCorruption(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*GatewayReservationManifest)
	}{
		{"missing", func(m *GatewayReservationManifest) { m.Version = 0 }},
		{"unknown_birth", func(m *GatewayReservationManifest) { m.Candidates[0].Binding.Birth = time.Time{} }},
		{"reduced_maximum", func(m *GatewayReservationManifest) { *m.MaximumHold.Tokens = 110 }},
		{"changed_other_candidate", func(m *GatewayReservationManifest) { m.Candidates[1].BoundEvidence.InputTokens++ }},
		{"duplicate_pair", func(m *GatewayReservationManifest) { m.Candidates[1] = m.Candidates[0] }},
		{"unreviewed_currency", func(m *GatewayReservationManifest) { m.Candidates[0].PriceBasis.Currency.Rates["EUR"] = "9" }},
		{"private_dimension", func(m *GatewayReservationManifest) { m.Native.PricingDimensions = []string{"private-question"} }},
		{"wrong_native_shape", func(m *GatewayReservationManifest) { m.Native.InputContainer = "input" }},
		{"unsupported_header", func(m *GatewayReservationManifest) { v := true; m.Native.MessagesHeadersSupported = &v }},
		{"missing_capacity", func(m *GatewayReservationManifest) { m.Candidates[0].Capacity = nil }},
		{"extra_price_basis", func(m *GatewayReservationManifest) { m.MaximumHold.Money = nil }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, _ := gatewayReservationSQLFixture(t)
			tc.change(m)
			c := m.Candidates[0]
			selected := routeattempt.Attempt{Number: 1, TargetID: c.Binding.ID, ConnectionID: c.Connection.ID, CredentialID: c.Credential.Identity.ID, Protocol: m.Protocol}
			if _, err := gatewayReservationSQLSelection(m, &selected); err == nil {
				t.Fatal("corrupted full capture admitted by selected-only validation")
			}
		})
	}
	if _, err := gatewayReservationSQLSelection(nil, nil); err == nil {
		t.Fatal("missing capture admitted")
	}
}

func TestGatewayReservationSQLRejectsCurrentConstituentChanges(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*GatewayReservationManifest, *gatewayReservationSQLFacts)
	}{
		{"provider_birth", func(m *GatewayReservationManifest, f *gatewayReservationSQLFacts) {
			id := m.Candidates[0].Provider.ID
			r := f.providers[id]
			r.CreatedAt = r.CreatedAt.Add(time.Second)
			f.providers[id] = r
		}},
		{"connection_parent", func(m *GatewayReservationManifest, f *gatewayReservationSQLFacts) {
			id := m.Candidates[0].Connection.ID
			r := f.connections[id]
			r.ProviderID = "prv_other"
			f.connections[id] = r
		}},
		{"connection_revision", func(m *GatewayReservationManifest, f *gatewayReservationSQLFacts) {
			id := m.Candidates[0].Connection.ID
			r := f.connections[id]
			r.ETag = "changed"
			f.connections[id] = r
		}},
		{"endpoint", func(m *GatewayReservationManifest, f *gatewayReservationSQLFacts) {
			id := m.Candidates[0].Connection.ID
			r := f.connections[id]
			r.BaseURL = "https://new.example/v1"
			f.connections[id] = r
		}},
		{"binding_birth", func(m *GatewayReservationManifest, f *gatewayReservationSQLFacts) {
			id := m.Candidates[0].Binding.ID
			r := f.bindings[id]
			r.CreatedAt = r.CreatedAt.Add(time.Second)
			f.bindings[id] = r
		}},
		{"binding_weight", func(m *GatewayReservationManifest, f *gatewayReservationSQLFacts) {
			id := m.Candidates[0].Binding.ID
			r := f.bindings[id]
			r.Weight = 0
			f.bindings[id] = r
		}},
		{"credential_birth", func(m *GatewayReservationManifest, f *gatewayReservationSQLFacts) {
			id := m.Candidates[0].Credential.Identity.ID
			r := f.credentials[id]
			r.CreatedAt = r.CreatedAt.Add(time.Second)
			f.credentials[id] = r
		}},
		{"credential_toggle", func(m *GatewayReservationManifest, f *gatewayReservationSQLFacts) {
			id := m.Candidates[0].Credential.Identity.ID
			r := f.credentials[id]
			r.Enabled = false
			f.credentials[id] = r
		}},
		{"stale_verification", func(m *GatewayReservationManifest, f *gatewayReservationSQLFacts) {
			id := m.Candidates[0].Credential.Identity.ID
			r := f.credentials[id]
			r.VerifiedTransportGeneration = "bad"
			f.credentials[id] = r
		}},
		{"coverage_removed", func(m *GatewayReservationManifest, f *gatewayReservationSQLFacts) {
			c := m.Candidates[0]
			delete(f.access, [2]string{c.Credential.Identity.ID, c.ProviderModel.ID})
		}},
		{"capacity_revision", func(m *GatewayReservationManifest, f *gatewayReservationSQLFacts) {
			id := m.Candidates[0].ProviderModel.ID
			r := f.quota.Bounds[id]
			r.ETag = "changed"
			f.quota.Bounds[id] = r
		}},
		{"unused_fx", func(_ *GatewayReservationManifest, f *gatewayReservationSQLFacts) {
			for i := range f.pricing.FX {
				if f.pricing.FX[i].Currency == "EUR" {
					f.pricing.FX[i].Rate = "99"
				}
			}
		}},
		{"price_revision", func(_ *GatewayReservationManifest, f *gatewayReservationSQLFacts) { f.pricing.Setting.ETag = "changed" }},
		{"capability", func(m *GatewayReservationManifest, f *gatewayReservationSQLFacts) {
			id := m.Candidates[0].ProviderModel.ID
			r := f.models[id]
			r.SupportsImageInput = !r.SupportsImageInput
			f.models[id] = r
		}},
		{"upstream_name", func(m *GatewayReservationManifest, f *gatewayReservationSQLFacts) {
			id := m.Candidates[0].ProviderModel.ID
			r := f.models[id]
			r.UpstreamName = "new-name"
			f.models[id] = r
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, f := gatewayReservationSQLFixture(t)
			tc.change(m, f)
			current, err := gatewayReservationSQLCurrent(m, m.Candidates[0], f)
			if err == nil && reflect.DeepEqual(current, m.Candidates[0]) {
				t.Fatal("changed current constituent admitted")
			}
		})
	}
}

func TestGatewayReservationSQLNativeClosedProtocolParity(t *testing.T) {
	for _, tc := range []struct {
		protocol, body string
		options        []gatewayNativeOptions
	}{
		{entity.ProtocolOpenAIChat, `{"messages":[{"role":"user","content":"private"}],"max_completion_tokens":12}`, nil},
		{entity.ProtocolOpenAIResponses, `{"input":"private","max_output_tokens":12}`, nil},
		{entity.ProtocolAnthropicMessages, `{"messages":[{"role":"user","content":"private"}],"max_tokens":12}`, []gatewayNativeOptions{{Messages: MessagesHeaders{Version: "2023-06-01"}}}},
		{entity.ProtocolGeminiGenerateContent, `{"contents":[{"parts":[{"text":"private"}]}],"generationConfig":{"maxOutputTokens":12}}`, nil},
	} {
		t.Run(tc.protocol, func(t *testing.T) {
			n, _, err := gatewayReservationNativeShape(tc.protocol, attachmentTestPayload(t, tc.body), tc.options...)
			if err != nil || !gatewayReservationSQLNativeValid(tc.protocol, n) {
				t.Fatal("existing parser capture rejected", err)
			}
		})
	}
}

func TestGatewayReservationSQLRequiresTransactionAndBounds(t *testing.T) {
	m, _ := gatewayReservationSQLFixture(t)
	if validateGatewayReservationAcceptSQL(nil, m) == nil || validateGatewayReservationAcceptSQL(&gorm.DB{}, m) == nil {
		t.Fatal("transactionless validation admitted")
	}
	if validateGatewayReservationDispatchSQL(nil, m, routeattempt.Attempt{Number: 5}) == nil {
		t.Fatal("attempt beyond original budget admitted")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	r := &gatewayReservationSQLReader{tx: &gorm.DB{Statement: &gorm.Statement{Context: ctx}}}
	if !errors.Is(r.query(), context.Canceled) {
		t.Fatal("cancelled loader started work")
	}
	r = &gatewayReservationSQLReader{tx: &gorm.DB{Statement: &gorm.Statement{Context: t.Context()}}}
	for range gatewayReservationSQLMaxQueries {
		if err := r.query(); err != nil {
			t.Fatal(err)
		}
	}
	if r.query() == nil {
		t.Fatal("query budget overflow admitted")
	}
	if err := r.count(gatewayReservationSQLMaxRows); err != nil {
		t.Fatal(err)
	}
	if r.count(1) == nil {
		t.Fatal("row budget overflow admitted")
	}
}
