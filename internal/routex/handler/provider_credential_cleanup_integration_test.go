package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/secretstore"
	"gorm.io/gorm"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Registered after the exact152 historical cases. This fixture uses normal APIs
// and a controlled HTTP origin, without administrative SQL policy/auth changes.
func testProviderCredentialCleanupLifecycle(t *testing.T, db *gorm.DB) {
	ctx := context.Background()
	store, e := secretstore.New(bytes.Repeat([]byte{87}, 32))
	if e != nil {
		t.Fatal(e)
	}
	var mu sync.Mutex
	values := map[string]map[string]string{}
	var writes, reads, destroys atomic.Int32
	entered := make(chan struct{})
	resume := make(chan struct{})
	var resumeOnce sync.Once
	releaseWrite := func() { resumeOnce.Do(func() { close(resume) }) }
	var hold atomic.Bool
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case "POST":
			writes.Add(1)
			if r.Header.Get("X-Vault-Token") != "cleanup-fixture-writer" || !strings.HasPrefix(r.URL.Path, "/v1/kv/data/owned/") {
				t.Error("wrong creator identity/path")
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
				t.Error("not exact credential CAS0")
			}
			mu.Lock()
			_, duplicate := values[r.URL.Path]
			values[r.URL.Path] = body.Data
			mu.Unlock()
			if duplicate {
				t.Error("creation replayed")
			}
			if hold.Swap(false) {
				close(entered)
				select {
				case <-resume:
				case <-r.Context().Done():
					return
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"version": 1, "destroyed": false, "deletion_time": ""}})
		case "GET":
			reads.Add(1)
			if r.Header.Get("X-Vault-Token") != "cleanup-fixture-reader" || r.URL.RawQuery != "version=1" {
				t.Error("not retained exact reader/version")
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
			if r.Header.Get("X-Vault-Token") != "independent-cleanup-token" || !strings.HasPrefix(r.URL.Path, "/v1/kv/destroy/owned/") {
				t.Error("wrong cleanup authority/path")
			}
			var body struct {
				Versions []int `json:"versions"`
			}
			if json.NewDecoder(r.Body).Decode(&body) != nil || !reflect.DeepEqual(body.Versions, []int{1}) {
				t.Error("nonexact destruction")
			}
			var claims int64
			if e := db.Model(&entity.ProviderCredentialCleanup{}).Where("state = ?", "pending").Count(&claims).Error; e != nil || claims != 1 {
				t.Error("remote effect without durable claim")
			}
			mu.Lock()
			delete(values, strings.Replace(r.URL.Path, "/destroy/", "/data/", 1))
			mu.Unlock()
			w.WriteHeader(204)
		default:
			t.Error("extra Vault operation")
			w.WriteHeader(400)
		}
	}))
	defer stub.Close()
	defer releaseWrite()
	svc, e := service.New(ctx, db, service.WithCredentialStorage(store), service.WithUpstreamPolicy(true))
	if e != nil {
		t.Fatal(e)
	}
	router := fox.New()
	New(svc).RegisterRoutes(router)
	setup := identityRequest(router, "POST", "/api/v1/setup", `{"email":"cleanup-admin@example.invalid","password":"test-only-cleanup-password","name":"Cleanup admin"}`, nil, "")
	expectStatus(t, setup, 201)
	admin, cookie := readIdentity(t, setup)
	if e = svc.StartSystemInstance(ctx, service.SystemInstanceMetadata{Name: "Cleanup owner", Hostname: "cleanup.invalid", Version: "fixture", GoVersion: "fixture", OS: "fixture", Arch: "fixture"}); e != nil {
		t.Fatal(e)
	}
	defer func() {
		stop, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if e := svc.StopSystemInstance(stop); e != nil {
			t.Error(e)
		}
	}()
	if e = svc.StartRuntime(ctx); e != nil {
		t.Fatal(e)
	}
	defer svc.StopRuntime()
	_, memberCookie, memberCSRF := createSystemStatusMember(t, svc, router, admin.User.ID, "cleanup-reader", []string{"providers.write", "secrets.read", "secrets.write"})
	request := func(method, path string, body any, etag string, session *http.Cookie, csrf string) *httptest.ResponseRecorder {
		raw := ""
		if body != nil {
			b, e := json.Marshal(body)
			if e != nil {
				t.Fatal(e)
			}
			raw = string(b)
		}
		req := httptest.NewRequest(method, "http://routex.test"+path, strings.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		if session != nil {
			req.AddCookie(session)
		}
		if csrf != "" {
			req.Header.Set("X-CSRF-Token", csrf)
		}
		if method != "GET" {
			req.Header.Set("Origin", "http://routex.test")
		}
		if etag != "" {
			req.Header.Set("If-Match", fmt.Sprintf("%q", etag))
		}
		out := httptest.NewRecorder()
		router.ServeHTTP(out, req)
		for _, forbidden := range []string{"independent-cleanup-token", "cleanup-fixture-writer\"", "cleanup-fixture-reader\"", "private-credential-value", "reference_id", "descriptor_sha256", "expected_marker_sha256", "intent_json", "operation_proof", "process_generation"} {
			if strings.Contains(out.Body.String(), forbidden) {
				t.Error("private cleanup material exposed")
			}
		}
		if !strings.Contains(out.Header().Get("Cache-Control"), "no-store") && strings.Contains(path, "provider-orphans") {
			t.Error("private cleanup response cacheable")
		}
		return out
	}
	uuid := func(n int) string { return fmt.Sprintf("80000000-1111-4111-8111-%012d", n) }
	cat := decodeCatalogResponse[service.VaultIntegrationPage](t, request("GET", "/api/v1/admin/secrets/integrations", nil, "", cookie, ""), 200)
	integration := decodeCatalogResponse[service.VaultConfigResult](t, request("POST", "/api/v1/admin/secrets/integrations", map[string]any{"request_id": uuid(1), "name": "Owned cleanup", "descriptor": service.VaultDescriptor{Endpoint: stub.URL, Mount: "kv", Prefix: "owned", DataField: "value"}, "writer_auth": map[string]string{"action": "replace", "token": "cleanup-fixture-writer"}, "reader_auth": map[string]string{"action": "replace", "token": "cleanup-fixture-reader"}, "reason": "Create saved cleanup descriptor"}, cat.ReviewETag, cookie, admin.CSRFToken), 200)
	policyPath := "/api/v1/admin/secrets/provider-storage"
	savePolicy := func(mode string, id, rev *string) {
		p := decodeCatalogResponse[service.CredentialStoragePolicyView](t, request("GET", policyPath, nil, "", cookie, ""), 200)
		expectStatus(t, request("PUT", policyPath, service.CredentialStoragePolicyInput{Mode: mode, IntegrationID: id, RevisionID: rev, Reason: "Change future writes only"}, p.ETag, cookie, admin.CSRFToken), 200)
	}
	savePolicy("vault", &integration.IntegrationID, &integration.RevisionID)
	contextView := decodeCatalogResponse[service.CredentialStorageContext](t, request("GET", "/api/v1/admin/provider-credential-storage-context", nil, "", cookie, ""), 200)
	creationID := uuid(2)
	body := map[string]any{"name": "Never committed provider", "connection_name": "Never committed Connection", "base_url": "https://example.invalid/v1", "protocol": "openai_chat", "credential_name": "Never committed Credential", "secret": "private-credential-value", "request_id": creationID, "storage_policy_etag": contextView.ETag}
	hold.Store(true)
	creationResult := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		creationResult <- request("POST", "/api/v1/admin/providers", body, "", cookie, admin.CSRFToken)
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("real Write not held")
	}
	savePolicy("inline", nil, nil)
	releaseWrite()
	expectStatus(t, <-creationResult, 409)
	var op entity.CredentialStorageOperation
	if e = db.Take(&op, "request_id = ?", creationID).Error; e != nil || op.State != "orphan" {
		t.Fatal("confirmed original orphan was not retained", e)
	}
	original := op
	base := "/api/v1/admin/secrets/integrations/" + integration.IntegrationID + "/provider-orphans"
	target := base + "/" + creationID
	expectStatus(t, request("GET", base, nil, "", memberCookie, ""), 403)
	page := decodeCatalogResponse[service.ProviderCredentialOrphanPage](t, request("GET", base+"?limit=1", nil, "", cookie, ""), 200)
	if len(page.Items) != 1 || page.NextCursor != nil {
		t.Fatal("scoped bounded orphan list")
	}
	response := request("GET", target, nil, "", cookie, "")
	view := decodeCatalogResponse[service.ProviderCredentialOrphanView](t, response, 200)
	if !view.Eligible || !view.OwnershipRecorded || !view.CanCleanup || len(view.BlockerCodes) != 0 || response.Header().Get("ETag") != fmt.Sprintf("%q", view.ReviewETag) {
		t.Fatal("fresh exact ownership preview/ETag")
	}
	command := map[string]any{"request_id": uuid(3), "reason": "Confirm exact never-published orphan", "cleanup_token": "independent-cleanup-token"}
	expectStatus(t, request("POST", target+"/cleanup", command, view.ReviewETag, memberCookie, memberCSRF), 403)
	expectStatus(t, request("POST", target+"/cleanup", command, strings.Repeat("a", 64)+"."+strings.Repeat("b", 64), cookie, admin.CSRFToken), 409)
	result := decodeCatalogResponse[service.ProviderCredentialCleanupResult](t, request("POST", target+"/cleanup", command, view.ReviewETag, cookie, admin.CSRFToken), 200)
	if result.Running || result.Receipt.State != "acknowledged" || !result.Receipt.Ownership.Succeeded || !result.Receipt.Cleanup.Observation.Succeeded {
		t.Fatal("cleanup did not record exact owned destroy")
	}
	delete(command, "cleanup_token")
	replay := decodeCatalogResponse[service.ProviderCredentialCleanupResult](t, request("POST", target+"/cleanup", command, view.ReviewETag, cookie, admin.CSRFToken), 200)
	if !providerCleanupReceiptEqual(result.Receipt, replay.Receipt) {
		t.Fatal("recorded replay changed fields")
	}
	receipt := decodeCatalogResponse[service.ProviderCredentialCleanupReceipt](t, request("GET", target+"/commands/"+uuid(3), nil, "", cookie, ""), 200)
	if !providerCleanupReceiptEqual(receipt, result.Receipt) {
		t.Fatal("receipt GET changed acknowledged command")
	}
	expectStatus(t, request("POST", "/api/v1/admin/providers", body, "", cookie, admin.CSRFToken), 409)
	var after entity.CredentialStorageOperation
	if e = db.Take(&after, "request_id = ?", creationID).Error; e != nil || !reflect.DeepEqual(original, after) {
		t.Fatal("original Write/Read plan changed", e)
	}
	for _, model := range []any{&entity.Provider{}, &entity.ProviderConnection{}, &entity.ProviderCredential{}, &entity.CredentialVaultReference{}, &entity.CallRecord{}, &entity.CallAttempt{}} {
		var n int64
		if e = db.Model(model).Count(&n).Error; e != nil || n != 0 {
			t.Fatal("cleanup restored business or native rows", e, n)
		}
	}
	if writes.Load() != 1 || reads.Load() != 2 || destroys.Load() != 1 {
		t.Fatal("remote replay or missing ownership proof", writes.Load(), reads.Load(), destroys.Load())
	}
}
func providerCleanupReceiptEqual(a, b service.ProviderCredentialCleanupReceipt) bool {
	if !a.StartedAt.Equal(b.StartedAt) || (a.FinishedAt == nil) != (b.FinishedAt == nil) {
		return false
	}
	if a.FinishedAt != nil && !a.FinishedAt.Equal(*b.FinishedAt) {
		return false
	}
	a.StartedAt = a.StartedAt.UTC()
	b.StartedAt = b.StartedAt.UTC()
	if a.FinishedAt != nil {
		x, y := a.FinishedAt.UTC(), b.FinishedAt.UTC()
		a.FinishedAt = &x
		b.FinishedAt = &y
	}
	return reflect.DeepEqual(a, b)
}
