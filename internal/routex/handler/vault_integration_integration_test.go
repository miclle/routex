package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/secretstore"
	"gorm.io/gorm"
)

// Proposed appended case136. The normal harness proves empty creation first;
// this scenario reconstructs only V72 metadata and preserves exact old history.
func testVaultIntegrationMigration(t *testing.T, db *gorm.DB) {
	ctx := context.Background()
	var ledger []int
	if e := db.Table("schema_migrations").Order("version").Pluck("version", &ledger).Error; e != nil {
		t.Fatal(e)
	}
	if len(ledger) < 72 {
		t.Fatal("expected retained V72 prefix", ledger)
	}
	for i, v := range ledger {
		if v != i+1 {
			t.Fatal("unordered migration history")
		}
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	old := entity.SecretRotationJob{ID: "srt_vault_history", InventoryVersion: 1, SourceKeyID: "old", TargetKeyID: "target", CutoverEpoch: 1, ScanGeneration: 1, ETag: strings.Repeat("a", 64), Status: "completed", Phase: "completed", Domain: 5, CountsJSON: `{"provider_credentials":{},"egresses":{},"smtp_settings":{},"storage_revisions":{},"user_mfa":{}}`, CreatedAt: now, UpdatedAt: now, CompletedAt: &now}
	if e := db.Create(&old).Error; e != nil {
		t.Fatal(e)
	}
	if e := db.Take(&old, "id = ?", old.ID).Error; e != nil {
		t.Fatal(e)
	}
	assert := func() {
		t.Helper()
		var got entity.SecretRotationJob
		if e := db.Take(&got, "id = ?", old.ID).Error; e != nil || !reflect.DeepEqual(old, got) {
			t.Fatal("old terminal history relabeled", e)
		}
		var after []int
		if e := db.Table("schema_migrations").Order("version").Pluck("version", &after).Error; e != nil || !reflect.DeepEqual(after, ledger) {
			t.Fatal("ledger drift", e)
		}
		for _, m := range []any{&entity.VaultIntegration{}, &entity.VaultCatalogue{}, &entity.VaultRevision{}, &entity.VaultWriterAuth{}, &entity.VaultReaderAuth{}, &entity.VaultConfigReceipt{}, &entity.VaultProbe{}, &entity.VaultProbeCommand{}} {
			if !db.Migrator().HasTable(m) {
				t.Fatal("missing owned table")
			}
		}
		for _, c := range []struct {
			m any
			n string
		}{{&entity.VaultProbe{}, "ck_vault_probe_state"}, {&entity.VaultProbe{}, "ck_vault_probe_version"}, {&entity.VaultProbeCommand{}, "ck_vault_command_kind"}, {&entity.SecretRotationJob{}, "ck_secret_inventory_version"}, {&entity.SecretProcessVerification{}, "ck_secret_process_inventory_version"}} {
			if !db.Migrator().HasConstraint(c.m, c.n) {
				t.Fatal("missing exact guard", c.n)
			}
		}
	}
	remove := func() {
		t.Helper()
		r := db.Table("schema_migrations").Where("version = ?", 72).Delete(&struct{}{})
		if r.Error != nil || r.RowsAffected != 1 {
			t.Fatal("own ledger removal", r.Error)
		}
	}
	// Historical predecessor lacked inventory_version. Preserve the old sentinel.
	for _, c := range []struct {
		m any
		n string
	}{{&entity.SecretRotationJob{}, "ck_secret_rotation_domain"}, {&entity.SecretRotationJob{}, "ck_secret_inventory_version"}, {&entity.SecretProcessVerification{}, "ck_secret_process_inventory_version"}} {
		if e := db.Migrator().DropConstraint(c.m, c.n); e != nil {
			t.Fatal(e)
		}
	}
	for _, m := range []any{&entity.SecretRotationJob{}, &entity.SecretProcessVerification{}} {
		if e := db.Migrator().DropColumn(m, "InventoryVersion"); e != nil {
			t.Fatal(e)
		}
	}
	remove()
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Go(func() { results <- database.Migrate(ctx, db) })
	}
	wg.Wait()
	close(results)
	for e := range results {
		if e != nil {
			t.Fatal(e)
		}
	}
	assert()
	if e := database.Migrate(ctx, db); e != nil {
		t.Fatal(e)
	}
	assert()
	// A committed table/index with a missing constraint is repaired on replay.
	if e := db.Migrator().DropConstraint(&entity.VaultProbe{}, "ck_vault_probe_state"); e != nil {
		t.Fatal(e)
	}
	remove()
	if e := database.Migrate(ctx, db); e != nil {
		t.Fatal(e)
	}
	assert()
	for _, version := range []int{0, 3} {
		if e := db.Model(&entity.SecretRotationJob{}).Where("id = ?", old.ID).Update("InventoryVersion", version).Error; e == nil {
			t.Fatal("invalid inventory version persisted")
		}
	}
	if e := db.Model(&entity.SecretRotationJob{}).Where("id = ?", old.ID).Update("Domain", 6).Error; e == nil {
		t.Fatal("legacy sentinel silently became new domain")
	}
	if e := db.Create(&entity.VaultWriterAuth{ID: "vlr_01aaaaaaaaaaaaaaaaaaaaaaaa", SecretGeneration: "vag_orphan", AuthCiphertext: "unknown"}).Error; e == nil {
		t.Fatal("orphan auth revision persisted")
	}
	assert()
}

// Proposed appended case137. Remote effects use a finite local Vault stub;
// no inference route, Key, stored Provider switch or native call is involved.
func testVaultIntegrationsLifecycle(t *testing.T, db *gorm.DB) {
	ctx := context.Background()
	store, e := secretstore.New(bytes.Repeat([]byte{114}, 32))
	if e != nil {
		t.Fatal(e)
	}
	var calls atomic.Int32
	var markerMu sync.Mutex
	marker := ""
	var path string
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		markerMu.Lock()
		defer markerMu.Unlock()
		if !strings.HasPrefix(r.URL.Path, "/v1/kv/") {
			t.Error("unexpected Vault path")
			w.WriteHeader(404)
			return
		}
		switch r.Method {
		case "POST":
			if r.Header.Get("X-Vault-Token") != "fixture-writer" {
				t.Error("wrong writer")
			}
			var v struct {
				Data    map[string]string `json:"data"`
				Options struct {
					CAS int `json:"cas"`
				} `json:"options"`
			}
			if json.NewDecoder(r.Body).Decode(&v) != nil || v.Options.CAS != 0 {
				t.Error("not CAS0")
			}
			path = r.URL.Path
			marker = v.Data["value"]
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":{"version":1,"destroyed":false,"deletion_time":""}}`))
		case "GET":
			if r.Header.Get("X-Vault-Token") != "fixture-reader" || r.URL.Path != path || r.URL.RawQuery != "version=1" {
				t.Error("wrong exact reader")
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"data": map[string]string{"value": marker}, "metadata": map[string]any{"version": 1, "destroyed": false, "deletion_time": ""}}})
		case "PUT":
			if r.Header.Get("X-Vault-Token") != "fixture-writer" || r.URL.Path != strings.Replace(path, "/data/", "/destroy/", 1) {
				t.Error("wrong exact destroy")
			}
			var v struct {
				Versions []int `json:"versions"`
			}
			if json.NewDecoder(r.Body).Decode(&v) != nil || !reflect.DeepEqual(v.Versions, []int{1}) {
				t.Error("destroy scope")
			}
			w.WriteHeader(204)
		default:
			t.Error("unexpected effect")
			w.WriteHeader(400)
		}
	}))
	defer stub.Close()
	svc, e := service.New(ctx, db, service.WithCredentialStorage(store), service.WithUpstreamPolicy(true))
	if e != nil {
		t.Fatal(e)
	}
	router := fox.New()
	New(svc).RegisterRoutes(router)
	setup := identityRequest(router, "POST", "/api/v1/setup", `{"email":"vault-admin@example.invalid","password":"test-only-vault-password","name":"Vault admin"}`, nil, "")
	expectStatus(t, setup, 201)
	admin, cookie := readIdentity(t, setup)
	_, memberCookie, memberCSRF := createSystemStatusMember(t, svc, router, admin.User.ID, "vault-member", []string{"secrets.read", "secrets.write", "secrets.test"})
	request := func(method, target, raw, etag string, session *http.Cookie, csrf string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "http://routex.test"+target, strings.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		if session != nil {
			req.AddCookie(session)
		}
		if csrf != "" {
			req.Header.Set("X-CSRF-Token", csrf)
		}
		if method != "GET" {
			req.Header.Set("Origin", "http://routex.test")
		}
		if etag != "" {
			req.Header.Set("If-Match", `"`+etag+`"`)
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}
	base := "/api/v1/admin/secrets/integrations"
	list := request("GET", base, "", "", cookie, "")
	expectStatus(t, list, 200)
	var page service.VaultIntegrationPage
	if json.Unmarshal(list.Body.Bytes(), &page) != nil || page.NextCursor != nil || len(page.Items) != 0 || !page.CanWrite || !page.CanTest {
		t.Fatal("empty catalogue contract")
	}
	expectStatus(t, request("GET", base, "", "", memberCookie, ""), 403)
	input := service.VaultConfigInput{RequestID: "72000000-1111-4111-8111-111111111111", Name: "Vault\ufeff", Descriptor: service.VaultDescriptor{Endpoint: stub.URL, Mount: "kv", Prefix: "routex-probes", DataField: "value"}, WriterAuth: service.VaultAuthInput{Action: "replace", Token: "fixture-writer"}, ReaderAuth: service.VaultAuthInput{Action: "replace", Token: "fixture-reader"}, Reason: "Reviewed integration"}
	raw, _ := json.Marshal(input)
	expectStatus(t, request("POST", base, string(raw), page.ReviewETag, memberCookie, memberCSRF), 403)
	expectStatus(t, request("POST", base, string(raw), "", cookie, admin.CSRFToken), 428)
	created := request("POST", base, string(raw), page.ReviewETag, cookie, admin.CSRFToken)
	expectStatus(t, created, 200)
	var saved service.VaultConfigResult
	if json.Unmarshal(created.Body.Bytes(), &saved) != nil || !saved.Committed || !saved.Changed {
		t.Fatal("saved revision")
	}
	expectStatus(t, request("POST", base, string(raw), page.ReviewETag, cookie, admin.CSRFToken), 200)
	target := base + "/" + saved.IntegrationID
	get := request("GET", target, "", "", cookie, "")
	expectStatus(t, get, 200)
	var current service.VaultIntegrationView
	if json.Unmarshal(get.Body.Bytes(), &current) != nil || get.Header().Get("ETag") != `"`+current.ReviewETag+`"` || !current.WriterAuth.Configured || !current.ReaderAuth.Configured || current.LastProbe != nil {
		t.Fatal("public metadata")
	}
	if strings.Contains(get.Body.String(), "fixture-writer") || strings.Contains(get.Body.String(), "expected_sha256") || calls.Load() != 0 {
		t.Fatal("secret DTO or implicit probe")
	}
	// Removing write authority preserves independent read/test and does not mutate config.
	if e := db.Where("role_id = ? AND permission = ?", "rol_admin", "secrets.write").Delete(&entity.RolePermission{}).Error; e != nil {
		t.Fatal(e)
	}
	input.RequestID = "72000000-2222-4222-8222-222222222222"
	input.Name = "Denied change"
	raw, _ = json.Marshal(input)
	expectStatus(t, request("PUT", target, string(raw), current.ReviewETag, cookie, admin.CSRFToken), 403)
	stage := service.VaultStageInput{RequestID: "72000000-3333-4333-8333-333333333333", Reason: "Reviewed write"}
	stageRaw, _ := json.Marshal(stage)
	var failAudit atomic.Bool
	callback := "test:vault_audit_failure"
	if e := db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
		if row, ok := tx.Statement.Dest.(*entity.AuditEvent); ok && failAudit.Load() && row.Action == "vault.integration.write" {
			_ = tx.AddError(errors.New("controlled audit failure"))
		}
	}); e != nil {
		t.Fatal(e)
	}
	defer func() { _ = db.Callback().Create().Remove(callback) }()
	failAudit.Store(true)
	expectStatus(t, request("POST", target+"/probes/write", string(stageRaw), current.ReviewETag, cookie, admin.CSRFToken), 500)
	if calls.Load() != 0 {
		t.Fatal("effect before claim commit")
	}
	failAudit.Store(false)
	written := request("POST", target+"/probes/write", string(stageRaw), current.ReviewETag, cookie, admin.CSRFToken)
	expectStatus(t, written, 200)
	var probe service.VaultProbeView
	if json.Unmarshal(written.Body.Bytes(), &probe) != nil || !probe.Write.Succeeded || probe.Read.Attempted || probe.State != "awaiting_read" || calls.Load() != 1 {
		t.Fatal("Write inferred Read")
	}
	expectStatus(t, request("POST", target+"/probes/write", string(stageRaw), current.ReviewETag, cookie, admin.CSRFToken), 200)
	if calls.Load() != 1 {
		t.Fatal("implicit Write replay")
	}
	stage.RequestID = "72000000-4444-4444-8444-444444444444"
	stage.Reason = "Reviewed reader"
	stageRaw, _ = json.Marshal(stage)
	readPath := target + "/probes/" + probe.ID + "/read"
	read := request("POST", readPath, string(stageRaw), probe.ReviewETag, cookie, admin.CSRFToken)
	expectStatus(t, read, 200)
	var finished service.VaultProbeView
	if json.Unmarshal(read.Body.Bytes(), &finished) != nil || finished.RequestID != probe.RequestID || !finished.Read.Succeeded || finished.Cleanup.State != "acknowledged" || finished.State != "completed" || calls.Load() != 3 {
		t.Fatal("explicit Read receipt")
	}
	expectStatus(t, request("POST", readPath, string(stageRaw), probe.ReviewETag, cookie, admin.CSRFToken), 200)
	if calls.Load() != 3 {
		t.Fatal("implicit Read replay")
	}
	for _, model := range []any{&entity.APIKey{}, &entity.CallRecord{}, &entity.CallAttempt{}} {
		var n int64
		if e := db.Model(model).Count(&n).Error; e != nil || n != 0 {
			t.Fatal("unexpected gateway effect", e)
		}
	}
	testVaultHistoricalCleanupLifecycle(t, db, router, cookie, admin.CSRFToken)
}
