package service

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
)

func credentialReadinessRuntimeFixture() (*Service, entity.ProviderCredential) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	replacement := entity.ProviderCredential{
		ID:                 "crd_replacement",
		ConnectionID:       "con_one",
		Name:               "Replacement",
		Enabled:            true,
		VerificationStatus: "verified",
		CreatedAt:          now.Add(-time.Minute),
	}
	auth := buildRuntimeAuthorization(&runtimeData{
		Models:         []entity.Model{{ID: "mdl_one", Status: "active"}},
		ProviderModels: []entity.ProviderModel{{ID: "pmd_one", ETag: "revision"}},
		Credentials:    []entity.ProviderCredential{replacement},
		Access:         []entity.CredentialModelAccess{{CredentialID: replacement.ID, ProviderModelID: "pmd_one"}},
	}, now.Add(time.Minute))
	auth.SourceDigest = "source_digest"
	auth.ConnectionRevisions["con_one"] = "transport"
	runtime := &gatewayRuntime{}
	runtime.auth.Store(auth)
	runtime.routes.Store(&runtimeRoutes{
		ID: "cfg_current", Digest: auth.SourceDigest,
		Models: map[string][]runtimeRoute{"mdl_one": {{
			Route: gatewayRoute{
				Client: &http.Client{}, BindingID: "bnd_one", Weight: 100,
				ProviderID: "prv_one", ProviderModelID: "pmd_one", ConnectionID: "con_one",
				Protocol: entity.ProtocolOpenAIChat, UpstreamName: "native", EgressRevision: "transport",
			},
			Credentials: []runtimeCredential{{ID: replacement.ID, Plaintext: "secret-must-not-leak"}},
		}}},
	})
	return &Service{runtime: runtime}, replacement
}

func TestCredentialReadinessRequiresPublishedCandidate(t *testing.T) {
	for _, test := range []struct {
		name, want string
		change     func(*Service)
	}{
		{"eligible", "", func(*Service) {}},
		{"auth_only", "coverage_missing", func(s *Service) {
			routes := s.runtime.routes.Load()
			candidates := routes.Models["mdl_one"]
			candidates[0].Credentials = nil
			candidates[0].Route.CredentialID = "crd_replacement"
			routes.Models["mdl_one"] = candidates
		}},
		{"source_digest_missing", "runtime_stale", func(s *Service) { s.runtime.auth.Load().SourceDigest = "" }},
		{"fresh_auth_old_routes", "runtime_stale", func(s *Service) { s.runtime.auth.Load().SourceDigest = "new_source" }},
		{"expired_lease", "runtime_unavailable", func(s *Service) { s.runtime.auth.Load().ValidUntil = time.Now().Add(-time.Second) }},
		{"zero_weight", "no_eligible_routes", func(s *Service) { s.runtime.routes.Load().Models["mdl_one"][0].Route.Weight = 0 }},
		{"inactive_model", "no_eligible_routes", func(s *Service) { s.runtime.auth.Load().Models["mdl_one"] = false }},
		{"disabled_provider_model", "no_eligible_routes", func(s *Service) { s.runtime.auth.Load().ProviderModels["pmd_one"] = false }},
		{"missing_access", "coverage_missing", func(s *Service) { delete(s.runtime.auth.Load().CredentialAccess["crd_replacement"], "pmd_one") }},
		{"credential_tombstone", "route_unavailable", func(s *Service) { s.InvalidateRuntimeCredential("crd_replacement") }},
		{"model_tombstone", "route_unavailable", func(s *Service) { s.InvalidateRuntimeModel("mdl_one") }},
		{"provider_model_tombstone", "route_unavailable", func(s *Service) { s.runtime.deniedProviderModels.Store("pmd_one", uint64(1)) }},
		{"credential_cooling", "route_unavailable", func(s *Service) { s.markGatewayCredentialRejected("crd_replacement") }},
		{"connection_cooling", "route_unavailable", func(s *Service) { s.markGatewayConnectionFailure("con_one") }},
		{"egress_generation", "route_unavailable", func(s *Service) { s.egressGeneration.Add(1) }},
		{"transport_revision", "route_unavailable", func(s *Service) { s.runtime.auth.Load().ConnectionRevisions["con_one"] = "changed" }},
		{"nil_client", "route_unavailable", func(s *Service) { s.runtime.routes.Load().Models["mdl_one"][0].Route.Client = nil }},
	} {
		t.Run(test.name, func(t *testing.T) {
			svc, _ := credentialReadinessRuntimeFixture()
			test.change(svc)
			capture, err := svc.captureCredentialRetirementRuntime("con_one", "crd_source", "crd_replacement")
			if err != nil {
				t.Fatal(err)
			}
			if test.want == "" {
				if len(capture.Blockers) != 0 || capture.EligibleRouteCount != 1 || len(capture.scope) != 1 {
					t.Fatalf("eligible capture %+v", capture)
				}
			} else if !slices.Contains(capture.Blockers, test.want) {
				t.Fatalf("blockers %v want %s", capture.Blockers, test.want)
			}
			encoded, _ := json.Marshal(capture)
			if strings.Contains(string(encoded), "secret-must-not-leak") || strings.Contains(string(encoded), "Plaintext") {
				t.Fatal("capture exposed secret material")
			}
		})
	}
}

func TestCredentialReadinessCaptureNeverWaitsForPublisher(t *testing.T) {
	svc, _ := credentialReadinessRuntimeFixture()
	svc.runtime.mu.Lock()
	defer svc.runtime.mu.Unlock()
	started := time.Now()
	capture, err := svc.captureCredentialRetirementRuntime("con_one", "crd_source", "crd_replacement")
	if err != nil || !slices.Contains(capture.Blockers, "runtime_unavailable") || time.Since(started) > time.Second {
		t.Fatal("capture waited for publisher mutex")
	}
	if got := svc.validateCredentialRetirementRuntimeCapture(capture); !slices.Contains(got, "runtime_unavailable") {
		t.Fatalf("recapture waited or trusted contention: %v", got)
	}
}

func TestCredentialReadinessRecaptureRejectsChangedConditions(t *testing.T) {
	for _, test := range []struct {
		name, want string
		change     func(*Service)
	}{
		{"unchanged", "", func(*Service) {}},
		{"new_authorization", "runtime_stale", func(s *Service) { copy := *s.runtime.auth.Load(); s.runtime.auth.Store(&copy) }},
		{"new_routes", "runtime_stale", func(s *Service) { copy := *s.runtime.routes.Load(); s.runtime.routes.Store(&copy) }},
		{"epoch", "runtime_stale", func(s *Service) { s.runtime.epoch.Add(1) }},
		{"egress", "runtime_stale", func(s *Service) { s.egressGeneration.Add(1) }},
		{"lease", "runtime_unavailable", func(s *Service) { s.runtime.auth.Load().ValidUntil = time.Now().Add(-time.Second) }},
		{"health", "route_unavailable", func(s *Service) { s.markGatewayCredentialRejected("crd_replacement") }},
	} {
		t.Run(test.name, func(t *testing.T) {
			svc, _ := credentialReadinessRuntimeFixture()
			capture, err := svc.captureCredentialRetirementRuntime("con_one", "crd_source", "crd_replacement")
			if err != nil {
				t.Fatal(err)
			}
			test.change(svc)
			blockers := svc.validateCredentialRetirementRuntimeCapture(capture)
			if test.want == "" {
				if len(blockers) != 0 {
					t.Fatal(blockers)
				}
			} else if !slices.Contains(blockers, test.want) {
				t.Fatalf("blockers %v want %s", blockers, test.want)
			}
		})
	}
}

func TestCredentialReadinessProjectionIsBoundedAndSorted(t *testing.T) {
	svc, _ := credentialReadinessRuntimeFixture()
	candidate := svc.runtime.routes.Load().Models["mdl_one"][0]
	candidates := []runtimeRoute{}
	for i := maxCredentialReadinessRoutes - 1; i >= 0; i-- {
		copy := candidate
		copy.Route.BindingID = fmt.Sprintf("bnd_%03d", i)
		candidates = append(candidates, copy)
	}
	svc.runtime.routes.Load().Models["mdl_one"] = candidates
	capture, err := svc.captureCredentialRetirementRuntime("con_one", "crd_source", "crd_replacement")
	if err != nil || len(capture.Blockers) != 0 || len(capture.scope) != maxCredentialReadinessRoutes || capture.scope[0].BindingID != "bnd_000" {
		t.Fatalf("bounded sorted capture %+v %v", capture, err)
	}
	svc.runtime.routes.Load().Models["mdl_one"] = append(candidates, candidate)
	capture, err = svc.captureCredentialRetirementRuntime("con_one", "crd_source", "crd_replacement")
	if err != nil || !slices.Contains(capture.Blockers, "scope_overflow") || capture.EligibleRouteCount != 0 || len(capture.scope) != 0 {
		t.Fatalf("overflow capture %+v %v", capture, err)
	}
}

func TestCredentialRetirementEvidenceRequiresExactNativeTerminalTuple(t *testing.T) {
	svc, replacement := credentialReadinessRuntimeFixture()
	capture, err := svc.captureCredentialRetirementRuntime("con_one", "crd_source", replacement.ID)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	valid := credentialRetirementEvidenceRow{
		AttemptID:                "att_proof",
		AttemptRequestID:         "req_proof",
		RequestID:                "req_proof",
		CredentialID:             replacement.ID,
		SnapshotID:               capture.SnapshotID,
		CallSnapshotID:           capture.SnapshotID,
		CallStartedAt:            now,
		AttemptStatus:            "success",
		CallStatus:               "success",
		NativeCompletionEvidence: "completed",
		ModelID:                  "mdl_one",
		ProviderModelID:          "pmd_one",
		CallProviderModelID:      "pmd_one",
		ConnectionID:             "con_one",
		CallConnectionID:         "con_one",
		Protocol:                 entity.ProtocolOpenAIChat,
		StartedAt:                now,
		CompletedAt:              now.Add(time.Second),
		AttemptNumber:            1,
	}
	if !valid.matches(replacement, capture) {
		t.Fatal("valid proof rejected")
	}
	for _, test := range []struct {
		name   string
		change func(*credentialRetirementEvidenceRow)
	}{
		{"unknown", func(row *credentialRetirementEvidenceRow) { row.NativeCompletionEvidence = "unknown" }},
		{"handoff", func(row *credentialRetirementEvidenceRow) { row.NativeCompletionEvidence = "handoff" }},
		{"blocked", func(row *credentialRetirementEvidenceRow) { row.NativeCompletionEvidence = "blocked" }},
		{"incomplete", func(row *credentialRetirementEvidenceRow) { row.NativeCompletionEvidence = "incomplete" }},
		{"case_folded_native", func(row *credentialRetirementEvidenceRow) { row.NativeCompletionEvidence = "COMPLETED" }},
		{"canceled", func(row *credentialRetirementEvidenceRow) { row.AttemptStatus = "canceled" }},
		{"logical_failure", func(row *credentialRetirementEvidenceRow) { row.CallStatus = "error" }},
		{"case_folded_status", func(row *credentialRetirementEvidenceRow) { row.CallStatus = "SUCCESS" }},
		{"wrong_credential", func(row *credentialRetirementEvidenceRow) { row.CredentialID = "crd_other" }},
		{"old_cfg", func(row *credentialRetirementEvidenceRow) { row.SnapshotID = "cfg_old" }},
		{"wrong_logical_cfg", func(row *credentialRetirementEvidenceRow) { row.CallSnapshotID = "cfg_other" }},
		{"wrong_model", func(row *credentialRetirementEvidenceRow) { row.ModelID = "mdl_other" }},
		{"wrong_protocol", func(row *credentialRetirementEvidenceRow) { row.Protocol = entity.ProtocolOpenAIResponses }},
		{"wrong_pm", func(row *credentialRetirementEvidenceRow) { row.ProviderModelID = "pmd_other" }},
		{"wrong_logical_pm", func(row *credentialRetirementEvidenceRow) { row.CallProviderModelID = "pmd_other" }},
		{"wrong_connection", func(row *credentialRetirementEvidenceRow) { row.ConnectionID = "con_other" }},
		{"wrong_logical_connection", func(row *credentialRetirementEvidenceRow) { row.CallConnectionID = "con_other" }},
		{"wrong_request", func(row *credentialRetirementEvidenceRow) { row.AttemptRequestID = "req_other" }},
		{"before_creation", func(row *credentialRetirementEvidenceRow) { row.StartedAt = replacement.CreatedAt.Add(-time.Second) }},
		{"logical_before_creation", func(row *credentialRetirementEvidenceRow) {
			row.CallStartedAt = replacement.CreatedAt.Add(-time.Second)
		}},
		{"invalid_ordinal", func(row *credentialRetirementEvidenceRow) { row.AttemptNumber = 0 }},
		{"backward_time", func(row *credentialRetirementEvidenceRow) { row.CompletedAt = row.StartedAt.Add(-time.Second) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			copy := valid
			test.change(&copy)
			if copy.matches(replacement, capture) {
				t.Fatal("hostile proof accepted")
			}
		})
	}
}
