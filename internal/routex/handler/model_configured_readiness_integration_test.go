package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
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

// The isolated harness supplies both real drivers. No runtime is started.
func testAdminModelConfiguredReadiness(t *testing.T, db *gorm.DB) {
	ctx := context.Background()
	var remoteCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		remoteCalls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer upstream.Close()
	store, err := secretstore.New(bytes.Repeat([]byte{193}, 32))
	if err != nil {
		t.Fatal(err)
	}
	svc, err := service.New(ctx, db, service.WithCredentialStorage(store), service.WithUpstreamPolicy(true))
	if err != nil {
		t.Fatal(err)
	}
	admin, err := svc.Initialize(ctx, "configured-admin@example.invalid", "configured-test-password", "Configured administrator")
	if err != nil {
		t.Fatal(err)
	}
	router := fox.New()
	New(svc).RegisterRoutes(router)
	loginBody, err := json.Marshal(map[string]string{"email": "configured-admin@example.invalid", "password": "configured-test-password"})
	if err != nil {
		t.Fatal(err)
	}
	login := identityRequest(router, "POST", "/api/v1/auth/login", string(loginBody), nil, "")
	expectStatus(t, login, http.StatusOK)
	auth, cookie := readIdentity(t, login)
	_, modelCookie, modelCSRF := createSystemStatusMember(t, svc, router, admin.User.ID, "configured-model-reader", []string{"models.read_all"})
	reader, readerCookie, readerCSRF := createSystemStatusMember(t, svc, router, admin.User.ID, "configured-both-reader", []string{"models.read_all", "providers.read"})
	_, providerCookie, providerCSRF := createSystemStatusMember(t, svc, router, admin.User.ID, "configured-provider-reader", []string{"providers.read"})
	create := func(rows ...any) {
		t.Helper()
		for _, row := range rows {
			if err := db.Create(row).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	update := func(row any, id, field string, value any) {
		t.Helper()
		result := db.Model(row).Where("id = ?", id).Update(field, value)
		if result.Error != nil || result.RowsAffected != 1 {
			t.Fatal("exact configured fixture update failed", field, result.Error, result.RowsAffected)
		}
	}
	const modelID = "mdl_configured"
	birth := time.Now().UTC().Truncate(time.Microsecond)
	for _, suffix := range []string{"a", "b"} {
		providerID, connectionID := "prv_configured_"+suffix, "con_configured_"+suffix
		pmID, credentialID := "pmd_configured_"+suffix, "crd_configured_"+suffix
		cipher, err := store.Seal(credentialID, "configured-test-secret-"+suffix)
		if err != nil {
			t.Fatal(err)
		}
		create(
			&entity.Provider{ID: providerID, Name: "Configured " + suffix, Enabled: true, ETag: "0", CreatedAt: birth},
			&entity.ProviderConnection{ID: connectionID, ProviderID: providerID, Name: "Configured " + suffix, BaseURL: upstream.URL + "/v1", Protocol: entity.ProtocolOpenAIChat, Adapter: "native", Enabled: true, TransportGeneration: "0", CreatedAt: birth},
			&entity.ProviderModel{ID: pmID, ConnectionID: connectionID, UpstreamName: "configured-" + suffix, CapabilityTransportGeneration: "0", CreatedAt: birth},
			&entity.ProviderCredential{ID: credentialID, ConnectionID: connectionID, Name: "Configured " + suffix, Ciphertext: cipher, Enabled: true, VerificationStatus: "verified", VerifiedTransportGeneration: "0", CreatedAt: birth},
			&entity.CredentialModelAccess{CredentialID: credentialID, ProviderModelID: pmID},
		)
	}
	for _, value := range []struct{ id, status string }{
		{modelID, entity.ResourceActive}, {"mdl_configured_zero", entity.ResourceActive},
		{"mdl_configured_disabled", entity.ResourceDisabled}, {"mdl_configured_archived", entity.ResourceArchived},
		{"mdl_configured_empty", entity.ResourceActive},
	} {
		id := value.id
		create(&entity.Model{ID: id, Status: value.status, CreatedAt: birth}, &entity.ModelName{Name: id, ModelID: id, CurrentModelID: &id, CreatedAt: birth})
	}
	create(
		&entity.ModelProviderBinding{ID: "bnd_configured_a", ModelID: modelID, ProviderModelID: "pmd_configured_a", Weight: 50, CreatedAt: birth},
		&entity.ModelProviderBinding{ID: "bnd_configured_b", ModelID: modelID, ProviderModelID: "pmd_configured_b", Weight: 50, CreatedAt: birth},
		&entity.ModelProviderBinding{ID: "bnd_configured_zero", ModelID: "mdl_configured_zero", ProviderModelID: "pmd_configured_a", Weight: 0, CreatedAt: birth},
		&entity.ModelProviderBinding{ID: "bnd_configured_disabled", ModelID: "mdl_configured_disabled", ProviderModelID: "pmd_configured_a", Weight: 100, CreatedAt: birth},
		&entity.ModelProviderBinding{ID: "bnd_configured_archived", ModelID: "mdl_configured_archived", ProviderModelID: "pmd_configured_a", Weight: 100, CreatedAt: birth},
		&entity.UserModelGrant{UserID: reader.User.ID, ModelID: modelID},
	)
	get := func(owner *http.Cookie, csrf, path string) *httptest.ResponseRecorder {
		return identityRequest(router, "GET", path, "", owner, csrf)
	}
	assertWire := func(response *httptest.ResponseRecorder, want *bool) ModelResponse {
		t.Helper()
		value := decodeCatalogResponse[ModelResponse](t, response, http.StatusOK)
		var wire map[string]json.RawMessage
		if err := json.Unmarshal(response.Body.Bytes(), &wire); err != nil {
			t.Fatal(err)
		}
		field, found := wire["configured_ready"]
		if !found || !reflect.DeepEqual(value.ConfiguredReady, want) || want == nil && string(field) != "null" {
			t.Fatal("configured availability must be an explicit authorized nullable fact", string(field), value.ConfiguredReady, want)
		}
		if strings.Contains(response.Body.String(), "configured-test-secret") || strings.Contains(response.Body.String(), "ciphertext") {
			t.Fatal("configured read exposed secret material")
		}
		return value
	}
	assertProjection := func(want *bool, legacyReady bool) {
		t.Helper()
		detail := assertWire(get(cookie, auth.CSRFToken, "/api/v1/admin/models/"+modelID), want)
		list := decodeCatalogResponse[ModelsResponse](t, get(cookie, auth.CSRFToken, "/api/v1/admin/models"), http.StatusOK)
		if len(list.Items) != 5 || len(detail.Bindings) != 2 || detail.Bindings[0].Ready != legacyReady {
			t.Fatal("configured summary changed catalogue shape or legacy readiness", detail, list)
		}
		found := false
		for _, row := range list.Items {
			if row.ID == modelID {
				found = true
				if !reflect.DeepEqual(row.ConfiguredReady, want) {
					t.Fatal("list/detail configured availability disagrees", row.ConfiguredReady, want)
				}
			}
			for _, binding := range row.Bindings {
				if binding.Supply != nil {
					t.Fatal("list borrowed detail-only supply facts")
				}
			}
		}
		if !found {
			t.Fatal("configured target missing from list")
		}
		assertWire(get(modelCookie, modelCSRF, "/api/v1/admin/models/"+modelID), nil)
		restricted := decodeCatalogResponse[ModelsResponse](t, get(modelCookie, modelCSRF, "/api/v1/admin/models"), http.StatusOK)
		for _, row := range restricted.Items {
			if row.ConfiguredReady != nil {
				t.Fatal("Model read alone exposed Provider configuration")
			}
		}
	}
	knownTrue, knownFalse := true, false
	var auditBefore int64
	if err := db.Model(&entity.AuditEvent{}).Count(&auditBefore).Error; err != nil {
		t.Fatal(err)
	}
	assertProjection(&knownTrue, true)
	for _, id := range []string{"mdl_configured_zero", "mdl_configured_disabled", "mdl_configured_archived", "mdl_configured_empty"} {
		assertWire(get(cookie, auth.CSRFToken, "/api/v1/admin/models/"+id), &knownFalse)
	}
	for _, p := range []string{"/api/v1/admin/models", "/api/v1/admin/models/" + modelID} {
		expectStatus(t, get(providerCookie, providerCSRF, p), http.StatusForbidden)
		expectStatus(t, get(nil, "", p), http.StatusUnauthorized)
	}
	expectStatus(t, get(cookie, auth.CSRFToken, "/api/v1/admin/models/mdl_CONFIGURED"), http.StatusNotFound)
	// Updates avoid GORM creation defaults replacing deliberately false facts.
	update(&entity.Provider{}, "prv_configured_a", "enabled", false)
	assertProjection(&knownTrue, true)
	update(&entity.Provider{}, "prv_configured_b", "enabled", false)
	assertProjection(&knownFalse, true)
	update(&entity.Provider{}, "prv_configured_a", "enabled", true)
	update(&entity.Provider{}, "prv_configured_b", "enabled", true)
	update(&entity.ProviderConnection{}, "con_configured_a", "enabled", false)
	assertProjection(&knownTrue, true)
	update(&entity.ProviderConnection{}, "con_configured_b", "enabled", false)
	assertProjection(&knownFalse, true)
	update(&entity.ProviderConnection{}, "con_configured_a", "enabled", true)
	update(&entity.ProviderConnection{}, "con_configured_b", "enabled", true)
	// Unknown transport stays Unknown even beside a separate eligible route.
	update(&entity.ProviderConnection{}, "con_configured_a", "transport_generation", "")
	assertProjection(nil, false)
	update(&entity.ProviderConnection{}, "con_configured_a", "transport_generation", "rev_01j00000000000000000000000")
	assertProjection(&knownTrue, false)
	update(&entity.ProviderConnection{}, "con_configured_b", "enabled", false)
	assertProjection(&knownFalse, false)
	update(&entity.ProviderConnection{}, "con_configured_a", "transport_generation", "0")
	update(&entity.ProviderConnection{}, "con_configured_b", "enabled", true)
	assertProjection(&knownTrue, true)
	// Current permission reads cannot borrow a previously admitted Session grant.
	var role entity.Role
	if err := db.Where("name = ?", "System configured-both-reader").Take(&role).Error; err != nil {
		t.Fatal(err)
	}
	permission := entity.RolePermission{RoleID: role.ID, Permission: "providers.read"}
	removed := db.Where("role_id = ? AND permission = ?", permission.RoleID, permission.Permission).Delete(&entity.RolePermission{})
	if removed.Error != nil || removed.RowsAffected != 1 {
		t.Fatal("could not revoke exact reader permission", removed.Error, removed.RowsAffected)
	}
	assertWire(get(readerCookie, readerCSRF, "/api/v1/admin/models/"+modelID), nil)
	create(&permission)
	assertWire(get(readerCookie, readerCSRF, "/api/v1/admin/models/"+modelID), &knownTrue)
	update(&entity.User{}, reader.User.ID, "disabled", true)
	expectStatus(t, get(readerCookie, readerCSRF, "/api/v1/admin/models/"+modelID), http.StatusUnauthorized)
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if rows, err := svc.ListAdminModels(cancelled, admin.User.ID); err == nil || rows != nil {
		t.Fatal("cancelled catalogue read returned configuration", rows, err)
	}
	var bindings []entity.ModelProviderBinding
	if err := db.Where("model_id = ?", modelID).Order("id").Find(&bindings).Error; err != nil {
		t.Fatal(err)
	}
	if len(bindings) != 2 || bindings[0].Weight != 50 || bindings[1].Weight != 50 {
		t.Fatal("read projection rewrote weights", bindings)
	}
	var auditAfter int64
	if err := db.Model(&entity.AuditEvent{}).Count(&auditAfter).Error; err != nil {
		t.Fatal(err)
	}
	if auditAfter != auditBefore || remoteCalls.Load() != 0 || svc.RuntimeStatus().Ready {
		t.Fatal("configured reads caused an audit, remote request or runtime publication", auditBefore, auditAfter, remoteCalls.Load())
	}
}
