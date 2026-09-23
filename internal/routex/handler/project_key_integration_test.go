package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fox-gonic/fox"
	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/secret"
	"github.com/miclle/routex/pkg/secretstore"
)

func testProjectKeyLifecycle(t *testing.T, db *gorm.DB) {
	ctx := context.Background()
	store, err := secretstore.New(bytes.Repeat([]byte{47}, 32))
	if err != nil {
		t.Fatal(err)
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer project-provider-secret" {
			t.Error("unexpected upstream credential")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"model":"project-upstream","choices":[],"usage":{"prompt_tokens":3,"completion_tokens":5}}`)
	}))
	defer upstream.Close()
	svc, err := service.New(ctx, db, service.WithCredentialStorage(store), service.WithUpstreamPolicy(true))
	if err != nil {
		t.Fatal(err)
	}
	router := fox.New()
	New(svc).RegisterRoutes(router)
	setup := identityRequest(router, "POST", "/api/v1/setup", `{"email":"project-key-admin@example.com","password":"project-key-password","name":"Key admin"}`, nil, "")
	expectStatus(t, setup, 201)
	admin, adminCookie := readIdentity(t, setup)
	as := func(cookie *http.Cookie, csrf, method, path string, body any) *httptest.ResponseRecorder {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		return identityRequest(router, method, path, string(encoded), cookie, csrf)
	}
	request := func(method, path string, body any) *httptest.ResponseRecorder {
		return as(adminCookie, admin.CSRFToken, method, path, body)
	}
	member := func(email string) (SessionResponse, *http.Cookie) {
		expectStatus(t, request("POST", "/api/v1/admin/members", map[string]any{"email": email, "name": email, "password": "project-key-password"}), 201)
		login := identityRequest(router, "POST", "/api/v1/auth/login", `{"email":"`+email+`","password":"project-key-password"}`, nil, "")
		expectStatus(t, login, 200)
		return readIdentity(t, login)
	}
	creator, creatorCookie := member("key-creator@example.com")
	successor, successorCookie := member("key-successor@example.com")
	outsider, outsiderCookie := member("key-outsider@example.com")
	creatorRequest := func(method, path string, body any) *httptest.ResponseRecorder {
		return as(creatorCookie, creator.CSRFToken, method, path, body)
	}
	successorRequest := func(method, path string, body any) *httptest.ResponseRecorder {
		return as(successorCookie, successor.CSRFToken, method, path, body)
	}
	outsiderRequest := func(method, path string, body any) *httptest.ResponseRecorder {
		return as(outsiderCookie, outsider.CSRFToken, method, path, body)
	}
	project := decodeCatalogResponse[ProjectResponse](t, creatorRequest("POST", "/api/v1/projects", map[string]any{"name": "Application credentials"}), 201)
	projectPath := "/api/v1/projects/" + project.ID
	keysPath := projectPath + "/keys"
	expectStatus(t, creatorRequest("PUT", projectPath+"/managers", map[string]any{"user_ids": []string{creator.User.ID, successor.User.ID}}), 200)
	ciphertext, err := store.Seal("crd_project_keys", "project-provider-secret")
	if err != nil {
		t.Fatal(err)
	}
	modelID, otherID := "mdl_project_keys", "mdl_project_other"
	for _, row := range []any{
		&entity.Provider{ID: "prv_project_keys", Name: "Provider"},
		&entity.ProviderConnection{ID: "con_project_keys", ProviderID: "prv_project_keys", Name: "Connection", BaseURL: upstream.URL + "/v1", Protocol: entity.ProtocolOpenAIChat},
		&entity.ProviderCredential{ID: "crd_project_keys", ConnectionID: "con_project_keys", Name: "Credential", Ciphertext: ciphertext, Enabled: true, VerificationStatus: "verified"},
		&entity.ProviderModel{ID: "pmd_project_keys", ConnectionID: "con_project_keys", UpstreamName: "project-upstream"},
		&entity.CredentialModelAccess{CredentialID: "crd_project_keys", ProviderModelID: "pmd_project_keys"},
		&entity.Model{ID: modelID, Status: "active"}, &entity.Model{ID: otherID, Status: "active"},
		&entity.ModelName{Name: "project-model", ModelID: modelID, CurrentModelID: &modelID}, &entity.ModelName{Name: "other-project-model", ModelID: otherID, CurrentModelID: &otherID},
		&entity.ModelProviderBinding{ID: "bnd_project_keys", ModelID: modelID, ProviderModelID: "pmd_project_keys", Weight: 100},
		&entity.ModelProviderBinding{ID: "bnd_project_other", ModelID: otherID, ProviderModelID: "pmd_project_keys", Weight: 100},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	expectStatus(t, request("PUT", projectPath+"/models", map[string]any{"model_ids": []string{modelID, otherID}}), 200)
	if err := svc.StartRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	defer svc.StopRuntime()
	if err := svc.StartCallRecorder(ctx, filepath.Join(t.TempDir(), "project-calls.db")); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := svc.StopCallRecorder(); err != nil {
			t.Error(err)
		}
	}()
	keyBody := map[string]any{"name": "Application", "model_ids": []string{modelID}, "delivery_mode": "manual"}
	expectStatus(t, outsiderRequest("GET", keysPath, nil), 404)
	expectStatus(t, outsiderRequest("POST", keysPath, keyBody), 404)
	expectStatus(t, creatorRequest("POST", keysPath, map[string]any{"name": "Unsupported", "model_ids": []string{modelID}, "delivery_mode": "vault"}), 400)
	expectStatus(t, creatorRequest("POST", keysPath, map[string]any{"name": "Missing mode", "model_ids": []string{modelID}}), 400)
	expectStatus(t, creatorRequest("POST", keysPath, map[string]any{"name": "Broaden scope", "model_ids": []string{"mdl_unknown"}, "delivery_mode": "manual"}), 403)
	created := decodeCatalogResponse[CreatedProjectKeyResponse](t, creatorRequest("POST", keysPath, keyBody), 201)
	keyPath := keysPath + "/" + created.Key.ID
	if created.Key.ProjectID != project.ID || created.Key.CreatorID != creator.User.ID || created.Key.Status != "pending" || !strings.HasPrefix(created.Secret, "rxp_") {
		t.Fatal("invalid Project Key creation")
	}
	if _, err := svc.AuthenticateAPIKey(ctx, created.Secret); err == nil {
		t.Fatal("undelivered Project Key authenticated")
	}
	expectStatus(t, identityRequest(router, "POST", keyPath+"/confirm", "", creatorCookie, ""), 403)
	expectStatus(t, successorRequest("POST", keyPath+"/confirm", nil), 200)
	auth, err := svc.AuthenticateAPIKey(ctx, created.Secret)
	if err != nil || auth.ProjectID != project.ID || auth.Key.UserID != "" || len(auth.ModelIDs) != 1 {
		t.Fatal("Project key acquired personal ownership or lost scope")
	}
	var stored entity.ProjectKey
	if err := db.First(&stored, "id = ?", created.Key.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.TokenHash != secret.SHA256Hex(created.Secret) || stored.TokenHash == created.Secret {
		t.Fatal("Project credential verification storage invalid")
	}
	for _, response := range []*httptest.ResponseRecorder{creatorRequest("GET", keysPath, nil), creatorRequest("GET", keyPath, nil)} {
		expectStatus(t, response, 200)
		for _, sensitive := range []string{created.Secret, stored.TokenHash, "token_hash", "project-provider-secret"} {
			if strings.Contains(response.Body.String(), sensitive) {
				t.Fatal("secret leaked in Project Key response")
			}
		}
	}
	expectStatus(t, creatorRequest("DELETE", "/api/v1/keys/"+created.Key.ID, nil), 404)
	call := func(bearer, model string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "http://routex.test/v1/chat/completions", strings.NewReader(`{"model":"`+model+`","messages":[{"role":"user","content":"Project fixture"}]}`))
		req.Header.Set("Authorization", "Bearer "+bearer)
		req.Header.Set("Content-Type", "application/json")
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		return res
	}
	expectStatus(t, call(created.Secret, "project-model"), 200)
	expectStatus(t, call(created.Secret, "other-project-model"), 404)
	if err := svc.FlushCallRecorder(ctx); err != nil {
		t.Fatal(err)
	}
	var facts []entity.CallRecord
	if err := db.Where("key_id = ?", created.Key.ID).Find(&facts).Error; err != nil || len(facts) != 2 {
		t.Fatal("Project gateway facts missing")
	}
	for _, fact := range facts {
		if fact.ProjectID != project.ID || fact.UserID != "" {
			t.Fatal("Project call attributed to creator")
		}
	}
	projectCalls := decodeCatalogResponse[CallsResponse](t, creatorRequest("GET", projectPath+"/calls", nil), 200)
	if len(projectCalls.Items) != 2 {
		t.Fatal("Project history is not scoped to its facts")
	}
	for _, fact := range facts {
		response := creatorRequest("GET", projectPath+"/calls/"+fact.RequestID, nil)
		expectStatus(t, response, 200)
		if strings.Contains(response.Body.String(), "connection_id") || strings.Contains(response.Body.String(), "error_code") {
			t.Fatal("Project history exposed internal diagnostics")
		}
		expectStatus(t, outsiderRequest("GET", projectPath+"/calls/"+fact.RequestID, nil), 404)
	}
	expectStatus(t, outsiderRequest("GET", projectPath+"/calls", nil), 404)
	personal := decodeCatalogResponse[CallsResponse](t, creatorRequest("GET", "/api/v1/calls", nil), 200)
	if len(personal.Items) != 0 {
		t.Fatal("Project calls leaked into creator personal calls")
	}
	expectStatus(t, request("PUT", projectPath+"/models", map[string]any{"model_ids": []string{otherID}}), 200)
	expectStatus(t, call(created.Secret, "project-model"), 404)
	expectStatus(t, request("PUT", projectPath+"/models", map[string]any{"model_ids": []string{modelID, otherID}}), 200)
	expectStatus(t, call(created.Secret, "project-model"), 200)
	expectStatus(t, call(created.Secret, "other-project-model"), 404)
	pending := decodeCatalogResponse[CreatedProjectKeyResponse](t, creatorRequest("POST", keysPath, keyBody), 201)
	expectStatus(t, request("PATCH", projectPath, map[string]any{"status": "disabled"}), 200)
	if _, err := svc.AuthenticateAPIKey(ctx, created.Secret); err == nil {
		t.Fatal("disabled Project retained gateway authority")
	}
	expectStatus(t, creatorRequest("POST", keysPath, keyBody), 409)
	expectStatus(t, creatorRequest("POST", keyPath+"/rotate", map[string]any{"delivery_mode": "manual"}), 409)
	expectStatus(t, creatorRequest("POST", keysPath+"/"+pending.Key.ID+"/confirm", nil), 409)
	expectStatus(t, creatorRequest("DELETE", keysPath+"/"+pending.Key.ID, nil), 204)
	expectStatus(t, creatorRequest("PATCH", keyPath, map[string]any{"enabled": false}), 200)
	expectStatus(t, request("PATCH", projectPath, map[string]any{"status": "active"}), 200)
	if _, err := svc.AuthenticateAPIKey(ctx, created.Secret); err == nil {
		t.Fatal("Project reactivation enabled explicitly disabled Key")
	}
	expectStatus(t, creatorRequest("PATCH", keyPath, map[string]any{"enabled": true}), 200)
	replacement := decodeCatalogResponse[CreatedProjectKeyResponse](t, successorRequest("POST", keyPath+"/rotate", map[string]any{"delivery_mode": "manual"}), 201)
	replacementPath := keysPath + "/" + replacement.Key.ID
	if replacement.Key.ReplacesKeyID == nil || *replacement.Key.ReplacesKeyID != created.Key.ID || replacement.Secret == created.Secret {
		t.Fatal("rotation lost replacement identity")
	}
	expectStatus(t, successorRequest("POST", replacementPath+"/confirm", nil), 200)
	if _, err := svc.AuthenticateAPIKey(ctx, created.Secret); err != nil {
		t.Fatal("delivery confirmation prematurely retired old Key")
	}
	completion := map[string]any{"replacement_key_id": replacement.Key.ID}
	expectStatus(t, successorRequest("POST", keyPath+"/complete-rotation", completion), 409)
	expectStatus(t, call(replacement.Secret, "other-project-model"), 404)
	if err := svc.FlushCallRecorder(ctx); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, successorRequest("POST", keyPath+"/complete-rotation", completion), 409)
	expectStatus(t, call(replacement.Secret, "project-model"), 200)
	if err := svc.FlushCallRecorder(ctx); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, successorRequest("POST", keyPath+"/complete-rotation", completion), 204)
	expectStatus(t, successorRequest("POST", keyPath+"/complete-rotation", completion), 204)
	expectStatus(t, successorRequest("PATCH", replacementPath, map[string]any{"enabled": false}), 200)
	expectStatus(t, successorRequest("POST", keyPath+"/complete-rotation", completion), 204)
	expectStatus(t, successorRequest("PATCH", replacementPath, map[string]any{"enabled": true}), 200)
	var completionAudits int64
	if err := db.Model(&entity.AuditEvent{}).Where("action = ? AND resource_id = ?", "project_key.rotation.complete", replacement.Key.ID).Count(&completionAudits).Error; err != nil || completionAudits != 1 {
		t.Fatal("Project rotation completion audit was duplicated or misattributed")
	}
	if _, err := svc.AuthenticateAPIKey(ctx, created.Secret); err == nil {
		t.Fatal("old Project Key remained valid after verified retirement")
	}
	// Project assets and credential ownership survive creator departure.
	survivor := decodeCatalogResponse[CreatedProjectKeyResponse](t, creatorRequest("POST", keysPath, keyBody), 201)
	expectStatus(t, creatorRequest("POST", keysPath+"/"+survivor.Key.ID+"/confirm", nil), 200)
	expectStatus(t, creatorRequest("PUT", projectPath+"/managers", map[string]any{"user_ids": []string{successor.User.ID}}), 200)
	expectStatus(t, creatorRequest("GET", keysPath, nil), 404)
	expectStatus(t, creatorRequest("GET", projectPath+"/calls", nil), 404)
	expectStatus(t, request("PATCH", "/api/v1/admin/members/"+creator.User.ID, map[string]any{"disabled": true}), 200)
	expectStatus(t, call(replacement.Secret, "project-model"), 200)
	expectStatus(t, call(survivor.Secret, "project-model"), 200)
	survivingRecord := decodeCatalogResponse[ProjectKeyResponse](t, successorRequest("GET", keysPath+"/"+survivor.Key.ID, nil), 200)
	if survivingRecord.CreatorID != creator.User.ID || survivingRecord.ProjectID != project.ID {
		t.Fatal("creator departure rewrote Project Key ownership")
	}
	remaining := decodeCatalogResponse[ProjectKeyResponse](t, successorRequest("GET", replacementPath, nil), 200)
	if remaining.ProjectID != project.ID || remaining.CreatorID != successor.User.ID {
		t.Fatal("creator departure transferred Project Key")
	}
	// Expiry and unconfirmed delivery deadlines remain hard authorization gates.
	expiring := decodeCatalogResponse[CreatedProjectKeyResponse](t, successorRequest("POST", keysPath, keyBody), 201)
	deadline := time.Now().UTC().Add(-time.Minute)
	if err := db.Model(&entity.ProjectKey{}).Where("id = ?", expiring.Key.ID).Update("delivery_expires_at", deadline).Error; err != nil {
		t.Fatal(err)
	}
	expectStatus(t, successorRequest("POST", keysPath+"/"+expiring.Key.ID+"/confirm", nil), 409)
	if err := db.Model(&entity.ProjectKey{}).Where("id = ?", replacement.Key.ID).Update("expires_at", deadline).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.RefreshRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AuthenticateAPIKey(ctx, replacement.Secret); err == nil {
		t.Fatal("expired Project Key authenticated")
	}
	expectStatus(t, successorRequest("PATCH", replacementPath, map[string]any{"enabled": true}), 409)
	// Manager removal and issuance serialize: a removed actor cannot confirm an
	// earlier pending credential, while another current manager can revoke it.
	expectStatus(t, successorRequest("PUT", projectPath+"/managers", map[string]any{"user_ids": []string{successor.User.ID, outsider.User.ID}}), 200)
	var wg sync.WaitGroup
	results := make(chan *httptest.ResponseRecorder, 1)
	wg.Go(func() { results <- outsiderRequest("POST", keysPath, keyBody) })
	wg.Go(func() {
		expectStatus(t, successorRequest("PUT", projectPath+"/managers", map[string]any{"user_ids": []string{successor.User.ID}}), 200)
	})
	wg.Wait()
	issued := <-results
	switch issued.Code {
	case 201:
		raced := decodeCatalogResponse[CreatedProjectKeyResponse](t, issued, 201)
		expectStatus(t, outsiderRequest("POST", keysPath+"/"+raced.Key.ID+"/confirm", nil), 404)
		expectStatus(t, successorRequest("DELETE", keysPath+"/"+raced.Key.ID, nil), 204)
	case 404:
	default:
		t.Fatalf("unexpected issuance/removal result %d", issued.Code)
	}
	testProjectKeyRevocationRaces(t, db, svc, keysPath, successorRequest, keyBody)
	// Project disable races with delivery confirmation under the same policy lock.
	concurrent := decodeCatalogResponse[CreatedProjectKeyResponse](t, successorRequest("POST", keysPath, keyBody), 201)
	confirmResult := make(chan int, 1)
	wg.Go(func() { confirmResult <- successorRequest("POST", keysPath+"/"+concurrent.Key.ID+"/confirm", nil).Code })
	wg.Go(func() { expectStatus(t, request("PATCH", projectPath, map[string]any{"status": "disabled"}), 200) })
	wg.Wait()
	confirmStatus := <-confirmResult
	if confirmStatus != 200 && confirmStatus != 409 {
		t.Fatalf("unexpected confirm/Project disable result %d", confirmStatus)
	}
	if _, err := svc.AuthenticateAPIKey(ctx, concurrent.Secret); err == nil {
		t.Fatal("confirmation race bypassed Project disable")
	}
	expectStatus(t, request("PATCH", projectPath, map[string]any{"status": "active"}), 200)
	expectStatus(t, request("PATCH", projectPath, map[string]any{"status": "disabled"}), 200)
	expectStatus(t, request("PATCH", projectPath, map[string]any{"status": "archived"}), 200)
	expectStatus(t, successorRequest("GET", keysPath, nil), 200)
	expectStatus(t, successorRequest("DELETE", replacementPath, nil), 409)
	if err := svc.FlushCallRecorder(ctx); err != nil {
		t.Fatal(err)
	}
}

func testProjectKeyRevocationRaces(t *testing.T, db *gorm.DB, svc *service.Service, keysPath string, request func(string, string, any) *httptest.ResponseRecorder, keyBody map[string]any) {
	t.Helper()
	for _, operation := range []string{"confirm", "rotate"} {
		key := decodeCatalogResponse[CreatedProjectKeyResponse](t, request("POST", keysPath, keyBody), 201)
		path := keysPath + "/" + key.Key.ID
		if operation == "rotate" {
			expectStatus(t, request("POST", path+"/confirm", nil), 200)
		}
		var wg sync.WaitGroup
		out := make(chan *httptest.ResponseRecorder, 1)
		wg.Go(func() { out <- request("POST", path+"/"+operation, map[string]any{"delivery_mode": "manual"}) })
		wg.Go(func() { expectStatus(t, request("DELETE", path, nil), 204) })
		wg.Wait()
		response := <-out
		if response.Code != 200 && response.Code != 201 && response.Code != 409 {
			t.Fatalf("unexpected %s/revoke result %d", operation, response.Code)
		}
		var stored entity.ProjectKey
		if err := db.First(&stored, "id = ?", key.Key.ID).Error; err != nil || stored.Status != entity.KeyRevoked {
			t.Fatal("concurrent operation restored revoked Key")
		}
		if _, err := svc.AuthenticateAPIKey(context.Background(), key.Secret); err == nil {
			t.Fatal("revocation race retained bearer authority")
		}
		if operation == "rotate" && response.Code == 201 {
			replacement := decodeCatalogResponse[CreatedProjectKeyResponse](t, response, 201)
			expectStatus(t, request("POST", keysPath+"/"+replacement.Key.ID+"/confirm", nil), 409)
		}
	}
}
