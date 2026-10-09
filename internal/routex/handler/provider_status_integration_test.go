package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/secretstore"
	"gorm.io/gorm"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func testProviderStatusLifecycle(t *testing.T, db *gorm.DB) {
	ctx := context.Background()
	store, err := secretstore.New(bytes.Repeat([]byte{94}, 32))
	if err != nil {
		t.Fatal(err)
	}
	svc, err := service.New(ctx, db, service.WithCredentialStorage(store), service.WithUpstreamPolicy(true), service.WithRuntimeRefreshInterval(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	router := fox.New()
	New(svc).RegisterRoutes(router)
	setup := identityRequest(router, "POST", "/api/v1/setup", `{"email":"provider-status@example.invalid","password":"test-only-provider-status","name":"Provider status admin"}`, nil, "")
	expectStatus(t, setup, 201)
	admin, cookie := readIdentity(t, setup)
	_, readCookie, readCSRF := createSystemStatusMember(t, svc, router, admin.User.ID, "provider-status-reader", []string{"providers.read"})
	writer, writeCookie, writeCSRF := createSystemStatusMember(t, svc, router, admin.User.ID, "provider-status-writer", []string{"providers.read", "providers.write"})
	_, deniedCookie, _ := createSystemStatusMember(t, svc, router, admin.User.ID, "provider-status-denied", []string{"models.read_all"})
	retainedModelID := "mdl_provider_status"
	for _, row := range []any{
		&entity.Provider{ID: "prv_status", Name: "Provider"},
		&entity.Provider{ID: "prv_status_empty", Name: "Empty Provider"},
		&entity.ProviderConnection{ID: "con_provider_status", ProviderID: "prv_status", Name: "Retained", BaseURL: "https://example.invalid/v1", Protocol: entity.ProtocolOpenAIChat, EgressMode: "direct"},
		&entity.ProviderCredential{ID: "crd_provider_status", ConnectionID: "con_provider_status", Name: "Retained", Ciphertext: "test-only-never-used", Enabled: false, VerificationStatus: "pending"},
		&entity.ProviderModel{ID: "pmd_provider_status", ConnectionID: "con_provider_status", UpstreamName: "Retained", Disabled: true, SupportsImageInput: true},
		&entity.Model{ID: retainedModelID, Status: "active"},
		&entity.ModelName{Name: "retained-provider-status", ModelID: retainedModelID, CurrentModelID: &retainedModelID},
		&entity.ModelProviderBinding{ID: "bnd_provider_status", ModelID: retainedModelID, ProviderModelID: "pmd_provider_status", Weight: 100},
		&entity.UserModelGrant{UserID: admin.User.ID, ModelID: retainedModelID},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	var failAudit, failPublication atomic.Bool
	var queryFailure atomic.Pointer[providerStatusIntegrationQueryFailure]
	queryCallback, createCallback := "test:provider_status_query", "test:provider_status_audit"
	if err := db.Callback().Query().Before("gorm:query").Register(queryCallback, func(tx *gorm.DB) {
		if failPublication.Load() && tx.Statement.Table == "models" {
			_ = tx.AddError(errors.New("controlled publication failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Create().Before("gorm:create").Register(createCallback, func(tx *gorm.DB) {
		if row, ok := tx.Statement.Dest.(*entity.AuditEvent); ok && failAudit.Load() && row.Action == "provider.status.update" {
			_ = tx.AddError(errors.New("controlled audit failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	// Retain only sanitized query failure categories, never SQL, arguments,
	// driver messages, transport configuration or secret source material.
	if err := db.Callback().Query().After("gorm:query").Register(queryCallback+":diagnostic", func(tx *gorm.DB) {
		if tx.Error == nil {
			return
		}
		failure := &providerStatusIntegrationQueryFailure{Category: providerStatusIntegrationErrorCategory(tx.Error)}
		switch tx.Statement.Table {
		case "providers", "models", "users", "roles", "role_permissions", "provider_connections", "provider_credentials", "audit_events", "egress_settings":
			failure.Table = tx.Statement.Table
		default:
			failure.Table = "other"
		}
		var state interface{ SQLState() string }
		if errors.As(tx.Error, &state) {
			code := state.SQLState()
			safe := len(code) == 5
			for _, character := range code {
				safe = safe && (character >= '0' && character <= '9' || character >= 'A' && character <= 'Z')
			}
			if safe {
				failure.DriverCode = code
			}
		}
		queryFailure.CompareAndSwap(nil, failure)
	}); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = db.Callback().Query().Remove(queryCallback + ":diagnostic")
		_ = db.Callback().Query().Remove(queryCallback)
		_ = db.Callback().Create().Remove(createCallback)
	}()
	if err := svc.StartRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	defer svc.StopRuntime()
	path := "/api/v1/admin/providers/prv_status/status"
	request := func(method, target, body, etag string, session *http.Cookie, csrf string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, "http://routex.test"+target, strings.NewReader(body))
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
		if out.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatal("private status header missing", out.Code)
		}
		return out
	}
	get := func(session *http.Cookie) service.ProviderStatusRecord {
		t.Helper()
		out := request("GET", path, "", "", session, "")
		row := decodeCatalogResponse[service.ProviderStatusRecord](t, out, 200)
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(out.Body.Bytes(), &fields); err != nil || len(fields) != 5 || len(row.ETag) != 129 || out.Header().Get("ETag") != strconv.Quote(row.ETag) || row.ID != "prv_status" {
			t.Fatal("exact status projection", err, out.Body.String())
		}
		return row
	}
	diagnosticStage := "status_lifecycle"
	statusWriteNumber := 0
	put := func(session *http.Cookie, csrf, etag string, enabled bool, status int) service.ProviderStatusWriteResult {
		t.Helper()
		statusWriteNumber++
		queryFailure.Store(nil)
		raw, _ := json.Marshal(service.ProviderStatusInput{Enabled: enabled, Reason: "Reviewed Provider status"})
		out := request("PUT", path, string(raw), etag, session, csrf)
		if status == 200 && out.Code != 200 {
			// Failure-only observation cannot change acceptance or replay the
			// committed intent. One shared deadline bounds both current reads.
			failure := queryFailure.Load()
			observe, cancel := context.WithTimeout(context.Background(), 750*time.Millisecond)
			var current entity.Provider
			rowErr := db.WithContext(observe).Select("id", "created_at", "enabled", "e_tag").Where("id = ?", "prv_status").Take(&current).Error
			var count int64
			auditErr := db.WithContext(observe).Model(&entity.AuditEvent{}).Where("action = ? AND resource_id = ?", "provider.status.update", "prv_status").Count(&count).Error
			contextCategory := providerStatusIntegrationErrorCategory(observe.Err())
			cancel()
			runtime := svc.RuntimeStatus()
			t.Logf("Provider status failure stage=%s write=%d target_enabled=%t actual_status=%d query_category=%+v current_read=%s current_id=%s current_birth=%s current_enabled=%t current_revision=%s audit_read=%s audit_count=%d observation_context=%s runtime_ready=%t runtime_error=%s current_application_failure_category=unknown",
				diagnosticStage, statusWriteNumber, enabled, out.Code, failure,
				providerStatusIntegrationErrorCategory(rowErr), current.ID, current.CreatedAt.UTC().Format(time.RFC3339Nano), current.Enabled, current.ETag,
				providerStatusIntegrationErrorCategory(auditErr), count, contextCategory, runtime.Ready, runtime.ErrorCode)
		}
		expectStatus(t, out, status)
		if status != 200 {
			return service.ProviderStatusWriteResult{}
		}
		row := decodeCatalogResponse[service.ProviderStatusWriteResult](t, out, 200)
		if !row.RuntimeApplied || row.Provider.Enabled != enabled || !row.Provider.CanEdit || row.Provider.ID != "prv_status" || out.Header().Get("ETag") != strconv.Quote(row.Provider.ETag) {
			t.Fatal("current applied target missing", out.Body.String())
		}
		return row
	}
	stored := func() entity.Provider {
		t.Helper()
		var row entity.Provider
		if err := db.Where("id = ?", "prv_status").Take(&row).Error; err != nil {
			t.Fatal(err)
		}
		return row
	}
	audits := func() int64 {
		t.Helper()
		var n int64
		if err := db.Model(&entity.AuditEvent{}).Where("action = ? AND resource_id = ?", "provider.status.update", "prv_status").Count(&n).Error; err != nil {
			t.Fatal(err)
		}
		return n
	}
	children := func() string {
		t.Helper()
		var cs []entity.ProviderConnection
		var creds []entity.ProviderCredential
		var models []entity.ProviderModel
		var bindings []entity.ModelProviderBinding
		var grants []entity.UserModelGrant
		for _, q := range []*gorm.DB{db.Order("id").Find(&cs), db.Order("id").Find(&creds), db.Order("id").Find(&models), db.Order("id").Find(&bindings), db.Order("user_id,model_id").Find(&grants)} {
			if q.Error != nil {
				t.Fatal(q.Error)
			}
		}
		raw, _ := json.Marshal([]any{cs, creds, models, bindings, grants})
		return string(raw)
	}
	baselineChildren := children()
	original := stored()
	if !original.Enabled {
		t.Fatal("default Provider disabled")
	}
	reader := get(readCookie)
	if reader.CanEdit {
		t.Fatal("read grant inferred write")
	}
	expectStatus(t, request("GET", path, "", "", deniedCookie, ""), 403)
	expectStatus(t, request("GET", strings.Replace(path, "prv_status", "PRV_STATUS", 1), "", "", cookie, ""), 400)
	expectStatus(t, request("GET", path+"?q=secret", "", "", cookie, ""), 400)
	review := get(cookie)
	put(readCookie, readCSRF, reader.ETag, false, 403)
	put(cookie, "", review.ETag, false, 403)
	failAudit.Store(true)
	put(cookie, admin.CSRFToken, review.ETag, false, 503)
	failAudit.Store(false)
	if !reflect.DeepEqual(stored(), original) || audits() != 0 {
		t.Fatal("audit rollback changed status/revision")
	}
	diagnosticStage = "first_disable_after_audit_rollback"
	disabled := put(cookie, admin.CSRFToken, review.ETag, false, 200)
	diagnosticStage = "remaining_status_lifecycle"
	if !disabled.Changed || audits() != 1 || stored().ETag == original.ETag {
		t.Fatal("disable missing atomic state/revision/audit")
	}
	if row := put(cookie, admin.CSRFToken, review.ETag, false, 200); row.Changed || audits() != 1 {
		t.Fatal("current-target retry duplicated audit")
	}
	put(cookie, admin.CSRFToken, review.ETag, true, 409)
	current := get(cookie)
	put(cookie, admin.CSRFToken, current.ETag, true, 200)
	if get(cookie).ETag == review.ETag {
		t.Fatal("status ABA reused token")
	}
	put(cookie, admin.CSRFToken, review.ETag, false, 409)
	// Shared revision makes status and name changes stale each other's reviews.
	name, err := svc.GetProviderMetadata(ctx, admin.User.ID, "prv_status")
	if err != nil {
		t.Fatal(err)
	}
	current = get(cookie)
	put(cookie, admin.CSRFToken, current.ETag, false, 200)
	if got, err := svc.WriteProviderMetadata(ctx, admin.User.ID, "prv_status", name.ETag, service.ProviderMetadataInput{Name: "Renamed", Reason: "Reviewed"}); got != nil || err == nil {
		t.Fatal("status failed to stale name token", got, err)
	}
	current = get(cookie)
	name, err = svc.GetProviderMetadata(ctx, admin.User.ID, "prv_status")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.WriteProviderMetadata(ctx, admin.User.ID, "prv_status", name.ETag, service.ProviderMetadataInput{Name: "Renamed", Reason: "Reviewed"}); err != nil {
		t.Fatal(err)
	}
	put(cookie, admin.CSRFToken, current.ETag, true, 409)
	current = get(cookie)
	beforeAudit := audits()
	failPublication.Store(true)
	put(cookie, admin.CSRFToken, current.ETag, true, 503)
	if !stored().Enabled || audits() != beforeAudit+1 {
		t.Fatal("uncertain write not durably committed")
	}
	put(cookie, admin.CSRFToken, current.ETag, true, 503)
	if audits() != beforeAudit+1 {
		t.Fatal("uncertain retry duplicated audit")
	}
	failPublication.Store(false)
	if row := put(cookie, admin.CSRFToken, current.ETag, true, 200); row.Changed || audits() != beforeAudit+1 {
		t.Fatal("retry claimed new operation")
	}
	writerReview := get(writeCookie)
	var assignment entity.UserRole
	if err := db.First(&assignment, "user_id = ?", writer.User.ID).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SaveRole(ctx, admin.User.ID, assignment.RoleID, "System provider-status-writer", []string{"providers.write"}); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, request("GET", path, "", "", writeCookie, ""), 403)
	if row := put(writeCookie, writeCSRF, writerReview.ETag, false, 200); !row.Changed {
		t.Fatal("write-only status denied")
	}
	if _, err := svc.SaveRole(ctx, admin.User.ID, assignment.RoleID, "System provider-status-writer", []string{"providers.read"}); err != nil {
		t.Fatal(err)
	}
	put(writeCookie, writeCSRF, writerReview.ETag, false, 403)
	current = get(cookie)
	put(cookie, admin.CSRFToken, current.ETag, true, 200)
	if children() != baselineChildren {
		t.Fatal("Provider state altered child configuration")
	}
	empty, err := svc.GetProviderStatus(ctx, admin.User.ID, "prv_status_empty")
	if err != nil {
		t.Fatal(err)
	}
	result, err := svc.WriteProviderStatus(ctx, admin.User.ID, empty.ID, empty.ETag, service.ProviderStatusInput{Reason: "Disable empty Provider"})
	if err != nil || result == nil || !result.RuntimeApplied || result.Provider.Enabled {
		t.Fatal("empty Provider application unproven", result, err)
	}
	var rows []entity.AuditEvent
	if err := db.Where("action = ? AND resource_id = ?", "provider.status.update", "prv_status").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	page := decodeCatalogResponse[service.AuditPage](t, identityRequest(router, "GET", "/api/v1/admin/audit?category=credentials&q=provider.status.update", "", cookie, ""), 200)
	if len(page.Items) != len(rows)+1 {
		t.Fatal("typed Provider audit hidden", len(page.Items), len(rows))
	}
	for _, row := range page.Items {
		var detail struct {
			Before, After bool
			Reason        string
		}
		if json.Unmarshal(row.Changes, &detail) != nil || detail.Before == detail.After || detail.Reason == "" || row.Action != "provider.status.update" || row.ResourceType != "provider" {
			t.Fatal("unsafe typed status audit", row.ID)
		}
	}
	testProviderStatusNativeLifecycle(t, db, svc, router, admin, cookie, &failPublication)
}

// This test-only category deliberately excludes raw driver error messages.
type providerStatusIntegrationQueryFailure struct {
	Table, Category, DriverCode string
}

func providerStatusIntegrationErrorCategory(err error) string {
	switch {
	case err == nil:
		return "none"
	case errors.Is(err, context.DeadlineExceeded):
		return "deadline"
	case errors.Is(err, context.Canceled):
		return "cancelled"
	case errors.Is(err, gorm.ErrRecordNotFound):
		return "not_found"
	default:
		return "other"
	}
}
