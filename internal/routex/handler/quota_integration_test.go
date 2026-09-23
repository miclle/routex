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
	"sync/atomic"
	"testing"

	"github.com/fox-gonic/fox"
	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/pricing"
	"github.com/miclle/routex/pkg/secret"
	"github.com/miclle/routex/pkg/secretstore"
)

// testQuotaLifecycle belongs to the single fresh-database integration harness.
func testQuotaLifecycle(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx := context.Background()
	store, err := secretstore.New(bytes.Repeat([]byte{79}, 32))
	if err != nil {
		t.Fatal(err)
	}
	var dispatched atomic.Int32
	var missingCache atomic.Bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		dispatched.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if missingCache.Load() {
			_, _ = io.WriteString(w, `{"object":"chat.completion","model":"quota-native","choices":[{"finish_reason":"stop"}],"usage":{"prompt_tokens":4,"completion_tokens":1}}`)
			return
		}
		_, _ = io.WriteString(w, `{"object":"chat.completion","model":"quota-native","choices":[{"finish_reason":"stop"}],"usage":{"prompt_tokens":4,"completion_tokens":1,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0}}}`)
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
	router := fox.New()
	New(svc).RegisterRoutes(router)
	setup := identityRequest(router, "POST", "/api/v1/setup", `{"email":"quota-admin@example.invalid","password":"quota-integration-password","name":"Quota admin"}`, nil, "")
	expectStatus(t, setup, 201)
	admin, cookie := readIdentity(t, setup)
	request := func(method, path string, body any, etag string) *httptest.ResponseRecorder {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(method, "http://routex.test"+path, bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-CSRF-Token", admin.CSRFToken)
		req.AddCookie(cookie)
		if etag != "" {
			req.Header.Set("If-Match", `"`+etag+`"`)
		}
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		return res
	}
	cipher, err := store.Seal("crd_quota", "quota-provider-secret")
	if err != nil {
		t.Fatal(err)
	}
	modelID := "mdl_quota"
	adminBearer := "rx_" + strings.Repeat("q", 43)
	for _, row := range []any{
		&entity.Provider{ID: "prv_quota", Name: "Quota provider"},
		&entity.ProviderConnection{ID: "con_quota", ProviderID: "prv_quota", Name: "Native", Protocol: entity.ProtocolOpenAIChat, BaseURL: upstream.URL + "/v1"},
		&entity.ProviderCredential{ID: "crd_quota", ConnectionID: "con_quota", Name: "Verified", Ciphertext: cipher, Enabled: true, VerificationStatus: "verified"},
		&entity.ProviderModel{ID: "pmd_quota", ConnectionID: "con_quota", UpstreamName: "quota-native"},
		&entity.CredentialModelAccess{CredentialID: "crd_quota", ProviderModelID: "pmd_quota"},
		&entity.Model{ID: modelID, Status: "active"}, &entity.ModelName{Name: "quota-model", ModelID: modelID, CurrentModelID: &modelID},
		&entity.ModelProviderBinding{ID: "bnd_quota", ModelID: modelID, ProviderModelID: "pmd_quota", Weight: 100},
		&entity.UserModelGrant{UserID: admin.User.ID, ModelID: modelID},
		&entity.APIKey{ID: "key_quota_admin", UserID: admin.User.ID, Name: "Admin", Prefix: "rx_masked", TokenHash: secret.SHA256Hex(adminBearer), Status: entity.KeyActive},
		&entity.APIKeyModel{KeyID: "key_quota_admin", ModelID: modelID},
		&entity.ModelPrice{ID: "price_quota", ProviderModelID: "pmd_quota", UpdateSource: "api"},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, metric := range []string{pricing.Input, pricing.Output, pricing.CacheRead, pricing.CacheWrite} {
		if err := db.Create(&entity.PriceRate{ID: "rate_quota_" + metric, ModelPriceID: "price_quota", Metric: metric, Tier: pricing.Base, Unit: pricing.Unit, Currency: "USD", Amount: "1", Enabled: true}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := svc.StartRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	spool := filepath.Join(t.TempDir(), "quota.db")
	if err := svc.StartCallRecorder(ctx, spool); err != nil {
		t.Fatal(err)
	}
	defer func() { svc.StopRuntime(); _ = svc.StopCallRecorder() }()
	settingPath := "/api/v1/admin/quota-settings"
	boundPath := "/api/v1/admin/provider-models/pmd_quota/reservation-bound"
	setting := decodeCatalogResponse[service.QuotaSettingsRecord](t, request("GET", settingPath, nil, ""), 200)
	if !setting.Editable || setting.Activated || setting.CoverageStart != nil {
		t.Fatal("migration fabricated quota history")
	}
	setting = decodeCatalogResponse[service.QuotaSettingsRecord](t, request("PUT", settingPath, map[string]any{"time_zone": "America/New_York", "reason": "Organization calendar"}, setting.ETag), 200)
	boundBody := map[string]any{"max_input_tokens": 10, "max_output_tokens": 10, "evidence": "Controlled upstream enforces this finite test capacity", "reason": "Acceptance fixture"}
	expectStatus(t, identityRequest(router, "PUT", boundPath, `{"max_input_tokens":10,"max_output_tokens":10,"evidence":"test","reason":"missing CSRF"}`, cookie, ""), 403)
	expectStatus(t, request("PUT", boundPath, boundBody, ""), 400)
	bound := decodeCatalogResponse[service.ReservationBoundRecord](t, request("PUT", boundPath, boundBody, "0"), 200)
	if !bound.Configured || bound.Protocol != entity.ProtocolOpenAIChat {
		t.Fatal("native capacity not derived")
	}
	retry := decodeCatalogResponse[service.ReservationBoundRecord](t, request("PUT", boundPath, boundBody, "0"), 200)
	if retry.ETag != bound.ETag {
		t.Fatal("capacity retry duplicated change")
	}
	call := func(bearer string, capped bool) *httptest.ResponseRecorder {
		body := map[string]any{"model": "quota-model", "messages": []map[string]string{{"role": "user", "content": "hello"}}}
		if capped {
			body["max_completion_tokens"] = 10
		}
		raw, _ := json.Marshal(body)
		req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+bearer)
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		return res
	}
	expectStatus(t, call(adminBearer, true), 200)
	afterActivation := decodeCatalogResponse[service.QuotaSettingsRecord](t, request("GET", settingPath, nil, ""), 200)
	if !afterActivation.Activated || afterActivation.Editable || afterActivation.TimeZone != "America/New_York" || afterActivation.CoverageStart == nil {
		t.Fatal("first admission did not freeze configured calendar")
	}
	expectStatus(t, request("PUT", settingPath, map[string]any{"time_zone": "UTC", "reason": "Unsafe rewrite"}, setting.ETag), 409)
	member, err := svc.CreateMember(ctx, admin.User.ID, "quota-member@example.invalid", "quota-integration-password", "Quota member", "member")
	if err != nil {
		t.Fatal(err)
	}
	memberBearer := "rx_" + strings.Repeat("m", 43)
	projectBearer := "rxp_" + strings.Repeat("j", 43)
	for _, row := range []any{
		&entity.UserModelGrant{UserID: member.User.ID, ModelID: modelID},
		&entity.APIKey{ID: "key_quota_member", UserID: member.User.ID, Name: "Member", Prefix: "rx_masked", TokenHash: secret.SHA256Hex(memberBearer), Status: entity.KeyActive},
		&entity.APIKeyModel{KeyID: "key_quota_member", ModelID: modelID},
		&entity.Project{ID: "prj_quota", Name: "Quota Project", CreatorID: admin.User.ID, Status: entity.ResourceActive},
		&entity.ProjectManager{ID: "pmg_quota", ProjectID: "prj_quota", UserID: admin.User.ID},
		&entity.ProjectModelGrant{ProjectID: "prj_quota", ModelID: modelID},
		&entity.ProjectKey{ID: "pky_quota", ProjectID: "prj_quota", CreatorID: admin.User.ID, Name: "Project", Prefix: "rxp_masked", TokenHash: secret.SHA256Hex(projectBearer), Status: entity.KeyActive, DeliveryMode: "manual"},
		&entity.ProjectKeyModel{KeyID: "pky_quota", ModelID: modelID},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := svc.RefreshRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	memberAuth, memberCookie := readIdentity(t, identityRequest(router, "POST", "/api/v1/auth/login", `{"email":"quota-member@example.invalid","password":"quota-integration-password"}`, nil, ""))
	expectStatus(t, identityRequest(router, "GET", boundPath, "", memberCookie, ""), 403)
	expectStatus(t, identityRequest(router, "GET", settingPath, "", memberCookie, ""), 403)
	userPath := "/api/v1/admin/members/" + member.User.ID + "/limits"
	adminPath := "/api/v1/admin/members/" + admin.User.ID + "/limits"
	projectPath := "/api/v1/projects/prj_quota/limits"
	read := func(path string) service.LimitRecord {
		return decodeCatalogResponse[service.LimitRecord](t, request("GET", path, nil, ""), 200)
	}
	write := func(path string, body map[string]any) service.LimitRecord {
		body["reason"] = "Quota acceptance"
		return decodeCatalogResponse[service.LimitRecord](t, request("PUT", path, body, read(path).ETag), 200)
	}
	denied := httptest.NewRequest("PUT", "http://routex.test"+userPath, strings.NewReader(`{"tokens_5h":100,"reason":"self escalation"}`))
	denied.Header.Set("Content-Type", "application/json")
	denied.Header.Set("If-Match", `"0"`)
	denied.Header.Set("X-CSRF-Token", memberAuth.CSRFToken)
	denied.AddCookie(memberCookie)
	deniedResult := httptest.NewRecorder()
	router.ServeHTTP(deniedResult, denied)
	expectStatus(t, deniedResult, 403)
	parent := write(userPath, map[string]any{"tokens_5h": 24, "tokens_7d": 24, "tokens_month": 24, "tpm": 24, "money_month": "1", "currency": "USD"})
	beforeDispatch := dispatched.Load()
	expectStatus(t, call(memberBearer, false), 400)
	if dispatched.Load() != beforeDispatch {
		t.Fatal("unbounded constrained request dispatched")
	}
	expectStatus(t, call(memberBearer, true), 200)
	expectStatus(t, call(memberBearer, true), 429)
	used := read(userPath)
	if used.QuotaUsage == nil || used.QuotaUsage.FiveHours == nil || !used.QuotaUsage.FiveHours.Covered || used.QuotaUsage.FiveHours.TokensUsed != 5 || used.QuotaUsage.Month.MoneyUsed["USD"] != "0.000005" {
		t.Fatalf("independent durable settlement incorrect: %+v", used.QuotaUsage)
	}
	expectStatus(t, request("PUT", userPath, map[string]any{"tokens_5h": 25, "reason": "stale"}, "0"), 409)
	write(adminPath, map[string]any{"tokens_5h": 100})
	expectStatus(t, call(adminBearer, true), 503)
	if report := read(adminPath); report.QuotaUsage.FiveHours.Covered {
		t.Fatal("pre-activation account claimed full coverage")
	}
	write(projectPath, map[string]any{"tokens_5h": 20, "money_month": "1", "currency": "USD"})
	expectStatus(t, call(projectBearer, true), 200)
	expectStatus(t, call(projectBearer, true), 429)
	if report := read(adminPath); report.QuotaUsage.FiveHours.TokensUsed != 5 {
		t.Fatal("Project assets charged manager's personal account")
	}
	// Refresh retry must not duplicate the policy revision or its audit event.
	retryBody := map[string]any{"tokens_5h": 24, "tokens_7d": 24, "tokens_month": 24, "tpm": 24, "money_month": "1", "currency": "USD", "reason": "Quota acceptance"}
	if retry := decodeCatalogResponse[service.LimitRecord](t, request("PUT", userPath, retryBody, "0"), 200); retry.ETag != parent.ETag {
		t.Fatal("policy retry changed revision")
	}
	var priceSetting entity.PricingSetting
	if err := db.First(&priceSetting, 1).Error; err != nil {
		t.Fatal(err)
	}
	expectStatus(t, request("PUT", "/api/v1/admin/prices/currency", map[string]any{"etag": priceSetting.ETag, "currency": pricing.FX{PlatformCurrency: "EUR", Rates: map[string]string{"USD": "1", "EUR": "1"}}}, ""), 409)
	// Unknown money does not fabricate a zero charge; known tokens still settle.
	write(userPath, map[string]any{})
	missingCache.Store(true)
	expectStatus(t, call(memberBearer, false), 200)
	missingCache.Store(false)
	write(userPath, map[string]any{"money_month": "1", "currency": "USD"})
	expectStatus(t, call(memberBearer, true), 503)
	used = read(userPath)
	if used.QuotaUsage.Month.MoneyUnknown != 1 || used.QuotaUsage.FiveHours.TokensUsed != 10 {
		t.Fatal("unknown pricing erased known usage")
	}
	if err := svc.FlushCallRecorder(ctx); err != nil {
		t.Fatal(err)
	}
	svc.StopRuntime()
	if err := svc.StopCallRecorder(); err != nil {
		t.Fatal(err)
	}
	svc = makeService()
	if err := svc.StartRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	if err := svc.StartCallRecorder(ctx, spool); err != nil {
		t.Fatal(err)
	}
	router = fox.New()
	New(svc).RegisterRoutes(router)
	restored := read(userPath)
	if restored.QuotaUsage.Month.MoneyUnknown != 1 || restored.QuotaUsage.FiveHours.TokensUsed != 10 {
		t.Fatal("SQL delivery/restart reset quota history")
	}
	expectStatus(t, call(memberBearer, true), 503)
}
