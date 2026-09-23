package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fox-gonic/fox"
	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/secretstore"
)

func testPersonalKeyRotationLifecycle(t *testing.T, db *gorm.DB) {
	ctx := context.Background()
	store, err := secretstore.New(bytes.Repeat([]byte{49}, 32))
	if err != nil {
		t.Fatal(err)
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer rotation-upstream-secret" {
			t.Error("wrong upstream credential")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"model":"rotation-upstream","choices":[],"usage":{"prompt_tokens":2,"completion_tokens":4}}`)
	}))
	defer upstream.Close()
	svc, err := service.New(ctx, db, service.WithCredentialStorage(store), service.WithUpstreamPolicy(true))
	if err != nil {
		t.Fatal(err)
	}
	router := fox.New()
	New(svc).RegisterRoutes(router)
	setup := identityRequest(router, "POST", "/api/v1/setup", `{"email":"rotation@example.com","password":"rotation-password","name":"Rotation owner"}`, nil, "")
	expectStatus(t, setup, 201)
	auth, cookie := readIdentity(t, setup)
	request := func(method, path string, body any) *httptest.ResponseRecorder {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		return identityRequest(router, method, path, string(encoded), cookie, auth.CSRFToken)
	}
	ciphertext, err := store.Seal("crd_rotation", "rotation-upstream-secret")
	if err != nil {
		t.Fatal(err)
	}
	modelID := "mdl_rotation"
	for _, row := range []any{
		&entity.Provider{ID: "prv_rotation", Name: "Provider"},
		&entity.ProviderConnection{ID: "con_rotation", ProviderID: "prv_rotation", Name: "Connection", BaseURL: upstream.URL + "/v1", Protocol: entity.ProtocolOpenAIChat},
		&entity.ProviderCredential{ID: "crd_rotation", ConnectionID: "con_rotation", Name: "Credential", Ciphertext: ciphertext, Enabled: true, VerificationStatus: "verified"},
		&entity.ProviderModel{ID: "pmd_rotation", ConnectionID: "con_rotation", UpstreamName: "rotation-upstream"},
		&entity.CredentialModelAccess{CredentialID: "crd_rotation", ProviderModelID: "pmd_rotation"},
		&entity.Model{ID: modelID, Status: "active"}, &entity.ModelName{Name: "rotation-model", ModelID: modelID, CurrentModelID: &modelID},
		&entity.ModelProviderBinding{ID: "bnd_rotation", ModelID: modelID, ProviderModelID: "pmd_rotation", Weight: 100},
		&entity.UserModelGrant{UserID: auth.User.ID, ModelID: modelID},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := svc.StartRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	defer svc.StopRuntime()
	create := func() CreatedKeyResponse {
		return decodeCatalogResponse[CreatedKeyResponse](t, request("POST", "/api/v1/keys", map[string]any{"name": "Application", "model_ids": []string{modelID}}), 201)
	}
	original := create()
	oldPath := "/api/v1/keys/" + original.Key.ID
	expectStatus(t, request("POST", oldPath+"/confirm", nil), 200)
	replacement := decodeCatalogResponse[CreatedKeyResponse](t, request("POST", oldPath+"/rotate", nil), 201)
	newPath := "/api/v1/keys/" + replacement.Key.ID
	completion := map[string]any{"replacement_key_id": replacement.Key.ID}
	expectStatus(t, request("POST", oldPath+"/complete-rotation", completion), 409)
	expectStatus(t, request("POST", newPath+"/confirm", nil), 200)
	if _, err := svc.AuthenticateAPIKey(ctx, original.Secret); err != nil {
		t.Fatal("confirmation retired source before application verification")
	}
	if _, err := svc.AuthenticateAPIKey(ctx, replacement.Secret); err != nil {
		t.Fatal("confirmed replacement did not activate")
	}
	expectStatus(t, request("POST", oldPath+"/complete-rotation", completion), 409)
	expectStatus(t, identityRequest(router, "POST", oldPath+"/complete-rotation", `{"replacement_key_id":"`+replacement.Key.ID+`"}`, cookie, ""), 403)
	call := func(bearer, model string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "http://routex.test/v1/chat/completions", strings.NewReader(`{"model":"`+model+`","messages":[{"role":"user","content":"Rotation check"}]}`))
		req.Header.Set("Authorization", "Bearer "+bearer)
		req.Header.Set("Content-Type", "application/json")
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		return res
	}
	expectStatus(t, call(original.Secret, "rotation-model"), 200)
	expectStatus(t, request("POST", oldPath+"/complete-rotation", completion), 409)
	expectStatus(t, call(replacement.Secret, "unknown-model"), 404)
	expectStatus(t, request("POST", oldPath+"/complete-rotation", completion), 409)
	// Hostile historical fixtures prove that another owner or Project attribution
	// cannot qualify. Successful acceptance below still requires a real HTTP call.
	now := time.Now().UTC()
	for _, fact := range []entity.CallRecord{
		{RequestID: "req_wrong_rotation_owner", UserID: "usr_other_history", KeyID: replacement.Key.ID, Protocol: entity.ProtocolOpenAIChat, Status: "success", StartedAt: now, CompletedAt: now},
		{RequestID: "req_wrong_rotation_project", ProjectID: "prj_other_history", KeyID: replacement.Key.ID, Protocol: entity.ProtocolOpenAIChat, Status: "success", StartedAt: now, CompletedAt: now},
	} {
		if err := db.Create(&fact).Error; err != nil {
			t.Fatal(err)
		}
	}
	expectStatus(t, request("POST", oldPath+"/complete-rotation", completion), 409)
	unrelated := create()
	expectStatus(t, request("POST", "/api/v1/keys/"+unrelated.Key.ID+"/confirm", nil), 200)
	expectStatus(t, call(unrelated.Secret, "rotation-model"), 200)
	expectStatus(t, request("POST", oldPath+"/complete-rotation", map[string]any{"replacement_key_id": unrelated.Key.ID}), 409)
	expectStatus(t, call(replacement.Secret, "rotation-model"), 200)
	var wg sync.WaitGroup
	codes := make(chan int, 2)
	for range 2 {
		wg.Go(func() { codes <- request("POST", oldPath+"/complete-rotation", completion).Code })
	}
	wg.Wait()
	close(codes)
	for status := range codes {
		if status != 204 {
			t.Fatalf("completion replay status %d", status)
		}
	}
	if _, err := svc.AuthenticateAPIKey(ctx, original.Secret); err == nil {
		t.Fatal("verified retirement left source usable")
	}
	var audits int64
	if err := db.Model(&entity.AuditEvent{}).Where("action = ? AND resource_id = ?", "key.rotation.complete", replacement.Key.ID).Count(&audits).Error; err != nil || audits != 1 {
		t.Fatal("concurrent completion duplicated or misattributed audit")
	}
	expectStatus(t, request("PATCH", newPath, map[string]any{"enabled": false}), 200)
	expectStatus(t, request("POST", oldPath+"/complete-rotation", completion), 204)
	// Completed history remains idempotent even if replacement is later revoked.
	expectStatus(t, request("DELETE", newPath, nil), 204)
	expectStatus(t, request("POST", oldPath+"/complete-rotation", completion), 204)
	// Emergency revocation never waits for delivery or a successful call.
	emergency := create()
	emergencyPath := "/api/v1/keys/" + emergency.Key.ID
	expectStatus(t, request("POST", emergencyPath+"/confirm", nil), 200)
	undelivered := decodeCatalogResponse[CreatedKeyResponse](t, request("POST", emergencyPath+"/rotate", nil), 201)
	expectStatus(t, request("DELETE", emergencyPath, nil), 204)
	if _, err := svc.AuthenticateAPIKey(ctx, emergency.Secret); err == nil {
		t.Fatal("emergency revocation waited for replacement")
	}
	expectStatus(t, request("POST", "/api/v1/keys/"+undelivered.Key.ID+"/confirm", nil), 409)
	expectStatus(t, request("POST", emergencyPath+"/complete-rotation", map[string]any{"replacement_key_id": undelivered.Key.ID}), 409)
	// Explicit ownership applies to completion too.
	other, err := svc.CreateMember(ctx, auth.User.ID, "rotation-other@example.com", "rotation-password", "Other", "member")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.CompletePersonalKeyRotation(ctx, other.User.ID, original.Key.ID, replacement.Key.ID); err == nil {
		t.Fatal("cross-owner completion accepted")
	}
}
