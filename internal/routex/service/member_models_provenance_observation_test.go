package service

import (
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/pricing"
)

// A fixed published authorization must reject changed grant provenance even
// though periodic publication can later legitimately publish that new basis.
func TestMemberModelsGrantProvenanceRestorationAcrossPublisherObservations(t *testing.T) {
	svc, data, published := memberModelsProjectionFixture(t)
	source := "mar_recorded_original"
	published.Grants[0].CreatedAt = time.Date(2026, 10, 1, 12, 0, 0, 123456000, time.UTC)
	published.Grants[0].SourceRequestID = &source
	data.Grants = slices.Clone(published.Grants)
	originalAuthorization := buildRuntimeAuthorization(published, time.Now().Add(time.Minute))
	originalAuthorization.SourceDigest = svc.runtime.routes.Load().Digest
	svc.runtime.auth.Store(originalAuthorization)
	originalGrants, originalRevision := slices.Clone(data.Grants), data.Subject.PersonalGrantRevision
	originalHash := originalAuthorization.PersonalGrantStates[data.Subject.ID].Hash
	baseline := svc.projectMemberModels("usr_reader", data)
	if baseline.RuntimeApplied == nil || !*baseline.RuntimeApplied || baseline.ApplicationStatus != "applied" {
		t.Fatal("fixed original publication is not established")
	}
	data.Grants[0].CreatedAt = data.Grants[0].CreatedAt.Add(time.Minute)
	changed := svc.projectMemberModels("usr_reader", data)
	if svc.runtime.auth.Load() != originalAuthorization || changed.RuntimeApplied == nil || *changed.RuntimeApplied || changed.ApplicationStatus != "not_applied" || changed.ETag == baseline.ETag {
		t.Fatal("fixed original publication accepted changed grant timestamp")
	}
	if changed.UserID != baseline.UserID || changed.CanEdit != baseline.CanEdit || len(changed.PersonalModels) != 1 || changed.PersonalModels[0].ID != baseline.PersonalModels[0].ID {
		t.Fatal("timestamp probe changed exact target authority or set")
	}
	data.Grants = slices.Clone(originalGrants)
	svc.runtime.mu.Lock()
	busy := svc.projectMemberModels("usr_reader", data)
	svc.runtime.mu.Unlock()
	if !busy.runtimeBusy || busy.RuntimeApplied != nil || busy.ApplicationStatus != "unavailable" || busy.ETag != baseline.ETag {
		t.Fatal("busy restored observation claimed application or changed durable review")
	}
	restored := svc.projectMemberModels("usr_reader", data)
	if restored.RuntimeApplied == nil || !*restored.RuntimeApplied || restored.ApplicationStatus != "applied" || restored.ETag != baseline.ETag {
		t.Fatal("fresh exact restoration did not apply original publication")
	}
	if busy.RuntimeApplied != nil || changed.RuntimeApplied == nil || *changed.RuntimeApplied {
		t.Fatal("later read rewrote an earlier unknown or mismatched observation")
	}
	if svc.runtime.auth.Load() != originalAuthorization || originalAuthorization.PersonalGrantStates[data.Subject.ID].Hash != originalHash || data.Subject.PersonalGrantRevision != originalRevision || !reflect.DeepEqual(data.Grants, originalGrants) || !reflect.DeepEqual(published.Grants, originalGrants) {
		t.Fatal("read-only observations altered publication, complete grant provenance or revision")
	}
}

// Exact stored rates are not numeric output when the current route proof is
// unavailable; a fresh ready observation must preserve zero, precision and unit.
func TestMemberModelsConfiguredPricesAcrossPublisherObservations(t *testing.T) {
	svc, data, _ := memberModelsProjectionFixture(t)
	data.Prices = []entity.ModelPrice{{ID: "prc_observed", ProviderModelID: "pmd_one"}}
	data.Rates = []entity.PriceRate{
		{ModelPriceID: "prc_observed", Metric: pricing.Input, Tier: pricing.Base, Unit: pricing.Unit, Currency: "USD", Amount: "0", Enabled: true},
		{ModelPriceID: "prc_observed", Metric: pricing.Output, Tier: pricing.Base, Unit: pricing.Unit, Currency: "CNY", Amount: "0.123456789012345678", Enabled: true},
	}
	originalRates, originalPrices := slices.Clone(data.Rates), slices.Clone(data.Prices)
	first := svc.projectMemberModels("usr_reader", data)
	assertPrices := func(view *MemberModelsWorkspace) {
		t.Helper()
		row := view.PersonalModels[0]
		if row.Availability != "ready" || row.InputPrice.State != "priced" || row.InputPrice.Rate == nil || *row.InputPrice.Rate != (MemberModelBaseRate{Amount: "0", Currency: "USD", Unit: pricing.Unit}) || row.OutputPrice.State != "priced" || row.OutputPrice.Rate == nil || *row.OutputPrice.Rate != (MemberModelBaseRate{Amount: "0.123456789012345678", Currency: "CNY", Unit: pricing.Unit}) {
			t.Fatal("known-ready configured price lost exact stored values")
		}
	}
	assertPrices(first)
	svc.runtime.mu.Lock()
	busy := svc.projectMemberModels("usr_reader", data)
	svc.runtime.mu.Unlock()
	row := busy.PersonalModels[0]
	if !busy.runtimeBusy || row.Availability != "unknown" || len(row.Protocols) != 0 || row.Selectable || row.InputPrice.State != "unavailable" || row.InputPrice.Rate != nil || row.OutputPrice.State != "unavailable" || row.OutputPrice.Rate != nil {
		t.Fatal("busy route fabricated usable configured price")
	}
	fresh := svc.projectMemberModels("usr_reader", data)
	assertPrices(fresh)
	if busy.PersonalModels[0].InputPrice.Rate != nil || busy.PersonalModels[0].OutputPrice.Rate != nil || busy.ETag != first.ETag || fresh.ETag != first.ETag || !reflect.DeepEqual(data.Rates, originalRates) || !reflect.DeepEqual(data.Prices, originalPrices) {
		t.Fatal("fresh observation changed prior unknown or stored pricing basis")
	}
}
