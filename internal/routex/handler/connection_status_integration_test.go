package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
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
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/secret"
	"github.com/miclle/routex/pkg/secretstore"
)

func testConnectionStatusLifecycle(t *testing.T, db *gorm.DB) {
	ctx := context.Background()
	store, err := secretstore.New(bytes.Repeat([]byte{83}, 32))
	if err != nil {
		t.Fatal(err)
	}
	received := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	var native atomic.Int32
	var nativeWG sync.WaitGroup
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Error("unexpected native path", r.URL.Path)
			w.WriteHeader(404)
			return
		}
		if native.Add(1) == 1 {
			close(received)
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"status-chat","object":"chat.completion","model":"upstream-status","choices":[{"index":0,"message":{"role":"assistant","content":"OK"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`)
	}))
	defer upstream.Close()
	defer releaseOnce.Do(func() { close(release) })
	svc, err := service.New(ctx, db, service.WithCredentialStorage(store), service.WithUpstreamPolicy(true))
	if err != nil {
		t.Fatal(err)
	}
	router := fox.New()
	New(svc).RegisterRoutes(router)
	setup := identityRequest(router, "POST", "/api/v1/setup", `{"email":"connection-status@example.invalid","password":"test-only-status-password","name":"Status admin"}`, nil, "")
	expectStatus(t, setup, 201)
	admin, cookie := readIdentity(t, setup)
	reader, readCookie, readCSRF := createSystemStatusMember(t, svc, router, admin.User.ID, "status-reader", []string{"providers.read"})
	writer, writeCookie, writeCSRF := createSystemStatusMember(t, svc, router, admin.User.ID, "status-writer", []string{"providers.read", "providers.write"})
	_, deniedCookie, _ := createSystemStatusMember(t, svc, router, admin.User.ID, "status-denied", []string{"models.read_all"})
	writer, err = svc.GetMember(ctx, admin.User.ID, writer.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reader.User.ID == writer.User.ID || len(writer.RoleIDs) != 1 {
		t.Fatal("independent actor fixture")
	}
	bearer := "rx_" + strings.Repeat("s", 43)
	modelID := "mdl_status_gate"
	cipher, err := store.Seal("crd_status_gate", "test-only-status-upstream")
	if err != nil {
		t.Fatal(err)
	}
	verified := time.Now().UTC().Truncate(time.Microsecond)
	for _, row := range []any{
		&entity.Provider{ID: "prv_status_gate", Name: "Status provider"},
		&entity.ProviderConnection{ID: "con_status_gate", ProviderID: "prv_status_gate", Name: "Original", Protocol: entity.ProtocolOpenAIChat, BaseURL: upstream.URL + "/v1", EgressMode: "direct", ETag: "rev_status_gate", Enabled: true},
		&entity.ProviderCredential{ID: "crd_status_gate", ConnectionID: "con_status_gate", Name: "Retained", Ciphertext: cipher, Enabled: true, VerificationStatus: "verified", VerifiedAt: &verified},
		&entity.ProviderModel{ID: "pmd_status_gate", ConnectionID: "con_status_gate", UpstreamName: "upstream-status"},
		&entity.CredentialModelAccess{CredentialID: "crd_status_gate", ProviderModelID: "pmd_status_gate"},
		&entity.Model{ID: modelID, Status: "active"},
		&entity.ModelName{Name: "connection-status-model", ModelID: modelID, CurrentModelID: &modelID},
		&entity.ModelProviderBinding{ID: "bnd_status_gate", ModelID: modelID, ProviderModelID: "pmd_status_gate", Weight: 100},
		&entity.UserModelGrant{UserID: admin.User.ID, ModelID: modelID},
		&entity.APIKey{ID: "key_status_gate", UserID: admin.User.ID, Name: "Transient status fixture", Prefix: "rx_masked", TokenHash: secret.SHA256Hex(bearer), Status: entity.KeyActive},
		&entity.APIKeyModel{KeyID: "key_status_gate", ModelID: modelID},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	// Install once before workers; only atomics change while processors execute.
	var failAudit, failPublication atomic.Bool
	const auditFault = "test:connection-status-audit"
	const readFault = "test:connection-status-publication"
	if err := db.Callback().Create().Before("gorm:create").Register(auditFault, func(tx *gorm.DB) {
		if row, ok := tx.Statement.Dest.(*entity.AuditEvent); ok && failAudit.Load() && row.Action == "connection.status.update" {
			_ = tx.AddError(errors.New("controlled status audit unavailable"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Callback().Create().Remove(auditFault) }()
	if err := db.Callback().Query().Before("gorm:query").Register(readFault, func(tx *gorm.DB) {
		if failPublication.Load() && tx.Statement.Table == "models" {
			_ = tx.AddError(errors.New("controlled status publication unavailable"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Callback().Query().Remove(readFault) }()
	spool := filepath.Join(t.TempDir(), "status-calls.db")
	if err := svc.StartRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	if err := svc.StartCallRecorder(ctx, spool); err != nil {
		svc.StopRuntime()
		t.Fatal(err)
	}
	defer func() {
		releaseOnce.Do(func() { close(release) })
		nativeWG.Wait()
		svc.StopRuntime()
		_ = svc.StopCallRecorder()
	}()
	path := "/api/v1/admin/connections/con_status_gate/status"
	request := func(method, target, raw, etag string, session *http.Cookie, csrf string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "http://routex.test"+target, strings.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		if session != nil {
			req.AddCookie(session)
		}
		if csrf != "" {
			req.Header.Set("X-CSRF-Token", csrf)
		}
		if etag != "" {
			req.Header.Set("If-Match", strconv.Quote(etag))
		}
		out := httptest.NewRecorder()
		router.ServeHTTP(out, req)
		return out
	}
	get := func(session *http.Cookie) service.ConnectionStatusRecord {
		t.Helper()
		out := request("GET", path, "", "", session, "")
		row := decodeCatalogResponse[service.ConnectionStatusRecord](t, out, 200)
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(out.Body.Bytes(), &fields); err != nil || len(fields) != 15 || out.Header().Get("ETag") != strconv.Quote(row.ETag) || out.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatal("status15/header", err, out.Body.String())
		}
		for _, key := range []string{"id", "provider_id", "name", "protocol", "base_url", "egress_mode", "egress_id", "etag", "can_edit", "enabled", "adapter", "api_version", "transport_generation", "can_edit_transport", "transport_locked"} {
			if _, ok := fields[key]; !ok {
				t.Fatal("missing status field", key)
			}
		}
		if row.Adapter != "native" || row.APIVersion != nil || string(fields["api_version"]) != "null" {
			t.Fatal("native status adapter/API version contract")
		}
		if row.ID != "con_status_gate" || row.ProviderID != "prv_status_gate" {
			t.Fatal("wrong exact target")
		}
		return row
	}
	put := func(session *http.Cookie, csrf, etag string, enabled bool, status int) service.ConnectionStatusWriteResult {
		t.Helper()
		raw, _ := json.Marshal(service.ConnectionStatusInput{Enabled: enabled, Reason: "Reviewed routing status"})
		out := request("PUT", path, string(raw), etag, session, csrf)
		expectStatus(t, out, status)
		if status != 200 {
			return service.ConnectionStatusWriteResult{}
		}
		row := decodeCatalogResponse[service.ConnectionStatusWriteResult](t, out, 200)
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(out.Body.Bytes(), &fields); err != nil || len(fields) != 3 || !row.RuntimeApplied || row.Connection.Enabled != enabled || !row.Connection.CanEdit || out.Header().Get("ETag") != strconv.Quote(row.Connection.ETag) {
			t.Fatal("exact applied status3", err, out.Body.String())
		}
		return row
	}
	readStored := func() entity.ProviderConnection {
		t.Helper()
		var row entity.ProviderConnection
		if err := db.Session(&gorm.Session{QueryFields: true}).Take(&row, "id = ?", "con_status_gate").Error; err != nil {
			t.Fatal(err)
		}
		return row
	}
	auditCount := func() int64 {
		t.Helper()
		var n int64
		if err := db.Model(&entity.AuditEvent{}).Where("action = ? AND resource_id = ?", "connection.status.update", "con_status_gate").Count(&n).Error; err != nil {
			t.Fatal(err)
		}
		return n
	}
	children := func() string {
		t.Helper()
		var credentials []entity.ProviderCredential
		var pms []entity.ProviderModel
		var bindings []entity.ModelProviderBinding
		var access []entity.CredentialModelAccess
		for _, q := range []*gorm.DB{db.Session(&gorm.Session{QueryFields: true}).Order("id").Find(&credentials), db.Session(&gorm.Session{QueryFields: true}).Order("id").Find(&pms), db.Session(&gorm.Session{QueryFields: true}).Order("id").Find(&bindings), db.Session(&gorm.Session{QueryFields: true}).Order("credential_id, provider_model_id").Find(&access)} {
			if q.Error != nil {
				t.Fatal(q.Error)
			}
		}
		raw, err := json.Marshal([]any{credentials, pms, bindings, access})
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}
	originalChildren := children()
	assertDiscovery := func(enabled bool) {
		t.Helper()
		modelsReq := httptest.NewRequest("GET", "/v1/models", nil)
		modelsReq.Header.Set("Authorization", "Bearer "+bearer)
		modelsOut := httptest.NewRecorder()
		router.ServeHTTP(modelsOut, modelsReq)
		expectStatus(t, modelsOut, 200)
		var discovered struct {
			Data []service.GatewayModel `json:"data"`
		}
		if err := json.Unmarshal(modelsOut.Body.Bytes(), &discovered); err != nil || len(discovered.Data) != 1 || discovered.Data[0].ID != "connection-status-model" {
			t.Fatal("exact authorized logical Model discovery", err)
		}
		model := discovered.Data[0]
		if enabled {
			capabilities, present := model.InputCapabilities[entity.ProtocolOpenAIChat]
			if !reflect.DeepEqual(model.Protocols, []string{entity.ProtocolOpenAIChat}) || len(model.InputCapabilities) != 1 || !present || capabilities == nil || len(capabilities) != 0 {
				t.Fatal("enabled supply missing exact Chat protocol/capability projection", model)
			}
		} else if model.Protocols == nil || len(model.Protocols) != 0 || model.InputCapabilities == nil || len(model.InputCapabilities) != 0 {
			t.Fatal("disabled supply leaked native protocol or capabilities", model)
		}
	}
	assertDiscovery(true)
	if row := get(readCookie); row.CanEdit || !row.Enabled {
		t.Fatal("reader editability/default")
	}
	expectStatus(t, request("GET", path, "", "", deniedCookie, ""), 403)
	put(readCookie, readCSRF, get(readCookie).ETag, false, 403)
	initial := get(cookie)
	expectStatus(t, request("PUT", path, `{"enabled":null,"reason":"R"}`, initial.ETag, cookie, admin.CSRFToken), 400)
	oldMeta := decodeCatalogResponse[service.ConnectionMetadataRecord](t, request("GET", "/api/v1/admin/connections/con_status_gate/metadata", "", "", cookie, ""), 200)
	oldCatalog := decodeCatalogResponse[ProvidersResponse](t, request("GET", "/api/v1/admin/providers", "", "", cookie, ""), 200)
	if len(oldCatalog.Items) != 1 || len(oldCatalog.Items[0].Connections) != 1 {
		t.Fatal("bounded initial catalogue")
	}
	failAudit.Store(true)
	put(cookie, admin.CSRFToken, initial.ETag, false, 503)
	failAudit.Store(false)
	if !readStored().Enabled || readStored().ETag != "rev_status_gate" || auditCount() != 0 {
		t.Fatal("audit failure did not rollback exact status")
	}
	failPublication.Store(true)
	put(cookie, admin.CSRFToken, initial.ETag, false, 503)
	failPublication.Store(false)
	if readStored().Enabled || auditCount() != 1 {
		t.Fatal("committed uncertain false not retained")
	}
	retry := put(cookie, admin.CSRFToken, initial.ETag, false, 200)
	if retry.Changed || auditCount() != 1 {
		t.Fatal("identical uncertainty retry duplicated audit")
	}
	raw, _ := json.Marshal(service.ConnectionMetadataInput{Name: "Should conflict", Reason: "Old metadata review"})
	expectStatus(t, request("PUT", "/api/v1/admin/connections/con_status_gate/metadata", string(raw), oldMeta.ETag, cookie, admin.CSRFToken), 409)
	egress, _ := json.Marshal(service.ConnectionEgressInput{ETag: oldCatalog.Items[0].Connections[0].ETag, Mode: "direct"})
	expectStatus(t, request("PATCH", "/api/v1/admin/connections/con_status_gate/egress", string(egress), "", cookie, admin.CSRFToken), 409)
	// A write-only actor retains its own reviewed token; current read loss cannot
	// grant GET but independent current write authority can confirm the PUT.
	writeReview := get(writeCookie)
	if _, err := svc.SaveRole(ctx, admin.User.ID, writer.RoleIDs[0], "System status-writer", []string{"providers.write"}); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, request("GET", path, "", "", writeCookie, ""), 403)
	if !put(writeCookie, writeCSRF, writeReview.ETag, true, 200).Changed {
		t.Fatal("independent writer failed")
	}
	assertDiscovery(true)
	// Capture a real received upstream request, then disable while its response is held.
	snapshot := svc.RuntimeStatus().SnapshotID
	nativeCall := func() *httptest.ResponseRecorder {
		callCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		req := httptest.NewRequestWithContext(callCtx, "POST", "/v1/chat/completions", strings.NewReader(`{"model":"connection-status-model","messages":[{"role":"user","content":"hello"}]}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+bearer)
		out := httptest.NewRecorder()
		router.ServeHTTP(out, req)
		return out
	}
	done := make(chan *httptest.ResponseRecorder, 1)
	nativeWG.Add(1)
	go func() { defer nativeWG.Done(); done <- nativeCall() }()
	select {
	case <-received:
	case <-time.After(10 * time.Second):
		t.Fatal("native request did not reach controlled upstream")
	}
	stopped := put(cookie, admin.CSRFToken, get(cookie).ETag, false, 200)
	if !stopped.Changed || readStored().Enabled {
		t.Fatal("explicit false not persisted")
	}
	releaseOnce.Do(func() { close(release) })
	select {
	case out := <-done:
		expectStatus(t, out, 200)
	case <-time.After(10 * time.Second):
		t.Fatal("held received call failed to finish")
	}
	denied := nativeCall()
	expectStatus(t, denied, 503)
	if native.Load() != 1 {
		t.Fatal("disabled Connection admitted another upstream")
	}
	if err := svc.FlushCallRecorder(ctx); err != nil {
		t.Fatal(err)
	}
	var calls []entity.CallRecord
	var attempts []entity.CallAttempt
	if err := db.Session(&gorm.Session{QueryFields: true}).Order("request_id").Find(&calls).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Session(&gorm.Session{QueryFields: true}).Order("id").Find(&attempts).Error; err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 || len(attempts) != 1 {
		t.Fatal("exact received+blocked call/attempt budget", len(calls), len(attempts))
	}
	attempt := attempts[0]
	if attempt.Status != "success" || attempt.NativeCompletionEvidence != "completed" || attempt.CredentialID != "crd_status_gate" || attempt.ConnectionID != "con_status_gate" || attempt.ProviderModelID != "pmd_status_gate" || attempt.SnapshotID != snapshot || attempt.ID == "" || !attempt.FinalUsageKnown {
		t.Fatal("immutable received native attribution/completion", attempt)
	}
	for _, row := range calls {
		if row.Status == "success" {
			if row.InputTokens == nil || *row.InputTokens != 3 || row.OutputTokens == nil || *row.OutputTokens != 2 || row.SnapshotID != snapshot {
				t.Fatal("known native settlement")
			}
		} else {
			if row.InputTokens != nil || row.OutputTokens != nil || row.ConnectionID != "" {
				t.Fatal("blocked call fabricated usage/route")
			}
			for _, a := range attempts {
				if a.RequestID == row.RequestID {
					t.Fatal("blocked call gained Attempt")
				}
			}
		}
	}
	if children() != originalChildren {
		t.Fatal("disable modified Credential/model/access/weights")
	}
	// Disabled Connections retain the same administrative metadata projection.
	out := request("GET", "/api/v1/admin/connections/con_status_gate/metadata", "", "", cookie, "")
	var meta map[string]json.RawMessage
	if err := json.Unmarshal(out.Body.Bytes(), &meta); err != nil || len(meta) != 14 {
		t.Fatal("disabled metadata14 unavailable", err)
	}
	for _, key := range []string{"id", "provider_id", "name", "protocol", "base_url", "egress_mode", "egress_id", "etag", "can_edit", "adapter", "api_version", "transport_generation", "can_edit_transport", "transport_locked"} {
		if _, ok := meta[key]; !ok {
			t.Fatal("missing disabled metadata field", key)
		}
	}
	if string(meta["adapter"]) != `"native"` || string(meta["api_version"]) != "null" {
		t.Fatal("disabled native metadata adapter/API version contract")
	}
	// Logical grant visibility survives; disabled supply advertises no usable protocol.
	assertDiscovery(false)
	stored := readStored()
	priorCalls := append([]entity.CallRecord(nil), calls...)
	priorAttempts := append([]entity.CallAttempt(nil), attempts...)
	svc.StopRuntime()
	if err := svc.StopCallRecorder(); err != nil {
		t.Fatal(err)
	}
	restarted, err := service.New(ctx, db, service.WithCredentialStorage(store), service.WithUpstreamPolicy(true))
	if err != nil {
		t.Fatal(err)
	}
	if err := restarted.StartRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	defer restarted.StopRuntime()
	if err := restarted.StartCallRecorder(ctx, spool); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = restarted.StopCallRecorder() }()
	svc = restarted
	router = fox.New()
	New(svc).RegisterRoutes(router)
	if get(cookie).Enabled || !connectionEnablementSameRow(stored, readStored()) || children() != originalChildren {
		t.Fatal("restart restored or changed disabled status/children")
	}
	assertDiscovery(false)
	if err := svc.FlushCallRecorder(ctx); err != nil {
		t.Fatal(err)
	}
	calls = nil
	attempts = nil
	if err := db.Session(&gorm.Session{QueryFields: true}).Order("request_id").Find(&calls).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Session(&gorm.Session{QueryFields: true}).Order("id").Find(&attempts).Error; err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(priorCalls, calls) || !reflect.DeepEqual(priorAttempts, attempts) || native.Load() != 1 {
		t.Fatal("restart changed immutable history or dispatched upstream")
	}
}
