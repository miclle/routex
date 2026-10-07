package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/secretstore"
)

func credentialReadinessRuntimeFixture(t *testing.T) (*Service, entity.ProviderCredential) {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Microsecond)
	replacement := entity.ProviderCredential{
		ID:                 "crd_replacement",
		ConnectionID:       "con_one",
		Name:               "Replacement",
		Enabled:            true,
		VerificationStatus: "verified",
		CreatedAt:          now.Add(-time.Minute),
	}
	store, err := secretstore.New([]byte(strings.Repeat("k", 32)))
	if err != nil {
		t.Fatal(err)
	}
	replacement.Ciphertext, err = store.Seal(replacement.ID, "secret-must-not-leak")
	if err != nil {
		t.Fatal(err)
	}
	replacement.StorageSource = "inline"
	auth := buildRuntimeAuthorization(&runtimeData{
		Connections:    []entity.ProviderConnection{{ID: "con_one", ProviderID: "prv_one", Enabled: true, CreatedAt: now.Add(-time.Hour)}},
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
				ConnectionBirth: now.Add(-time.Hour),
				Client:          &http.Client{}, BindingID: "bnd_one", Weight: 100,
				ProviderID: "prv_one", ProviderModelID: "pmd_one", ConnectionID: "con_one",
				Protocol: entity.ProtocolOpenAIChat, UpstreamName: "native", EgressRevision: "transport",
			},
			Credentials: []runtimeCredential{{ID: replacement.ID, CipherHash: credentialSourceProof(replacement), Plaintext: "secret-must-not-leak"}},
		}}},
	})
	return &Service{runtime: runtime, secrets: store}, replacement
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

		{"connection_disabled", "route_unavailable", func(s *Service) {
			proof := s.runtime.auth.Load().Connections["con_one"]
			proof.Enabled = false
			s.runtime.auth.Load().Connections["con_one"] = proof
		}},
		{"connection_denied", "route_unavailable", func(s *Service) { s.runtime.deniedConnections.Store("con_one", uint64(1)) }},
		{"connection_birth_changed", "route_unavailable", func(s *Service) {
			proof := s.runtime.auth.Load().Connections["con_one"]
			proof.Birth = proof.Birth.Add(time.Second)
			s.runtime.auth.Load().Connections["con_one"] = proof
		}},
		{"connection_provider_changed", "route_unavailable", func(s *Service) {
			proof := s.runtime.auth.Load().Connections["con_one"]
			proof.ProviderID = "prv_other"
			s.runtime.auth.Load().Connections["con_one"] = proof
		}},
		{"connection_missing", "route_unavailable", func(s *Service) { delete(s.runtime.auth.Load().Connections, "con_one") }},
		{"transport_revision", "route_unavailable", func(s *Service) { s.runtime.auth.Load().ConnectionRevisions["con_one"] = "changed" }},
		{"nil_client", "route_unavailable", func(s *Service) { s.runtime.routes.Load().Models["mdl_one"][0].Route.Client = nil }},
	} {
		t.Run(test.name, func(t *testing.T) {
			svc, _ := credentialReadinessRuntimeFixture(t)
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
	svc, _ := credentialReadinessRuntimeFixture(t)
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

		{"connection_disabled", "route_unavailable", func(s *Service) {
			proof := s.runtime.auth.Load().Connections["con_one"]
			proof.Enabled = false
			s.runtime.auth.Load().Connections["con_one"] = proof
		}},
		{"connection_denied", "route_unavailable", func(s *Service) { s.runtime.deniedConnections.Store("con_one", uint64(1)) }},
		{"connection_birth_changed", "route_unavailable", func(s *Service) {
			proof := s.runtime.auth.Load().Connections["con_one"]
			proof.Birth = proof.Birth.Add(time.Second)
			s.runtime.auth.Load().Connections["con_one"] = proof
		}},
		{"connection_provider_changed", "route_unavailable", func(s *Service) {
			proof := s.runtime.auth.Load().Connections["con_one"]
			proof.ProviderID = "prv_other"
			s.runtime.auth.Load().Connections["con_one"] = proof
		}},
		{"connection_missing", "route_unavailable", func(s *Service) { delete(s.runtime.auth.Load().Connections, "con_one") }},
		{"health", "route_unavailable", func(s *Service) { s.markGatewayCredentialRejected("crd_replacement") }},
	} {
		t.Run(test.name, func(t *testing.T) {
			svc, _ := credentialReadinessRuntimeFixture(t)
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
	svc, _ := credentialReadinessRuntimeFixture(t)
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
	svc, replacement := credentialReadinessRuntimeFixture(t)
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
	if !valid.matches(replacement, capture, valid.AttemptID) ||
		valid.matches(replacement, capture, "att_newer") ||
		valid.matches(replacement, capture, "ATT_PROOF") ||
		valid.matches(replacement, capture, "") {
		t.Fatal("explicit proof silently switched or accepted a folded identity")
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

func TestCredentialRetirementPublicationPinPrecedesDatabaseBorrow(t *testing.T) {
	svc, _ := credentialReadinessRuntimeFixture(t)
	// The fixture has no database. A publisher crossing the pin before checking
	// its canceled context would attempt to borrow it and fail this test.
	release, err := svc.pinCredentialRetirementRuntime()
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if secondRelease, err := svc.pinCredentialRetirementRuntime(); err == nil || secondRelease != nil {
		t.Fatal("a second retirement bypassed the exclusive publication pin")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{})
	done := make(chan error, 1)
	go func() { close(started); done <- svc.RefreshRuntime(ctx) }()
	<-started
	select {
	case err := <-done:
		t.Fatalf("publisher bypassed the active retirement pin: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	cancel()
	release()
	select {
	case err := <-done:
		if !errors.Is(err, runtimeUnavailable) {
			t.Fatalf("canceled publisher reached database: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("publisher failed to finish after pin release")
	}
	// Release is idempotent so a deferred release remains safe before refresh.
	release()
}

func TestCredentialRetirementPublicationPinRejectsActivePublisher(t *testing.T) {
	svc, _ := credentialReadinessRuntimeFixture(t)
	svc.runtime.publication.RLock()
	defer svc.runtime.publication.RUnlock()
	if release, err := svc.pinCredentialRetirementRuntime(); err == nil || release != nil {
		t.Fatal("retirement borrowed state while publication was active")
	}
}

func TestCredentialRetirementApplicationRequiresDisabledAbsentSource(t *testing.T) {
	svc, replacement := credentialReadinessRuntimeFixture(t)
	source := entity.ProviderCredential{ID: "crd_source", ConnectionID: replacement.ConnectionID}
	replacement.ReplacesCredentialID = &source.ID
	capture, err := svc.captureCredentialRetirementRuntime(source.ConnectionID, source.ID, replacement.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := credentialRetirementRuntimeApplicationBlockers(source, replacement, capture); len(got) != 0 {
		t.Fatalf("disabled absent source was rejected: %v", got)
	}
	for _, sample := range []struct {
		name, blocker string
		change        func(*entity.ProviderCredential, *entity.ProviderCredential, *credentialRetirementRuntimeCapture)
	}{
		{"missing_source", "runtime_stale", func(source, _ *entity.ProviderCredential, _ *credentialRetirementRuntimeCapture) { source.ID = "" }},
		{"reenabled_source", "runtime_stale", func(source, _ *entity.ProviderCredential, _ *credentialRetirementRuntimeCapture) {
			source.Enabled = true
		}},
		{"wrong_lineage", "runtime_stale", func(_, replacement *entity.ProviderCredential, _ *credentialRetirementRuntimeCapture) {
			replacement.ReplacesCredentialID = nil
		}},
		{"wrong_connection", "runtime_stale", func(_, replacement *entity.ProviderCredential, _ *credentialRetirementRuntimeCapture) {
			replacement.ConnectionID = "con_other"
		}},
		{"disabled_replacement", "replacement_disabled", func(_, replacement *entity.ProviderCredential, _ *credentialRetirementRuntimeCapture) {
			replacement.Enabled = false
		}},
		{"unverified_replacement", "replacement_unverified", func(_, replacement *entity.ProviderCredential, _ *credentialRetirementRuntimeCapture) {
			replacement.VerificationStatus = "failed"
		}},
		{"source_still_candidate", "route_unavailable", func(_, _ *entity.ProviderCredential, capture *credentialRetirementRuntimeCapture) {
			capture.scope[0].SourceCandidate = true
		}},
	} {
		t.Run(sample.name, func(t *testing.T) {
			sourceCopy, replacementCopy, captureCopy := source, replacement, *capture
			captureCopy.scope = append([]credentialReadinessRoute(nil), capture.scope...)
			sample.change(&sourceCopy, &replacementCopy, &captureCopy)
			if blockers := credentialRetirementRuntimeApplicationBlockers(sourceCopy, replacementCopy, &captureCopy); !slices.Contains(blockers, sample.blocker) {
				t.Fatalf("application blockers %v, want %s", blockers, sample.blocker)
			}
		})
	}
}

func TestCredentialRetirementCaptureFindsSourceAfterReplacement(t *testing.T) {
	svc, _ := credentialReadinessRuntimeFixture(t)
	routes := svc.runtime.routes.Load()
	candidates := routes.Models["mdl_one"]
	candidates[0].Credentials = append(candidates[0].Credentials, runtimeCredential{ID: "crd_source"})
	routes.Models["mdl_one"] = candidates
	capture, err := svc.captureCredentialRetirementRuntime("con_one", "crd_source", "crd_replacement")
	if err != nil || len(capture.scope) != 1 || !capture.scope[0].SourceCandidate || !capture.scope[0].ReplacementCandidate {
		t.Fatalf("candidate membership depends on pool order: %+v %v", capture, err)
	}
}
