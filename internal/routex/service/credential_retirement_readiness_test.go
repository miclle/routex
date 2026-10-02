package service

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
)

func retirementReadinessUnitPair() (entity.ProviderCredential, entity.ProviderCredential, *credentialRetirementRuntimeProjection) {
	source := entity.ProviderCredential{ID: "crd_source", ConnectionID: "con_review", Name: "Source", Enabled: true, Ciphertext: "source-private-envelope"}
	replacement := entity.ProviderCredential{ID: "crd_replacement", ConnectionID: source.ConnectionID, ReplacesCredentialID: &source.ID, Name: "Replacement", Enabled: true, VerificationStatus: "verified", Ciphertext: "replacement-private-envelope"}
	projection := &credentialRetirementRuntimeProjection{SnapshotID: "cfg_review", SourceDigest: "private-runtime-digest", ScopeDigest: "private-scope-digest", EligibleRouteCount: 1, Evidence: &credentialRetirementAttemptEvidence{AttemptID: "att_review", CompletedAt: time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)}}
	return source, replacement, projection
}

func TestCredentialRetirementReadinessTargetValidation(t *testing.T) {
	first, second := "crd_01arz3ndektsv4rrffq69g5fav", "crd_01arz3ndektsv4rrffq69g5faw"
	if !validCredentialRetirementPairIDs(first, second) {
		t.Fatal("canonical distinct credential IDs rejected")
	}
	for _, values := range [][2]string{{first, first}, {"", second}, {first, ""}, {"crd_short", second}, {strings.ToUpper(first), second}, {first, second + "/"}, {" " + first, second}} {
		if validCredentialRetirementPairIDs(values[0], values[1]) {
			t.Fatal("invalid retirement pair accepted")
		}
	}
}

func TestCredentialRetirementReadinessValidator(t *testing.T) {
	source, replacement, projection := retirementReadinessUnitPair()
	initial, err := credentialRetirementReadinessRecord(source, replacement, projection, nil)
	if err != nil || !initial.Eligible || initial.Evidence == nil || initial.SnapshotID == nil || len(initial.ETag) != 64 {
		t.Fatalf("valid readiness failed: %+v %v", initial, err)
	}
	for name, mutate := range map[string]func(*entity.ProviderCredential, *entity.ProviderCredential, *credentialRetirementRuntimeProjection){
		"source metadata":      func(s, _ *entity.ProviderCredential, _ *credentialRetirementRuntimeProjection) { s.Name = "Changed" },
		"replacement metadata": func(_, r *entity.ProviderCredential, _ *credentialRetirementRuntimeProjection) { r.Priority = 2 },
		"lineage": func(_, r *entity.ProviderCredential, _ *credentialRetirementRuntimeProjection) {
			other := "crd_other"
			r.ReplacesCredentialID = &other
		},
		"snapshot": func(_, _ *entity.ProviderCredential, p *credentialRetirementRuntimeProjection) {
			p.SnapshotID = "cfg_other"
		},
		"runtime digest": func(_, _ *entity.ProviderCredential, p *credentialRetirementRuntimeProjection) {
			p.SourceDigest = "other"
		},
		"scope": func(_, _ *entity.ProviderCredential, p *credentialRetirementRuntimeProjection) {
			p.ScopeDigest = "other"
		},
		"route count": func(_, _ *entity.ProviderCredential, p *credentialRetirementRuntimeProjection) {
			p.EligibleRouteCount = 2
		},
		"attempt": func(_, _ *entity.ProviderCredential, p *credentialRetirementRuntimeProjection) {
			p.Evidence.AttemptID = "att_other"
		},
		"proof time": func(_, _ *entity.ProviderCredential, p *credentialRetirementRuntimeProjection) {
			p.Evidence.CompletedAt = p.Evidence.CompletedAt.Add(time.Microsecond)
		},
	} {
		t.Run(name, func(t *testing.T) {
			s, r, p := retirementReadinessUnitPair()
			mutate(&s, &r, p)
			changed, err := credentialRetirementReadinessRecord(s, r, p, nil)
			if err != nil || changed.ETag == initial.ETag {
				t.Fatal("reviewed readiness change preserved its validator")
			}
		})
	}
	source.Ciphertext, replacement.Ciphertext = "other-private-source", "other-private-replacement"
	projection.Evidence.CompletedAt = projection.Evidence.CompletedAt.In(time.FixedZone("offset", 3600))
	unchanged, err := credentialRetirementReadinessRecord(source, replacement, projection, nil)
	if err != nil || unchanged.ETag != initial.ETag {
		t.Fatal("secret material or timestamp timezone directly changed readiness validator")
	}
	encoded, err := json.Marshal(initial)
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &wire); err != nil || len(wire) != 8 {
		t.Fatal("readiness wire DTO changed")
	}
	for _, forbidden := range []string{"private", "ciphertext", "request_id", "user_id", "key_id", "token", "usage", "price", "source_digest", "scope_digest"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("readiness exposed internal material %s", forbidden)
		}
	}
}

func TestCredentialRetirementReadinessBlockers(t *testing.T) {
	source, replacement, projection := retirementReadinessUnitPair()
	source.Enabled, replacement.Enabled, replacement.VerificationStatus = false, false, "pending"
	projection.Blockers = []string{"coverage_missing", "coverage_missing", "runtime_stale"}
	blocked, err := credentialRetirementReadinessRecord(source, replacement, projection, []string{"runtime_stale"})
	if err != nil || blocked.Eligible || blocked.Evidence != nil || blocked.SnapshotID != nil || strings.Join(blocked.Blockers, ",") != "source_disabled,replacement_disabled,replacement_unverified,runtime_stale,coverage_missing" {
		t.Fatalf("blockers are not bounded/sorted or stale evidence was exposed: %+v %v", blocked, err)
	}
	for _, count := range []int{-1, 257} {
		projection.EligibleRouteCount = count
		if _, err := credentialRetirementReadinessRecord(source, replacement, projection, nil); err == nil {
			t.Fatal("invalid route count accepted")
		}
	}
	projection.EligibleRouteCount = 1
	projection.Blockers = []string{"arbitrary upstream body"}
	if _, err := credentialRetirementReadinessRecord(source, replacement, projection, nil); err == nil {
		t.Fatal("arbitrary blocker exposed")
	}
	_, _, projection = retirementReadinessUnitPair()
	projection.Evidence, projection.SnapshotID, projection.EligibleRouteCount = nil, "", 0
	blocked, err = credentialRetirementReadinessRecord(source, replacement, projection, nil)
	if err != nil || blocked.Eligible || blocked.Evidence != nil || blocked.SnapshotID != nil || !strings.Contains(strings.Join(blocked.Blockers, ","), "evidence_missing") {
		t.Fatal("missing runtime/proof could appear eligible")
	}
}
