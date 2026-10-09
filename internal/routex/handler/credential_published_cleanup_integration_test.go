package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/secretstore"
	"gorm.io/gorm"
)

// Fresh focused execution can prove a joined published cleanup. Ordered full
// execution may retain offline/unproven generations; in that case this fixture
// proves the conservative zero-effect denial instead, without deleting history
// or manufacturing acknowledgments. Neither branch dispatches inference.
func testCredentialPublishedCleanupLifecycle(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx := context.Background()
	store, err := secretstore.New(bytes.Repeat([]byte{89}, 32))
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	values := map[string]map[string]string{}
	var reads, destroys atomic.Int32
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case "POST":
			if !strings.HasPrefix(r.URL.Path, "/v1/kv/data/published/") || r.Header.Get("X-Vault-Token") != "published-writer" {
				t.Error("wrong original CAS writer")
				w.WriteHeader(400)
				return
			}
			var body struct {
				Data    map[string]string `json:"data"`
				Options struct {
					CAS int `json:"cas"`
				} `json:"options"`
			}
			if json.NewDecoder(r.Body).Decode(&body) != nil || body.Options.CAS != 0 || len(body.Data) != 2 {
				t.Error("wrong original credential envelope")
				w.WriteHeader(400)
				return
			}
			mu.Lock()
			_, exists := values[r.URL.Path]
			if !exists {
				values[r.URL.Path] = body.Data
			}
			mu.Unlock()
			if exists {
				t.Error("original write replayed")
				w.WriteHeader(409)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"version": 1, "destroyed": false, "deletion_time": ""}})
		case "GET":
			reads.Add(1)
			if r.Header.Get("X-Vault-Token") != "published-reader" || r.URL.RawQuery != "version=1" {
				t.Error("wrong original reader/version")
			}
			mu.Lock()
			data, ok := values[r.URL.Path]
			mu.Unlock()
			if !ok {
				w.WriteHeader(404)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"data": data, "metadata": map[string]any{"version": 1, "destroyed": false, "deletion_time": ""}}})
		case "PUT":
			destroys.Add(1)
			if r.Header.Get("X-Vault-Token") != "published-cleanup" || !strings.HasPrefix(r.URL.Path, "/v1/kv/destroy/published/") {
				t.Error("wrong independent destroy authority")
			}
			var body struct {
				Versions []int `json:"versions"`
			}
			if json.NewDecoder(r.Body).Decode(&body) != nil || !reflect.DeepEqual(body.Versions, []int{1}) {
				t.Error("destroy broadened versions")
			}
			var claimed int64
			if err := db.Model(&entity.CredentialSourceDenial{}).Where("remote_request_id IS NOT NULL").Count(&claimed).Error; err != nil || claimed != 1 {
				t.Error("no permanent physical claim before SDK", err)
			}
			w.WriteHeader(204)
		default:
			t.Error("extra remote operation")
			w.WriteHeader(400)
		}
	}))
	defer stub.Close()
	svc, err := service.New(ctx, db, service.WithCredentialStorage(store), service.WithUpstreamPolicy(true))
	if err != nil {
		t.Fatal(err)
	}
	router := fox.New()
	New(svc).RegisterRoutes(router)
	setup := identityRequest(router, "POST", "/api/v1/setup", `{"email":"published-cleanup@example.invalid","password":"published-cleanup-password","name":"Published cleanup"}`, nil, "")
	expectStatus(t, setup, 201)
	admin, cookie := readIdentity(t, setup)
	if err := svc.StartSystemInstance(ctx, service.SystemInstanceMetadata{Name: "Published owner", Hostname: "published.invalid", Version: "fixture", GoVersion: "fixture", OS: "fixture", Arch: "fixture"}); err != nil {
		t.Fatal(err)
	}
	defer func() {
		svc.StopRuntime()
		stop, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := svc.StopSystemInstance(stop); err != nil {
			t.Error(err)
		}
	}()
	if err := svc.StartRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	etagRequest := func(method, path, raw, etag string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "http://routex.test"+path, strings.NewReader(raw))
		req.AddCookie(cookie)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-CSRF-Token", admin.CSRFToken)
		req.Header.Set("Origin", "http://routex.test")
		req.Header.Set("If-Match", fmt.Sprintf("%q", etag))
		out := httptest.NewRecorder()
		router.ServeHTTP(out, req)
		for _, private := range []string{"published-cleanup\"", "controlled-credential", "physical_object", "known_no_effect", "remote_request_id", "operation_proof"} {
			if strings.Contains(out.Body.String(), private) {
				t.Error("private source cleanup facts exposed")
			}
		}
		return out
	}
	request := systemStatusRequest(t, router, cookie, admin.CSRFToken)
	uuid := func(n int) string { return fmt.Sprintf("88000000-1111-4111-8111-%012d", n) }
	catalog := decodeCatalogResponse[service.VaultIntegrationPage](t, request("GET", "/api/v1/admin/secrets/integrations", nil), 200)
	raw, _ := json.Marshal(map[string]any{"request_id": uuid(1), "name": "Published cleanup source", "descriptor": service.VaultDescriptor{Endpoint: stub.URL, Mount: "kv", Prefix: "published", DataField: "value"}, "writer_auth": map[string]string{"action": "replace", "token": "published-writer"}, "reader_auth": map[string]string{"action": "replace", "token": "published-reader"}, "reason": "Reviewed controlled source"})
	cfg := decodeCatalogResponse[service.VaultConfigResult](t, etagRequest("POST", "/api/v1/admin/secrets/integrations", string(raw), catalog.ReviewETag), 200)
	policy := decodeCatalogResponse[service.CredentialStoragePolicyView](t, request("GET", "/api/v1/admin/secrets/provider-storage", nil), 200)
	raw, _ = json.Marshal(service.CredentialStoragePolicyInput{Mode: "vault", IntegrationID: &cfg.IntegrationID, RevisionID: &cfg.RevisionID, Reason: "Reviewed future storage"})
	expectStatus(t, etagRequest("PUT", "/api/v1/admin/secrets/provider-storage", string(raw), policy.ETag), 200)
	contextView := decodeCatalogResponse[service.CredentialStorageContext](t, request("GET", "/api/v1/admin/provider-credential-storage-context", nil), 200)
	expectStatus(t, request("POST", "/api/v1/admin/providers", map[string]any{"name": "Published Provider", "connection_name": "Published Connection", "base_url": "https://example.invalid/v1", "protocol": "openai_chat", "credential_name": "Published Credential", "secret": "controlled-credential", "request_id": uuid(2), "storage_policy_etag": contextView.ETag}), 201)
	var op entity.CredentialStorageOperation
	if err := db.Take(&op, "request_id = ?", uuid(2)).Error; err != nil || op.State != "committed" {
		t.Fatal("normal creation not committed", err)
	}
	original := op
	target := "/api/v1/admin/secrets/integrations/" + cfg.IntegrationID + "/provider-orphans/" + op.RequestID
	active := decodeCatalogResponse[service.ProviderCredentialOrphanView](t, request("GET", target, nil), 200)
	if active.Eligible || !active.CanCleanup {
		t.Fatal("live committed reference cleanup admitted")
	}
	metadata, err := svc.GetCredentialMetadata(ctx, admin.User.ID, op.CredentialID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.DeleteCredential(ctx, admin.User.ID, op.CredentialID, metadata.ETag, service.CredentialDeleteInput{Reason: "Reviewed logical Credential removal"}); err != nil {
		t.Fatal(err)
	}
	baselineReads := reads.Load()
	view := decodeCatalogResponse[service.ProviderCredentialOrphanView](t, request("GET", target, nil), 200)
	if view.State != "committed" || !view.OwnershipRecorded || !view.CanCleanup {
		t.Fatal("published identity/provenance lost")
	}
	input := map[string]any{"request_id": uuid(3), "reason": "Explicit joined published cleanup", "cleanup_token": "published-cleanup"}
	raw, _ = json.Marshal(input)
	outcome := etagRequest("POST", target+"/cleanup", string(raw), view.ReviewETag)
	if !view.Eligible {
		expectStatus(t, outcome, 409)
		if destroys.Load() != 0 || reads.Load() != baselineReads {
			t.Fatal("unproven historical process reached ownership GET/destroy")
		}
		var records int64
		if err := db.Model(&entity.CredentialPublishedCleanup{}).Where("creation_request_id = ?", op.RequestID).Count(&records).Error; err != nil || records != 0 {
			t.Fatal("blocked preview claimed a command", err)
		}
	} else {
		result := decodeCatalogResponse[service.ProviderCredentialCleanupResult](t, outcome, 200)
		if result.Running || result.Receipt.State != "acknowledged" || !result.Receipt.Cleanup.Observation.Succeeded || destroys.Load() != 1 || reads.Load() != baselineReads+1 {
			t.Fatal("exact joined remote outcome missing")
		}
		var v80 int64
		if err := db.Model(&entity.ProviderCredentialCleanup{}).Where("creation_request_id = ?", op.RequestID).Count(&v80).Error; err != nil || v80 != 0 {
			t.Fatal("published command changed V80", err)
		}
		receipt := decodeCatalogResponse[service.ProviderCredentialCleanupReceipt](t, request("GET", target+"/commands/"+uuid(3), nil), 200)
		if !providerCleanupReceiptEqual(receipt, result.Receipt) {
			t.Fatal("immutable receipt changed")
		}
		delete(input, "cleanup_token")
		raw, _ = json.Marshal(input)
		replay := decodeCatalogResponse[service.ProviderCredentialCleanupResult](t, etagRequest("POST", target+"/cleanup", string(raw), view.ReviewETag), 200)
		if !providerCleanupReceiptEqual(replay.Receipt, result.Receipt) || destroys.Load() != 1 || reads.Load() != baselineReads+1 {
			t.Fatal("original UUID replayed SDK")
		}
		input["reason"] = "Changed command"
		raw, _ = json.Marshal(input)
		expectStatus(t, etagRequest("POST", target+"/cleanup", string(raw), view.ReviewETag), 409)
	}
	var retained entity.CredentialStorageOperation
	if err := db.Take(&retained, "request_id = ?", op.RequestID).Error; err != nil || !reflect.DeepEqual(original, retained) {
		t.Fatal("cleanup changed original creation history", err)
	}
}
