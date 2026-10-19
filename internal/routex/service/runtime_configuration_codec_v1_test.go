package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/limits"
)

func configurationTestIDV1(prefix string, ordinal int) string {
	return fmt.Sprintf("%s_%026d", prefix, ordinal)
}
func configurationTestPointerV1[T any](v T) *T { return &v }

func configurationFixtureDataV1() *runtimeData {
	birth := time.Date(2026, 10, 10, 1, 2, 3, 123456789, time.UTC)
	user, model, provider, connection, credential, pm, key, project, projectKey, team, egress := configurationTestIDV1("usr", 1), configurationTestIDV1("mdl", 1), configurationTestIDV1("prv", 1), configurationTestIDV1("con", 1), configurationTestIDV1("crd", 1), configurationTestIDV1("pmd", 1), configurationTestIDV1("key", 1), configurationTestIDV1("prj", 1), configurationTestIDV1("pky", 1), configurationTestIDV1("tea", 1), configurationTestIDV1("egr", 1)
	expiry := time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC)
	app := configurationTestIDV1("raa", 1)
	return &runtimeData{
		ApprovalApplications: []entity.RegistrationApprovalApplication{{ID: app, UserID: user, UserCreatedAt: birth, CreatedAt: birth, State: "approved", Revision: strings.Repeat("2", 64), DecidedAt: &birth, DecisionActorID: &user, DecisionReason: configurationTestPointerV1("reviewed")}},
		Users:                []entity.User{{ID: user, CreatedAt: birth, PersonalGrantRevision: strings.Repeat("1", 64), ApprovalApplicationID: &app, PasswordHash: "excluded-user-password-sentinel"}},
		Keys:                 []entity.APIKey{{ID: key, UserID: user, LifecycleRevision: "0", TokenHash: strings.Repeat("a", 64), Status: entity.KeyActive, DeliveryMode: "manual", CreatedAt: birth, ExpiresAt: &expiry}},
		Scopes:               []entity.APIKeyModel{{KeyID: key, ModelID: model}},
		Grants:               []entity.UserModelGrant{{UserID: user, ModelID: model, CreatedAt: birth, SourceRequestID: configurationTestPointerV1(configurationTestIDV1("mrr", 1))}},
		Models:               []entity.Model{{ID: model, Status: "active", CreatedAt: birth}},
		Names:                []entity.ModelName{{Name: "example", ModelID: model, CurrentModelID: nil, ExpiresAt: nil, CreatedAt: birth}},
		Providers:            []entity.Provider{{ID: provider, Enabled: true, ETag: "0", Name: "Provider", CreatedAt: birth}},
		Connections:          []entity.ProviderConnection{{ID: connection, ProviderID: provider, Enabled: true, ETag: "0", Name: "Connection", BaseURL: "https://upstream.example.test", Protocol: entity.ProtocolOpenAIChat, Adapter: entity.AdapterNative, TransportGeneration: "0", EgressMode: "proxy", EgressID: &egress, CreatedAt: birth}},
		Credentials:          []entity.ProviderCredential{{ID: credential, ConnectionID: connection, Enabled: true, Name: "Credential", Ciphertext: "excluded-credential-envelope-sentinel", StorageSource: "inline", Priority: 0, VerifiedTransportGeneration: "0", VerificationStatus: "verified", VerifiedAt: &birth, CoverageRevision: 1, CoverageIntentSHA256: strings.Repeat("3", 64), CreatedAt: birth}},
		ProviderModels:       []entity.ProviderModel{{ID: pm, ConnectionID: connection, UpstreamName: "model", ETag: "0", CapabilityTransportGeneration: "0", SupportsImageInput: true, SupportsPDFInput: true, CreatedAt: birth}},
		Access:               []entity.CredentialModelAccess{{CredentialID: credential, ProviderModelID: pm}},
		Attestations:         []entity.CredentialDeploymentAttestation{{CredentialID: credential, ProviderModelID: pm, Identity: strings.Repeat("4", 64)}},
		Bindings:             []entity.ModelProviderBinding{{ID: configurationTestIDV1("bnd", 1), ModelID: model, ProviderModelID: pm, Weight: 100, CreatedAt: birth}},
		Egresses:             []entity.Egress{{ID: egress, Name: "Proxy", Kind: "https", Host: "proxy.example.test", Port: 443, Enabled: true, ETag: "0", SecretGeneration: configurationTestIDV1("sec", 1), AuthCiphertext: "excluded-egress-envelope-sentinel", LastDiagnostic: "excluded-diagnostic-sentinel", CreatedAt: birth}},
		EgressSetting:        entity.EgressSetting{ID: 1, ETag: "0", DefaultEgressID: &egress},
		Limits:               []entity.ResourceLimit{{ScopeKind: "user", ScopeID: user, ETag: "0", Tokens5H: configurationTestPointerV1(int64(0)), Tokens7D: nil, MoneyMonth: configurationTestPointerV1("0.000000000000000001"), Currency: "USD", IPMode: "none", IPRangesJSON: "[]", TokensMonthBehavior: "stop", MoneyMonthBehavior: "alert_only"}},
		LimitPolicies:        map[string]limits.Policy{limitAccount("user", user): {Tokens5H: configurationTestPointerV1(int64(0)), Tokens7D: nil, MoneyMonth: configurationTestPointerV1("0.000000000000000001"), Currency: "USD", IPMode: "none", IPRanges: []string{}, TokensMonthBehavior: "stop", MoneyMonthBehavior: "alert_only"}},
		LimitRoots:           map[string]string{key: key, projectKey: projectKey},
		Quota:                &runtimeQuotaData{Setting: entity.QuotaSetting{ID: 1, ETag: "0", TimeZone: "UTC", AccountingStarted: true}, Bounds: map[string]entity.ReservationBound{pm: {ProviderModelID: pm, Protocol: entity.ProtocolOpenAIChat, TransportGeneration: "0", MaxInputTokens: 100, MaxOutputTokens: 50, ETag: "0", Evidence: "reviewed"}}, Created: map[string]time.Time{limitAccount("user", user): birth}, Revisions: map[string]string{limitAccount("user", user): "0"}, Currency: "USD"},
		ProjectData:          &projectRuntimeData{Projects: []entity.Project{{ID: project, Name: "Project", CreatorID: user, Status: entity.ResourceActive, CreatedAt: birth}}, Managers: []entity.ProjectManager{{ID: configurationTestIDV1("pmg", 1), ProjectID: project, UserID: user}}, Keys: []entity.ProjectKey{{ID: projectKey, ProjectID: project, CreatorID: user, TokenHash: strings.Repeat("b", 64), Status: entity.KeyActive, DeliveryMode: "manual", ExpiresAt: &expiry, CreatedAt: birth}}, Scopes: []entity.ProjectKeyModel{{KeyID: projectKey, ModelID: model}}, Grants: []entity.ProjectModelGrant{{ProjectID: project, ModelID: model}}},
		TeamSessionData:      &teamSessionRuntimeData{AuthenticationPolicy: entity.GovernanceSetting{AuthenticationPolicyRevision: strings.Repeat("0", 64)}, Teams: []entity.Team{{ID: team, Name: "Team", Status: entity.ResourceActive, CreatedAt: birth}}, Memberships: []entity.TeamMembership{{ID: configurationTestIDV1("tmm", 1), TeamID: team, UserID: user, Role: entity.TeamOwner, Status: entity.ResourceActive, JoinedAt: &birth}}, Grants: []entity.TeamModelGrant{{TeamID: team, ModelID: model, SourceRequestID: configurationTestPointerV1(configurationTestIDV1("trq", 1))}}, Sessions: []entity.Session{{ID: configurationTestIDV1("ses", 1), UserID: user, TokenHash: strings.Repeat("c", 64), PrimaryMethod: "local", CreatedAt: birth, ExpiresAt: expiry}}},
		Pricing:              &runtimePricingData{Setting: entity.PricingSetting{ID: 1, ETag: "0", PlatformCurrency: "USD"}, FX: []entity.PricingExchangeRate{{Currency: "USD", Rate: "1.000000000000000000"}}, Prices: []entity.ModelPrice{{ID: configurationTestIDV1("prc", 1), ProviderModelID: pm, ContextThreshold: 0, CreatedAt: birth}}, Rates: []entity.PriceRate{{ID: configurationTestIDV1("rat", 1), ModelPriceID: configurationTestIDV1("prc", 1), Metric: "INPUT_TOKEN", Tier: "base", Unit: "1M_TOKEN", Currency: "USD", Amount: "0.123456789012345678", Enabled: true}}},
	}
}

func configurationFixtureV1(t *testing.T) runtimeConfigurationV1 {
	t.Helper()
	data := configurationFixtureDataV1()
	sourceDigest, err := runtimeDigest(data)
	if err != nil {
		t.Fatal(err)
	}
	auth := buildRuntimeAuthorization(data, time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC))
	auth.SourceDigest = sourceDigest
	projection, err := runtimeAuthorizationProjection(t.Context(), auth)
	if err != nil {
		t.Fatal("production projection prerequisite", err)
	}
	result, err := freezeRuntimeConfigurationV1(data, sourceDigest, projection, strings.Repeat("e", 64))
	if err != nil {
		t.Fatal(err)
	}
	return result
}
func configurationEncodeV1(t *testing.T, document runtimeConfigurationV1) ([]byte, string) {
	t.Helper()
	raw, digest, err := encodeRuntimeConfigurationV1(t.Context(), document)
	if err != nil || len(digest) != 64 {
		t.Fatal("codec positive prerequisite", err)
	}
	return raw, digest
}
func configurationRejectV1(t *testing.T, raw []byte, digest string) {
	t.Helper()
	facts, err := reconstructRuntimeConfigurationV1(t.Context(), raw, digest)
	if err == nil || facts != nil || err.Error() != errRuntimeConfigurationCodecV1.Error() {
		t.Fatal("invalid source returned facts or unsafe error")
	}
}

func TestRuntimeConfigurationV1ColdRoundtrip(t *testing.T) {
	if file := os.Getenv("ROUTEX_TEST_CONFIG_CODEC_CHILD"); file != "" {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		facts, err := reconstructRuntimeConfigurationV1(t.Context(), raw, os.Getenv("ROUTEX_TEST_CONFIG_CODEC_DIGEST"))
		if err != nil {
			t.Fatal(err)
		}
		expected := configurationFixtureV1(t)
		expected.PricingObservationDigest = configurationPricingDigestV1(expected.PricingObservation)
		if !reflect.DeepEqual(expected, facts.Document) || len(facts.UnresolvedSecrets) != 2 {
			t.Fatal("cold complete-source reconstruction lost facts")
		}
		return
	}
	raw, digest := configurationEncodeV1(t, configurationFixtureV1(t))
	file := filepath.Join(t.TempDir(), "configuration-v1.json")
	if err := os.WriteFile(file, raw, 0600); err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestRuntimeConfigurationV1ColdRoundtrip$", "-test.count=1")
	command.Env = append(os.Environ(), "ROUTEX_TEST_CONFIG_CODEC_CHILD="+file, "ROUTEX_TEST_CONFIG_CODEC_DIGEST="+digest)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("cold child failed: %v\n%s", err, output)
	}
	for _, secret := range []string{"excluded-user-password-sentinel", "excluded-credential-envelope-sentinel", "excluded-egress-envelope-sentinel", "excluded-diagnostic-sentinel"} {
		if bytes.Contains(raw, []byte(secret)) {
			t.Fatal("secret or diagnostics retained")
		}
	}
}

func TestRuntimeConfigurationV1StrictCanonicalTamper(t *testing.T) {
	raw, digest := configurationEncodeV1(t, configurationFixtureV1(t))
	cases := map[string][]byte{
		"duplicate_field":                    bytes.Replace(raw, []byte(`"Format":`), []byte(`"Format":"routex.gateway-configuration.v1","Format":`), 1),
		"unknown_field":                      append(append([]byte{}, raw[:len(raw)-1]...), []byte(`,"Plaintext":"private-sentinel"}`)...),
		"missing_field":                      bytes.Replace(raw, []byte(`"NetworkPolicyDigest":"`+strings.Repeat("e", 64)+`",`), nil, 1),
		"whitespace":                         append([]byte("\n"), raw...),
		"trailing_document":                  append(append([]byte{}, raw...), []byte(`{}`)...),
		"invalid_codec":                      bytes.Replace(raw, []byte(runtimeConfigurationFormatV1), []byte("routex.gateway-configuration.v2"), 1),
		"invalid_pricing_observation_digest": bytes.Replace(raw, []byte(`"PricingObservationDigest":"`), []byte(`"PricingObservationDigest":"0`), 1),
	}
	for name, changed := range cases {
		t.Run(name, func(t *testing.T) { configurationRejectV1(t, changed, configurationContentDigestV1(changed)) })
	}
	configurationRejectV1(t, raw, strings.Repeat("f", 64))
	configurationRejectV1(t, raw, digest+" ")
}

func TestRuntimeConfigurationV1NullZeroDecimalAndGenerationFidelity(t *testing.T) {
	raw, digest := configurationEncodeV1(t, configurationFixtureV1(t))
	facts, err := reconstructRuntimeConfigurationV1(t.Context(), raw, digest)
	if err != nil {
		t.Fatal(err)
	}
	s := facts.Document.Source
	if s.Limits[0].Tokens5H == nil || *s.Limits[0].Tokens5H != 0 || s.Limits[0].Tokens7D != nil || *s.Limits[0].MoneyMonth != "0.000000000000000001" || facts.Document.PricingObservation.FX[0].Rate != "1.000000000000000000" || facts.Document.PricingObservation.Rates[0].Amount != "0.123456789012345678" {
		t.Fatal("null/zero/decimal source changed")
	}
	if s.Connections[0].TransportGeneration != "0" || s.ProviderModels[0].CapabilityTransportGeneration != "0" || s.Credentials[0].VerifiedTransportGeneration != "0" || s.Credentials[0].SourceProof == "" || s.Egresses[0].SecretGeneration != configurationTestIDV1("sec", 1) {
		t.Fatal("private transport/source facts lost")
	}
	if !s.Users[0].CreatedAt.Equal(configurationFixtureDataV1().Users[0].CreatedAt) {
		t.Fatal("nanosecond birth lost")
	}
	document := configurationFixtureV1(t)
	document.Source.Scopes = nil
	nilRaw, nilDigest := configurationEncodeV1(t, document)
	document.Source.Scopes = []runtimeConfigurationAPIKeyModelV1{}
	emptyRaw, emptyDigest := configurationEncodeV1(t, document)
	if nilDigest == emptyDigest || bytes.Equal(nilRaw, emptyRaw) {
		t.Fatal("nil and empty collection aliased")
	}
}

func TestRuntimeConfigurationV1EverySourceDomainChangesContent(t *testing.T) {
	_, baseline := configurationEncodeV1(t, configurationFixtureV1(t))
	changes := map[string]func(*runtimeConfigurationV1){
		"approval":       func(d *runtimeConfigurationV1) { d.Source.ApprovalApplications[0].Revision = strings.Repeat("9", 64) },
		"actor":          func(d *runtimeConfigurationV1) { d.Source.Users[0].Disabled = true },
		"personal_key":   func(d *runtimeConfigurationV1) { d.Source.Keys[0].LifecycleRevision = "1" },
		"key_scope":      func(d *runtimeConfigurationV1) { d.Source.Scopes = nil },
		"personal_grant": func(d *runtimeConfigurationV1) { d.Source.Grants[0].SourceRequestID = nil },
		"model":          func(d *runtimeConfigurationV1) { d.Source.Models[0].Status = "archived" },
		"name": func(d *runtimeConfigurationV1) {
			d.Source.Names[0].ExpiresAt = configurationTestPointerV1(time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC))
		},
		"provider": func(d *runtimeConfigurationV1) { d.Source.Providers[0].Enabled = false },
		"connection_transport": func(d *runtimeConfigurationV1) {
			d.Source.Connections[0].TransportGeneration = configurationTestIDV1("rev", 2)
		},
		"credential_generation": func(d *runtimeConfigurationV1) {
			d.Source.Credentials[0].VerifiedTransportGeneration = configurationTestIDV1("rev", 3)
		},
		"credential_source":      func(d *runtimeConfigurationV1) { d.Source.Credentials[0].SourceProof = strings.Repeat("8", 64) },
		"coverage":               func(d *runtimeConfigurationV1) { d.Source.Access = nil },
		"deployment_attestation": func(d *runtimeConfigurationV1) { d.Source.Attestations[0].Identity = strings.Repeat("9", 64) },
		"capability_generation": func(d *runtimeConfigurationV1) {
			d.Source.ProviderModels[0].CapabilityTransportGeneration = configurationTestIDV1("rev", 4)
		},
		"binding": func(d *runtimeConfigurationV1) { d.Source.Bindings[0].Weight = 0 },
		"egress_secret_generation": func(d *runtimeConfigurationV1) {
			d.Source.Egresses[0].SecretGeneration = configurationTestIDV1("sec", 2)
		},
		"network_default":        func(d *runtimeConfigurationV1) { d.Source.EgressSetting.DefaultEgressID = nil },
		"stored_limit_null_zero": func(d *runtimeConfigurationV1) { d.Source.Limits[0].Tokens5H = nil },
		"effective_limit": func(d *runtimeConfigurationV1) {
			for k, v := range d.Source.LimitPolicies {
				v.Tokens5H = nil
				d.Source.LimitPolicies[k] = v
			}
		},
		"rotation_root":         func(d *runtimeConfigurationV1) { d.Source.LimitRoots = nil },
		"accounting_activation": func(d *runtimeConfigurationV1) { d.Source.Quota.Setting.AccountingStarted = false },
		"reservation_generation": func(d *runtimeConfigurationV1) {
			for k, v := range d.Source.Quota.Bounds {
				v.TransportGeneration = configurationTestIDV1("rev", 5)
				d.Source.Quota.Bounds[k] = v
			}
		},
		"quota_birth": func(d *runtimeConfigurationV1) {
			for k, v := range d.Source.Quota.Created {
				d.Source.Quota.Created[k] = v.Add(time.Nanosecond)
			}
		},
		"project":         func(d *runtimeConfigurationV1) { d.Source.ProjectData.Projects[0].Status = "archived" },
		"project_manager": func(d *runtimeConfigurationV1) { d.Source.ProjectData.Managers = nil },
		"project_key":     func(d *runtimeConfigurationV1) { d.Source.ProjectData.Keys[0].Status = entity.KeyRevoked },
		"project_scope":   func(d *runtimeConfigurationV1) { d.Source.ProjectData.Scopes = nil },
		"project_grant":   func(d *runtimeConfigurationV1) { d.Source.ProjectData.Grants = nil },
		"authentication_policy": func(d *runtimeConfigurationV1) {
			d.Source.TeamSessionData.AuthenticationPolicy.Revision = strings.Repeat("9", 64)
		},
		"session_proof": func(d *runtimeConfigurationV1) {
			d.Source.TeamSessionData.Sessions[0].OIDCPolicyRevision = strings.Repeat("9", 64)
		},
		"team":                  func(d *runtimeConfigurationV1) { d.Source.TeamSessionData.Teams[0].Status = "archived" },
		"membership":            func(d *runtimeConfigurationV1) { d.Source.TeamSessionData.Memberships[0].Status = "archived" },
		"team_grant_provenance": func(d *runtimeConfigurationV1) { d.Source.TeamSessionData.Grants[0].SourceRequestID = nil },
		"pricing_observation":   func(d *runtimeConfigurationV1) { d.PricingObservation.Rates[0].Amount = "0.000000000000000001" },
		"network_bootstrap":     func(d *runtimeConfigurationV1) { d.NetworkPolicyDigest = strings.Repeat("9", 64) },
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			d := configurationFixtureV1(t)
			change(&d)
			_, digest := configurationEncodeV1(t, d)
			if digest == baseline {
				t.Fatal("source domain omitted from content digest")
			}
		})
	}
}

func TestRuntimeConfigurationV1MalformedSourceAndOrder(t *testing.T) {
	changes := map[string]func(*runtimeConfigurationV1){
		"duplicate_model": func(d *runtimeConfigurationV1) { d.Source.Models = append(d.Source.Models, d.Source.Models[0]) },
		"reversed_users": func(d *runtimeConfigurationV1) {
			u := d.Source.Users[0]
			u.ID = configurationTestIDV1("usr", 2)
			d.Source.Users = append([]runtimeConfigurationUserV1{u}, d.Source.Users...)
		},
		"null_birth": func(d *runtimeConfigurationV1) { d.Source.Credentials[0].CreatedAt = time.Time{} },
		"noncanonical_birth_zone": func(d *runtimeConfigurationV1) {
			d.Source.Users[0].CreatedAt = d.Source.Users[0].CreatedAt.In(time.FixedZone("offset", 3600))
		},
		"transport_generation": func(d *runtimeConfigurationV1) { d.Source.Connections[0].TransportGeneration = "rev_invalid" },
		"source_generation":    func(d *runtimeConfigurationV1) { d.Source.Egresses[0].SecretGeneration = "sec_invalid" },
		"invalid_reference": func(d *runtimeConfigurationV1) {
			d.Source.Bindings[0].ProviderModelID = "pmd_invalid"
		},
		"owner_alias":      func(d *runtimeConfigurationV1) { d.Source.Keys[0].UserID = strings.ToUpper(d.Source.Keys[0].UserID) },
		"empty_token_hash": func(d *runtimeConfigurationV1) { d.Source.Keys[0].TokenHash = "" },
		"decimal_exponent": func(d *runtimeConfigurationV1) { d.PricingObservation.Rates[0].Amount = "1e-18" },
		"decimal_sign":     func(d *runtimeConfigurationV1) { d.Source.Limits[0].MoneyMonth = configurationTestPointerV1("-1") },
		"negative_bound": func(d *runtimeConfigurationV1) {
			for k, v := range d.Source.Quota.Bounds {
				v.MaxInputTokens = -1
				d.Source.Quota.Bounds[k] = v
			}
		},
		"weight": func(d *runtimeConfigurationV1) { d.Source.Bindings[0].Weight = 101 },
		"inline_vault_mix": func(d *runtimeConfigurationV1) {
			d.Source.Credentials[0].VaultReference = &runtimeConfigurationCredentialVaultReferenceV1{}
		},
		"emergency_mfa_generation": func(d *runtimeConfigurationV1) {
			d.Source.TeamSessionData.Sessions[0].EmergencyMFAGeneration = "invalid"
		},
		"invalid_identity_provider": func(d *runtimeConfigurationV1) {
			d.Source.TeamSessionData.OIDCProvider = &runtimeConfigurationOIDCProviderV1{ID: "other"}
		},
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			d := configurationFixtureV1(t)
			change(&d)
			raw, digest, err := encodeRuntimeConfigurationV1(t.Context(), d)
			if err == nil || raw != nil || digest != "" {
				t.Fatal("malformed source encoded")
			}
		})
	}
}

func TestRuntimeConfigurationV1BoundsAndCancellation(t *testing.T) {
	raw, digest := configurationEncodeV1(t, configurationFixtureV1(t))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if facts, err := reconstructRuntimeConfigurationV1(ctx, raw, digest); err == nil || facts != nil {
		t.Fatal("cancelled reconstruction returned facts")
	}
	if raw, digest, err := encodeRuntimeConfigurationV1(ctx, configurationFixtureV1(t)); err == nil || raw != nil || digest != "" {
		t.Fatal("cancelled encoding succeeded")
	}
	//lint:ignore SA1012 This deliberately verifies rejection of a nil context.
	//nolint:staticcheck // Intentionally exercise nil-context rejection.
	if configurationJSONBoundV1(nil, raw) {
		t.Fatal("nil context accepted")
	}
	document := configurationFixtureV1(t)
	document.Source.Providers[0].Name = strings.Repeat("<", runtimeConfigurationMaxBytesV1/6)
	if raw, digest, err := encodeRuntimeConfigurationV1(t.Context(), document); err == nil || raw != nil || digest != "" {
		t.Fatal("escaped encoder byte overflow accepted")
	}
	oversized := bytes.Repeat([]byte(" "), runtimeConfigurationMaxBytesV1+1)
	configurationRejectV1(t, oversized, configurationContentDigestV1(oversized))
	entries := []byte("[" + strings.Repeat("0,", runtimeConfigurationMaxEntriesV1) + "0]")
	if configurationJSONBoundV1(t.Context(), entries) {
		t.Fatal("entry overflow accepted")
	}
	deeplyNested := []byte(strings.Repeat("[", 33) + "0" + strings.Repeat("]", 33))
	if configurationJSONBoundV1(t.Context(), deeplyNested) {
		t.Fatal("depth overflow accepted")
	}
}

func TestRuntimeConfigurationV1FreezeDetachedFromMutableSource(t *testing.T) {
	data := configurationFixtureDataV1()
	originalModel := data.Models[0]
	data.Models = append(data.Models, entity.Model{ID: configurationTestIDV1("mdl", 2), Status: "active", CreatedAt: originalModel.CreatedAt})
	data.Models[0], data.Models[1] = data.Models[1], data.Models[0]
	d, err := freezeRuntimeConfigurationV1(data, strings.Repeat("d", 64), strings.Repeat("f", 64), strings.Repeat("e", 64))
	if err != nil {
		t.Fatal(err)
	}
	raw, digest := configurationEncodeV1(t, d)
	*data.Limits[0].Tokens5H = 9
	*data.LimitPolicies[limitAccount("user", data.Users[0].ID)].MoneyMonth = "9"
	*data.Keys[0].ExpiresAt = time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC)
	data.Credentials[0].Name = "changed"
	data.Models[0].Status = "archived"
	data.Pricing.Rates[0].Amount = "9"
	again, newDigest := configurationEncodeV1(t, d)
	if !bytes.Equal(raw, again) || digest != newDigest || data.Models[0].ID != configurationTestIDV1("mdl", 2) {
		t.Fatal("freeze retains pointers or sorts live input")
	}
	facts, err := reconstructRuntimeConfigurationV1(t.Context(), raw, digest)
	if err != nil {
		t.Fatal(err)
	}
	facts.Document.Source.Keys[0].Name = "detached"
	if bytes.Contains(raw, []byte("detached")) || d.Source.Keys[0].Name == "detached" {
		t.Fatal("decode reused encoder source")
	}
}

func TestRuntimeConfigurationV1PricingHasSeparateObservedDigest(t *testing.T) {
	before := configurationFixtureV1(t)
	raw, digest := configurationEncodeV1(t, before)
	first, err := reconstructRuntimeConfigurationV1(t.Context(), raw, digest)
	if err != nil {
		t.Fatal(err)
	}
	before.Source.Users[0].Disabled = true
	next, nextDigest := configurationEncodeV1(t, before)
	second, err := reconstructRuntimeConfigurationV1(t.Context(), next, nextDigest)
	if err != nil {
		t.Fatal(err)
	}
	if digest == nextDigest || first.Document.PricingObservationDigest != second.Document.PricingObservationDigest {
		t.Fatal("pricing observation follows unrelated authority")
	}
	before.PricingObservation.Rates[0].Amount = "0.000000000000000001"
	next, nextDigest = configurationEncodeV1(t, before)
	third, err := reconstructRuntimeConfigurationV1(t.Context(), next, nextDigest)
	if err != nil {
		t.Fatal(err)
	}
	if third.Document.PricingObservationDigest == second.Document.PricingObservationDigest {
		t.Fatal("pricing observation omitted exact amount")
	}
	// Even rehashed outer bytes cannot disguise a changed sibling pricing digest.
	forged := third.Document
	forged.PricingObservationDigest = second.Document.PricingObservationDigest
	changed, err := json.Marshal(forged)
	if err != nil {
		t.Fatal(err)
	}
	configurationRejectV1(t, changed, configurationContentDigestV1(changed))
}

func TestRuntimeConfigurationV1VaultOpaqueDependencyFidelity(t *testing.T) {
	data := configurationFixtureDataV1()
	c := &data.Credentials[0]
	c.StorageSource = "vault"
	c.Ciphertext = ""
	c.VaultReference = &entity.CredentialVaultReference{CredentialID: c.ID, CredentialBirth: c.CreatedAt, ReferenceID: strings.Repeat("a", 32), ExpectedMarkerSHA256: strings.Repeat("b", 64), DescriptorSHA256: strings.Repeat("c", 64), IntegrationID: configurationTestIDV1("vlt", 1), IntegrationBirth: c.CreatedAt.Add(-time.Hour), RevisionID: configurationTestIDV1("vlr", 1), ReaderGeneration: configurationTestIDV1("vag", 1), SourceContext: strings.Repeat("d", 64), PhysicalObject: strings.Repeat("e", 64), ReaderMethod: "approle", ReaderCiphertext: "excluded-reader-envelope-sentinel"}
	expectedProof := credentialSourceProof(*c)
	if expectedProof == "" {
		t.Fatal("real opaque Vault proof prerequisite")
	}
	d, err := freezeRuntimeConfigurationV1(data, strings.Repeat("1", 64), strings.Repeat("2", 64), strings.Repeat("3", 64))
	if err != nil {
		t.Fatal(err)
	}
	raw, digest := configurationEncodeV1(t, d)
	if bytes.Contains(raw, []byte("excluded-reader-envelope-sentinel")) {
		t.Fatal("Vault reader material retained")
	}
	facts, err := reconstructRuntimeConfigurationV1(t.Context(), raw, digest)
	if err != nil {
		t.Fatal(err)
	}
	ref := facts.Document.Source.Credentials[0].VaultReference
	if ref == nil || ref.ReaderGeneration != c.VaultReference.ReaderGeneration || ref.SourceContext != c.VaultReference.SourceContext || ref.PhysicalObject != c.VaultReference.PhysicalObject || ref.ReaderMethod != "approle" || !ref.CredentialBirth.Equal(c.CreatedAt) || !ref.IntegrationBirth.Equal(c.VaultReference.IntegrationBirth) || facts.UnresolvedSecrets[0].SourceProof != expectedProof {
		t.Fatal("Vault immutable source identity lost")
	}
	for name, change := range map[string]func(*runtimeConfigurationV1){
		"credential_birth": func(d *runtimeConfigurationV1) {
			d.Source.Credentials[0].VaultReference.CredentialBirth = time.Time{}
		},
		"reference":   func(d *runtimeConfigurationV1) { d.Source.Credentials[0].VaultReference.ReferenceID = "invalid" },
		"integration": func(d *runtimeConfigurationV1) { d.Source.Credentials[0].VaultReference.IntegrationID = "invalid" },
		"revision":    func(d *runtimeConfigurationV1) { d.Source.Credentials[0].VaultReference.RevisionID = "invalid" },
		"physical_object": func(d *runtimeConfigurationV1) {
			d.Source.Credentials[0].VaultReference.PhysicalObject = "not-a-digest"
		},
		"reader_method": func(d *runtimeConfigurationV1) { d.Source.Credentials[0].VaultReference.ReaderMethod = "invalid" },
		"reader_generation": func(d *runtimeConfigurationV1) {
			d.Source.Credentials[0].VaultReference.ReaderGeneration = "with space"
		},
	} {
		t.Run(name, func(t *testing.T) {
			copy, err := reconstructRuntimeConfigurationV1(t.Context(), raw, digest)
			if err != nil {
				t.Fatal(err)
			}
			change(&copy.Document)
			if bytes, digest, err := encodeRuntimeConfigurationV1(t.Context(), copy.Document); err == nil || bytes != nil || digest != "" {
				t.Fatal("malformed opaque reference encoded")
			}
		})
	}
}

func TestRuntimeConfigurationV1AllIdentitySourceFidelity(t *testing.T) {
	data := configurationFixtureDataV1()
	d := data.TeamSessionData
	u := data.Users[0]
	config, policy := strings.Repeat("5", 64), strings.Repeat("6", 64)
	b := u.CreatedAt.Add(time.Minute)
	d.OIDCProvider = &entity.OIDCProvider{ID: "oidc", ConfigRevision: config, PolicyRevision: policy, Issuer: "https://oidc.example.test", Enabled: true, VerifiedConfigRevision: config, VerifiedBy: u.ID, VerifiedUserCreatedAt: &u.CreatedAt, VerifiedBindingID: configurationTestIDV1("oib", 1), VerifiedBindingCreatedAt: &b, AuthCiphertext: "excluded-oidc-secret"}
	d.OIDCBindings = []entity.OIDCBinding{{ID: configurationTestIDV1("oib", 1), UserID: u.ID, UserCreatedAt: u.CreatedAt, ConfigRevision: config, Subject: "subject", SubjectDigest: strings.Repeat("7", 64), CreatedAt: b}}
	d.OAuthProvider = &entity.OAuthProvider{ID: "oauth", ConfigRevision: config, PolicyRevision: policy, AuthorizationURL: "https://oauth.example.test/auth", TokenURL: "https://oauth.example.test/token", UserInfoURL: "https://oauth.example.test/user", Enabled: false, AuthCiphertext: "excluded-oauth-secret"}
	d.OAuthBindings = []entity.OAuthBinding{{ID: configurationTestIDV1("oab", 1), ProviderID: "oauth", SubjectKind: "string", UserID: u.ID, UserCreatedAt: u.CreatedAt, ConfigRevision: config, Subject: "subject", SubjectDigest: strings.Repeat("7", 64), CreatedAt: b}}
	d.LDAPProvider = &entity.LDAPProvider{ID: "ldap", ConfigRevision: config, PolicyRevision: policy, Endpoint: "ldaps://ldap.example.test:636", IdentityAttribute: "entryUUID", AuthCiphertext: "excluded-ldap-secret"}
	d.LDAPBindings = []entity.LDAPBinding{{ID: configurationTestIDV1("ldb", 1), ProviderID: "ldap", IdentityAttribute: "entryUUID", UserID: u.ID, UserCreatedAt: u.CreatedAt, ConfigRevision: config, Subject: "subject", SubjectDigest: strings.Repeat("7", 64), CreatedAt: b}}
	d.SAMLProvider = &entity.SAMLProvider{ID: "saml", ConfigRevision: config, PolicyRevision: policy, IDPIssuer: "https://saml.example.test"}
	d.SAMLBindings = []entity.SAMLBinding{{ID: configurationTestIDV1("smb", 1), ProviderID: "saml", Issuer: "https://saml.example.test", UserID: u.ID, UserCreatedAt: u.CreatedAt, ConfigRevision: config, Subject: "subject", SubjectDigest: strings.Repeat("7", 64), CreatedAt: b}}
	d.NamedIdentityProviders = map[string]*entity.NamedIdentityProvider{}
	for i, provider := range []string{"github", "google", "discord", "telegram"} {
		descriptor, ok := namedIdentityDescriptor(provider)
		if !ok {
			t.Fatal("fixed profile prerequisite")
		}
		d.NamedIdentityProviders[provider] = &entity.NamedIdentityProvider{ID: provider, ProfileID: descriptor.profile, IdentityIssuer: descriptor.issuer, ConfigRevision: config, PolicyRevision: policy, AuthCiphertext: "excluded-named-secret"}
		d.NamedIdentityBindings = append(d.NamedIdentityBindings, entity.NamedIdentityBinding{ID: configurationTestIDV1("nib", i+1), ProviderID: provider, ProfileID: descriptor.profile, IdentityIssuer: descriptor.issuer, SubjectKind: "string", UserID: u.ID, UserCreatedAt: u.CreatedAt, ConfigRevision: config, Subject: "subject", SubjectDigest: strings.Repeat("7", 64), CreatedAt: b})
	}
	d.AuthenticationPolicy.ForcedSSOEnabled = true
	d.AuthenticationPolicy.EmergencyAdminID = u.ID
	d.AuthenticationPolicy.EmergencyAdminCreatedAt = &u.CreatedAt
	d.EmergencyUsers = map[string]entity.User{u.ID: {ID: u.ID, Role: entity.RoleAdmin, CreatedAt: u.CreatedAt, PasswordHash: "excluded-emergency-password-hash"}}
	d.EmergencyMFA = map[string]entity.UserMFA{u.ID: {UserID: u.ID, Enabled: true, Generation: strings.Repeat("A", 43)}}
	d.Sessions[0].OIDCBindingID = d.OIDCBindings[0].ID
	d.Sessions[0].OIDCBindingCreatedAt = &b
	d.Sessions[0].OIDCConfigRevision = config
	d.Sessions[0].OIDCPolicyRevision = policy
	d.Sessions[0].OIDCUserCreatedAt = &u.CreatedAt
	doc, err := freezeRuntimeConfigurationV1(data, strings.Repeat("1", 64), strings.Repeat("2", 64), strings.Repeat("3", 64))
	if err != nil {
		t.Fatal(err)
	}
	raw, digest := configurationEncodeV1(t, doc)
	facts, err := reconstructRuntimeConfigurationV1(t.Context(), raw, digest)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(doc.Source.TeamSessionData, facts.Document.Source.TeamSessionData) {
		t.Fatal("identity policy/binding/Session source fidelity lost")
	}
	for _, secret := range []string{"excluded-oidc-secret", "excluded-oauth-secret", "excluded-ldap-secret", "excluded-named-secret", "excluded-emergency-password-hash"} {
		if bytes.Contains(raw, []byte(secret)) {
			t.Fatal("login material retained")
		}
	}
	if facts.Document.Source.TeamSessionData.EmergencyUsers[u.ID].PasswordDigest != configurationCipherProofV1("excluded-emergency-password-hash") {
		t.Fatal("emergency proof digest changed")
	}
}

// This uses the real primary-proof evaluator and Team Session authenticator.
// Decode is never connected to either runtime pointer: stale facts remain denied.
func configurationDeniedHistoryFixtureV1(t *testing.T, mode string) (*Service, *runtimeData, string) {
	t.Helper()
	data := configurationFixtureDataV1()
	d := data.TeamSessionData
	u := data.Users[0]
	config, policy := strings.Repeat("5", 64), strings.Repeat("6", 64)
	bindingBirth := u.CreatedAt.Add(time.Minute)
	bindingID := configurationTestIDV1("oib", 1)
	d.OIDCProvider = &entity.OIDCProvider{ID: "oidc", ConfigRevision: config, PolicyRevision: policy, Issuer: "https://oidc.example.test", Enabled: true, VerifiedConfigRevision: config, VerifiedBy: u.ID, VerifiedUserCreatedAt: &u.CreatedAt, VerifiedBindingID: bindingID, VerifiedBindingCreatedAt: &bindingBirth}
	binding := entity.OIDCBinding{ID: bindingID, UserID: u.ID, UserCreatedAt: u.CreatedAt, ConfigRevision: config, Subject: "subject", SubjectDigest: oidcSubjectDigest(d.OIDCProvider.Issuer, "subject"), CreatedAt: bindingBirth}
	cookie := strings.Repeat("Q", 43)
	session := &d.Sessions[0]
	session.TokenHash = configurationCipherProofV1(cookie)
	session.PrimaryMethod = "oidc"
	session.OIDCBindingID = bindingID
	session.OIDCBindingCreatedAt = &bindingBirth
	session.OIDCConfigRevision = config
	session.OIDCPolicyRevision = policy
	session.OIDCUserCreatedAt = &u.CreatedAt
	d.OIDCBindings = []entity.OIDCBinding{binding}
	if !oidcRuntimePrimary(*session, d.OIDCProvider, map[string]entity.OIDCBinding{binding.ID: binding}, map[string]entity.User{u.ID: u}) {
		t.Fatal("real current primary proof positive prerequisite")
	}
	switch mode {
	case "stale_user_birth":
		d.OIDCBindings[0].UserCreatedAt = u.CreatedAt.Add(-time.Nanosecond)
	case "absent_user":
		data.Users = nil
	default:
		t.Fatal("unknown denied fixture")
	}
	if oidcRuntimePrimary(*session, d.OIDCProvider, map[string]entity.OIDCBinding{binding.ID: d.OIDCBindings[0]}, func() map[string]entity.User {
		result := map[string]entity.User{}
		for _, u := range data.Users {
			result[u.ID] = u
		}
		return result
	}()) {
		t.Fatal("historical proof became eligible")
	}
	runtime := &gatewayRuntime{}
	runtime.auth.Store(buildRuntimeAuthorization(data, time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC)))
	return &Service{runtime: runtime}, data, cookie
}

func TestRuntimeConfigurationV1ColdDeniedHistory(t *testing.T) {
	if file := os.Getenv("ROUTEX_TEST_DENIED_CODEC_CHILD"); file != "" {
		mode := os.Getenv("ROUTEX_TEST_DENIED_CODEC_MODE")
		service, data, cookie := configurationDeniedHistoryFixtureV1(t, mode)
		before := service.runtime.auth.Load()
		_, err := service.RuntimeAuthenticateTeamSession(t.Context(), cookie, data.TeamSessionData.Teams[0].ID)
		requireTeamSessionError(t, err, 401, "invalid_session")
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		facts, err := reconstructRuntimeConfigurationV1(t.Context(), raw, os.Getenv("ROUTEX_TEST_DENIED_CODEC_DIGEST"))
		if err != nil {
			t.Fatal(err)
		}
		expected, err := freezeRuntimeConfigurationV1(data, strings.Repeat("1", 64), strings.Repeat("2", 64), strings.Repeat("3", 64))
		if err != nil {
			t.Fatal(err)
		}
		expected.PricingObservationDigest = configurationPricingDigestV1(expected.PricingObservation)
		if !reflect.DeepEqual(facts.Document, expected) || service.runtime.auth.Load() != before || service.runtime.routes.Load() != nil {
			t.Fatal("cold historical source lost facts or changed installed authority")
		}
		_, err = service.RuntimeAuthenticateTeamSession(t.Context(), cookie, data.TeamSessionData.Teams[0].ID)
		requireTeamSessionError(t, err, 401, "invalid_session")
		return
	}
	for _, mode := range []string{"stale_user_birth", "absent_user"} {
		t.Run(mode, func(t *testing.T) {
			_, data, _ := configurationDeniedHistoryFixtureV1(t, mode)
			document, err := freezeRuntimeConfigurationV1(data, strings.Repeat("1", 64), strings.Repeat("2", 64), strings.Repeat("3", 64))
			if err != nil {
				t.Fatal(err)
			}
			raw, digest := configurationEncodeV1(t, document)
			file := filepath.Join(t.TempDir(), "denied-history.json")
			if err := os.WriteFile(file, raw, 0600); err != nil {
				t.Fatal(err)
			}
			command := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestRuntimeConfigurationV1ColdDeniedHistory$", "-test.count=1")
			command.Env = append(os.Environ(), "ROUTEX_TEST_DENIED_CODEC_CHILD="+file, "ROUTEX_TEST_DENIED_CODEC_MODE="+mode, "ROUTEX_TEST_DENIED_CODEC_DIGEST="+digest)
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("denied historical cold child failed: %v\n%s", err, output)
			}
		})
	}
}

func TestRuntimeConfigurationV1StaleBirthAndUnavailableSourceFacts(t *testing.T) {
	document := configurationFixtureV1(t)
	birth := document.Source.Users[0].CreatedAt
	oldBirth := birth.Add(-time.Hour)
	document.Source.ApprovalApplications[0].UserCreatedAt = oldBirth
	policy := &document.Source.TeamSessionData.AuthenticationPolicy
	policy.Enforced = true
	policy.EmergencyAdminID = configurationTestIDV1("usr", 9)
	policy.EmergencyAdminCreatedAt = &oldBirth
	document.Source.TeamSessionData.EmergencyUsers = map[string]runtimeConfigurationEmergencyUserV1{policy.EmergencyAdminID: {ID: policy.EmergencyAdminID, CreatedAt: oldBirth, Enabled: false, IntrinsicAdministrator: true, PasswordDigest: strings.Repeat("a", 64)}}
	document.Source.TeamSessionData.EmergencyMFA = map[string]runtimeConfigurationEmergencyMFAV1{policy.EmergencyAdminID: {UserID: policy.EmergencyAdminID, Enabled: false, Generation: strings.Repeat("A", 43)}}
	c := &document.Source.Credentials[0]
	c.StorageSource = "vault"
	c.SourceProof = "" // Recorded unavailable reference does not become a ready source.
	c.VaultReference = &runtimeConfigurationCredentialVaultReferenceV1{CredentialID: c.ID, CredentialBirth: oldBirth, ReferenceID: strings.Repeat("a", 32), ExpectedMarkerSHA256: strings.Repeat("b", 64), DescriptorSHA256: strings.Repeat("c", 64), IntegrationID: configurationTestIDV1("vlt", 9), IntegrationBirth: oldBirth, RevisionID: configurationTestIDV1("vlr", 9), ReaderGeneration: "", ReaderMethod: "", PhysicalObject: "", SourceContext: ""}
	document.Source.Connections[0].ProviderID = configurationTestIDV1("prv", 9)
	document.Source.Bindings[0].ProviderModelID = configurationTestIDV1("pmd", 9)
	document.Source.ProjectData.Managers[0].UserID = configurationTestIDV1("usr", 9)
	raw, digest := configurationEncodeV1(t, document)
	facts, err := reconstructRuntimeConfigurationV1(t.Context(), raw, digest)
	if err != nil {
		t.Fatal(err)
	}
	document.PricingObservationDigest = configurationPricingDigestV1(document.PricingObservation)
	if !reflect.DeepEqual(facts.Document, document) || facts.UnresolvedSecrets[0].SourceProof != "" || !facts.Document.Source.Credentials[0].VaultReference.CredentialBirth.Equal(oldBirth) || !facts.Document.Source.ApprovalApplications[0].UserCreatedAt.Equal(oldBirth) {
		t.Fatal("stale/dangling source was backfilled or became eligible")
	}
}

func TestRuntimeConfigurationV1HistoricalOffboardingManagerIdentity(t *testing.T) {
	for _, prefix := range []string{"pmg", "pjm"} {
		t.Run(prefix, func(t *testing.T) {
			document := configurationFixtureV1(t)
			identity := configurationTestIDV1(prefix, 901)
			document.Source.ProjectData.Managers[0].ID = identity
			raw, digest := configurationEncodeV1(t, document)
			facts, err := reconstructRuntimeConfigurationV1(t.Context(), raw, digest)
			if err != nil || facts.Document.Source.ProjectData.Managers[0].ID != identity {
				t.Fatal("historical manager identity was rejected or rewritten", err)
			}
		})
	}
	for _, invalid := range []string{configurationTestIDV1("other", 901), strings.ToUpper(configurationTestIDV1("pjm", 901)), configurationTestIDV1("pjm", 901) + " "} {
		document := configurationFixtureV1(t)
		document.Source.ProjectData.Managers[0].ID = invalid
		if raw, _, err := encodeRuntimeConfigurationV1(t.Context(), document); err == nil || len(raw) != 0 {
			t.Fatal("foreign or aliased manager identity admitted")
		}
	}
}
