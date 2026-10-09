package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fox-gonic/fox"
	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/id"
	"github.com/miclle/routex/pkg/secret"
	"github.com/miclle/routex/pkg/secretstore"
)

func testCredentialDeleteLifecycle(t *testing.T, db *gorm.DB) {
	ctx := context.Background()
	store, err := secretstore.New(bytes.Repeat([]byte{75}, 32))
	if err != nil {
		t.Fatal(err)
	}
	var dispatches atomic.Int32
	var selected atomic.Value
	var hold atomic.Bool
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseUpstream := func() { releaseOnce.Do(func() { close(release) }) }
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/models" {
			_, _ = w.Write([]byte(`{"data":[{"id":"delete-upstream"}]}`))
			return
		}
		dispatches.Add(1)
		selected.Store(r.Header.Get("Authorization"))
		if hold.Load() {
			entered <- struct{}{}
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
		}
		_, _ = w.Write([]byte(`{"model":"delete-upstream","choices":[],"usage":{"prompt_tokens":1,"completion_tokens":1}}`))
	}))
	defer upstream.Close()
	defer releaseUpstream()
	svc, err := service.New(ctx, db, service.WithCredentialStorage(store), service.WithUpstreamPolicy(true), service.WithRuntimeRefreshInterval(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	router := fox.New()
	New(svc).RegisterRoutes(router)
	setup := identityRequest(router, "POST", "/api/v1/setup", `{"email":"credential-delete@example.invalid","password":"test-only-delete-password","name":"Delete admin"}`, nil, "")
	expectStatus(t, setup, 201)
	admin, cookie := readIdentity(t, setup)
	reader, readCookie, readCSRF := createSystemStatusMember(t, svc, router, admin.User.ID, "delete-reader", []string{"providers.read"})
	_, writeCookie, writeCSRF := createSystemStatusMember(t, svc, router, admin.User.ID, "delete-writer", []string{"providers.write"})
	provider, err := svc.CreateProvider(ctx, admin.User.ID, "Deletion provider", service.CreateConnectionInput{Name: "Deletion connection", BaseURL: upstream.URL + "/v1", Protocol: entity.ProtocolOpenAIChat, CredentialName: "Primary", Secret: "test-only-delete-primary"})
	if err != nil {
		t.Fatal(err)
	}
	connection := provider.Connections[0].Connection
	primary := provider.Connections[0].Credentials[0]
	backup, err := svc.CreateCredential(ctx, admin.User.ID, connection.ID, "Backup", "test-only-delete-backup", 20)
	if err != nil {
		t.Fatal(err)
	}
	for _, credentialID := range []string{primary.ID, backup.ID} {
		verification, err := svc.VerifyCredential(ctx, admin.User.ID, credentialID)
		if err != nil || !verification.Verified {
			t.Fatal("controlled discovery failed")
		}
		if _, err := svc.SetCredentialEnabled(ctx, admin.User.ID, credentialID, true); err != nil {
			t.Fatal(err)
		}
	}
	var providerModel entity.ProviderModel
	if err := db.Where("connection_id = ?", connection.ID).First(&providerModel).Error; err != nil {
		t.Fatal(err)
	}
	model, err := svc.CreateModel(ctx, admin.User.ID, "delete-model", providerModel.ID)
	if err != nil {
		t.Fatal(err)
	}
	model, err = svc.SetModelWeights(ctx, admin.User.ID, model.Model.ID, []service.ModelWeight{{BindingID: model.Bindings[0].Binding.ID, Weight: 100}})
	if err != nil {
		t.Fatal(err)
	}
	binding := model.Bindings[0].Binding
	bearer := "rx_" + strings.Repeat("d", 43)
	for _, row := range []any{
		&entity.APIKey{ID: "key_credential_delete", UserID: admin.User.ID, Name: "Deletion Key", Prefix: "rx_masked", TokenHash: secret.SHA256Hex(bearer), Status: entity.KeyActive},
		&entity.APIKeyModel{KeyID: "key_credential_delete", ModelID: model.Model.ID},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	if err := svc.RecordCall(ctx, service.CallFact{RequestID: "req_credential_delete_history", UserID: admin.User.ID, KeyID: "key_credential_delete", ModelID: model.Model.ID, ModelName: model.Name, ProviderID: provider.Provider.ID, ProviderName: provider.Provider.Name, ProviderModelID: providerModel.ID, ConnectionID: connection.ID, ConnectionName: connection.Name, Protocol: entity.ProtocolOpenAIChat, UpstreamModelName: providerModel.UpstreamName, Status: "success", StartedAt: now, CompletedAt: now.Add(time.Second), Attempts: []service.CallAttempt{{ID: "att_credential_delete_history", ProviderID: provider.Provider.ID, ProviderName: provider.Provider.Name, ProviderModelID: providerModel.ID, ConnectionID: connection.ID, ConnectionName: connection.Name, UpstreamModelName: providerModel.UpstreamName, AttemptNumber: 1, Status: "success", HTTPStatus: 200, WorkEvidence: "completed", StartedAt: now, CompletedAt: now.Add(time.Second)}}}); err != nil {
		t.Fatal(err)
	}
	var historicalCall entity.CallRecord
	var historicalAttempt entity.CallAttempt
	if err := db.First(&historicalCall, "request_id = ?", "req_credential_delete_history").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&historicalAttempt, "id = ?", "att_credential_delete_history").Error; err != nil {
		t.Fatal(err)
	}
	unpublished, err := svc.CreateCredential(ctx, admin.User.ID, connection.ID, "Unpublished removal", "test-only-unpublished-removal", 1)
	if err != nil {
		t.Fatal(err)
	}
	unpublishedMetadata, err := svc.GetCredentialMetadata(ctx, admin.User.ID, unpublished.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.DeleteCredential(ctx, admin.User.ID, unpublished.ID, unpublishedMetadata.ETag, service.CredentialDeleteInput{Reason: "Reviewed unpublished removal"})
	var publicationError *apperrors.Error
	if !errors.As(err, &publicationError) || publicationError.Code != 503 {
		t.Fatal("uninitialized runtime incorrectly confirmed deletion publication")
	}
	if err := svc.StartRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	// A one-hour refresh interval keeps fault injection deterministic while
	// the live publisher still handles explicit mutations and final cleanup.
	defer svc.StopRuntime() // Join even if recorder startup fails.
	if err := svc.StartCallRecorder(ctx, filepath.Join(t.TempDir(), "credential-delete.db")); err != nil {
		t.Fatal(err)
	}
	defer func() { svc.StopRuntime(); _ = svc.StopCallRecorder() }()
	basePath := "/api/v1/admin/credentials/"
	request := func(method, credentialID, body, etag string, sessionCookie *http.Cookie, csrf string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "http://routex.test"+basePath+credentialID, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if sessionCookie != nil {
			req.AddCookie(sessionCookie)
		}
		if csrf != "" {
			req.Header.Set("X-CSRF-Token", csrf)
		}
		if etag != "" {
			req.Header.Set("If-Match", etag)
		}
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		return res
	}
	read := func(credentialID string) service.CredentialMetadataRecord {
		return decodeCatalogResponse[service.CredentialMetadataRecord](t, request("GET", credentialID+"/metadata", "", "", readCookie, ""), 200)
	}
	body := `{"reason":"Reviewed credential removal"}`
	remove := func(credentialID, etag string) *httptest.ResponseRecorder {
		return request("DELETE", credentialID, body, strconv.Quote(etag), writeCookie, writeCSRF)
	}
	confirm := func(res *httptest.ResponseRecorder, credentialID string) {
		t.Helper()
		result := decodeCatalogResponse[service.CredentialDeleteRecord](t, res, 200)
		if result.ID != credentialID || !result.Absent || !result.RuntimeApplied {
			t.Fatal("deletion did not confirm current absence and publication")
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(res.Body.Bytes(), &fields); err != nil || len(fields) != 3 || strings.Contains(res.Body.String(), "secret") || strings.Contains(res.Body.String(), "ciphertext") {
			t.Fatal("unsafe deletion response shape")
		}
	}
	initial := read(primary.ID)
	confirm(remove(unpublished.ID, unpublishedMetadata.ETag), unpublished.ID)
	expectStatus(t, request("DELETE", primary.ID, body, strconv.Quote(initial.ETag), nil, ""), 401)
	expectStatus(t, request("DELETE", primary.ID, body, strconv.Quote(initial.ETag), readCookie, readCSRF), 403)
	expectStatus(t, request("DELETE", primary.ID, body, strconv.Quote(initial.ETag), cookie, ""), 403)
	if _, err := svc.DeleteCredential(ctx, reader.User.ID, primary.ID, initial.ETag, service.CredentialDeleteInput{Reason: "Direct authorization check"}); err == nil {
		t.Fatal("service allowed unauthorized deletion")
	}
	for _, invalid := range []string{`{}`, `{"reason":null}`, `{"reason":" "}`, `{"reason":"bad\nreason"}`, `{"reason":"Reviewed","secret":"forbidden"}`, body + `{}`, `{"reason":"` + strings.Repeat("界", 342) + `"}`} {
		expectStatus(t, request("DELETE", primary.ID, invalid, strconv.Quote(initial.ETag), writeCookie, writeCSRF), 400)
	}
	for _, header := range []string{"", initial.ETag, `W/"` + initial.ETag + `"`, `*`, `"first","second"`, `"0"`, strconv.Quote(strings.Repeat("z", 64))} {
		expectStatus(t, request("DELETE", primary.ID, body, header, writeCookie, writeCSRF), 400)
	}
	expectStatus(t, remove("crd_missing", initial.ETag), 400)
	expectStatus(t, remove(primary.ID, strings.Repeat("0", 64)), 409)
	// Failure after the discovery rows were removed must roll back the entire
	// transaction, including the credential row and audit event.
	rollbackCallback := "test:credential_delete_rollback"
	if err := db.Callback().Delete().After("gorm:delete").Register(rollbackCallback, func(tx *gorm.DB) {
		if tx.Statement.Table == "provider_credentials" {
			_ = tx.AddError(errors.New("test-only delete rollback"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, remove(primary.ID, initial.ETag), 500)
	if err := db.Callback().Delete().Remove(rollbackCallback); err != nil {
		t.Fatal(err)
	}
	var discoveries int64
	if err := db.Model(&entity.CredentialModelAccess{}).Where("credential_id = ?", primary.ID).Count(&discoveries).Error; err != nil || discoveries != 1 || read(primary.ID).ETag != initial.ETag {
		t.Fatal("failed delete partially removed credential or discovery")
	}
	auditCount := func(credentialID string) int64 {
		var count int64
		if err := db.Model(&entity.AuditEvent{}).Where("action = ? AND resource_id = ?", "credential.delete", credentialID).Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		return count
	}
	if auditCount(primary.ID) != 0 {
		t.Fatal("rolled-back deletion left an audit")
	}
	infer := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "http://routex.test/v1/chat/completions", strings.NewReader(`{"model":"delete-model","messages":[{"role":"user","content":"test"}]}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+bearer)
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		return res
	}
	// Corrupting only the target's ciphertext after snapshot preparation proves
	// removal does not decrypt it. The dispatched request retains its snapshot.
	if err := db.Model(&entity.ProviderCredential{}).Where("id = ?", primary.ID).Update("ciphertext", "test-only-unreadable-ciphertext").Error; err != nil {
		t.Fatal(err)
	}
	hold.Store(true)
	inflight := make(chan *httptest.ResponseRecorder, 1)
	go func() { inflight <- infer() }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("controlled request never entered upstream")
	}
	confirm(remove(primary.ID, initial.ETag), primary.ID)
	hold.Store(false)
	releaseUpstream()
	select {
	case result := <-inflight:
		expectStatus(t, result, 200)
	case <-time.After(5 * time.Second):
		t.Fatal("credential removal aborted dispatched request")
	}
	if selected.Load() != "Bearer test-only-delete-primary" {
		t.Fatal("initial request used wrong credential")
	}
	expectStatus(t, request("GET", primary.ID+"/metadata", "", "", readCookie, ""), 404)
	confirm(remove(primary.ID, initial.ETag), primary.ID)
	if auditCount(primary.ID) != 1 {
		t.Fatal("absent reconciliation duplicated deletion audit")
	}
	expectStatus(t, infer(), 200)
	if selected.Load() != "Bearer test-only-delete-backup" {
		t.Fatal("backup credential was not used after removal")
	}
	adminModels := decodeCatalogResponse[ModelsResponse](t, identityRequest(router, "GET", "/api/v1/admin/models", "", cookie, ""), 200)
	if len(adminModels.Items) != 1 || len(adminModels.Items[0].Bindings) != 1 || !adminModels.Items[0].Bindings[0].Ready || adminModels.Items[0].Bindings[0].Weight != 100 {
		t.Fatal("primary deletion changed model identity, readiness, or routing weights")
	}
	// Last eligible deletion commits despite publication failure. The tombstone
	// must block old cached credentials before any subsequent gateway dispatch.
	backupMetadata := read(backup.ID)
	var failPublication atomic.Bool
	publicationCallback := "test:credential_delete_publication"
	if err := db.Callback().Query().Before("gorm:query").Register(publicationCallback, func(tx *gorm.DB) {
		if failPublication.Load() && tx.Statement.Table == "models" {
			_ = tx.AddError(errors.New("test-only unavailable publication"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Callback().Query().Remove(publicationCallback) }()
	failPublication.Store(true)
	expectStatus(t, remove(backup.ID, backupMetadata.ETag), 503)
	beforeDispatches := dispatches.Load()
	expectStatus(t, infer(), 503)
	if dispatches.Load() != beforeDispatches {
		t.Fatal("failed publication dispatched a deleted cached credential")
	}
	expectStatus(t, request("GET", backup.ID+"/metadata", "", "", readCookie, ""), 404)
	failPublication.Store(false)
	confirm(remove(backup.ID, backupMetadata.ETag), backup.ID)
	if auditCount(backup.ID) != 1 {
		t.Fatal("uncertain retry duplicated deletion audit")
	}
	expectStatus(t, infer(), 503)
	if dispatches.Load() != beforeDispatches {
		t.Fatal("last credential deletion dispatched upstream")
	}
	adminModels = decodeCatalogResponse[ModelsResponse](t, identityRequest(router, "GET", "/api/v1/admin/models", "", cookie, ""), 200)
	if len(adminModels.Items) != 1 || adminModels.Items[0].ID != model.Model.ID || adminModels.Items[0].Bindings[0].ID != binding.ID || adminModels.Items[0].Bindings[0].Ready || adminModels.Items[0].Bindings[0].Weight != 100 {
		t.Fatal("last deletion rewrote model, binding, or routing weight instead of readiness")
	}
	var remainingBinding entity.ModelProviderBinding
	if err := db.First(&remainingBinding, "id = ?", binding.ID).Error; err != nil || !reflect.DeepEqual(remainingBinding, binding) {
		t.Fatal("deletion mutated binding")
	}
	var callAfter entity.CallRecord
	var attemptAfter entity.CallAttempt
	if err := db.First(&callAfter, "request_id = ?", historicalCall.RequestID).Error; err != nil || !reflect.DeepEqual(callAfter, historicalCall) {
		t.Fatal("deletion rewrote call history")
	}
	if err := db.First(&attemptAfter, "id = ?", historicalAttempt.ID).Error; err != nil || !reflect.DeepEqual(attemptAfter, historicalAttempt) {
		t.Fatal("deletion rewrote attempt history")
	}
	for _, model := range []any{&entity.Provider{}, &entity.ProviderConnection{}, &entity.ProviderModel{}, &entity.Model{}, &entity.UserModelGrant{}} {
		var count int64
		if err := db.Model(model).Count(&count).Error; err != nil || count != 1 {
			t.Fatal("credential deletion removed related catalogue or grants")
		}
	}
	if err := db.Model(&entity.CredentialModelAccess{}).Count(&discoveries).Error; err != nil || discoveries != 0 {
		t.Fatal("credential deletion left discovery references")
	}
	// Concurrent metadata change or creation cannot recreate the deleted ID.
	candidate, err := svc.CreateCredential(ctx, admin.User.ID, connection.ID, "Concurrent candidate", "test-only-delete-candidate", 1)
	if err != nil {
		t.Fatal(err)
	}
	candidateMetadata := read(candidate.ID)
	statuses := make(chan int, 2)
	var wg sync.WaitGroup
	wg.Go(func() { statuses <- remove(candidate.ID, candidateMetadata.ETag).Code })
	wg.Go(func() {
		statuses <- request("PUT", candidate.ID+"/metadata", `{"name":"Concurrent rename","priority":1,"reason":"Reviewed concurrent change"}`, strconv.Quote(candidateMetadata.ETag), writeCookie, writeCSRF).Code
	})
	wg.Wait()
	close(statuses)
	counts := map[int]int{}
	for status := range statuses {
		counts[status]++
	}
	if counts[200] != 1 || (counts[404] != 1 && counts[409] != 1) {
		t.Fatalf("concurrent deletion/metadata = %v", counts)
	}
	remaining := request("GET", candidate.ID+"/metadata", "", "", readCookie, "")
	if remaining.Code == 200 {
		latest := decodeCatalogResponse[service.CredentialMetadataRecord](t, remaining, 200)
		confirm(remove(candidate.ID, latest.ETag), candidate.ID)
	} else {
		expectStatus(t, remaining, 404)
	}
	if auditCount(candidate.ID) != 1 {
		t.Fatal("concurrent deletion duplicated audit")
	}
	unknownID, err := id.NewPrefixed("crd")
	if err != nil {
		t.Fatal(err)
	}
	confirm(remove(unknownID, initial.ETag), unknownID)
	if auditCount(unknownID) != 0 {
		t.Fatal("absence reconciliation invented deletion history")
	}
}
