package service

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
)

func controllerDescriptorFixture() APIKeyDeliveryDescriptorView {
	suffix := "01arz3ndektsv4rrffq69g5fav"
	return APIKeyDeliveryDescriptorView{SchemaVersion: 1, KeyID: "key_" + suffix, ProfileID: "vap_" + suffix, PrincipalID: "vpb_" + suffix, SlotID: "kds_" + suffix, DescriptorRevision: 7, Endpoint: "https://vault.example.invalid", Namespace: "tenant/app", Mount: "kv", Path: "api-keys/vap_" + suffix + "/routex-api-key-key_" + suffix, DataField: "value", Version: 1, Method: "jwt", AuthMount: "workload", Role: "routex-api-key-vpb_" + suffix, Coordinator: "not_configured", ApplicationStatus: "unknown"}
}

func TestAPIKeyDeliveryControllerAcknowledgementTimestampStableAcrossDriverLocations(t *testing.T) {
	instant := time.Date(2026, 10, 14, 1, 0, 0, 123456000, time.UTC)
	ack := entity.APIKeyDeliveryControllerAcknowledgement{RequestID: "00000000-0000-4000-8000-000000000001", DescriptorRevision: 1, RecordedAt: instant}
	original, err := json.Marshal(apiKeyDeliveryControllerAcknowledgementView(ack))
	if err != nil {
		t.Fatal(err)
	}
	ack.RecordedAt = instant.In(time.FixedZone("driver-local", 8*60*60))
	replayed, err := json.Marshal(apiKeyDeliveryControllerAcknowledgementView(ack))
	if err != nil || string(original) != string(replayed) {
		t.Fatal("equivalent persisted report changed its wire timestamp")
	}
}

func TestAPIKeyDeliveryControllerAgentConfigurationExactFileAndVersion(t *testing.T) {
	d := controllerDescriptorFixture()
	v, err := apiKeyDeliveryAgentConfiguration(d)
	if err != nil {
		t.Fatal(err)
	}
	if v.SlotID != d.SlotID || v.KeyID != d.KeyID || v.DescriptorRevision != "7" || v.ConfigurationDigest != "" || v.JWTFile != apiKeyDeliveryControllerJWTFile || v.KeyFile != "/var/run/secrets/routex/api-keys/"+d.SlotID {
		t.Fatal("configuration identity changed")
	}
	for _, required := range []string{`perms = "0600"`, `backup = false`, `error_on_missing_key = true`, `exit_on_retry_failure = true`, `remove_jwt_after_reading = false`, "kv/data/" + d.Path + "?version=1", `mount_path = "auth/workload"`, "sprig_fail"} {
		if !strings.Contains(v.AgentConfiguration, required) {
			t.Fatal("missing required Agent contract", required)
		}
	}
	for _, forbidden := range []string{"sink {", "command =", "token =", "rxdctl_", "rx_live_", "version=2"} {
		if strings.Contains(v.AgentConfiguration, forbidden) {
			t.Fatal("unexpected Agent authority", forbidden)
		}
	}
	second, err := apiKeyDeliveryAgentConfiguration(d)
	if err != nil || second != v {
		t.Fatal("configuration renderer is not deterministic")
	}
}

func TestAPIKeyDeliveryControllerAgentConfigurationRejectsInjectionAndIdentitySwap(t *testing.T) {
	for name, mutate := range map[string]func(*APIKeyDeliveryDescriptorView){
		"endpoint_credentials":  func(d *APIKeyDeliveryDescriptorView) { d.Endpoint = "https://secret@vault.example.invalid" },
		"endpoint_percent":      func(d *APIKeyDeliveryDescriptorView) { d.Endpoint = "https://vault.example.invalid/%2e%2e" },
		"endpoint_template":     func(d *APIKeyDeliveryDescriptorView) { d.Endpoint = "https://vault.example.invalid/${token}" },
		"endpoint_double_slash": func(d *APIKeyDeliveryDescriptorView) { d.Endpoint = "https://vault.example.invalid//api" },
		"namespace_traversal":   func(d *APIKeyDeliveryDescriptorView) { d.Namespace = "a/../b" },
		"path_quote":            func(d *APIKeyDeliveryDescriptorView) { d.Path += `"` },
		"field_template":        func(d *APIKeyDeliveryDescriptorView) { d.DataField = "${key}" },
		"field_path":            func(d *APIKeyDeliveryDescriptorView) { d.DataField = "secret/value" },
		"auth_template":         func(d *APIKeyDeliveryDescriptorView) { d.AuthMount = "{{secret}}" },
		"role_mismatch":         func(d *APIKeyDeliveryDescriptorView) { d.Role = "other" },
		"duplicate_identity": func(d *APIKeyDeliveryDescriptorView) {
			d.ProfileID = d.SlotID
			d.Path = "keys/" + d.ProfileID + "/routex-api-key-" + d.KeyID
		},
		"revision_zero":     func(d *APIKeyDeliveryDescriptorView) { d.DescriptorRevision = 0 },
		"revision_overflow": func(d *APIKeyDeliveryDescriptorView) { d.DescriptorRevision = 1 << 63 },
		"version_latest":    func(d *APIKeyDeliveryDescriptorView) { d.Version = 0 },
		"method_token":      func(d *APIKeyDeliveryDescriptorView) { d.Method = "token" },
		"schema":            func(d *APIKeyDeliveryDescriptorView) { d.SchemaVersion = 2 },
	} {
		t.Run(name, func(t *testing.T) {
			d := controllerDescriptorFixture()
			mutate(&d)
			v, err := apiKeyDeliveryAgentConfiguration(d)
			if err == nil || v.AgentConfiguration != "" {
				t.Fatal("unsafe configuration accepted")
			}
		})
	}
}

func TestAPIKeyDeliveryControllerBearerIndependentCanonicalAndRedacted(t *testing.T) {
	bearer := "rxdctl_" + strings.Repeat("A", 43)
	digest, err := apiKeyDeliveryControllerBearerDigest(bearer)
	if err != nil || !personalModelETag(digest) || digest == bearer {
		t.Fatal("invalid digest-only credential")
	}
	for _, bad := range []string{"", strings.TrimPrefix(bearer, "rxdctl_"), "rx_live_" + strings.Repeat("A", 43), bearer + "=", bearer + " ", "Bearer " + bearer, "rxdctl_" + strings.Repeat("A", 42) + "B"} {
		if _, err := apiKeyDeliveryControllerBearerDigest(bad); err == nil {
			t.Fatal("noncanonical or unrelated credential accepted")
		}
	}
	result := APIKeyDeliveryControllerEnrollmentResult{Bearer: bearer, Created: true}
	raw, err := json.Marshal(result)
	if err != nil || strings.Contains(string(raw), bearer) || strings.Contains(fmt.Sprintf("%+v %#v", result, result), bearer) {
		t.Fatal("one-time credential leaked through generic serialization")
	}
}

func TestAPIKeyDeliveryControllerScopeAndAssignmentRequireEveryBirth(t *testing.T) {
	birth := time.Date(2026, 10, 14, 1, 0, 0, 0, time.UTC)
	f := apiKeyDeliveryControllerConfigurationFacts{Owner: apiKeyDeliveryOwner{Kind: "personal", ID: "usr_exact", Birth: birth, Digest: strings.Repeat("a", 64)}, SlotBirth: birth, ProfileID: "vap_exact", ProfileBirth: birth, PrincipalID: "vpb_exact", PrincipalBirth: birth, BindingID: "kvb_exact", BindingBirth: birth, KeyBirth: birth, View: APIKeyDeliveryControllerConfiguration{SlotID: "kds_exact", KeyID: "key_exact", DescriptorRevision: "7", ConfigurationDigest: strings.Repeat("b", 64)}}
	g := entity.APIKeyDeliveryControllerGrant{OwnerKind: f.Owner.Kind, OwnerID: f.Owner.ID, OwnerBirth: birth, OwnerDigest: f.Owner.Digest, SlotID: f.View.SlotID, SlotBirth: birth, ProfileID: f.ProfileID, ProfileBirth: birth, PrincipalID: f.PrincipalID, PrincipalBirth: birth, ReviewedBindingID: f.BindingID, ReviewedBindingBirth: birth, ReviewedKeyID: f.View.KeyID, ReviewedKeyBirth: birth, ReviewedDescriptorRevision: 7, ConfigurationDigest: f.View.ConfigurationDigest}
	if !apiKeyDeliveryControllerAssigned(g, f) {
		t.Fatal("exact assignment rejected")
	}
	for name, mutate := range map[string]func(*entity.APIKeyDeliveryControllerGrant){
		"owner": func(g *entity.APIKeyDeliveryControllerGrant) { g.OwnerID += "x" }, "owner_kind": func(g *entity.APIKeyDeliveryControllerGrant) { g.OwnerKind = "project" }, "owner_birth": func(g *entity.APIKeyDeliveryControllerGrant) { g.OwnerBirth = g.OwnerBirth.Add(time.Microsecond) }, "owner_digest": func(g *entity.APIKeyDeliveryControllerGrant) { g.OwnerDigest = strings.Repeat("c", 64) },
		"slot": func(g *entity.APIKeyDeliveryControllerGrant) { g.SlotID += "x" }, "slot_birth": func(g *entity.APIKeyDeliveryControllerGrant) { g.SlotBirth = g.SlotBirth.Add(time.Microsecond) }, "profile": func(g *entity.APIKeyDeliveryControllerGrant) { g.ProfileID += "x" }, "profile_birth": func(g *entity.APIKeyDeliveryControllerGrant) { g.ProfileBirth = g.ProfileBirth.Add(time.Microsecond) }, "principal": func(g *entity.APIKeyDeliveryControllerGrant) { g.PrincipalID += "x" }, "principal_birth": func(g *entity.APIKeyDeliveryControllerGrant) {
			g.PrincipalBirth = g.PrincipalBirth.Add(time.Microsecond)
		},
		"binding": func(g *entity.APIKeyDeliveryControllerGrant) { g.ReviewedBindingID += "x" }, "binding_birth": func(g *entity.APIKeyDeliveryControllerGrant) {
			g.ReviewedBindingBirth = g.ReviewedBindingBirth.Add(time.Microsecond)
		}, "key": func(g *entity.APIKeyDeliveryControllerGrant) { g.ReviewedKeyID += "x" }, "key_birth": func(g *entity.APIKeyDeliveryControllerGrant) {
			g.ReviewedKeyBirth = g.ReviewedKeyBirth.Add(time.Microsecond)
		}, "revision": func(g *entity.APIKeyDeliveryControllerGrant) { g.ReviewedDescriptorRevision++ }, "digest": func(g *entity.APIKeyDeliveryControllerGrant) { g.ConfigurationDigest = strings.Repeat("d", 64) },
	} {
		t.Run(name, func(t *testing.T) {
			v := g
			mutate(&v)
			if apiKeyDeliveryControllerAssigned(v, f) {
				t.Fatal("stale assignment accepted")
			}
		})
	}
}

func TestAPIKeyDeliveryControllerLifetimeAndAdvisoryAcknowledgement(t *testing.T) {
	now := time.Date(2026, 10, 14, 1, 0, 0, 0, time.UTC)
	g := entity.APIKeyDeliveryControllerGrant{CreatedAt: now, ExpiresAt: now.Add(time.Hour)}
	if !apiKeyDeliveryControllerLive(g, now) || apiKeyDeliveryControllerLive(g, g.ExpiresAt) || apiKeyDeliveryControllerLive(g, now.Add(-time.Microsecond)) {
		t.Fatal("invalid grant lifetime boundary")
	}
	g.RevokedAt = &now
	if apiKeyDeliveryControllerLive(g, now) {
		t.Fatal("revoked grant admitted")
	}
	g.RevokedAt = nil
	g.ExpiresAt = now.Add(apiKeyDeliveryControllerMaxLifetime + time.Microsecond)
	if apiKeyDeliveryControllerLive(g, now) {
		t.Fatal("unbounded lifetime admitted")
	}
	in := APIKeyDeliveryControllerAcknowledgementInput{RequestID: "00000000-0000-4000-8000-000000000001", DescriptorRevision: "7", ConfigurationDigest: strings.Repeat("a", 64), AgentState: "running", OutputState: "written"}
	if !apiKeyDeliveryControllerAcknowledgementValid(in) {
		t.Fatal("valid advisory report rejected")
	}
	for _, bad := range []string{"completed", "applied", "retired", "success", ""} {
		v := in
		v.OutputState = bad
		if apiKeyDeliveryControllerAcknowledgementValid(v) {
			t.Fatal("stronger proof state admitted")
		}
		v = in
		v.AgentState = bad
		if apiKeyDeliveryControllerAcknowledgementValid(v) {
			t.Fatal("invalid Agent state admitted")
		}
	}
	for _, bad := range []string{"0", "07", "+7", "1e1", "9223372036854775808"} {
		v := in
		v.DescriptorRevision = bad
		if apiKeyDeliveryControllerAcknowledgementValid(v) {
			t.Fatal("inexact revision admitted")
		}
	}
	v := apiKeyDeliveryControllerAcknowledgementView(entity.APIKeyDeliveryControllerAcknowledgement{AgentState: "running", OutputState: "written", DescriptorRevision: 7})
	if v.EvidenceKind != "controller_report" || v.OutputState != "written" {
		t.Fatal("controller report promoted")
	}
}
