package service

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
)

func archiveRoutingPlanFixture(t *testing.T) (*Service, entity.User, entity.RuntimeConfigurationArchive, runtimeArchiveRoutingState, runtimeConfigurationSourceV1, map[string]routingSupplyState) {
	t.Helper()
	data := configurationFixtureDataV1()
	// Every input is frozen/detached; no database or network is used by planning.
	source := configurationFixtureV1(t).Source
	current := runtimeArchiveRoutingState{data.Providers, data.Connections, data.Models, data.ProviderModels, data.Bindings, data.Egresses, data.EgressSetting}
	actor := entity.User{ID: configurationTestIDV1("usr", 900), CreatedAt: time.Date(2026, 10, 10, 1, 2, 3, 0, time.UTC)}
	header := entity.RuntimeConfigurationArchive{ID: configurationTestIDV1("rcv", 900), CreatedAt: actor.CreatedAt}
	supplies := map[string]routingSupplyState{data.ProviderModels[0].ID: {Covered: true, SourceAvailable: true}}
	return &Service{}, actor, header, current, source, supplies
}
func TestRuntimeArchiveRollbackPlanClosedAllowlist(t *testing.T) {
	s, actor, header, current, source, supplies := archiveRoutingPlanFixture(t)
	current.Connections[0].Enabled = false
	current.Providers[0].Enabled = false
	current.ProviderModels[0].Disabled = true
	current.Models[0].Status = entity.ResourceDisabled
	source.Providers[0].Enabled = true
	source.Connections[0].Enabled = true
	source.ProviderModels[0].Disabled = false
	source.Models[0].Status = entity.ResourceActive
	source.Connections[0].BaseURL = "https://replacement.example.test"
	source.Connections[0].TransportGeneration = configurationTestIDV1("rev", 900)
	source.ProviderModels[0].SupportsImageInput = false
	source.ProviderModels[0].CapabilityTransportGeneration = configurationTestIDV1("rev", 901)
	source.Users[0].Disabled = true
	source.Credentials[0].Enabled = false
	source.Credentials[0].SourceProof = "historical-proof-sentinel"
	before := runtimeArchiveRoutingDigest(current)
	plan := s.planRuntimeArchiveRollback(actor, header, current, source, supplies, true)
	if !plan.Preview.CanApply || len(plan.Preview.Connections) != 1 || len(plan.Preview.Declarations) != 1 {
		t.Fatal("valid disabled preparation rejected", plan.Preview.BlockerCodes)
	}
	desired := plan.Desired
	if desired.Providers[0].Enabled || desired.Connections[0].Enabled || !desired.ProviderModels[0].Disabled || desired.Models[0].Status != entity.ResourceDisabled {
		t.Fatal("historical lifecycle restored")
	}
	if desired.Connections[0].TransportGeneration != current.Connections[0].TransportGeneration || desired.ProviderModels[0].CapabilityTransportGeneration != current.ProviderModels[0].CapabilityTransportGeneration {
		t.Fatal("historical proof restored")
	}
	if desired.Connections[0].BaseURL != source.Connections[0].BaseURL || desired.ProviderModels[0].SupportsImageInput {
		t.Fatal("allowlisted fields absent")
	}
	if before != runtimeArchiveRoutingDigest(current) {
		t.Fatal("planning mutated authoritative input")
	}
}
func TestRuntimeArchiveRollbackPlanTopologyAndAuthority(t *testing.T) {
	for _, which := range []string{"enabled_transport", "provider_birth", "connection_birth", "provider_model_birth", "model_birth", "binding_birth", "extra_model", "rebound", "locked_protocol", "empty_disabled_protocol", "locked_adapter", "write_denied", "archived_model"} {
		t.Run(which, func(t *testing.T) {
			s, a, h, c, source, supply := archiveRoutingPlanFixture(t)
			write := true
			code := "topology_changed"
			switch which {
			case "enabled_transport":
				source.Connections[0].BaseURL = "https://changed.example.test"
				code = "changed_transport_enabled"
			case "provider_birth":
				source.Providers[0].CreatedAt = source.Providers[0].CreatedAt.Add(time.Millisecond)
			case "connection_birth":
				source.Connections[0].CreatedAt = source.Connections[0].CreatedAt.Add(time.Millisecond)
			case "provider_model_birth":
				source.ProviderModels[0].CreatedAt = source.ProviderModels[0].CreatedAt.Add(time.Millisecond)
			case "model_birth":
				source.Models[0].CreatedAt = source.Models[0].CreatedAt.Add(time.Millisecond)
			case "binding_birth":
				source.Bindings[0].CreatedAt = source.Bindings[0].CreatedAt.Add(time.Millisecond)
			case "extra_model":
				c.Models = append(c.Models, entity.Model{ID: configurationTestIDV1("mdl", 902)})
			case "rebound":
				source.Bindings[0].ProviderModelID = configurationTestIDV1("pmd", 902)
			case "locked_protocol":
				c.Connections[0].Enabled = false
				source.Connections[0].Protocol = "openai_responses"
				code = "transport_topology_locked"
			case "empty_disabled_protocol":
				c.Connections[0].Enabled = false
				c.ProviderModels, c.Bindings = nil, nil
				source.ProviderModels, source.Bindings = nil, nil
				source.Connections[0].Protocol = entity.ProtocolOpenAIResponses
				source.Connections[0].BaseURL = "https://must-not-restore.example.test"
				code = "transport_topology_locked"
			case "locked_adapter":
				c.Connections[0].Enabled = false
				source.Connections[0].Adapter = entity.AdapterAzureOpenAIClassic
				source.Connections[0].APIVersion = configurationTestPointerV1("2024-10-21")
				code = "transport_topology_locked"
			case "write_denied":
				write = false
				code = "write_authority_required"
			case "archived_model":
				c.Models[0].Status = entity.ResourceArchived
				source.ProviderModels[0].SupportsImageInput = false
				code = "model_archived"
			}
			p := s.planRuntimeArchiveRollback(a, h, c, source, supply, write)
			if p.Preview.CanApply || !slices.Contains(p.Preview.BlockerCodes, code) {
				t.Fatal("unsafe archive admitted", p.Preview.BlockerCodes)
			}
			if which == "empty_disabled_protocol" && (!reflect.DeepEqual(p.Desired.Connections, c.Connections) || len(p.Preview.Connections) != 0) {
				t.Fatal("historical protocol changed an empty disabled Connection")
			}
		})
	}
}
func TestRuntimeArchiveRollbackPlanCompleteWeightsAndReadiness(t *testing.T) {
	for _, which := range []string{"valid", "sum", "disabled", "source_unavailable", "uncovered", "egress_disabled", "changed_transport_new_positive"} {
		t.Run(which, func(t *testing.T) {
			s, a, h, c, source, supplies := archiveRoutingPlanFixture(t)
			// A second stable relation supplies a complete 40/60 target in one protocol.
			pm := c.ProviderModels[0]
			pm.ID = configurationTestIDV1("pmd", 902)
			c.ProviderModels = append(c.ProviderModels, pm)
			archivedPM := source.ProviderModels[0]
			archivedPM.ID = pm.ID
			source.ProviderModels = append(source.ProviderModels, archivedPM)
			b := c.Bindings[0]
			b.ID = configurationTestIDV1("bnd", 902)
			b.ProviderModelID = pm.ID
			b.Weight = 0
			c.Bindings = append(c.Bindings, b)
			ab := source.Bindings[0]
			ab.ID = b.ID
			ab.ProviderModelID = b.ProviderModelID
			ab.Weight = 60
			source.Bindings = append(source.Bindings, ab)
			source.Bindings[0].Weight = 40
			supplies[pm.ID] = routingSupplyState{Covered: true, SourceAvailable: true}
			switch which {
			case "sum":
				source.Bindings[0].Weight = 41
			case "disabled":
				c.Connections[0].Enabled = false
			case "source_unavailable":
				supplies[pm.ID] = routingSupplyState{Covered: true}
			case "uncovered":
				supplies[pm.ID] = routingSupplyState{SourceAvailable: true}
			case "egress_disabled":
				c.Egresses[0].Enabled = false
			case "changed_transport_new_positive":
				c.Connections[0].Enabled = false
				source.Connections[0].BaseURL = "https://changed.example.test"
			}
			p := s.planRuntimeArchiveRollback(a, h, c, source, supplies, true)
			if (which == "valid") != p.Preview.CanApply {
				t.Fatal("complete routing eligibility changed", p.Preview.BlockerCodes)
			}
			if which == "valid" && (len(p.Preview.Weights) != 2 || len(p.WeightModels) != 1) {
				t.Fatal("incomplete proposed routing")
			}
		})
	}
}
func TestRuntimeArchiveRollbackIntentAndProofSeparation(t *testing.T) {
	s, a, h, c, source, supplies := archiveRoutingPlanFixture(t)
	p := s.planRuntimeArchiveRollback(a, h, c, source, supplies, true)
	input := RuntimeArchiveRollbackInput{"605670ca-5a6a-4f98-8021-000000000090", "Reviewed routing only"}
	now := time.Now().UTC().Truncate(time.Microsecond)
	row := entity.RuntimeArchiveRollback{RequestID: input.RequestID, ActorID: a.ID, ActorCreatedAt: a.CreatedAt, ArchiveID: h.ID, ArchiveCreatedAt: h.CreatedAt, ReviewETag: p.Preview.ETag, DesiredDigest: p.DesiredDigest, Reason: input.Reason, AppliedAt: now}
	row.InputDigest = runtimeArchiveRollbackIntent(a, h.ID, p.Preview.ETag, input)
	if !runtimeArchiveRollbackValid(row) {
		t.Fatal("exact original intent invalid")
	}
	for _, which := range []string{"actor", "archive", "review", "reason", "request", "digest"} {
		changed := row
		switch which {
		case "actor":
			changed.ActorCreatedAt = changed.ActorCreatedAt.Add(time.Microsecond)
		case "archive":
			changed.ArchiveID = configurationTestIDV1("rcv", 903)
		case "review":
			changed.ReviewETag = strings.Repeat("a", 64)
		case "reason":
			changed.Reason += " other"
		case "request":
			changed.RequestID = "605670ca-5a6a-4f98-8021-000000000091"
		case "digest":
			changed.InputDigest = strings.Repeat("b", 64)
		}
		if runtimeArchiveRollbackValid(changed) {
			t.Fatal("mutated original intent admitted", which)
		}
	}
	// Receipt reads use the shared live-ticket projection directly. Pending is
	// transient original work, not a completed refresh or current runtime proof.
	lease := &systemInstanceLease{id: configurationTestIDV1("ins", 904), startedAt: now}
	s.instance = lease
	s.runtime = &gatewayRuntime{done: make(chan struct{})}
	publication := entity.RuntimePublicationCommand{RequestID: input.RequestID, ActorID: a.ID, ActorCreatedAt: a.CreatedAt, Reason: input.Reason, ClaimInstanceID: lease.id, ClaimInstanceStartedAt: lease.startedAt, ClaimedAt: now, Outcome: "claimed"}
	if got := s.runtimePublicationReceipt(publication); got == nil || got.Status != "unknown" || got.FinalizedAt != nil || got.FailureCode != nil {
		t.Fatal("retained claim invented live ownership", got)
	}
	live, cancelLive := context.WithCancel(t.Context())
	defer cancelLive()
	ticket := &runtimePublicationTicket{row: publication, lease: lease, ctx: live}
	s.runtimePublicationCommands.Store(publication.RequestID, ticket)
	if got := s.runtimePublicationReceipt(publication); got == nil || got.Status != "pending" || got.RequestID != input.RequestID || got.FinalizedAt != nil || got.FailureCode != nil {
		t.Fatal("matching live rollback publication lost pending status", got)
	}
	cancelLive()
	if got := s.runtimePublicationReceipt(publication); got == nil || got.Status != "unknown" {
		t.Fatal("expired ticket remained pending", got)
	}
	s.runtimePublicationCommands.Delete(publication.RequestID)
	for _, outcome := range []string{"refresh_returned_nil", "refresh_returned_error"} {
		terminal := publication
		terminal.Outcome, terminal.FinalizedAt = outcome, &now
		if outcome == "refresh_returned_error" {
			code := "runtime_refresh_failed"
			terminal.FailureCode = &code
		}
		if got := s.runtimePublicationReceipt(terminal); got == nil || got.Status != outcome || got.FinalizedAt == nil || !got.FinalizedAt.Equal(now) || !reflect.DeepEqual(got.FailureCode, terminal.FailureCode) {
			t.Fatal("original terminal outcome changed in rollback receipt", got)
		}
	}
	changedHeader := h
	changedHeader.ContentDigest = strings.Repeat("f", 64)
	if s.planRuntimeArchiveRollback(a, changedHeader, c, source, supplies, true).Preview.ETag == p.Preview.ETag {
		t.Fatal("archive content replacement retained reviewed token")
	}
	c.Connections[0].ETag = "new-revision"
	c.Connections[0].TransportGeneration = configurationTestIDV1("rev", 903)
	if runtimeArchiveRoutingDigest(c) != p.DesiredDigest {
		t.Fatal("current transport proof revision confused with routing target")
	}
	c.Connections[0].Enabled = false
	if runtimeArchiveRoutingDigest(c) == p.DesiredDigest {
		t.Fatal("superseding lifecycle falsely retained application")
	}
	if !reflect.DeepEqual(p.Preview.BlockerCodes, []string{}) {
		t.Fatal("positive fixture blockers")
	}
}

func TestRuntimeArchiveRollbackSourceRelationBounds(t *testing.T) {
	for _, relation := range []string{"providers", "connections", "provider_models", "models", "bindings", "egresses"} {
		t.Run(relation, func(t *testing.T) {
			_, _, _, _, source, _ := archiveRoutingPlanFixture(t)
			fill := func(count int) {
				switch relation {
				case "providers":
					source.Providers = make([]runtimeConfigurationProviderV1, count)
				case "connections":
					source.Connections = make([]runtimeConfigurationProviderConnectionV1, count)
				case "provider_models":
					source.ProviderModels = make([]runtimeConfigurationProviderModelV1, count)
				case "models":
					source.Models = make([]runtimeConfigurationModelV1, count)
				case "bindings":
					source.Bindings = make([]runtimeConfigurationModelProviderBindingV1, count)
				case "egresses":
					source.Egresses = make([]runtimeConfigurationEgressV1, count)
				}
			}
			fill(runtimeArchiveRollbackBound)
			if !runtimeArchiveRollbackSourceWithinBound(source) {
				t.Fatal("exact complete relation bound rejected")
			}
			fill(runtimeArchiveRollbackBound + 1)
			if runtimeArchiveRollbackSourceWithinBound(source) {
				t.Fatal("oversized archive relation entered cross-relation planning")
			}
		})
	}
}
