package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
)

func azureIdentityFixture() (entity.ProviderCredential, entity.ProviderConnection, entity.ProviderModel) {
	birth := time.Date(2026, 10, 7, 0, 0, 0, 123456000, time.UTC)
	version := "2024-10-21"
	return entity.ProviderCredential{ID: "crd_one", ConnectionID: "con_one", CreatedAt: birth, StorageSource: "inline", Ciphertext: "encrypted", VerificationStatus: "verified", Enabled: true}, entity.ProviderConnection{ID: "con_one", Adapter: entity.AdapterAzureOpenAIClassic, APIVersion: &version, Protocol: entity.ProtocolOpenAIChat, BaseURL: "https://example.invalid", CreatedAt: birth}, entity.ProviderModel{ID: "pmd_one", ConnectionID: "con_one", UpstreamName: "deployment-A", CreatedAt: birth}
}
func TestAzureAttestationLogicalIdentityAndNoDiscoveryInheritance(t *testing.T) {
	c, connection, pm := azureIdentityFixture()
	identity := deploymentAttestationIdentity(c, connection, pm)
	if identity == "" {
		t.Fatal("no identity")
	}
	originals := []entity.CredentialModelAccess{{CredentialID: c.ID, ProviderModelID: pm.ID}}
	if got := deploymentCoverageProjection([]entity.ProviderCredential{c}, []entity.ProviderConnection{connection}, []entity.ProviderModel{pm}, originals, nil); len(got) != 0 {
		t.Fatal("discovery incorrectly attested deployment")
	}
	rows := []entity.CredentialDeploymentAttestation{{CredentialID: c.ID, ProviderModelID: pm.ID, Identity: identity}}
	projection := deploymentCoverageProjection([]entity.ProviderCredential{c}, []entity.ProviderConnection{connection}, []entity.ProviderModel{pm}, originals, rows)
	if !reflect.DeepEqual(projection, originals) {
		t.Fatal("attestation not projected")
	}
	c.Ciphertext = "root-rewrapped"
	c.Name = "Renamed"
	c.Priority = 2
	c.Enabled = false
	c.VerifiedAt = new(time.Time)
	if deploymentAttestationIdentity(c, connection, pm) != identity {
		t.Fatal("mutable/root facts changed logical identity")
	}
	for name, change := range map[string]func(*entity.ProviderCredential, *entity.ProviderConnection, *entity.ProviderModel){
		"replacement": func(c *entity.ProviderCredential, _ *entity.ProviderConnection, _ *entity.ProviderModel) {
			c.ID = "crd_replacement"
		},
		"credential_birth": func(c *entity.ProviderCredential, _ *entity.ProviderConnection, _ *entity.ProviderModel) {
			c.CreatedAt = c.CreatedAt.Add(time.Second)
		},
		"connection_provider": func(_ *entity.ProviderCredential, c *entity.ProviderConnection, _ *entity.ProviderModel) {
			c.ProviderID = "prv_other"
		},
		"connection_birth": func(_ *entity.ProviderCredential, c *entity.ProviderConnection, _ *entity.ProviderModel) {
			c.CreatedAt = c.CreatedAt.Add(time.Second)
		},
		"model_birth": func(_ *entity.ProviderCredential, _ *entity.ProviderConnection, m *entity.ProviderModel) {
			m.CreatedAt = m.CreatedAt.Add(time.Second)
		},
		"deployment": func(_ *entity.ProviderCredential, _ *entity.ProviderConnection, m *entity.ProviderModel) {
			m.UpstreamName = "deployment-B"
		},
		"endpoint": func(_ *entity.ProviderCredential, c *entity.ProviderConnection, _ *entity.ProviderModel) {
			c.BaseURL = "https://other.invalid"
		},
		"version": func(_ *entity.ProviderCredential, c *entity.ProviderConnection, _ *entity.ProviderModel) {
			v := "2024-10-21-preview"
			c.APIVersion = &v
		},
		"adapter": func(_ *entity.ProviderCredential, c *entity.ProviderConnection, _ *entity.ProviderModel) {
			c.Adapter = entity.AdapterNative
			c.APIVersion = nil
		},
	} {
		t.Run(name, func(t *testing.T) {
			a, b, d := c, connection, pm
			change(&a, &b, &d)
			if deploymentAttestationIdentity(a, b, d) == identity {
				t.Fatal("identity drift allowed")
			}
			if name != "adapter" && len(deploymentCoverageProjection([]entity.ProviderCredential{a}, []entity.ProviderConnection{b}, []entity.ProviderModel{d}, nil, rows)) != 0 {
				t.Fatal("stale row authorized")
			}
		})
	}
	if !reflect.DeepEqual(originals, []entity.CredentialModelAccess{{CredentialID: "crd_one", ProviderModelID: "pmd_one"}}) {
		t.Fatal("discovery mutated")
	}
}
func TestAzureCoverageStrictCompleteIntent(t *testing.T) {
	for _, raw := range []string{`{}`, `{"provider_model_ids":null,"reason":"r"}`, `{"provider_model_ids":[],"reason":" r"}`, `{"provider_model_ids":[],"reason":null}`, `{"provider_model_ids":["pmd_one","pmd_one"],"reason":"r"}`, `{"provider_model_ids":[null],"reason":"r"}`, `{"provider_model_ids":["PMD_one"],"reason":"r"}`, `{"provider_model_ids":[],"reason":"r","extra":true}`, `{"provider_model_ids":[],"reason":"r","reason":"x"}`, `{"provider_model_ids":[],"reason":"r\n"}`, `{"provider_model_ids":[],"reason":"\ud800"}`} {
		var input DeploymentCoverageInput
		if json.Unmarshal([]byte(raw), &input) == nil {
			t.Fatalf("invalid complete input accepted: %s", raw)
		}
	}
	for _, raw := range []string{`{"provider_model_ids":[],"reason":"Revoke explicitly"}`, `{"provider_model_ids":["pmd_two","pmd_one"],"reason":"Attest exact deployments"}`} {
		var input DeploymentCoverageInput
		if err := json.Unmarshal([]byte(raw), &input); err != nil {
			t.Fatal(err)
		}
	}
	var input DeploymentCoverageInput
	if json.Unmarshal([]byte(`{"provider_model_ids":[],"reason":"`+strings.Repeat("r", 1025)+`"}`), &input) == nil {
		t.Fatal("reason overflow")
	}
}
func azureRuntimeFixture(t *testing.T, endpoint string) (*Service, *runtimeData, string) {
	t.Helper()
	svc, data, bearer := runtimeFixture(t, endpoint)
	_, connection, pm := azureIdentityFixture()
	connection.BaseURL = endpoint
	connection.Enabled = true
	connection.ProviderID = "prv_one"
	connection.Name = "Azure"
	data.Connections[0] = connection
	data.ProviderModels[0] = pm
	data.Credentials[0].CreatedAt = pm.CreatedAt
	data.Credentials[0].StorageSource = "inline"
	data.Access = nil
	data.Attestations = []entity.CredentialDeploymentAttestation{{CredentialID: data.Credentials[0].ID, ProviderModelID: pm.ID, Identity: deploymentAttestationIdentity(data.Credentials[0], connection, pm)}}
	svc.runtime.auth.Store(buildRuntimeAuthorization(data, time.Now().Add(time.Minute)))
	routes, err := svc.buildRuntimeRoutes(data)
	if err != nil {
		t.Fatal(err)
	}
	svc.runtime.routes.Store(&runtimeRoutes{ID: "cfg_azure", Models: routes, PublishedAt: time.Now()})
	return svc, data, bearer
}
func TestAzureProductionGatewayTransportAndRevokedPreparedCoverage(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/openai/deployments/deployment-A/chat/completions" || r.URL.Query().Get("api-version") != "2024-10-21" || r.Header.Get("api-key") != "test-upstream-secret" || r.Header.Get("Authorization") != "" {
			t.Error("production Azure transport mismatch")
		}
		raw, _ := io.ReadAll(r.Body)
		var body map[string]json.RawMessage
		if json.Unmarshal(raw, &body) != nil || body["model"] != nil {
			t.Error("model body selector leaked")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"model":"deployment-A","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}}`)
	}))
	defer server.Close()
	svc, data, bearer := azureRuntimeFixture(t, server.URL)
	defer svc.upstream.CloseIdleConnections()
	result, err := svc.GatewayChat(context.Background(), bearer, []byte(`{"model":"public-model","messages":[{"role":"user","content":"hello"}]}`), "req_azure")
	if err != nil || result == nil || result.Response == nil {
		t.Fatal(err)
	}
	_ = result.Response.Body.Close()
	if calls.Load() != 1 || result.CredentialID != "crd_one" || result.ProviderModelID != "pmd_one" || result.ConnectionID != "con_one" || result.SnapshotID != "cfg_azure" {
		t.Fatal("dispatch or attribution")
	}
	oldRoutes := svc.runtime.routes.Load()
	data.Attestations = nil
	data.Access = nil
	svc.runtime.auth.Store(buildRuntimeAuthorization(data, time.Now().Add(time.Minute)))
	if svc.runtime.routes.Load() != oldRoutes {
		t.Fatal("test replaced prepared routes")
	}
	_, err = svc.GatewayChat(context.Background(), bearer, []byte(`{"model":"public-model","messages":[{"role":"user","content":"hello"}]}`), "req_after_revoke")
	var unavailable *GatewayError
	if !errors.As(err, &unavailable) || unavailable.Status != http.StatusServiceUnavailable || unavailable.Code != "upstream_unavailable" || calls.Load() != 1 {
		t.Fatal("revoked coverage reused old prepared route")
	}
}

func TestAzureCoverageGenerationAndRetainedVaultIdentity(t *testing.T) {
	c, connection, pm := azureIdentityFixture()
	original := deploymentAttestationIdentity(c, connection, pm)
	revision := credentialRuntimeRevision(c)
	digest0, e := runtimeDigest(&runtimeData{Credentials: []entity.ProviderCredential{c}})
	if e != nil {
		t.Fatal(e)
	}
	c.CoverageRevision = 1
	if deploymentAttestationIdentity(c, connection, pm) != original || credentialRuntimeRevision(c) == revision {
		t.Fatal("coverage generation changed secret identity or failed to change publication")
	}
	c.CoverageRevision = 2
	digest2, e := runtimeDigest(&runtimeData{Credentials: []entity.ProviderCredential{c}})
	if e != nil || digest2 == digest0 {
		t.Fatal("empty ABA restored runtime/root publication digest")
	}
	if credentialRuntimeRevision(c) == revision {
		t.Fatal("empty set ABA restored publication revision")
	}
	c.StorageSource = "vault"
	c.Ciphertext = ""
	c.VaultReference = &entity.CredentialVaultReference{CredentialID: c.ID, CredentialBirth: c.CreatedAt, ReferenceID: strings.Repeat("a", 32), ExpectedMarkerSHA256: strings.Repeat("b", 64), DescriptorSHA256: strings.Repeat("c", 64), IntegrationID: "vlt_00000000000000000000000000", IntegrationBirth: c.CreatedAt, RevisionID: "vlr_00000000000000000000000000", ReaderGeneration: "vag_reader", SourceContext: "current-proof", ReaderCiphertext: "encrypted-original", ReaderMethod: "token", PhysicalObject: rootHash("azure-fixture-object")}
	identity := deploymentAttestationIdentity(c, connection, pm)
	if identity == "" {
		t.Fatal("valid retained reference unavailable")
	}
	c.VaultReference.ReaderCiphertext = "rewrapped"
	c.VaultReference.SourceContext = "new-current-proof"
	if deploymentAttestationIdentity(c, connection, pm) != identity {
		t.Fatal("root rewrap retargeted retained identity")
	}
	c.VaultReference.ReaderGeneration = "vag_other"
	if deploymentAttestationIdentity(c, connection, pm) == identity {
		t.Fatal("new auth generation inherited attestation")
	}
}

func TestAzureGatewayRedirectNeverForwardsCredential(t *testing.T) {
	for _, status := range []int{301, 302, 303, 307, 308} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var calls, forwarded atomic.Int32
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				forwarded.Add(1)
				if r.Header.Get("api-key") != "" {
					t.Error("credential forwarded")
				}
			}))
			defer target.Close()
			remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.Header().Set("Location", target.URL+"/steal")
				w.WriteHeader(status)
			}))
			defer remote.Close()
			svc, _, bearer := azureRuntimeFixture(t, remote.URL)
			defer svc.upstream.CloseIdleConnections()
			result, err := svc.GatewayChat(context.Background(), bearer, []byte(`{"model":"public-model","messages":[{"role":"user","content":"hello"}]}`), "req_azure_redirect")
			if err == nil || calls.Load() != 1 || forwarded.Load() != 0 || result == nil || len(result.Attempts) != 1 || result.Attempts[0].CredentialID != "crd_one" || result.Attempts[0].SnapshotID != "cfg_azure" {
				t.Fatalf("production redirect: error=%t calls=%d forwarded=%d result=%t attempts=%d", err != nil, calls.Load(), forwarded.Load(), result != nil, func() int {
					if result == nil {
						return -1
					}
					return len(result.Attempts)
				}())
			}
		})
	}
}

func TestAzureCoverageReviewRejectsABAAndOnlyReconcilesImmediateIntent(t *testing.T) {
	identity := strings.Repeat("a", 64)
	etag := func(d string) string { return identity + "." + strings.Repeat(d, 64) }
	empty := DeploymentCoverageInput{ProviderModelIDs: []string{}, Reason: "Withdraw"}
	nonempty := DeploymentCoverageInput{ProviderModelIDs: []string{"pmd_one"}, Reason: "Attest"}
	original := DeploymentCoverageRecord{ETag: etag("0"), ProviderModels: []DeploymentCoverageModel{{ID: "pmd_one"}}}
	credential := entity.ProviderCredential{}
	if !coverageReviewAuthorized(credential, original, original.ETag, empty) {
		t.Fatal("fresh exact review rejected")
	}
	first := original
	first.ETag = etag("1")
	first.ProviderModels = append([]DeploymentCoverageModel(nil), original.ProviderModels...)
	first.ProviderModels[0].Attested = true
	credential.CoverageRevision = 1
	credential.CoverageReviewETag = original.ETag
	credential.CoverageIntentSHA256 = coverageIntentDigest(nonempty)
	if !coverageReviewAuthorized(credential, first, original.ETag, nonempty) {
		t.Fatal("immediate uncertain current-value reconciliation rejected")
	}

	withdrawn := original
	withdrawn.ETag = etag("2")
	credential.CoverageRevision = 2
	credential.CoverageReviewETag = first.ETag
	credential.CoverageIntentSHA256 = coverageIntentDigest(empty)
	if !coverageReviewAuthorized(credential, withdrawn, first.ETag, empty) {
		t.Fatal("uncertain withdrawal retry rejected")
	}
	if coverageReviewAuthorized(credential, withdrawn, original.ETag, empty) {
		t.Fatal("empty/nonempty/empty accepted old empty review")
	}
	if coverageReviewAuthorized(credential, withdrawn, original.ETag, nonempty) {
		t.Fatal("uncertain revoked coverage resurrected")
	}
	if !coverageReviewAuthorized(credential, withdrawn, withdrawn.ETag, empty) || credential.CoverageRevision != 2 {
		t.Fatal("fresh unchanged reconciliation changed generation")
	}
	reattested := first
	reattested.ETag = etag("3")
	credential.CoverageRevision = 3
	credential.CoverageReviewETag = withdrawn.ETag
	credential.CoverageIntentSHA256 = coverageIntentDigest(nonempty)
	if coverageReviewAuthorized(credential, reattested, original.ETag, nonempty) {
		t.Fatal("nonempty ABA accepted old uncertain intent")
	}
	changedReason := nonempty
	changedReason.Reason = "Other reason"
	if coverageReviewAuthorized(entity.ProviderCredential{CoverageRevision: 1, CoverageReviewETag: original.ETag, CoverageIntentSHA256: coverageIntentDigest(nonempty)}, first, original.ETag, changedReason) {
		t.Fatal("uncertain original intent was rewritten")
	}
	otherActor := reattested
	otherActor.ETag = strings.Repeat("b", 64) + "." + strings.Repeat("3", 64)
	if coverageReviewAuthorized(credential, otherActor, withdrawn.ETag, nonempty) {
		t.Fatal("prior actor intent reconciled")
	}
	for _, bad := range []entity.ProviderCredential{{CoverageRevision: -1}, {CoverageRevision: 0, CoverageReviewETag: original.ETag}, {CoverageRevision: 1}, {CoverageRevision: 1, CoverageReviewETag: original.ETag, CoverageIntentSHA256: "bad"}} {
		if validCoverageIntentState(bad) {
			t.Fatal("invalid retained review state accepted")
		}
	}
}

func TestAzureConnectionCreationValidatesOriginalOriginBeforeNormalization(t *testing.T) {
	version := "2024-10-21"
	svc := &Service{}
	for _, raw := range []string{"https://example.invalid//", "https://example.invalid///", "https://example.invalid/a/..", "https://example.invalid/./", "https://example.invalid/%2f", "https://example.invalid/%2e/", "https://example.invalid/\n", " https://example.invalid"} {
		_, e := svc.prepareConnectionMetadata("prv_one", CreateConnectionInput{Name: "Azure", BaseURL: raw, Protocol: entity.ProtocolOpenAIChat, Adapter: entity.AdapterAzureOpenAIClassic, APIVersion: &version})
		if e == nil {
			t.Fatal("invalid raw Azure origin normalized into valid creation")
		}
	}
	for _, raw := range []string{"https://example.invalid", "https://example.invalid/"} {
		c, e := svc.prepareConnectionMetadata("prv_one", CreateConnectionInput{Name: "Azure", BaseURL: raw, Protocol: entity.ProtocolOpenAIChat, Adapter: entity.AdapterAzureOpenAIClassic, APIVersion: &version})
		if e != nil || c.BaseURL != "https://example.invalid" {
			t.Fatal("valid explicit origin rejected")
		}
	}
}

func TestAzureCoveragePublicWhitelistAndLegacyCreationIntent(t *testing.T) {
	view := DeploymentCoverageRecord{ProviderModels: []DeploymentCoverageModel{}, VerifiedAt: nil}
	raw, e := json.Marshal(view)
	if e != nil {
		t.Fatal(e)
	}
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(raw, &fields)
	if len(fields) != 10 || string(fields["verified_at"]) != "null" || string(fields["provider_models"]) != "[]" {
		t.Fatal("coverage DTO shape")
	}
	result, _ := json.Marshal(DeploymentCoverageWriteResult{Coverage: view, RuntimeApplied: true})
	var clean map[string]json.RawMessage
	_ = json.Unmarshal(result, &clean)
	if len(clean) != 3 {
		t.Fatal("write whitelist")
	}
	c := entity.ProviderCredential{CoverageRevision: 5, CoverageReviewETag: strings.Repeat("a", 64) + "." + strings.Repeat("b", 64), CoverageIntentSHA256: strings.Repeat("c", 64)}
	encoded, _ := json.Marshal(c)
	if strings.Contains(string(encoded), "Coverage") || strings.Contains(string(encoded), "coverage_") {
		t.Fatal("private coverage checkpoint exposed")
	}
	intent := credentialCreationJSON(credentialCreationIntent{Connection: CreateConnectionInput{Name: "Native", Protocol: entity.ProtocolOpenAIChat, BaseURL: "https://example.invalid/v1"}})
	if strings.Contains(intent, "Adapter") || strings.Contains(intent, "APIVersion") {
		t.Fatal("legacy omitted intent serialization changed")
	}
	explicit := credentialCreationJSON(credentialCreationIntent{Connection: CreateConnectionInput{Name: "Native", Protocol: entity.ProtocolOpenAIChat, BaseURL: "https://example.invalid/v1", Adapter: entity.AdapterNative}})
	if intent == explicit {
		t.Fatal("explicit transport intent not retained")
	}
}
