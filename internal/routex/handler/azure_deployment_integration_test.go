package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/secretstore"
	"gorm.io/gorm"
)

func testAzureDeploymentCoverage(t *testing.T, db *gorm.DB) {
	ctx := context.Background()
	var discovery, native atomic.Int32
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/openai/models" {
			native.Add(1)
			w.WriteHeader(500)
			return
		}
		if r.URL.RawQuery != "api-version=2024-10-21" || r.Header.Get("api-key") != "azure-test-only" || r.Header.Get("Authorization") != "" {
			t.Error("Azure discovery transport")
		}
		discovery.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[{"id":"foundation-model-not-a-deployment"}]}`)
	}))
	defer remote.Close()
	store, e := secretstore.New(bytes.Repeat([]byte{92}, 32))
	if e != nil {
		t.Fatal(e)
	}
	svc, e := service.New(ctx, db, service.WithCredentialStorage(store), service.WithUpstreamPolicy(true))
	if e != nil {
		t.Fatal(e)
	}
	router := fox.New()
	New(svc).RegisterRoutes(router)
	setup := identityRequest(router, "POST", "/api/v1/setup", `{"email":"azure-coverage@example.invalid","password":"test-only-azure-password","name":"Azure admin"}`, nil, "")
	expectStatus(t, setup, 201)
	admin, cookie := readIdentity(t, setup)
	_, readerCookie, readerCSRF := createSystemStatusMember(t, svc, router, admin.User.ID, "azure-reader", []string{"providers.read"})
	_, deniedCookie, _ := createSystemStatusMember(t, svc, router, admin.User.ID, "azure-denied", []string{"models.read_all"})
	request := func(method, path string, body any, etag string, c *http.Cookie, csrf string) *httptest.ResponseRecorder {
		t.Helper()
		var raw []byte
		if body != nil {
			var e error
			raw, e = json.Marshal(body)
			if e != nil {
				t.Fatal(e)
			}
		}
		req := httptest.NewRequest(method, path, bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", "http://example.com")
		if c != nil {
			req.AddCookie(c)
		}
		if csrf != "" {
			req.Header.Set("X-CSRF-Token", csrf)
		}
		if etag != "" {
			req.Header.Set("If-Match", strconv.Quote(etag))
		}
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		return res
	}
	provider := decodeCatalogResponse[ProviderResponse](t, request("POST", "/api/v1/admin/providers", map[string]any{"name": "Azure supplier", "connection_name": "Classic Chat", "base_url": remote.URL, "protocol": "openai_chat", "adapter": "azure_openai_classic", "api_version": "2024-10-21", "credential_name": "Key", "secret": "azure-test-only"}, "", cookie, admin.CSRFToken), 201)
	connection := provider.Connections[0]
	credential := connection.Credentials[0]
	if connection.Adapter != "azure_openai_classic" || connection.APIVersion == nil || *connection.APIVersion != "2024-10-21" {
		t.Fatal("transport projection")
	}
	path := "/api/v1/admin/credentials/" + credential.ID
	coveragePath := path + "/deployment-coverage"
	pm := decodeCatalogResponse[ProviderModelResponse](t, request("POST", "/api/v1/admin/connections/"+connection.ID+"/models", map[string]string{"upstream_name": "deployment-exact-A"}, "", cookie, admin.CSRFToken), 201)
	verification := decodeCatalogResponse[VerifyCredentialResponse](t, request("POST", path+"/verify", map[string]any{}, "", cookie, admin.CSRFToken), 200)
	if !verification.Verified || verification.DiscoveredModels != 0 || discovery.Load() != 1 || native.Load() != 0 {
		t.Fatal("authentication falsely claimed deployment discovery")
	}
	var discovered, models int64
	if e := db.Model(&entity.CredentialModelAccess{}).Where("credential_id = ?", credential.ID).Count(&discovered).Error; e != nil {
		t.Fatal(e)
	}
	if e := db.Model(&entity.ProviderModel{}).Where("connection_id = ?", connection.ID).Count(&models).Error; e != nil || discovered != 0 || models != 1 {
		t.Fatal("Azure model catalogue materialized routable coverage")
	}
	var failAudit, failPublication atomic.Bool
	const auditFault = "test:azure-coverage-audit"
	const publicationFault = "test:azure-coverage-publication"
	if e := db.Callback().Create().Before("gorm:create").Register(auditFault, func(tx *gorm.DB) {
		if row, ok := tx.Statement.Dest.(*entity.AuditEvent); ok && row.Action == "credential.deployment_coverage" && failAudit.Load() {
			_ = tx.AddError(errors.New("controlled coverage audit failure"))
		}
	}); e != nil {
		t.Fatal(e)
	}
	defer func() { _ = db.Callback().Create().Remove(auditFault) }()
	if e := db.Callback().Query().Before("gorm:query").Register(publicationFault, func(tx *gorm.DB) {
		if failPublication.Load() && tx.Statement.Table == "models" {
			_ = tx.AddError(errors.New("controlled coverage publication failure"))
		}
	}); e != nil {
		t.Fatal(e)
	}
	defer func() { _ = db.Callback().Query().Remove(publicationFault) }()
	if e := svc.StartRuntime(ctx); e != nil {
		t.Fatal(e)
	}
	defer svc.StopRuntime()
	get := func(c *http.Cookie) service.DeploymentCoverageRecord {
		t.Helper()
		res := request("GET", coveragePath, nil, "", c, "")
		view := decodeCatalogResponse[service.DeploymentCoverageRecord](t, res, 200)
		if res.Header().Get("ETag") != strconv.Quote(view.ETag) || !strings.Contains(res.Header().Get("Cache-Control"), "no-store") {
			t.Fatal("review headers")
		}
		return view
	}
	view := get(cookie)
	if len(view.ProviderModels) != 1 || view.ProviderModels[0].ID != pm.ID || view.ProviderModels[0].Attested || !view.ProviderModels[0].CanAttest {
		t.Fatal("initial coverage")
	}
	readView := get(readerCookie)
	if readView.CanEdit {
		t.Fatal("read permission broadened write")
	}
	expectStatus(t, request("PUT", coveragePath, map[string]any{"provider_model_ids": []string{pm.ID}, "reason": "Unauthorized"}, readView.ETag, readerCookie, readerCSRF), 403)
	expectStatus(t, request("GET", coveragePath, nil, "", deniedCookie, ""), 403)
	expectStatus(t, request("GET", coveragePath, nil, "", nil, ""), 401)
	intent := map[string]any{"provider_model_ids": []string{pm.ID}, "reason": "Reviewed deployment permission"}
	failAudit.Store(true)
	expectStatus(t, request("PUT", coveragePath, intent, view.ETag, cookie, admin.CSRFToken), 500)
	failAudit.Store(false)
	if get(cookie).ProviderModels[0].Attested {
		t.Fatal("audit failure committed coverage")
	}
	failPublication.Store(true)
	expectStatus(t, request("PUT", coveragePath, intent, view.ETag, cookie, admin.CSRFToken), 503)
	failPublication.Store(false)
	saved := decodeCatalogResponse[service.DeploymentCoverageWriteResult](t, request("PUT", coveragePath, intent, view.ETag, cookie, admin.CSRFToken), 200)
	if !saved.RuntimeApplied || saved.Changed || !saved.Coverage.ProviderModels[0].Attested {
		t.Fatal("exact uncertain current-value reconciliation")
	}
	if e := db.Model(&entity.CredentialModelAccess{}).Where("credential_id = ?", credential.ID).Count(&discovered).Error; e != nil || discovered != 0 {
		t.Fatal("attestation persisted as discovery")
	}
	// Withdrawal must invalidate old desired-set review even after a failed publication.
	revoke := map[string]any{"provider_model_ids": []string{}, "reason": "Withdraw deployment permission"}
	failPublication.Store(true)
	expectStatus(t, request("PUT", coveragePath, revoke, saved.Coverage.ETag, cookie, admin.CSRFToken), 503)
	failPublication.Store(false)
	expectStatus(t, request("PUT", coveragePath, intent, view.ETag, cookie, admin.CSRFToken), 409)
	revoked := get(cookie)
	if revoked.ProviderModels[0].Attested {
		t.Fatal("withdrawal restored stale coverage")
	}
	decodeCatalogResponse[service.DeploymentCoverageWriteResult](t, request("PUT", coveragePath, revoke, saved.Coverage.ETag, cookie, admin.CSRFToken), 200)
	expectStatus(t, request("PUT", coveragePath, revoke, view.ETag, cookie, admin.CSRFToken), 409)
	changedReason := map[string]any{"provider_model_ids": []string{}, "reason": "Changed uncertain reason"}
	expectStatus(t, request("PUT", coveragePath, changedReason, saved.Coverage.ETag, cookie, admin.CSRFToken), 409)
	var revisionBefore, revisionAfter entity.ProviderCredential
	if e := db.Where("id = ?", credential.ID).Take(&revisionBefore).Error; e != nil {
		t.Fatal(e)
	}
	decodeCatalogResponse[service.DeploymentCoverageWriteResult](t, request("PUT", coveragePath, revoke, revoked.ETag, cookie, admin.CSRFToken), 200)
	if e := db.Where("id = ?", credential.ID).Take(&revisionAfter).Error; e != nil || revisionBefore.CoverageRevision != revisionAfter.CoverageRevision {
		t.Fatal("unchanged reconciliation advanced revision")
	}
	// A new Credential is a separate secret identity, even on the same Connection.
	replacement := decodeCatalogResponse[CredentialResponse](t, request("POST", "/api/v1/admin/connections/"+connection.ID+"/credentials", map[string]any{"name": "Separate key", "secret": "azure-test-only", "priority": 1}, "", cookie, admin.CSRFToken), 201)
	replacementView := decodeCatalogResponse[service.DeploymentCoverageRecord](t, request("GET", "/api/v1/admin/credentials/"+replacement.ID+"/deployment-coverage", nil, "", cookie, ""), 200)
	if replacementView.ProviderModels[0].Attested {
		t.Fatal("replacement inherited attestation")
	}
	if discovery.Load() != 1 || native.Load() != 0 {
		t.Fatal("coverage mutation made upstream request")
	}
}
