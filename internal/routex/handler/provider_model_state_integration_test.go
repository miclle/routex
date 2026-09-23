package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
	t.Helper()
	ctx := context.Background()
	// Recreate the pre-state columns with a real retained supplier row, then
	// verify portable upgrades preserve its identity and original eligibility.
	for _, field := range []string{"Disabled", "ETag"} {
		if err := db.Migrator().DropColumn(&entity.ProviderModel{}, field); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Table("schema_migrations").Where("version = ?", 15).Delete(&struct{}{}).Error; err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := database.Migrate(ctx, db); err != nil {
			t.Fatal(err)
		}
	}
	var preserved entity.ProviderModel
	if err := db.First(&preserved, "id = ?", pm.ID).Error; err != nil || preserved.Disabled || preserved.ETag != "0" || preserved.UpstreamName != pm.UpstreamName {
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
	before := dispatches.Load()
	disabled := decodeCatalogResponse[ProviderModelResponse](t, write(false, pm.ETag), 200)
	if disabled.Enabled || disabled.ETag == pm.ETag {
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
	if err := db.Model(&entity.AuditEvent{}).Where("resource_id = ? AND action IN ?", pm.ID, []string{"provider_model.enable", "provider_model.disable"}).Count(&events).Error; err != nil || events != 2 {
		t.Fatal("state audit missing", err)
	}
}
