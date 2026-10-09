package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/limits"
	"github.com/miclle/routex/pkg/pricing"
	"github.com/miclle/routex/pkg/secret"
	"github.com/miclle/routex/pkg/secretstore"
	"gorm.io/gorm"
)

func testProjectRateLimitRequestLifecycle(t *testing.T, db *gorm.DB) {
	ctx := context.Background()
	store, err := secretstore.New(bytes.Repeat([]byte{101}, 32))
	if err != nil {
		t.Fatal(err)
	}
	type blockedDispatch struct {
		entered chan struct{}
		release chan struct{}
		once    sync.Once
	}
	var blockMu sync.Mutex
	var blocked *blockedDispatch
	setBlock := func() *blockedDispatch {
		b := &blockedDispatch{entered: make(chan struct{}, 4), release: make(chan struct{})}
		blockMu.Lock()
		blocked = b
		blockMu.Unlock()
		return b
	}
	waitDispatch := func(b *blockedDispatch) {
		t.Helper()
		select {
		case <-b.entered:
		case <-time.After(5 * time.Second):
			t.Fatal("native held dispatch did not reach upstream")
		}
	}
	clearBlock := func(b *blockedDispatch) {
		blockMu.Lock()
		blocked = nil
		blockMu.Unlock()
		b.once.Do(func() { close(b.release) })
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		blockMu.Lock()
		b := blocked
		blockMu.Unlock()
		if b != nil {
			b.entered <- struct{}{}
			select {
			case <-b.release:
			case <-r.Context().Done():
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"Completed"},"finish_reason":"stop"}],"usage":{"prompt_tokens":4,"completion_tokens":1,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0}}}`)
	}))
	defer upstream.Close()
	// Release held requests before the server waits for active connections.
	defer func() {
		blockMu.Lock()
		b := blocked
		blocked = nil
		blockMu.Unlock()
		if b != nil {
			b.once.Do(func() { close(b.release) })
		}
	}()
	makeService := func() *service.Service {
		svc, err := service.New(ctx, db, service.WithCredentialStorage(store), service.WithUpstreamPolicy(true), service.WithRuntimeRefreshInterval(time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		return svc
	}
	svc := makeService()
	admin, err := svc.Initialize(ctx, "rate-request-admin@example.invalid", "rate-request-password", "Rate reviewer")
	if err != nil {
		t.Fatal(err)
	}
	manager, err := svc.CreateMember(ctx, admin.User.ID, "rate-request-manager@example.invalid", "rate-request-password", "Rate manager", entity.RoleMember)
	if err != nil {
		t.Fatal(err)
	}
	other, err := svc.CreateMember(ctx, admin.User.ID, "rate-request-other@example.invalid", "rate-request-password", "Other member", entity.RoleMember)
	if err != nil {
		t.Fatal(err)
	}
	modelID, projectID := "mdl_rate_request", "prj_rate_request"
	adminBearer, projectBearer := "rx_"+strings.Repeat("q", 43), "rxp_"+strings.Repeat("r", 43)
	cipher, err := store.Seal("crd_rate_request", "rate-request-upstream-secret")
	if err != nil {
		t.Fatal(err)
	}
	create := func(rows ...any) {
		t.Helper()
		for _, row := range rows {
			if err := db.Create(row).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	create(
		&entity.Provider{ID: "prv_rate_request", Name: "Rate request provider"},
		&entity.ProviderConnection{ID: "con_rate_request", ProviderID: "prv_rate_request", Name: "Native", Protocol: entity.ProtocolOpenAIChat, BaseURL: upstream.URL + "/v1"},
		&entity.ProviderCredential{ID: "crd_rate_request", ConnectionID: "con_rate_request", Name: "Ready", Ciphertext: cipher, Enabled: true, VerificationStatus: "verified"},
		&entity.ProviderModel{ID: "pmd_rate_request", ConnectionID: "con_rate_request", UpstreamName: "native"},
		&entity.CredentialModelAccess{CredentialID: "crd_rate_request", ProviderModelID: "pmd_rate_request"},
		&entity.Model{ID: modelID, Status: entity.ResourceActive},
		&entity.ModelName{Name: "rate-request-native", ModelID: modelID, CurrentModelID: &modelID},
		&entity.ModelProviderBinding{ID: "bnd_rate_request", ModelID: modelID, ProviderModelID: "pmd_rate_request", Weight: 100},
		&entity.UserModelGrant{UserID: admin.User.ID, ModelID: modelID},
		&entity.APIKey{ID: "key_rate_request", UserID: admin.User.ID, Name: "Warmup", Prefix: "rx_masked", TokenHash: secret.SHA256Hex(adminBearer), Status: entity.KeyActive},
		&entity.APIKeyModel{KeyID: "key_rate_request", ModelID: modelID},
	)
	router := fox.New()
	New(svc).RegisterRoutes(router)
	spool := filepath.Join(t.TempDir(), "rate-request.db")
	start := func() {
		t.Helper()
		if err := svc.StartRuntime(ctx); err != nil {
			t.Fatal(err)
		}
		if err := svc.StartCallRecorder(ctx, spool); err != nil {
			t.Fatal(err)
		}
	}
	start()
	defer func() { svc.StopRuntime(); _ = svc.StopCallRecorder() }()
	call := func(bearer string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "http://routex.test/v1/chat/completions", strings.NewReader(`{"model":"rate-request-native","messages":[{"role":"user","content":"hello"}],"max_completion_tokens":1}`))
		req.RemoteAddr = "127.0.0.1:43210"
		req.Header.Set("Authorization", "Bearer "+bearer)
		req.Header.Set("Content-Type", "application/json")
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		return res
	}
	expectStatus(t, call(adminBearer), 200)
	if err := svc.FlushCallRecorder(ctx); err != nil {
		t.Fatal(err)
	}
	create(
		&entity.Project{ID: projectID, Name: "Rate request Project", Description: "Scoped request acceptance", CreatorID: manager.User.ID, Status: entity.ResourceActive},
		&entity.ProjectManager{ID: "pjm_rate_request", ProjectID: projectID, UserID: manager.User.ID},
		&entity.ProjectModelGrant{ProjectID: projectID, ModelID: modelID},
		&entity.ProjectKey{ID: "pky_rate_request", ProjectID: projectID, CreatorID: manager.User.ID, Name: "Project", Prefix: "rxp_masked", TokenHash: secret.SHA256Hex(projectBearer), Status: entity.KeyActive, DeliveryMode: "manual"},
		&entity.ProjectKeyModel{KeyID: "pky_rate_request", ModelID: modelID},
	)
	if err := svc.RefreshRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	adminSession, adminCookie := readIdentity(t, identityRequest(router, "POST", "/api/v1/auth/login", `{"email":"rate-request-admin@example.invalid","password":"rate-request-password"}`, nil, ""))
	managerSession, managerCookie := readIdentity(t, identityRequest(router, "POST", "/api/v1/auth/login", `{"email":"rate-request-manager@example.invalid","password":"rate-request-password"}`, nil, ""))
	sendRaw := func(cookie *http.Cookie, csrf, method, path, body, etag string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "http://routex.test"+path, strings.NewReader(body))
		req.AddCookie(cookie)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-CSRF-Token", csrf)
		if etag != "" {
			req.Header.Set("If-Match", `"`+etag+`"`)
		}
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		return res
	}
	send := func(cookie *http.Cookie, csrf, method, path string, body any, etag string) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(body)
		return sendRaw(cookie, csrf, method, path, string(raw), etag)
	}
	asAdmin := func(method, path string, body any, etag string) *httptest.ResponseRecorder {
		return send(adminCookie, adminSession.CSRFToken, method, path, body, etag)
	}
	asManager := func(method, path string, body any, etag string) *httptest.ResponseRecorder {
		return send(managerCookie, managerSession.CSRFToken, method, path, body, etag)
	}
	base := "/api/v1/projects/" + projectID
	limitPath, requestPath := base+"/limits", base+"/requests"
	number := func(v int64) *int64 { return &v }
	write := func(policy limits.Policy) service.LimitRecord {
		current := decodeCatalogResponse[service.LimitRecord](t, asAdmin("GET", limitPath, nil, ""), 200)
		return decodeCatalogResponse[service.LimitRecord](t, asAdmin("PUT", limitPath, service.LimitInput{Policy: policy, Reason: "Controlled rate policy"}, current.ETag), 200)
	}
	initial := limits.Policy{TokensMonth: number(1000), Tokens5H: number(1000), Tokens7D: number(1000), RPM: number(1), Concurrency: number(1), IPMode: "allowlist", IPRanges: []string{"127.0.0.1"}}
	initialRecord := write(initial)
	expectStatus(t, asAdmin("PUT", "/api/v1/admin/provider-models/pmd_rate_request/reservation-bound", map[string]any{"max_input_tokens": 4, "max_output_tokens": 1, "evidence": "Controlled five token native output", "reason": "Rate request acceptance"}, "0"), 200)
	contextRecord := decodeCatalogResponse[service.ProjectRequestLimitsContext](t, asManager("GET", base+"/request-limits-context", nil, ""), 200)
	quotaContext := decodeCatalogResponse[service.ProjectQuotaRequestContext](t, asManager("GET", base+"/request-quota-context", nil, ""), 200)
	if contextRecord.ReviewETag != quotaContext.ReviewETag || contextRecord.PolicyETag != initialRecord.ETag || *contextRecord.CurrentRateLimit.RPM != 1 || *contextRecord.CurrentQuota.TokensMonth != 1000 {
		t.Fatal("combined context lost coherent generation")
	}
	expectStatus(t, asAdmin("GET", base+"/request-limits-context", nil, ""), 403)
	for _, raw := range []string{
		`{"request_id":"req_bad","kind":"RATE_LIMIT","rate_limit":null,"reason":"Invalid"}`,
		`{"request_id":"req_bad","kind":"RATE_LIMIT","rate_limit":{},"reason":"Invalid"}`,
		`{"request_id":"req_bad","kind":"RATE_LIMIT","rate_limit":{"rpm":null},"reason":"Invalid"}`,
		`{"request_id":"req_bad","kind":"RATE_LIMIT","rate_limit":{"tpm":1.5},"reason":"Invalid"}`,
		`{"request_id":"req_bad","kind":"RATE_LIMIT","rate_limit":{"concurrency":9007199254740992},"reason":"Invalid"}`,
		`{"request_id":"req_bad","kind":"RATE_LIMIT","rate_limit":{"rpm":2,"currency":"USD"},"reason":"Invalid"}`,
		`{"request_id":"req_bad","kind":"RATE_LIMIT","rate_limit":{"rpm":2},"quota":null,"reason":"Invalid"}`,
		`{"request_id":"req_bad","kind":"RATE_LIMIT","rate_limit":{"rpm":2},"model_ids":[],"reason":"Invalid"}`,
		`{"request_id":"req_bad","kind":"QUOTA","quota":{"tokens_month":10},"rate_limit":null,"reason":"Invalid"}`,
		`{"request_id":"req_bad","model_ids":["mdl_rate_request"],"rate_limit":null,"reason":"Invalid"}`,
	} {
		expectStatus(t, sendRaw(managerCookie, managerSession.CSRFToken, "POST", requestPath, raw, contextRecord.ReviewETag), 400)
	}
	newRate := func(id string, patch *service.ProjectRateLimitPatch) *service.ProjectRequestRecord {
		c, err := svc.GetProjectRequestLimitsContext(ctx, manager.User.ID, projectID)
		if err != nil {
			t.Fatal(err)
		}
		record, err := svc.CreateProjectRequest(ctx, manager.User.ID, projectID, service.ProjectRequestInput{Kind: entity.ProjectRequestRateLimit, RequestID: id, RateLimit: patch, Reason: "Native rate capacity", ReviewETag: c.ReviewETag})
		if err != nil {
			t.Fatal(err)
		}
		return record
	}
	approve := func(record *service.ProjectRequestRecord) (*service.ProjectRequestRecord, service.ProjectRequestDecision) {
		detail, err := svc.GetProjectRequest(ctx, admin.User.ID, projectID, record.ID)
		if err != nil {
			t.Fatal(err)
		}
		decision := service.ProjectRequestDecision{Action: "approve", Reason: "Reviewed native rate capacity", ReviewETag: detail.ApprovalReviewETag}
		result, err := svc.DecideProjectRequest(ctx, admin.User.ID, projectID, record.ID, decision)
		if err != nil {
			t.Fatal(err)
		}
		if result.ApplicationStatus != "applied" || result.RuntimeApplied == nil || !*result.RuntimeApplied || result.ApprovedRateLimit == nil || result.ApprovedQuota != nil {
			t.Fatalf("rate approval lacks exact publication: %+v", result)
		}
		return result, decision
	}
	rpm := newRate("req_rate_rpm", &service.ProjectRateLimitPatch{RPM: number(2)})
	if rpm.BaselineRateLimit == nil || *rpm.BaselineRateLimit.RPM != 1 || rpm.RuntimeApplied != nil {
		t.Fatal("pending request lost immutable rate baseline")
	}
	expectStatus(t, call(projectBearer), 200)
	if err := svc.FlushCallRecorder(ctx); err != nil {
		t.Fatal(err)
	}
	beforeUsage := decodeCatalogResponse[service.LimitRecord](t, asAdmin("GET", limitPath, nil, ""), 200)
	expectStatus(t, call(projectBearer), 429)
	rpmDetail := decodeCatalogResponse[service.ProjectRequestRecord](t, asAdmin("GET", requestPath+"/"+rpm.ID, nil, ""), 200)
	decisionBody := map[string]any{"action": "approve", "reason": "Reviewed native rate capacity"}
	expectStatus(t, asManager("POST", requestPath+"/"+rpm.ID+"/decision", decisionBody, rpmDetail.ApprovalReviewETag), 403)
	expectStatus(t, asAdmin("POST", requestPath+"/"+rpm.ID+"/decision", map[string]any{"action": "approve"}, rpmDetail.ApprovalReviewETag), 400)
	_, _ = approve(rpm)
	current := decodeCatalogResponse[service.LimitRecord](t, asAdmin("GET", limitPath, nil, ""), 200)
	if current.QuotaUsage.Month.TokensUsed != beforeUsage.QuotaUsage.Month.TokensUsed || current.QuotaUsage.Month.TokensUsed != 5 || *current.Stored.TokensMonth != 1000 || *current.Stored.Tokens5H != 1000 || *current.Stored.Tokens7D != 1000 || current.Stored.IPMode != "allowlist" || current.Stored.IPRanges[0] != "127.0.0.1/32" {
		t.Fatal("rate approval changed quota, IP or settled usage")
	}
	expectStatus(t, call(projectBearer), 200)
	if err := svc.FlushCallRecorder(ctx); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, call(projectBearer), 429)
	// A new Project isolates TPM from the prior Project's rolling RPM count.
	addProject := func(suffix string) (string, string) {
		pid := "prj_rate_" + suffix
		bearer := "rxp_" + strings.Repeat(suffix[:1], 43)
		create(&entity.Project{ID: pid, Name: "Rate " + suffix, CreatorID: manager.User.ID, Status: entity.ResourceActive}, &entity.ProjectManager{ID: "pjm_rate_" + suffix, ProjectID: pid, UserID: manager.User.ID}, &entity.ProjectModelGrant{ProjectID: pid, ModelID: modelID}, &entity.ProjectKey{ID: "pky_rate_" + suffix, ProjectID: pid, CreatorID: manager.User.ID, Name: "Native", Prefix: "rxp_masked", TokenHash: secret.SHA256Hex(bearer), Status: entity.KeyActive, DeliveryMode: "manual"}, &entity.ProjectKeyModel{KeyID: "pky_rate_" + suffix, ModelID: modelID})
		if err := svc.RefreshRuntime(ctx); err != nil {
			t.Fatal(err)
		}
		return pid, bearer
	}
	projectID, projectBearer = addProject("tpm")
	base = "/api/v1/projects/" + projectID
	limitPath = base + "/limits"
	tpmInitial := initial
	tpmInitial.RPM = number(100)
	tpmInitial.TPM = number(5)
	write(tpmInitial)
	tpm := newRate("req_rate_tpm", &service.ProjectRateLimitPatch{TPM: number(10)})
	expectStatus(t, call(projectBearer), 200)
	if err := svc.FlushCallRecorder(ctx); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, call(projectBearer), 429)
	_, _ = approve(tpm)
	expectStatus(t, call(projectBearer), 200)
	if err := svc.FlushCallRecorder(ctx); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, call(projectBearer), 429)
	projectID, projectBearer = addProject("concurrency")
	base = "/api/v1/projects/" + projectID
	limitPath, requestPath = base+"/limits", base+"/requests"
	concurrentInitial := initial
	concurrentInitial.RPM = number(100)
	write(concurrentInitial)
	concurrency := newRate("req_rate_concurrency", &service.ProjectRateLimitPatch{Concurrency: number(2)})
	blockedResult := make(chan *httptest.ResponseRecorder, 2)
	waitResult := func() *httptest.ResponseRecorder {
		t.Helper()
		select {
		case result := <-blockedResult:
			return result
		case <-time.After(5 * time.Second):
			t.Fatal("released native dispatch did not finish")
			return nil
		}
	}
	b := setBlock()
	go func() { blockedResult <- call(projectBearer) }()
	waitDispatch(b)
	expectStatus(t, call(projectBearer), 429)
	clearBlock(b)
	expectStatus(t, waitResult(), 200)
	_, _ = approve(concurrency)
	b = setBlock()
	for range 2 {
		go func() { blockedResult <- call(projectBearer) }()
	}
	for range 2 {
		waitDispatch(b)
	}
	expectStatus(t, call(projectBearer), 429)
	clearBlock(b)
	for range 2 {
		expectStatus(t, waitResult(), 200)
	}
	if err := svc.FlushCallRecorder(ctx); err != nil {
		t.Fatal(err)
	}
	current = decodeCatalogResponse[service.LimitRecord](t, asAdmin("GET", limitPath, nil, ""), 200)
	if current.QuotaUsage.Month.TokensUsed != 15 || current.QuotaUsage.Month.TokensUnknown != 0 {
		t.Fatal("held dispatches changed settled accounting")
	}
	zero := newRate("req_rate_zero", &service.ProjectRateLimitPatch{RPM: number(0)})
	_, _ = approve(zero)
	expectStatus(t, call(projectBearer), 429)
	// Current generation and platform pricing participate in the review; a rate
	// approval preserves a finite monthly budget and cannot reinterpret it.
	money := "20.000000000000000001"
	finite := current.Stored
	finite.MoneyMonth = &money
	finite.Currency = "USD"
	write(finite)
	guarded := newRate("req_rate_guarded", &service.ProjectRateLimitPatch{RPM: number(100)})
	guardedDetail, err := svc.GetProjectRequest(ctx, admin.User.ID, projectID, guarded.ID)
	if err != nil {
		t.Fatal(err)
	}
	fx, err := svc.GetPricingCurrency(ctx, admin.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.WritePricingCurrency(ctx, admin.User.ID, fx.ETag, pricing.FX{PlatformCurrency: "USD", Rates: map[string]string{"USD": "1", "EUR": "2"}}); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, asAdmin("POST", requestPath+"/"+guarded.ID+"/decision", decisionBody, guardedDetail.ApprovalReviewETag), 409)
	if err := db.Model(&entity.PricingSetting{}).Where("id = ?", 1).Update("platform_currency", "EUR").Error; err != nil {
		t.Fatal(err)
	}
	guardedDetail, err = svc.GetProjectRequest(ctx, admin.User.ID, projectID, guarded.ID)
	if err != nil {
		t.Fatal(err)
	}
	expectStatus(t, asAdmin("POST", requestPath+"/"+guarded.ID+"/decision", decisionBody, guardedDetail.ApprovalReviewETag), 409)
	if err := db.Model(&entity.PricingSetting{}).Where("id = ?", 1).Update("platform_currency", "USD").Error; err != nil {
		t.Fatal(err)
	}
	guardedApproved, _ := approve(guarded)
	current = decodeCatalogResponse[service.LimitRecord](t, asAdmin("GET", limitPath, nil, ""), 200)
	if current.Stored.MoneyMonth == nil || *current.Stored.MoneyMonth != money || current.QuotaUsage.Month.TokensUsed != 15 {
		t.Fatal("rate approval replaced exact monthly budget or reset usage")
	}
	// Limited reviewers see both limit kinds, never model history, including
	// when a newer model record would otherwise consume a bounded page.
	create(&entity.Role{ID: "rol_rate_only", Name: "Rate only", NameKey: "rate-only"}, &entity.RolePermission{RoleID: "rol_rate_only", Permission: "projects.limits.write"}, &entity.UserRole{UserID: other.User.ID, RoleID: "rol_rate_only"}, &entity.Model{ID: "mdl_rate_addition", Status: entity.ResourceActive})
	quotaC, err := svc.GetProjectQuotaRequestContext(ctx, manager.User.ID, projectID)
	if err != nil {
		t.Fatal(err)
	}
	quotaR, err := svc.CreateProjectRequest(ctx, manager.User.ID, projectID, service.ProjectRequestInput{Kind: entity.ProjectRequestQuota, RequestID: "req_rate_monthly", Quota: &service.ProjectQuotaPatch{TokensMonth: number(2000)}, Reason: "Independent monthly request", ReviewETag: quotaC.ReviewETag})
	if err != nil {
		t.Fatal(err)
	}
	modelR, err := svc.CreateProjectRequest(ctx, manager.User.ID, projectID, service.ProjectRequestInput{RequestID: "req_rate_model", ModelIDs: []string{"mdl_rate_addition"}, Reason: "Independent model request"})
	if err != nil {
		t.Fatal(err)
	}
	page, err := svc.ListProjectRequests(ctx, other.User.ID, projectID, service.ProjectRequestFilter{Limit: 1})
	if err != nil || len(page.Items) != 1 || page.Items[0].ID != quotaR.ID {
		t.Fatal("per-kind scope failed before page limit", page, err)
	}
	if _, err := svc.GetProjectRequest(ctx, other.User.ID, projectID, modelR.ID); err == nil {
		t.Fatal("limit reviewer read model history")
	}
	if _, err := svc.GetProjectRequest(ctx, other.User.ID, projectID, guarded.ID); err != nil {
		t.Fatal(err)
	}
	// Opposite permission and collated role/manager aliases cannot authorize
	// RATE history, review or the coherent manager-only submission context.
	if err := db.Where("user_id = ?", other.User.ID).Delete(&entity.UserRole{}).Error; err != nil {
		t.Fatal(err)
	}
	create(&entity.Role{ID: "rol_rate_model_only", Name: "Model review only", NameKey: "rate-model-only"}, &entity.RolePermission{RoleID: "rol_rate_model_only", Permission: "projects.models.write"}, &entity.UserRole{UserID: other.User.ID, RoleID: "rol_rate_model_only"})
	modelPage, err := svc.ListProjectRequests(ctx, other.User.ID, projectID, service.ProjectRequestFilter{Limit: 1})
	if err != nil || len(modelPage.Items) != 1 || modelPage.Items[0].ID != modelR.ID || modelPage.NextCursor != "" {
		t.Fatal("model reviewer saw RATE or QUOTA", modelPage, err)
	}
	if _, err := svc.GetProjectRequest(ctx, other.User.ID, projectID, guarded.ID); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatal("model reviewer read rate amount", err)
	}
	if err := db.Where("user_id = ?", other.User.ID).Delete(&entity.UserRole{}).Error; err != nil {
		t.Fatal(err)
	}
	for _, alias := range []entity.UserRole{{UserID: strings.ToUpper(other.User.ID), RoleID: "rol_rate_only"}, {UserID: other.User.ID, RoleID: "ROL_RATE_ONLY"}} {
		err := db.Create(&alias).Error
		if errors.Is(err, gorm.ErrForeignKeyViolated) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if _, err := svc.ListProjectRequests(ctx, other.User.ID, projectID, service.ProjectRequestFilter{Limit: 1}); !errors.Is(err, apperrors.ErrNotFound) {
			t.Fatal("collated role alias granted rate history", err)
		}
		if _, err := svc.DecideProjectRequest(ctx, other.User.ID, projectID, guarded.ID, service.ProjectRequestDecision{Action: "reject", Reason: "Aliased review"}); !errors.Is(err, apperrors.ErrForbidden) {
			t.Fatal("collated role alias granted rate review", err)
		}
		if err := db.Where("user_id = ?", alias.UserID).Delete(&entity.UserRole{}).Error; err != nil {
			t.Fatal(err)
		}
	}
	aliasManager := entity.ProjectManager{ID: "pjm_rate_alias", ProjectID: projectID, UserID: strings.ToUpper(other.User.ID)}
	if err := db.Create(&aliasManager).Error; err == nil {
		if _, err := svc.GetProjectRequestLimitsContext(ctx, other.User.ID, projectID); !errors.Is(err, apperrors.ErrForbidden) {
			t.Fatal("collated manager alias granted rate context", err)
		}
		if err := db.Delete(&aliasManager).Error; err != nil {
			t.Fatal(err)
		}
	} else if !errors.Is(err, gorm.ErrForeignKeyViolated) {
		t.Fatal(err)
	}
	racing := newRate("req_rate_racing", &service.ProjectRateLimitPatch{Concurrency: number(3)})
	racingDetail, err := svc.GetProjectRequest(ctx, admin.User.ID, projectID, racing.ID)
	if err != nil {
		t.Fatal(err)
	}
	racingDB, err := database.Open(ctx, db.Name(), os.Getenv("ROUTEX_TEST_"+strings.ToUpper(db.Name())+"_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	racingPool, _ := racingDB.DB()
	defer func() { _ = racingPool.Close() }()
	racingService, err := service.New(ctx, racingDB)
	if err != nil {
		t.Fatal(err)
	}
	outcomes := make(chan error, 2)
	var wg sync.WaitGroup
	wg.Go(func() {
		_, err := svc.DecideProjectRequest(ctx, admin.User.ID, projectID, racing.ID, service.ProjectRequestDecision{Action: "approve", Reason: "Concurrent rate approval", ReviewETag: racingDetail.ApprovalReviewETag})
		outcomes <- err
	})
	wg.Go(func() {
		_, err := racingService.DecideProjectRequest(ctx, manager.User.ID, projectID, racing.ID, service.ProjectRequestDecision{Action: "withdraw"})
		outcomes <- err
	})
	wg.Wait()
	close(outcomes)
	accepted := 0
	for outcome := range outcomes {
		if outcome == nil {
			accepted++
		} else {
			var code interface{ StatusCode() int }
			if !errors.As(outcome, &code) || code.StatusCode() != 409 {
				t.Fatal("unexpected concurrent terminal result", outcome)
			}
		}
	}
	if accepted != 1 {
		t.Fatal("concurrent rate request has multiple terminal decisions", accepted)
	}
	var terminalCount int64
	if err := db.Model(&entity.AuditEvent{}).Where("resource_id = ? AND action IN ?", racing.ID, []string{"project.request.approve", "project.request.withdraw"}).Count(&terminalCount).Error; err != nil || terminalCount != 1 {
		t.Fatal("concurrent decisions duplicated audit", terminalCount, err)
	}
	// A failed post-commit publish retains its exact decision. Rejected retries
	// never reconcile uncertainty, and the original intent does not write twice.
	uncertain := newRate("req_rate_uncertain", &service.ProjectRateLimitPatch{TPM: number(100)})
	uncertainDetail, err := svc.GetProjectRequest(ctx, admin.User.ID, projectID, uncertain.ID)
	if err != nil {
		t.Fatal(err)
	}
	const callback = "rate-request-publication-failure"
	if err := db.Callback().Query().Before("gorm:query").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table == "providers" {
			_ = tx.AddError(errors.New("controlled publication failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	uncertainDecision := service.ProjectRequestDecision{Action: "approve", Reason: "Durable rate approval", ReviewETag: uncertainDetail.ApprovalReviewETag}
	_, outage := svc.DecideProjectRequest(ctx, admin.User.ID, projectID, uncertain.ID, uncertainDecision)
	if err := db.Callback().Query().Remove(callback); err != nil {
		t.Fatal(err)
	}
	var status interface{ StatusCode() int }
	if !errors.As(outage, &status) || status.StatusCode() != 503 {
		t.Fatal("missing durable uncertain response", outage)
	}
	changedDecision := uncertainDecision
	changedDecision.ReviewETag = strings.Repeat("b", 64)
	expectStatus(t, asAdmin("POST", requestPath+"/"+uncertain.ID+"/decision", map[string]any{"action": "approve", "reason": uncertainDecision.Reason}, changedDecision.ReviewETag), 409)
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	maxOpen := pool.Stats().MaxOpenConnections
	pool.SetMaxOpenConns(1)
	reconciled, err := svc.DecideProjectRequest(ctx, admin.User.ID, projectID, uncertain.ID, uncertainDecision)
	pool.SetMaxOpenConns(maxOpen)
	if err != nil || reconciled.ApplicationStatus != "applied" {
		t.Fatal("single connection failed exact reconciliation", err)
	}
	var decisions int64
	if err := db.Model(&entity.AuditEvent{}).Where("resource_id = ? AND action = ?", uncertain.ID, "project.request.approve").Count(&decisions).Error; err != nil || decisions != 1 {
		t.Fatal("receipt replay wrote twice", decisions, err)
	}
	later := current.Stored
	later.RPM = number(150)
	laterRecord := write(later)
	replayed, err := svc.DecideProjectRequest(ctx, admin.User.ID, projectID, uncertain.ID, uncertainDecision)
	if err != nil || replayed.ApplicationStatus != "superseded" || *replayed.CurrentRateLimit.RPM != 150 || replayed.ApprovedPolicyETag != reconciled.ApprovedPolicyETag {
		t.Fatal("historical receipt restored rates", err)
	}
	// Reopen an independent pool/runtime and journal; historical retries remain
	// immutable even after the original process state is gone.
	svc.StopRuntime()
	if err := svc.StopCallRecorder(); err != nil {
		t.Fatal(err)
	}
	independent, err := database.Open(ctx, db.Name(), os.Getenv("ROUTEX_TEST_"+strings.ToUpper(db.Name())+"_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { p, _ := independent.DB(); _ = p.Close() }()
	fresh, err := service.New(ctx, independent, service.WithCredentialStorage(store), service.WithUpstreamPolicy(true), service.WithRuntimeRefreshInterval(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if err := fresh.StartRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	defer fresh.StopRuntime()
	if err := fresh.StartCallRecorder(ctx, spool); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = fresh.StopCallRecorder() }()
	replayed, err = fresh.DecideProjectRequest(ctx, admin.User.ID, projectID, uncertain.ID, uncertainDecision)
	if err != nil || replayed.ApplicationStatus != "superseded" || replayed.CurrentPolicyETag != laterRecord.ETag {
		t.Fatal("restart restored historical rates", err)
	}
	if guardedApproved.ApprovedPolicyETag == replayed.CurrentPolicyETag {
		t.Fatal("later policy revision was not retained")
	}
}
