package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/limits"
	"github.com/miclle/routex/pkg/pricing"
	"github.com/miclle/routex/pkg/secret"
	"github.com/miclle/routex/pkg/secretstore"
	"gorm.io/gorm"
)

func testProjectQuotaRequestLifecycle(t *testing.T, db *gorm.DB) {
	ctx := context.Background()
	store, err := secretstore.New(bytes.Repeat([]byte{101}, 32))
	if err != nil {
		t.Fatal(err)
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"Completed"},"finish_reason":"stop"}],"usage":{"prompt_tokens":4,"completion_tokens":1,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0}}}`)
	}))
	defer upstream.Close()
	makeService := func() *service.Service {
		svc, err := service.New(ctx, db, service.WithCredentialStorage(store), service.WithUpstreamPolicy(true))
		if err != nil {
			t.Fatal(err)
		}
		return svc
	}
	svc := makeService()
	admin, err := svc.Initialize(ctx, "quota-request-admin@example.invalid", "quota-request-password", "Quota reviewer")
	if err != nil {
		t.Fatal(err)
	}
	manager, err := svc.CreateMember(ctx, admin.User.ID, "quota-request-manager@example.invalid", "quota-request-password", "Quota manager", entity.RoleMember)
	if err != nil {
		t.Fatal(err)
	}
	other, err := svc.CreateMember(ctx, admin.User.ID, "quota-request-other@example.invalid", "quota-request-password", "Other member", entity.RoleMember)
	if err != nil {
		t.Fatal(err)
	}
	modelID, projectID := "mdl_quota_request", "prj_quota_request"
	adminBearer, projectBearer := "rx_"+strings.Repeat("q", 43), "rxp_"+strings.Repeat("r", 43)
	cipher, err := store.Seal("crd_quota_request", "quota-request-upstream-secret")
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
		&entity.Provider{ID: "prv_quota_request", Name: "Quota request provider"},
		&entity.ProviderConnection{ID: "con_quota_request", ProviderID: "prv_quota_request", Name: "Native", Protocol: entity.ProtocolOpenAIChat, BaseURL: upstream.URL + "/v1"},
		&entity.ProviderCredential{ID: "crd_quota_request", ConnectionID: "con_quota_request", Name: "Ready", Ciphertext: cipher, Enabled: true, VerificationStatus: "verified"},
		&entity.ProviderModel{ID: "pmd_quota_request", ConnectionID: "con_quota_request", UpstreamName: "native"},
		&entity.CredentialModelAccess{CredentialID: "crd_quota_request", ProviderModelID: "pmd_quota_request"},
		&entity.Model{ID: modelID, Status: entity.ResourceActive},
		&entity.ModelName{Name: "quota-request-native", ModelID: modelID, CurrentModelID: &modelID},
		&entity.ModelProviderBinding{ID: "bnd_quota_request", ModelID: modelID, ProviderModelID: "pmd_quota_request", Weight: 100},
		&entity.UserModelGrant{UserID: admin.User.ID, ModelID: modelID},
		&entity.APIKey{ID: "key_quota_request", UserID: admin.User.ID, Name: "Warmup", Prefix: "rx_masked", TokenHash: secret.SHA256Hex(adminBearer), Status: entity.KeyActive},
		&entity.APIKeyModel{KeyID: "key_quota_request", ModelID: modelID},
	)
	router := fox.New()
	New(svc).RegisterRoutes(router)
	spool := filepath.Join(t.TempDir(), "quota-request.db")
	start := func() {
		t.Helper()
		if err := svc.StartRuntime(ctx); err != nil {
			t.Fatal(err)
		}
		svc.StopRuntime() // Acceptance relies on synchronous publication hooks.
		if err := svc.StartCallRecorder(ctx, spool); err != nil {
			t.Fatal(err)
		}
	}
	start()
	defer func() { svc.StopRuntime(); _ = svc.StopCallRecorder() }()
	call := func(bearer string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "http://routex.test/v1/chat/completions", strings.NewReader(`{"model":"quota-request-native","messages":[{"role":"user","content":"hello"}],"max_completion_tokens":1}`))
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
		&entity.Project{ID: projectID, Name: "Monthly request Project", Description: "Scoped request acceptance", CreatorID: manager.User.ID, Status: entity.ResourceActive},
		&entity.ProjectManager{ID: "pjm_quota_request", ProjectID: projectID, UserID: manager.User.ID},
		&entity.ProjectModelGrant{ProjectID: projectID, ModelID: modelID},
		&entity.ProjectKey{ID: "pky_quota_request", ProjectID: projectID, CreatorID: manager.User.ID, Name: "Project", Prefix: "rxp_masked", TokenHash: secret.SHA256Hex(projectBearer), Status: entity.KeyActive, DeliveryMode: "manual"},
		&entity.ProjectKeyModel{KeyID: "pky_quota_request", ModelID: modelID},
	)
	if err := svc.RefreshRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	adminSession, adminCookie := readIdentity(t, identityRequest(router, "POST", "/api/v1/auth/login", `{"email":"quota-request-admin@example.invalid","password":"quota-request-password"}`, nil, ""))
	managerSession, managerCookie := readIdentity(t, identityRequest(router, "POST", "/api/v1/auth/login", `{"email":"quota-request-manager@example.invalid","password":"quota-request-password"}`, nil, ""))
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
	write := func(policy limits.Policy) service.LimitRecord {
		current := decodeCatalogResponse[service.LimitRecord](t, asAdmin("GET", limitPath, nil, ""), 200)
		return decodeCatalogResponse[service.LimitRecord](t, asAdmin("PUT", limitPath, service.LimitInput{Policy: policy, Reason: "Controlled direct policy"}, current.ETag), 200)
	}
	number := func(value int64) *int64 { return &value }
	initial := limits.Policy{TokensMonth: number(5), Tokens5H: number(100), RPM: number(100), Concurrency: number(2)}
	initialRecord := write(initial)
	expectStatus(t, asAdmin("PUT", "/api/v1/admin/provider-models/pmd_quota_request/reservation-bound", map[string]any{"max_input_tokens": 4, "max_output_tokens": 1, "evidence": "Controlled five token native request", "reason": "Quota request acceptance"}, "0"), 200)
	contextRecord := decodeCatalogResponse[service.ProjectQuotaRequestContext](t, asManager("GET", base+"/request-quota-context", nil, ""), 200)
	if contextRecord.PolicyETag != initialRecord.ETag || *contextRecord.CurrentQuota.TokensMonth != 5 {
		t.Fatal("submission context did not bind current monthly policy")
	}
	for _, raw := range []string{
		`{"request_id":"req_bad","kind":null,"model_ids":["mdl_quota_request"],"reason":"Invalid"}`,
		`{"request_id":"req_bad","kind":"","model_ids":["mdl_quota_request"],"reason":"Invalid"}`,
		`{"request_id":"req_bad","model_ids":["mdl_quota_request"],"quota":null,"reason":"Invalid"}`,
		`{"request_id":"req_bad","kind":"QUOTA","quota":{"tokens_month":10},"model_ids":[],"reason":"Invalid"}`,
		`{"request_id":"req_bad","kind":"QUOTA","quota":{"tokens_month":10},"model_ids":null,"reason":"Invalid"}`,
		`{"request_id":"req_bad","kind":"QUOTA","quota":{"tokens_month":null},"reason":"Invalid"}`,
		`{"request_id":"req_bad","kind":"QUOTA","quota":{"tokens_month":0,"currency":""},"reason":"Invalid"}`,
		`{"request_id":"req_bad","kind":"QUOTA","quota":{},"reason":"Invalid"}`,
		`{"request_id":"req_bad","kind":"QUOTA","quota":{"rpm":10},"reason":"Invalid"}`,
	} {
		expectStatus(t, sendRaw(managerCookie, managerSession.CSRFToken, "POST", requestPath, raw, contextRecord.ReviewETag), 400)
	}
	body := map[string]any{"request_id": "req_quota_first", "kind": "QUOTA", "quota": map[string]any{"tokens_month": 10}, "reason": "Monthly application capacity"}
	expectStatus(t, asManager("POST", requestPath, body, ""), 400)
	request := decodeCatalogResponse[service.ProjectRequestRecord](t, asManager("POST", requestPath, body, contextRecord.ReviewETag), 201)
	if request.Kind != entity.ProjectRequestQuota || request.BaselinePolicyETag != initialRecord.ETag || request.RuntimeApplied != nil {
		t.Fatal("creation fabricated application or lost baseline")
	}
	expectStatus(t, call(projectBearer), 200)
	if err := svc.FlushCallRecorder(ctx); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, call(projectBearer), 429)
	detailPath := requestPath + "/" + request.ID
	detail := decodeCatalogResponse[service.ProjectRequestRecord](t, asAdmin("GET", detailPath, nil, ""), 200)
	decision := map[string]any{"action": "approve", "reason": "Controlled monthly approval"}
	expectStatus(t, asManager("POST", detailPath+"/decision", decision, detail.ApprovalReviewETag), 403)
	expectStatus(t, asAdmin("POST", detailPath+"/decision", map[string]any{"action": "approve"}, detail.ApprovalReviewETag), 400)
	approved := decodeCatalogResponse[service.ProjectRequestRecord](t, asAdmin("POST", detailPath+"/decision", decision, detail.ApprovalReviewETag), 200)
	if approved.ApplicationStatus != "applied" || approved.RuntimeApplied == nil || !*approved.RuntimeApplied || *approved.ApprovedQuota.TokensMonth != 10 {
		t.Fatalf("approval did not confirm exact published revision: %+v", approved)
	}
	current := decodeCatalogResponse[service.LimitRecord](t, asAdmin("GET", limitPath, nil, ""), 200)
	if current.Stored.Tokens5H == nil || *current.Stored.Tokens5H != 100 || current.Stored.RPM == nil || *current.Stored.RPM != 100 || current.Stored.Concurrency == nil || *current.Stored.Concurrency != 2 {
		t.Fatal("monthly patch replaced other controls")
	}
	expectStatus(t, call(projectBearer), 200)
	if err := svc.FlushCallRecorder(ctx); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, call(projectBearer), 429)
	later := current.Stored
	later.TokensMonth = number(15)
	laterRecord := write(later)
	replayed := decodeCatalogResponse[service.ProjectRequestRecord](t, asAdmin("POST", detailPath+"/decision", decision, detail.ApprovalReviewETag), 200)
	if replayed.ApplicationStatus != "superseded" || replayed.RuntimeApplied == nil || *replayed.RuntimeApplied || replayed.ApprovedPolicyETag != approved.ApprovedPolicyETag || *replayed.CurrentQuota.TokensMonth != 15 {
		t.Fatal("historical replay restored an old policy or invented current application")
	}
	retry := decodeCatalogResponse[service.ProjectRequestRecord](t, asManager("POST", requestPath, body, contextRecord.ReviewETag), 201)
	if retry.ID != request.ID || retry.BaselinePolicyETag != initialRecord.ETag {
		t.Fatal("creation replay rebased its immutable baseline")
	}
	var limitRow entity.ResourceLimit
	if err := db.First(&limitRow, "scope_kind = ? AND scope_id = ?", "project", projectID).Error; err != nil || limitRow.ETag != laterRecord.ETag {
		t.Fatal("decision replay changed current policy revision", err)
	}
	// A fresh review preserves later controls, while a stale review cannot apply.
	ctx2 := decodeCatalogResponse[service.ProjectQuotaRequestContext](t, asManager("GET", base+"/request-quota-context", nil, ""), 200)
	body2 := map[string]any{"request_id": "req_quota_second", "kind": "QUOTA", "quota": map[string]any{"tokens_month": 20}, "reason": "Second capacity"}
	second := decodeCatalogResponse[service.ProjectRequestRecord](t, asManager("POST", requestPath, body2, ctx2.ReviewETag), 201)
	secondPath := requestPath + "/" + second.ID
	secondDetail := decodeCatalogResponse[service.ProjectRequestRecord](t, asAdmin("GET", secondPath, nil, ""), 200)
	later.RPM = number(90)
	write(later)
	expectStatus(t, asAdmin("POST", secondPath+"/decision", decision, secondDetail.ApprovalReviewETag), 409)
	secondDetail = decodeCatalogResponse[service.ProjectRequestRecord](t, asAdmin("GET", secondPath, nil, ""), 200)
	decodeCatalogResponse[service.ProjectRequestRecord](t, asAdmin("POST", secondPath+"/decision", decision, secondDetail.ApprovalReviewETag), 200)
	current = decodeCatalogResponse[service.LimitRecord](t, asAdmin("GET", limitPath, nil, ""), 200)
	if *current.Stored.RPM != 90 || *current.Stored.TokensMonth != 20 {
		t.Fatal("fresh review did not preserve the latest independent controls")
	}
	// Exact per-kind rights filter before the cursor limit and expose only a
	// directed request workspace for a quota-only reviewer.
	create(
		&entity.Role{ID: "rol_quota_only", Name: "Quota only", NameKey: "quota-only"},
		&entity.RolePermission{RoleID: "rol_quota_only", Permission: "projects.limits.write"},
		&entity.UserRole{UserID: other.User.ID, RoleID: "rol_quota_only"},
		&entity.Model{ID: "mdl_quota_addition", Status: entity.ResourceActive},
	)
	modelInput := service.ProjectRequestInput{RequestID: "req_model_legacy", ModelIDs: []string{"mdl_quota_addition"}, Reason: "Legacy model request"}
	modelRequest, err := svc.CreateProjectRequest(ctx, manager.User.ID, projectID, modelInput)
	if err != nil {
		t.Fatal(err)
	}
	legacyRaw, _ := json.Marshal(struct {
		ActorID, ProjectID string
		Input              struct {
			RequestID string
			ModelIDs  []string
			Reason    string
		}
	}{manager.User.ID, projectID, struct {
		RequestID string
		ModelIDs  []string
		Reason    string
	}{modelInput.RequestID, modelInput.ModelIDs, modelInput.Reason}})
	var historical entity.ProjectModelRequest
	if err := db.First(&historical, "id = ?", modelRequest.ID).Error; err != nil || historical.RequestHash != secret.SHA256Hex(string(legacyRaw)) {
		t.Fatal("MODEL_ACCESS creation hash changed from the released encoding", err)
	}
	modelRetry, err := svc.CreateProjectRequest(ctx, manager.User.ID, projectID, modelInput)
	if err != nil || modelRetry.ID != modelRequest.ID {
		t.Fatal("released model creation retry failed", err)
	}
	page, err := svc.ListProjectRequests(ctx, other.User.ID, projectID, service.ProjectRequestFilter{Limit: 1})
	if err != nil || len(page.Items) != 1 || page.Items[0].Kind != entity.ProjectRequestQuota || page.NextCursor == "" {
		t.Fatal("quota-only list paginated unauthorized MODEL_ACCESS rows", err)
	}
	if _, err := svc.GetProjectRequest(ctx, other.User.ID, projectID, modelRequest.ID); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatal("quota-only reviewer read model intent", err)
	}
	otherSession, otherCookie := readIdentity(t, identityRequest(router, "POST", "/api/v1/auth/login", `{"email":"quota-request-other@example.invalid","password":"quota-request-password"}`, nil, ""))
	workspace := send(otherCookie, otherSession.CSRFToken, "GET", base, nil, "")
	expectStatus(t, workspace, 200)
	var minimal map[string]json.RawMessage
	if json.Unmarshal(workspace.Body.Bytes(), &minimal) != nil || len(minimal) != 5 || string(minimal["request_workspace_only"]) != "true" {
		t.Fatalf("quota-only workspace exposed unrelated relationships: %s", workspace.Body.String())
	}
	if _, err := svc.GetProjectQuotaRequestContext(ctx, other.User.ID, projectID); !errors.Is(err, apperrors.ErrForbidden) {
		t.Fatal("review permission granted submission management", err)
	}
	// Opposite review authority exposes MODEL_ACCESS only, even when a QUOTA
	// row is newer than every eligible model row.
	if err := db.Where("user_id = ?", other.User.ID).Delete(&entity.UserRole{}).Error; err != nil {
		t.Fatal(err)
	}
	create(&entity.Role{ID: "rol_model_only", Name: "Model only", NameKey: "model-only"},
		&entity.RolePermission{RoleID: "rol_model_only", Permission: "projects.models.write"},
		&entity.UserRole{UserID: other.User.ID, RoleID: "rol_model_only"})
	page, err = svc.ListProjectRequests(ctx, other.User.ID, projectID, service.ProjectRequestFilter{Limit: 1})
	if err != nil || len(page.Items) != 1 || page.Items[0].ID != modelRequest.ID || page.NextCursor != "" {
		t.Fatal("model-only history included quota facts", err)
	}
	if _, err := svc.GetProjectRequest(ctx, other.User.ID, projectID, request.ID); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatal("model-only authority exposed quota intent", err)
	}
	if err := db.Where("user_id = ?", other.User.ID).Delete(&entity.UserRole{}).Error; err != nil {
		t.Fatal(err)
	}
	// A database accepting a collated foreign-key alias must still deny every
	// request source. Databases rejecting that alias are equally safe.
	for _, alias := range []entity.UserRole{{UserID: strings.ToUpper(other.User.ID), RoleID: "rol_quota_only"}, {UserID: other.User.ID, RoleID: "ROL_QUOTA_ONLY"}} {
		err := db.Create(&alias).Error
		if err != nil {
			if !errors.Is(err, gorm.ErrForeignKeyViolated) {
				t.Fatal("unexpected alias constraint error", err)
			}
			continue
		}
		if _, err := svc.ListProjectRequests(ctx, other.User.ID, projectID, service.ProjectRequestFilter{Limit: 1}); !errors.Is(err, apperrors.ErrNotFound) {
			t.Fatal("collated role alias granted quota history", err)
		}
		if _, err := svc.DecideProjectRequest(ctx, other.User.ID, projectID, request.ID, service.ProjectRequestDecision{Action: "reject", Reason: "Alias"}); !errors.Is(err, apperrors.ErrForbidden) {
			t.Fatal("collated role alias granted quota review", err)
		}
		if err := db.Where("user_id = ?", alias.UserID).Delete(&entity.UserRole{}).Error; err != nil {
			t.Fatal(err)
		}
	}
	managerAlias := entity.ProjectManager{ID: "pjm_quota_alias", ProjectID: projectID, UserID: strings.ToUpper(other.User.ID)}
	if err := db.Create(&managerAlias).Error; err == nil {
		if _, err := svc.GetProjectQuotaRequestContext(ctx, other.User.ID, projectID); !errors.Is(err, apperrors.ErrForbidden) {
			t.Fatal("collated manager alias granted submission context", err)
		}
		if err := db.Delete(&managerAlias).Error; err != nil {
			t.Fatal(err)
		}
	} else if !errors.Is(err, gorm.ErrForeignKeyViolated) {
		t.Fatal(err)
	}
	createQuota := func(clientID string, patch *service.ProjectQuotaPatch) *service.ProjectRequestRecord {
		t.Helper()
		context, err := svc.GetProjectQuotaRequestContext(ctx, manager.User.ID, projectID)
		if err != nil {
			t.Fatal(err)
		}
		record, err := svc.CreateProjectRequest(ctx, manager.User.ID, projectID, service.ProjectRequestInput{Kind: entity.ProjectRequestQuota, RequestID: clientID, Quota: patch, Reason: "Controlled lifecycle request", ReviewETag: context.ReviewETag})
		if err != nil {
			t.Fatal(err)
		}
		return record
	}
	zero := createQuota("req_quota_zero", &service.ProjectQuotaPatch{TokensMonth: number(0)})
	if zero.RequestedQuota.TokensMonth == nil || *zero.RequestedQuota.TokensMonth != 0 {
		t.Fatal("explicit finite zero disappeared")
	}
	expectStatus(t, asAdmin("POST", requestPath+"/"+zero.ID+"/decision", map[string]any{"action": "reject", "reason": "Reject zero"}, strings.Repeat("a", 65)), 400)
	if _, err := svc.DecideProjectRequest(ctx, admin.User.ID, projectID, zero.ID, service.ProjectRequestDecision{Action: "reject", Reason: "Reject zero"}); err != nil {
		t.Fatal(err)
	}
	lifecycle := createQuota("req_quota_departure", &service.ProjectQuotaPatch{TokensMonth: number(25)})
	lifecycleDetail, err := svc.GetProjectRequest(ctx, admin.User.ID, projectID, lifecycle.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Where("project_id = ? AND user_id = ?", projectID, manager.User.ID).Delete(&entity.ProjectManager{}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.DecideProjectRequest(ctx, admin.User.ID, projectID, lifecycle.ID, service.ProjectRequestDecision{Action: "approve", Reason: "Review after departure", ReviewETag: lifecycleDetail.ApprovalReviewETag}); err == nil {
		t.Fatal("former manager request was approved")
	}
	if _, err := svc.DecideProjectRequest(ctx, manager.User.ID, projectID, lifecycle.ID, service.ProjectRequestDecision{Action: "withdraw"}); err != nil {
		t.Fatal("former applicant could not withdraw its pending intent", err)
	}
	create(&entity.ProjectManager{ID: "pjm_quota_rejoin", ProjectID: projectID, UserID: manager.User.ID})
	racing := createQuota("req_quota_racing", &service.ProjectQuotaPatch{TokensMonth: number(25)})
	racingDetail, err := svc.GetProjectRequest(ctx, admin.User.ID, projectID, racing.ID)
	if err != nil {
		t.Fatal(err)
	}
	freshDB, err := database.Open(ctx, db.Name(), os.Getenv("ROUTEX_TEST_"+strings.ToUpper(db.Name())+"_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	freshPool, _ := freshDB.DB()
	defer func() { _ = freshPool.Close() }()
	freshSvc, err := service.New(ctx, freshDB)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	outcomes := make(chan error, 2)
	wg.Go(func() {
		_, err := svc.DecideProjectRequest(ctx, admin.User.ID, projectID, racing.ID, service.ProjectRequestDecision{Action: "approve", Reason: "Concurrent approval", ReviewETag: racingDetail.ApprovalReviewETag})
		outcomes <- err
	})
	wg.Go(func() {
		_, err := freshSvc.DecideProjectRequest(ctx, manager.User.ID, projectID, racing.ID, service.ProjectRequestDecision{Action: "withdraw"})
		outcomes <- err
	})
	wg.Wait()
	close(outcomes)
	success := 0
	for err := range outcomes {
		if err == nil {
			success++
		}
	}
	var decisions int64
	if err := db.Model(&entity.AuditEvent{}).Where("resource_id = ? AND action IN ?", racing.ID, []string{"project.request.approve", "project.request.withdraw"}).Count(&decisions).Error; err != nil || decisions != 1 || success != 1 {
		t.Fatal("concurrent independent-pool decisions did not produce one terminal receipt", success, decisions, err)
	}
	// Money submission is never converted through a float. A separate fresh
	// Project establishes accounting coverage for its priced native calls.
	pricePage, err := svc.ListPrices(ctx, admin.User.ID, service.PriceFilter{})
	if err != nil {
		t.Fatal(err)
	}
	rates := []pricing.Rate{}
	for _, metric := range []string{pricing.Input, pricing.Output, pricing.CacheRead, pricing.CacheWrite} {
		rates = append(rates, pricing.Rate{Metric: metric, Tier: pricing.Base, Unit: pricing.Unit, Currency: "USD", Amount: "1000000", Enabled: true})
	}
	if _, err := svc.WritePrices(ctx, admin.User.ID, pricePage.ETag, []service.PriceInput{{ProviderModelID: "pmd_quota_request", Rates: rates}}); err != nil {
		t.Fatal(err)
	}
	moneyProject, moneyBearer := "prj_quota_money", "rxp_"+strings.Repeat("m", 43)
	create(&entity.Project{ID: moneyProject, Name: "Exact money", CreatorID: manager.User.ID, Status: entity.ResourceActive},
		&entity.ProjectManager{ID: "pjm_quota_money", ProjectID: moneyProject, UserID: manager.User.ID},
		&entity.ProjectModelGrant{ProjectID: moneyProject, ModelID: modelID},
		&entity.ProjectKey{ID: "pky_quota_money", ProjectID: moneyProject, CreatorID: manager.User.ID, Name: "Money", Prefix: "rxp_masked", TokenHash: secret.SHA256Hex(moneyBearer), Status: entity.KeyActive, DeliveryMode: "manual"},
		&entity.ProjectKeyModel{KeyID: "pky_quota_money", ModelID: modelID})
	// The conservative reservation adds four half-unit rounding margins;
	// provision those exact decimal units without changing actual five-unit use.
	money := "5.000000000000000002"
	if _, err := svc.SetResourceLimit(ctx, admin.User.ID, service.LimitTarget{Kind: "project", ID: moneyProject}, "0", service.LimitInput{Policy: limits.Policy{MoneyMonth: &money, Currency: "USD"}, Reason: "Initial finite money"}); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, call(moneyBearer), 200)
	if err := svc.FlushCallRecorder(ctx); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, call(moneyBearer), 429)
	moneyLimitPath := "/api/v1/projects/" + moneyProject + "/limits"
	assertMoney := func(expected, policyAmount string) {
		t.Helper()
		current := decodeCatalogResponse[service.LimitRecord](t, asAdmin("GET", moneyLimitPath, nil, ""), 200)
		if current.QuotaUsage == nil || current.QuotaUsage.Month == nil || current.QuotaUsage.Month.MoneyUnknown != 0 || current.QuotaUsage.Month.MoneyUsed["USD"] != expected {
			t.Fatalf("reservation margin changed settled native amount: %+v", current.QuotaUsage)
		}
		if current.Stored.MoneyMonth == nil || *current.Stored.MoneyMonth != policyAmount {
			t.Fatal("stored money policy lost its exact decimal amount")
		}
	}
	assertMoney("5", money)
	moneyContext, err := svc.GetProjectQuotaRequestContext(ctx, manager.User.ID, moneyProject)
	if err != nil {
		t.Fatal(err)
	}
	exactMoney := "10.000000000000000004"
	moneyRequest, err := svc.CreateProjectRequest(ctx, manager.User.ID, moneyProject, service.ProjectRequestInput{Kind: entity.ProjectRequestQuota, RequestID: "req_quota_money", Quota: &service.ProjectQuotaPatch{MoneyMonth: &exactMoney, Currency: "USD"}, Reason: "Exact monthly budget", ReviewETag: moneyContext.ReviewETag})
	if err != nil {
		t.Fatal(err)
	}
	if moneyRequest.BaselineQuota == nil || moneyRequest.BaselineQuota.MoneyMonth == nil || *moneyRequest.BaselineQuota.MoneyMonth != money {
		t.Fatal("money request history lost its exact baseline")
	}
	moneyDetail, err := svc.GetProjectRequest(ctx, admin.User.ID, moneyProject, moneyRequest.ID)
	if err != nil {
		t.Fatal(err)
	}
	currencyPage, err := svc.GetPricingCurrency(ctx, admin.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.WritePricingCurrency(ctx, admin.User.ID, currencyPage.ETag, pricing.FX{PlatformCurrency: "USD", Rates: map[string]string{"USD": "1", "EUR": "2"}}); err != nil {
		t.Fatal(err)
	}
	moneyDecision := service.ProjectRequestDecision{Action: "approve", Reason: "Exact money approval", ReviewETag: moneyDetail.ApprovalReviewETag}
	if _, err := svc.DecideProjectRequest(ctx, admin.User.ID, moneyProject, moneyRequest.ID, moneyDecision); err == nil {
		t.Fatal("currency generation changed without invalidating review")
	}
	moneyDetail, err = svc.GetProjectRequest(ctx, admin.User.ID, moneyProject, moneyRequest.ID)
	if err != nil {
		t.Fatal(err)
	}
	moneyDecision.ReviewETag = moneyDetail.ApprovalReviewETag
	moneyApproved, err := svc.DecideProjectRequest(ctx, admin.User.ID, moneyProject, moneyRequest.ID, moneyDecision)
	if err != nil || moneyApproved.ApprovedQuota == nil || *moneyApproved.ApprovedQuota.MoneyMonth != exactMoney || moneyApproved.ApplicationStatus != "applied" {
		t.Fatal("exact money approval lost value or publication", err)
	}
	expectStatus(t, call(moneyBearer), 200)
	if err := svc.FlushCallRecorder(ctx); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, call(moneyBearer), 429)
	assertMoney("10", exactMoney)
	guardContext, err := svc.GetProjectQuotaRequestContext(ctx, manager.User.ID, moneyProject)
	if err != nil {
		t.Fatal(err)
	}
	guardRequest, err := svc.CreateProjectRequest(ctx, manager.User.ID, moneyProject, service.ProjectRequestInput{
		Kind:       entity.ProjectRequestQuota,
		RequestID:  "req_quota_currency_guard",
		Quota:      &service.ProjectQuotaPatch{TokensMonth: number(100)},
		Reason:     "Keep current money while changing tokens",
		ReviewETag: guardContext.ReviewETag,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Raw contradictory denomination is deliberately injected to exercise
	// fail-closed application reporting independently of safe currency writers.
	if err := db.Model(&entity.PricingSetting{}).Where("id = ?", 1).Update("platform_currency", "EUR").Error; err != nil {
		t.Fatal(err)
	}
	moneyDetail, err = svc.GetProjectRequest(ctx, admin.User.ID, moneyProject, moneyRequest.ID)
	if err != nil || moneyDetail.RuntimeApplied == nil || *moneyDetail.RuntimeApplied || moneyDetail.ApplicationStatus != "pending" {
		t.Fatal("stale published denomination was reported applied", err)
	}
	guardDetail, err := svc.GetProjectRequest(ctx, admin.User.ID, moneyProject, guardRequest.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, guardError := svc.DecideProjectRequest(ctx, admin.User.ID, moneyProject, guardRequest.ID, service.ProjectRequestDecision{Action: "approve", Reason: "Fresh review of contradictory finite policy", ReviewETag: guardDetail.ApprovalReviewETag})
	var guardStatus interface{ StatusCode() int }
	if !errors.As(guardError, &guardStatus) || guardStatus.StatusCode() != 409 {
		t.Fatal("fresh token-only review saved a contradictory finite money policy", guardError)
	}
	var guardedRequest entity.ProjectModelRequest
	if err := db.First(&guardedRequest, "id = ?", guardRequest.ID).Error; err != nil || guardedRequest.Status != entity.ProjectRequestPending || guardedRequest.ApprovedPolicyETag != "" {
		t.Fatal("contradictory approval changed its pending request", err)
	}
	var guardedPolicy entity.ResourceLimit
	if err := db.First(&guardedPolicy, "scope_kind = ? AND scope_id = ?", "project", moneyProject).Error; err != nil || guardedPolicy.ETag != moneyApproved.ApprovedPolicyETag || guardedPolicy.TokensMonth != nil {
		t.Fatal("contradictory approval changed the stored policy", err)
	}
	if err := db.Model(&entity.PricingSetting{}).Where("id = ?", 1).Update("platform_currency", "USD").Error; err != nil {
		t.Fatal(err)
	}
	// A committed approval with failed publication retains its immutable
	// decision receipt. The exact retry republishes rather than writing twice.
	uncertain := createQuota("req_quota_uncertain", &service.ProjectQuotaPatch{TokensMonth: number(30)})
	uncertainDetail, err := svc.GetProjectRequest(ctx, admin.User.ID, projectID, uncertain.ID)
	if err != nil {
		t.Fatal(err)
	}
	const failureCallback = "quota-request-runtime-publication-failure"
	if err := db.Callback().Query().Before("gorm:query").Register(failureCallback, func(tx *gorm.DB) {
		if tx.Statement.Table == "providers" {
			_ = tx.AddError(errors.New("controlled runtime publication unavailable"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	uncertainDecision := service.ProjectRequestDecision{Action: "approve", Reason: "Durable approval under publication outage", ReviewETag: uncertainDetail.ApprovalReviewETag}
	_, uncertainError := svc.DecideProjectRequest(ctx, admin.User.ID, projectID, uncertain.ID, uncertainDecision)
	if err := db.Callback().Query().Remove(failureCallback); err != nil {
		t.Fatal(err)
	}
	var statusError interface{ StatusCode() int }
	if !errors.As(uncertainError, &statusError) || statusError.StatusCode() != 503 {
		t.Fatal("publication outage did not preserve an uncertain durable response", uncertainError)
	}
	var savedUncertain entity.ProjectModelRequest
	if err := db.First(&savedUncertain, "id = ?", uncertain.ID).Error; err != nil || savedUncertain.Status != entity.ProjectRequestApproved || savedUncertain.ApprovedPolicyETag == "" {
		t.Fatal("publication failure lost the committed approval", err)
	}
	pending, err := svc.GetProjectRequest(ctx, admin.User.ID, projectID, uncertain.ID)
	if err != nil || pending.ApplicationStatus != "pending" || pending.RuntimeApplied == nil || *pending.RuntimeApplied {
		t.Fatal("failed publication was reported applied", err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	previousOpen := pool.Stats().MaxOpenConnections
	pool.SetMaxOpenConns(1)
	reconciled, err := svc.DecideProjectRequest(ctx, admin.User.ID, projectID, uncertain.ID, uncertainDecision)
	pool.SetMaxOpenConns(previousOpen)
	if err != nil || reconciled.ApplicationStatus != "applied" || reconciled.ApprovedPolicyETag != savedUncertain.ApprovedPolicyETag {
		t.Fatal("single-connection retry did not reconcile the exact approved revision", err)
	}
	if err := db.Model(&entity.AuditEvent{}).Where("resource_id = ? AND action = ?", uncertain.ID, "project.request.approve").Count(&decisions).Error; err != nil || decisions != 1 {
		t.Fatal("publication retry wrote a second decision", decisions, err)
	}
	inactive := createQuota("req_quota_inactive", &service.ProjectQuotaPatch{TokensMonth: number(35)})
	inactiveDetail, err := svc.GetProjectRequest(ctx, admin.User.ID, projectID, inactive.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&entity.Project{}).Where("id = ?", projectID).Update("status", entity.ResourceDisabled).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.DecideProjectRequest(ctx, admin.User.ID, projectID, inactive.ID, service.ProjectRequestDecision{Action: "approve", Reason: "Inactive review", ReviewETag: inactiveDetail.ApprovalReviewETag}); err == nil {
		t.Fatal("inactive Project approval succeeded")
	}
	if err := db.Model(&entity.Project{}).Where("id = ?", projectID).Update("status", entity.ResourceActive).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&entity.User{}).Where("id = ?", manager.User.ID).Update("disabled", true).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.DecideProjectRequest(ctx, admin.User.ID, projectID, inactive.ID, service.ProjectRequestDecision{Action: "approve", Reason: "Disabled applicant review", ReviewETag: inactiveDetail.ApprovalReviewETag}); err == nil {
		t.Fatal("disabled applicant approval succeeded")
	}
	if err := db.Model(&entity.User{}).Where("id = ?", manager.User.ID).Update("disabled", false).Error; err != nil {
		t.Fatal(err)
	}
	// Publication and receipt survive a new service and journal process without
	// repeating the original patch after a later direct policy edit.
	later.TokensMonth = number(15)
	write(later)
	if err := svc.StopCallRecorder(); err != nil {
		t.Fatal(err)
	}
	svc = makeService()
	router = fox.New()
	New(svc).RegisterRoutes(router)
	start()
	replayed = decodeCatalogResponse[service.ProjectRequestRecord](t, asAdmin("POST", detailPath+"/decision", decision, detail.ApprovalReviewETag), 200)
	if replayed.ApplicationStatus != "superseded" || *replayed.CurrentQuota.TokensMonth != 15 || !reflect.DeepEqual(replayed.ApprovedQuota, approved.ApprovedQuota) {
		t.Fatal("restart replay lost historical approval or restored a patch")
	}
	// An ordinary foreign actor gets identical denial for existing and missing
	// targets; source existence must not be probed before review authority.
	if err := db.Where("user_id = ?", other.User.ID).Delete(&entity.UserRole{}).Error; err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{request.ID, "pmr_absent"} {
		if _, err := svc.DecideProjectRequest(ctx, other.User.ID, projectID, target, service.ProjectRequestDecision{Action: "approve", Reason: "Unauthorized", ReviewETag: detail.ApprovalReviewETag}); !errors.Is(err, apperrors.ErrForbidden) {
			t.Fatal("foreign source existence altered denial", err)
		}
	}
}
