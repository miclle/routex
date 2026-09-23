package handler

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/secret"
)

func testKeyLifecycle(t *testing.T, db *gorm.DB) {
	router := identityRouter(t, db)
	setup := identityRequest(router, "POST", "/api/v1/setup", `{"email":"owner@example.com","password":"valid-password","name":"Owner"}`, nil, "")
	expectStatus(t, setup, 201)
	auth, cookie := readIdentity(t, setup)
	ctx := context.Background()
	svc, err := service.New(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	for _, modelID := range []string{"mdl_allowed", "mdl_other"} {
		if err := db.Create(&entity.Model{ID: modelID, Status: "active"}).Error; err != nil {
			t.Fatal(err)
		}
	}
	grant := entity.UserModelGrant{UserID: auth.User.ID, ModelID: "mdl_allowed"}
	if err := db.Create(&grant).Error; err != nil {
		t.Fatal(err)
	}
	create := func() CreatedKeyResponse {
		t.Helper()
		res := identityRequest(router, "POST", "/api/v1/keys", `{"name":"Automation","model_ids":["mdl_allowed"]}`, cookie, auth.CSRFToken)
		expectStatus(t, res, 201)
		var data CreatedKeyResponse
		if err := json.Unmarshal(res.Body.Bytes(), &data); err != nil {
			t.Fatal(err)
		}
		if data.Key.Status != "pending" || len(data.Secret) != 46 || data.Key.DeliveryExpiresAt == nil {
			t.Fatalf("invalid creation: %+v", data.Key)
		}
		var stored entity.APIKey
		if err := db.First(&stored, "id = ?", data.Key.ID).Error; err != nil {
			t.Fatal(err)
		}
		if stored.TokenHash != secret.SHA256Hex(data.Secret) {
			t.Fatal("secret must only be stored as digest")
		}
		return data
	}
	expectStatus(t, identityRequest(router, "POST", "/api/v1/keys", `{"name":"Forbidden","model_ids":["mdl_other"]}`, cookie, auth.CSRFToken), 403)
	expectStatus(t, identityRequest(router, "POST", "/api/v1/keys", `{"name":"Empty","model_ids":[]}`, cookie, auth.CSRFToken), 400)
	expectStatus(t, identityRequest(router, "POST", "/api/v1/keys", `{"name":"No CSRF","model_ids":["mdl_allowed"]}`, cookie, ""), 403)
	original := create()
	if _, err := svc.AuthenticateAPIKey(ctx, original.Secret); err == nil {
		t.Fatal("pending key authenticated")
	}
	list := identityRequest(router, "GET", "/api/v1/keys", "", cookie, "")
	expectStatus(t, list, 200)
	if strings.Contains(list.Body.String(), original.Secret) || strings.Contains(list.Body.String(), secret.SHA256Hex(original.Secret)) {
		t.Fatal("secret or digest leaked in list")
	}
	confirm := func(keyID string) {
		t.Helper()
		expectStatus(t, identityRequest(router, "POST", "/api/v1/keys/"+keyID+"/confirm", "", cookie, auth.CSRFToken), 200)
	}
	confirm(original.Key.ID)
	expectStatus(t, identityRequest(router, "POST", "/api/v1/keys/"+original.Key.ID+"/confirm", "", cookie, auth.CSRFToken), 409)
	if got, err := svc.AuthenticateAPIKey(ctx, original.Secret); err != nil || len(got.ModelIDs) != 1 {
		t.Fatalf("active key: %v", err)
	}
	// Revoked user grants take effect immediately without changing the Key's original ceiling.
	if err := db.Delete(&grant).Error; err != nil {
		t.Fatal(err)
	}
	if got, err := svc.AuthenticateAPIKey(ctx, original.Secret); err != nil || len(got.ModelIDs) != 0 {
		t.Fatalf("grant revocation not effective: %v", err)
	}
	if err := db.Create(&grant).Error; err != nil {
		t.Fatal(err)
	}
	rotate := func() CreatedKeyResponse {
		t.Helper()
		res := identityRequest(router, "POST", "/api/v1/keys/"+original.Key.ID+"/rotate", "", cookie, auth.CSRFToken)
		expectStatus(t, res, 201)
		var data CreatedKeyResponse
		if err := json.Unmarshal(res.Body.Bytes(), &data); err != nil {
			t.Fatal(err)
		}
		return data
	}
	canceled := rotate()
	expectStatus(t, identityRequest(router, "DELETE", "/api/v1/keys/"+canceled.Key.ID, "", cookie, auth.CSRFToken), 204)
	if _, err := svc.AuthenticateAPIKey(ctx, original.Secret); err != nil {
		t.Fatal("canceling rotation revoked original", err)
	}
	// Delivery confirmation may activate multiple replacements; it never proves
	// application rollout or retires the old credential.
	a, b := rotate(), rotate()
	var wg sync.WaitGroup
	codes := make(chan int, 2)
	for _, key := range []CreatedKeyResponse{a, b} {
		wg.Go(func() {
			codes <- identityRequest(router, "POST", "/api/v1/keys/"+key.Key.ID+"/confirm", "", cookie, auth.CSRFToken).Code
		})
	}
	wg.Wait()
	close(codes)
	successes, conflicts := 0, 0
	for code := range codes {
		switch code {
		case 200:
			successes++
		case 409:
			conflicts++
		default:
			t.Fatalf("concurrent confirmation status %d", code)
		}
	}
	if successes != 2 || conflicts != 0 {
		t.Fatalf("rotation winners %d conflicts %d", successes, conflicts)
	}
	if _, err := svc.AuthenticateAPIKey(ctx, original.Secret); err != nil {
		t.Fatal("confirmation prematurely retired original")
	}
	for _, key := range []CreatedKeyResponse{a, b} {
		if _, err := svc.AuthenticateAPIKey(ctx, key.Secret); err != nil {
			continue
		}
		expectStatus(t, identityRequest(router, "PATCH", "/api/v1/keys/"+key.Key.ID, `{"enabled":false,"name":"Paused"}`, cookie, auth.CSRFToken), 200)
		if _, err := svc.AuthenticateAPIKey(ctx, key.Secret); err == nil {
			t.Fatal("disabled key still valid")
		}
		disabledRotation, err := svc.RotatePersonalKey(ctx, auth.User.ID, key.Key.ID)
		if err != nil {
			t.Fatal(err)
		}
		confirmed, err := svc.ConfirmKeyDelivery(ctx, auth.User.ID, disabledRotation.Record.Key.ID)
		if err != nil || confirmed.Key.Status != entity.KeyDisabled {
			t.Fatalf("rotation widened activation: %v", err)
		}
	}
	pending := create()
	if err := db.Model(&entity.APIKey{}).Where("id = ?", pending.Key.ID).Update("delivery_expires_at", time.Now().Add(-time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	expectStatus(t, identityRequest(router, "POST", "/api/v1/keys/"+pending.Key.ID+"/confirm", "", cookie, auth.CSRFToken), 409)
	fresh := create()
	confirm(fresh.Key.ID)
	if err := svc.RevokePersonalKey(ctx, "usr_not_owner", fresh.Key.ID); err == nil {
		t.Fatal("cross-owner revoke allowed")
	}
	// Cross-owner operations must be indistinguishable from an absent ID.
	if _, err := svc.UpdatePersonalKey(ctx, "usr_not_owner", fresh.Key.ID, nil, new(true)); err == nil {
		t.Fatal("cross-owner update allowed")
	}
	if err := svc.RevokePersonalKey(ctx, auth.User.ID, fresh.Key.ID); err != nil {
		t.Fatal(err)
	}
	restarted, err := service.New(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := restarted.AuthenticateAPIKey(ctx, fresh.Secret); err == nil {
		t.Fatal("revocation lost after service restart")
	}
	if _, err := svc.UpdatePersonalKey(ctx, auth.User.ID, fresh.Key.ID, nil, new(true)); err == nil {
		t.Fatal("revoked key reactivated")
	}
	var audits int64
	if err := db.Model(&entity.AuditEvent{}).Where("resource_type = ?", "api_key").Count(&audits).Error; err != nil || audits < 10 {
		t.Fatalf("audit trail %d: %v", audits, err)
	}
}
