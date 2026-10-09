package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/pricing"
	"github.com/miclle/routex/pkg/secretstore"
	"gorm.io/gorm"
)

// Root's real-driver harness supplies an independently migrated disposable DB.
func testModelAliasRetirementLifecycle(t *testing.T, db *gorm.DB) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	var auditFailure, armPublicationFailure, publicationFailure atomic.Bool
	const callback = "test:model_alias_retirement_faults"
	if err := db.Callback().Create().Before("gorm:create").Register(callback+":audit", func(tx *gorm.DB) {
		if auditFailure.Load() && tx.Statement.Table == "audit_events" {
			_ = tx.AddError(errors.New("controlled alias audit failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Create().After("gorm:create").Register(callback+":publication", func(tx *gorm.DB) {
		if armPublicationFailure.Load() && tx.Statement.Table == "audit_events" && tx.Error == nil {
			publicationFailure.Store(true)
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Query().Before("gorm:query").Register(callback, func(tx *gorm.DB) {
		if publicationFailure.Load() && tx.Statement.Table == "providers" {
			_ = tx.AddError(errors.New("controlled alias publication failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	var services []*service.Service
	var calls sync.WaitGroup
	entered, released := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(released) }) }
	defer func() {
		release()
		cancel()
		calls.Wait()
		auditFailure.Store(false)
		armPublicationFailure.Store(false)
		publicationFailure.Store(false)
		for _, svc := range services {
			svc.StopRuntime()
			_ = svc.StopCallRecorder()
		}
		// No global callback mutation until all runtime/recorder/native workers join.
		_ = db.Callback().Query().Remove(callback)
		_ = db.Callback().Create().Remove(callback + ":audit")
		_ = db.Callback().Create().Remove(callback + ":publication")
	}()
	var dispatches atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		protocol := entity.ProtocolOpenAIChat
		switch r.URL.Path {
		case "/v1/chat/completions":
		case "/v1/responses":
			protocol = entity.ProtocolOpenAIResponses
		case "/v1/messages":
			protocol = entity.ProtocolAnthropicMessages
		case "/v1beta/models/native-alias:generateContent":
			protocol = entity.ProtocolGeminiGenerateContent
		default:
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		dispatches.Add(1)
		if r.Header.Get("Cookie") != "" || r.Header.Get("X-CSRF-Token") != "" {
			t.Error("native dispatch leaked Session proof")
		}
		switch protocol {
		case entity.ProtocolAnthropicMessages:
			if r.Header.Get("x-api-key") != "alias-native-secret" || r.Header.Get("Authorization") != "" {
				t.Error("Messages lost exact native Credential")
			}
		case entity.ProtocolGeminiGenerateContent:
			if r.Header.Get("x-goog-api-key") != "alias-native-secret" || r.Header.Get("Authorization") != "" {
				t.Error("Gemini lost exact native Credential")
			}
		default:
			if r.Header.Get("Authorization") != "Bearer alias-native-secret" {
				t.Error("OpenAI lost exact native Credential")
			}
		}
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			return
		}
		var payload map[string]json.RawMessage
		if json.Unmarshal(raw, &payload) != nil || protocol != entity.ProtocolGeminiGenerateContent && string(payload["model"]) != `"native-alias"` {
			t.Error("native dispatch did not preserve its exact upstream model")
		}
		if strings.Contains(string(raw), "barrier") {
			close(entered)
			select {
			case <-released:
			case <-ctx.Done():
				return
			}
		}
		contentType, completion := teamNativeProtocolResponse(protocol, "completed", false)
		if protocol == entity.ProtocolOpenAIChat {
			completion = modelAliasNativeCompletion
		}
		w.Header().Set("Content-Type", contentType)
		_, _ = io.WriteString(w, completion)
	}))
	// Cleanup runs after the deferred native join even on a failed assertion.
	t.Cleanup(upstream.Close)
	store, err := secretstore.New([]byte(strings.Repeat("a", 32)))
	if err != nil {
		t.Fatal(err)
	}
	makeService := func() *service.Service {
		t.Helper()
		svc, err := service.New(ctx, db, service.WithCredentialStorage(store), service.WithUpstreamPolicy(true), service.WithRuntimeRefreshInterval(time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		services = append(services, svc)
		return svc
	}
	svc := makeService()
	router := fox.New()
	New(svc).RegisterRoutes(router)
	setup := identityRequest(router, http.MethodPost, "/api/v1/setup", `{"email":"alias-admin@example.invalid","password":"alias-test-password","name":"Alias administrator"}`, nil, "")
	expectStatus(t, setup, http.StatusCreated)
	admin, cookie := readIdentity(t, setup)
	reader, readCookie, readCSRF := createSystemStatusMember(t, svc, router, admin.User.ID, "alias-reader", []string{"models.read_all"})
	writer, writeCookie, writeCSRF := createSystemStatusMember(t, svc, router, admin.User.ID, "alias-writer", []string{"models.write"})
	_, deniedCookie, deniedCSRF := createSystemStatusMember(t, svc, router, admin.User.ID, "alias-denied", []string{"providers.read"})
	modelID, otherID, teamID := "mdl_alias_retirement", "mdl_alias_foreign", "tem_alias_retirement"
	cipher, err := store.Seal("crd_alias_retirement", "alias-native-secret")
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range []any{
		&entity.Provider{ID: "prv_alias_retirement", Name: "Controlled alias supplier"},
		&entity.ProviderConnection{ID: "con_alias_retirement", ProviderID: "prv_alias_retirement", Name: "Controlled Chat", Protocol: entity.ProtocolOpenAIChat, BaseURL: upstream.URL + "/v1"},
		&entity.ProviderCredential{ID: "crd_alias_retirement", ConnectionID: "con_alias_retirement", Name: "Verified", Ciphertext: cipher, Enabled: true, VerificationStatus: "verified"},
		&entity.ProviderModel{ID: "pmd_alias_retirement", ConnectionID: "con_alias_retirement", UpstreamName: "native-alias"},
		&entity.CredentialModelAccess{CredentialID: "crd_alias_retirement", ProviderModelID: "pmd_alias_retirement"},
		&entity.Model{ID: modelID, Status: entity.ResourceActive},
		&entity.Model{ID: otherID, Status: entity.ResourceActive},
		&entity.ModelName{Name: "Alias/Original", ModelID: modelID, CurrentModelID: &modelID},
		&entity.ModelName{Name: "foreign-alias-model", ModelID: otherID, CurrentModelID: &otherID},
		&entity.ModelProviderBinding{ID: "bnd_alias_retirement", ModelID: modelID, ProviderModelID: "pmd_alias_retirement", Weight: 100},
		&entity.UserModelGrant{UserID: admin.User.ID, ModelID: modelID},
		&entity.Team{ID: teamID, Name: "Alias Team", Status: entity.ResourceActive},
		&entity.TeamMembership{ID: "tmm_alias_retirement", TeamID: teamID, UserID: admin.User.ID, Role: entity.TeamOwner, Status: entity.ResourceActive},
		&entity.TeamModelGrant{TeamID: teamID, ModelID: modelID},
		&entity.ModelPrice{ID: "mpr_alias_retirement", ProviderModelID: "pmd_alias_retirement", UpdateSource: "api"},
		&entity.PriceRate{ID: "rat_alias_retirement", ModelPriceID: "mpr_alias_retirement", Metric: pricing.Input, Tier: pricing.Base, Unit: pricing.Unit, Currency: "USD", Amount: "0.125000000000000001", Enabled: true},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, protocol := range []string{entity.ProtocolOpenAIResponses, entity.ProtocolAnthropicMessages, entity.ProtocolGeminiGenerateContent} {
		suffix := teamNativeSuffix(protocol)
		con, credential, pm := "con_alias_"+suffix, "crd_alias_"+suffix, "pmd_alias_"+suffix
		ciphertext, err := store.Seal(credential, "alias-native-secret")
		if err != nil {
			t.Fatal(err)
		}
		base := upstream.URL + "/v1"
		if protocol == entity.ProtocolGeminiGenerateContent {
			base = upstream.URL + "/v1beta"
		}
		for _, row := range []any{
			&entity.ProviderConnection{ID: con, ProviderID: "prv_alias_retirement", Name: suffix, Protocol: protocol, BaseURL: base},
			&entity.ProviderCredential{ID: credential, ConnectionID: con, Name: "Verified " + suffix, Ciphertext: ciphertext, Enabled: true, VerificationStatus: "verified"},
			&entity.ProviderModel{ID: pm, ConnectionID: con, UpstreamName: "native-alias"},
			&entity.CredentialModelAccess{CredentialID: credential, ProviderModelID: pm},
			&entity.ModelProviderBinding{ID: "bnd_alias_" + suffix, ModelID: modelID, ProviderModelID: pm, Weight: 100},
		} {
			if err := db.Create(row).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	request := func(r http.Handler, method, path, raw, etag string, principal *http.Cookie, csrf string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "http://routex.test"+path, strings.NewReader(raw)).WithContext(ctx)
		req.Header.Set("Content-Type", "application/json")
		if principal != nil {
			req.AddCookie(principal)
		}
		if csrf != "" {
			req.Header.Set("X-CSRF-Token", csrf)
		}
		if etag != "" {
			req.Header.Set("If-Match", etag)
		}
		res := httptest.NewRecorder()
		r.ServeHTTP(res, req)
		return res
	}
	path := "/api/v1/admin/models/" + modelID
	rename := func(name string, deadline time.Time) {
		t.Helper()
		raw, err := json.Marshal(map[string]any{"name": name, "alias_expires_at": deadline.Format(time.RFC3339Nano)})
		if err != nil {
			t.Fatal(err)
		}
		expectStatus(t, request(router, http.MethodPost, path+"/rename", string(raw), "", cookie, admin.CSRFToken), http.StatusOK)
	}
	for _, name := range []string{"AliasOriginal", "alias-audit", "alias-outage", "alias-current"} {
		rename(name, time.Now().UTC().Add(time.Hour))
	}
	key := decodeCatalogResponse[CreatedKeyResponse](t, request(router, http.MethodPost, "/api/v1/keys", `{"name":"Alias native Key","model_ids":["`+modelID+`"]}`, "", cookie, admin.CSRFToken), http.StatusCreated)
	expectStatus(t, request(router, http.MethodPost, "/api/v1/keys/"+key.Key.ID+"/confirm", "", "", cookie, admin.CSRFToken), http.StatusOK)
	if err := svc.StartRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	// Explicit refreshes keep deterministic publication faults out of ticker races.
	journal := filepath.Join(t.TempDir(), "alias-calls.db")
	if err := svc.StartCallRecorder(ctx, journal); err != nil {
		t.Fatal(err)
	}
	reviewPath := func(id, name string) string {
		return "/api/v1/admin/models/" + url.PathEscape(id) + "/alias-retirement?name=" + url.QueryEscape(name)
	}
	read := func(name string) service.ModelAliasRetirementReview {
		t.Helper()
		res := request(router, http.MethodGet, reviewPath(modelID, name), "", "", cookie, "")
		value := decodeCatalogResponse[service.ModelAliasRetirementReview](t, res, http.StatusOK)
		if value.ModelID != modelID || value.Alias.Name != name || len(value.ETag) != 64 || res.Header().Get("ETag") != strconv.Quote(value.ETag) || !strings.Contains(res.Header().Get("Cache-Control"), "no-store") || value.ObservedAt.IsZero() {
			t.Fatalf("alias review lacks exact private server context: %+v", value)
		}
		for _, forbidden := range []string{"ciphertext", "alias-native-secret", key.Secret, "granted_user_ids", "provider_model_id"} {
			if strings.Contains(res.Body.String(), forbidden) {
				t.Fatal("alias review exposed unrelated private facts")
			}
		}
		return value
	}
	retire := func(name, etag string) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(service.ModelAliasRetirementInput{Name: name, Reason: "Reviewed compatibility deadline"})
		return request(router, http.MethodPost, path+"/alias-retirement", string(raw), strconv.Quote(etag), cookie, admin.CSRFToken)
	}
	native := func(r http.Handler, name, prompt string, team bool, protocol string) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(map[string]any{"model": name, "messages": []map[string]string{{"role": "user", "content": prompt}}, "max_tokens": 8})
		target := "/v1/chat/completions"
		switch protocol {
		case entity.ProtocolOpenAIResponses:
			raw, _ = json.Marshal(map[string]any{"model": name, "input": "Denied alias", "max_output_tokens": 8})
			target = "/v1/responses"
		case entity.ProtocolAnthropicMessages:
			target = "/v1/messages"
		case entity.ProtocolGeminiGenerateContent:
			raw = []byte(`{"contents":[{"role":"user","parts":[{"text":"Denied alias"}]}],"generationConfig":{"maxOutputTokens":8}}`)
			target = "/v1beta/models/" + name + ":generateContent"
		}
		if team {
			target = "/api/v1/teams/" + teamID + strings.TrimPrefix(target, "/v1")
			if protocol == entity.ProtocolGeminiGenerateContent {
				target = "/api/v1/teams/" + teamID + "/models/" + name + ":generateContent"
			}
		}
		req := httptest.NewRequest(http.MethodPost, "http://routex.test"+target, strings.NewReader(string(raw))).WithContext(ctx)
		req.Header.Set("Content-Type", "application/json")
		if protocol == entity.ProtocolAnthropicMessages {
			req.Header.Set("anthropic-version", "2023-06-01")
		}
		if team {
			req.AddCookie(cookie)
			req.Header.Set("X-CSRF-Token", admin.CSRFToken)
			req.Header.Set("Origin", "http://routex.test")
		} else {
			req.Header.Set("Authorization", "Bearer "+key.Secret)
		}
		res := httptest.NewRecorder()
		r.ServeHTTP(res, req)
		return res
	}
	auditCount := func() int64 {
		t.Helper()
		var count int64
		if err := db.Model(&entity.AuditEvent{}).Where("resource_id = ? AND action = ?", modelID, "model.alias.retire").Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		return count
	}
	savedName := func(name string) entity.ModelName {
		t.Helper()
		var row entity.ModelName
		if err := db.First(&row, "name = ?", name).Error; err != nil {
			t.Fatal(err)
		}
		return row
	}
	t.Log("alias stage: exact selectors, independent permissions and stale review")
	slashReview := read("Alias/Original")
	if slashReview.Alias.Name != "Alias/Original" || slashReview.State != "compatibility" {
		t.Fatal("URL-encoded scalar name lost embedded slash identity")
	}
	initial := read("AliasOriginal")
	if _, err := svc.GetModelAliasRetirement(ctx, strings.ToUpper(admin.User.ID), modelID, "AliasOriginal"); err == nil {
		t.Fatal("actor collation alias borrowed review authority")
	}
	if initial.State != "compatibility" || !initial.CanRetire || !initial.RuntimeApplied || initial.CurrentName != "alias-current" {
		t.Fatalf("live compatibility review incorrect: %+v", initial)
	}
	raw := `{"name":"AliasOriginal","reason":"Reviewed"}`
	expectStatus(t, request(router, http.MethodGet, reviewPath(modelID, "AliasOriginal"), "", "", nil, ""), http.StatusUnauthorized)
	expectStatus(t, request(router, http.MethodGet, reviewPath(modelID, "AliasOriginal"), "", "", writeCookie, ""), http.StatusForbidden)
	readerReview := decodeCatalogResponse[service.ModelAliasRetirementReview](t, request(router, http.MethodGet, reviewPath(modelID, "AliasOriginal"), "", "", readCookie, ""), http.StatusOK)
	if readerReview.CanRetire {
		t.Fatal("read-only role received stop authority")
	}
	for _, principal := range []struct {
		cookie *http.Cookie
		csrf   string
	}{{readCookie, readCSRF}, {deniedCookie, deniedCSRF}, {cookie, ""}} {
		expectStatus(t, request(router, http.MethodPost, path+"/alias-retirement", raw, strconv.Quote(initial.ETag), principal.cookie, principal.csrf), http.StatusForbidden)
	}
	for _, selector := range []string{"Aliasoriginal", "foreign-alias-model"} {
		expectStatus(t, request(router, http.MethodGet, reviewPath(modelID, selector), "", "", cookie, ""), http.StatusNotFound)
	}
	for _, id := range []string{"mdl_" + strings.ToUpper(strings.TrimPrefix(modelID, "mdl_"))} {
		expectStatus(t, request(router, http.MethodGet, reviewPath(id, "AliasOriginal"), "", "", cookie, ""), http.StatusNotFound)
	}
	expectStatus(t, request(router, http.MethodGet, reviewPath(modelID, "AliasOriginal "), "", "", cookie, ""), http.StatusBadRequest)
	expectStatus(t, request(router, http.MethodGet, reviewPath(modelID+" ", "AliasOriginal"), "", "", cookie, ""), http.StatusBadRequest)
	for _, query := range []string{"", "?name=", "?name=alias-audit&name=alias-audit", "?name=alias-audit&user_id=" + admin.User.ID} {
		expectStatus(t, request(router, http.MethodGet, path+"/alias-retirement"+query, "", "", cookie, ""), http.StatusBadRequest)
	}
	for _, etag := range []string{"", "unquoted", `W/"` + initial.ETag + `"`, strconv.Quote(strings.Repeat("F", 64))} {
		expectStatus(t, request(router, http.MethodPost, path+"/alias-retirement", raw, etag, cookie, admin.CSRFToken), http.StatusBadRequest)
	}
	for _, body := range []string{`{"name":"AliasOriginal","reason":""}`, `{"name":"AliasOriginal","reason":"bad\nreason"}`, `{"name":"AliasOriginal","reason":null}`, `{"name":"AliasOriginal","reason":"Reviewed","name":"alias-audit"}`, `{"name":"AliasOriginal","reason":"Reviewed","expires_at":"tomorrow"}`, `{"name":"AliasOriginal","reason":"` + strings.Repeat("界", 342) + `"}`} {
		expectStatus(t, request(router, http.MethodPost, path+"/alias-retirement", body, strconv.Quote(initial.ETag), cookie, admin.CSRFToken), http.StatusBadRequest)
	}
	current := read("alias-current")
	expectStatus(t, retire("alias-current", current.ETag), http.StatusConflict)
	if current.State != "current" || current.CanRetire || current.Alias.ExpiresAt != nil {
		t.Fatal("current name received compatibility semantics")
	}
	rename("alias-current-next", time.Now().UTC().Add(time.Hour))
	expectStatus(t, retire("AliasOriginal", initial.ETag), http.StatusConflict)
	expectStatus(t, retire("foreign-alias-model", initial.ETag), http.StatusNotFound)
	if auditCount() != 0 {
		t.Fatal("denied/conflicting stop wrote audit")
	}
	// Persisted resource baselines are captured after legitimate rename/key setup.
	baselineTables := []string{"models", "model_provider_bindings", "user_model_grants", "team_model_grants", "api_keys", "api_key_models", "model_prices", "price_rates"}
	var baselineModel entity.Model
	if err := db.Take(&baselineModel, "id = ?", modelID).Error; err != nil || baselineModel.ConfigUpdatedAt == nil {
		t.Fatal("alias baseline has no recorded configuration timestamp", err)
	}
	recordedModel := baselineModel
	assertRecordedModel := func(changed bool) {
		t.Helper()
		var current entity.Model
		if err := db.Take(&current, "id = ?", modelID).Error; err != nil || current.ConfigUpdatedAt == nil || !current.CreatedAt.Equal(recordedModel.CreatedAt) {
			t.Fatal("alias operation lost recorded configuration time or changed birth", err)
		}
		if changed && !current.ConfigUpdatedAt.After(*recordedModel.ConfigUpdatedAt) || !changed && !current.ConfigUpdatedAt.Equal(*recordedModel.ConfigUpdatedAt) {
			t.Fatal("alias configuration time disagrees with saved change, replay or rollback")
		}
		recordedModel = current
	}
	baseline := make(map[string]string)
	capture := func(table string) string {
		t.Helper()
		var rows []map[string]any
		if err := db.Table(table).Find(&rows).Error; err != nil {
			t.Fatal(err)
		}
		encoded := make([]string, 0, len(rows))
		for _, row := range rows {
			if table == "models" {
				var rowID string
				switch value := row["id"].(type) {
				case string:
					rowID = value
				case []byte:
					rowID = string(value)
				default:
					t.Fatal("unknown persisted Model ID representation")
				}
				if rowID == modelID {
					// Only legitimate expiry changes may advance this target's time.
					delete(row, "config_updated_at")
				}
			}
			raw, err := json.Marshal(row)
			if err != nil {
				t.Fatal(err)
			}
			encoded = append(encoded, string(raw))
		}
		slices.Sort(encoded)
		return strings.Join(encoded, "\n")
	}
	for _, table := range baselineTables {
		baseline[table] = capture(table)
	}
	t.Log("alias stage: audit rollback preserves exact saved deadline")
	auditReview, auditBefore := read("alias-audit"), savedName("alias-audit")
	auditFailure.Store(true)
	expectStatus(t, retire("alias-audit", auditReview.ETag), http.StatusInternalServerError)
	auditFailure.Store(false)
	if !reflect.DeepEqual(auditBefore, savedName("alias-audit")) || auditCount() != 0 {
		t.Fatal("audit failure committed deadline or audit")
	}
	assertRecordedModel(false)
	t.Log("alias stage: genuine Personal and Team alias calls before stop")
	var beforeIDs []string
	teamRequestIDs := make(map[string]bool)
	expectedProtocols := make(map[string]string)
	for _, team := range []bool{false, true} {
		for _, protocol := range []string{entity.ProtocolOpenAIChat, entity.ProtocolOpenAIResponses, entity.ProtocolAnthropicMessages, entity.ProtocolGeminiGenerateContent} {
			t.Logf("live alias native Team=%v protocol=%s", team, protocol)
			res := native(router, "AliasOriginal", "Before", team, protocol)
			expectStatus(t, res, http.StatusOK)
			requestID := res.Header().Get("X-Request-ID")
			if requestID == "" {
				t.Fatal("native alias omitted request identity")
			}
			beforeIDs = append(beforeIDs, requestID)
			teamRequestIDs[requestID] = team
			expectedProtocols[requestID] = protocol
		}
	}
	if dispatches.Load() != 8 {
		t.Fatal("expected eight genuine Key/Team native alias dispatches")
	}
	t.Log("alias stage: already-dispatched call may finish after published stop")
	blocked := make(chan *httptest.ResponseRecorder, 1)
	calls.Go(func() { blocked <- native(router, "AliasOriginal", "barrier", false, entity.ProtocolOpenAIChat) })
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("native alias did not reach real upstream barrier")
	}
	review := read("AliasOriginal")
	stopBefore := time.Now().UTC()
	stopped := decodeCatalogResponse[service.ModelAliasRetirementResult](t, retire("AliasOriginal", review.ETag), http.StatusOK)
	stopAfter := time.Now().UTC()
	if !stopped.Retired || !stopped.Changed || !stopped.RuntimeApplied || stopped.Alias == nil || stopped.Alias.State != "retired" || stopped.Alias.CanRetire || stopped.Alias.Alias.ExpiresAt == nil || stopped.Alias.Alias.ExpiresAt.Before(stopBefore.Add(-time.Millisecond)) || stopped.Alias.Alias.ExpiresAt.After(stopAfter) {
		t.Fatalf("stop lacks saved exact deadline/current runtime proof: %+v", stopped)
	}
	assertRecordedModel(true)
	release()
	var finished *httptest.ResponseRecorder
	select {
	case finished = <-blocked:
	case <-ctx.Done():
		t.Fatal("already-dispatched call did not finish")
	}
	expectStatus(t, finished, http.StatusOK)
	calls.Wait()
	retained := savedName("AliasOriginal")
	if retained.ModelID != modelID || retained.CurrentModelID != nil || retained.ExpiresAt == nil || !retained.ExpiresAt.Equal(*stopped.Alias.Alias.ExpiresAt) || auditCount() != 1 {
		t.Fatal("stop lost retained exact owner/deadline or duplicated audit")
	}
	var stopAudit entity.AuditEvent
	if err := db.Where("resource_id = ? AND action = ?", modelID, "model.alias.retire").Take(&stopAudit).Error; err != nil {
		t.Fatal(err)
	}
	var stopDetails struct {
		ModelID string     `json:"model_id"`
		Name    string     `json:"name"`
		Before  *time.Time `json:"before_expires_at"`
		After   *time.Time `json:"after_expires_at"`
		Reason  string     `json:"reason"`
	}
	if stopAudit.ActorID != admin.User.ID || stopAudit.ResourceType != "model" || stopAudit.DetailsJSON == nil || json.Unmarshal([]byte(*stopAudit.DetailsJSON), &stopDetails) != nil || stopDetails.ModelID != modelID || stopDetails.Name != "AliasOriginal" || stopDetails.Reason != "Reviewed compatibility deadline" || stopDetails.Before == nil || !stopDetails.Before.Equal(*review.Alias.ExpiresAt) || stopDetails.After == nil || !stopDetails.After.Equal(*retained.ExpiresAt) {
		t.Fatalf("stop audit lost exact authorized transition: %+v", stopAudit)
	}
	t.Log("alias stage: four native name positions deny stopped alias without dispatch")
	var deniedRequestIDs []string
	for _, team := range []bool{false, true} {
		for _, protocol := range []string{entity.ProtocolOpenAIChat, entity.ProtocolOpenAIResponses, entity.ProtocolAnthropicMessages, entity.ProtocolGeminiGenerateContent} {
			t.Logf("stopped alias denial Team=%v protocol=%s", team, protocol)
			denial := native(router, "AliasOriginal", "Denied", team, protocol)
			expectStatus(t, denial, http.StatusNotFound)
			deniedRequestIDs = append(deniedRequestIDs, denial.Header().Get("X-Request-ID"))
		}
	}
	if dispatches.Load() != 9 {
		t.Fatal("stopped alias dispatched upstream")
	}
	retried := decodeCatalogResponse[service.ModelAliasRetirementResult](t, retire("AliasOriginal", review.ETag), http.StatusOK)
	if retried.Changed || !retried.Retired || !retried.RuntimeApplied || auditCount() != 1 || !reflect.DeepEqual(retained, savedName("AliasOriginal")) {
		t.Fatal("exact retired-target retry rewrote deadline/audit")
	}
	assertRecordedModel(false)
	t.Log("alias stage: publication failure is saved expiry, not runtime success")
	outageReview := read("alias-outage")
	armPublicationFailure.Store(true)
	outageBody := `{"name":"alias-outage","reason":"Reviewed write-only compatibility deadline"}`
	expectStatus(t, request(router, http.MethodPost, path+"/alias-retirement", outageBody, strconv.Quote(outageReview.ETag), writeCookie, writeCSRF), http.StatusServiceUnavailable)
	armPublicationFailure.Store(false)
	outageSaved := savedName("alias-outage")
	outageRead := read("alias-outage")
	if outageSaved.ExpiresAt == nil || outageSaved.ExpiresAt.After(time.Now()) || outageRead.State != "retired" || outageRead.RuntimeApplied || auditCount() != 2 {
		t.Fatal("publication failure lost commit or claimed application")
	}
	assertRecordedModel(true)
	expectStatus(t, native(router, "alias-outage", "Denied outage", false, entity.ProtocolOpenAIChat), http.StatusNotFound)
	publicationFailure.Store(false)
	outageRetry := decodeCatalogResponse[service.ModelAliasRetirementResult](t, request(router, http.MethodPost, path+"/alias-retirement", outageBody, strconv.Quote(outageReview.ETag), writeCookie, writeCSRF), http.StatusOK)
	if outageRetry.Changed || !outageRetry.RuntimeApplied || auditCount() != 2 || !reflect.DeepEqual(outageSaved, savedName("alias-outage")) {
		t.Fatal("publication retry restored expiry or duplicated audit")
	}
	assertRecordedModel(false)
	for _, team := range []bool{false, true} {
		denial := native(router, "alias-outage", "Denied Gemini", team, entity.ProtocolGeminiGenerateContent)
		expectStatus(t, denial, http.StatusNotFound)
		deniedRequestIDs = append(deniedRequestIDs, denial.Header().Get("X-Request-ID"))
	}
	if dispatches.Load() != 9 {
		t.Fatal("publication outage/reconciliation dispatched stopped alias")
	}
	// Write-only authority is independent of review authority. Lost authority
	// must be checked before an already-retired target can reconcile.
	if _, err := svc.SetMemberRoles(ctx, admin.User.ID, writer.User.ID, nil); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, request(router, http.MethodPost, path+"/alias-retirement", raw, strconv.Quote(review.ETag), writeCookie, writeCSRF), http.StatusForbidden)
	if _, err := svc.RetireModelAlias(ctx, reader.User.ID, modelID, review.ETag, service.ModelAliasRetirementInput{Name: "AliasOriginal", Reason: "No borrowed permission"}); err == nil {
		t.Fatal("direct service accepted read-only actor")
	}
	t.Log("alias stage: reserved name, current native name and immutable call history")
	expectStatus(t, request(router, http.MethodPost, path+"/rename", `{"name":"AliasOriginal"}`, "", cookie, admin.CSRFToken), http.StatusConflict)
	expectStatus(t, request(router, http.MethodPost, "/api/v1/admin/models", `{"name":"AliasOriginal","provider_model_id":"pmd_alias_retirement"}`, "", cookie, admin.CSRFToken), http.StatusConflict)
	currentCall := native(router, "alias-current-next", "Current still works", false, entity.ProtocolOpenAIChat)
	expectStatus(t, currentCall, http.StatusOK)
	if err := svc.FlushCallRecorder(ctx); err != nil {
		t.Fatal(err)
	}
	var deniedAttempts int64
	if err := db.Model(&entity.CallAttempt{}).Where("request_id IN ?", deniedRequestIDs).Count(&deniedAttempts).Error; err != nil || deniedAttempts != 0 {
		t.Fatalf("stopped aliases entered native Execute: attempts=%d err=%v", deniedAttempts, err)
	}
	var immutableCalls []entity.CallRecord
	var immutableAttempts []entity.CallAttempt
	requestIDs := append(slices.Clone(beforeIDs), finished.Header().Get("X-Request-ID"), currentCall.Header().Get("X-Request-ID"))
	expectedProtocols[finished.Header().Get("X-Request-ID")] = entity.ProtocolOpenAIChat
	expectedProtocols[currentCall.Header().Get("X-Request-ID")] = entity.ProtocolOpenAIChat
	if err := db.Where("request_id IN ?", requestIDs).Order("request_id").Find(&immutableCalls).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Where("request_id IN ?", requestIDs).Order("id").Find(&immutableAttempts).Error; err != nil {
		t.Fatal(err)
	}
	if len(immutableCalls) != 10 || len(immutableAttempts) != 10 {
		t.Fatalf("native immutable history missing: calls=%d attempts=%d", len(immutableCalls), len(immutableAttempts))
	}
	for _, call := range immutableCalls {
		if call.ModelID != modelID || call.UserID != admin.User.ID || call.ProjectID != "" || call.Protocol != expectedProtocols[call.RequestID] || call.InputTokens == nil || *call.InputTokens != 4 || call.OutputTokens == nil || *call.OutputTokens != 1 || call.Status != "success" || call.SnapshotID == "" || call.ModelName != "AliasOriginal" && call.ModelName != "alias-current-next" {
			t.Fatalf("native alias history lost original stable attribution: %+v", call)
		}
		if teamRequestIDs[call.RequestID] {
			if call.TeamID != teamID || call.TeamMembershipID != "tmm_alias_retirement" || call.KeyID != "" {
				t.Fatal("Team alias borrowed Personal Key attribution")
			}
		} else if call.KeyID != key.Key.ID || call.TeamID != "" || call.TeamMembershipID != "" {
			t.Fatal("Personal alias borrowed Team attribution")
		}
	}
	snapshotByRequest := make(map[string]string)
	for _, call := range immutableCalls {
		snapshotByRequest[call.RequestID] = call.SnapshotID
	}
	for _, attempt := range immutableAttempts {
		expectedCredential, expectedProviderModel := "crd_alias_retirement", "pmd_alias_retirement"
		if protocol := expectedProtocols[attempt.RequestID]; protocol != entity.ProtocolOpenAIChat {
			suffix := teamNativeSuffix(protocol)
			expectedCredential, expectedProviderModel = "crd_alias_"+suffix, "pmd_alias_"+suffix
		}
		if attempt.Status != "success" || attempt.NativeCompletionEvidence != "completed" || attempt.CredentialID != expectedCredential || attempt.ProviderModelID != expectedProviderModel || attempt.SnapshotID == "" || attempt.SnapshotID != snapshotByRequest[attempt.RequestID] {
			t.Fatalf("alias attempt lacked genuine completed attribution: %+v", attempt)
		}
	}
	assertRecordedModel(false)
	for _, table := range baselineTables {
		if capture(table) != baseline[table] {
			t.Fatalf("alias stop changed unrelated resource table %s", table)
		}
	}
	var afterAliasModel entity.Model
	if err := db.Take(&afterAliasModel, "id = ?", modelID).Error; err != nil || afterAliasModel.ConfigUpdatedAt == nil || !afterAliasModel.ConfigUpdatedAt.After(*baselineModel.ConfigUpdatedAt) || !afterAliasModel.CreatedAt.Equal(baselineModel.CreatedAt) {
		t.Fatal("legitimate alias expiry lost monotonic target configuration time or changed birth", err)
	}
	t.Log("alias stage: natural expiry reconciles target without historical operation claim")
	rename("alias-final", time.Now().UTC().Add(time.Second))
	naturalReview, naturalSaved := read("alias-current-next"), savedName("alias-current-next")
	if naturalSaved.ExpiresAt == nil {
		t.Fatal("real rename did not persist natural compatibility deadline")
	}
	wait := time.Until(naturalSaved.ExpiresAt.Add(5 * time.Millisecond))
	if wait > 0 {
		timer := time.NewTimer(wait)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			t.Fatal("natural expiry wait exceeded fixture budget")
		}
	}
	naturalResult := decodeCatalogResponse[service.ModelAliasRetirementResult](t, retire("alias-current-next", naturalReview.ETag), http.StatusOK)
	if naturalResult.Changed || !naturalResult.Retired || !naturalResult.RuntimeApplied || auditCount() != 2 || !reflect.DeepEqual(naturalSaved, savedName("alias-current-next")) {
		t.Fatal("natural expiry invented historical stop, changed deadline or failed current publication")
	}
	expectStatus(t, native(router, "alias-current-next", "Naturally expired", false, entity.ProtocolOpenAIChat), http.StatusNotFound)
	caseDistinct := decodeCatalogResponse[ModelResponse](t, request(router, http.MethodPost, "/api/v1/admin/models", `{"name":"aliasoriginal","provider_model_id":"pmd_alias_retirement"}`, "", cookie, admin.CSRFToken), http.StatusCreated)
	if caseDistinct.ID == modelID || caseDistinct.Name != "aliasoriginal" {
		t.Fatal("reserved exact name consumed case-distinct identity")
	}
	svc.StopRuntime() // Join the outgoing live publisher before service replacement.
	if err := svc.StopCallRecorder(); err != nil {
		t.Fatal(err)
	}
	t.Log("alias stage: new Service/journal restart retains expiry, history and reservation")
	restarted := makeService()
	restartRouter := fox.New()
	New(restarted).RegisterRoutes(restartRouter)
	if err := restarted.StartRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	if err := restarted.StartCallRecorder(ctx, journal); err != nil {
		t.Fatal(err)
	}
	if err := restarted.FlushCallRecorder(ctx); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, native(restartRouter, "AliasOriginal", "Denied restart", false, entity.ProtocolOpenAIChat), http.StatusNotFound)
	expectStatus(t, native(restartRouter, "alias-final", "Current after restart", true, entity.ProtocolOpenAIChat), http.StatusOK)
	if dispatches.Load() != 11 || !reflect.DeepEqual(retained, savedName("AliasOriginal")) || auditCount() != 2 {
		t.Fatal("restart replayed dispatch, restored alias or rewrote audit")
	}
	var afterCalls []entity.CallRecord
	var afterAttempts []entity.CallAttempt
	if err := db.Where("request_id IN ?", requestIDs).Order("request_id").Find(&afterCalls).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Where("request_id IN ?", requestIDs).Order("id").Find(&afterAttempts).Error; err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(immutableCalls, afterCalls) || !reflect.DeepEqual(immutableAttempts, afterAttempts) {
		t.Fatal("alias mutation/restart rewrote immutable native facts")
	}
	t.Logf("alias lifecycle proved %d genuine native dispatches, retained names and exact immutable history", dispatches.Load())
}

const modelAliasNativeCompletion = `{"object":"chat.completion","model":"native-alias","choices":[{"index":0,"message":{"role":"assistant","content":"Completed"},"finish_reason":"stop"}],"usage":{"prompt_tokens":4,"completion_tokens":1,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0}}}`

func TestModelAliasRetirementControlledCompletion(t *testing.T) {
	usage := parseGatewayUsage([]byte(modelAliasNativeCompletion))
	if !usage.Complete || usage.NativeCompletionEvidence != "completed" || usage.Input == nil || *usage.Input != 4 || usage.Output == nil || *usage.Output != 1 {
		t.Fatalf("controlled response lacks exact native completion/usage: %+v", usage)
	}
}
