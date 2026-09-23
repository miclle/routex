package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/secret"
	"github.com/miclle/routex/pkg/secretstore"
	"github.com/miclle/routex/pkg/upstream"
)

func runtimeFixture(t *testing.T, baseURL string) (*Service, *runtimeData, string) {
	t.Helper()
	store, err := secretstore.New(bytes.Repeat([]byte{31}, 32))
	if err != nil {
		t.Fatal(err)
	}
	ciphertext, err := store.Seal("crd_one", "test-upstream-secret")
	if err != nil {
		t.Fatal(err)
	}
	modelID := "mdl_one"
	bearer := "rx_" + strings.Repeat("a", 43)
	data := &runtimeData{
		Users:          []entity.User{{ID: "usr_one"}},
		Keys:           []entity.APIKey{{ID: "key_one", UserID: "usr_one", TokenHash: secret.SHA256Hex(bearer), Status: entity.KeyActive}},
		Scopes:         []entity.APIKeyModel{{KeyID: "key_one", ModelID: modelID}},
		Grants:         []entity.UserModelGrant{{UserID: "usr_one", ModelID: modelID}},
		Models:         []entity.Model{{ID: modelID, Status: "active", CreatedAt: time.Now().UTC()}},
		Names:          []entity.ModelName{{Name: "public-model", ModelID: modelID, CurrentModelID: &modelID}},
		Connections:    []entity.ProviderConnection{{ID: "con_one", ProviderID: "prv_one", BaseURL: baseURL, Protocol: entity.ProtocolOpenAIChat}},
		ProviderModels: []entity.ProviderModel{{ID: "pmd_one", ConnectionID: "con_one", UpstreamName: "provider-model"}},
		Credentials:    []entity.ProviderCredential{{ID: "crd_one", ConnectionID: "con_one", Ciphertext: ciphertext, Enabled: true, VerificationStatus: "verified"}},
		Access:         []entity.CredentialModelAccess{{CredentialID: "crd_one", ProviderModelID: "pmd_one"}},
		Bindings:       []entity.ModelProviderBinding{{ID: "bnd_one", ModelID: modelID, ProviderModelID: "pmd_one", Weight: 100}},
	}
	s := &Service{secrets: store, upstream: upstream.NewClient(true), allowPrivateUpstream: true, runtime: &gatewayRuntime{}}
	s.runtime.auth.Store(buildRuntimeAuthorization(data, time.Now().Add(time.Minute)))
	routes, err := s.buildRuntimeRoutes(data)
	if err != nil {
		t.Fatal(err)
	}
	s.runtime.routes.Store(&runtimeRoutes{ID: "cfg_test", Models: routes, PublishedAt: time.Now()})
	return s, data, bearer
}

func TestRuntimeHotPathWithoutDatabaseOrSecretStore(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-upstream-secret" {
			t.Error("wrong prepared credential")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"model":"provider-model","choices":[]}`)
	}))
	defer server.Close()
	s, _, bearer := runtimeFixture(t, server.URL+"/v1")
	// Neither a database handle nor a secret store exists on the hot path.
	s.secrets = nil
	models, err := s.GatewayModels(context.Background(), bearer)
	if err != nil || len(models) != 1 || models[0].ID != "public-model" {
		t.Fatalf("runtime models: %v", err)
	}
	result, err := s.GatewayChat(context.Background(), bearer, []byte(`{"model":"public-model","messages":[{"role":"user","content":"hello"}]}`), "req_test")
	if err != nil {
		t.Fatal(err)
	}
	if err := result.Response.Body.Close(); err != nil {
		t.Error(err)
	}
	s.upstream.CloseIdleConnections()
}

func TestRuntimeLeaseExpiryAndImmediateTombstones(t *testing.T) {
	for _, kind := range []string{"key", "user", "model", "credential"} {
		t.Run(kind, func(t *testing.T) {
			s, _, bearer := runtimeFixture(t, "http://127.0.0.1")
			switch kind {
			case "key":
				s.InvalidateRuntimeKey("key_one")
			case "user":
				s.InvalidateRuntimeUser("usr_one")
			case "model":
				s.InvalidateRuntimeModel("mdl_one")
			case "credential":
				s.InvalidateRuntimeCredential("crd_one")
			}
			key, err := s.AuthenticateAPIKey(context.Background(), bearer)
			switch kind {
			case "key", "user":
				if !errors.Is(err, apperrors.ErrUnauthorized) {
					t.Fatal("revocation was not immediate")
				}
			case "model":
				if err != nil || len(key.ModelIDs) != 0 {
					t.Fatal("grant reduction was not immediate")
				}
			case "credential":
				if _, _, err := s.runtimeRoute("mdl_one"); err == nil {
					t.Fatal("disabled credential remained usable")
				}
			}
		})
	}
	s, _, bearer := runtimeFixture(t, "http://127.0.0.1")
	auth := *s.runtime.auth.Load()
	auth.ValidUntil = time.Now().Add(-time.Second)
	s.runtime.auth.Store(&auth)
	if _, err := s.AuthenticateAPIKey(context.Background(), bearer); !errors.Is(err, runtimeUnavailable) {
		t.Fatal("expired authorization lease accepted")
	}
	if _, _, err := s.runtimeRoute("mdl_one"); err == nil {
		t.Fatal("expired lease routed a request")
	}
}

func TestRuntimeGenerationPreservesNewerRevocation(t *testing.T) {
	var tombstones sync.Map
	tombstones.Store("old", uint64(1))
	tombstones.Store("new", uint64(3))
	clearRuntimeTombstones(&tombstones, 2)
	if runtimeDenied(&tombstones, "old") || !runtimeDenied(&tombstones, "new") {
		t.Fatal("old publication cleared a newer revocation")
	}
}

func TestRuntimePreparationValidationAndCoverage(t *testing.T) {
	s, data, bearer := runtimeFixture(t, "http://127.0.0.1")
	old := s.runtime.routes.Load()
	data.Bindings[0].Weight = 50
	if _, err := s.buildRuntimeRoutes(data); err == nil {
		t.Fatal("invalid weights published")
	}
	if s.runtime.routes.Load() != old {
		t.Fatal("failed preparation replaced last valid snapshot")
	}
	data.Bindings[0].Weight = 100
	data.Credentials[0].Ciphertext = "tampered"
	if _, err := s.buildRuntimeRoutes(data); err == nil {
		t.Fatal("invalid credential published")
	}
	data.Credentials[0].Enabled = false
	s.runtime.auth.Store(buildRuntimeAuthorization(data, time.Now().Add(time.Minute)))
	if _, _, err := s.runtimeRoute("mdl_one"); err == nil {
		t.Fatal("last-valid routing resurrected a revoked credential")
	}
	data.Grants = nil
	s.runtime.auth.Store(buildRuntimeAuthorization(data, time.Now().Add(time.Minute)))
	key, err := s.AuthenticateAPIKey(context.Background(), bearer)
	if err != nil || len(key.ModelIDs) != 0 {
		t.Fatal("bad routes prevented current grant reduction")
	}
}

func TestRuntimeNamesExpiryAndCredentialAccessReduction(t *testing.T) {
	s, data, _ := runtimeFixture(t, "http://127.0.0.1")
	expired := time.Now().Add(-time.Second)
	data.Names = append(data.Names, entity.ModelName{Name: "expired", ModelID: "mdl_one", ExpiresAt: &expired})
	data.Access = nil
	s.runtime.auth.Store(buildRuntimeAuthorization(data, time.Now().Add(time.Minute)))
	if _, ok := s.runtimeModelName("expired"); ok {
		t.Fatal("expired alias accepted")
	}
	if _, _, err := s.runtimeRoute("mdl_one"); err == nil {
		t.Fatal("removed credential model access retained through an old route")
	}
}
