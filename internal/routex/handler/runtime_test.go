package handler

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/secret"
	"github.com/miclle/routex/pkg/secretstore"
)

func testRuntimeLifecycle(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx := context.Background()
	store, err := secretstore.New(bytes.Repeat([]byte{39}, 32))
	if err != nil {
		t.Fatal(err)
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer runtime-test-secret" {
			t.Error("prepared upstream credential mismatch")
		}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/models" {
			_, _ = io.WriteString(w, `{"data":[{"id":"provider-model"}]}`)
			return
		}
		_, _ = io.WriteString(w, `{"model":"provider-model","choices":[]}`)
	}))
	defer upstream.Close()
	privateDB, err := database.Open(ctx, db.Name(), os.Getenv("ROUTEX_TEST_"+strings.ToUpper(db.Name())+"_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	privateDB = privateDB.Session(&gorm.Session{Logger: logger.Discard})
	pool, err := privateDB.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pool.Close() }()
	svc, err := service.New(ctx, privateDB, service.WithCredentialStorage(store), service.WithUpstreamPolicy(true))
	if err != nil {
		t.Fatal(err)
	}
	admin, err := svc.Initialize(ctx, "runtime@example.invalid", "test-only-runtime-password", "Runtime Test")
	if err != nil {
		t.Fatal(err)
	}
	ciphertext, err := store.Seal("crd_runtime", "runtime-test-secret")
	if err != nil {
		t.Fatal(err)
	}
	bearer := "rx_" + strings.Repeat("r", 43)
	modelID := "mdl_runtime"
	rows := []any{
		&entity.Provider{ID: "prv_runtime", Name: "Runtime Provider"},
		&entity.ProviderConnection{ID: "con_runtime", ProviderID: "prv_runtime", Name: "Primary", BaseURL: upstream.URL + "/v1", Protocol: entity.ProtocolOpenAIChat},
		&entity.ProviderCredential{ID: "crd_runtime", ConnectionID: "con_runtime", Name: "Primary", Ciphertext: ciphertext, Enabled: true, VerificationStatus: "verified"},
		&entity.ProviderModel{ID: "pmd_runtime", ConnectionID: "con_runtime", UpstreamName: "provider-model"},
		&entity.CredentialModelAccess{CredentialID: "crd_runtime", ProviderModelID: "pmd_runtime"},
		&entity.Model{ID: modelID, Status: "active"},
		&entity.ModelName{Name: "runtime-model", ModelID: modelID, CurrentModelID: &modelID},
		&entity.ModelProviderBinding{ID: "bnd_runtime", ModelID: modelID, ProviderModelID: "pmd_runtime", Weight: 100},
		&entity.UserModelGrant{UserID: admin.User.ID, ModelID: modelID},
		&entity.APIKey{ID: "key_runtime", UserID: admin.User.ID, Name: "Runtime Key", Prefix: bearer[:11], TokenHash: secret.SHA256Hex(bearer), Status: entity.KeyActive},
		&entity.APIKeyModel{KeyID: "key_runtime", ModelID: modelID},
	}
	for _, row := range rows {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := svc.StartRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	defer svc.StopRuntime()
	// Use explicit refreshes for deterministic fault injection; production polls
	// run under the same publisher and are joined rather than leaked by this test.
	svc.StopRuntime()
	first := svc.RuntimeStatus()
	if !first.Enabled || !first.Ready || first.SnapshotID == "" {
		t.Fatal("initial snapshot not ready")
	}
	var count int64
	if err := db.Model(&entity.RuntimePublication{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatal("publication metadata not persisted")
	}
	testRuntimePublicMutations(t, db, svc, admin.User.ID, bearer)
	first = svc.RuntimeStatus()
	if err := db.Model(&entity.ModelProviderBinding{}).Where("id = ?", "bnd_runtime").Update("weight", 50).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.RefreshRuntime(ctx); err == nil {
		t.Fatal("invalid route publication succeeded")
	}
	if got := svc.RuntimeStatus(); got.SnapshotID != first.SnapshotID || got.ErrorCode != "invalid_configuration" {
		t.Fatal("invalid configuration replaced last-valid snapshot")
	}
	if err := db.Where("user_id = ? AND model_id = ?", admin.User.ID, modelID).Delete(&entity.UserModelGrant{}).Error; err != nil {
		t.Fatal(err)
	}
	svc.InvalidateRuntimeModel(modelID)
	if err := svc.RefreshRuntime(ctx); err == nil {
		t.Fatal("bad routing should still fail independently of authorization")
	}
	key, err := svc.AuthenticateAPIKey(ctx, bearer)
	if err != nil || len(key.ModelIDs) != 0 {
		t.Fatal("last-valid route retained a revoked grant")
	}
	if err := db.Create(&entity.UserModelGrant{UserID: admin.User.ID, ModelID: modelID}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&entity.ModelProviderBinding{}).Where("id = ?", "bnd_runtime").Update("weight", 100).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.RefreshRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	if err := pool.Close(); err != nil {
		t.Fatal(err)
	}
	// A missing secret manager and failed database refresh do not erase an
	// already prepared route. Authentication remains bounded by its short lease.
	service.WithCredentialStorage(nil)(svc)
	if err := svc.RefreshRuntime(ctx); err == nil {
		t.Fatal("closed database refresh succeeded")
	}
	result, err := svc.GatewayChat(ctx, bearer, []byte(`{"model":"runtime-renamed","messages":[{"role":"user","content":"hello"}]}`), "req_runtime")
	if err != nil {
		t.Fatalf("last-valid in-memory call failed: %v", err)
	}
	if err := result.Response.Body.Close(); err != nil {
		t.Error(err)
	}
	svc.InvalidateRuntimeKey("key_runtime")
	if _, err := svc.AuthenticateAPIKey(ctx, bearer); err == nil {
		t.Fatal("in-memory revocation failed during a database outage")
	}
	if err := svc.RefreshAfterMutation(ctx, nil); err == nil {
		t.Fatal("failed publication should not acknowledge a successful mutation")
	}
}

// Polling is stopped by the caller: each assertion depends on the public write
// publishing its state synchronously, rather than eventual background refresh.
func testRuntimePublicMutations(t *testing.T, db *gorm.DB, svc *service.Service, userID, fixtureBearer string) {
	t.Helper()
	ctx := context.Background()
	created, err := svc.CreatePersonalKey(ctx, userID, "Published Key", []string{"mdl_runtime"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AuthenticateAPIKey(ctx, created.Secret); !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatal("pending key became usable")
	}
	if _, err := svc.ConfirmKeyDelivery(ctx, userID, created.Record.Key.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AuthenticateAPIKey(ctx, created.Secret); err != nil {
		t.Fatal("confirmed key not published")
	}
	enabled := false
	if _, err := svc.UpdatePersonalKey(ctx, userID, created.Record.Key.ID, nil, &enabled); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AuthenticateAPIKey(ctx, created.Secret); !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatal("disabled key remained usable")
	}
	enabled = true
	if _, err := svc.UpdatePersonalKey(ctx, userID, created.Record.Key.ID, nil, &enabled); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AuthenticateAPIKey(ctx, created.Secret); err != nil {
		t.Fatal("reenabled key not published")
	}
	replacement, err := svc.RotatePersonalKey(ctx, userID, created.Record.Key.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AuthenticateAPIKey(ctx, created.Secret); err != nil {
		t.Fatal("pending rotation revoked the original too soon")
	}
	if _, err := svc.ConfirmKeyDelivery(ctx, userID, replacement.Record.Key.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AuthenticateAPIKey(ctx, created.Secret); !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatal("confirmed rotation retained the old key")
	}
	if _, err := svc.AuthenticateAPIKey(ctx, replacement.Secret); err != nil {
		t.Fatal("replacement not published")
	}
	if err := svc.RevokePersonalKey(ctx, userID, replacement.Record.Key.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AuthenticateAPIKey(ctx, replacement.Secret); !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatal("revoked replacement remained usable")
	}
	if _, err := svc.SetModelGrants(ctx, userID, "mdl_runtime", nil); err != nil {
		t.Fatal(err)
	}
	key, err := svc.AuthenticateAPIKey(ctx, fixtureBearer)
	if err != nil || len(key.ModelIDs) != 0 {
		t.Fatal("grant reduction was not published")
	}
	if _, err := svc.SetModelGrants(ctx, userID, "mdl_runtime", []string{userID}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetCredentialEnabled(ctx, userID, "crd_runtime", false); err != nil {
		t.Fatal(err)
	}
	if result, err := svc.GatewayChat(ctx, fixtureBearer, []byte(`{"model":"runtime-model","messages":[{"role":"user","content":"test"}]}`), "req_disabled_credential"); err == nil {
		_ = result.Response.Body.Close()
		t.Fatal("disabled credential remained routable")
	} else {
		var gatewayErr *service.GatewayError
		if !errors.As(err, &gatewayErr) || gatewayErr.Code != "upstream_unavailable" {
			t.Fatal("disabled credential failed for an unrelated reason")
		}
	}
	if _, err := svc.SetCredentialEnabled(ctx, userID, "crd_runtime", true); err != nil {
		t.Fatal(err)
	}
	verified, err := svc.VerifyCredential(ctx, userID, "crd_runtime")
	if err != nil || !verified.Verified {
		t.Fatal("credential verification did not publish")
	}
	if _, err := svc.SetModelWeights(ctx, userID, "mdl_runtime", []service.ModelWeight{{BindingID: "bnd_runtime", Weight: 100}}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RenameModel(ctx, userID, "mdl_runtime", "runtime-renamed", nil); err != nil {
		t.Fatal(err)
	}
	if result, err := svc.GatewayChat(ctx, fixtureBearer, []byte(`{"model":"runtime-model","messages":[{"role":"user","content":"test"}]}`), "req_expired_alias"); err == nil {
		_ = result.Response.Body.Close()
		t.Fatal("expired alias remained routable after rename")
	} else {
		var gatewayErr *service.GatewayError
		if !errors.As(err, &gatewayErr) || gatewayErr.Code != "model_not_found" {
			t.Fatal("expired alias failed for an unrelated reason")
		}
	}
	testDisabledKeyOwner(t, db, svc)
}

func testDisabledKeyOwner(t *testing.T, db *gorm.DB, svc *service.Service) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	user := entity.User{ID: "usr_runtime_race", Email: "runtime-race@example.invalid", PasswordHash: "unused-test-hash", Name: "Key Owner", Role: entity.RoleMember}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&entity.UserModelGrant{UserID: user.ID, ModelID: "mdl_runtime"}).Error; err != nil {
		t.Fatal(err)
	}
	pending, err := svc.CreatePersonalKey(ctx, user.ID, "Owner Key", []string{"mdl_runtime"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Hold the same owner lock used by governance while a concurrent issuance
	// attempts to enter. It must recheck disabled state after the lock is released.
	tx := db.WithContext(ctx).Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	defer func() { _ = tx.Rollback().Error }()
	var locked entity.User
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&locked, "id = ?", user.ID).Error; err != nil {
		t.Fatal(err)
	}
	started, done := make(chan struct{}), make(chan error, 1)
	go func() {
		close(started)
		_, err := svc.CreatePersonalKey(ctx, user.ID, "Concurrent Key", []string{"mdl_runtime"}, nil)
		done <- err
	}()
	<-started
	select {
	case <-done:
		t.Fatal("key creation bypassed the account lifecycle lock")
	case <-time.After(50 * time.Millisecond):
	}
	if err := tx.Model(&entity.User{}).Where("id = ?", user.ID).Update("disabled", true).Error; err != nil {
		t.Fatal(err)
	}
	if err := tx.Model(&entity.APIKey{}).Where("user_id = ?", user.ID).Update("status", entity.KeyRevoked).Error; err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit().Error; err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, apperrors.ErrUnauthorized) {
			t.Fatal("in-flight issuance did not reject the disabled account")
		}
	case <-ctx.Done():
		t.Fatal("key owner serialization did not complete")
	}
	enabled := true
	operations := map[string]func() error{
		"create": func() error {
			_, err := svc.CreatePersonalKey(ctx, user.ID, "Disabled Key", []string{"mdl_runtime"}, nil)
			return err
		},
		"confirm": func() error { _, err := svc.ConfirmKeyDelivery(ctx, user.ID, pending.Record.Key.ID); return err },
		"update": func() error {
			_, err := svc.UpdatePersonalKey(ctx, user.ID, pending.Record.Key.ID, nil, &enabled)
			return err
		},
		"rotate": func() error { _, err := svc.RotatePersonalKey(ctx, user.ID, pending.Record.Key.ID); return err },
		"revoke": func() error { return svc.RevokePersonalKey(ctx, user.ID, pending.Record.Key.ID) },
	}
	for name, operation := range operations {
		if err := operation(); !errors.Is(err, apperrors.ErrUnauthorized) {
			t.Fatalf("disabled owner %s did not return unauthorized", name)
		}
	}
	if err := db.Model(&entity.User{}).Where("id = ?", user.ID).Update("disabled", false).Error; err != nil {
		t.Fatal(err)
	}
	var active int64
	if err := db.Model(&entity.APIKey{}).Where("user_id = ? AND status <> ?", user.ID, entity.KeyRevoked).Count(&active).Error; err != nil || active != 0 {
		t.Fatal("reenabling the owner resurrected an issued key")
	}
}
