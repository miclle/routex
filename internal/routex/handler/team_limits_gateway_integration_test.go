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
	"github.com/miclle/routex/pkg/pricing"
	"github.com/miclle/routex/pkg/secret"
	"github.com/miclle/routex/pkg/secretstore"
	"gorm.io/gorm"
)

func testTeamLimitsGatewayLifecycle(t *testing.T, db *gorm.DB) {
	ctx := context.Background()
	store, err := secretstore.New(bytes.Repeat([]byte{119}, 32))
	if err != nil {
		t.Fatal(err)
	}
	var dispatches atomic.Int32
	var blocked, unknown atomic.Bool
	entered, release := make(chan struct{}, 1), make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dispatches.Add(1)
		var payload map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error("invalid native Team request", err)
		}
		if blocked.Load() {
			select {
			case entered <- struct{}{}:
			default:
			}
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		if unknown.Load() {
			_, _ = io.WriteString(w, `{"object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"Done"},"finish_reason":"stop"}]}`)
			return
		}
		if string(payload["stream"]) == "true" {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "data: {\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"Done\"},\"finish_reason\":null}]}\n\n")
			_, _ = io.WriteString(w, "data: {\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n")
			_, _ = io.WriteString(w, "data: {\"object\":\"chat.completion.chunk\",\"choices\":[],\"usage\":{\"prompt_tokens\":4,\"completion_tokens\":1,\"prompt_tokens_details\":{\"cached_tokens\":0,\"cache_write_tokens\":0}}}\n\ndata: [DONE]\n\n")
			return
		}
		_, _ = io.WriteString(w, `{"object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"Done"},"finish_reason":"stop"}],"usage":{"prompt_tokens":4,"completion_tokens":1,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0}}}`)
	}))
	defer upstream.Close()
	defer unblock()
	var instances []*service.Service
	defer func() {
		unblock()
		for _, svc := range instances {
			svc.StopRuntime()
			_ = svc.StopCallRecorder()
		}
	}()
	makeService := func() *service.Service {
		svc, err := service.New(ctx, db, service.WithCredentialStorage(store), service.WithUpstreamPolicy(true))
		if err != nil {
			t.Fatal(err)
		}
		instances = append(instances, svc)
		return svc
	}
	svc := makeService()
	admin, err := svc.Initialize(ctx, "team-limits-admin@example.invalid", "team-limits-password", "Team limits administrator")
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
	modelID := "mdl_team_limits"
	bearer := "rx_" + strings.Repeat("l", 43)
	cipher, err := store.Seal("crd_team_limits", "team-limits-upstream-secret")
	if err != nil {
		t.Fatal(err)
	}
	create(
		&entity.Provider{ID: "prv_team_limits", Name: "Team limits provider"},
		&entity.ProviderConnection{ID: "con_team_limits", ProviderID: "prv_team_limits", Name: "Native Chat", Protocol: entity.ProtocolOpenAIChat, BaseURL: upstream.URL + "/v1"},
		&entity.ProviderCredential{ID: "crd_team_limits", ConnectionID: "con_team_limits", Name: "Verified", Ciphertext: cipher, Enabled: true, VerificationStatus: "verified"},
		&entity.ProviderModel{ID: "pmd_team_limits", ConnectionID: "con_team_limits", UpstreamName: "native"},
		&entity.CredentialModelAccess{CredentialID: "crd_team_limits", ProviderModelID: "pmd_team_limits"},
		&entity.Model{ID: modelID, Status: entity.ResourceActive}, &entity.ModelName{Name: "team-limits-native", ModelID: modelID, CurrentModelID: &modelID},
		&entity.ModelProviderBinding{ID: "bnd_team_limits", ModelID: modelID, ProviderModelID: "pmd_team_limits", Weight: 100},
		&entity.UserModelGrant{UserID: admin.User.ID, ModelID: modelID},
		&entity.APIKey{ID: "key_team_limits", UserID: admin.User.ID, Name: "Coverage warmup", Prefix: "rx_masked", TokenHash: secret.SHA256Hex(bearer), Status: entity.KeyActive},
		&entity.APIKeyModel{KeyID: "key_team_limits", ModelID: modelID},
		&entity.ModelPrice{ID: "price_team_limits", ProviderModelID: "pmd_team_limits", UpdateSource: "api"},
		&entity.ReservationBound{ProviderModelID: "pmd_team_limits", Protocol: entity.ProtocolOpenAIChat, MaxInputTokens: 4, MaxOutputTokens: 1, ETag: "bnd_team_limits", Evidence: "Controlled native maximum", Reason: "Team acceptance"},
	)
	for _, metric := range []string{pricing.Input, pricing.Output, pricing.CacheRead, pricing.CacheWrite} {
		create(&entity.PriceRate{ID: "rate_team_" + metric, ModelPriceID: "price_team_limits", Metric: metric, Tier: pricing.Base, Unit: pricing.Unit, Currency: "USD", Amount: "1000000", Enabled: true})
	}
	router := fox.New()
	New(svc).RegisterRoutes(router)
	if err := svc.StartRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	svc.StopRuntime()
	spool := filepath.Join(t.TempDir(), "team-limits.db")
	if err := svc.StartCallRecorder(ctx, spool); err != nil {
		t.Fatal(err)
	}
	body := `{"model":"team-limits-native","messages":[{"role":"user","content":"Hello"}],"max_completion_tokens":1}`
	warmup := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	warmup.Header.Set("Authorization", "Bearer "+bearer)
	warmup.Header.Set("Content-Type", "application/json")
	warmupResponse := httptest.NewRecorder()
	router.ServeHTTP(warmupResponse, warmup)
	expectStatus(t, warmupResponse, 200)
	if err := svc.FlushCallRecorder(ctx); err != nil {
		t.Fatal(err)
	}
	// The actual journal activation precedes Team creation; no fabricated
	// historical observation or membership birth substitutes for coverage.
	first, err := svc.CreateMember(ctx, admin.User.ID, "team-limits-first@example.invalid", "team-limits-password", "First caller", entity.RoleMember)
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.CreateMember(ctx, admin.User.ID, "team-limits-second@example.invalid", "team-limits-password", "Second caller", entity.RoleMember)
	if err != nil {
		t.Fatal(err)
	}
	teamID := "tem_limits"
	create(
		&entity.Team{ID: teamID, Name: "Finite Team", Status: entity.ResourceActive},
		&entity.TeamMembership{ID: "tmm_limits_owner", TeamID: teamID, UserID: admin.User.ID, Role: entity.TeamOwner, Status: entity.ResourceActive},
		&entity.TeamMembership{ID: "tmm_limits_first", TeamID: teamID, UserID: first.User.ID, Role: entity.TeamMember, Status: entity.ResourceActive},
		&entity.TeamMembership{ID: "tmm_limits_second", TeamID: teamID, UserID: second.User.ID, Role: entity.TeamMember, Status: entity.ResourceActive},
		&entity.TeamModelGrant{TeamID: teamID, ModelID: modelID},
	)
	firstSession, firstCookie := readIdentity(t, identityRequest(router, "POST", "/api/v1/auth/login", `{"email":"team-limits-first@example.invalid","password":"team-limits-password"}`, nil, ""))
	secondSession, secondCookie := readIdentity(t, identityRequest(router, "POST", "/api/v1/auth/login", `{"email":"team-limits-second@example.invalid","password":"team-limits-password"}`, nil, ""))
	zero := int64(0)
	if _, err := svc.SetResourceLimit(ctx, admin.User.ID, service.LimitTarget{Kind: "user", ID: first.User.ID}, "0", service.LimitInput{Policy: limits.Policy{TokensMonth: &zero, RPM: &zero}, Reason: "Personal isolation"}); err != nil {
		t.Fatal(err)
	}
	read := func(userID string) *service.LimitRecord {
		t.Helper()
		value, err := svc.GetTeamResourceLimit(ctx, admin.User.ID, teamID, userID)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	write := func(userID, patch string) *service.LimitRecord {
		t.Helper()
		before := read(userID)
		var input service.TeamLimitInput
		if err := json.Unmarshal([]byte(patch), &input); err != nil {
			t.Fatal(err)
		}
		value, err := svc.SetTeamResourceLimit(ctx, admin.User.ID, teamID, userID, before.ETag, input)
		if err != nil || !value.Enforced {
			t.Fatal("saved Team policy not applied", value, err)
		}
		return value
	}
	native := func(cookie *http.Cookie, csrf string, stream ...bool) *httptest.ResponseRecorder {
		requestCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		requestBody := body
		if len(stream) != 0 && stream[0] {
			requestBody = strings.TrimSuffix(body, "}") + `,"stream":true,"stream_options":{"include_usage":true}}`
		}
		req := httptest.NewRequestWithContext(requestCtx, "POST", "http://routex.test/api/v1/teams/"+teamID+"/chat/completions", strings.NewReader(requestBody))
		req.AddCookie(cookie)
		req.Header.Set("X-CSRF-Token", csrf)
		req.Header.Set("Origin", "http://routex.test")
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		req.Header.Set("Content-Type", "application/json")
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		return res
	}
	assertUsage := func(userID string, tokens int64) *service.LimitRecord {
		t.Helper()
		if err := svc.FlushCallRecorder(ctx); err != nil {
			t.Fatal(err)
		}
		value := read(userID)
		if value.QuotaUsage == nil || value.QuotaUsage.Month == nil || !value.QuotaUsage.Month.Covered || value.QuotaUsage.Month.TokensUsed != tokens || value.QuotaUsage.Month.TokensUnknown != 0 {
			t.Fatal("Team usage is not authoritative", value)
		}
		return value
	}
	write("", `{"tokens_month":10,"reason":"Team aggregate acceptance"}`)
	write(first.User.ID, `{"tokens_month":5,"reason":"Independent member ceiling"}`)
	expectStatus(t, native(firstCookie, firstSession.CSRFToken), 200)
	beforeDispatch := dispatches.Load()
	expectStatus(t, native(firstCookie, firstSession.CSRFToken), 429)
	if dispatches.Load() != beforeDispatch || *read("").RPMUsed != 1 || *read(first.User.ID).RPMUsed != 1 {
		t.Fatal("child rejection partially consumed aggregate admission")
	}
	streamed := native(secondCookie, secondSession.CSRFToken, true)
	expectStatus(t, streamed, 200)
	if streamed.Header().Get("Content-Type") != "text/event-stream" || !strings.Contains(streamed.Body.String(), "[DONE]") {
		t.Fatal("finite Team SSE lost native forwarding")
	}
	expectStatus(t, native(secondCookie, secondSession.CSRFToken), 429)
	assertUsage("", 10)
	firstUsage := assertUsage(first.User.ID, 5)
	assertUsage(second.User.ID, 5)
	pairAccount := firstUsage.AccountID
	personal, err := svc.GetResourceLimit(ctx, admin.User.ID, service.LimitTarget{Kind: "user", ID: first.User.ID})
	if err != nil || personal.QuotaUsage.Month.TokensUsed != 0 || personal.QuotaUsage.Month.TokensUnknown != 0 {
		t.Fatal("finite Team debited Personal ledger", personal, err)
	}
	write("", `{"tokens_month":100,"rpm":0,"reason":"Explicit aggregate rate zero"}`)
	expectStatus(t, native(secondCookie, secondSession.CSRFToken), 429)
	write("", `{"rpm":null,"tpm":0,"reason":"Explicit aggregate TPM zero"}`)
	expectStatus(t, native(secondCookie, secondSession.CSRFToken), 429)
	write("", `{"tpm":null,"reason":"Restore aggregate rate"}`)
	write(first.User.ID, `{"tokens_month":50,"rpm":0,"reason":"Explicit child rate zero"}`)
	expectStatus(t, native(firstCookie, firstSession.CSRFToken), 429)
	expectStatus(t, native(secondCookie, secondSession.CSRFToken), 200)
	assertUsage("", 15)
	write(first.User.ID, `{"rpm":null,"reason":"Restore child rate"}`)
	write("", `{"concurrency":1,"reason":"Aggregate live admission"}`)
	blocked.Store(true)
	firstResult := make(chan *httptest.ResponseRecorder, 1)
	go func() { firstResult <- native(secondCookie, secondSession.CSRFToken) }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("Team upstream did not enter controlled concurrency hold")
	}
	expectStatus(t, native(firstCookie, firstSession.CSRFToken), 429)
	blocked.Store(false)
	unblock()
	select {
	case response := <-firstResult:
		expectStatus(t, response, 200)
	case <-time.After(5 * time.Second):
		t.Fatal("Team upstream did not finish released concurrency hold")
	}
	assertUsage("", 20)
	write("", `{"concurrency":null,"money_month":"25.000000000000000002","currency":"USD","reason":"Exact settled Team money"}`)
	write(first.User.ID, `{"money_month":"5","currency":"USD","reason":"Independent settled member money"}`)
	beforeDispatch = dispatches.Load()
	expectStatus(t, native(firstCookie, firstSession.CSRFToken), 429)
	if dispatches.Load() != beforeDispatch {
		t.Fatal("member money exhaustion borrowed aggregate allowance")
	}
	expectStatus(t, native(secondCookie, secondSession.CSRFToken), 200)
	expectStatus(t, native(secondCookie, secondSession.CSRFToken), 429)
	value := assertUsage("", 25)
	if value.Stored.MoneyMonth == nil || *value.Stored.MoneyMonth != "25.000000000000000002" || value.QuotaUsage.Month.MoneyUsed["USD"] != "25" {
		t.Fatal("Team monetary policy or settlement lost exact decimal", value)
	}
	// Retain the same pair policy and usage after replacing its relationship.
	if _, err := svc.SetTeamMembers(ctx, admin.User.ID, teamID, []service.TeamMemberInput{{UserID: admin.User.ID, Role: entity.TeamOwner, Status: entity.ResourceActive}, {UserID: second.User.ID, Role: entity.TeamMember, Status: entity.ResourceActive}}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetTeamMembers(ctx, admin.User.ID, teamID, []service.TeamMemberInput{{UserID: admin.User.ID, Role: entity.TeamOwner, Status: entity.ResourceActive}, {UserID: first.User.ID, Role: entity.TeamMember, Status: entity.ResourceActive}, {UserID: second.User.ID, Role: entity.TeamMember, Status: entity.ResourceActive}}); err != nil {
		t.Fatal(err)
	}
	if after := assertUsage(first.User.ID, 5); after.AccountID != pairAccount || after.Stored.TokensMonth == nil || *after.Stored.TokensMonth != 50 {
		t.Fatal("rejoin reset member account or policy", after)
	}
	if err := svc.StopCallRecorder(); err != nil {
		t.Fatal(err)
	}
	svc = makeService()
	router = fox.New()
	New(svc).RegisterRoutes(router)
	if err := svc.StartRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	svc.StopRuntime()
	if err := svc.StartCallRecorder(ctx, spool); err != nil {
		t.Fatal(err)
	}
	assertUsage("", 25)
	if after := assertUsage(first.User.ID, 5); after.AccountID != pairAccount {
		t.Fatal("restart changed pair account", after)
	}
	expectStatus(t, native(firstCookie, firstSession.CSRFToken), 429)
	write("", `{"money_month":null,"tokens_month":null,"reason":"Observe unbounded native usage separately"}`)
	write(first.User.ID, `{"tokens_month":null,"money_month":null,"reason":"Observe missing usage without invented bound"}`)
	body = `{"model":"team-limits-native","messages":[{"role":"user","content":"Hello"}]}`
	unknown.Store(true)
	expectStatus(t, native(firstCookie, firstSession.CSRFToken), 200)
	unknown.Store(false)
	if err := svc.FlushCallRecorder(ctx); err != nil {
		t.Fatal(err)
	}
	write("", `{"tokens_month":100,"money_month":"100","currency":"USD","reason":"Finite policy requires known prior use"}`)
	body = `{"model":"team-limits-native","messages":[{"role":"user","content":"Hello"}],"max_completion_tokens":1}`
	expectStatus(t, native(firstCookie, firstSession.CSRFToken), 503)
	if after := read(""); after.QuotaUsage.Month.TokensUnknown != 1 || after.QuotaUsage.Month.MoneyUnknown != 1 {
		t.Fatal("missing native usage was silently settled as zero", after)
	}
}
