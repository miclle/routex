package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
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
	lostReplyClosureUnknown := false
	defer func() {
		if lostReplyClosureUnknown {
			providerCleanupAssertUnprovenProcess(t, db, svc)
			return
		}
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
	providerCleanupLostDestroyReply(t, db, store, svc, admin, cookie, &lostReplyClosureUnknown)
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

// A fresh Service must read the durable unknown receipt without repeating the
// ambiguous remote attempt or retaining the first request's cleanup Token.
// Receipt replay does not prove the original process closed or authorize a new
// cleanup after the headerless response made source closure unprovable.
func providerCleanupLostDestroyReply(t *testing.T, db *gorm.DB, store *secretstore.Store, originalService *service.Service, admin SessionResponse, cookie *http.Cookie, closureUnknown *bool) {
	t.Helper()
	ctx := context.Background()
	creationID := "80000000-1111-4111-8111-000000000004"
	commandID := "80000000-1111-4111-8111-000000000005"
	const reason = "Retain unknown after exact destroy reply loss"
	var mu sync.Mutex
	var value map[string]string
	var valuePath string
	var writes, reads, destroys atomic.Int32
	writeEntered := make(chan struct{})
	writeRelease := make(chan struct{})
	var releaseOnce sync.Once
	releaseWrite := func() { releaseOnce.Do(func() { close(writeRelease) }) }
	destroyReturned := make(chan struct{})
	var destroyOnce sync.Once
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case "POST":
			if writes.Add(1) != 1 || r.Header.Get("X-Vault-Token") != "cleanup-fixture-writer" || !strings.HasPrefix(r.URL.Path, "/v1/kv/data/lost/") || r.URL.RawQuery != "" {
				t.Error("lost-reply creation identity/path/replay")
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
				t.Error("lost-reply creation is not exact CAS0")
				w.WriteHeader(400)
				return
			}
			mu.Lock()
			value, valuePath = body.Data, r.URL.Path
			mu.Unlock()
			close(writeEntered)
			select {
			case <-writeRelease:
			case <-r.Context().Done():
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"version": 1, "destroyed": false, "deletion_time": ""}})
		case "GET":
			reads.Add(1)
			mu.Lock()
			data, path := value, valuePath
			mu.Unlock()
			if r.Header.Get("X-Vault-Token") != "cleanup-fixture-reader" || r.URL.RawQuery != "version=1" || r.URL.Path != path {
				t.Error("lost-reply retained ownership identity/version/path")
				w.WriteHeader(400)
				return
			}
			if data == nil {
				w.WriteHeader(404)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"data": data, "metadata": map[string]any{"version": 1, "destroyed": false, "deletion_time": ""}}})
		case "PUT":
			defer destroyOnce.Do(func() { close(destroyReturned) })
			if destroys.Add(1) != 1 || r.Header.Get("X-Vault-Token") != "independent-cleanup-token" || r.URL.RawQuery != "" {
				t.Error("lost-reply destroy authority/replay")
			}
			var body struct {
				Versions []int `json:"versions"`
			}
			if json.NewDecoder(r.Body).Decode(&body) != nil || !reflect.DeepEqual(body.Versions, []int{1}) {
				t.Error("lost-reply destroy changed exact version")
			}
			var pending entity.ProviderCredentialCleanup
			if err := db.Take(&pending, "creation_request_id = ? AND request_id = ?", creationID, commandID).Error; err != nil || pending.State != "pending" || pending.ActorID != admin.User.ID || pending.Reason != reason {
				t.Error("lost-reply effect lacks exact durable pending claim")
			}
			mu.Lock()
			if value == nil || r.URL.Path != strings.Replace(valuePath, "/data/", "/destroy/", 1) {
				t.Error("lost-reply effect changed owned physical object")
			}
			value = nil
			mu.Unlock()
			// Apply the controlled effect first, then close without ever writing
			// a status or body. An explicit HTTP503 is not this witness.
			hijacker, ok := w.(http.Hijacker)
			if !ok {
				t.Error("lost-reply origin cannot own its exact connection")
				return
			}
			conn, _, err := hijacker.Hijack()
			if err != nil {
				t.Error("lost-reply connection capture failed")
				return
			}
			if err = conn.Close(); err != nil {
				t.Error("lost-reply connection close failed")
			}
		default:
			t.Error("lost-reply extra remote operation")
			w.WriteHeader(400)
		}
	}))
	defer stub.Close()
	defer releaseWrite()
	router := fox.New()
	New(originalService).RegisterRoutes(router)
	request := func(method, path string, body any, etag, csrf string) *httptest.ResponseRecorder {
		raw := ""
		if body != nil {
			data, err := json.Marshal(body)
			if err != nil {
				t.Fatal("lost-reply request encoding", err)
			}
			raw = string(data)
		}
		req := httptest.NewRequest(method, "http://routex.test"+path, strings.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(cookie)
		if csrf != "" {
			req.Header.Set("X-CSRF-Token", csrf)
		}
		if method != "GET" {
			req.Header.Set("Origin", "http://routex.test")
		}
		if etag != "" {
			req.Header.Set("If-Match", fmt.Sprintf("%q", etag))
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		for _, forbidden := range []string{"independent-cleanup-token", "cleanup-fixture-writer\"", "cleanup-fixture-reader\"", "private-lost-reply-value", "reference_id", "descriptor_sha256", "expected_marker_sha256", "intent_json", "operation_proof", "process_generation"} {
			if strings.Contains(response.Body.String(), forbidden) {
				t.Error("lost-reply response exposed private material")
			}
		}
		if strings.Contains(path, "provider-orphans") && !strings.Contains(response.Header().Get("Cache-Control"), "no-store") {
			t.Error("lost-reply receipt is cacheable")
		}
		return response
	}
	catalog := decodeCatalogResponse[service.VaultIntegrationPage](t, request("GET", "/api/v1/admin/secrets/integrations", nil, "", ""), 200)
	integration := decodeCatalogResponse[service.VaultConfigResult](t, request("POST", "/api/v1/admin/secrets/integrations", map[string]any{"request_id": "80000000-1111-4111-8111-000000000006", "name": "Lost cleanup reply", "descriptor": service.VaultDescriptor{Endpoint: stub.URL, Mount: "kv", Prefix: "lost", DataField: "value"}, "writer_auth": map[string]string{"action": "replace", "token": "cleanup-fixture-writer"}, "reader_auth": map[string]string{"action": "replace", "token": "cleanup-fixture-reader"}, "reason": "Capture independent lost-reply lineage"}, catalog.ReviewETag, admin.CSRFToken), 200)
	policyPath := "/api/v1/admin/secrets/provider-storage"
	savePolicy := func(mode string, id, revision *string) {
		policy := decodeCatalogResponse[service.CredentialStoragePolicyView](t, request("GET", policyPath, nil, "", ""), 200)
		expectStatus(t, request("PUT", policyPath, service.CredentialStoragePolicyInput{Mode: mode, IntegrationID: id, RevisionID: revision, Reason: "Capture lost-reply orphan through normal policy"}, policy.ETag, admin.CSRFToken), 200)
	}
	savePolicy("vault", &integration.IntegrationID, &integration.RevisionID)
	storage := decodeCatalogResponse[service.CredentialStorageContext](t, request("GET", "/api/v1/admin/provider-credential-storage-context", nil, "", ""), 200)
	creation := map[string]any{"name": "Lost-reply uncommitted Provider", "connection_name": "Lost-reply uncommitted Connection", "base_url": "https://example.invalid/v1", "protocol": "openai_chat", "credential_name": "Lost-reply uncommitted Credential", "secret": "private-lost-reply-value", "request_id": creationID, "storage_policy_etag": storage.ETag}
	created := make(chan *httptest.ResponseRecorder, 1)
	go func() { created <- request("POST", "/api/v1/admin/providers", creation, "", admin.CSRFToken) }()
	select {
	case <-writeEntered:
	case <-time.After(5 * time.Second):
		t.Fatal("lost-reply CAS0 did not enter owned origin")
	}
	savePolicy("inline", nil, nil)
	releaseWrite()
	select {
	case response := <-created:
		expectStatus(t, response, 409)
	case <-time.After(5 * time.Second):
		t.Fatal("lost-reply creation did not join after release")
	}
	var original entity.CredentialStorageOperation
	if err := db.Take(&original, "request_id = ?", creationID).Error; err != nil || original.State != "orphan" {
		t.Fatal("lost-reply confirmed orphan absent", err)
	}
	target := "/api/v1/admin/secrets/integrations/" + integration.IntegrationID + "/provider-orphans/" + creationID
	viewResponse := request("GET", target, nil, "", "")
	view := decodeCatalogResponse[service.ProviderCredentialOrphanView](t, viewResponse, 200)
	if !view.Eligible || !view.OwnershipRecorded || !view.CanCleanup || len(view.BlockerCodes) != 0 || viewResponse.Header().Get("ETag") != fmt.Sprintf("%q", view.ReviewETag) {
		t.Fatal("lost-reply exact fresh ownership preview unavailable")
	}
	command := map[string]any{"request_id": commandID, "reason": reason, "cleanup_token": "independent-cleanup-token"}
	resultResponse := request("POST", target+"/cleanup", command, view.ReviewETag, admin.CSRFToken)
	delete(command, "cleanup_token")
	result := decodeCatalogResponse[service.ProviderCredentialCleanupResult](t, resultResponse, 200)
	if result.Running || result.Receipt.State != "unknown" || result.Receipt.CreationRequestID != creationID || result.Receipt.RequestID != commandID || result.Receipt.IntegrationID != integration.IntegrationID || result.Receipt.RevisionID != integration.RevisionID || result.Receipt.FinishedAt == nil || !result.Receipt.Ownership.Succeeded || result.Receipt.Cleanup.State != "unknown" || !result.Receipt.Cleanup.Observation.Attempted || result.Receipt.Cleanup.Observation.Succeeded || result.Receipt.Cleanup.Observation.Failure == nil {
		t.Fatal("lost-reply API did not preserve exact terminal unknown receipt")
	}
	failure := result.Receipt.Cleanup.Observation.Failure
	if failure.Stage != "cleanup" || failure.Code != "transport" || failure.HTTPStatus != 0 {
		t.Fatal("lost-reply failure did not retain headerless transport uncertainty", failure.Stage, failure.Code, failure.HTTPStatus)
	}
	select {
	case <-destroyReturned:
	case <-time.After(5 * time.Second):
		t.Fatal("lost-reply origin handler did not return after exact close")
	}
	mu.Lock()
	absent := value == nil
	mu.Unlock()
	if !absent || writes.Load() != 1 || reads.Load() != 2 || destroys.Load() != 1 {
		t.Fatal("lost-reply effect or exact remote counts", absent, writes.Load(), reads.Load(), destroys.Load())
	}
	var saved entity.ProviderCredentialCleanup
	if err := db.Take(&saved, "creation_request_id = ? AND request_id = ?", creationID, commandID).Error; err != nil || saved.State != "unknown" || saved.ActorID != admin.User.ID || !saved.ActorBirth.Equal(original.ActorBirth) || saved.IntegrationID != integration.IntegrationID || saved.RevisionID != integration.RevisionID || saved.Reason != reason || saved.ReviewedETag != view.ReviewETag || saved.FinishedAt == nil {
		t.Fatal("lost-reply real database did not retain exact unknown claim", err)
	}
	// The request and owned destroy handler have returned above; stop/join the
	// runtime separately. Headerless loss must not become source-closure proof.
	// This reconstruction is not production-process shutdown acceptance.
	originalService.StopRuntime()
	providerCleanupAssertUnprovenProcess(t, db, originalService)
	*closureUnknown = true
	fresh, err := service.New(ctx, db, service.WithCredentialStorage(store), service.WithUpstreamPolicy(true))
	if err != nil {
		t.Fatal("lost-reply fresh Service construction", err)
	}
	if err = fresh.StartSystemInstance(ctx, service.SystemInstanceMetadata{Name: "Lost-reply reconstructed owner", Hostname: "cleanup.invalid", Version: "fixture", GoVersion: "fixture", OS: "fixture", Arch: "fixture"}); err != nil {
		t.Fatal("lost-reply reconstructed instance start", err)
	}
	defer func() {
		fresh.StopRuntime()
		stop, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := fresh.StopSystemInstance(stop); err != nil {
			t.Error("lost-reply reconstructed instance did not join", err)
		}
	}()
	if err = fresh.StartRuntime(ctx); err != nil {
		t.Fatal("lost-reply reconstructed runtime start", err)
	}
	router = fox.New()
	New(fresh).RegisterRoutes(router)
	current := decodeCatalogResponse[SessionResponse](t, request("GET", "/api/v1/auth/session", nil, "", ""), 200)
	if current.User.ID != admin.User.ID || current.CSRFToken == "" {
		t.Fatal("lost-reply original Session did not reauthorize")
	}
	blocked := decodeCatalogResponse[service.ProviderCredentialOrphanView](t, request("GET", target, nil, "", ""), 200)
	cleanupClaimed, ownershipUnknown := false, false
	for _, code := range blocked.BlockerCodes {
		cleanupClaimed = cleanupClaimed || code == "cleanup_claimed"
		ownershipUnknown = ownershipUnknown || code == "process_ownership_unknown"
	}
	if blocked.Eligible || !blocked.OwnershipRecorded || !cleanupClaimed || !ownershipUnknown {
		t.Fatal("lost-reply fresh Service inferred executable cleanup from a retained receipt")
	}
	retained := decodeCatalogResponse[service.ProviderCredentialCleanupReceipt](t, request("GET", target+"/commands/"+commandID, nil, "", ""), 200)
	if !providerCleanupReceiptEqual(result.Receipt, retained) {
		t.Fatal("lost-reply fresh Service changed exact historical receipt")
	}
	if _, hasToken := command["cleanup_token"]; hasToken {
		t.Fatal("lost-reply reconciliation retained transient cleanup Token")
	}
	replayed := decodeCatalogResponse[service.ProviderCredentialCleanupResult](t, request("POST", target+"/cleanup", command, view.ReviewETag, current.CSRFToken), 200)
	if replayed.Running || !providerCleanupReceiptEqual(retained, replayed.Receipt) {
		t.Fatal("lost-reply token-free reconciliation changed unknown receipt")
	}
	separate := map[string]any{"request_id": "80000000-1111-4111-8111-000000000007", "reason": "Cannot retry ambiguous remote effect", "cleanup_token": "independent-cleanup-token"}
	expectStatus(t, request("POST", target+"/cleanup", separate, view.ReviewETag, current.CSRFToken), 409)
	clear(separate)
	var after entity.ProviderCredentialCleanup
	var afterOperation entity.CredentialStorageOperation
	if err = db.Take(&after, "creation_request_id = ?", creationID).Error; err != nil || !reflect.DeepEqual(saved, after) {
		t.Fatal("lost-reply original durable unknown claim changed", err)
	}
	if err = db.Take(&afterOperation, "request_id = ?", creationID).Error; err != nil || !reflect.DeepEqual(original, afterOperation) {
		t.Fatal("lost-reply original write/read facts changed", err)
	}
	final := decodeCatalogResponse[service.ProviderCredentialCleanupReceipt](t, request("GET", target+"/commands/"+commandID, nil, "", ""), 200)
	if !providerCleanupReceiptEqual(retained, final) || writes.Load() != 1 || reads.Load() != 2 || destroys.Load() != 1 {
		t.Fatal("lost-reply receipt/rejected new UUID replayed remote work", writes.Load(), reads.Load(), destroys.Load())
	}
	for _, model := range []any{&entity.Provider{}, &entity.ProviderConnection{}, &entity.ProviderCredential{}, &entity.CredentialVaultReference{}, &entity.CallRecord{}, &entity.CallAttempt{}} {
		var count int64
		if err = db.Model(model).Count(&count).Error; err != nil || count != 0 {
			t.Fatal("lost-reply reconciliation restored business or native rows", err, count)
		}
	}
}

// A returned request is not a positive close proof after the SDK lost headers.
// Repeated shutdown must remain failed and must not fabricate durable closure.
func providerCleanupAssertUnprovenProcess(t *testing.T, db *gorm.DB, svc *service.Service) {
	t.Helper()
	processID := svc.CurrentSystemInstanceID()
	var before entity.CredentialSourceProcess
	if processID == "" || db.Take(&before, "process_id = ?", processID).Error != nil || before.ProcessID != processID || before.ClosedAt != nil {
		t.Fatal("lost-reply original process lacked exact unclosed registration")
	}
	stop, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	err := svc.StopSystemInstance(stop)
	cancel()
	var unavailable *apperrors.Error
	if !errors.As(err, &unavailable) || unavailable.Code != http.StatusServiceUnavailable || unavailable.Message != "Vault configuration unavailable" {
		t.Fatal("lost-reply shutdown did not retain unproven source closure", err)
	}
	var process entity.CredentialSourceProcess
	var instance entity.SystemInstance
	if err := db.Take(&process, "process_id = ?", processID).Error; err != nil || !reflect.DeepEqual(before, process) || process.ClosedAt != nil {
		t.Fatal("lost-reply shutdown changed or fabricated source closure", err)
	}
	if err := db.Take(&instance, "id = ?", processID).Error; err != nil || instance.ID != processID || !instance.StartedAt.Equal(process.Birth) || instance.StoppedAt != nil || instance.RetiredAt != nil || svc.CurrentSystemInstanceID() != processID {
		t.Fatal("lost-reply shutdown fabricated instance closure or retirement", err)
	}
}
