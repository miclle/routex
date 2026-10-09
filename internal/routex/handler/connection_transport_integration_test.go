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
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/secret"
	"github.com/miclle/routex/pkg/secretstore"
	"gorm.io/gorm"
)

// Unregistered source: the root owns the additive 179-case integration selector.
func testConnectionTransportLifecycle(t *testing.T, db *gorm.DB) {
	ctx := context.Background()
	store, e := secretstore.New(bytes.Repeat([]byte{102}, 32))
	if e != nil {
		t.Fatal(e)
	}
	received, finish := make(chan struct{}), make(chan struct{})
	var release sync.Once
	var workers sync.WaitGroup
	var nativeA, nativeB, discovery atomic.Int32
	server := func(isA bool) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer test-only-transport-key" {
				t.Error("unexpected credential")
				w.WriteHeader(401)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			if r.URL.Path == "/v1/models" {
				discovery.Add(1)
				_, _ = io.WriteString(w, `{"data":[{"id":"transport-upstream"}]}`)
				return
			}
			if r.URL.Path != "/v1/chat/completions" {
				t.Error("unexpected native target")
				w.WriteHeader(400)
				return
			}
			if isA {
				if nativeA.Add(1) == 1 {
					close(received)
					select {
					case <-finish:
					case <-r.Context().Done():
						return
					}
				}
			} else {
				nativeB.Add(1)
			}
			_, _ = io.WriteString(w, `{"id":"transport-native","object":"chat.completion","model":"transport-upstream","choices":[{"index":0,"message":{"role":"assistant","content":"OK"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`)
		}))
	}
	a, b := server(true), server(false)
	defer a.Close()
	defer b.Close()
	defer func() { release.Do(func() { close(finish) }); workers.Wait() }()
	svc, e := service.New(ctx, db, service.WithCredentialStorage(store), service.WithUpstreamPolicy(true), service.WithRuntimeRefreshInterval(time.Hour))
	if e != nil {
		t.Fatal(e)
	}
	router := fox.New()
	New(svc).RegisterRoutes(router)
	setup := identityRequest(router, "POST", "/api/v1/setup", `{"email":"transport@example.invalid","password":"test-only-transport-password","name":"Transport administrator"}`, nil, "")
	expectStatus(t, setup, 201)
	admin, cookie := readIdentity(t, setup)
	_, readerCookie, readerCSRF := createSystemStatusMember(t, svc, router, admin.User.ID, "transport-reader", []string{"providers.read"})
	cipher, e := store.Seal("crd_transport_live", "test-only-transport-key")
	if e != nil {
		t.Fatal(e)
	}
	mid := "mdl_transport_live"
	bearer := "rx_" + strings.Repeat("t", 43)
	for _, row := range []any{&entity.Provider{ID: "prv_transport_live", Name: "Transport supplier"}, &entity.ProviderConnection{ID: "con_transport_live", ProviderID: "prv_transport_live", Name: "Original destination", BaseURL: a.URL + "/v1", Protocol: entity.ProtocolOpenAIChat, Adapter: entity.AdapterNative, EgressMode: "direct"}, &entity.ProviderCredential{ID: "crd_transport_live", ConnectionID: "con_transport_live", Name: "Retained credential", Ciphertext: cipher, Enabled: true, VerificationStatus: "verified"}, &entity.ProviderModel{ID: "pmd_transport_live", ConnectionID: "con_transport_live", UpstreamName: "transport-upstream", SupportsImageInput: true, SupportsPDFInput: true}, &entity.CredentialModelAccess{CredentialID: "crd_transport_live", ProviderModelID: "pmd_transport_live"}, &entity.Model{ID: mid, Status: entity.ResourceActive}, &entity.ModelName{Name: "transport-public", ModelID: mid, CurrentModelID: &mid}, &entity.ModelProviderBinding{ID: "bnd_transport_live", ModelID: mid, ProviderModelID: "pmd_transport_live", Weight: 100}, &entity.UserModelGrant{UserID: admin.User.ID, ModelID: mid}, &entity.APIKey{ID: "key_transport_live", UserID: admin.User.ID, Name: "Retained key", Prefix: "rx_masked", TokenHash: secret.SHA256Hex(bearer), Status: entity.KeyActive}, &entity.APIKeyModel{KeyID: "key_transport_live", ModelID: mid}, &entity.ReservationBound{ProviderModelID: "pmd_transport_live", Protocol: entity.ProtocolOpenAIChat, ETag: "bnd_retained", PreviousETag: "0", ActorID: admin.User.ID, Reason: "Original attestation", Evidence: "Native capacity", MaxInputTokens: 100, MaxOutputTokens: 20}} {
		if e := db.Create(row).Error; e != nil {
			t.Fatal(e)
		}
	}
	var publicationFailure, auditFailure atomic.Bool
	callback := "test:connection_transport_publication"
	if e := db.Callback().Query().Before("gorm:query").Register(callback, func(tx *gorm.DB) {
		if publicationFailure.Load() && tx.Statement.Table == "models" {
			_ = tx.AddError(errors.New("controlled unavailable publication"))
		}
	}); e != nil {
		t.Fatal(e)
	}
	defer func() { _ = db.Callback().Query().Remove(callback) }()
	auditCallback := "test:connection_transport_atomic_audit"
	if e := db.Callback().Create().Before("gorm:create").Register(auditCallback, func(tx *gorm.DB) {
		row, ok := tx.Statement.Dest.(*entity.AuditEvent)
		if auditFailure.Load() && ok && row.Action == "connection.transport.update" {
			_ = tx.AddError(errors.New("controlled unavailable transport audit"))
		}
	}); e != nil {
		t.Fatal(e)
	}
	defer func() { _ = db.Callback().Create().Remove(auditCallback) }()
	if e := svc.StartRuntime(ctx); e != nil {
		t.Fatal(e)
	}
	defer func() { svc.StopRuntime() }()
	journal := filepath.Join(t.TempDir(), "transport-native.db")
	if e := svc.StartCallRecorder(ctx, journal); e != nil {
		t.Fatal(e)
	}
	defer func() {
		release.Do(func() { close(finish) })
		workers.Wait()
		if e := svc.StopCallRecorder(); e != nil {
			t.Error(e)
		}
	}()
	request := func(method, path string, body any, etag string, session *http.Cookie, csrf string) *httptest.ResponseRecorder {
		t.Helper()
		raw, e := json.Marshal(body)
		if e != nil {
			t.Fatal(e)
		}
		req := httptest.NewRequest(method, "http://routex.test"+path, bytes.NewReader(raw))
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
	metadataPath := "/api/v1/admin/connections/con_transport_live/metadata"
	statusPath := "/api/v1/admin/connections/con_transport_live/status"
	pmPath := "/api/v1/admin/provider-models/pmd_transport_live"
	capacityPath := pmPath + "/reservation-bound"
	get := func() service.ConnectionMetadataRecord {
		t.Helper()
		res := request("GET", metadataPath, nil, "", cookie, "")
		row := decodeCatalogResponse[service.ConnectionMetadataRecord](t, res, 200)
		if res.Header().Get("ETag") != strconv.Quote(row.ETag) || res.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatal("metadata private strong review")
		}
		return row
	}
	status := func(enabled bool) {
		t.Helper()
		row := decodeCatalogResponse[service.ConnectionStatusRecord](t, request("GET", statusPath, nil, "", cookie, ""), 200)
		saved := decodeCatalogResponse[service.ConnectionStatusWriteResult](t, request("PUT", statusPath, map[string]any{"enabled": enabled, "reason": "Explicit parent status"}, row.ETag, cookie, admin.CSRFToken), 200)
		if !saved.RuntimeApplied || saved.Connection.Enabled != enabled {
			t.Fatal("parent status proof")
		}
	}
	put := func(row service.ConnectionMetadataRecord, url, protocol string, want int) service.ConnectionMetadataWriteResult {
		t.Helper()
		res := request("PUT", metadataPath, map[string]any{"name": row.Name, "reason": "Reviewed transport destination", "transport": map[string]any{"base_url": url, "protocol": protocol, "adapter": "native", "api_version": nil}}, row.ETag, cookie, admin.CSRFToken)
		expectStatus(t, res, want)
		if want != 200 {
			return service.ConnectionMetadataWriteResult{}
		}
		saved := decodeCatalogResponse[service.ConnectionMetadataWriteResult](t, res, 200)
		if !saved.RuntimeApplied || !saved.Connection.CanEditTransport || !saved.Connection.TransportLocked {
			t.Fatal("saved target application/precondition")
		}
		return saved
	}
	model := func() ProviderModelResponse {
		t.Helper()
		list := decodeCatalogResponse[ProvidersResponse](t, request("GET", "/api/v1/admin/providers", nil, "", cookie, ""), 200)
		for _, p := range list.Items {
			for _, c := range p.Connections {
				for _, pm := range c.ProviderModels {
					if pm.ID == "pmd_transport_live" {
						return pm
					}
				}
			}
		}
		t.Fatal("exact model missing")
		return ProviderModelResponse{}
	}
	bound := func() service.ReservationBoundRecord {
		t.Helper()
		res := request("GET", capacityPath, nil, "", cookie, "")
		row := decodeCatalogResponse[service.ReservationBoundRecord](t, res, 200)
		if len(row.ETag) != 64 || res.Header().Get("ETag") != strconv.Quote(row.ETag) || res.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatal("capacity reviewed token")
		}
		return row
	}
	capability := func(pm ProviderModelResponse, want int) {
		t.Helper()
		expectStatus(t, request("PATCH", pmPath, map[string]any{"etag": pm.ETag, "capability_review_etag": pm.CapabilityReviewETag, "supports_image_input": true, "supports_pdf_input": true}, "", cookie, admin.CSRFToken), want)
	}
	capacity := func(review string, want int) {
		t.Helper()
		expectStatus(t, request("PUT", capacityPath, map[string]any{"max_input_tokens": 100, "max_output_tokens": 20, "evidence": "Native capacity", "reason": "Review current destination capacity"}, review, cookie, admin.CSRFToken), want)
	}
	call := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "http://routex.test/v1/chat/completions", strings.NewReader(`{"model":"transport-public","messages":[{"role":"user","content":"transport evidence"}]}`))
		req.Header.Set("Authorization", "Bearer "+bearer)
		req.Header.Set("Content-Type", "application/json")
		out := httptest.NewRecorder()
		router.ServeHTTP(out, req)
		return out
	}
	protocols := func(want int) {
		t.Helper()
		items, e := svc.GatewayModels(ctx, bearer)
		if e != nil || len(items) != 1 || len(items[0].Protocols) != want {
			t.Fatal("effective discovery", e, items)
		}
	}
	auditCount := func() int64 {
		t.Helper()
		var n int64
		if e := db.Model(&entity.AuditEvent{}).Where("action = ? AND resource_id = ?", "connection.transport.update", "con_transport_live").Count(&n).Error; e != nil {
			t.Fatal(e)
		}
		return n
	}
	type children struct {
		Credential entity.ProviderCredential
		Model      entity.ProviderModel
		Bound      entity.ReservationBound
		Access     []entity.CredentialModelAccess
		Binding    entity.ModelProviderBinding
		Grant      entity.UserModelGrant
	}
	readChildren := func() children {
		t.Helper()
		var rows children
		for _, item := range []struct {
			dest       any
			column, id string
		}{{&rows.Credential, "id", "crd_transport_live"}, {&rows.Model, "id", "pmd_transport_live"}, {&rows.Bound, "provider_model_id", "pmd_transport_live"}, {&rows.Binding, "id", "bnd_transport_live"}, {&rows.Grant, "user_id", admin.User.ID}} {
			if e := db.Where(item.column+" = ?", item.id).Take(item.dest).Error; e != nil {
				t.Fatal(e)
			}
		}
		if e := db.Where("credential_id = ?", "crd_transport_live").Find(&rows.Access).Error; e != nil {
			t.Fatal(e)
		}
		return rows
	}
	original := readChildren()
	oldBound := bound()
	initial := get()
	if initial.CanEditTransport || !initial.TransportLocked || !model().CapabilitiesTransportCurrent || !oldBound.TransportCurrent {
		t.Fatal("legacy recorded proof projection")
	}
	protocols(1)
	put(initial, b.URL+"/v1", entity.ProtocolOpenAIChat, 409)
	if auditCount() != 0 {
		t.Fatal("enabled transport mutated")
	}
	held := make(chan *httptest.ResponseRecorder, 1)
	workers.Go(func() { held <- call() })
	select {
	case <-received:
	case <-time.After(10 * time.Second):
		t.Fatal("native admission not received")
	}
	snapshot := svc.RuntimeStatus().SnapshotID
	status(false)
	review := get()
	if !review.CanEditTransport {
		t.Fatal("disabled edit authority absent")
	}
	expectStatus(t, request("PUT", metadataPath, map[string]any{"name": review.Name, "reason": "Denied read-only mutation", "transport": map[string]any{"base_url": b.URL + "/v1", "protocol": entity.ProtocolOpenAIChat, "adapter": "native", "api_version": nil}}, review.ETag, readerCookie, readerCSRF), 403)
	put(review, b.URL+"/v1", entity.ProtocolOpenAIResponses, 409)
	auditFailure.Store(true)
	put(review, b.URL+"/v1", entity.ProtocolOpenAIChat, 500)
	auditFailure.Store(false)
	if get().ETag != review.ETag || auditCount() != 0 || !reflect.DeepEqual(original, readChildren()) {
		t.Fatal("audit failure committed transport or changed children")
	}
	changed := put(review, b.URL+"/v1", entity.ProtocolOpenAIChat, 200)
	if !changed.Changed || changed.Connection.TransportGeneration == "0" || auditCount() != 1 || !reflect.DeepEqual(original, readChildren()) {
		t.Fatal("transport edit mutated children or failed generation/audit")
	}
	put(review, b.URL+"/v1", entity.ProtocolOpenAIChat, 409)
	release.Do(func() { close(finish) })
	select {
	case out := <-held:
		expectStatus(t, out, 200)
	case <-time.After(10 * time.Second):
		t.Fatal("held original attempt did not finish")
	}
	expectStatus(t, call(), 503)
	protocols(0)
	if nativeA.Load() != 1 || nativeB.Load() != 0 {
		t.Fatal("new native dispatch slipped across edit")
	}
	verify, e := svc.VerifyCredential(ctx, admin.User.ID, "crd_transport_live")
	if e != nil || !verify.Verified || discovery.Load() != 1 {
		t.Fatal("explicit current verification", e)
	}
	if model().CapabilitiesTransportCurrent {
		t.Fatal("verification stamped retained capability")
	}
	pm := model()
	expectStatus(t, request("PATCH", pmPath, map[string]any{"etag": pm.ETag, "enabled": true}, "", cookie, admin.CSRFToken), 200)
	if model().CapabilitiesTransportCurrent {
		t.Fatal("availability toggle stamped stale capability")
	}
	capacity(oldBound.ETag, 409)
	capability(model(), 200)
	capacity(bound().ETag, 200)
	if !model().CapabilitiesTransportCurrent || !bound().TransportCurrent {
		t.Fatal("explicit complete pair/capacity failed")
	}
	status(true)
	expectStatus(t, call(), 200)
	if nativeB.Load() != 1 {
		t.Fatal("new eligible destination not used")
	}
	protocols(1)
	status(false)
	abaReview := get()
	stalePM, staleBound := model(), bound()
	prior := readChildren()
	publicationFailure.Store(true)
	put(abaReview, a.URL+"/v1", entity.ProtocolOpenAIChat, 503)
	publicationFailure.Store(false)
	current := get()
	if current.TransportGeneration == changed.Connection.TransportGeneration || current.TransportGeneration == "0" || auditCount() != 2 || !reflect.DeepEqual(prior, readChildren()) {
		t.Fatal("ABA/uncertainty lost committed facts")
	}
	put(abaReview, a.URL+"/v1", entity.ProtocolOpenAIChat, 409)
	noOp := put(current, a.URL+"/v1", entity.ProtocolOpenAIChat, 200)
	if noOp.Changed || auditCount() != 2 {
		t.Fatal("new current review no-op invented historical receipt/audit")
	}
	capability(stalePM, 409)
	capacity(staleBound.ETag, 409)
	status(true)
	expectStatus(t, call(), 503)
	protocols(0)
	if nativeA.Load() != 1 || nativeB.Load() != 1 {
		t.Fatal("ABA revived old verification")
	}
	// Replace the Service with the same database/storage and original Session/Key.
	if e := svc.StopCallRecorder(); e != nil {
		t.Fatal(e)
	}
	svc.StopRuntime()
	svc, e = service.New(ctx, db, service.WithCredentialStorage(store), service.WithUpstreamPolicy(true), service.WithRuntimeRefreshInterval(time.Hour))
	if e != nil {
		t.Fatal(e)
	}
	router = fox.New()
	New(svc).RegisterRoutes(router)
	if e := svc.StartRuntime(ctx); e != nil {
		t.Fatal(e)
	}
	if e := svc.StartCallRecorder(ctx, journal); e != nil {
		t.Fatal(e)
	}
	expectStatus(t, request("GET", metadataPath, nil, "", cookie, ""), 200)
	expectStatus(t, call(), 503)
	protocols(0)
	status(false)
	verify, e = svc.VerifyCredential(ctx, admin.User.ID, "crd_transport_live")
	if e != nil || !verify.Verified {
		t.Fatal("explicit ABA verification", e)
	}
	capability(model(), 200)
	capacity(bound().ETag, 200)
	status(true)
	expectStatus(t, call(), 200)
	if nativeA.Load() != 2 || nativeB.Load() != 1 || discovery.Load() != 2 {
		t.Fatal("unexpected native/verification request totals")
	}
	// Deliver completed journal facts before asserting immutable SQL history.
	if e := svc.FlushCallRecorder(ctx); e != nil {
		t.Fatal(e)
	}
	if e := svc.StopCallRecorder(); e != nil {
		t.Fatal(e)
	}
	var attempts []entity.CallAttempt
	if e := db.Where("provider_model_id = ?", "pmd_transport_live").Order("started_at,id").Find(&attempts).Error; e != nil {
		t.Fatal(e)
	}
	if len(attempts) != 3 || attempts[0].CredentialID != "crd_transport_live" || attempts[0].ConnectionID != "con_transport_live" || attempts[0].SnapshotID != snapshot {
		t.Logf("observed attempts=%d expected attempts=3 held snapshot=%q", len(attempts), snapshot)
		for _, attempt := range attempts {
			t.Logf("attempt id=%q credential_id=%q connection_id=%q snapshot_id=%q", attempt.ID, attempt.CredentialID, attempt.ConnectionID, attempt.SnapshotID)
		}
		t.Fatal("held attempt identity/snapshot history changed")
	}
	for _, attempt := range attempts {
		if attempt.NativeCompletionEvidence != "completed" {
			t.Fatal("native history lost terminal evidence")
		}
	}
	if auditCount() != 2 {
		t.Fatal("recovery rewrote transport command history")
	}
}
