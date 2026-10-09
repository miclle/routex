package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fox-gonic/fox"
	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/limits"
	"github.com/miclle/routex/pkg/pricing"
	"github.com/miclle/routex/pkg/secret"
	"github.com/miclle/routex/pkg/secretstore"
)

// The shared harness executes this against each supported real database.
func testTeamNativeProtocolsLifecycle(t *testing.T, db *gorm.DB) {
	ctx := context.Background()
	var storageReads atomic.Int64
	const storageCallback = "test_team_native_storage_reads"
	if err := db.Callback().Query().Before("gorm:query").Register(storageCallback, func(tx *gorm.DB) {
		if tx.Statement.Table == "storage_objects" {
			storageReads.Add(1)
		}
	}); err != nil {
		t.Fatal(err)
	}
	// Register before starting database workers and remove after their later defers stop them.
	defer func() { _ = db.Callback().Query().Remove(storageCallback) }()
	store, err := secretstore.New(bytes.Repeat([]byte{137}, 32))
	if err != nil {
		t.Fatal(err)
	}
	protocols := []string{entity.ProtocolOpenAIChat, entity.ProtocolOpenAIResponses, entity.ProtocolAnthropicMessages, entity.ProtocolGeminiGenerateContent}
	var dispatches atomic.Int64
	var mode atomic.Value
	mode.Store("completed")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dispatches.Add(1)
		protocol := ""
		switch r.URL.Path {
		case "/v1/chat/completions":
			protocol = entity.ProtocolOpenAIChat
		case "/v1/responses":
			protocol = entity.ProtocolOpenAIResponses
		case "/v1/messages":
			protocol = entity.ProtocolAnthropicMessages
		case "/v1beta/models/native-gemini:generateContent", "/v1beta/models/native-gemini:streamGenerateContent":
			protocol = entity.ProtocolGeminiGenerateContent
		default:
			t.Errorf("unexpected native endpoint %s", r.URL.String())
			w.WriteHeader(400)
			return
		}
		if r.Header.Get("Cookie") != "" || r.Header.Get("X-CSRF-Token") != "" || r.URL.Query().Get("key") != "" {
			t.Error("private Session or Key reached upstream")
		}
		if protocol == entity.ProtocolAnthropicMessages {
			if r.Header.Get("x-api-key") != "native-team-upstream" || r.Header.Get("anthropic-version") != "2023-06-01" || r.Header.Get("Authorization") != "" {
				t.Error("Messages credential/header transport changed")
			}
		} else if protocol == entity.ProtocolGeminiGenerateContent {
			if r.Header.Get("x-goog-api-key") != "native-team-upstream" || r.Header.Get("Authorization") != "" {
				t.Error("Gemini credential transport changed")
			}
		} else if r.Header.Get("Authorization") != "Bearer native-team-upstream" {
			t.Error("OpenAI credential transport changed")
		}
		var payload map[string]json.RawMessage
		if json.NewDecoder(r.Body).Decode(&payload) != nil {
			t.Error("invalid native JSON")
			w.WriteHeader(400)
			return
		}
		stream := string(payload["stream"]) == "true"
		if protocol == entity.ProtocolGeminiGenerateContent {
			stream = strings.HasSuffix(r.URL.Path, ":streamGenerateContent")
			if payload["model"] != nil || payload["stream"] != nil || payload["contents"] == nil || stream && r.URL.Query().Get("alt") != "sse" {
				t.Error("Gemini request was translated")
			}
		} else {
			var name string
			if json.Unmarshal(payload["model"], &name) != nil || name != "native-"+teamNativeSuffix(protocol) {
				t.Error("incorrect exact native model")
			}
		}
		contentType, raw := teamNativeProtocolResponse(protocol, mode.Load().(string), stream)
		w.Header().Set("Content-Type", contentType)
		_, _ = io.WriteString(w, raw)
	}))
	defer upstream.Close()
	var instances []*service.Service
	defer func() {
		for _, instance := range instances {
			instance.StopRuntime()
			_ = instance.StopCallRecorder()
		}
	}()
	makeService := func() *service.Service {
		t.Helper()
		value, err := service.New(ctx, db, service.WithCredentialStorage(store), service.WithUpstreamPolicy(true), service.WithRuntimeRefreshInterval(time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		instances = append(instances, value)
		return value
	}
	svc := makeService()
	admin, err := svc.Initialize(ctx, "team-native-admin@example.invalid", "team-native-password", "Native Team administrator")
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
	create(&entity.Provider{ID: "prv_team_native", Name: "Native Team provider"})
	modelIDs := []string{}
	for _, protocol := range protocols {
		suffix := teamNativeSuffix(protocol)
		connectionID, credentialID, providerModelID, modelID := "con_tnative_"+suffix, "crd_tnative_"+suffix, "pmd_tnative_"+suffix, "mdl_tnative_"+suffix
		cipher, err := store.Seal(credentialID, "native-team-upstream")
		if err != nil {
			t.Fatal(err)
		}
		base := upstream.URL + "/v1"
		if protocol == entity.ProtocolGeminiGenerateContent {
			base = upstream.URL + "/v1beta"
		}
		create(
			&entity.ProviderConnection{ID: connectionID, ProviderID: "prv_team_native", Name: suffix, Protocol: protocol, BaseURL: base},
			&entity.ProviderCredential{ID: credentialID, ConnectionID: connectionID, Name: "Verified native credential", Ciphertext: cipher, Enabled: true, VerificationStatus: "verified"},
			&entity.ProviderModel{ID: providerModelID, ConnectionID: connectionID, UpstreamName: "native-" + suffix},
			&entity.CredentialModelAccess{CredentialID: credentialID, ProviderModelID: providerModelID},
			&entity.Model{ID: modelID, Status: entity.ResourceActive},
			&entity.ModelName{Name: "team-native-" + suffix, ModelID: modelID, CurrentModelID: &modelID},
			&entity.ModelProviderBinding{ID: "mpb_tnative_" + suffix, ModelID: modelID, ProviderModelID: providerModelID, Weight: 100},
			&entity.ModelPrice{ID: "price_tnative_" + suffix, ProviderModelID: providerModelID, UpdateSource: "api"},
			&entity.ReservationBound{ProviderModelID: providerModelID, Protocol: protocol, MaxInputTokens: 4, MaxOutputTokens: 1, ETag: "bound_tnative_" + suffix, Evidence: "Controlled native maximum", Reason: "Native Team acceptance"},
		)
		for i, metric := range []string{pricing.Input, pricing.Output, pricing.CacheRead, pricing.CacheWrite} {
			create(&entity.PriceRate{ID: fmt.Sprintf("rate_tnative_%s_%d", suffix, i), ModelPriceID: "price_tnative_" + suffix, Metric: metric, Tier: pricing.Base, Unit: pricing.Unit, Currency: "USD", Amount: "1000000", Enabled: true})
		}
		modelIDs = append(modelIDs, modelID)
	}
	// A real Personal call activates journal coverage before new Team subjects.
	warmupBearer := "rx_" + strings.Repeat("n", 43)
	create(&entity.UserModelGrant{UserID: admin.User.ID, ModelID: modelIDs[0]}, &entity.APIKey{ID: "key_tnative_warmup", UserID: admin.User.ID, Name: "Coverage warmup", Prefix: "rx_masked", TokenHash: secret.SHA256Hex(warmupBearer), Status: entity.KeyActive}, &entity.APIKeyModel{KeyID: "key_tnative_warmup", ModelID: modelIDs[0]})
	router := fox.New()
	New(svc).RegisterRoutes(router)
	if err := svc.StartRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	spool := filepath.Join(t.TempDir(), "team-native.db")
	if err := svc.StartCallRecorder(ctx, spool); err != nil {
		t.Fatal(err)
	}
	warmup := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(teamNativeProtocolBody(protocols[0], false)))
	warmup.Header.Set("Content-Type", "application/json")
	warmup.Header.Set("Authorization", "Bearer "+warmupBearer)
	warmupResponse := httptest.NewRecorder()
	router.ServeHTTP(warmupResponse, warmup)
	expectStatus(t, warmupResponse, 200)
	if err := svc.FlushCallRecorder(ctx); err != nil {
		t.Fatal(err)
	}
	first, firstCookie, firstCSRF := createSystemStatusMember(t, svc, router, admin.User.ID, "team-native-first", nil)
	second, secondCookie, secondCSRF := createSystemStatusMember(t, svc, router, admin.User.ID, "team-native-second", nil)
	_, outsiderCookie, outsiderCSRF := createSystemStatusMember(t, svc, router, admin.User.ID, "team-native-outsider", []string{"teams.read_all", "teams.models.write", "teams.write"})
	const teamID = "tem_native_protocols"
	create(&entity.Team{ID: teamID, Name: "Native protocol Team", Status: entity.ResourceActive}, &entity.TeamMembership{ID: "tmm_tnative_owner", TeamID: teamID, UserID: admin.User.ID, Role: entity.TeamOwner, Status: entity.ResourceActive}, &entity.TeamMembership{ID: "tmm_tnative_first", TeamID: teamID, UserID: first.User.ID, Role: entity.TeamMember, Status: entity.ResourceActive}, &entity.TeamMembership{ID: "tmm_tnative_second", TeamID: teamID, UserID: second.User.ID, Role: entity.TeamMember, Status: entity.ResourceActive})
	for _, modelID := range modelIDs {
		create(&entity.TeamModelGrant{TeamID: teamID, ModelID: modelID})
	}
	if err := svc.RefreshRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	invoke := func(cookie *http.Cookie, csrf, method, path, body string, change func(*http.Request)) *httptest.ResponseRecorder {
		t.Helper()
		// Renew the real five-second lease explicitly; test speed must not stand
		// in for authorization or silently expire a long actual-driver matrix.
		if err := svc.RefreshRuntime(ctx); err != nil {
			t.Fatal(err)
		}
		bounded, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		req := httptest.NewRequestWithContext(bounded, method, "http://routex.test"+path, strings.NewReader(body))
		if cookie != nil {
			req.AddCookie(cookie)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", "http://routex.test")
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		if csrf != "" {
			req.Header.Set("X-CSRF-Token", csrf)
		}
		req.Header.Set("anthropic-version", "2023-06-01")
		if change != nil {
			change(req)
		}
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		return res
	}
	pathFor := func(protocol string, stream bool) string {
		return "/api/v1/teams/" + teamID + teamNativeProtocolPath(protocol, stream)
	}
	native := func(protocol string, stream bool) *httptest.ResponseRecorder {
		t.Helper()
		return invoke(firstCookie, firstCSRF, "POST", pathFor(protocol, stream), teamNativeProtocolBody(protocol, stream), nil)
	}
	readLimit := func(userID string) *service.LimitRecord {
		t.Helper()
		value, err := svc.GetTeamResourceLimit(ctx, admin.User.ID, teamID, userID)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	writeLimit := func(userID, patch string) *service.LimitRecord {
		t.Helper()
		before := readLimit(userID)
		var input service.TeamLimitInput
		if err := json.Unmarshal([]byte(patch), &input); err != nil {
			t.Fatal(err)
		}
		value, err := svc.SetTeamResourceLimit(ctx, admin.User.ID, teamID, userID, before.ETag, input)
		if err != nil || !value.Enforced {
			t.Fatal("native Team policy not applied", value, err)
		}
		return value
	}
	assertUsage := func(userID string, tokens int64, money string) *service.LimitRecord {
		t.Helper()
		if err := svc.FlushCallRecorder(ctx); err != nil {
			t.Fatal(err)
		}
		value := readLimit(userID)
		if value.QuotaUsage == nil || value.QuotaUsage.Month == nil || !value.QuotaUsage.Month.Covered || value.QuotaUsage.Month.TokensUsed != tokens || value.QuotaUsage.Month.TokensHeld != 0 || value.QuotaUsage.Month.TokensUnknown != 0 || value.QuotaUsage.Month.MoneyUnknown != 0 || value.QuotaUsage.Month.MoneyUsed["USD"] != money {
			t.Fatalf("native settlement is not exact: %+v", value.QuotaUsage)
		}
		return value
	}
	modelsPath := "/api/v1/teams/" + teamID + "/inference-models"
	expectStatus(t, invoke(outsiderCookie, "", "GET", modelsPath, "", nil), 403)
	expectStatus(t, invoke(firstCookie, "", "GET", modelsPath+"?key=foreign", "", nil), 400)
	expectStatus(t, invoke(firstCookie, "", "GET", strings.Replace(modelsPath, teamID, strings.ToUpper(teamID), 1), "", nil), 403)
	listed := invoke(firstCookie, "", "GET", modelsPath, "", nil)
	expectStatus(t, listed, 200)
	var discovery struct {
		Data []service.GatewayModel `json:"data"`
	}
	if json.Unmarshal(listed.Body.Bytes(), &discovery) != nil || len(discovery.Data) != 4 {
		t.Fatal("protocol-only discovery lost eligible models", listed.Body.String())
	}
	for _, item := range discovery.Data {
		suffix := strings.TrimPrefix(item.ID, "team-native-")
		index := slices.IndexFunc(protocols, func(protocol string) bool { return teamNativeSuffix(protocol) == suffix })
		if index < 0 || item.ModelID != modelIDs[index] || !slices.Equal(item.Protocols, []string{protocols[index]}) || item.AttachmentScope != "team" || item.PersonalAttachments || len(item.InputCapabilities) != 1 || len(item.InputCapabilities[protocols[index]]) != 0 {
			t.Fatal("Team discovery invented protocols or media", item)
		}
	}
	// Discovery reflects ready native routes, independently for each protocol.
	for _, protocol := range protocols {
		credentialID := "crd_tnative_" + teamNativeSuffix(protocol)
		if _, err := svc.SetCredentialEnabled(ctx, admin.User.ID, credentialID, false); err != nil {
			t.Fatal(err)
		}
		res := invoke(firstCookie, "", "GET", modelsPath, "", nil)
		expectStatus(t, res, 200)
		if json.Unmarshal(res.Body.Bytes(), &discovery) != nil || len(discovery.Data) != 3 || slices.ContainsFunc(discovery.Data, func(item service.GatewayModel) bool { return item.ID == "team-native-"+teamNativeSuffix(protocol) }) {
			t.Fatal("unready native route remained in Team discovery", protocol, res.Body.String())
		}
		before := dispatches.Load()
		denied := native(protocol, false)
		if denied.Code != 404 && denied.Code != 503 {
			t.Fatal("disabled native Credential remained callable", protocol, denied.Code)
		}
		if dispatches.Load() != before {
			t.Fatal("unready native route dispatched")
		}
		if _, err := svc.SetCredentialEnabled(ctx, admin.User.ID, credentialID, true); err != nil {
			t.Fatal(err)
		}
	}
	// Personal policy denial cannot contaminate the separate Team account pair.
	zero := int64(0)
	personalTarget := service.LimitTarget{Kind: "user", ID: first.User.ID}
	personalBefore, err := svc.GetResourceLimit(ctx, admin.User.ID, personalTarget)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetResourceLimit(ctx, admin.User.ID, personalTarget, personalBefore.ETag, service.LimitInput{Policy: limits.Policy{TokensMonth: &zero, RPM: &zero}, Reason: "Personal isolation"}); err != nil {
		t.Fatal(err)
	}
	writeLimit("", `{"tokens_month":100,"tpm":1000,"money_month":"100","currency":"USD","reason":"All native Team scopes"}`)
	writeLimit(first.User.ID, `{"tokens_month":40,"money_month":"100","currency":"USD","reason":"Independent member cap"}`)
	completedIDs := []string{}
	assertFact := func(res *httptest.ResponseRecorder, protocol, evidence string, stream, known bool) entity.CallRecord {
		t.Helper()
		if err := svc.FlushCallRecorder(ctx); err != nil {
			t.Fatal(err)
		}
		requestID := res.Header().Get("X-Request-ID")
		if requestID == "" {
			t.Fatal("native response omitted immutable request ID")
		}
		var call entity.CallRecord
		var attempts []entity.CallAttempt
		if err := db.Take(&call, "request_id = ?", requestID).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Where("request_id = ?", requestID).Find(&attempts).Error; err != nil {
			t.Fatal(err)
		}
		index := slices.Index(protocols, protocol)
		// Gemini retains terminal envelope completeness even when numeric usage
		// is absent. Known amounts are checked independently below.
		finalEnvelope := known || protocol == entity.ProtocolGeminiGenerateContent && !stream && mode.Load().(string) == "unknown_usage"
		if call.TeamID != teamID || call.UserID != first.User.ID || call.TeamMembershipID != "tmm_tnative_first" || call.KeyID != "" || call.ProjectID != "" || call.ModelID != modelIDs[index] || call.Protocol != protocol || call.Stream != stream || len(attempts) != 1 || attempts[0].NativeCompletionEvidence != evidence || attempts[0].CredentialID != "crd_tnative_"+teamNativeSuffix(protocol) || attempts[0].SnapshotID != call.SnapshotID || attempts[0].FinalUsageKnown != finalEnvelope {
			t.Fatalf("native protocol evidence/immutable subject mismatch: %+v %+v", call, attempts)
		}
		if known && (call.InputTokens == nil || *call.InputTokens != 4 || call.OutputTokens == nil || *call.OutputTokens != 1) {
			t.Fatal("terminal usage changed", call)
		}
		if evidence == "completed" && (call.Status != "success" || attempts[0].Status != "success") {
			t.Fatal("completion did not produce native success", call, attempts)
		}
		return call
	}
	for _, protocol := range protocols {
		for _, stream := range []bool{false, true} {
			before := dispatches.Load()
			response := native(protocol, stream)
			expectStatus(t, response, 200)
			if dispatches.Load() != before+1 {
				t.Fatal("one Team call performed multiple native dispatches")
			}
			call := assertFact(response, protocol, "completed", stream, true)
			completedIDs = append(completedIDs, call.RequestID)
		}
	}
	assertUsage("", 40, "40")
	pairBefore := assertUsage(first.User.ID, 40, "40")
	if pairBefore.RPMUsed == nil || *pairBefore.RPMUsed != 8 || readLimit("").RPMUsed == nil || *readLimit("").RPMUsed != 8 {
		t.Fatal("native protocols did not share exactly one admission per call")
	}
	beforeDispatch := dispatches.Load()
	for _, protocol := range protocols {
		expectStatus(t, native(protocol, false), 429)
	}
	if dispatches.Load() != beforeDispatch || *readLimit("").RPMUsed != 8 || *readLimit(first.User.ID).RPMUsed != 8 {
		t.Fatal("member refusal partly reserved aggregate allowance")
	}
	writeLimit(first.User.ID, `{"tokens_month":null,"reason":"Observe aggregate boundary"}`)
	writeLimit("", `{"tokens_month":40,"reason":"Exact aggregate exhaustion"}`)
	for _, protocol := range protocols {
		expectStatus(t, native(protocol, false), 429)
	}
	writeLimit("", `{"tokens_month":null,"tpm":0,"reason":"Native TPM exhaustion"}`)
	for _, protocol := range protocols {
		expectStatus(t, native(protocol, false), 429)
	}
	writeLimit("", `{"tpm":null,"money_month":"40.000000000000000001","currency":"USD","reason":"Exact aggregate money"}`)
	for _, protocol := range protocols {
		expectStatus(t, native(protocol, false), 429)
	}
	writeLimit("", `{"money_month":"100","currency":"USD","reason":"Observe member money boundary"}`)
	writeLimit(first.User.ID, `{"money_month":"40","currency":"USD","reason":"Exact member money"}`)
	for _, protocol := range protocols {
		expectStatus(t, native(protocol, false), 429)
	}
	if dispatches.Load() != beforeDispatch {
		t.Fatal("finite native token/TPM/money refusal dispatched")
	}
	assertUsage("", 40, "40")
	assertUsage(first.User.ID, 40, "40")
	writeLimit("", `{"money_month":null,"reason":"Observe native finality without finite reservation"}`)
	writeLimit(first.User.ID, `{"money_month":null,"reason":"Observe native evidence independently"}`)
	// Authentication, authority, selectors and media fail before upstream/storage.
	for _, protocol := range protocols {
		path, body := pathFor(protocol, false), teamNativeProtocolBody(protocol, false)
		for _, denied := range []struct {
			cookie *http.Cookie
			csrf   string
			change func(*http.Request)
			status int
		}{
			{nil, firstCSRF, nil, 401}, {firstCookie, "", nil, 403}, {outsiderCookie, outsiderCSRF, nil, 403},
			{firstCookie, firstCSRF, func(r *http.Request) { r.Header.Set("Origin", "https://untrusted.invalid") }, 403},
			{firstCookie, firstCSRF, func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "cross-site") }, 403},
			{firstCookie, firstCSRF, func(r *http.Request) { r.Header.Add("X-CSRF-Token", firstCSRF) }, 403},
			{firstCookie, firstCSRF, func(r *http.Request) { r.AddCookie(firstCookie) }, 401},
		} {
			expectStatus(t, invoke(denied.cookie, denied.csrf, "POST", path, body, denied.change), denied.status)
		}
		for _, header := range []string{"Authorization", "x-api-key", "x-goog-api-key", "anthropic-workspace-id", "anthropic-user-profile-id", "OpenAI-Organization", "OpenAI-Project", "X-Workspace-ID", "X-Project-ID", "X-Team-ID", "X-User-ID"} {
			res := invoke(firstCookie, firstCSRF, "POST", path, body, func(r *http.Request) { r.Header.Set(header, "forbidden-selector") })
			if res.Code != 400 && res.Code != 403 {
				t.Fatalf("native Key/selector header %s accepted: %d", header, res.Code)
			}
		}
		for _, query := range []string{"?key=foreign", "?api_key=foreign", "?team_id=other", "?alt=json"} {
			expectStatus(t, invoke(firstCookie, firstCSRF, "POST", path+query, body, nil), 400)
		}
		for _, field := range []string{"team_id", "project_id", "user_id", "key_id", "workspace", "session_id", "attachment_scope"} {
			injected := strings.TrimSuffix(body, "}") + `,"` + field + `":"foreign"}`
			expectStatus(t, invoke(firstCookie, firstCSRF, "POST", path, injected, nil), 400)
		}
		for _, media := range teamNativeRejectedBodies(protocol) {
			res := invoke(firstCookie, firstCSRF, "POST", path, media.body, nil)
			assertTeamNativeMediaError(t, protocol, media.nativeValidation, res)
		}
		oversized := strings.Repeat(" ", gatewayRequestLimit+1)
		expectStatus(t, invoke(firstCookie, firstCSRF, "POST", path, oversized, nil), 413)
	}
	if dispatches.Load() != beforeDispatch || storageReads.Load() != 0 {
		t.Fatal("invalid native input reached upstream or attachment lookup", dispatches.Load(), storageReads.Load())
	}
	// Team grants do not lend protocol-only models to the existing Personal Key.
	for _, protocol := range protocols[1:] {
		path := "/v1" + teamNativeProtocolPath(protocol, false)
		if protocol == entity.ProtocolGeminiGenerateContent {
			path = "/v1beta" + teamNativeProtocolPath(protocol, false)
		}
		request := httptest.NewRequest("POST", path, strings.NewReader(teamNativeProtocolBody(protocol, false)))
		request.Header.Set("Content-Type", "application/json")
		switch protocol {
		case entity.ProtocolOpenAIResponses:
			request.Header.Set("Authorization", "Bearer "+warmupBearer)
		case entity.ProtocolAnthropicMessages:
			request.Header.Set("x-api-key", warmupBearer)
			request.Header.Set("anthropic-version", "2023-06-01")
		case entity.ProtocolGeminiGenerateContent:
			request.Header.Set("x-goog-api-key", warmupBearer)
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != 403 && response.Code != 404 {
			t.Fatal("Team native access expanded existing Personal Key", protocol, response.Code)
		}
	}
	if dispatches.Load() != beforeDispatch {
		t.Fatal("Personal Key borrowed native Team grants")
	}
	// Exact route subsets have no cross-protocol fallback.
	for index, protocol := range protocols {
		other := protocols[(index+1)%len(protocols)]
		body := strings.ReplaceAll(teamNativeProtocolBody(protocol, false), "team-native-"+teamNativeSuffix(protocol), "team-native-"+teamNativeSuffix(other))
		path := pathFor(protocol, false)
		if protocol == entity.ProtocolGeminiGenerateContent {
			path = strings.ReplaceAll(path, "team-native-gemini", "team-native-"+teamNativeSuffix(other))
		}
		res := invoke(firstCookie, firstCSRF, "POST", path, body, nil)
		if res.Code != 404 && res.Code != 503 {
			t.Fatal("wrong protocol acquired another native route", res.Code)
		}
	}
	if dispatches.Load() != beforeDispatch {
		t.Fatal("unsupported protocol dispatched fallback")
	}
	// Valid text/tool JSON remains opaque even when it contains media-like names.
	for _, protocol := range protocols {
		res := invoke(firstCookie, firstCSRF, "POST", pathFor(protocol, false), teamNativeOpaqueBody(protocol), nil)
		expectStatus(t, res, 200)
		assertFact(res, protocol, "completed", false, true)
	}
	// Native terminal markers are independent of HTTP success and known usage.
	for _, protocol := range protocols {
		for _, evidence := range []string{"handoff", "blocked", "incomplete", "unknown"} {
			for _, stream := range []bool{false, true} {
				mode.Store(evidence)
				res := native(protocol, stream)
				expectStatus(t, res, 200)
				assertFact(res, protocol, evidence, stream, true)
			}
		}
	}
	for _, protocol := range protocols {
		mode.Store("truncated")
		res := native(protocol, true)
		expectStatus(t, res, 200)
		call := assertFact(res, protocol, "unknown", true, false)
		if call.Status == "success" {
			t.Fatal("truncated native stream became successful completion", protocol)
		}
	}
	for _, protocol := range protocols {
		mode.Store("unknown_usage")
		res := native(protocol, false)
		expectStatus(t, res, 200)
		call := assertFact(res, protocol, "completed", false, false)
		if call.InputTokens != nil || call.OutputTokens != nil {
			t.Fatal("missing native usage became zero", protocol, call)
		}
	}
	mode.Store("completed")
	// Captured identities cannot survive any revocation checkpoint or rejoin.
	identity, err := svc.RuntimeAuthenticateTeamSession(ctx, firstCookie.Value, teamID)
	if err != nil {
		t.Fatal(err)
	}
	denyCaptured := func(captured *service.TeamSessionIdentity) {
		t.Helper()
		if err := svc.RefreshRuntime(ctx); err != nil {
			t.Fatal(err)
		}
		before := dispatches.Load()
		for _, protocol := range protocols {
			body := []byte(teamNativeProtocolBody(protocol, false))
			var result *service.GatewayResult
			var err error
			switch protocol {
			case entity.ProtocolOpenAIChat:
				result, err = svc.TeamGatewayChat(ctx, captured, body, "req_captured_chat")
			case entity.ProtocolOpenAIResponses:
				result, err = svc.TeamGatewayResponses(ctx, captured, body, "req_captured_responses")
			case entity.ProtocolAnthropicMessages:
				result, err = svc.TeamGatewayMessages(ctx, captured, body, "req_captured_messages", service.MessagesHeaders{Version: "2023-06-01"})
			case entity.ProtocolGeminiGenerateContent:
				result, err = svc.TeamGatewayGemini(ctx, captured, body, "req_captured_gemini", "team-native-gemini", false)
			}
			if err == nil || result != nil && (result.Admitted || result.Response != nil) {
				t.Fatal("revoked captured identity reserved or dispatched", protocol, result, err)
			}
		}
		if dispatches.Load() != before {
			t.Fatal("captured revoked subject reached upstream")
		}
	}
	for _, mutate := range []func(*service.TeamSessionIdentity){
		func(value *service.TeamSessionIdentity) { value.SessionID = strings.ToUpper(value.SessionID) },
		func(value *service.TeamSessionIdentity) { value.UserID = strings.ToUpper(value.UserID) },
		func(value *service.TeamSessionIdentity) { value.TeamID = strings.ToUpper(value.TeamID) },
		func(value *service.TeamSessionIdentity) {
			value.TeamMembershipID = strings.ToUpper(value.TeamMembershipID)
		},
	} {
		alias := *identity
		mutate(&alias)
		denyCaptured(&alias)
	}
	for _, modelID := range modelIDs {
		if err := svc.ReauthorizeTeamSession(ctx, identity, strings.ToUpper(modelID)); err == nil {
			t.Fatal("model alias borrowed exact Team scope")
		}
	}
	// A disabled current Model must disappear from native discovery and admission.
	if err := db.Model(&entity.Model{}).Where("id IN ?", modelIDs).Update("status", entity.ResourceDisabled).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.RefreshRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	denyCaptured(identity)
	unavailableModels := invoke(firstCookie, "", "GET", modelsPath, "", nil)
	expectStatus(t, unavailableModels, 200)
	if json.Unmarshal(unavailableModels.Body.Bytes(), &discovery) != nil || len(discovery.Data) != 0 {
		t.Fatal("disabled native models remained discoverable")
	}
	if err := db.Model(&entity.Model{}).Where("id IN ?", modelIDs).Update("status", entity.ResourceActive).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.RefreshRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	ownerOnly := []service.TeamMemberInput{{UserID: admin.User.ID, Role: entity.TeamOwner, Status: entity.ResourceActive}, {UserID: second.User.ID, Role: entity.TeamMember, Status: entity.ResourceActive}}
	if _, err := svc.SetTeamMembers(ctx, admin.User.ID, teamID, ownerOnly); err != nil {
		t.Fatal(err)
	}
	denyCaptured(identity)
	withFirst := append(slices.Clone(ownerOnly), service.TeamMemberInput{UserID: first.User.ID, Role: entity.TeamMember, Status: entity.ResourceActive})
	if _, err := svc.SetTeamMembers(ctx, admin.User.ID, teamID, withFirst); err != nil {
		t.Fatal(err)
	}
	rejoined, err := svc.RuntimeAuthenticateTeamSession(ctx, firstCookie.Value, teamID)
	if err != nil || rejoined.TeamMembershipID == identity.TeamMembershipID {
		t.Fatal("rejoin restored captured relationship", rejoined, err)
	}
	denyCaptured(identity)
	if after := readLimit(first.User.ID); after.AccountID != pairBefore.AccountID {
		t.Fatal("rejoin reset stable member accounting identity")
	}
	// A fresh Session identity can call again; its fact retains the new generation.
	res := native(protocols[1], false)
	expectStatus(t, res, 200)
	if err := svc.FlushCallRecorder(ctx); err != nil {
		t.Fatal(err)
	}
	var rejoinedCall entity.CallRecord
	if err := db.Take(&rejoinedCall, "request_id = ?", res.Header().Get("X-Request-ID")).Error; err != nil || rejoinedCall.TeamMembershipID != rejoined.TeamMembershipID {
		t.Fatal("new call borrowed old membership attribution", rejoinedCall, err)
	}
	for _, requestID := range completedIDs {
		var historical entity.CallRecord
		if err := db.Take(&historical, "request_id = ?", requestID).Error; err != nil || historical.TeamMembershipID != identity.TeamMembershipID || historical.TeamID != teamID || historical.UserID != first.User.ID {
			t.Fatal("relationship replacement rewrote immutable native facts", historical, err)
		}
	}
	if _, err := svc.SetResourceModels(ctx, admin.User.ID, service.TeamResource, teamID, nil); err != nil {
		t.Fatal(err)
	}
	denyCaptured(rejoined)
	if _, err := svc.SetResourceModels(ctx, admin.User.ID, service.TeamResource, teamID, modelIDs); err != nil {
		t.Fatal(err)
	}
	disabled, active := entity.ResourceDisabled, entity.ResourceActive
	if _, err := svc.UpdateResource(ctx, admin.User.ID, service.TeamResource, teamID, service.ResourceUpdate{Status: &disabled}); err != nil {
		t.Fatal(err)
	}
	denyCaptured(rejoined)
	if _, err := svc.UpdateResource(ctx, admin.User.ID, service.TeamResource, teamID, service.ResourceUpdate{Status: &active}); err != nil {
		t.Fatal(err)
	}
	firstDisabled := true
	if _, err := svc.UpdateMember(ctx, admin.User.ID, first.User.ID, &firstDisabled, nil); err != nil {
		t.Fatal(err)
	}
	denyCaptured(rejoined)
	firstDisabled = false
	if _, err := svc.UpdateMember(ctx, admin.User.ID, first.User.ID, &firstDisabled, nil); err != nil {
		t.Fatal(err)
	}
	// The unaffected second Session survives a fresh Service/runtime/journal.
	beforeSecond := invoke(secondCookie, secondCSRF, "POST", pathFor(protocols[3], false), teamNativeProtocolBody(protocols[3], false), nil)
	expectStatus(t, beforeSecond, 200)
	if err := svc.FlushCallRecorder(ctx); err != nil {
		t.Fatal(err)
	}
	var beforeRestart entity.CallRecord
	if err := db.Take(&beforeRestart, "request_id = ?", beforeSecond.Header().Get("X-Request-ID")).Error; err != nil {
		t.Fatal(err)
	}
	svc.StopRuntime()
	if err := svc.StopCallRecorder(); err != nil {
		t.Fatal(err)
	}
	svc = makeService()
	router = fox.New()
	New(svc).RegisterRoutes(router)
	if err := svc.StartRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	if err := svc.StartCallRecorder(ctx, spool); err != nil {
		t.Fatal(err)
	}
	for _, protocol := range protocols {
		expectStatus(t, invoke(secondCookie, secondCSRF, "POST", pathFor(protocol, false), teamNativeProtocolBody(protocol, false), nil), 200)
	}
	if err := svc.FlushCallRecorder(ctx); err != nil {
		t.Fatal(err)
	}
	var afterRestart entity.CallRecord
	if err := db.Take(&afterRestart, "request_id = ?", beforeRestart.RequestID).Error; err != nil || !reflect.DeepEqual(beforeRestart, afterRestart) {
		t.Fatal("restart changed immutable native fact", err)
	}
	secondIdentity, err := svc.RuntimeAuthenticateTeamSession(ctx, secondCookie.Value, teamID)
	if err != nil {
		t.Fatal(err)
	}
	// Missing final usage survives restart as unknown, never zero allowance.
	writeLimit("", `{"tokens_month":100000,"reason":"Unknown native usage is not zero"}`)
	beforeUnknown := dispatches.Load()
	for _, protocol := range protocols {
		expectStatus(t, invoke(secondCookie, secondCSRF, "POST", pathFor(protocol, false), teamNativeProtocolBody(protocol, false), nil), 503)
	}
	if dispatches.Load() != beforeUnknown {
		t.Fatal("finite Team policy ignored unknown native usage")
	}
	if value := readLimit(""); value.QuotaUsage == nil || value.QuotaUsage.Month == nil || value.QuotaUsage.Month.TokensUnknown < 1 {
		t.Fatal("truncated native usage disappeared on restart")
	}
	if err := svc.RevokeAccountSession(ctx, second.User.ID, secondIdentity.SessionID); err != nil {
		t.Fatal(err)
	}
	denyCaptured(secondIdentity)
	for _, protocol := range protocols {
		expectStatus(t, invoke(secondCookie, secondCSRF, "POST", pathFor(protocol, false), teamNativeProtocolBody(protocol, false), nil), 401)
	}
	var personalGrants, personalKeys, personalCalls int64
	for _, query := range []*gorm.DB{db.Model(&entity.UserModelGrant{}).Where("user_id IN ?", []string{first.User.ID, second.User.ID}).Count(&personalGrants), db.Model(&entity.APIKey{}).Where("user_id IN ?", []string{first.User.ID, second.User.ID}).Count(&personalKeys), db.Model(&entity.CallRecord{}).Where("user_id IN ? AND team_id = ?", []string{first.User.ID, second.User.ID}, "").Count(&personalCalls)} {
		if query.Error != nil {
			t.Fatal(query.Error)
		}
	}
	if personalGrants != 0 || personalKeys != 0 || personalCalls != 0 {
		t.Fatal("native Team protocols created Personal access or call history")
	}
	personalAfter, err := svc.GetResourceLimit(ctx, admin.User.ID, personalTarget)
	if err != nil || personalAfter.Stored.TokensMonth == nil || *personalAfter.Stored.TokensMonth != 0 || personalAfter.Stored.RPM == nil || *personalAfter.Stored.RPM != 0 {
		t.Fatal("native Team accounting changed Personal policy", personalAfter, err)
	}
	if storageReads.Load() != 0 {
		t.Fatal("native text/tool JSON resolved a Personal attachment", storageReads.Load())
	}
	var keyScope []entity.APIKeyModel
	if err := db.Where("key_id = ?", "key_tnative_warmup").Find(&keyScope).Error; err != nil || len(keyScope) != 1 || keyScope[0].ModelID != modelIDs[0] {
		t.Fatal("Team native protocols changed existing Key ceiling", err)
	}
}

func assertTeamNativeMediaError(t *testing.T, protocol string, nativeValidation bool, res *httptest.ResponseRecorder) {
	t.Helper()
	expectStatus(t, res, http.StatusBadRequest)
	var body struct {
		Type      string `json:"type"`
		RequestID string `json:"request_id"`
		Error     struct {
			Type    string          `json:"type"`
			Code    json.RawMessage `json:"code"`
			Status  string          `json:"status"`
			Message string          `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil || body.Error.Message == "" {
		t.Fatalf("%s media refusal lost its error message: %s", protocol, res.Body.String())
	}
	if nativeValidation {
		if protocol != entity.ProtocolGeminiGenerateContent || !strings.Contains(body.Error.Message, "provider-owned resources are unsupported") {
			t.Fatalf("%s provider resource refusal lost its native validation: %s", protocol, res.Body.String())
		}
	} else if !strings.Contains(body.Error.Message, "text input only") {
		t.Fatalf("%s media refusal lost its Team text-only validation: %s", protocol, res.Body.String())
	}
	switch protocol {
	case entity.ProtocolAnthropicMessages:
		if body.Type != "error" || body.RequestID == "" || body.Error.Type != "invalid_request_error" || len(body.Error.Code) != 0 {
			t.Fatalf("Messages media refusal lost its native envelope: %s", res.Body.String())
		}
	case entity.ProtocolGeminiGenerateContent:
		if string(body.Error.Code) != "400" || body.Error.Status != "INVALID_ARGUMENT" || body.Error.Type != "" {
			t.Fatalf("Gemini media refusal lost its native envelope: %s", res.Body.String())
		}
	default:
		if body.Error.Type != "invalid_request_error" || string(body.Error.Code) != `"unsupported_input"` {
			t.Fatalf("%s media refusal lost its native envelope: %s", protocol, res.Body.String())
		}
	}
}

func teamNativeSuffix(protocol string) string {
	switch protocol {
	case entity.ProtocolOpenAIChat:
		return "chat"
	case entity.ProtocolOpenAIResponses:
		return "responses"
	case entity.ProtocolAnthropicMessages:
		return "messages"
	default:
		return "gemini"
	}
}

func teamNativeProtocolPath(protocol string, stream bool) string {
	switch protocol {
	case entity.ProtocolOpenAIChat:
		return "/chat/completions"
	case entity.ProtocolOpenAIResponses:
		return "/responses"
	case entity.ProtocolAnthropicMessages:
		return "/messages"
	default:
		if stream {
			return "/models/team-native-gemini:streamGenerateContent?alt=sse"
		}
		return "/models/team-native-gemini:generateContent"
	}
}

func teamNativeProtocolBody(protocol string, stream bool) string {
	if protocol == entity.ProtocolGeminiGenerateContent {
		return `{"contents":[{"role":"user","parts":[{"text":"Native Team text"}]}],"generationConfig":{"maxOutputTokens":1,"candidateCount":1}}`
	}
	base := `{"model":"team-native-` + teamNativeSuffix(protocol) + `",`
	switch protocol {
	case entity.ProtocolOpenAIChat:
		base += `"messages":[{"role":"user","content":"Native Team text"}],"max_completion_tokens":1`
	case entity.ProtocolOpenAIResponses:
		base += `"input":"Native Team text","max_output_tokens":1`
	case entity.ProtocolAnthropicMessages:
		base += `"messages":[{"role":"user","content":"Native Team text"}],"max_tokens":1`
	}
	if stream {
		base += `,"stream":true`
		if protocol == entity.ProtocolOpenAIChat {
			base += `,"stream_options":{"include_usage":true}`
		}
	}
	return base + `}`
}

func teamNativeProtocolResponse(protocol, mode string, stream bool) (string, string) {
	chatUsage := `{"prompt_tokens":4,"completion_tokens":1,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0}}`
	responsesUsage := `{"input_tokens":4,"output_tokens":1,"input_tokens_details":{"cached_tokens":0,"cache_write_tokens":0}}`
	messagesUsage := `{"input_tokens":4,"output_tokens":1,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}`
	geminiUsage := `{"promptTokenCount":4,"cachedContentTokenCount":0,"candidatesTokenCount":1,"thoughtsTokenCount":0,"totalTokenCount":5}`
	if mode == "unknown_usage" {
		chatUsage, responsesUsage, messagesUsage, geminiUsage = "null", "null", "null", "null"
	}
	contentType, raw := "application/json", ""
	if stream {
		contentType = "text/event-stream"
	}
	switch protocol {
	case entity.ProtocolOpenAIChat:
		finish, message := "stop", `{"role":"assistant","content":"Done"}`
		switch mode {
		case "handoff":
			finish, message = "tool_calls", `{"role":"assistant","content":null,"tool_calls":[{"id":"call_native","type":"function","function":{"name":"tool","arguments":"{}"}}]}`
		case "blocked":
			finish = "content_filter"
		case "incomplete":
			finish = "length"
		case "unknown":
			finish = "future_stop"
		}
		if !stream {
			raw = chatCompletionFixture(finish, message, chatUsage, false, 0)
		} else {
			partial := strings.Replace(chatCompletionFixture("", `{"role":"assistant","content":"Done"}`, "null", true, 0), `"finish_reason":""`, `"finish_reason":null`, 1)
			raw = "data: " + partial + "\n\n"
			if mode != "truncated" {
				raw += "data: " + chatCompletionFixture(finish, message, "null", true, 0) + "\n\ndata: {\"object\":\"chat.completion.chunk\",\"choices\":[],\"usage\":" + chatUsage + "}\n\ndata: [DONE]\n\n"
			}
		}
	case entity.ProtocolOpenAIResponses:
		status, output := "completed", responsesTextOutput
		switch mode {
		case "handoff":
			output = `[{"type":"function_call","name":"tool","call_id":"call_native","arguments":"{}","status":"completed"}]`
		case "blocked":
			output = `[{"type":"message","role":"assistant","status":"completed","content":[{"type":"refusal","refusal":"Cannot answer"}]}]`
		case "incomplete":
			status = "incomplete"
		case "unknown":
			output = `[]`
		}
		raw = responseOutputFixture(status, responsesUsage, output)
		if stream {
			if mode == "truncated" {
				raw = responseEventFixture("response.created", "in_progress", "null", 0)
			} else {
				raw = fmt.Sprintf("event: response.%s\ndata: {\"type\":\"response.%s\",\"sequence_number\":0,\"response\":%s}\n\n", status, status, raw)
			}
		}
	case entity.ProtocolAnthropicMessages:
		reason := "end_turn"
		switch mode {
		case "handoff":
			reason = "tool_use"
		case "blocked":
			reason = "refusal"
		case "incomplete":
			reason = "max_tokens"
		case "unknown":
			reason = "future_stop"
		}
		raw = messagesFixture(reason, messagesUsage)
		if stream {
			start := strings.Replace(messagesFixture("", messagesUsage), `"content":[{"type":"text","text":"hello"}]`, `"content":[]`, 1)
			raw = "event: message_start\ndata: {\"type\":\"message_start\",\"message\":" + start + "}\n\n"
			if mode != "truncated" {
				raw += fmt.Sprintf("event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":%q,\"stop_sequence\":null},\"usage\":{\"output_tokens\":1}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n", reason)
			}
		}
	case entity.ProtocolGeminiGenerateContent:
		finish, parts := "STOP", `[{"text":"Done"}]`
		switch mode {
		case "handoff":
			parts = `[{"functionCall":{"name":"tool","args":{}}}]`
		case "blocked":
			finish = "SAFETY"
		case "incomplete":
			finish = "MAX_TOKENS"
		case "unknown":
			finish = "FUTURE_STOP"
		}
		raw = fmt.Sprintf(`{"candidates":[{"index":0,"content":{"parts":%s},"finishReason":%q}],"modelVersion":"private-model","usageMetadata":%s}`, parts, finish, geminiUsage)
		if stream {
			if mode == "truncated" {
				raw = `{"candidates":[{"index":0,"content":{"parts":[{"text":"partial"}]}}]}`
			}
			raw = "data: " + raw + "\n\n"
		}
	}
	return contentType, raw
}

type teamNativeRejectedInput struct {
	body             string
	nativeValidation bool
}

func teamNativeRejectedBodies(protocol string) []teamNativeRejectedInput {
	teamOnly := func(bodies ...string) []teamNativeRejectedInput {
		inputs := make([]teamNativeRejectedInput, 0, len(bodies))
		for _, body := range bodies {
			inputs = append(inputs, teamNativeRejectedInput{body: body})
		}
		return inputs
	}
	model := `"model":"team-native-` + teamNativeSuffix(protocol) + `",`
	switch protocol {
	case entity.ProtocolOpenAIChat:
		return teamOnly(`{`+model+`"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"routex://attachments/obj_native"}}]}]}`, `{`+model+`"messages":[{"role":"user","content":"hello","audio":{"id":"audio_native"}}]}`)
	case entity.ProtocolOpenAIResponses:
		return teamOnly(`{`+model+`"input":[{"role":"user","content":[{"type":"input_file","file_url":"routex://attachments/obj_native"}]}]}`, `{`+model+`"input":[{"type":"function_call_output","call_id":"call_native","output":[{"type":"input_image","image_url":"https://untrusted.invalid/image"}]}]}`, `{`+model+`"input":"hello","tools":[{"type":"image_generation"}]}`)
	case entity.ProtocolAnthropicMessages:
		bodies := []string{`{` + model + `"messages":[{"role":"user","content":[{"type":"tool_result","tool_use_id":"tool_native","content":[{"type":"document","source":{"type":"url","url":"routex://attachments/obj_native"}}]}]}],"max_tokens":1}`, `{` + model + `"messages":[{"role":"user","content":"hello"}],"system":[{"type":"image","source":{"type":"url","url":"https://untrusted.invalid/image"}}],"max_tokens":1}`}
		nested := `[{"type":"text","text":"hello"}]`
		for range 9 {
			nested = `[{"type":"tool_result","tool_use_id":"tool_native","content":` + nested + `}]`
		}
		return teamOnly(append(bodies, `{`+model+`"messages":[{"role":"user","content":`+nested+`}],"max_tokens":1}`)...)
	default:
		return []teamNativeRejectedInput{
			{body: `{"contents":[{"parts":[{"inlineData":{"mimeType":"image/png","data":"AA=="}}]}]}`},
			{body: `{"contents":[{"parts":[{"functionResponse":{"name":"tool","response":{},"parts":[{"inlineData":{"mimeType":"image/png","data":"AA=="}}]}}]}]}`},
			// Provider file references fail the native parser before Team media validation.
			{body: `{"contents":[{"parts":[{"functionResponse":{"name":"tool","response":{},"parts":[{"fileData":{"mimeType":"application/pdf","fileUri":"routex://attachments/obj_native"}}]}}]}]}`, nativeValidation: true},
			{body: `{"contents":[{"parts":[{"text":"hello"}]}],"generationConfig":{"responseModalities":["AUDIO"]}}`},
			{body: `{"contents":[{"parts":[{"text":"hello"}]}],"generationConfig":{"candidateCount":2}}`},
		}
	}
}

func teamNativeOpaqueBody(protocol string) string {
	opaque := `{"type":"image","fileUri":"routex://attachments/obj_native","url":"https://untrusted.invalid/image"}`
	model := `"model":"team-native-` + teamNativeSuffix(protocol) + `",`
	switch protocol {
	case entity.ProtocolOpenAIChat:
		return `{` + model + `"messages":[{"role":"user","content":"routex://attachments/obj_native"}],"tools":[{"type":"function","function":{"name":"tool","parameters":` + opaque + `}}],"max_completion_tokens":1}`
	case entity.ProtocolOpenAIResponses:
		return `{` + model + `"input":[{"type":"function_call","name":"tool","call_id":"call_native","arguments":"image_url"},{"type":"function_call_output","call_id":"call_native","output":"routex://attachments/obj_native"}],"tools":[{"type":"function","name":"tool","parameters":` + opaque + `}],"max_output_tokens":1}`
	case entity.ProtocolAnthropicMessages:
		return `{` + model + `"messages":[{"role":"assistant","content":[{"type":"tool_use","id":"tool_native","name":"tool","input":` + opaque + `}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"tool_native","content":"routex://attachments/obj_native"}]}],"max_tokens":1}`
	default:
		return `{"contents":[{"role":"user","parts":[{"functionResponse":{"name":"tool","response":` + opaque + `}}]}],"generationConfig":{"maxOutputTokens":1}}`
	}
}

// Validate upstream fixtures without a database before using their facts as
// evidence in both actual-driver lifecycles.
func TestTeamNativeProtocolFixtureFinality(t *testing.T) {
	for _, protocol := range []string{entity.ProtocolOpenAIChat, entity.ProtocolOpenAIResponses, entity.ProtocolAnthropicMessages, entity.ProtocolGeminiGenerateContent} {
		for _, evidence := range []string{"completed", "handoff", "blocked", "incomplete", "unknown", "truncated", "unknown_usage"} {
			for _, stream := range []bool{false, true} {
				if evidence == "truncated" && !stream || evidence == "unknown_usage" && stream {
					continue
				}
				t.Run(protocol+"/"+evidence+fmt.Sprint(stream), func(t *testing.T) {
					_, raw := teamNativeProtocolResponse(protocol, evidence, stream)
					var observation gatewayObservation
					var err error
					writer := httptest.NewRecorder()
					if stream {
						switch protocol {
						case entity.ProtocolOpenAIChat:
							observation, err = proxyGatewayStream(context.Background(), writer, strings.NewReader(raw), "public", 1)
						case entity.ProtocolOpenAIResponses:
							observation, err = proxyResponsesStream(context.Background(), writer, strings.NewReader(raw), "public")
						case entity.ProtocolAnthropicMessages:
							observation, err = proxyMessagesStream(context.Background(), writer, strings.NewReader(raw), "public", "req_fixture")
						case entity.ProtocolGeminiGenerateContent:
							observation, err = proxyGeminiStream(context.Background(), writer, strings.NewReader(raw), "public", "req_fixture", 1)
						}
					} else {
						switch protocol {
						case entity.ProtocolOpenAIChat:
							observation = parseGatewayUsage([]byte(raw))
						case entity.ProtocolOpenAIResponses:
							var encoded []byte
							var status string
							encoded, status, err = rewriteResponsesObject([]byte(raw), "public", false)
							observation = observeGatewayUsage(service.ParseResponsesUsage(encoded))
							observation.NativeCompletionEvidence = responsesCompletionEvidence(encoded, status)
						case entity.ProtocolAnthropicMessages:
							var encoded []byte
							encoded, err = rewriteMessagesObject([]byte(raw), "public", true)
							observation = observeGatewayUsage(service.ParseMessagesUsage(encoded, true))
							observation.NativeCompletionEvidence = observedMessagesCompletion(encoded)
						case entity.ProtocolGeminiGenerateContent:
							state := geminiStreamState{expected: 1}
							var encoded []byte
							encoded, err = state.object([]byte(raw), "public", "req_fixture")
							observation = observeGatewayUsage(service.ParseGeminiUsage(encoded, true))
							observation.NativeCompletionEvidence = state.completionEvidence()
						}
					}
					if evidence == "truncated" {
						if err == nil || observation.Complete || observation.NativeCompletionEvidence != "unknown" {
							t.Fatal("fixture fabricated truncated finality", observation, err)
						}
						return
					}
					if evidence == "unknown_usage" {
						finalEnvelope := protocol == entity.ProtocolGeminiGenerateContent
						if err != nil || observation.Complete != finalEnvelope || observation.NativeCompletionEvidence != "completed" || observation.Input != nil || observation.Output != nil {
							t.Fatal("fixture promoted missing usage or lost native completion", observation, err)
						}
						return
					}
					expectedIncomplete := protocol == entity.ProtocolOpenAIResponses && evidence == "incomplete" && stream
					if (err != nil) != expectedIncomplete || observation.NativeCompletionEvidence != evidence || !observation.Complete || observation.Input == nil || *observation.Input != 4 || observation.Output == nil || *observation.Output != 1 {
						t.Fatal("fixture does not express expected native evidence/usage", observation, err)
					}
				})
			}
		}
	}
}
