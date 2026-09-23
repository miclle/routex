package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fox-gonic/fox"
	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/secretstore"
)

func decodeCatalogResponse[T any](t *testing.T, res *httptest.ResponseRecorder, status int) T {
	t.Helper()
	expectStatus(t, res, status)
	var value T
	if err := json.Unmarshal(res.Body.Bytes(), &value); err != nil {
		t.Fatal(err)
	}
	return value
}

// Called by the single database lifecycle owner after a fresh migration.
func testCatalogLifecycle(t *testing.T, db *gorm.DB) {
	var upstreamFailure atomic.Bool
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if upstreamFailure.Load() || r.Header.Get("Authorization") == "Bearer invalid-test-secret" {
			w.WriteHeader(401)
			_, _ = w.Write([]byte(`{"error":"do-not-expose-upstream-secret"}`))
			return
		}
		switch r.Header.Get("Authorization") {
		case "Bearer catalog-test-secret":
			_, _ = w.Write([]byte(`{"data":[{"id":"upstream-model"},{"id":"UPSTREAM-model"}]}`))
		case "Bearer limited-test-secret":
			_, _ = w.Write([]byte(`{"data":[{"id":"limited-model"}]}`))
		default:
			w.WriteHeader(401)
		}
	}))
	t.Cleanup(upstreamServer.Close)
	store, err := secretstore.New([]byte(strings.Repeat("k", 32)))
	if err != nil {
		t.Fatal(err)
	}
	svc, err := service.New(context.Background(), db, service.WithCredentialStorage(store), service.WithUpstreamPolicy(true))
	if err != nil {
		t.Fatal(err)
	}
	router := fox.New()
	New(svc).RegisterRoutes(router)
	setup := identityRequest(router, "POST", "/api/v1/setup", `{"email":"catalog@example.com","password":"catalog-password","name":"Catalog admin"}`, nil, "")
	expectStatus(t, setup, 201)
	admin, adminCookie := readIdentity(t, setup)
	request := func(method, path string, body any) *httptest.ResponseRecorder {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		return identityRequest(router, method, path, string(encoded), adminCookie, admin.CSRFToken)
	}
	providerBody := map[string]any{"name": "Controlled supplier", "connection_name": "Primary connection", "base_url": upstreamServer.URL + "/v1", "protocol": "openai_chat", "credential_name": "Primary credential", "secret": "catalog-test-secret"}
	encodedProvider, _ := json.Marshal(providerBody)
	expectStatus(t, identityRequest(router, "POST", "/api/v1/admin/providers", string(encodedProvider), adminCookie, ""), 403)
	provider := decodeCatalogResponse[ProviderResponse](t, request("POST", "/api/v1/admin/providers", providerBody), 201)
	if len(provider.Connections) != 1 || len(provider.Connections[0].Credentials) != 1 {
		t.Fatal("provider aggregate missing connection or credential")
	}
	connection := provider.Connections[0]
	credential := connection.Credentials[0]
	if credential.Enabled || credential.VerificationStatus != "pending" {
		t.Fatal("new credential must be pending and disabled")
	}
	var stored entity.ProviderCredential
	if err := db.First(&stored, "id = ?", credential.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Ciphertext == "" || strings.Contains(stored.Ciphertext, "catalog-test-secret") {
		t.Fatal("credential not encrypted at rest")
	}
	if plaintext, err := store.Open(stored.ID, stored.Ciphertext); err != nil || plaintext != "catalog-test-secret" {
		t.Fatal("stored credential cannot be recovered with its reference")
	}
	if _, err := store.Open("another-reference", stored.Ciphertext); err == nil {
		t.Fatal("encrypted credential accepted the wrong reference")
	}
	providers := request("GET", "/api/v1/admin/providers", nil)
	expectStatus(t, providers, 200)
	for _, forbidden := range []string{"catalog-test-secret", "ciphertext", stored.Ciphertext} {
		if strings.Contains(providers.Body.String(), forbidden) {
			t.Fatal("provider response exposed credential material")
		}
	}
	credentialPath := "/api/v1/admin/credentials/" + credential.ID
	expectStatus(t, request("PATCH", credentialPath, map[string]bool{"enabled": true}), 409)
	pm := decodeCatalogResponse[ProviderModelResponse](t, request("POST", "/api/v1/admin/connections/"+connection.ID+"/models", map[string]string{"upstream_name": "upstream-model"}), 201)
	model := decodeCatalogResponse[ModelResponse](t, request("POST", "/api/v1/admin/models", map[string]string{"name": "gateway-model", "provider_model_id": pm.ID}), 201)
	if model.Bindings[0].Weight != 0 || model.Bindings[0].Ready || len(model.GrantedUserIDs) != 1 || model.GrantedUserIDs[0] != admin.User.ID {
		t.Fatal("new model must have a zero-weight candidate and explicit creator grant")
	}
	modelPath := "/api/v1/admin/models/" + model.ID
	weights := func(a int) map[string]any {
		return map[string]any{"weights": []map[string]any{{"binding_id": model.Bindings[0].ID, "weight": a}}}
	}
	expectStatus(t, request("PUT", modelPath+"/weights", weights(100)), 409)
	verification := decodeCatalogResponse[VerifyCredentialResponse](t, request("POST", credentialPath+"/verify", map[string]any{}), 200)
	if !verification.Verified || verification.DiscoveredModels != 2 {
		t.Fatal("real model discovery failed")
	}
	var remainedDisabled entity.ProviderCredential
	if err := db.First(&remainedDisabled, "id = ?", credential.ID).Error; err != nil || remainedDisabled.Enabled {
		t.Fatal("verification must not enable a credential")
	}
	enabled := decodeCatalogResponse[CredentialResponse](t, request("PATCH", credentialPath, map[string]bool{"enabled": true}), 200)
	if !enabled.Enabled || enabled.VerificationStatus != "verified" {
		t.Fatal("verified credential was not enabled")
	}
	model = decodeCatalogResponse[ModelResponse](t, request("PUT", modelPath+"/weights", weights(100)), 200)
	if !model.Bindings[0].Ready || model.Bindings[0].Weight != 100 {
		t.Fatal("validated binding did not activate")
	}
	// Model discovery, including case-sensitive upstream names, survives a fresh service.
	var discovered []entity.ProviderModel
	if err := db.Where("connection_id = ?", connection.ID).Find(&discovered).Error; err != nil || len(discovered) != 2 {
		t.Fatalf("case-sensitive upstream names not preserved: %v", err)
	}
	var secondModelID string
	for _, item := range discovered {
		if item.UpstreamName == "UPSTREAM-model" {
			secondModelID = item.ID
		}
	}
	model = decodeCatalogResponse[ModelResponse](t, request("POST", modelPath+"/bindings", map[string]string{"provider_model_id": secondModelID}), 201)
	if len(model.Bindings) != 2 || model.Bindings[1].Weight != 0 {
		t.Fatal("new candidate received weight")
	}
	expectStatus(t, request("PUT", modelPath+"/weights", map[string]any{"weights": []map[string]any{{"binding_id": model.Bindings[0].ID, "weight": 100}}}), 409)
	bothWeights := func(a, b int) map[string]any {
		return map[string]any{"weights": []map[string]any{{"binding_id": model.Bindings[0].ID, "weight": a}, {"binding_id": model.Bindings[1].ID, "weight": b}}}
	}
	expectStatus(t, request("PUT", modelPath+"/weights", bothWeights(50, 49)), 400)
	model = decodeCatalogResponse[ModelResponse](t, request("PUT", modelPath+"/weights", bothWeights(50, 50)), 200)
	// Serialize competing updates at the model row; never expose mixed weights.
	responses := make(chan int, 2)
	var wg sync.WaitGroup
	for _, pair := range [][2]int{{20, 80}, {70, 30}} {
		encoded, _ := json.Marshal(bothWeights(pair[0], pair[1]))
		wg.Go(func() {
			responses <- identityRequest(router, "PUT", modelPath+"/weights", string(encoded), adminCookie, admin.CSRFToken).Code
		})
	}
	wg.Wait()
	close(responses)
	for status := range responses {
		if status != 200 {
			t.Fatalf("concurrent weight update returned %d", status)
		}
	}
	var bindings []entity.ModelProviderBinding
	if err := db.Where("model_id = ?", model.ID).Order("created_at, id").Find(&bindings).Error; err != nil {
		t.Fatal(err)
	}
	if len(bindings) != 2 || bindings[0].Weight+bindings[1].Weight != 100 {
		t.Fatal("concurrent weights were not atomic")
	}

	invalid := decodeCatalogResponse[CredentialResponse](t, request("POST", "/api/v1/admin/connections/"+connection.ID+"/credentials", map[string]any{"name": "Invalid", "secret": "invalid-test-secret", "priority": 1}), 201)
	failed := request("POST", "/api/v1/admin/credentials/"+invalid.ID+"/verify", map[string]any{})
	failedVerification := decodeCatalogResponse[VerifyCredentialResponse](t, failed, 200)
	if failedVerification.Verified || strings.Contains(failed.Body.String(), "do-not-expose") {
		t.Fatal("upstream error was exposed or accepted")
	}
	expectStatus(t, request("PATCH", "/api/v1/admin/credentials/"+invalid.ID, map[string]bool{"enabled": true}), 409)
	limited := decodeCatalogResponse[CredentialResponse](t, request("POST", "/api/v1/admin/connections/"+connection.ID+"/credentials", map[string]any{"name": "Limited", "secret": "limited-test-secret", "priority": 2}), 201)
	decodeCatalogResponse[VerifyCredentialResponse](t, request("POST", "/api/v1/admin/credentials/"+limited.ID+"/verify", map[string]any{}), 200)
	expectStatus(t, request("PATCH", "/api/v1/admin/credentials/"+limited.ID, map[string]bool{"enabled": true}), 409)

	model = decodeCatalogResponse[ModelResponse](t, request("POST", modelPath+"/rename", map[string]any{"name": "renamed-model", "alias_expires_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339)}), 200)
	if model.Name != "renamed-model" || len(model.Names) != 2 || model.ID == "" {
		t.Fatal("rename lost model identity or history")
	}
	if resolved, err := svc.ResolveModelName(context.Background(), "gateway-model"); err != nil || resolved.ID != model.ID {
		t.Fatal("unexpired compatibility name did not preserve stable identity")
	}
	if err := db.Model(&entity.ModelName{}).Where("name = ?", "gateway-model").Update("expires_at", time.Now().Add(-time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ResolveModelName(context.Background(), "gateway-model"); err == nil {
		t.Fatal("expired compatibility name remained callable")
	}
	expectStatus(t, request("POST", modelPath+"/rename", map[string]string{"name": "gateway-model"}), 409)
	expectStatus(t, request("POST", "/api/v1/admin/models", map[string]string{"name": "gateway-model", "provider_model_id": pm.ID}), 409)
	caseDistinct := decodeCatalogResponse[ModelResponse](t, request("POST", "/api/v1/admin/models", map[string]string{"name": "Renamed-model", "provider_model_id": pm.ID}), 201)
	if caseDistinct.ID == model.ID {
		t.Fatal("case-distinct model names collided")
	}
	// No administrator bypass: remove its persisted grant and verify visibility.
	var adminEntity entity.User
	if err := db.First(&adminEntity, "id = ?", admin.User.ID).Error; err != nil {
		t.Fatal(err)
	}
	member := entity.User{ID: "usr_catalog_member", Email: "catalog-member@example.com", Name: "Catalog member", Role: entity.RoleMember, PasswordHash: adminEntity.PasswordHash}
	if err := db.Create(&member).Error; err != nil {
		t.Fatal(err)
	}
	memberLogin := identityRequest(router, "POST", "/api/v1/auth/login", `{"email":"catalog-member@example.com","password":"catalog-password"}`, nil, "")
	expectStatus(t, memberLogin, 200)
	memberSession, memberCookie := readIdentity(t, memberLogin)
	expectStatus(t, identityRequest(router, "GET", "/api/v1/admin/providers", "", memberCookie, ""), 403)
	expectStatus(t, identityRequest(router, "POST", "/api/v1/admin/providers", string(encodedProvider), memberCookie, memberSession.CSRFToken), 403)
	memberBefore := decodeCatalogResponse[VisibleModelsResponse](t, identityRequest(router, "GET", "/api/v1/models", "", memberCookie, ""), 200)
	if len(memberBefore.Items) != 0 {
		t.Fatal("member without grant can see a model")
	}
	decodeCatalogResponse[ModelResponse](t, request("PUT", modelPath+"/grants", map[string]any{"user_ids": []string{member.ID}}), 200)
	adminVisible := decodeCatalogResponse[VisibleModelsResponse](t, request("GET", "/api/v1/models", nil), 200)
	for _, item := range adminVisible.Items {
		if item.ID == model.ID {
			t.Fatal("administrator received implicit model access")
		}
	}
	memberAfter := decodeCatalogResponse[VisibleModelsResponse](t, identityRequest(router, "GET", "/api/v1/models", "", memberCookie, ""), 200)
	if len(memberAfter.Items) != 1 || memberAfter.Items[0].ID != model.ID || memberAfter.Items[0].Name != "renamed-model" {
		t.Fatal("member grant or stable rename not reflected")
	}
	expectStatus(t, request("PUT", modelPath+"/grants", map[string]any{"user_ids": []string{"usr_missing"}}), 400)
	var remaining int64
	if err := db.Model(&entity.UserModelGrant{}).Where("model_id = ? AND user_id = ?", model.ID, member.ID).Count(&remaining).Error; err != nil || remaining != 1 {
		t.Fatal("invalid grant replacement partially changed access")
	}
	upstreamFailure.Store(true)
	failedVerification = decodeCatalogResponse[VerifyCredentialResponse](t, request("POST", credentialPath+"/verify", map[string]any{}), 200)
	if failedVerification.Verified {
		t.Fatal("authentication failure verified credential")
	}
	var revoked entity.ProviderCredential
	if err := db.First(&revoked, "id = ?", credential.ID).Error; err != nil || revoked.Enabled || revoked.VerificationStatus != "failed" {
		t.Fatal("failed re-verification must disable credential")
	}
	allModels := decodeCatalogResponse[ModelsResponse](t, request("GET", "/api/v1/admin/models", nil), 200)
	for _, item := range allModels.Items {
		for _, binding := range item.Bindings {
			if binding.Ready {
				t.Fatal("failed credential left binding ready")
			}
		}
	}
	var auditCount int64
	if err := db.Table("audit_events").Where("actor_id = ?", admin.User.ID).Count(&auditCount).Error; err != nil || auditCount < 10 {
		t.Fatalf("catalog changes not audited: %d %v", auditCount, err)
	}
	t.Logf("catalog lifecycle verified: %d audit events", auditCount)
}
