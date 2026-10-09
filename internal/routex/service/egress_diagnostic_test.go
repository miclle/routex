package service

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
)

func TestEgressDiagnosticNativeRequestAdapters(t *testing.T) {
	version := "2024-10-21"
	for _, tc := range []struct {
		name, protocol, adapter, base, path, authHeader string
		version                                         *string
	}{
		{"chat", entity.ProtocolOpenAIChat, entity.AdapterNative, "https://example.com/v1", "/v1/models", "Authorization", nil},
		{"responses", entity.ProtocolOpenAIResponses, entity.AdapterNative, "https://example.com/v1", "/v1/models", "Authorization", nil},
		{"messages", entity.ProtocolAnthropicMessages, entity.AdapterNative, "https://example.com/v1", "/v1/models", "x-api-key", nil},
		{"gemini", entity.ProtocolGeminiGenerateContent, entity.AdapterNative, "https://example.com/v1beta", "/v1beta/models", "x-goog-api-key", nil},
		{"azure", entity.ProtocolOpenAIChat, entity.AdapterAzureOpenAIClassic, "https://example.com", "/openai/models", "api-key", &version},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &Service{}
			c := entity.ProviderConnection{BaseURL: tc.base, Protocol: tc.protocol, Adapter: tc.adapter, APIVersion: tc.version}
			request, err := s.egressDiagnosticRequest(context.Background(), c, "test-only-native-secret")
			if err != nil || request.Method != "GET" || request.URL.Path != tc.path || request.Header.Get("Accept") != "application/json" {
				t.Fatal("wrong native metadata request", err)
			}
			for _, header := range []string{"Authorization", "x-api-key", "x-goog-api-key", "api-key"} {
				want := ""
				if header == tc.authHeader {
					want = "test-only-native-secret"
					if header == "Authorization" {
						want = "Bearer " + want
					}
				}
				if request.Header.Get(header) != want {
					t.Fatal("incorrect native authentication", header)
				}
			}
			if (tc.name == "messages") != (request.Header.Get("anthropic-version") == "2023-06-01") {
				t.Fatal("incorrect Messages version")
			}
			wantQuery := ""
			if tc.name == "azure" {
				wantQuery = "api-version=" + version
			}
			if request.URL.RawQuery != wantQuery {
				t.Fatal("incorrect adapter query")
			}
		})
	}
}

func TestEgressAPICapturePreservesWriteOnlyAndPrioritySelection(t *testing.T) {
	s, roles, _, state, _ := connectionDiagnosticSQLService(t)
	state.credential.Enabled, state.credential.VerificationStatus = true, "verified"
	state.egressTestPermission = true
	roles.deny["providers.read"] = true
	beforeConnection, beforeCredential := state.base.row, state.credential
	captured, err := s.egressAPICapture(context.Background(), "usr_admin", "con_target", "")
	if err != nil || captured.Credential.ID != "crd_target" || captured.Source == "" {
		t.Fatal("write-authorized selected source missing", err)
	}
	current, err := s.egressAPICapture(context.Background(), "usr_admin", "con_target", captured.Credential.ID)
	if err != nil || !captured.same(current) {
		t.Fatal("unchanged selected source rejected", err)
	}
	foundOrder, foundExact := false, false
	for _, query := range roles.queries {
		if strings.Contains(query, `FROM "provider_credentials"`) {
			foundOrder = foundOrder || strings.Contains(query, "priority, created_at, id")
			foundExact = foundExact || strings.Contains(query, `"id"`)
		}
	}
	if !foundOrder || !foundExact || len(roles.writes) != 0 || len(roles.data.audits) != 0 ||
		!reflect.DeepEqual(state.base.row, beforeConnection) || !reflect.DeepEqual(state.credential, beforeCredential) {
		t.Fatal("selection order, exact reread or read-only contract changed")
	}
	for _, options := range state.readOptions {
		if !options.ReadOnly || options.Isolation != driver.IsolationLevel(sql.LevelRepeatableRead) {
			t.Fatal("mutable source capture", options)
		}
	}
}

func TestEgressAPICaptureRejectsUnsafeIdentityAndAuthority(t *testing.T) {
	for _, name := range []string{"egress_test", "providers_write", "pending_actor", "actor_alias", "connection_alias", "provider_alias", "credential_alias", "foreign_credential", "missing_credential", "missing_birth", "disabled", "unverified", "unknown_source", "empty_source", "adapter"} {
		t.Run(name, func(t *testing.T) {
			s, roles, control, state, _ := connectionDiagnosticSQLService(t)
			state.credential.Enabled, state.credential.VerificationStatus = true, "verified"
			state.egressTestPermission = true
			baseline, err := s.egressAPICapture(context.Background(), "usr_admin", "con_target", "crd_target")
			if err != nil || baseline.Connection.ID != "con_target" || baseline.Credential.ID != "crd_target" || baseline.Source == "" || len(roles.writes) != 0 || len(roles.data.audits) != 0 {
				t.Fatal("authorized exact negative-case baseline unavailable", name, err)
			}
			switch name {
			case "egress_test":
				roles.deny["egress.test"] = true
			case "providers_write":
				roles.deny["providers.write"] = true
			case "pending_actor":
				roleDefinitionSQLPending(roles, control)
			case "actor_alias":
				roles.actorAlias = true
			case "connection_alias":
				state.base.aliasConnection = true
			case "provider_alias":
				state.base.aliasProvider = true
			case "credential_alias":
				state.alias = true
			case "foreign_credential":
				state.credential.ConnectionID = "con_other"
			case "missing_credential":
				state.missing = true
			case "missing_birth":
				state.credential.CreatedAt = time.Time{}
			case "disabled":
				state.credential.Enabled = false
			case "unverified":
				state.credential.VerificationStatus = "pending"
			case "unknown_source":
				state.credential.StorageSource = "unknown"
			case "empty_source":
				state.credential.Ciphertext = ""
			case "adapter":
				state.base.row.Adapter = "unknown"
			}
			captured, err := s.egressAPICapture(context.Background(), "usr_admin", "con_target", "crd_target")
			if err == nil || captured.Source != "" || len(roles.writes) != 0 {
				t.Fatal("unsafe source admitted", name, err)
			}
		})
	}
}

func TestEgressAPICaptureRechecksSourceAdapterAndBirth(t *testing.T) {
	for _, name := range []string{"ciphertext", "credential_birth", "actor_birth", "provider_birth", "connection_birth", "revision", "adapter", "credential_alias", "write_revoked", "test_revoked"} {
		t.Run(name, func(t *testing.T) {
			s, roles, _, state, _ := connectionDiagnosticSQLService(t)
			state.credential.Enabled, state.credential.VerificationStatus = true, "verified"
			state.egressTestPermission = true
			captured, err := s.egressAPICapture(context.Background(), "usr_admin", "con_target", "")
			if err != nil || captured.Connection.ID != "con_target" || captured.Credential.ID != "crd_target" || captured.Source == "" || len(roles.writes) != 0 || len(roles.data.audits) != 0 {
				t.Fatal("authorized exact source-recheck baseline unavailable", name, err)
			}
			switch name {
			case "ciphertext":
				state.credential.Ciphertext = "changed-retained-source"
			case "credential_birth":
				state.credential.CreatedAt = state.credential.CreatedAt.Add(time.Microsecond)
			case "actor_birth":
				actor := roles.data.users["usr_admin"]
				actor.CreatedAt = actor.CreatedAt.Add(time.Microsecond)
				roles.data.users[actor.ID] = actor
			case "provider_birth":
				state.base.provider.CreatedAt = state.base.provider.CreatedAt.Add(time.Microsecond)
			case "connection_birth":
				state.base.row.CreatedAt = state.base.row.CreatedAt.Add(time.Microsecond)
			case "revision":
				state.base.row.ETag = "changed"
			case "adapter":
				version := "2024-10-21"
				state.base.row.Adapter = entity.AdapterAzureOpenAIClassic
				state.base.row.BaseURL = "https://example.com"
				state.base.row.APIVersion = &version
			case "credential_alias":
				state.alias = true
			case "write_revoked":
				roles.deny["providers.write"] = true
			case "test_revoked":
				roles.deny["egress.test"] = true
			}
			current, err := s.egressAPICapture(context.Background(), "usr_admin", "con_target", captured.Credential.ID)
			if err == nil && captured.same(current) {
				t.Fatal("obsolete source or adapter accepted", name)
			}
			if len(roles.writes) != 0 {
				t.Fatal("proof read changed domain state")
			}
		})
	}
}

func TestEgressDiagnosticVaultProofAndHolderLifetime(t *testing.T) {
	_, ref := finiteSourceFixture("https://vault.example.com")
	ref.SourceContext, ref.PhysicalObject = strings.Repeat("d", 64), rootHash("egress-diagnostic-object")
	credential := entity.ProviderCredential{ID: ref.CredentialID, ConnectionID: "con_target", CreatedAt: ref.CredentialBirth, StorageSource: "vault", VaultReference: &ref, Enabled: true, VerificationStatus: "verified"}
	captured := egressAPIProof{Connection: entity.ProviderConnection{ID: credential.ConnectionID}, Credential: credential, Review: ConnectionMetadataRecord{ETag: "exact-review"}, Source: credentialSourceProof(credential)}
	if captured.Source == "" {
		t.Fatal("invalid retained Vault fixture")
	}
	for _, name := range []string{"reference", "revision", "integration", "integration_birth", "reader_generation", "source_context"} {
		t.Run(name, func(t *testing.T) {
			current := captured
			changed := ref
			switch name {
			case "reference":
				changed.ReferenceID = strings.Repeat("e", 32)
			case "revision":
				changed.RevisionID = "vlr_00000000000000000000000001"
			case "integration":
				changed.IntegrationID = "vlt_00000000000000000000000001"
			case "integration_birth":
				changed.IntegrationBirth = changed.IntegrationBirth.Add(time.Microsecond)
			case "reader_generation":
				changed.ReaderGeneration = "replaced"
			case "source_context":
				changed.SourceContext = strings.Repeat("e", 64)
			}
			current.Credential.VaultReference = &changed
			current.Source = credentialSourceProof(current.Credential)
			if captured.same(current) {
				t.Fatal("Vault source replacement accepted", name)
			}
		})
	}
	s := &Service{}
	holder, err := s.acquireCredentialSource(credential)
	if err != nil || !holder.admitUse() {
		t.Fatal("source not held", err)
	}
	key, err := credentialHolderKey(credential)
	if err != nil || sourceHolderCount(t, s, key) != 1 {
		t.Fatal("missing source holder", err)
	}
	if s.credentialSources.closeOnly(ref.PhysicalObject) || holder.admitUse() {
		t.Fatal("active source falsely drained or admitted after denial")
	}
	holder.release()
	if sourceHolderCount(t, s, key) != 0 || !s.credentialSources.joined(ref.PhysicalObject) {
		t.Fatal("source holder did not join")
	}
	if _, err = s.acquireCredentialSource(credential); err == nil {
		t.Fatal("closed source reopened")
	}
}

func TestEgressDiagnosticCandidateProofRejectsAliasesAndRebirth(t *testing.T) {
	original := entity.Egress{ID: "egr_target", Kind: "socks5", Host: "127.0.0.1", Port: 1080, CreatedAt: time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC), ETag: "0"}
	for _, name := range []string{"identity", "birth", "revision", "host", "port", "auth", "generation", "enablement"} {
		t.Run(name, func(t *testing.T) {
			current := original
			switch name {
			case "identity":
				current.ID = "EGR_TARGET"
			case "birth":
				current.CreatedAt = current.CreatedAt.Add(time.Microsecond)
			case "revision":
				current.ETag = "new"
			case "host":
				current.Host = "127.0.0.2"
			case "port":
				current.Port++
			case "auth":
				current.AuthCiphertext = "changed"
			case "generation":
				current.SecretGeneration = "changed"
			case "enablement":
				current.Enabled = true
			}
			if egressDiagnosticSame(original, current) {
				t.Fatal("obsolete candidate accepted")
			}
		})
	}
	if !egressDiagnosticSame(original, original) {
		t.Fatal("disabled unchanged diagnostic candidate rejected")
	}
}

func TestEgressAPICaptureCancelledBeforeUse(t *testing.T) {
	s, roles, _, state, _ := connectionDiagnosticSQLService(t)
	state.credential.Enabled, state.credential.VerificationStatus = true, "verified"
	state.egressTestPermission = true
	baseline, err := s.egressAPICapture(context.Background(), "usr_admin", "con_target", "")
	if err != nil || baseline.Connection.ID != "con_target" || baseline.Credential.ID != "crd_target" || baseline.Source == "" || len(roles.writes) != 0 || len(roles.data.audits) != 0 {
		t.Fatal("authorized exact cancellation baseline unavailable", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.egressAPICapture(ctx, "usr_admin", "con_target", ""); err == nil {
		t.Fatal("cancelled source capture accepted")
	}
	if len(roles.writes) != 0 {
		t.Fatal("cancelled capture changed domain state")
	}
	if _, err := s.egressDiagnosticRequest(context.Background(), entity.ProviderConnection{BaseURL: "https://example.com", Protocol: entity.ProtocolOpenAIResponses, Adapter: entity.AdapterAzureOpenAIClassic}, "private"); err != apperrors.ErrBadRequest {
		t.Fatal("unsupported adapter protocol accepted", err)
	}
}
