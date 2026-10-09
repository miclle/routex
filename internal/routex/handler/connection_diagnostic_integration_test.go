package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fox-gonic/fox"
	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/secretstore"
)

// The integration owner registers this additive scenario after the current
// frozen matrix has completed. It starts no runtime or call recorder.
func testConnectionDiagnosticLifecycle(t *testing.T, db *gorm.DB) {
	ctx := context.Background()
	store, err := secretstore.New(bytes.Repeat([]byte{94}, 32))
	if err != nil {
		t.Fatal(err)
	}
	var gets, inference atomic.Int32
	var mode atomic.Int32
	var mutate atomic.Bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			inference.Add(1)
			w.WriteHeader(500)
			return
		}
		gets.Add(1)
		if mutate.Swap(false) {
			if err := db.Model(&entity.ProviderConnection{}).Where("id = ?", "con_diagnostic_chat").UpdateColumn("e_tag", "changed_during_test").Error; err != nil {
				t.Error("controlled Connection change failed")
			}
		}
		w.Header().Set("Content-Type", "application/json")
		switch mode.Load() {
		case 1:
			w.WriteHeader(401)
			_, _ = w.Write([]byte(`{"error":"test-only-private-upstream-error"}`))
			return
		case 2:
			_, _ = w.Write([]byte(`{"data":[{"id":"a","type":"model"}],"has_more":true,"last_id":"a"}`))
			return
		case 3:
			w.Header().Set("Location", "/redirected")
			w.WriteHeader(302)
			return
		case 4:
			_, _ = w.Write([]byte(strings.Repeat(" ", 2*1024*1024+1)))
			return
		}
		switch r.URL.Path {
		case "/azure/openai/models", "/openai/models":
			if r.Header.Get("api-key") != "test-only-selected-diagnostic-key" || r.Header.Get("Authorization") != "" || r.URL.Query().Get("api-version") != "2024-10-21" {
				t.Error("Azure native authentication changed")
			}
			_, _ = w.Write([]byte(`{"data":[{"id":"unattested-deployment"}]}`))
		case "/messages/models":
			if r.Header.Get("x-api-key") != "test-only-selected-diagnostic-key" || r.Header.Get("anthropic-version") != "2023-06-01" {
				t.Error("Messages native authentication changed")
			}
			_, _ = w.Write([]byte(`{"data":[{"id":"native-a","type":"model"}],"has_more":false}`))
		case "/gemini/models":
			if r.Header.Get("x-goog-api-key") != "test-only-selected-diagnostic-key" {
				t.Error("Gemini native authentication changed")
			}
			_, _ = w.Write([]byte(`{"models":[{"name":"models/native-a","supportedGenerationMethods":["generateContent"]},{"name":"models/embed-a","supportedGenerationMethods":["embedContent"]}]}`))
		case "/chat/models", "/responses/models":
			if r.Header.Get("Authorization") != "Bearer test-only-selected-diagnostic-key" {
				t.Error("test selected an implicit pool credential")
			}
			_, _ = w.Write([]byte(`{"data":[{"id":"native-a"},{"id":"native-a"}]}`))
		default:
			inference.Add(1)
			w.WriteHeader(404)
		}
	}))
	defer upstream.Close()
	svc, err := service.New(ctx, db, service.WithCredentialStorage(store), service.WithUpstreamPolicy(true))
	if err != nil {
		t.Fatal(err)
	}
	router := fox.New()
	New(svc).RegisterRoutes(router)
	setup := identityRequest(router, "POST", "/api/v1/setup", `{"email":"connection-diagnostic@example.invalid","password":"test-only-diagnostic-password","name":"Diagnostic admin"}`, nil, "")
	expectStatus(t, setup, 201)
	admin, cookie := readIdentity(t, setup)
	_, readCookie, readCSRF := createSystemStatusMember(t, svc, router, admin.User.ID, "diagnostic-reader", []string{"providers.read"})
	_, writeCookie, writeCSRF := createSystemStatusMember(t, svc, router, admin.User.ID, "diagnostic-writer", []string{"providers.write"})
	_, deniedCookie, deniedCSRF := createSystemStatusMember(t, svc, router, admin.User.ID, "diagnostic-denied", []string{"models.read_all"})
	provider := entity.Provider{ID: "prv_diagnostic", Name: "Test provider"}
	if err := db.Create(&provider).Error; err != nil {
		t.Fatal(err)
	}
	// The creation default is enabled; persist and read back disabled state so
	// every native diagnostic proves stored transport is independent of routing.
	disabledProvider := db.Model(&provider).Update("enabled", false)
	if disabledProvider.Error != nil || disabledProvider.RowsAffected != 1 {
		t.Fatal("disabled diagnostic Provider was not persisted", disabledProvider.Error)
	}
	var recordedProvider entity.Provider
	if err := db.Select("id", "enabled").Where("id = ?", provider.ID).First(&recordedProvider).Error; err != nil || recordedProvider.ID != provider.ID || recordedProvider.Enabled {
		t.Fatal("diagnostic Provider is not recorded disabled", err)
	}
	type nativeCase struct {
		suffix, protocol, adapter string
		version                   *string
	}
	version := "2024-10-21"
	cases := []nativeCase{{"chat", entity.ProtocolOpenAIChat, entity.AdapterNative, nil}, {"responses", entity.ProtocolOpenAIResponses, entity.AdapterNative, nil}, {"messages", entity.ProtocolAnthropicMessages, entity.AdapterNative, nil}, {"gemini", entity.ProtocolGeminiGenerateContent, entity.AdapterNative, nil}, {"azure", entity.ProtocolOpenAIChat, entity.AdapterAzureOpenAIClassic, &version}}
	for _, test := range cases {
		connectionID, credentialID := "con_diagnostic_"+test.suffix, "crd_diagnostic_"+test.suffix
		base := upstream.URL + "/" + test.suffix
		if test.suffix == "azure" {
			base = upstream.URL
		}
		connection := entity.ProviderConnection{ID: connectionID, ProviderID: provider.ID, Name: test.suffix, BaseURL: base, Protocol: test.protocol, Adapter: test.adapter, APIVersion: test.version, EgressMode: "direct", ETag: "diagnostic_initial"}
		if err := db.Create(&connection).Error; err != nil {
			t.Fatal(err)
		}
		// Persist false explicitly despite the creation default. Tests remain
		// permitted for stored transport; none claims routing eligibility.
		if err := db.Model(&connection).Update("enabled", false).Error; err != nil {
			t.Fatal(err)
		}
		cipher, err := store.Seal(credentialID, "test-only-selected-diagnostic-key")
		if err != nil {
			t.Fatal(err)
		}
		credential := entity.ProviderCredential{ID: credentialID, ConnectionID: connectionID, Name: "Explicit pending", Ciphertext: cipher, StorageSource: "inline", Priority: 99, Enabled: false, VerificationStatus: "pending"}
		if err := db.Create(&credential).Error; err != nil {
			t.Fatal(err)
		}
		otherID := "crd_diag_pool_" + test.suffix
		otherCipher, err := store.Seal(otherID, "test-only-unselected-pool-key")
		if err != nil {
			t.Fatal(err)
		}
		other := entity.ProviderCredential{ID: otherID, ConnectionID: connectionID, Name: "Earlier pool candidate", Ciphertext: otherCipher, Priority: 0, Enabled: true, VerificationStatus: "verified"}
		if err := db.Create(&other).Error; err != nil {
			t.Fatal(err)
		}
		model := entity.ProviderModel{ID: "pmd_diagnostic_" + test.suffix, ConnectionID: connectionID, UpstreamName: "retained-model"}
		if err := db.Create(&model).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&entity.CredentialModelAccess{CredentialID: otherID, ProviderModelID: model.ID}).Error; err != nil {
			t.Fatal(err)
		}
	}
	request := func(method, target, raw, etag string, session *http.Cookie, csrf, origin string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "http://routex.test"+target, strings.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		if session != nil {
			req.AddCookie(session)
		}
		if csrf != "" {
			req.Header.Set("X-CSRF-Token", csrf)
		}
		if etag != "" {
			req.Header.Set("If-Match", etag)
		}
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		out := httptest.NewRecorder()
		router.ServeHTTP(out, req)
		return out
	}
	metadata := func(suffix string) service.ConnectionMetadataRecord {
		return decodeCatalogResponse[service.ConnectionMetadataRecord](t, request("GET", "/api/v1/admin/connections/con_diagnostic_"+suffix+"/metadata", "", "", cookie, "", ""), 200)
	}
	readState := func() string {
		var providers []entity.Provider
		var connections []entity.ProviderConnection
		var credentials []entity.ProviderCredential
		var models []entity.ProviderModel
		var access []entity.CredentialModelAccess
		var bindings []entity.ModelProviderBinding
		var grants []entity.UserModelGrant
		for _, query := range []struct {
			rows  any
			order string
		}{{&providers, "id"}, {&connections, "id"}, {&credentials, "id"}, {&models, "id"}, {&access, "credential_id, provider_model_id"}, {&bindings, "id"}, {&grants, "user_id, model_id"}} {
			if err := db.Order(query.order).Find(query.rows).Error; err != nil {
				t.Fatal(err)
			}
		}
		counts := map[string]int64{}
		for key, table := range map[string]any{"audits": &entity.AuditEvent{}, "calls": &entity.CallRecord{}, "attempts": &entity.CallAttempt{}} {
			var count int64
			if err := db.Model(table).Count(&count).Error; err != nil {
				t.Fatal(err)
			}
			counts[key] = count
		}
		raw, err := json.Marshal([]any{providers, connections, credentials, models, access, bindings, grants, counts})
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}
	before := readState()
	runtimeBefore := svc.RuntimeStatus()
	for _, test := range cases {
		review := metadata(test.suffix)
		path := "/api/v1/admin/connections/con_diagnostic_" + test.suffix + "/test"
		raw := `{"credential_id":"crd_diagnostic_` + test.suffix + `"}`
		out := request("POST", path, raw, strconv.Quote(review.ETag), cookie, admin.CSRFToken, "")
		result := decodeCatalogResponse[service.ConnectionDiagnosticResult](t, out, 200)
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(out.Body.Bytes(), &fields); err != nil || len(fields) != 6 || out.Header().Get("Cache-Control") != "private, no-store" || result.ConnectionID != review.ID || result.CredentialID != "crd_diagnostic_"+test.suffix || result.Outcome != "passed" || result.CheckedAt.IsZero() || result.CheckedAt.Location() != time.UTC {
			t.Fatal("diagnostic result contract", out.Code)
		}
		if test.suffix == "azure" {
			if result.Scope != "authentication_only" || result.DiscoveredModelCount != nil {
				t.Fatal("Azure invented deployment coverage")
			}
		} else if result.Scope != "model_discovery" || result.DiscoveredModelCount == nil || *result.DiscoveredModelCount != 1 {
			t.Fatal("native discovery count/scope", result)
		}
		if readState() != before {
			t.Fatal("successful diagnostic changed retained configuration or history")
		}
	}
	path := "/api/v1/admin/connections/con_diagnostic_chat/test"
	review := metadata("chat")
	body := `{"credential_id":"crd_diagnostic_chat"}`
	for _, denial := range []struct {
		cookie                   *http.Cookie
		csrf, origin, etag, body string
		status                   int
	}{{nil, "", "", strconv.Quote(review.ETag), body, 401}, {readCookie, readCSRF, "", strconv.Quote(review.ETag), body, 403}, {writeCookie, writeCSRF, "", strconv.Quote(review.ETag), body, 403}, {deniedCookie, deniedCSRF, "", strconv.Quote(review.ETag), body, 403}, {cookie, "incorrect-csrf", "", strconv.Quote(review.ETag), body, 403}, {cookie, admin.CSRFToken, "https://foreign.example.invalid", strconv.Quote(review.ETag), body, 403}, {cookie, admin.CSRFToken, "", "", body, 400}, {cookie, admin.CSRFToken, "", strconv.Quote(review.ETag), `{"credential_id":"crd_diagnostic_messages"}`, 404}} {
		count := gets.Load()
		out := request("POST", path, denial.body, denial.etag, denial.cookie, denial.csrf, denial.origin)
		expectStatus(t, out, denial.status)
		if out.Header().Get("Cache-Control") != "private, no-store" || gets.Load() != count || readState() != before {
			t.Fatal("denied test dispatched or changed retained facts")
		}
	}
	for _, value := range []int32{1, 3, 4} {
		mode.Store(value)
		out := request("POST", path, body, strconv.Quote(review.ETag), cookie, admin.CSRFToken, "")
		result := decodeCatalogResponse[service.ConnectionDiagnosticResult](t, out, 200)
		if result.Outcome != "failed" || result.DiscoveredModelCount != nil || strings.Contains(out.Body.String(), "test-only-private") || readState() != before {
			t.Fatal("failed diagnostic mutated or leaked upstream data")
		}
	}
	mode.Store(2)
	messages := metadata("messages")
	out := request("POST", "/api/v1/admin/connections/con_diagnostic_messages/test", `{"credential_id":"crd_diagnostic_messages"}`, strconv.Quote(messages.ETag), cookie, admin.CSRFToken, "")
	result := decodeCatalogResponse[service.ConnectionDiagnosticResult](t, out, 200)
	if result.Outcome != "failed" || result.DiscoveredModelCount != nil || readState() != before {
		t.Fatal("partial native pagination reported success")
	}
	mode.Store(0)
	mutate.Store(true)
	out = request("POST", path, body, strconv.Quote(review.ETag), cookie, admin.CSRFToken, "")
	expectStatus(t, out, 409)
	if err := db.Model(&entity.ProviderConnection{}).Where("id = ?", "con_diagnostic_chat").UpdateColumn("e_tag", "diagnostic_initial").Error; err != nil {
		t.Fatal(err)
	}
	if readState() != before || inference.Load() != 0 {
		t.Fatal("diagnostic added inference or changed domain state")
	}
	// A later explicit test is a fresh operation, never a historical receipt.
	out = request("POST", path, body, strconv.Quote(review.ETag), cookie, admin.CSRFToken, "")
	expectStatus(t, out, 200)
	if readState() != before || !reflect.DeepEqual(svc.RuntimeStatus(), runtimeBefore) {
		t.Fatal("diagnostic started runtime or changed facts")
	}
}
