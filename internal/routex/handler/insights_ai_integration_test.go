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
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/limits"
	"github.com/miclle/routex/pkg/secretstore"
	"gorm.io/gorm"
)

type insightsAIIntegrationBarrier struct {
	entered, release, cancelled             chan struct{}
	enteredOnce, releaseOnce, cancelledOnce sync.Once
}

func (b *insightsAIIntegrationBarrier) unblock() { b.releaseOnce.Do(func() { close(b.release) }) }

// The root registry supplies a fresh real database for each supported driver.
// All native work is performed by a local controlled provider, never a paid service.
func testInsightsAIIntegration(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	db = db.WithContext(ctx)
	var dispatches atomic.Int64
	var mode atomic.Value
	mode.Store("completed")
	var held atomic.Pointer[insightsAIIntegrationBarrier]
	var forbidden atomic.Value
	forbidden.Store([]string{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet && r.URL.Path == "/v1/models" {
			_, _ = io.WriteString(w, "{\"data\":[{\"id\":\"native-insights\"}]}")
			return
		}
		if r.Method != http.MethodPost || r.URL.Path != "/v1/chat/completions" {
			w.WriteHeader(404)
			return
		}
		dispatches.Add(1)
		var body map[string]json.RawMessage
		if json.NewDecoder(r.Body).Decode(&body) != nil || len(body) != 4 || string(body["model"]) != "\"native-insights\"" || string(body["stream"]) != "false" || string(body["max_completion_tokens"]) != "2048" || r.Header.Get("Cookie") != "" || r.Header.Get("X-CSRF-Token") != "" || r.Header.Get("Authorization") != "Bearer insights-provider-secret" {
			t.Error("fixed native body or private credential boundary changed")
			w.WriteHeader(400)
			return
		}
		var messages []struct{ Role, Content string }
		if json.Unmarshal(body["messages"], &messages) != nil || len(messages) != 2 || messages[0].Role != "system" || messages[1].Role != "user" {
			t.Error("fixed two-message analysis grammar changed")
			w.WriteHeader(400)
			return
		}
		for _, private := range forbidden.Load().([]string) {
			if strings.Contains(messages[1].Content, private) {
				t.Error("private identity entered aggregate projection", private)
			}
		}
		if b := held.Load(); b != nil {
			b.enteredOnce.Do(func() { close(b.entered) })
			select {
			case <-b.release:
			case <-r.Context().Done():
				if b.cancelled != nil {
					b.cancelledOnce.Do(func() { close(b.cancelled) })
				}
				return
			}
		}
		content := "\"M1 has recorded tokens; unknown coverage remains separate.\""
		if mode.Load().(string) == "invalid_content" {
			content = "null"
		}
		_, _ = io.WriteString(w, chatCompletionFixture("stop", "{\"role\":\"assistant\",\"content\":"+content+"}", "{\"prompt_tokens\":10,\"completion_tokens\":4,\"prompt_tokens_details\":{\"cached_tokens\":0,\"cache_write_tokens\":0}}", false, 0))
	}))
	defer upstream.Close()
	defer func() {
		if b := held.Load(); b != nil {
			b.unblock()
		}
	}()
	store, err := secretstore.New(bytes.Repeat([]byte{108}, 32))
	if err != nil {
		t.Fatal(err)
	}
	svc, err := service.New(ctx, db, service.WithCredentialStorage(store), service.WithUpstreamPolicy(true), service.WithRuntimeRefreshInterval(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		svc.StopRuntime()
		if err := svc.StopCallRecorder(); err != nil {
			t.Error(err)
		}
	}()
	router := fox.New()
	New(svc).RegisterRoutes(router)
	setup := identityRequest(router, http.MethodPost, "/api/v1/setup", "{\"email\":\"insights-admin@example.invalid\",\"password\":\"test-only-insights-password\",\"name\":\"Insights administrator\"}", nil, "")
	expectStatus(t, setup, 201)
	admin, cookie := readIdentity(t, setup)
	provider, err := svc.CreateProvider(ctx, admin.User.ID, "Private Insights Provider", service.CreateConnectionInput{Name: "Private Insights Connection", BaseURL: upstream.URL + "/v1", Protocol: entity.ProtocolOpenAIChat, CredentialName: "Private Insights Credential", Secret: "insights-provider-secret"})
	if err != nil {
		t.Fatal(err)
	}
	credentialID := provider.Connections[0].Credentials[0].ID
	if _, err := svc.VerifyCredential(ctx, admin.User.ID, credentialID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetCredentialEnabled(ctx, admin.User.ID, credentialID, true); err != nil {
		t.Fatal(err)
	}
	var pm entity.ProviderModel
	if err := db.Where("connection_id = ?", provider.Connections[0].Connection.ID).First(&pm).Error; err != nil {
		t.Fatal(err)
	}
	personal, err := svc.CreateModel(ctx, admin.User.ID, "private-analysis-model", pm.ID)
	if err != nil {
		t.Fatal(err)
	}
	teamOnly, err := svc.CreateModel(ctx, admin.User.ID, "team-only-analysis-model", pm.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, model := range []*service.ModelCatalog{personal, teamOnly} {
		if _, err := svc.SetModelWeights(ctx, admin.User.ID, model.Model.ID, []service.ModelWeight{{BindingID: model.Bindings[0].Binding.ID, Weight: 100}}); err != nil {
			t.Fatal(err)
		}
	}
	create := func(t *testing.T, row any) {
		t.Helper()
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	// Legacy CreateModel explicitly grants its creator. Replace that complete
	// fixture set so the second route is genuinely Team-only.
	removed := db.Where("user_id = ? AND model_id IN ?", admin.User.ID, []string{personal.Model.ID, teamOnly.Model.ID}).Delete(&entity.UserModelGrant{})
	if removed.Error != nil || removed.RowsAffected != 2 {
		t.Fatal("creator grant fixture did not match exact source", removed.Error, removed.RowsAffected)
	}
	create(t, &entity.UserModelGrant{UserID: admin.User.ID, ModelID: personal.Model.ID})
	if err := db.Model(&entity.User{}).Where("id = ?", admin.User.ID).UpdateColumn("personal_grant_revision", strings.Repeat("a", 64)).Error; err != nil {
		t.Fatal(err)
	}
	create(t, &entity.Team{ID: "tem_insights_only", Name: "Private analysis Team", Status: entity.ResourceActive})
	create(t, &entity.TeamMembership{ID: "tmm_insights_only", TeamID: "tem_insights_only", UserID: admin.User.ID, Role: entity.TeamOwner, Status: entity.ResourceActive})
	create(t, &entity.TeamModelGrant{TeamID: "tem_insights_only", ModelID: teamOnly.Model.ID})
	if err := svc.StartRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	if err := svc.StartCallRecorder(ctx, filepath.Join(t.TempDir(), "insights-call-buffer.db")); err != nil {
		t.Fatal(err)
	}
	forbidden.Store([]string{admin.User.ID, personal.Model.ID, teamOnly.Model.ID, "private-analysis-model", "team-only-analysis-model", "Private Insights Provider", "Private Insights Connection", "insights-provider-secret"})
	input := service.InsightsAIInput{Report: "tokens_by_model", ModelID: personal.Model.ID, From: time.Now().UTC().Add(-time.Hour).Format(time.RFC3339), To: time.Now().UTC().Add(time.Hour).Format(time.RFC3339), Timezone: "UTC", Language: "en"}
	do := func(method, path, body string, csrf string, etag ...string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "http://routex.test"+path, strings.NewReader(body)).WithContext(ctx)
		req.AddCookie(cookie)
		if len(etag) != 0 {
			req.Header.Set("If-Match", "\""+etag[0]+"\"")
		}
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		if csrf != "" {
			req.Header.Set("X-CSRF-Token", csrf)
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		return response
	}
	analyze := func(value service.InsightsAIInput, csrf string) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(value)
		return do(http.MethodPost, "/api/v1/admin/insights/analyze", string(raw), csrf)
	}
	target := service.LimitTarget{Kind: "user", ID: admin.User.ID}
	expectedTokens := int64(0)
	recorded := func(t *testing.T, response *httptest.ResponseRecorder, wantStatus string) {
		t.Helper()
		if err := svc.FlushCallRecorder(ctx); err != nil {
			t.Fatal(err)
		}
		requestID := response.Header().Get("X-Request-ID")
		var fact entity.CallRecord
		if err := db.Where("request_id = ?", requestID).First(&fact).Error; err != nil {
			t.Fatal("actual analysis call not persisted", err)
		}
		if fact.UserID != admin.User.ID || fact.KeyID != "" || fact.TeamID != "" || fact.ProjectID != "" || fact.ModelID != personal.Model.ID || fact.Protocol != entity.ProtocolOpenAIChat || fact.Status != wantStatus || fact.InputTokens == nil || *fact.InputTokens != 10 || fact.OutputTokens == nil || *fact.OutputTokens != 4 {
			t.Fatal("analysis lost true personal native attribution/economics", fact)
		}
		var attempts []entity.CallAttempt
		if err := db.Where("request_id = ?", requestID).Find(&attempts).Error; err != nil {
			t.Fatal(err)
		}
		if len(attempts) != 1 || attempts[0].CredentialID != credentialID || attempts[0].SnapshotID == "" || attempts[0].SnapshotID != fact.SnapshotID || attempts[0].NativeCompletionEvidence != "completed" || attempts[0].Status != wantStatus || !attempts[0].FinalUsageKnown {
			t.Fatal("durable attempt lost exact route/native proof", attempts)
		}
		expectedTokens += 14
		policy, err := svc.GetResourceLimit(ctx, admin.User.ID, target)
		if err != nil {
			t.Fatal(err)
		}
		if policy.QuotaUsage == nil || policy.QuotaUsage.Month == nil || policy.QuotaUsage.Month.TokensUsed != expectedTokens || policy.QuotaUsage.Month.TokensHeld != 0 || policy.QuotaUsage.Month.TokensUnknown != 0 {
			t.Fatal("native usage did not settle exact personal account", policy.QuotaUsage)
		}
	}
	denyBeforeDispatch := func(t *testing.T, response *httptest.ResponseRecorder, status int, before int64) {
		t.Helper()
		expectStatus(t, response, status)
		if dispatches.Load() != before {
			t.Fatal("denied request reached provider")
		}
	}
	permission := entity.RolePermission{RoleID: "rol_admin", Permission: "calls.read_all"}
	revokePermission := func(t *testing.T) {
		t.Helper()
		result := db.Where("role_id = ? AND permission = ?", permission.RoleID, permission.Permission).Delete(&entity.RolePermission{})
		if result.Error != nil || result.RowsAffected != 1 {
			t.Fatal("failed to revoke exact aggregate authority", result.Error, result.RowsAffected)
		}
	}
	restorePermission := func(t *testing.T) { t.Helper(); create(t, &permission) }
	runHeld := func(t *testing.T, revoke func(*testing.T), status int) *httptest.ResponseRecorder {
		t.Helper()
		b := &insightsAIIntegrationBarrier{entered: make(chan struct{}), release: make(chan struct{})}
		held.Store(b)
		done := make(chan *httptest.ResponseRecorder, 1)
		go func() { done <- analyze(input, admin.CSRFToken) }()
		joined := false
		defer func() {
			b.unblock()
			held.Store(nil)
			if !joined {
				select {
				case <-done:
				case <-time.After(5 * time.Second):
					t.Error("held analysis worker failed bounded join")
				}
			}
		}()
		select {
		case <-b.entered:
		case response := <-done:
			joined = true
			t.Fatalf("analysis ended before controlled barrier: %d", response.Code)
		case <-ctx.Done():
			t.Fatal("analysis never reached controlled barrier")
		}
		revoke(t)
		b.unblock()
		select {
		case response := <-done:
			joined = true
			expectStatus(t, response, status)
			if strings.Contains(response.Body.String(), "recorded tokens") || strings.Contains(response.Body.String(), personal.Model.ID) {
				t.Fatal("revoked actor received private analysis")
			}
			return response
		case <-ctx.Done():
			t.Fatal("held analysis failed to join")
		}
		return nil
	}
	t.Run("session_csrf_and_personal_picker", func(t *testing.T) {
		before := dispatches.Load()
		denyBeforeDispatch(t, analyze(input, ""), 403, before)
		response := do(http.MethodGet, "/api/v1/admin/insights/models", "", "")
		expectStatus(t, response, 200)
		var models service.InsightsAIModels
		if json.Unmarshal(response.Body.Bytes(), &models) != nil || len(models.Items) != 1 || models.Items[0].ModelID != personal.Model.ID || models.Items[0].Name != "private-analysis-model" {
			t.Fatal("Team-only grant broadened personal picker", response.Body.String())
		}
		var keys int64
		if err := db.Model(&entity.APIKey{}).Count(&keys).Error; err != nil || keys != 0 {
			t.Fatal("analysis created or required an API Key", err, keys)
		}
	})
	t.Run("aggregate_permission_and_exact_selected_model", func(t *testing.T) {
		before := dispatches.Load()
		revokePermission(t)
		response := analyze(input, admin.CSRFToken)
		restorePermission(t)
		denyBeforeDispatch(t, response, 403, before)
		other := input
		other.ModelID = teamOnly.Model.ID
		denyBeforeDispatch(t, analyze(other, admin.CSRFToken), 404, before)
		other.ModelID = strings.ToUpper(input.ModelID)
		denyBeforeDispatch(t, analyze(other, admin.CSRFToken), 400, before)
	})
	t.Run("native_persisted_personal_attribution_and_settlement", func(t *testing.T) {
		response := analyze(input, admin.CSRFToken)
		expectStatus(t, response, 200)
		var view service.InsightsAIResponse
		if json.Unmarshal(response.Body.Bytes(), &view) != nil || view.Completion != "completed" || view.Text == nil || view.Capture.From != input.From || view.Capture.To != input.To || view.Capture.Timezone != input.Timezone || len(view.Labels) != 0 {
			t.Fatal("fixed empty capture response changed", response.Body.String())
		}
		recorded(t, response, "success")
	})
	t.Run("malformed_content_retains_observed_usage", func(t *testing.T) {
		mode.Store("invalid_content")
		defer mode.Store("completed")
		response := analyze(input, admin.CSRFToken)
		expectStatus(t, response, 502)
		recorded(t, response, "error")
	})
	t.Run("zero_quota_denies_before_dispatch", func(t *testing.T) {
		policy, err := svc.GetResourceLimit(ctx, admin.User.ID, target)
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			current, err := svc.GetResourceLimit(ctx, admin.User.ID, target)
			if err != nil {
				t.Error("read controlled policy for restoration", err)
				return
			}
			if _, err := svc.SetResourceLimit(ctx, admin.User.ID, target, current.ETag, service.LimitInput{Policy: policy.Stored, Reason: "Restore controlled policy"}); err != nil {
				t.Error("restore controlled policy", err)
			}
		}()
		zero := int64(0)
		if _, err := svc.SetResourceLimit(ctx, admin.User.ID, target, policy.ETag, service.LimitInput{Policy: limits.Policy{RPM: &zero}, Reason: "Controlled zero quota denial"}); err != nil {
			t.Fatal(err)
		}
		before := dispatches.Load()
		denyBeforeDispatch(t, analyze(input, admin.CSRFToken), 429, before)
		boundPath := "/api/v1/admin/provider-models/" + pm.ID + "/reservation-bound"
		capacityReview := decodeCatalogResponse[service.ReservationBoundRecord](t, do(http.MethodGet, boundPath, "", ""), 200)
		capacityBody, err := json.Marshal(service.ReservationBoundInput{MaxInputTokens: 100, MaxOutputTokens: 2048, Evidence: "Controlled provider emits ten input and four output tokens", Reason: "Exercise finite personal token admission"})
		if err != nil {
			t.Fatal(err)
		}
		capacity := decodeCatalogResponse[service.ReservationBoundRecord](t, do(http.MethodPut, boundPath, string(capacityBody), admin.CSRFToken, capacityReview.ETag), 200)
		if !capacity.Configured || !capacity.TransportCurrent || capacity.ProviderModelID != pm.ID || capacity.Protocol != entity.ProtocolOpenAIChat || capacity.MaxInputTokens != 100 || capacity.MaxOutputTokens != 2048 {
			t.Fatal("finite token denial lacks reviewed current capacity", capacity)
		}
		current, err := svc.GetResourceLimit(ctx, admin.User.ID, target)
		if err != nil {
			t.Fatal(err)
		}
		finite := policy.Stored
		finite.Tokens5H = &zero
		if _, err := svc.SetResourceLimit(ctx, admin.User.ID, target, current.ETag, service.LimitInput{Policy: finite, Reason: "Controlled exhausted personal token budget"}); err != nil {
			t.Fatal(err)
		}
		before = dispatches.Load()
		denied := analyze(input, admin.CSRFToken)
		denyBeforeDispatch(t, denied, 429, before)
		var failure ErrorResponse
		if json.Unmarshal(denied.Body.Bytes(), &failure) != nil || failure.Code != http.StatusTooManyRequests {
			t.Fatal("finite token denial was not an exhausted quota", denied.Body.String())
		}
		if err := svc.FlushCallRecorder(ctx); err != nil {
			t.Fatal(err)
		}
		usage, err := svc.GetResourceLimit(ctx, admin.User.ID, target)
		if err != nil || usage.QuotaUsage == nil || usage.QuotaUsage.Month == nil || usage.QuotaUsage.Active == nil || usage.Stored.RPM != nil || usage.Stored.Tokens5H == nil || *usage.Stored.Tokens5H != 0 || usage.QuotaUsage.Month.TokensUsed != expectedTokens || usage.QuotaUsage.Month.TokensHeld != 0 || usage.QuotaUsage.Month.TokensUnknown != 0 || usage.QuotaUsage.Active.TokensHeld != 0 || usage.QuotaUsage.Active.TokensUnknown != 0 {
			t.Fatal("rejected finite personal request changed usage or retained a hold", usage, err)
		}
	})
	t.Run("sql_actor_birth_before_dispatch", func(t *testing.T) {
		var actor entity.User
		if err := db.Where("id = ?", admin.User.ID).First(&actor).Error; err != nil {
			t.Fatal(err)
		}
		before := dispatches.Load()
		if err := db.Model(&entity.User{}).Where("id = ?", actor.ID).UpdateColumn("created_at", actor.CreatedAt.Add(time.Second)).Error; err != nil {
			t.Fatal(err)
		}
		response := analyze(input, admin.CSRFToken)
		if err := db.Model(&entity.User{}).Where("id = ?", actor.ID).UpdateColumn("created_at", actor.CreatedAt).Error; err != nil {
			t.Fatal(err)
		}
		denyBeforeDispatch(t, response, 401, before)
	})
	t.Run("late_permission_revocation", func(t *testing.T) {
		response := runHeld(t, revokePermission, 403)
		restorePermission(t)
		recorded(t, response, "success")
	})
	t.Run("late_session_revocation", func(t *testing.T) {
		response := runHeld(t, func(t *testing.T) {
			expectStatus(t, do(http.MethodPost, "/api/v1/auth/logout", "", admin.CSRFToken), 204)
		}, 401)
		recorded(t, response, "success")
		before := dispatches.Load()
		denyBeforeDispatch(t, analyze(input, admin.CSRFToken), 401, before)

		// A fresh Session owns this separate call; the earlier revoked Session stays denied.
		login := identityRequest(router, http.MethodPost, "/api/v1/auth/login", "{\"email\":\"insights-admin@example.invalid\",\"password\":\"test-only-insights-password\"}", nil, "")
		expectStatus(t, login, 200)
		fresh, freshCookie := readIdentity(t, login)
		if fresh.User.ID != admin.User.ID {
			t.Fatal("cancellation fixture changed the exact personal actor")
		}
		requestCtx, cancelRequest := context.WithCancel(ctx)
		defer cancelRequest()
		b := &insightsAIIntegrationBarrier{entered: make(chan struct{}), release: make(chan struct{}), cancelled: make(chan struct{})}
		held.Store(b)
		done := make(chan *httptest.ResponseRecorder, 1)
		joined := false
		defer func() {
			cancelRequest()
			b.unblock()
			held.Store(nil)
			if !joined {
				select {
				case <-done:
				case <-time.After(5 * time.Second):
					t.Error("post-dispatch cancellation worker failed bounded join")
				}
			}
		}()
		raw, err := json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPost, "http://routex.test/api/v1/admin/insights/analyze", bytes.NewReader(raw)).WithContext(requestCtx)
		req.AddCookie(freshCookie)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-CSRF-Token", fresh.CSRFToken)
		before = dispatches.Load()
		go func() {
			response := httptest.NewRecorder()
			router.ServeHTTP(response, req)
			done <- response
		}()
		select {
		case <-b.entered:
		case response := <-done:
			joined = true
			t.Fatalf("cancellation did not reach actual upstream dispatch: %d", response.Code)
		case <-ctx.Done():
			t.Fatal("cancellation never reached actual upstream dispatch")
		}
		cancelRequest()
		var cancelled *httptest.ResponseRecorder
		select {
		case cancelled = <-done:
			joined = true
		case <-time.After(5 * time.Second):
			t.Fatal("cancelled native request did not join")
		}
		select {
		case <-b.cancelled:
		case <-time.After(5 * time.Second):
			t.Fatal("controlled upstream did not observe cancellation")
		}
		expectStatus(t, cancelled, 503)
		if dispatches.Load() != before+1 || strings.Contains(cancelled.Body.String(), "recorded tokens") || strings.Contains(cancelled.Body.String(), personal.Model.ID) {
			t.Fatal("cancellation repeated native work or published a private answer")
		}
		if err := svc.FlushCallRecorder(ctx); err != nil {
			t.Fatal(err)
		}
		requestID := cancelled.Header().Get("X-Request-ID")
		var fact entity.CallRecord
		if err := db.Where("request_id = ?", requestID).First(&fact).Error; err != nil {
			t.Fatal("cancelled native call was not persisted", err)
		}
		if fact.UserID != admin.User.ID || fact.KeyID != "" || fact.TeamID != "" || fact.ProjectID != "" || fact.ModelID != personal.Model.ID || fact.Protocol != entity.ProtocolOpenAIChat || fact.Status != "canceled" || fact.ErrorCode != "canceled" || fact.RouteStopReason != "canceled" || fact.InputTokens != nil || fact.OutputTokens != nil {
			t.Fatal("post-dispatch cancellation lost personal attribution or invented known usage", fact)
		}
		var attempts []entity.CallAttempt
		if err := db.Where("request_id = ?", requestID).Find(&attempts).Error; err != nil {
			t.Fatal(err)
		}
		if len(attempts) != 1 || attempts[0].CredentialID != credentialID || attempts[0].SnapshotID == "" || attempts[0].SnapshotID != fact.SnapshotID || attempts[0].ProviderModelID != pm.ID || attempts[0].ConnectionID != provider.Connections[0].Connection.ID || attempts[0].Status != "error" || attempts[0].ErrorCode != "canceled" || attempts[0].WorkEvidence != "unknown" || attempts[0].NativeCompletionEvidence != "unknown" || attempts[0].FinalUsageKnown {
			t.Fatal("cancelled actual attempt lost exact route or claimed completion", attempts)
		}
		usage, err := svc.GetResourceLimit(ctx, admin.User.ID, target)
		if err != nil || usage.QuotaUsage == nil || usage.QuotaUsage.Month == nil || usage.QuotaUsage.Active == nil || usage.QuotaUsage.Month.TokensUsed != expectedTokens || usage.QuotaUsage.Month.TokensUnknown != 1 || usage.QuotaUsage.Month.TokensHeld != 0 || usage.QuotaUsage.Active.TokensHeld != 0 || usage.QuotaUsage.Active.TokensUnknown != 0 {
			t.Fatal("cancelled actual work became zero usage or retained an active hold", usage, err)
		}
	})
}
