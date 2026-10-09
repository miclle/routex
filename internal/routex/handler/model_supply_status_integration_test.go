package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
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
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/pricing"
	"github.com/miclle/routex/pkg/secretstore"
)

// The shared lifecycle harness runs this against both supported real drivers.
func testModelSupplyStatusLifecycle(t *testing.T, db *gorm.DB) {
	ctx := context.Background()
	var dispatches atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dispatches.Add(1)
		if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer supply-test-secret" {
			t.Error("supply call changed its configured native route or credential")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var payload struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil || payload.Model != "supply-upstream" {
			t.Error("supply call lost its exact upstream model", err, payload)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"supply-chat","object":"chat.completion","model":"supply-upstream","choices":[{"index":0,"message":{"role":"assistant","content":"Hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2}}`)
	}))
	defer upstream.Close()
	store, err := secretstore.New(bytes.Repeat([]byte{147}, 32))
	if err != nil {
		t.Fatal(err)
	}
	svc, err := service.New(ctx, db, service.WithCredentialStorage(store), service.WithUpstreamPolicy(true))
	if err != nil {
		t.Fatal(err)
	}
	admin, err := svc.Initialize(ctx, "supply-status-admin@example.invalid", "supply-status-password", "Supply status administrator")
	if err != nil {
		t.Fatal(err)
	}
	router := fox.New()
	New(svc).RegisterRoutes(router)
	login := identityRequest(router, "POST", "/api/v1/auth/login", `{"email":"supply-status-admin@example.invalid","password":"supply-status-password"}`, nil, "")
	expectStatus(t, login, http.StatusOK)
	identity, cookie := readIdentity(t, login)
	reader, readerCookie, readerCSRF := createSystemStatusMember(t, svc, router, admin.User.ID, "supply-reader", []string{"models.read_all"})
	create := func(rows ...any) {
		t.Helper()
		for _, row := range rows {
			if err := db.Create(row).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	const modelID, providerModelID, credentialID, bindingID = "mdl_supply_status", "pmd_supply_status", "crd_supply_status", "bnd_supply_status"
	cipher, err := store.Seal(credentialID, "supply-test-secret")
	if err != nil {
		t.Fatal(err)
	}
	expires := time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond)
	create(
		&entity.Provider{ID: "prv_supply_status", Name: "Supply provider"},
		&entity.ProviderConnection{ID: "con_supply_status", ProviderID: "prv_supply_status", Name: "Supply connection", BaseURL: upstream.URL + "/v1", Protocol: entity.ProtocolOpenAIChat},
		&entity.ProviderModel{ID: providerModelID, ConnectionID: "con_supply_status", UpstreamName: "supply-upstream"},
		&entity.ProviderCredential{ID: credentialID, ConnectionID: "con_supply_status", Name: "Supply credential", Ciphertext: cipher, Enabled: true, VerificationStatus: "verified"},
		&entity.CredentialModelAccess{CredentialID: credentialID, ProviderModelID: providerModelID},
		&entity.Model{ID: modelID, Status: entity.ResourceActive},
		&entity.ModelName{Name: "supply-status", ModelID: modelID, CurrentModelID: func() *string { value := modelID; return &value }()},
		&entity.ModelName{Name: "supply-prior", ModelID: modelID, ExpiresAt: &expires},
		&entity.ModelProviderBinding{ID: bindingID, ModelID: modelID, ProviderModelID: providerModelID, Weight: 100},
		&entity.UserModelGrant{UserID: admin.User.ID, ModelID: modelID},
		&entity.UserModelGrant{UserID: reader.User.ID, ModelID: modelID},
	)
	priceBase, err := svc.ListPrices(ctx, admin.User.ID, service.PriceFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.WritePrices(ctx, admin.User.ID, priceBase.ETag, []service.PriceInput{{ProviderModelID: providerModelID, Rates: []pricing.Rate{
		{Metric: pricing.Input, Tier: pricing.Base, Unit: pricing.Unit, Currency: "USD", Amount: "0", Enabled: true},
		{Metric: pricing.Output, Tier: pricing.Base, Unit: pricing.Unit, Currency: "USD", Amount: "0.123456789012345678", Enabled: false},
	}}}); err != nil {
		t.Fatal(err)
	}
	key, err := svc.CreatePersonalKey(ctx, admin.User.ID, "Supply call", []string{modelID}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ConfirmKeyDelivery(ctx, admin.User.ID, key.Record.Key.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.StartRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	defer svc.StopRuntime()
	request := func(method, path string, body any) *httptest.ResponseRecorder {
		t.Helper()
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		return identityRequest(router, method, path, string(raw), cookie, identity.CSRFToken)
	}
	modelPath := "/api/v1/admin/models/" + modelID
	pricePath := "/api/v1/admin/provider-models/" + providerModelID + "/price"
	baseline := decodeCatalogResponse[ModelResponse](t, request("GET", modelPath, nil), http.StatusOK)
	baselinePrice := decodeCatalogResponse[service.PricePage](t, request("GET", pricePath, nil), http.StatusOK)
	if len(baseline.Bindings) != 1 || !baseline.Bindings[0].Ready || baseline.Bindings[0].Weight != 100 || len(baseline.Names) != 2 || len(baseline.GrantedUserIDs) != 2 {
		t.Fatal("baseline supply is not configured and available", baseline)
	}
	assertCatalog := func(ready, covered bool) {
		t.Helper()
		want := baseline
		want.ConfiguredReady = nil
		want.Bindings = append([]ModelBindingResponse(nil), baseline.Bindings...)
		want.Bindings[0].Ready = ready
		want.Bindings[0].Supply = nil
		for _, auth := range []struct {
			cookie *http.Cookie
			csrf   string
			supply bool
		}{{cookie, identity.CSRFToken, true}, {readerCookie, readerCSRF, false}} {
			detail := decodeCatalogResponse[ModelResponse](t, identityRequest(router, "GET", modelPath, "", auth.cookie, auth.csrf), http.StatusOK)
			listed := decodeCatalogResponse[ModelsResponse](t, identityRequest(router, "GET", "/api/v1/admin/models", "", auth.cookie, auth.csrf), http.StatusOK)
			if len(detail.Bindings) != 1 || len(listed.Items) != 1 || len(listed.Items[0].Bindings) != 1 || listed.Items[0].Bindings[0].Supply != nil {
				t.Fatal("bounded catalogue or list-only supply contract changed")
			}
			supply := detail.Bindings[0].Supply
			if auth.supply {
				if supply == nil || supply.ProviderName != "Supply provider" || supply.ConnectionName != "Supply connection" || supply.ConfiguredAvailable != ready || supply.VerificationCovered != covered {
					t.Fatal("detail supply lost recorded configuration/verification separation")
				}
			} else if supply != nil {
				t.Fatal("Model-only reader borrowed Provider supply authority")
			}
			if auth.supply {
				if detail.ConfiguredReady == nil || *detail.ConfiguredReady != ready || listed.Items[0].ConfiguredReady == nil || *listed.Items[0].ConfiguredReady != ready {
					t.Fatal("authorized configured summary ignored complete current supply")
				}
			} else if detail.ConfiguredReady != nil || listed.Items[0].ConfiguredReady != nil {
				t.Fatal("Model-only reader received configured Provider availability")
			}
			detail.ConfiguredReady = nil
			listed.Items[0].ConfiguredReady = nil
			// Compare every common catalogue fact after separately checking the authorized detail-only projection.
			detail.Bindings[0].Supply = nil
			if !reflect.DeepEqual(detail, want) || len(listed.Items) != 1 || !reflect.DeepEqual(listed.Items[0], want) {
				t.Fatal("availability changed weights, identities, names, grants, or list/detail agreement", detail, listed, want)
			}
		}
		price := decodeCatalogResponse[service.PricePage](t, request("GET", pricePath, nil), http.StatusOK)
		if !reflect.DeepEqual(price, baselinePrice) {
			t.Fatal("supply availability changed the independent exact price generation", price, baselinePrice)
		}
	}
	invoke := func(status int, dispatched bool) {
		t.Helper()
		// Renew the real snapshot lease so expiry cannot explain a disabled-route denial.
		if err := svc.RefreshRuntime(ctx); err != nil {
			t.Fatal(err)
		}
		before := dispatches.Load()
		req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"supply-status","messages":[{"role":"user","content":"Hello"}]}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+key.Secret)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		expectStatus(t, response, status)
		want := before
		if dispatched {
			want++
		}
		if dispatches.Load() != want {
			t.Fatal("native dispatch disagrees with supply availability", before, dispatches.Load())
		}
	}
	assertCatalog(true, true)
	invoke(http.StatusOK, true)
	disabled := decodeCatalogResponse[ProviderModelResponse](t, request("PATCH", "/api/v1/admin/provider-models/"+providerModelID, map[string]any{"enabled": false, "etag": "0"}), http.StatusOK)
	if disabled.Enabled {
		t.Fatal("Provider Model did not become disabled")
	}
	assertCatalog(false, true)
	invoke(http.StatusServiceUnavailable, false)
	weights := map[string]any{"weights": []map[string]any{{"binding_id": bindingID, "weight": 100}}}
	updated := decodeCatalogResponse[ModelResponse](t, request("PUT", modelPath+"/weights", weights), http.StatusOK)
	if updated.Bindings[0].Ready || updated.Bindings[0].Weight != 100 {
		t.Fatal("weight configuration confused temporary availability with credential readiness", updated)
	}
	// All enabled verified credentials must cover the model, even while disabled.
	uncoveredCipher, err := store.Seal("crd_supply_uncovered", "supply-test-secret")
	if err != nil {
		t.Fatal(err)
	}
	create(&entity.ProviderCredential{ID: "crd_supply_uncovered", ConnectionID: "con_supply_status", Name: "Uncovered credential", Ciphertext: uncoveredCipher, Enabled: true, VerificationStatus: "verified"})
	expectStatus(t, request("PUT", modelPath+"/weights", weights), http.StatusConflict)
	assertCatalog(false, false)
	if err := db.Model(&entity.ProviderCredential{}).Where("id = ?", "crd_supply_uncovered").Update("enabled", false).Error; err != nil {
		t.Fatal(err)
	}
	expectStatus(t, request("PUT", modelPath+"/weights", weights), http.StatusOK)
	assertCatalog(false, true)
	invoke(http.StatusServiceUnavailable, false)
	enabled := decodeCatalogResponse[ProviderModelResponse](t, request("PATCH", "/api/v1/admin/provider-models/"+providerModelID, map[string]any{"enabled": true, "etag": disabled.ETag}), http.StatusOK)
	if !enabled.Enabled {
		t.Fatal("Provider Model did not become enabled")
	}
	assertCatalog(true, true)
	invoke(http.StatusOK, true)
	// An administrator's role label does not override its current permission rows.
	permission := entity.RolePermission{RoleID: "rol_admin", Permission: "models.read_all"}
	deleted := db.Where("role_id = ? AND permission = ?", permission.RoleID, permission.Permission).Delete(&entity.RolePermission{})
	if deleted.Error != nil || deleted.RowsAffected != 1 {
		t.Fatal("could not revoke current administrator Model read authority", deleted.Error, deleted.RowsAffected)
	}
	for _, path := range []string{"/api/v1/admin/models", modelPath} {
		expectStatus(t, request("GET", path, nil), http.StatusForbidden)
		expectStatus(t, identityRequest(router, "GET", path, "", readerCookie, readerCSRF), http.StatusOK)
	}
	if _, err := svc.GetAdminModel(ctx, admin.User.ID, modelID); !errors.Is(err, apperrors.ErrForbidden) {
		t.Fatal("detail service borrowed stale administrator read authority", err)
	}
	create(&permission)
	assertCatalog(true, true)
}
