package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/secret"
	"github.com/miclle/routex/pkg/secretstore"
	"gorm.io/gorm"
)

// Share the registered lifecycle's exact live Service and administrator. This
// adds native API acceptance without creating a separate integration selector.
func testProviderStatusNativeLifecycle(t *testing.T, db *gorm.DB, svc *service.Service, router http.Handler, admin SessionResponse, cookie *http.Cookie, failPublication *atomic.Bool) {
	t.Helper()
	ctx := context.Background()
	defer failPublication.Store(false)
	received, finish := make(chan struct{}), make(chan struct{})
	var finishOnce sync.Once
	var workers sync.WaitGroup
	var native atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer test-only-provider-native" {
			t.Error("unexpected native target/authentication")
			w.WriteHeader(400)
			return
		}
		if native.Add(1) == 1 {
			close(received)
			select {
			case <-finish:
			case <-r.Context().Done():
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"provider-status-chat","object":"chat.completion","model":"upstream-provider-status","choices":[{"index":0,"message":{"role":"assistant","content":"OK"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`)
	}))
	defer upstream.Close()
	defer func() { finishOnce.Do(func() { close(finish) }); workers.Wait() }()
	store, err := secretstore.New(bytes.Repeat([]byte{94}, 32))
	if err != nil {
		t.Fatal(err)
	}
	const providerID = "prv_native_status"
	const otherProviderID = "prv_native_other"
	const modelID = "mdl_native_status"
	const childModelID = "mdl_native_child"
	const otherModelID = "mdl_native_other"
	const keyID = "key_native_provider"
	bearer := "rx_" + strings.Repeat("v", 43)
	for _, p := range []entity.Provider{{ID: providerID, Name: "Native status Provider"}, {ID: otherProviderID, Name: "Other native Provider"}} {
		if err := db.Create(&p).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, target := range []struct {
		connection, model, pm, credential, binding, provider, name string
		enabled                                                    bool
	}{
		{"con_native_status", modelID, "pmd_native_status", "crd_native_status", "bnd_native_status", providerID, "provider-status-native", true},
		{"con_native_child", childModelID, "pmd_native_child", "crd_native_child", "bnd_native_child", providerID, "provider-status-child", false},
		{"con_native_other", otherModelID, "pmd_native_other", "crd_native_other", "bnd_native_other", otherProviderID, "provider-status-other", true},
	} {
		cipher, err := store.Seal(target.credential, "test-only-provider-native")
		if err != nil {
			t.Fatal(err)
		}
		mid := target.model
		for _, row := range []any{
			&entity.ProviderConnection{ID: target.connection, ProviderID: target.provider, Name: "Recorded child", BaseURL: upstream.URL + "/v1", Protocol: entity.ProtocolOpenAIChat, EgressMode: "direct", Enabled: true},
			&entity.ProviderCredential{ID: target.credential, ConnectionID: target.connection, Name: "Native credential", Ciphertext: cipher, Enabled: true, VerificationStatus: "verified"},
			&entity.ProviderModel{ID: target.pm, ConnectionID: target.connection, UpstreamName: "upstream-provider-status", SupportsImageInput: true, SupportsPDFInput: true},
			&entity.CredentialModelAccess{CredentialID: target.credential, ProviderModelID: target.pm},
			&entity.Model{ID: mid, Status: entity.ResourceActive},
			&entity.ModelName{Name: target.name, ModelID: mid, CurrentModelID: &mid},
			&entity.ModelProviderBinding{ID: target.binding, ModelID: mid, ProviderModelID: target.pm, Weight: 100},
			&entity.UserModelGrant{UserID: admin.User.ID, ModelID: mid},
		} {
			if err := db.Create(row).Error; err != nil {
				t.Fatal(err)
			}
		}
		if !target.enabled {
			if err := db.Model(&entity.ProviderConnection{}).Where("id = ?", target.connection).Update("enabled", false).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := db.Create(&entity.APIKey{ID: keyID, UserID: admin.User.ID, Name: "Transient native Provider fixture", Prefix: "rx_masked", TokenHash: secret.SHA256Hex(bearer), Status: entity.KeyActive}).Error; err != nil {
		t.Fatal(err)
	}
	for _, mid := range []string{modelID, childModelID, otherModelID} {
		if err := db.Create(&entity.APIKeyModel{KeyID: keyID, ModelID: mid}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := svc.RefreshRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	if err := svc.StartCallRecorder(ctx, filepath.Join(t.TempDir(), "provider-status-native.db")); err != nil {
		t.Fatal(err)
	}
	defer func() {
		finishOnce.Do(func() { close(finish) })
		workers.Wait()
		if err := svc.StopCallRecorder(); err != nil {
			t.Error(err)
		}
	}()
	statusPath := "/api/v1/admin/providers/" + providerID + "/status"
	get := func() service.ProviderStatusRecord {
		t.Helper()
		return decodeCatalogResponse[service.ProviderStatusRecord](t, identityRequest(router, "GET", statusPath, "", cookie, ""), 200)
	}
	write := func(stage, etag string, enabled bool, want int) service.ProviderStatusWriteResult {
		t.Helper()
		raw, _ := json.Marshal(service.ProviderStatusInput{Enabled: enabled, Reason: "Reviewed native Provider status"})
		req := httptest.NewRequest("PUT", statusPath, bytes.NewReader(raw))
		req.AddCookie(cookie)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-CSRF-Token", admin.CSRFToken)
		req.Header.Set("If-Match", strconv.Quote(etag))
		out := httptest.NewRecorder()
		router.ServeHTTP(out, req)
		if want == http.StatusOK && out.Code != want {
			providerStatusNativeFailureDiagnostic(t, db, svc, providerID, stage, enabled, out.Code, native.Load())
		}
		expectStatus(t, out, want)
		if want != 200 {
			return service.ProviderStatusWriteResult{}
		}
		result := decodeCatalogResponse[service.ProviderStatusWriteResult](t, out, 200)
		if !result.RuntimeApplied || result.Provider.Enabled != enabled || result.Provider.ID != providerID {
			t.Fatal("status API failed current application proof")
		}
		return result
	}
	call := func(name string) *httptest.ResponseRecorder {
		callCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		raw, _ := json.Marshal(map[string]any{"model": name, "messages": []map[string]string{{"role": "user", "content": "Native status acceptance"}}})
		req := httptest.NewRequestWithContext(callCtx, "POST", "/v1/chat/completions", bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+bearer)
		out := httptest.NewRecorder()
		router.ServeHTTP(out, req)
		return out
	}
	discovery := func(enabled bool) {
		t.Helper()
		req := httptest.NewRequest("GET", "/v1/models", nil)
		req.Header.Set("Authorization", "Bearer "+bearer)
		out := httptest.NewRecorder()
		router.ServeHTTP(out, req)
		expectStatus(t, out, 200)
		var body struct {
			Data []service.GatewayModel `json:"data"`
		}
		if err := json.Unmarshal(out.Body.Bytes(), &body); err != nil || len(body.Data) != 3 {
			t.Fatal("wrong scoped Model discovery", err)
		}
		for _, row := range body.Data {
			ready := row.ID == "provider-status-other" || row.ID == "provider-status-native" && enabled
			if ready {
				if len(row.Protocols) != 1 || row.Protocols[0] != entity.ProtocolOpenAIChat || len(row.InputCapabilities[entity.ProtocolOpenAIChat]) != 2 {
					t.Fatal("ready native metadata missing", row.ID)
				}
			} else if len(row.Protocols) != 0 || len(row.InputCapabilities) != 0 {
				t.Fatal("disabled Provider/child leaked protocols or capabilities", row.ID)
			}
		}
	}
	discovery(true)
	snapshot := svc.RuntimeStatus().SnapshotID
	done := make(chan *httptest.ResponseRecorder, 1)
	workers.Add(1)
	go func() { defer workers.Done(); done <- call("provider-status-native") }()
	select {
	case <-received:
	case <-time.After(10 * time.Second):
		t.Fatal("native request not received before status commit")
	}
	write("disable_received_request", get().ETag, false, 200)
	discovery(false)
	expectStatus(t, call("provider-status-native"), 503)
	if native.Load() != 1 {
		t.Fatal("disabled Provider admitted new native work", native.Load())
	}
	// The received request owns its immutable attempt and completes after disable.
	finishOnce.Do(func() { close(finish) })
	select {
	case out := <-done:
		expectStatus(t, out, 200)
	case <-time.After(10 * time.Second):
		t.Fatal("received native work did not finish")
	}
	if err := svc.FlushCallRecorder(ctx); err != nil {
		t.Fatal(err)
	}
	var attempts []entity.CallAttempt
	if err := db.Session(&gorm.Session{QueryFields: true}).Where("provider_id = ?", providerID).Find(&attempts).Error; err != nil {
		t.Fatal(err)
	}
	if len(attempts) != 1 {
		t.Fatal("queued denied call created attempted Provider attribution", len(attempts))
	}
	attempt := attempts[0]
	if attempt.Status != "success" || attempt.NativeCompletionEvidence != "completed" || !attempt.FinalUsageKnown || attempt.SnapshotID != snapshot || attempt.CredentialID != "crd_native_status" || attempt.ConnectionID != "con_native_status" || attempt.ProviderModelID != "pmd_native_status" || attempt.ID == "" {
		t.Fatal("received attempt lost exact native attribution/completion")
	}
	// Other Providers remain independently eligible under the same Key.
	expectStatus(t, call("provider-status-other"), 200)
	write("enable_provider", get().ETag, true, 200)
	discovery(true)
	expectStatus(t, call("provider-status-native"), 200)
	expectStatus(t, call("provider-status-child"), 503)
	if native.Load() != 3 {
		t.Fatal("reenable dispatched disabled child or lost independent route", native.Load())
	}
	var child entity.ProviderConnection
	if err := db.Where("id = ?", "con_native_child").Take(&child).Error; err != nil || child.Enabled {
		t.Fatal("Provider enable mutated disabled child", err)
	}
	// A durable disable with an unavailable publication must immediately deny
	// the prior snapshot and must not report runtime application before retry.
	review := get()
	failPublication.Store(true)
	write("disable_publication_unavailable", review.ETag, false, 503)
	expectStatus(t, call("provider-status-native"), 503)
	if native.Load() != 3 {
		t.Fatal("committed disable publication failure revived old snapshot")
	}
	failPublication.Store(false)
	if retry := write("retry_committed_disable", review.ETag, false, 200); retry.Changed {
		t.Fatal("unknown disable retry invented a new historical operation")
	}
	discovery(false)
}

// Failure-only bounded recorded facts identify the unchanged expected200 stage.
// These diagnostics never retry a mutation or infer runtime application.
func providerStatusNativeFailureDiagnostic(t *testing.T, db *gorm.DB, svc *service.Service, providerID, stage string, requested bool, statusCode int, nativeReceived int32) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 750*time.Millisecond)
	defer cancel()
	var current entity.Provider
	read := db.WithContext(ctx).Select("id", "enabled", "e_tag").Where("id = ?", providerID).Take(&current)
	available := read.Error == nil && current.ID == providerID
	var audits int64
	auditRead := db.WithContext(ctx).Model(&entity.AuditEvent{}).Where("action = ? AND resource_type = ? AND resource_id = ?", "provider.status.update", "provider", providerID).Count(&audits)
	revision := "unavailable"
	if available && len(current.ETag) <= 30 && (current.ETag == "0" || strings.HasPrefix(current.ETag, "rev_")) {
		revision = current.ETag
	}
	runtime := svc.RuntimeStatus()
	runtimeCategory := "unknown"
	switch runtime.ErrorCode {
	case "":
		runtimeCategory = "no_recorded_refresh_error"
	case "database_unavailable":
		runtimeCategory = "database_unavailable"
	case "invalid_configuration":
		runtimeCategory = "invalid_configuration"
	}
	confirmation := "current_record_unavailable"
	if available {
		confirmation = "current_target_differs"
		if current.Enabled == requested {
			confirmation = "current_target_matches_application_unproven"
		}
	}
	t.Logf("Provider status failure: stage=%s status=%d requested_enabled=%t native_received=%d current_available=%t current_enabled=%t current_revision=%q audit_count_available=%t audit_count=%d runtime_ready=%t runtime_category=%s confirmation=%s", stage, statusCode, requested, nativeReceived, available, current.Enabled, revision, auditRead.Error == nil, audits, runtime.Ready, runtimeCategory, confirmation)
}
