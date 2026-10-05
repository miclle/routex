package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/fox-gonic/fox"
	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/secret"
	"github.com/miclle/routex/pkg/secretstore"
)

func testDefaultLimitsLifecycle(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx := context.Background()
	var failCopy, failPublication atomic.Bool
	const createCallback, queryCallback = "default-limits-create-outage", "default-limits-publication-outage"
	if err := db.Callback().Create().Before("gorm:create").Register(createCallback, func(tx *gorm.DB) {
		if failCopy.Load() && tx.Statement.Table == "resource_limits" {
			_ = tx.AddError(errors.New("controlled default-copy failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Query().Before("gorm:query").Register(queryCallback, func(tx *gorm.DB) {
		if failPublication.Load() && tx.Statement.Table == "providers" {
			_ = tx.AddError(errors.New("controlled reset publication failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	store, err := secretstore.New(bytes.Repeat([]byte{133}, 32))
	if err != nil {
		t.Fatal(err)
	}
	var dispatches atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dispatches.Add(1)
		if r.Header.Get("Authorization") != "Bearer default-limits-upstream" || r.Header.Get("Cookie") != "" {
			t.Error("incorrect native authentication")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, chatCompletionFixture("stop", `{"role":"assistant","content":"Default acceptance"}`, `{"prompt_tokens":2,"completion_tokens":1,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0}}`, false, 0))
	}))
	defer upstream.Close()
	svc, err := service.New(ctx, db, service.WithCredentialStorage(store), service.WithUpstreamPolicy(true))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		failCopy.Store(false)
		failPublication.Store(false)
		svc.StopRuntime()
		if err := svc.StopCallRecorder(); err != nil {
			t.Error(err)
		}
		if err := db.Callback().Create().Remove(createCallback); err != nil {
			t.Error(err)
		}
		if err := db.Callback().Query().Remove(queryCallback); err != nil {
			t.Error(err)
		}
	}()
	router := fox.New()
	New(svc).RegisterRoutes(router)
	failCopy.Store(true)
	_, initializeErr := svc.Initialize(ctx, "defaults-failed-setup@example.invalid", "test-only-defaults-password", "Failed initialization")
	failCopy.Store(false)
	if initializeErr == nil {
		t.Fatal("initialization accepted a failed atomic default copy")
	}
	for _, model := range []any{&entity.User{}, &entity.Session{}, &entity.ResourceLimit{}, &entity.AuditEvent{}} {
		var count int64
		if err := db.Model(model).Count(&count).Error; err != nil || count != 0 {
			t.Fatal("failed initialization persisted a partial identity", model, count, err)
		}
	}
	setup := identityRequest(router, "POST", "/api/v1/setup", `{"email":"defaults-admin@example.invalid","password":"test-only-defaults-password","name":"Default administrator"}`, nil, "")
	expectStatus(t, setup, 201)
	admin, adminCookie := readIdentity(t, setup)
	var administratorPolicy entity.ResourceLimit
	if err := db.First(&administratorPolicy, "scope_kind = ? AND scope_id = ?", "user", admin.User.ID).Error; err != nil || administratorPolicy.AppliedDefaultETag == nil || administratorPolicy.TokensMonth != nil {
		t.Fatal("setup omitted unlimited creation template provenance", err)
	}
	outsider, outsiderCookie, outsiderCSRF := createSystemStatusMember(t, svc, router, admin.User.ID, "defaults-outsider", nil)
	_, readerCookie, readerCSRF := createSystemStatusMember(t, svc, router, admin.User.ID, "defaults-reader", []string{"system.read"})
	_, writerCookie, _ := createSystemStatusMember(t, svc, router, admin.User.ID, "defaults-writer", []string{"limits.settings.write"})
	request := func(method, path string, body any, etag string, cookie *http.Cookie, csrf string) *httptest.ResponseRecorder {
		t.Helper()
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(method, "http://routex.test"+path, bytes.NewReader(encoded))
		req.Header.Set("Content-Type", "application/json")
		if etag != "" {
			req.Header.Set("If-Match", `"`+etag+`"`)
		}
		if cookie != nil {
			req.AddCookie(cookie)
		}
		if csrf != "" {
			req.Header.Set("X-CSRF-Token", csrf)
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		return response
	}
	asAdmin := func(method, path string, body any, etag string) *httptest.ResponseRecorder {
		return request(method, path, body, etag, adminCookie, admin.CSRFToken)
	}
	rulePath := func(kind string) string { return "/api/v1/admin/default-limits/" + kind }
	readRule := func(kind string) service.DefaultLimitRecord {
		t.Helper()
		response := asAdmin("GET", rulePath(kind), nil, "")
		result := decodeCatalogResponse[service.DefaultLimitRecord](t, response, 200)
		if result.Kind != kind || len(result.RuleETag) != 64 || len(result.ETag) != 64 || response.Header().Get("ETag") != `"`+result.ETag+`"` || !result.Editable {
			t.Fatal("incoherent default review", response.Body.String())
		}
		return result
	}
	policy := func(month any) map[string]any {
		return map[string]any{"tokens_5h": nil, "tokens_7d": nil, "tokens_month": month, "tpm": nil, "money_month": nil, "currency": "", "rpm": nil, "concurrency": nil}
	}
	writeRule := func(kind string, month any) service.DefaultLimitRecord {
		t.Helper()
		before := readRule(kind)
		return decodeCatalogResponse[service.DefaultLimitRecord](t, asAdmin("PUT", rulePath(kind), map[string]any{"policy": policy(month), "reason": "Controlled creation template"}, before.ETag), 200)
	}
	initial := readRule("user")
	readRule("team")
	expectStatus(t, request("GET", rulePath("user"), nil, "", nil, ""), 401)
	expectStatus(t, request("GET", rulePath("user"), nil, "", outsiderCookie, ""), 403)
	_, actorAliasErr := svc.GetDefaultLimit(ctx, strings.ToUpper(admin.User.ID), "user")
	var actorFailure *apperrors.Error
	if !errors.As(actorAliasErr, &actorFailure) || actorFailure.Code != 401 {
		t.Fatal("aliased administrator identity read default rules", actorAliasErr)
	}
	readOnly := decodeCatalogResponse[service.DefaultLimitRecord](t, request("GET", rulePath("user"), nil, "", readerCookie, ""), 200)
	if readOnly.Editable {
		t.Fatal("read authority permits default editing")
	}
	expectStatus(t, request("PUT", rulePath("user"), map[string]any{"policy": policy(6), "reason": "Forbidden read-only write"}, initial.ETag, readerCookie, readerCSRF), 403)
	expectStatus(t, request("GET", rulePath("user"), nil, "", writerCookie, ""), 200)
	expectStatus(t, request("PUT", rulePath("user"), map[string]any{"policy": policy(6), "reason": "No CSRF"}, initial.ETag, writerCookie, ""), 403)
	expectStatus(t, asAdmin("PUT", rulePath("user"), map[string]any{"policy": policy(6), "reason": "No reviewed validator"}, ""), 400)
	expectStatus(t, asAdmin("PUT", rulePath("user"), map[string]any{"policy": policy(6), "reason": "Wildcard validator"}, "*"), 400)
	expectStatus(t, asAdmin("PUT", rulePath("user"), map[string]any{"policy": policy(6), "reason": "Aliased validator"}, strings.ToUpper(initial.ETag)), 400)
	expectStatus(t, asAdmin("GET", rulePath("USER"), nil, ""), 400)
	for _, malformed := range []any{-1, int64(9007199254740992), "6"} {
		expectStatus(t, asAdmin("PUT", rulePath("user"), map[string]any{"policy": policy(malformed), "reason": "Invalid finite template"}, initial.ETag), 400)
	}
	unsupported := policy(6)
	unsupported["ip_mode"] = "none"
	expectStatus(t, asAdmin("PUT", rulePath("user"), map[string]any{"policy": unsupported, "reason": "No default IP policy"}, initial.ETag), 400)
	cipher, err := store.Seal("crd_defaults", "default-limits-upstream")
	if err != nil {
		t.Fatal(err)
	}
	modelID := "mdl_defaults"
	bearer := "rx_" + strings.Repeat("d", 43)
	warmupBearer := "rx_" + strings.Repeat("w", 43)
	for _, row := range []any{
		&entity.Provider{ID: "prv_defaults", Name: "Default provider"},
		&entity.ProviderConnection{ID: "con_defaults", ProviderID: "prv_defaults", Name: "Chat", Protocol: entity.ProtocolOpenAIChat, BaseURL: upstream.URL + "/v1"},
		&entity.ProviderCredential{ID: "crd_defaults", ConnectionID: "con_defaults", Name: "Verified", Ciphertext: cipher, Enabled: true, VerificationStatus: "verified"},
		&entity.ProviderModel{ID: "pmd_defaults", ConnectionID: "con_defaults", UpstreamName: "native"},
		&entity.CredentialModelAccess{CredentialID: "crd_defaults", ProviderModelID: "pmd_defaults"},
		&entity.ReservationBound{ProviderModelID: "pmd_defaults", Protocol: entity.ProtocolOpenAIChat, MaxInputTokens: 2, MaxOutputTokens: 1, ETag: "bnd_defaults", Evidence: "Controlled native maximum", Reason: "Default acceptance"},
		&entity.Model{ID: modelID, Status: entity.ResourceActive}, &entity.ModelName{Name: "default-model", ModelID: modelID, CurrentModelID: &modelID},
		&entity.ModelProviderBinding{ID: "bnd_default_route", ModelID: modelID, ProviderModelID: "pmd_defaults", Weight: 100},
		&entity.UserModelGrant{UserID: admin.User.ID, ModelID: modelID},
		&entity.APIKey{ID: "key_defaults_warmup", UserID: admin.User.ID, Name: "Journal activation", Prefix: "rx_masked", TokenHash: secret.SHA256Hex(warmupBearer), Status: entity.KeyActive},
		&entity.APIKeyModel{KeyID: "key_defaults_warmup", ModelID: modelID},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := svc.StartCallRecorder(ctx, filepath.Join(t.TempDir(), "default-limits.db")); err != nil {
		t.Fatal(err)
	}
	if err := svc.StartRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	svc.StopRuntime()
	refresh := func() {
		t.Helper()
		if err := svc.RefreshRuntime(ctx); err != nil {
			t.Fatal(err)
		}
	}
	refresh()
	invokeNative := func(token string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "http://routex.test/v1/chat/completions", strings.NewReader(`{"model":"default-model","messages":[{"role":"user","content":"Finite default"}],"max_completion_tokens":1}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		req.RemoteAddr = "127.0.0.1:1234"
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		return response
	}
	// Native observation activates the real journal before finite identities
	// exist. Their creation time, not an invented historical zero, bounds coverage.
	expectStatus(t, invokeNative(warmupBearer), 200)
	if err := svc.FlushCallRecorder(ctx); err != nil {
		t.Fatal(err)
	}
	var oldPolicy entity.ResourceLimit
	if err := db.First(&oldPolicy, "scope_kind = ? AND scope_id = ?", "user", outsider.User.ID).Error; err != nil {
		t.Fatal(err)
	}
	var publicationsBefore int64
	if err := db.Model(&entity.RuntimePublication{}).Count(&publicationsBefore).Error; err != nil {
		t.Fatal(err)
	}
	userRule := writeRule("user", 6)
	var oldAfter entity.ResourceLimit
	var publicationsAfter int64
	if err := db.First(&oldAfter, "scope_kind = ? AND scope_id = ?", "user", outsider.User.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&entity.RuntimePublication{}).Count(&publicationsAfter).Error; err != nil || !reflect.DeepEqual(oldPolicy, oldAfter) || publicationsBefore != publicationsAfter {
		t.Fatal("default write changed an existing policy or published runtime", err)
	}
	expectStatus(t, asAdmin("PUT", rulePath("user"), map[string]any{"policy": policy(7), "reason": "Stale rule"}, initial.ETag), 409)
	user, err := svc.CreateMember(ctx, admin.User.ID, "defaults-new@example.invalid", "test-only-defaults-password", "New default member", entity.RoleMember)
	if err != nil {
		t.Fatal(err)
	}
	var copied entity.ResourceLimit
	if err := db.First(&copied, "scope_kind = ? AND scope_id = ?", "user", user.User.ID).Error; err != nil || copied.TokensMonth == nil || *copied.TokensMonth != 6 || copied.AppliedDefaultETag == nil || *copied.AppliedDefaultETag != userRule.RuleETag || copied.DefaultResetETag != nil {
		t.Fatal("new User did not atomically copy creation template", copied, err)
	}
	if err := svc.SetRegistrationEnabled(ctx, admin.User.ID, true); err != nil {
		t.Fatal(err)
	}
	registered, err := svc.Register(ctx, "defaults-register@example.invalid", "test-only-defaults-password", "Registered default member")
	if err != nil {
		t.Fatal(err)
	}
	var registrationPolicy entity.ResourceLimit
	if err := db.First(&registrationPolicy, "scope_kind = ? AND scope_id = ?", "user", registered.User.ID).Error; err != nil || registrationPolicy.AppliedDefaultETag == nil || *registrationPolicy.AppliedDefaultETag != userRule.RuleETag || registrationPolicy.TokensMonth == nil || *registrationPolicy.TokensMonth != 6 {
		t.Fatal("registration omitted atomic creation template", err)
	}
	teamRule := writeRule("team", 0)
	team, err := svc.CreateResource(ctx, admin.User.ID, service.TeamResource, "Zero default Team", "Zero is a real cap", []string{admin.User.ID})
	if err != nil {
		t.Fatal(err)
	}
	var teamPolicy entity.ResourceLimit
	if err := db.First(&teamPolicy, "scope_kind = ? AND scope_id = ?", "team", team.ID).Error; err != nil || teamPolicy.TokensMonth == nil || *teamPolicy.TokensMonth != 0 || teamPolicy.AppliedDefaultETag == nil || *teamPolicy.AppliedDefaultETag != teamRule.RuleETag {
		t.Fatal("new Team omitted zero creation template", err)
	}
	ownedTeam, err := svc.CreateResource(ctx, admin.User.ID, service.TeamResource, "Owner reset boundary", "Owner responsibility is read-only", []string{outsider.User.ID})
	if err != nil {
		t.Fatal(err)
	}
	ownedResetPath := "/api/v1/teams/" + ownedTeam.ID + "/limits/default-reset"
	ownedReview := decodeCatalogResponse[service.DefaultLimitResetContext](t, request("GET", ownedResetPath, nil, "", outsiderCookie, ""), 200)
	if ownedReview.Editable {
		t.Fatal("Team ownership implicitly granted all quota dimensions")
	}
	expectStatus(t, request("POST", ownedResetPath, map[string]any{"reason": "Owner cannot reset"}, ownedReview.ETag, outsiderCookie, outsiderCSRF), 403)
	// Copy failure must roll back the identity, relationships, Session and audit.
	for _, create := range []func() error{
		func() error {
			_, err := svc.CreateMember(ctx, admin.User.ID, "defaults-failed-create@example.invalid", "test-only-defaults-password", "Failed creation", entity.RoleMember)
			return err
		},
		func() error {
			_, err := svc.Register(ctx, "defaults-failed-register@example.invalid", "test-only-defaults-password", "Failed registration")
			return err
		},
		func() error {
			_, err := svc.CreateResource(ctx, admin.User.ID, service.TeamResource, "Failed Team copy", "", []string{admin.User.ID})
			return err
		},
	} {
		counts := func() []int64 {
			t.Helper()
			result := make([]int64, 6)
			for i, model := range []any{&entity.User{}, &entity.Team{}, &entity.TeamMembership{}, &entity.ResourceLimit{}, &entity.Session{}, &entity.AuditEvent{}} {
				if err := db.Model(model).Count(&result[i]).Error; err != nil {
					t.Fatal(err)
				}
			}
			return result
		}
		before := counts()
		failCopy.Store(true)
		err := create()
		failCopy.Store(false)
		if err == nil || !reflect.DeepEqual(before, counts()) {
			t.Fatal("default-copy failure persisted a partial resource", err)
		}
	}
	for _, row := range []any{
		&entity.UserModelGrant{UserID: user.User.ID, ModelID: modelID},
		&entity.APIKey{ID: "key_defaults", UserID: user.User.ID, Name: "Default acceptance", Prefix: "rx_masked", TokenHash: secret.SHA256Hex(bearer), Status: entity.KeyActive},
		&entity.APIKeyModel{KeyID: "key_defaults", ModelID: modelID},
		&entity.TeamModelGrant{TeamID: team.ID, ModelID: modelID},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	refresh()
	native := func() *httptest.ResponseRecorder { return invokeNative(bearer) }
	expectStatus(t, native(), 200)
	expectStatus(t, native(), 200)
	beforeDispatch := dispatches.Load()
	expectStatus(t, native(), 429)
	teamNative := asAdmin("POST", "/api/v1/teams/"+team.ID+"/chat/completions", map[string]any{"model": "default-model", "messages": []map[string]string{{"role": "user", "content": "Zero default"}}, "max_completion_tokens": 1}, "")
	expectStatus(t, teamNative, 429)
	if dispatches.Load() != beforeDispatch {
		t.Fatal("finite defaults dispatched exhausted User or zero-cap Team")
	}
	limitPath := "/api/v1/admin/members/" + user.User.ID + "/limits"
	readLimit := func() service.LimitRecord {
		t.Helper()
		return decodeCatalogResponse[service.LimitRecord](t, asAdmin("GET", limitPath, nil, ""), 200)
	}
	limit := readLimit()
	if limit.QuotaUsage == nil || limit.QuotaUsage.Month == nil || !limit.QuotaUsage.Month.Covered || limit.QuotaUsage.Month.TokensUsed != 6 {
		t.Fatal("default native usage not authoritatively settled", limit)
	}
	// Ordinary limit edits clear copied provenance; reset preserves IP and usage.
	expectStatus(t, asAdmin("PUT", limitPath, map[string]any{"tokens_month": 6, "ip_mode": "allowlist", "ip_ranges": []string{"127.0.0.0/8"}, "reason": "Independent IP policy"}, limit.ETag), 200)
	writeRule("user", 9)
	resetPath := limitPath + "/default-reset"
	selfReset := "/api/v1/admin/members/" + outsider.User.ID + "/limits/default-reset"
	selfContext := decodeCatalogResponse[service.DefaultLimitResetContext](t, request("GET", selfReset, nil, "", outsiderCookie, ""), 200)
	if selfContext.Editable || selfContext.DefaultRule == nil {
		t.Fatal("resource-scoped default review granted settings or reset authority")
	}
	expectStatus(t, request("POST", selfReset, map[string]any{"reason": "No reset authority"}, selfContext.ETag, outsiderCookie, outsiderCSRF), 403)
	teamReset := "/api/v1/teams/" + team.ID + "/limits/default-reset"
	expectStatus(t, request("GET", teamReset, nil, "", outsiderCookie, ""), 404)
	expectStatus(t, asAdmin("GET", "/api/v1/admin/members/"+strings.ToUpper(user.User.ID)+"/limits/default-reset", nil, ""), 404)
	readContext := func() service.DefaultLimitResetContext {
		t.Helper()
		return decodeCatalogResponse[service.DefaultLimitResetContext](t, asAdmin("GET", resetPath, nil, ""), 200)
	}
	review := readContext()
	if review.AppliedDefaultETag != nil || !review.Editable || review.DefaultRule == nil || review.Limit == nil {
		t.Fatal("ordinary policy edit retained default provenance or omitted review", review)
	}
	result := decodeCatalogResponse[service.DefaultLimitResetResult](t, asAdmin("POST", resetPath, map[string]any{"reason": "Explicit default reset"}, review.ETag), 200)
	if !result.Saved || !result.RuntimeApplied || result.Limit == nil || result.Limit.Stored.TokensMonth == nil || *result.Limit.Stored.TokensMonth != 9 || result.Limit.Stored.IPMode != "allowlist" || !reflect.DeepEqual(result.Limit.Stored.IPRanges, []string{"127.0.0.0/8"}) || result.Limit.QuotaUsage.Month.TokensUsed != 6 {
		t.Fatal("default reset changed usage/IP or falsely confirmed application", result)
	}
	if result.AppliedDefaultETag != review.DefaultRule.RuleETag || result.DefaultResetETag != review.ETag {
		t.Fatal("reset omitted exact reviewed source provenance")
	}
	expectStatus(t, native(), 200)
	beforeDispatch = dispatches.Load()
	expectStatus(t, native(), 429)
	if dispatches.Load() != beforeDispatch {
		t.Fatal("reset erased settled usage")
	}
	writeRule("team", 5)
	teamReview := decodeCatalogResponse[service.DefaultLimitResetContext](t, asAdmin("GET", teamReset, nil, ""), 200)
	teamResetResult := decodeCatalogResponse[service.DefaultLimitResetResult](t, asAdmin("POST", teamReset, map[string]any{"reason": "Explicit Team default reset"}, teamReview.ETag), 200)
	if !teamResetResult.Saved || !teamResetResult.RuntimeApplied || teamResetResult.Limit == nil || teamResetResult.Limit.Stored.TokensMonth == nil || *teamResetResult.Limit.Stored.TokensMonth != 5 {
		t.Fatal("Team reset did not apply the exact default revision", teamResetResult)
	}
	teamBody := map[string]any{"model": "default-model", "messages": []map[string]string{{"role": "user", "content": "Reset Team default"}}, "max_completion_tokens": 1}
	expectStatus(t, asAdmin("POST", "/api/v1/teams/"+team.ID+"/chat/completions", teamBody, ""), 200)
	beforeDispatch = dispatches.Load()
	expectStatus(t, asAdmin("POST", "/api/v1/teams/"+team.ID+"/chat/completions", teamBody, ""), 429)
	if dispatches.Load() != beforeDispatch {
		t.Fatal("Team reset did not enforce its saved aggregate cap")
	}
	// A saved reset whose publication failed remains replayable after a new
	// template revision; it must never replace its original saved target.
	writeRule("user", 12)
	lostReview := readContext()
	intent := map[string]any{"reason": "Controlled uncertain reset"}
	failPublication.Store(true)
	expectStatus(t, asAdmin("POST", resetPath, intent, lostReview.ETag), 503)
	failPublication.Store(false)
	expectStatus(t, asAdmin("POST", resetPath, map[string]any{"reason": "Different retry intent"}, lostReview.ETag), 409)
	writeRule("user", 15)
	replayed := decodeCatalogResponse[service.DefaultLimitResetResult](t, asAdmin("POST", resetPath, intent, lostReview.ETag), 200)
	if !replayed.Saved || !replayed.RuntimeApplied || replayed.AppliedDefaultETag != lostReview.DefaultRule.RuleETag || replayed.Limit.Stored.TokensMonth == nil || *replayed.Limit.Stored.TokensMonth != 12 {
		t.Fatal("unknown reset replay rebased onto a newer default", replayed)
	}
	current := readLimit()
	expectStatus(t, asAdmin("PUT", limitPath, map[string]any{"tokens_month": 18, "reason": "Later explicit policy"}, current.ETag), 200)
	expectStatus(t, asAdmin("POST", resetPath, intent, lostReview.ETag), 409)
	if after := readLimit(); after.Stored.TokensMonth == nil || *after.Stored.TokensMonth != 18 || after.QuotaUsage.Month.TokensUsed != 9 {
		t.Fatal("historical reset retry restored superseded policy or changed use", after)
	}
	// The exact decimal is stored unchanged. A pricing-generation change requires
	// a fresh composite review even when the denomination itself is unchanged.
	beforeMoney := readRule("user")
	moneyPolicy := policy(15)
	moneyPolicy["money_month"], moneyPolicy["currency"] = "0.000000000000000001", beforeMoney.PlatformCurrency
	moneyRule := decodeCatalogResponse[service.DefaultLimitRecord](t, asAdmin("PUT", rulePath("user"), map[string]any{"policy": moneyPolicy, "reason": "Exact creation money"}, beforeMoney.ETag), 200)
	if moneyRule.Policy.MoneyMonth == nil || *moneyRule.Policy.MoneyMonth != "0.000000000000000001" {
		t.Fatal("default template rounded exact money")
	}
	var pricingBefore entity.PricingSetting
	if err := db.First(&pricingBefore, 1).Error; err != nil {
		t.Fatal(err)
	}
	// A finite future-creation money rule blocks denomination changes even
	// when no existing account has copied a finite money policy.
	currencyBefore := decodeCatalogResponse[service.PricingCurrencyPage](t, asAdmin("GET", "/api/v1/admin/prices/currency", nil, ""), 200)
	expectStatus(t, asAdmin("PUT", "/api/v1/admin/prices/currency", map[string]any{
		"etag":     currencyBefore.ETag,
		"currency": map[string]any{"platform_currency": "EUR", "rates": map[string]string{"USD": "1"}},
	}, ""), 409)
	var currencyAfter entity.PricingSetting
	if err := db.First(&currencyAfter, 1).Error; err != nil || currencyAfter.ETag != pricingBefore.ETag || currencyAfter.PlatformCurrency != pricingBefore.PlatformCurrency {
		t.Fatal("finite creation default changed the platform denomination", currencyAfter, err)
	}
	if unchanged := readRule("user"); unchanged.RuleETag != moneyRule.RuleETag || unchanged.Policy.MoneyMonth == nil || *unchanged.Policy.MoneyMonth != "0.000000000000000001" {
		t.Fatal("rejected denomination change rewrote exact default money", unchanged)
	}
	if err := db.Model(&entity.PricingSetting{}).Where("id = ?", 1).Update("e_tag", "pricing-defaults-new-generation").Error; err != nil {
		t.Fatal(err)
	}
	expectStatus(t, asAdmin("PUT", rulePath("user"), map[string]any{"policy": policy(20), "reason": "Obsolete currency generation"}, moneyRule.ETag), 409)
	freshMoney := readRule("user")
	wrongCurrency := policy(20)
	wrongCurrency["money_month"], wrongCurrency["currency"] = "1", "EUR"
	expectStatus(t, asAdmin("PUT", rulePath("user"), map[string]any{"policy": wrongCurrency, "reason": "Wrong denomination"}, freshMoney.ETag), 409)
	if err := db.Save(&pricingBefore).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.FlushCallRecorder(ctx); err != nil {
		t.Fatal(err)
	}
	var history int64
	if err := db.Model(&entity.CallRecord{}).Where("user_id = ? AND key_id = ?", user.User.ID, "key_defaults").Count(&history).Error; err != nil || history < 3 {
		t.Fatal("default reset discarded immutable calls", history, err)
	}
	stateRouter, stateRuntime := memberStateRuntimeFixtureRouter(t, db, service.WithCredentialStorage(store), service.WithUpstreamPolicy(true))
	defer stateRuntime.StopRuntime()
	expectStatus(t, reviewedMemberStateFixtureRequest(t, stateRouter, adminCookie, admin.CSRFToken, user.User.ID, map[string]any{"disabled": true}), 200)
	expectStatus(t, asAdmin("GET", resetPath, nil, ""), 409)
	expectStatus(t, asAdmin("POST", resetPath, map[string]any{"reason": "Cannot reset disabled User"}, lostReview.ETag), 409)
	expectStatus(t, asAdmin("PATCH", "/api/v1/admin/teams/"+team.ID, map[string]any{"status": "disabled"}, ""), 200)
	expectStatus(t, asAdmin("GET", teamReset, nil, ""), 409)
	expectStatus(t, asAdmin("POST", teamReset, map[string]any{"reason": "Cannot reset inactive Team"}, teamReview.ETag), 409)
}
