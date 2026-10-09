package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/secretstore"
	"gorm.io/gorm"
)

func testProviderModelState(t *testing.T, db *gorm.DB, direct http.Handler, store *secretstore.Store, pm entity.ProviderModel, body, bearer string, dispatches *atomic.Int32) {
	ctx := context.Background()
	// Register only while the fixture has no background database workers. Later
	// defers join the local runtime before these callbacks are removed.
	var diagnosticErrors atomic.Int32
	queryErrors := func(kind string) func(*gorm.DB) {
		return func(tx *gorm.DB) {
			if tx.Error == nil || tx.Error == gorm.ErrRecordNotFound || diagnosticErrors.Add(1) > 16 {
				return
			}
			table := "other"
			switch tx.Statement.Table {
			case "provider_models", "provider_connections", "providers", "provider_credentials", "credential_model_accesses", "schema_migrations":
				table = tx.Statement.Table
			}
			t.Logf("historical ProviderModel %s error: table=%q type=%T cached_plan_result_type=%t", kind, table, tx.Error, strings.Contains(tx.Error.Error(), "cached plan must not change result type"))
		}
	}
	const queryDiagnostic = "test:provider_model_state_query_errors"
	if err := db.Callback().Query().After("gorm:query").Register(queryDiagnostic, queryErrors("query")); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := db.Callback().Query().Remove(queryDiagnostic); err != nil {
			t.Error(err)
		}
	}()
	const rowDiagnostic = "test:provider_model_state_row_errors"
	if err := db.Callback().Row().After("gorm:row").Register(rowDiagnostic, queryErrors("row")); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := db.Callback().Row().Remove(rowDiagnostic); err != nil {
			t.Error(err)
		}
	}()
	// Historical DDL invalidates physical driver statement descriptions. All
	// migration calls are synchronous and precede the local runtime; keep Open's
	// production configuration and restore database/sql's default idle allowance.
	drainMigrationCache := func() {
		t.Helper()
		pool, err := db.DB()
		if err != nil {
			t.Fatal(err)
		}
		if inUse := pool.Stats().InUse; inUse != 0 {
			t.Fatalf("historical ProviderModel cache drain requires quiescence: in_use=%d", inUse)
		}
		pool.SetMaxIdleConns(0)
		pool.SetMaxIdleConns(2)
	}
	// Recreate each released upgrade boundary with a retained supplier row.
	// Versions 15 and 22 remain independently retryable and preserve identity.
	for _, field := range []string{"Disabled", "ETag"} {
		if err := db.Migrator().DropColumn(&entity.ProviderModel{}, field); err != nil {
			t.Fatal(err)
		}
	}
	result := db.Table("schema_migrations").Where("version = ?", 15).Delete(&struct{}{})
	if result.Error != nil || result.RowsAffected != 1 {
		t.Fatalf("remove migration 15 ledger: affected=%d, err=%v", result.RowsAffected, result.Error)
	}
	drainMigrationCache()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	drainMigrationCache()
	for _, field := range []string{"Disabled", "ETag"} {
		if !db.Migrator().HasColumn(&entity.ProviderModel{}, field) {
			t.Fatalf("provider model state migration did not restore %s", field)
		}
	}
	if err := db.Model(&entity.ProviderModel{}).Where("id = ?", pm.ID).Updates(map[string]any{"disabled": false, "e_tag": "0"}).Error; err != nil {
		t.Fatalf("restored provider model state columns are not writable: %v", err)
	}
	for _, field := range []string{"SupportsImageInput", "SupportsPDFInput"} {
		if err := db.Migrator().DropColumn(&entity.ProviderModel{}, field); err != nil {
			t.Fatal(err)
		}
	}
	result = db.Table("schema_migrations").Where("version = ?", 22).Delete(&struct{}{})
	if result.Error != nil || result.RowsAffected != 1 {
		t.Fatalf("remove migration 22 ledger: affected=%d, err=%v", result.RowsAffected, result.Error)
	}
	for range 2 {
		drainMigrationCache()
		if err := database.Migrate(ctx, db); err != nil {
			t.Fatal(err)
		}
		drainMigrationCache()
	}
	for _, field := range []string{"SupportsImageInput", "SupportsPDFInput"} {
		if !db.Migrator().HasColumn(&entity.ProviderModel{}, field) {
			t.Fatalf("provider model capability migration did not restore %s", field)
		}
	}
	if err := db.Model(&entity.ProviderModel{}).Where("id = ?", pm.ID).Updates(map[string]any{"e_tag": "0", "supports_image_input": false, "supports_pdf_input": false}).Error; err != nil {
		t.Fatalf("restored provider model columns are not writable: %v", err)
	}
	var preserved entity.ProviderModel
	// Recreated historical columns can change SELECT * result order on an existing
	// driver connection. Read only this upgrade's frozen preservation contract.
	if err := db.Select("disabled", "supports_image_input", "supports_pdf_input", "e_tag", "upstream_name").First(&preserved, "id = ?", pm.ID).Error; err != nil || preserved.Disabled || preserved.SupportsImageInput || preserved.SupportsPDFInput || preserved.ETag != "0" || preserved.UpstreamName != pm.UpstreamName {
		t.Fatal("existing supply changed during upgrade", err)
	}
	svc, err := service.New(ctx, db, service.WithCredentialStorage(store), service.WithUpstreamPolicy(true))
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.StartRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	defer svc.StopRuntime()
	router := fox.New()
	New(svc).RegisterRoutes(router)
	admin, cookie := readIdentity(t, identityRequest(router, "POST", "/api/v1/auth/login", `{"email":"gateway@example.invalid","password":"test-only-gateway-password"}`, nil, ""))
	path := "/api/v1/admin/provider-models/" + pm.ID
	write := func(enabled bool, etag string) *httptest.ResponseRecorder {
		raw, err := json.Marshal(map[string]any{"enabled": enabled, "etag": etag})
		if err != nil {
			t.Fatal(err)
		}
		return identityRequest(router, "PATCH", path, string(raw), cookie, admin.CSRFToken)
	}
	expectStatus(t, identityRequest(router, "PATCH", path, `{"enabled":false,"etag":"0"}`, nil, ""), 401)
	expectStatus(t, identityRequest(router, "PATCH", path, `{"enabled":false,"etag":"0"}`, cookie, ""), 403)
	expectStatus(t, identityRequest(router, "PATCH", path, `{"enabled":false,"etag":"0","other":1}`, cookie, admin.CSRFToken), 400)
	expectStatus(t, identityRequest(router, "PATCH", path, `{"etag":"0"}`, cookie, admin.CSRFToken), 400)
	currentProviders := decodeCatalogResponse[ProvidersResponse](t, identityRequest(router, "GET", "/api/v1/admin/providers", "", cookie, ""), 200)
	capabilityReview := ProviderModelResponse{}
	for _, provider := range currentProviders.Items {
		for _, connection := range provider.Connections {
			for _, candidate := range connection.ProviderModels {
				if connection.ID == pm.ConnectionID && candidate.ID == pm.ID {
					capabilityReview = candidate
				}
			}
		}
	}
	if capabilityReview.ID != pm.ID || capabilityReview.ETag != "0" || len(capabilityReview.CapabilityReviewETag) != 64 {
		t.Fatal("exact current capability review missing")
	}
	var capabilityBefore entity.ProviderModel
	if err := db.Select("disabled", "supports_image_input", "supports_pdf_input", "capability_transport_generation").First(&capabilityBefore, "id = ?", pm.ID).Error; err != nil {
		t.Fatal("capability audit baseline missing", err)
	}
	capabilityBody, err := json.Marshal(map[string]any{"enabled": true, "supports_image_input": true, "supports_pdf_input": true, "etag": "0", "capability_review_etag": capabilityReview.CapabilityReviewETag})
	if err != nil {
		t.Fatal(err)
	}
	capabilityResponse := identityRequest(router, "PATCH", path, string(capabilityBody), cookie, admin.CSRFToken)
	if capabilityResponse.Code != http.StatusOK {
		t.Fatalf("capability update returned %d: %s", capabilityResponse.Code, capabilityResponse.Body.String())
	}
	capabilities := decodeCatalogResponse[ProviderModelResponse](t, capabilityResponse, 200)
	if !capabilities.SupportsImageInput || !capabilities.SupportsPDFInput || capabilities.ETag == "0" || !capabilities.Enabled {
		t.Fatal("input capabilities were not updated atomically")
	}
	var persisted entity.ProviderModel
	if err := db.First(&persisted, "id = ?", pm.ID).Error; err != nil || !persisted.SupportsImageInput || !persisted.SupportsPDFInput {
		t.Fatal("input capabilities were not persisted", err)
	}
	expectStatus(t, write(false, "0"), 409)
	before := dispatches.Load()
	disabled := decodeCatalogResponse[ProviderModelResponse](t, write(false, capabilities.ETag), 200)
	if disabled.Enabled || disabled.ETag == capabilities.ETag || !disabled.SupportsImageInput || !disabled.SupportsPDFInput {
		t.Fatal("state or ETag did not change")
	}
	invoke := func(handler http.Handler, status int) {
		req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+bearer)
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		expectStatus(t, res, status)
	}
	invoke(direct, 503)
	invoke(router, 503)
	if dispatches.Load() != before {
		t.Fatal("disabled supply reached upstream")
	}
	expectStatus(t, write(true, pm.ETag), 409)
	enabled := decodeCatalogResponse[ProviderModelResponse](t, write(true, disabled.ETag), 200)
	if !enabled.Enabled {
		t.Fatal("supply did not re-enable")
	}
	invoke(direct, 200)
	invoke(router, 200)
	var bindings int64
	if err := db.Model(&entity.ModelProviderBinding{}).Where("provider_model_id = ? AND weight = ?", pm.ID, 100).Count(&bindings).Error; err != nil || bindings != 1 {
		t.Fatal("state edit changed routing weights", err)
	}
	var events int64
	if err := db.Model(&entity.AuditEvent{}).Where("resource_id = ? AND action IN ?", pm.ID, []string{"provider_model.enable", "provider_model.disable", "provider_model.update", "provider_model.capability.review"}).Count(&events).Error; err != nil || events != 3 {
		t.Fatalf("state audit missing: count=%d, err=%v", events, err)
	}
	for action, want := range map[string]int64{"provider_model.enable": 1, "provider_model.disable": 1, "provider_model.update": 0} {
		var count int64
		if err := db.Model(&entity.AuditEvent{}).Where("resource_id = ? AND action = ?", pm.ID, action).Count(&count).Error; err != nil || count != want {
			t.Fatalf("state audit action=%q count=%d want=%d: %v", action, count, want, err)
		}
	}
	var capabilityEvents []entity.AuditEvent
	if err := db.Where("resource_id = ? AND action = ?", pm.ID, "provider_model.capability.review").Limit(2).Find(&capabilityEvents).Error; err != nil || len(capabilityEvents) != 1 {
		t.Fatalf("typed capability audit missing or duplicated: count=%d, err=%v", len(capabilityEvents), err)
	}
	capabilityEvent := capabilityEvents[0]
	if capabilityEvent.ActorID != admin.User.ID || capabilityEvent.ResourceType != "provider_model" || capabilityEvent.ResourceID != pm.ID || capabilityEvent.DetailsJSON == nil {
		t.Fatal("typed capability audit identity or details missing")
	}
	var details map[string]any
	wantDetails := map[string]any{
		"version": "provider_model.capability.v1",
		"before":  map[string]any{"enabled": !capabilityBefore.Disabled, "image": capabilityBefore.SupportsImageInput, "pdf": capabilityBefore.SupportsPDFInput, "generation": capabilityBefore.CapabilityTransportGeneration},
		"after":   map[string]any{"enabled": !persisted.Disabled, "image": persisted.SupportsImageInput, "pdf": persisted.SupportsPDFInput, "generation": persisted.CapabilityTransportGeneration},
	}
	if err := json.Unmarshal([]byte(*capabilityEvent.DetailsJSON), &details); err != nil || !reflect.DeepEqual(details, wantDetails) {
		t.Fatal("typed capability audit did not retain exact reviewed transition", err)
	}
}
