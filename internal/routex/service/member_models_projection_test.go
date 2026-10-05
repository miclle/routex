package service

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/pricing"
)

func memberModelsProjectionFixture(t *testing.T) (*Service, *memberModelsData, *runtimeData) {
	t.Helper()
	svc, data, _ := runtimeFixture(t, "https://example.invalid/v1")
	svc.runtime.done = make(chan struct{})
	data.Users[0].CreatedAt = time.Now().UTC().Add(-time.Hour)
	data.Users[0].Role = entity.RoleMember
	data.Users[0].PersonalGrantRevision = strings.Repeat("a", 64)
	data.EgressSetting = entity.EgressSetting{ID: 1, ETag: "0"}
	auth := buildRuntimeAuthorization(data, time.Now().Add(time.Minute))
	auth.SourceDigest = "same"
	svc.runtime.auth.Store(auth)
	routes, err := svc.buildRuntimeRoutes(data)
	if err != nil {
		t.Fatal(err)
	}
	svc.runtime.routes.Store(&runtimeRoutes{ID: "cfg_members", Digest: "same", Models: routes})
	view := &memberModelsData{Subject: data.Users[0], Grants: slices.Clone(data.Grants), Models: slices.Clone(data.Models), Names: slices.Clone(data.Names), Bindings: slices.Clone(data.Bindings), ProviderModels: slices.Clone(data.ProviderModels), Connections: slices.Clone(data.Connections), Credentials: slices.Clone(data.Credentials), Access: slices.Clone(data.Access), Providers: slices.Clone(data.Providers), EgressSetting: data.EgressSetting, CanEdit: true, ProvidersRead: true, PricesRead: true, TeamModels: map[string]bool{}, TeamBasis: []memberModelsTeamBasis{}}
	return svc, view, data
}
func TestMemberModelsProjectionHasNineSafeCellsAndIndependentVisibility(t *testing.T) {
	svc, data, _ := memberModelsProjectionFixture(t)
	data.Prices = []entity.ModelPrice{{ID: "prc_one", ProviderModelID: "pmd_one"}}
	data.Rates = []entity.PriceRate{{ModelPriceID: "prc_one", Metric: pricing.Input, Tier: pricing.Base, Unit: pricing.Unit, Currency: "USD", Amount: "0", Enabled: true}}
	view := svc.projectMemberModels("usr_reader", data)
	if len(view.PersonalModels) != 1 || len(view.AvailableModels) != 0 || view.RuntimeApplied == nil || !*view.RuntimeApplied || view.ApplicationStatus != "applied" {
		t.Fatal(view)
	}
	row := view.PersonalModels[0]
	if row.Availability != "ready" || !slices.Equal(row.Protocols, []string{entity.ProtocolOpenAIChat}) || row.InputPrice.State != "priced" || row.InputPrice.Rate.Amount != "0" || row.OutputPrice.State != "missing" || row.Type != nil || row.UpdatedAt != nil || row.Selectable || !slices.Equal(row.Providers, []string{"Provider One"}) {
		t.Fatal(row)
	}
	raw, _ := json.Marshal(view)
	for _, private := range []string{"PersonalGrantRevision", "personal_grant_revision", "SourceRequestID", "source_request_id", "connection_id", "provider_model_id", "ciphertext", "password", "token_hash", "secret", "receipt"} {
		if strings.Contains(string(raw), private) {
			t.Fatal("private fact entered workspace", private, string(raw))
		}
	}
	etag := view.ETag
	data.ProvidersRead = false
	data.PricesRead = false
	view = svc.projectMemberModels("usr_reader", data)
	if view.PersonalModels[0].Providers != nil || view.PersonalModels[0].InputPrice.State != "unauthorized" || view.PersonalModels[0].InputPrice.Rate != nil || view.ETag != etag {
		t.Fatal("visibility changed reviewed grant intent", view)
	}
	data.Grants = nil
	data.ProvidersRead = true
	data.PricesRead = true
	data.CanEdit = false
	view = svc.projectMemberModels("usr_reader", data)
	if len(view.AvailableModels) != 0 || view.CanEdit {
		t.Fatal("read-only permission leaked write", view)
	}
}
func TestMemberModelsReviewETagCoversIdentityProvenanceAndEligibilityOnly(t *testing.T) {
	svc, data, _ := memberModelsProjectionFixture(t)
	data.TeamBasis = []memberModelsTeamBasis{{TeamID: "tea_one", MembershipID: "tmm_one", ModelID: "mdl_team"}}
	original := svc.projectMemberModels("usr_reader", data).ETag
	data.Providers = []entity.Provider{{ID: "prv_one", Name: "Renamed Provider"}}
	data.Prices = []entity.ModelPrice{{ID: "price", ProviderModelID: "pmd_one"}}
	data.Rates = []entity.PriceRate{{ModelPriceID: "price", Metric: pricing.Input, Tier: pricing.Base, Unit: pricing.Unit, Currency: "USD", Amount: "9", Enabled: true}}
	if current := svc.projectMemberModels("usr_reader", data).ETag; current != original {
		t.Fatal("cosmetic label/price changed grant review")
	}
	if current := svc.projectMemberModels("usr_other", data).ETag; current == original {
		t.Fatal("review not actor-bound")
	}
	for _, change := range []func(*memberModelsData){
		func(d *memberModelsData) { d.Subject.PersonalGrantRevision = strings.Repeat("b", 64) }, func(d *memberModelsData) { d.Subject.CreatedAt = d.Subject.CreatedAt.Add(time.Second) }, func(d *memberModelsData) { d.Subject.Disabled = true }, func(d *memberModelsData) { d.TeamBasis[0].MembershipID = "tmm_new" }, func(d *memberModelsData) { source := "mar_original"; d.Grants[0].SourceRequestID = &source }, func(d *memberModelsData) { d.Grants[0].CreatedAt = d.Grants[0].CreatedAt.Add(time.Second) }, func(d *memberModelsData) { d.Bindings[0].Weight = 0 }, func(d *memberModelsData) { d.ProviderModels[0].Disabled = true }, func(d *memberModelsData) { d.Names[0].Name = "renamed-public" },
	} {
		_, copy, _ := memberModelsProjectionFixture(t)
		copy.Subject = data.Subject
		copy.TeamBasis = slices.Clone(data.TeamBasis)
		copy.Grants = slices.Clone(data.Grants)
		copy.Models = slices.Clone(data.Models)
		copy.Names = slices.Clone(data.Names)
		copy.Bindings = slices.Clone(data.Bindings)
		copy.ProviderModels = slices.Clone(data.ProviderModels)
		copy.Connections = slices.Clone(data.Connections)
		copy.Credentials = slices.Clone(data.Credentials)
		copy.Access = slices.Clone(data.Access)
		change(copy)
		if svc.projectMemberModels("usr_reader", copy).ETag == original {
			t.Fatal("review ignored changed authority/eligibility")
		}
	}
}
func TestMemberModelsEligibilityFailsClosedForIncompleteCurrentPublication(t *testing.T) {
	for _, change := range []func(*Service, *memberModelsData){
		func(s *Service, _ *memberModelsData) { s.runtime.auth.Load().ValidUntil = time.Now().Add(-time.Second) },
		func(s *Service, _ *memberModelsData) { s.runtime.routes.Load().Digest = "previous" },
		func(_ *Service, d *memberModelsData) { d.Bindings[0].Weight = 0 },
		func(_ *Service, d *memberModelsData) {
			d.Credentials = append(d.Credentials, entity.ProviderCredential{ID: "crd_uncovered", ConnectionID: "con_one", Enabled: true, VerificationStatus: "verified"})
		},
		func(_ *Service, d *memberModelsData) { d.EgressSetting.ETag = "unpublished" },
		func(_ *Service, d *memberModelsData) { d.ProviderModels[0].ETag = "unpublished" },
		func(s *Service, _ *memberModelsData) { s.InvalidateRuntimeModel("mdl_one") },
	} {
		svc, data, _ := memberModelsProjectionFixture(t)
		change(svc, data)
		view := svc.projectMemberModels("usr_reader", data)
		row := view.PersonalModels[0]
		if row.Availability != "unknown" || len(row.Protocols) != 0 || row.Selectable {
			t.Fatal("stale or partial route claimed ready", row)
		}
	}
	svc, data, _ := memberModelsProjectionFixture(t)
	data.Models[0].Status = entity.ResourceDisabled
	row := svc.projectMemberModels("usr_reader", data).PersonalModels[0]
	if row.Availability != "unavailable" || len(row.Protocols) != 0 {
		t.Fatal(row)
	}
	svc, data, runtimeData := memberModelsProjectionFixture(t)
	data.ProviderModels[0].Disabled = true
	runtimeData.ProviderModels[0].Disabled = true
	auth := buildRuntimeAuthorization(runtimeData, time.Now().Add(time.Minute))
	auth.SourceDigest = "same"
	svc.runtime.auth.Store(auth)
	row = svc.projectMemberModels("usr_reader", data).PersonalModels[0]
	if row.Availability != "unavailable" || len(row.Protocols) != 0 {
		t.Fatal("known disabled supply claimed ready", row)
	}
}
func TestMemberModelsAuditUsesTypedBoundedProjection(t *testing.T) {
	changes := memberModelsAuditChanges{UserID: "usr_subject", Before: []string{"mdl_one"}, After: []string{}, Reason: "explicit removal"}
	raw, _ := json.Marshal(changes)
	details := string(raw)
	row := entity.AuditEvent{Action: "member.models.update", ResourceType: "user_model_grant", ResourceID: "usr_subject", DetailsJSON: &details}
	if projected, valid := memberModelsAuditProjection(row); !valid || len(projected.After) != 0 {
		t.Fatal(projected, valid)
	}
	projected := auditRecord(row)
	if len(projected.Changes) == 0 {
		t.Fatal("typed audit dispatch missing")
	}
	for _, bad := range []string{`{"user_id":"usr_foreign","before":[],"after":[],"reason":"review"}`, `{"user_id":"usr_subject","before":null,"after":[],"reason":"review"}`, `{"user_id":"usr_subject","before":[],"after":["mdl_one","mdl_one"],"reason":"review"}`} {
		row.DetailsJSON = &bad
		if _, valid := memberModelsAuditProjection(row); valid {
			t.Fatal("unsafe typed audit accepted")
		}
	}
}

// A publisher lock is an observation condition, not a reviewed grant or route change.
func TestMemberModelsETagStableAcrossPublisherContention(t *testing.T) {
	for _, direction := range []string{"GET ready PUT busy", "GET busy PUT ready"} {
		t.Run(direction, func(t *testing.T) {
			svc, data, _ := memberModelsProjectionFixture(t)
			ready := svc.projectMemberModels("usr_reader", data)
			if ready.PersonalModels[0].Availability != "ready" {
				t.Fatal("fixture must establish actual published ready projection")
			}
			svc.runtime.mu.Lock()
			busy := svc.projectMemberModels("usr_reader", data)
			svc.runtime.mu.Unlock()
			if !busy.runtimeBusy || busy.RuntimeApplied != nil || busy.ApplicationStatus != "unavailable" || busy.PersonalModels[0].Availability != "unknown" || len(busy.PersonalModels[0].Protocols) != 0 {
				t.Fatal("contended projection must remain truthfully unknown")
			}
			get, put := ready, busy
			if direction == "GET busy PUT ready" {
				get, put = busy, svc.projectMemberModels("usr_reader", data)
			}
			// This is the exact ETag comparison reached by a reduction, which has
			// no addition to validate and has not changed any reviewed SQL basis.
			if get.ETag != put.ETag {
				t.Fatalf("same reviewed durable basis falsely conflicts: GET=%s PUT=%s", get.PersonalModels[0].Availability, put.PersonalModels[0].Availability)
			}
		})
	}
}

func TestMemberModelsETagContentionNeverAllowsUnknownAddition(t *testing.T) {
	svc, data, _ := memberModelsProjectionFixture(t)
	data.Grants = nil
	ready := svc.projectMemberModels("usr_reader", data)
	if len(ready.AvailableModels) != 1 || !ready.AvailableModels[0].Selectable || ready.AvailableModels[0].Availability != "ready" {
		t.Fatal("positive addition fixture not selectable")
	}
	svc.runtime.mu.Lock()
	busy := svc.projectMemberModels("usr_reader", data)
	svc.runtime.mu.Unlock()
	if len(busy.AvailableModels) != 1 || busy.AvailableModels[0].Selectable || busy.AvailableModels[0].Availability != "unknown" || len(busy.AvailableModels[0].Protocols) != 0 || busy.RuntimeApplied != nil {
		t.Fatal("unknown addition was made eligible by review normalization")
	}
	if ready.ETag != busy.ETag {
		t.Fatal("transient observation altered durable addition review")
	}
	data.ProviderModels[0].Disabled = true
	changed := svc.projectMemberModels("usr_reader", data)
	if changed.ETag == ready.ETag || changed.AvailableModels[0].Selectable {
		t.Fatal("actual disabled configuration must invalidate review and eligibility")
	}
}

func TestMemberModelsETagStillCoversDurableChangeWhileBusy(t *testing.T) {
	changes := []struct {
		name   string
		change func(*memberModelsData)
	}{
		{"grant ABA", func(d *memberModelsData) { d.Subject.PersonalGrantRevision = strings.Repeat("b", 64) }},
		{"provenance", func(d *memberModelsData) { source := "mar_other"; d.Grants[0].SourceRequestID = &source }},
		{"subject disabled", func(d *memberModelsData) { d.Subject.Disabled = true }},
		{"Team membership", func(d *memberModelsData) {
			d.TeamBasis = []memberModelsTeamBasis{{TeamID: "tea_one", MembershipID: "tmm_changed", ModelID: "mdl_one"}}
		}},
		{"name", func(d *memberModelsData) { d.Names[0].Name = "changed-public-name" }},
		{"weight", func(d *memberModelsData) { d.Bindings[0].Weight = 0 }},
		{"credential coverage", func(d *memberModelsData) { d.Access = nil }},
		{"egress revision", func(d *memberModelsData) { d.EgressSetting.ETag = "changed" }},
		{"permission", func(d *memberModelsData) { d.CanEdit = false }},
	}
	for _, test := range changes {
		t.Run(test.name, func(t *testing.T) {
			svc, data, _ := memberModelsProjectionFixture(t)
			svc.runtime.mu.Lock()
			defer svc.runtime.mu.Unlock()
			before := svc.projectMemberModels("usr_reader", data)
			test.change(data)
			after := svc.projectMemberModels("usr_reader", data)
			if before.ETag == after.ETag {
				t.Fatal("actual durable authority/configuration change ignored")
			}
		})
	}
}

func TestMemberModelsETagNeverSubstitutesForCurrentAdditionEligibility(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*Service, *memberModelsData)
	}{
		{"expired lease", func(s *Service, _ *memberModelsData) { s.runtime.auth.Load().ValidUntil = time.Now().Add(-time.Second) }},
		{"Model tombstone", func(s *Service, _ *memberModelsData) { s.InvalidateRuntimeModel("mdl_one") }},
		{"routes unpublished", func(s *Service, _ *memberModelsData) { s.runtime.routes.Load().Digest = "old" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			svc, data, _ := memberModelsProjectionFixture(t)
			data.Grants = nil
			ready := svc.projectMemberModels("usr_reader", data)
			test.change(svc, data)
			current := svc.projectMemberModels("usr_reader", data)
			if len(current.AvailableModels) != 1 || current.AvailableModels[0].Availability != "unknown" || current.AvailableModels[0].Selectable || len(current.AvailableModels[0].Protocols) != 0 {
				t.Fatal("durable review incorrectly granted current admission eligibility")
			}
			if current.ETag != ready.ETag {
				t.Fatal("runtime-only state incorrectly changed reviewed configuration")
			}
		})
	}
}
