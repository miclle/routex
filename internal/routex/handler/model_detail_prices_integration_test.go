package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/fox-gonic/fox"
	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/pricing"
)

// The lifecycle owner runs this bounded detail contract against both real drivers.
func testModelDetailPricesLifecycle(t *testing.T, db *gorm.DB) {
	ctx := context.Background()
	svc, err := service.New(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	router := fox.New()
	New(svc).RegisterRoutes(router)
	setup := identityRequest(router, "POST", "/api/v1/setup", `{"email":"model-detail-admin@example.invalid","password":"model-detail-password","name":"Model detail admin"}`, nil, "")
	expectStatus(t, setup, http.StatusCreated)
	admin, adminCookie := readIdentity(t, setup)
	request := func(cookie *http.Cookie, csrf, method, path string, body any) *httptest.ResponseRecorder {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		return identityRequest(router, method, path, string(raw), cookie, csrf)
	}
	adminRequest := func(method, path string, body any) *httptest.ResponseRecorder {
		return request(adminCookie, admin.CSRFToken, method, path, body)
	}
	create := func(row any) {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	member := func(email, name string, permission string) (SessionResponse, *http.Cookie) {
		expectStatus(t, adminRequest("POST", "/api/v1/admin/members", map[string]string{"email": email, "name": name, "password": "model-detail-password"}), 201)
		login := identityRequest(router, "POST", "/api/v1/auth/login", `{"email":"`+email+`","password":"model-detail-password"}`, nil, "")
		expectStatus(t, login, 200)
		identity, cookie := readIdentity(t, login)
		role := entity.Role{ID: "rol_" + name, Name: name, NameKey: name}
		create(&role)
		create(&entity.RolePermission{RoleID: role.ID, Permission: permission})
		create(&entity.UserRole{UserID: identity.User.ID, RoleID: role.ID})
		return identity, cookie
	}
	modelReader, modelCookie := member("model-detail-reader@example.invalid", "model_detail_reader", "models.read_all")
	priceReader, priceCookie := member("model-price-reader@example.invalid", "model_price_reader", "prices.read")
	const modelID = "mdl_detail_Model"
	const providerModelID = "pmd_detail_Model"
	create(&entity.Provider{ID: "prv_detail", Name: "Detail provider"})
	create(&entity.ProviderConnection{ID: "con_detail", ProviderID: "prv_detail", Name: "Detail connection", Protocol: entity.ProtocolOpenAIChat, BaseURL: "https://example.invalid/v1"})
	create(&entity.ProviderModel{ID: providerModelID, ConnectionID: "con_detail", UpstreamName: "detail-upstream"})
	create(&entity.ProviderModel{ID: "pmd_detail_unpriced", ConnectionID: "con_detail", UpstreamName: "unpriced-upstream"})
	create(&entity.Model{ID: modelID, Status: entity.ResourceActive})
	create(&entity.ModelName{Name: "detail-current", ModelID: modelID, CurrentModelID: func() *string { v := modelID; return &v }()})
	expires := time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond)
	create(&entity.ModelName{Name: "detail-prior", ModelID: modelID, ExpiresAt: &expires})
	create(&entity.ModelProviderBinding{ID: "bnd_detail", ModelID: modelID, ProviderModelID: providerModelID, Weight: 100})
	create(&entity.UserModelGrant{UserID: modelReader.User.ID, ModelID: modelID})
	create(&entity.ProviderCredential{ID: "crd_detail", ConnectionID: "con_detail", Name: "Detail credential", Ciphertext: "private-stored-ciphertext", Enabled: true, VerificationStatus: "verified"})
	create(&entity.CredentialModelAccess{CredentialID: "crd_detail", ProviderModelID: providerModelID})
	setting, err := svc.ListPrices(ctx, admin.User.ID, service.PriceFilter{})
	if err != nil {
		t.Fatal(err)
	}
	prices, err := svc.WritePrices(ctx, admin.User.ID, setting.ETag, []service.PriceInput{{ProviderModelID: providerModelID, Rates: []pricing.Rate{{Metric: pricing.Input, Tier: pricing.Base, Unit: pricing.Unit, Currency: "USD", Amount: "0", Enabled: true}, {Metric: pricing.Output, Tier: pricing.Base, Unit: pricing.Unit, Currency: "USD", Amount: "0.123456789012345678", Enabled: false}}}})
	if err != nil {
		t.Fatal(err)
	}
	modelPath := "/api/v1/admin/models/" + modelID
	pricePath := "/api/v1/admin/provider-models/" + providerModelID + "/price"
	expectStatus(t, identityRequest(router, "GET", modelPath, "", nil, ""), 401)
	expectStatus(t, identityRequest(router, "GET", pricePath, "", nil, ""), 401)
	modelGet := func(path string) *httptest.ResponseRecorder {
		return request(modelCookie, modelReader.CSRFToken, "GET", path, nil)
	}
	priceGet := func(path string) *httptest.ResponseRecorder {
		return request(priceCookie, priceReader.CSRFToken, "GET", path, nil)
	}
	detail := decodeCatalogResponse[ModelResponse](t, modelGet(modelPath), 200)
	if detail.ID != modelID || detail.Name != "detail-current" || len(detail.Names) != 2 || len(detail.Bindings) != 1 || !detail.Bindings[0].Ready || detail.Bindings[0].ProviderModelID != providerModelID || !slices.Equal(detail.GrantedUserIDs, []string{modelReader.User.ID}) {
		t.Fatal("scoped Model detail lost existing DTO facts", detail)
	}
	listed := decodeCatalogResponse[ModelsResponse](t, modelGet("/api/v1/admin/models"), 200)
	if len(listed.Items) != 1 || !reflect.DeepEqual(listed.Items[0], detail) {
		t.Fatal("detail differs from list contract", listed, detail)
	}
	page := decodeCatalogResponse[service.PricePage](t, priceGet(pricePath), 200)
	if page.ETag != prices.ETag || len(page.Items) != 1 || page.Items[0].ProviderModelID != providerModelID || len(page.Items[0].Rates) != 2 || page.NextCursor != "" || page.Currency.PlatformCurrency != "USD" {
		t.Fatal("scoped price generation/target incorrect", page)
	}
	for _, rate := range page.Items[0].Rates {
		if rate.Metric == pricing.Input && (rate.Amount != "0" || !rate.Enabled) || rate.Metric == pricing.Output && (rate.Amount != "0.123456789012345678" || rate.Enabled) {
			t.Fatal("scoped price altered zero/disabled exact decimal", rate)
		}
	}
	// A GET body cannot override the resource captured from the URL.
	for _, field := range []string{"provider_model_id", "ProviderModelID"} {
		bodyRead := decodeCatalogResponse[service.PricePage](t, request(priceCookie, priceReader.CSRFToken, "GET", pricePath, map[string]string{field: "pmd_detail_unpriced"}), 200)
		if len(bodyRead.Items) != 1 || bodyRead.Items[0].ProviderModelID != providerModelID {
			t.Fatal("price GET body replaced path identity", bodyRead)
		}
	}
	for _, response := range []*httptest.ResponseRecorder{modelGet(modelPath), priceGet(pricePath)} {
		for _, private := range []string{"private-stored-ciphertext", "ciphertext", "base_url", "password_hash", "secret"} {
			if strings.Contains(response.Body.String(), private) {
				t.Fatal("detail exposed unrelated secret field", private)
			}
		}
	}
	// Each permission permits its own scoped read and confers no other directory
	// or write authority, even for a real current Session.
	expectStatus(t, modelGet(pricePath), 403)
	expectStatus(t, priceGet(modelPath), 403)
	expectStatus(t, modelGet("/api/v1/admin/providers"), 403)
	expectStatus(t, priceGet("/api/v1/admin/providers"), 403)
	expectStatus(t, request(modelCookie, modelReader.CSRFToken, "PUT", modelPath+"/weights", map[string]any{"weights": []map[string]any{{"binding_id": "bnd_detail", "weight": 100}}}), 403)
	expectStatus(t, request(priceCookie, priceReader.CSRFToken, "PUT", "/api/v1/admin/prices", map[string]any{"etag": page.ETag, "items": []any{}}), 403)
	for _, query := range []string{"?provider_id=private", "?model_id=other", "?limit=1", "?q=one&q=two"} {
		expectStatus(t, modelGet(modelPath+query), 400)
		expectStatus(t, priceGet(pricePath+query), 400)
	}
	for _, target := range []string{"mdl_missing", strings.ToLower(modelID), modelID + "%20"} {
		response := modelGet("/api/v1/admin/models/" + target)
		if response.Code != 400 && response.Code != 404 {
			t.Fatal("Model alias borrowed canonical target", target, response.Code, response.Body.String())
		}
	}
	for _, target := range []string{"pmd_missing", strings.ToLower(providerModelID), providerModelID + "%20", "pmd_detail_unpriced"} {
		response := priceGet("/api/v1/admin/provider-models/" + target + "/price")
		if response.Code != 400 && response.Code != 404 {
			t.Fatal("price alias/missing schedule borrowed current target", target, response.Code, response.Body.String())
		}
	}
	if _, err := svc.GetAdminModel(ctx, strings.ToUpper(admin.User.ID), modelID); !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatal("Model detail borrowed aliased enabled actor", err)
	}
	if _, err := svc.GetPrice(ctx, strings.ToUpper(admin.User.ID), providerModelID); !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatal("price detail borrowed aliased enabled actor", err)
	}
	// Detail must remain addressable without loading an unrelated broken Model.
	create(&entity.Model{ID: "mdl_unrelated", Status: entity.ResourceActive})
	const noDirectoryCallback = "test:model-detail-no-directory"
	if err := db.Callback().Query().Before("gorm:query").Register(noDirectoryCallback, func(tx *gorm.DB) {
		if tx.Statement.Table == "models" && tx.Statement.Clauses["WHERE"].Expression == nil {
			_ = tx.AddError(errors.New("unscoped Model directory read is unavailable"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := db.Callback().Query().Remove(noDirectoryCallback); err != nil {
			t.Error(err)
		}
	}()
	expectStatus(t, modelGet(modelPath), 200)
	if err := db.Model(&entity.ProviderCredential{}).Where("id = ?", "crd_detail").Update("verification_status", "VERIFIED").Error; err != nil {
		t.Fatal(err)
	}
	statusAlias := decodeCatalogResponse[ModelResponse](t, modelGet(modelPath), 200)
	if statusAlias.Bindings[0].Ready {
		t.Fatal("detail readiness borrowed nonexact verification status")
	}
	if err := db.Model(&entity.ProviderCredential{}).Where("id = ?", "crd_detail").Update("verification_status", "verified").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&entity.ProviderCredential{}).Where("id = ?", "crd_detail").Update("enabled", false).Error; err != nil {
		t.Fatal(err)
	}
	refreshed := decodeCatalogResponse[ModelResponse](t, modelGet(modelPath), 200)
	if refreshed.Bindings[0].Ready {
		t.Fatal("detail retained old credential readiness")
	}
	if err := db.Where("user_id = ? AND role_id = ?", modelReader.User.ID, "rol_model_detail_reader").Delete(&entity.UserRole{}).Error; err != nil {
		t.Fatal(err)
	}
	expectStatus(t, modelGet(modelPath), 403)
	if _, err := svc.GetAdminModel(ctx, modelReader.User.ID, modelID); !errors.Is(err, apperrors.ErrForbidden) {
		t.Fatal("service reused revoked Model read authority", err)
	}
	if err := db.Model(&entity.User{}).Where("id = ?", priceReader.User.ID).Update("disabled", true).Error; err != nil {
		t.Fatal(err)
	}
	expectStatus(t, priceGet(pricePath), 401)
	if _, err := svc.GetPrice(ctx, priceReader.User.ID, providerModelID); !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatal("service reused disabled price actor", err)
	}
}
