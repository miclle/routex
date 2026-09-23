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
	if err := compileRuntimeLimits(data); err != nil {
		t.Fatal(err)
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

func projectRuntimeFixture(t *testing.T) (*Service, *runtimeData, string, string) {
	t.Helper()
	s, data, personalBearer := runtimeFixture(t, "http://127.0.0.1")
	projectBearer := "rxp_" + strings.Repeat("p", 43)
	data.Users = append(data.Users, entity.User{ID: "usr_creator", Disabled: true})
	data.Models = append(data.Models,
		entity.Model{ID: "mdl_disabled", Status: "disabled"},
		entity.Model{ID: "mdl_ungranted", Status: "active"},
		entity.Model{ID: "mdl_outside_scope", Status: "active"},
	)
	data.ProjectData = &projectRuntimeData{
		Projects: []entity.Project{{ID: "prj_one", Status: entity.ResourceActive, CreatorID: "usr_creator"}},
		Managers: []entity.ProjectManager{{ProjectID: "prj_one", UserID: "usr_one"}},
		Keys:     []entity.ProjectKey{{ID: "pky_one", ProjectID: "prj_one", CreatorID: "usr_creator", TokenHash: secret.SHA256Hex(projectBearer), Status: entity.KeyActive}},
		Scopes: []entity.ProjectKeyModel{
			{KeyID: "pky_one", ModelID: "mdl_one"},
			{KeyID: "pky_one", ModelID: "mdl_disabled"},
			{KeyID: "pky_one", ModelID: "mdl_ungranted"},
		},
		Grants: []entity.ProjectModelGrant{
			{ProjectID: "prj_one", ModelID: "mdl_one"},
			{ProjectID: "prj_one", ModelID: "mdl_disabled"},
			{ProjectID: "prj_one", ModelID: "mdl_outside_scope"},
		},
	}
	if err := compileRuntimeLimits(data); err != nil {
		t.Fatal(err)
	}
	s.runtime.auth.Store(buildRuntimeAuthorization(data, time.Now().Add(time.Minute)))
	return s, data, projectBearer, personalBearer
}

func TestProjectRuntimeAuthorizationOwnershipAndIntersection(t *testing.T) {
	s, _, projectBearer, personalBearer := projectRuntimeFixture(t)
	// Runtime lookup has no database handle and needs no external secret store.
	s.secrets = nil
	key, err := s.AuthenticateAPIKey(context.Background(), projectBearer)
	if err != nil || key.ProjectID != "prj_one" || key.Key.UserID != "" || len(key.ModelIDs) != 1 || key.ModelIDs[0] != "mdl_one" {
		t.Fatal("project authorization did not preserve ownership and explicit scope intersection")
	}
	personal, err := s.AuthenticateAPIKey(context.Background(), personalBearer)
	if err != nil || personal.ProjectID != "" || personal.Key.UserID != "usr_one" {
		t.Fatal("project-key support changed personal ownership")
	}
	models, err := s.GatewayModels(context.Background(), projectBearer)
	if err != nil || len(models) != 1 || models[0].ID != "public-model" {
		t.Fatal("project model listing did not use the memory snapshot")
	}
	// Neither the creator nor a manager is the project's key owner. The current
	// manager lifecycle is evaluated by the project authorization publication.
	s.InvalidateRuntimeUser("usr_creator")
	s.InvalidateRuntimeUser("usr_one")
	if _, err := s.AuthenticateAPIKey(context.Background(), projectBearer); err != nil {
		t.Fatal("a user tombstone was incorrectly treated as project ownership")
	}
	if _, err := s.AuthenticateAPIKey(context.Background(), personalBearer); !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatal("personal owner tombstone was bypassed")
	}
}

func TestProjectRuntimeLifecycleAndManagerChanges(t *testing.T) {
	for _, kind := range []string{"disabled", "archived", "no_manager", "disabled_manager", "disabled_key", "pending_key", "revoked_key", "expired_key", "grant_removed", "model_tombstone", "key_tombstone", "project_tombstone", "expired_lease"} {
		t.Run(kind, func(t *testing.T) {
			s, data, bearer, personalBearer := projectRuntimeFixture(t)
			expired := time.Now().Add(-time.Second)
			until := time.Now().Add(time.Minute)
			switch kind {
			case "disabled":
				data.ProjectData.Projects[0].Status = entity.ResourceDisabled
			case "archived":
				data.ProjectData.Projects[0].Status = entity.ResourceArchived
			case "no_manager":
				data.ProjectData.Managers = nil
			case "disabled_manager":
				data.Users[0].Disabled = true
			case "disabled_key":
				data.ProjectData.Keys[0].Status = entity.KeyDisabled
			case "pending_key":
				data.ProjectData.Keys[0].Status = entity.KeyPending
			case "revoked_key":
				data.ProjectData.Keys[0].Status = entity.KeyRevoked
			case "expired_key":
				data.ProjectData.Keys[0].ExpiresAt = &expired
			case "grant_removed":
				data.ProjectData.Grants = nil
			case "model_tombstone":
				s.InvalidateRuntimeModel("mdl_one")
			case "key_tombstone":
				s.InvalidateRuntimeKey("pky_one")
			case "project_tombstone":
				s.InvalidateRuntimeProject("prj_one")
			case "expired_lease":
				until = expired
			}
			s.runtime.auth.Store(buildRuntimeAuthorization(data, until))
			key, err := s.AuthenticateAPIKey(context.Background(), bearer)
			switch kind {
			case "grant_removed", "model_tombstone":
				if err != nil || len(key.ModelIDs) != 0 {
					t.Fatal("project model reduction did not remove inference access")
				}
			case "expired_lease":
				if !errors.Is(err, runtimeUnavailable) {
					t.Fatal("project key bypassed authorization lease expiry")
				}
			default:
				if !errors.Is(err, apperrors.ErrUnauthorized) {
					t.Fatal("unavailable project or key remained authorized")
				}
			}
			if kind == "project_tombstone" {
				if _, err := s.AuthenticateAPIKey(context.Background(), personalBearer); err != nil {
					t.Fatal("project tombstone revoked an unrelated personal key")
				}
			}
		})
	}
	s, data, bearer, _ := projectRuntimeFixture(t)
	data.Users[0].Disabled = true
	data.Users = append(data.Users, entity.User{ID: "usr_new_manager"})
	data.ProjectData.Managers = []entity.ProjectManager{{ProjectID: "prj_one", UserID: "usr_new_manager"}}
	s.runtime.auth.Store(buildRuntimeAuthorization(data, time.Now().Add(time.Minute)))
	if _, err := s.AuthenticateAPIKey(context.Background(), bearer); err != nil {
		t.Fatal("replacing the current manager unexpectedly revoked project-owned keys")
	}
}

func TestProjectRuntimeTombstoneGenerations(t *testing.T) {
	s, data, bearer, _ := projectRuntimeFixture(t)
	prior := s.runtime.epoch.Load()
	s.InvalidateRuntimeProject("prj_one")
	clearRuntimeTombstones(&s.runtime.deniedProjects, prior)
	if _, err := s.AuthenticateAPIKey(context.Background(), bearer); !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatal("stale publication erased newer project invalidation")
	}
	data.ProjectData.Projects[0].Status = entity.ResourceDisabled
	s.runtime.auth.Store(buildRuntimeAuthorization(data, time.Now().Add(time.Minute)))
	clearRuntimeTombstones(&s.runtime.deniedProjects, s.runtime.epoch.Load())
	if _, err := s.AuthenticateAPIKey(context.Background(), bearer); !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatal("fresh authorization restored a disabled project after clearing its tombstone")
	}
}

func TestRuntimeBearerKindsMatchKeyOwnership(t *testing.T) {
	s, _, projectBearer, personalBearer := projectRuntimeFixture(t)
	for _, invalid := range []string{"", "rxp_", "rx_", "rxp_" + strings.Repeat("p", 42), "rx_" + strings.Repeat("a", 44)} {
		if _, err := s.AuthenticateAPIKey(context.Background(), invalid); !errors.Is(err, apperrors.ErrUnauthorized) {
			t.Fatal("malformed bearer accepted")
		}
	}
	auth := s.runtime.auth.Load()
	project := auth.Keys[secret.SHA256Hex(projectBearer)]
	personal := auth.Keys[secret.SHA256Hex(personalBearer)]
	auth.Keys[secret.SHA256Hex(projectBearer)] = personal
	auth.Keys[secret.SHA256Hex(personalBearer)] = project
	for _, bearer := range []string{projectBearer, personalBearer} {
		if _, err := s.AuthenticateAPIKey(context.Background(), bearer); !errors.Is(err, apperrors.ErrUnauthorized) {
			t.Fatal("bearer kind accepted mismatched ownership metadata")
		}
	}
}
