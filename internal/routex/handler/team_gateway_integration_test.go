package handler

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base32"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/eventqueue"
	"github.com/miclle/routex/pkg/limits"
	"github.com/miclle/routex/pkg/secret"
	"github.com/miclle/routex/pkg/secretstore"
	"gorm.io/gorm"
)

func testTeamGatewayLifecycle(t *testing.T, db *gorm.DB) {
	ctx := context.Background()
	var publicationOutage, databaseOutage atomic.Bool
	const failureCallback = "team-gateway-controlled-query-outage"
	if err := db.Callback().Query().Before("gorm:query").Register(failureCallback, func(tx *gorm.DB) {
		if databaseOutage.Load() {
			_ = tx.AddError(errors.New("controlled primary outage"))
		} else if publicationOutage.Load() && tx.Statement.Table == "providers" {
			_ = tx.AddError(errors.New("controlled route publication outage"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	var services []*service.Service
	defer func() {
		// GORM callback registration is immutable while any runtime or recorder
		// can query this pool. Restore availability before stopping and joining
		// every service, including the pre-restart instance, then remove it.
		publicationOutage.Store(false)
		databaseOutage.Store(false)
		for _, current := range services {
			current.StopRuntime()
			_ = current.StopCallRecorder()
		}
		_ = db.Callback().Query().Remove(failureCallback)
	}()
	store, err := secretstore.New(bytes.Repeat([]byte{111}, 32))
	if err != nil {
		t.Fatal(err)
	}
	var dispatches atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dispatches.Add(1)
		if r.Header.Get("Cookie") != "" || r.Header.Get("X-CSRF-Token") != "" || r.Header.Get("Authorization") != "Bearer team-upstream-secret" {
			t.Error("Session proof forwarded to upstream")
		}
		var body map[string]json.RawMessage
		raw, _ := io.ReadAll(r.Body)
		if json.Unmarshal(raw, &body) != nil {
			t.Error("invalid upstream body")
		}
		if string(body["stream"]) == "true" {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "data: {\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"Done\"},\"finish_reason\":null}]}\n\n")
			_, _ = io.WriteString(w, "data: {\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":4,\"completion_tokens\":1,\"prompt_tokens_details\":{\"cached_tokens\":0,\"cache_write_tokens\":0}}}\n\n")
			_, _ = io.WriteString(w, "data: {\"object\":\"chat.completion.chunk\",\"choices\":[],\"usage\":{\"prompt_tokens\":4,\"completion_tokens\":1,\"prompt_tokens_details\":{\"cached_tokens\":0,\"cache_write_tokens\":0}}}\n\ndata: [DONE]\n\n")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"object":"chat.completion","model":"native","choices":[{"index":0,"message":{"role":"assistant","content":"Done"},"finish_reason":"stop"}],"usage":{"prompt_tokens":4,"completion_tokens":1,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0}}}`)
	}))
	defer upstream.Close()
	makeService := func() *service.Service {
		svc, err := service.New(ctx, db, service.WithCredentialStorage(store), service.WithUpstreamPolicy(true), service.WithRuntimeRefreshInterval(time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		services = append(services, svc)
		return svc
	}
	svc := makeService()
	admin, err := svc.Initialize(ctx, "team-gateway-admin@example.invalid", "team-gateway-password", "Team administrator")
	if err != nil {
		t.Fatal(err)
	}
	member, err := svc.CreateMember(ctx, admin.User.ID, "team-gateway-member@example.invalid", "team-gateway-password", "Team caller", entity.RoleMember)
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.CreateMember(ctx, admin.User.ID, "team-gateway-outsider@example.invalid", "team-gateway-password", "Outside caller", entity.RoleMember)
	if err != nil {
		t.Fatal(err)
	}
	cipher, err := store.Seal("crd_team_gateway", "team-upstream-secret")
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
	firstModel, otherModel, teamID, otherTeam := "mdl_team_gateway", "mdl_team_other", "tem_gateway", "tem_other_gateway"
	personalBearer := "rx_" + strings.Repeat("p", 43)
	create(
		&entity.Provider{ID: "prv_team_gateway", Name: "Native Team provider"},
		&entity.ProviderConnection{ID: "con_team_gateway", ProviderID: "prv_team_gateway", Name: "Native Chat", Protocol: entity.ProtocolOpenAIChat, BaseURL: upstream.URL + "/v1"},
		&entity.ProviderCredential{ID: "crd_team_gateway", ConnectionID: "con_team_gateway", Name: "Verified", Ciphertext: cipher, Enabled: true, VerificationStatus: "verified"},
		&entity.ProviderModel{ID: "pmd_team_gateway", ConnectionID: "con_team_gateway", UpstreamName: "native"},
		&entity.CredentialModelAccess{CredentialID: "crd_team_gateway", ProviderModelID: "pmd_team_gateway"},
		&entity.Model{ID: firstModel, Status: entity.ResourceActive}, &entity.Model{ID: otherModel, Status: entity.ResourceActive},
		&entity.ModelName{Name: "team-native", ModelID: firstModel, CurrentModelID: &firstModel}, &entity.ModelName{Name: "other-team-native", ModelID: otherModel, CurrentModelID: &otherModel},
		&entity.ModelProviderBinding{ID: "bnd_team_gateway", ModelID: firstModel, ProviderModelID: "pmd_team_gateway", Weight: 100}, &entity.ModelProviderBinding{ID: "bnd_team_other", ModelID: otherModel, ProviderModelID: "pmd_team_gateway", Weight: 100},
		&entity.Team{ID: teamID, Name: "Native Team", Status: entity.ResourceActive}, &entity.Team{ID: otherTeam, Name: "Other Team", Status: entity.ResourceActive},
		&entity.TeamMembership{ID: "tmm_gateway_owner", TeamID: teamID, UserID: admin.User.ID, Role: entity.TeamOwner, Status: entity.ResourceActive},
		&entity.TeamMembership{ID: "tmm_gateway_member", TeamID: teamID, UserID: member.User.ID, Role: entity.TeamMember, Status: entity.ResourceActive},
		&entity.TeamMembership{ID: "tmm_gateway_other", TeamID: otherTeam, UserID: member.User.ID, Role: entity.TeamMember, Status: entity.ResourceActive},
		&entity.TeamModelGrant{TeamID: teamID, ModelID: firstModel}, &entity.TeamModelGrant{TeamID: otherTeam, ModelID: otherModel},
		&entity.APIKey{ID: "key_team_gateway", UserID: member.User.ID, Name: "Personal cannot borrow Team", Prefix: "rx_masked", TokenHash: secret.SHA256Hex(personalBearer), Status: entity.KeyActive},
		&entity.APIKeyModel{KeyID: "key_team_gateway", ModelID: firstModel},
	)
	router := fox.New()
	New(svc).RegisterRoutes(router)
	if err := svc.StartRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	spool := filepath.Join(t.TempDir(), "team-gateway.db")
	if err := svc.StartCallRecorder(ctx, spool); err != nil {
		t.Fatal(err)
	}
	memberSession, memberCookie := readIdentity(t, identityRequest(router, "POST", "/api/v1/auth/login", `{"email":"team-gateway-member@example.invalid","password":"team-gateway-password"}`, nil, ""))
	_, outsiderCookie := readIdentity(t, identityRequest(router, "POST", "/api/v1/auth/login", `{"email":"team-gateway-outsider@example.invalid","password":"team-gateway-password"}`, nil, ""))
	// A Personal zero policy is a real current policy, yet Team Session accounts
	// are independent and have no invented finite Team quota.
	zero := int64(0)
	personalTarget := service.LimitTarget{Kind: "user", ID: member.User.ID}
	personalPolicy, err := svc.GetResourceLimit(ctx, admin.User.ID, personalTarget)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetResourceLimit(ctx, admin.User.ID, personalTarget, personalPolicy.ETag, service.LimitInput{Policy: limits.Policy{TokensMonth: &zero, RPM: &zero}, Reason: "Personal isolation acceptance"}); err != nil {
		t.Fatal(err)
	}
	native := func(cookie *http.Cookie, csrf, method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "http://routex.test"+path, strings.NewReader(body))
		if cookie != nil {
			req.AddCookie(cookie)
		}
		req.Header.Set("Content-Type", "application/json")
		if csrf != "" {
			req.Header.Set("X-CSRF-Token", csrf)
		}
		req.Header.Set("Origin", "http://routex.test")
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		for key, value := range headers {
			req.Header.Set(key, value)
		}
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		return res
	}
	modelPath := "/api/v1/teams/" + teamID + "/inference-models"
	chatPath := "/api/v1/teams/" + teamID + "/chat/completions"
	body := `{"model":"team-native","messages":[{"role":"user","content":"Hello"}],"max_completion_tokens":1}`
	discovery := native(memberCookie, "", "GET", modelPath, "", nil)
	expectStatus(t, discovery, 200)
	var models struct {
		Object string                 `json:"object"`
		Data   []service.GatewayModel `json:"data"`
	}
	if json.Unmarshal(discovery.Body.Bytes(), &models) != nil || models.Object != "list" || len(models.Data) != 1 || models.Data[0].ID != "team-native" || models.Data[0].ModelID != firstModel || models.Data[0].AttachmentScope != "team" || models.Data[0].PersonalAttachments || len(models.Data[0].InputCapabilities[entity.ProtocolOpenAIChat]) != 0 {
		t.Fatal("Team discovery fabricated or unioned scope", discovery.Body.String())
	}
	for _, test := range []struct {
		cookie  *http.Cookie
		csrf    string
		headers map[string]string
		status  int
	}{
		{nil, memberSession.CSRFToken, nil, 401},
		{memberCookie, "", nil, 403},
		{memberCookie, strings.Repeat("a", 64), nil, 403},
		{memberCookie, memberSession.CSRFToken, map[string]string{"Origin": "https://external.example"}, 403},
		{memberCookie, memberSession.CSRFToken, map[string]string{"Sec-Fetch-Site": "cross-site"}, 403},
		{memberCookie, memberSession.CSRFToken, map[string]string{"Authorization": "Bearer " + personalBearer}, 403},
		{outsiderCookie, memberSession.CSRFToken, nil, 403},
	} {
		expectStatus(t, native(test.cookie, test.csrf, "POST", chatPath, body, test.headers), test.status)
	}
	if dispatches.Load() != 0 {
		t.Fatal("invalid Session/origin/CSRF dispatched upstream")
	}
	foreignBody := `{"model":"other-team-native","messages":[{"role":"user","content":"Hello"}]}`
	expectStatus(t, native(memberCookie, memberSession.CSRFToken, "POST", chatPath, foreignBody, nil), 404)
	personal := httptest.NewRequest("POST", "http://routex.test/v1/chat/completions", strings.NewReader(body))
	personal.Header.Set("Authorization", "Bearer "+personalBearer)
	personal.Header.Set("Content-Type", "application/json")
	personalResponse := httptest.NewRecorder()
	router.ServeHTTP(personalResponse, personal)
	expectStatus(t, personalResponse, 404)
	// No native media occurrence reaches attachment storage or upstream, while
	// opaque tool arguments and ordinary text remain native caller data.
	for _, media := range []string{
		`{"model":"team-native","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://example.invalid/image.png"}}]}]}`,
		`{"model":"team-native","messages":[{"role":"user","content":[{"type":"file","file":{"file_data":"routex://attachments/obj_team_forbidden"}}]}]}`,
	} {
		expectStatus(t, native(memberCookie, memberSession.CSRFToken, "POST", chatPath, media, nil), 400)
	}
	if dispatches.Load() != 0 {
		t.Fatal("foreign grant/Key/media boundary dispatched upstream")
	}
	expectStatus(t, identityRequest(router, "POST", "/api/v1/keys", `{"name":"Team cannot create Personal","model_ids":["mdl_team_gateway"]}`, memberCookie, memberSession.CSRFToken), 403)
	ordinary := native(memberCookie, memberSession.CSRFToken, "POST", chatPath, body, nil)
	expectStatus(t, ordinary, 200)
	streamBody := `{"model":"team-native","messages":[{"role":"user","content":"Hello"}],"max_completion_tokens":1,"stream":true,"stream_options":{"include_usage":true}}`
	streamed := native(memberCookie, memberSession.CSRFToken, "POST", chatPath, streamBody, nil)
	expectStatus(t, streamed, 200)
	if !strings.Contains(streamed.Body.String(), "[DONE]") || streamed.Header().Get("Content-Type") != "text/event-stream" {
		t.Fatal("Team native SSE lost terminal forwarding")
	}
	if err := svc.FlushCallRecorder(ctx); err != nil {
		t.Fatal(err)
	}
	for _, response := range []*httptest.ResponseRecorder{ordinary, streamed} {
		var fact entity.CallRecord
		if err := db.First(&fact, "request_id = ?", response.Header().Get("X-Request-ID")).Error; err != nil {
			t.Fatal(err)
		}
		if fact.TeamID != teamID || fact.TeamMembershipID != "tmm_gateway_member" || fact.UserID != member.User.ID || fact.ProjectID != "" || fact.KeyID != "" || fact.InputTokens == nil || *fact.InputTokens != 4 || fact.OutputTokens == nil || *fact.OutputTokens != 1 {
			t.Fatal("native Team fact lost immutable scope/usage", fact)
		}
		var attempt entity.CallAttempt
		if err := db.First(&attempt, "request_id = ?", fact.RequestID).Error; err != nil || attempt.NativeCompletionEvidence != "completed" || !attempt.FinalUsageKnown || attempt.CredentialID != "crd_team_gateway" || attempt.SnapshotID == "" {
			t.Fatal("Team native attempt lost completion/attribution", attempt, err)
		}
	}
	personalLimit, err := svc.GetResourceLimit(ctx, admin.User.ID, service.LimitTarget{Kind: "user", ID: member.User.ID})
	if err != nil || personalLimit.QuotaUsage.Month.TokensUsed != 0 || personalLimit.QuotaUsage.Month.TokensUnknown != 0 {
		t.Fatal("Team Session debited Personal quota", personalLimit, err)
	}
	// Runtime grant revocation remains effective when route publication fails.
	publicationOutage.Store(true)
	_, revokeError := svc.SetResourceModels(ctx, admin.User.ID, service.TeamResource, teamID, []string{})
	publicationOutage.Store(false)
	if revokeError == nil {
		t.Fatal("fixture did not force failed publication after durable grant removal")
	}
	before := dispatches.Load()
	revoked := native(memberCookie, memberSession.CSRFToken, "POST", chatPath, body, nil)
	if revoked.Code != 403 && revoked.Code != 404 {
		t.Fatal("grant removal did not fail closed", revoked.Code, revoked.Body.String())
	}
	if dispatches.Load() != before {
		t.Fatal("revoked Team grant dispatched")
	}
	if _, err := svc.SetResourceModels(ctx, admin.User.ID, service.TeamResource, teamID, []string{firstModel}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetTeamMembers(ctx, admin.User.ID, teamID, []service.TeamMemberInput{{UserID: admin.User.ID, Role: entity.TeamOwner, Status: entity.ResourceActive}}); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, native(memberCookie, memberSession.CSRFToken, "POST", chatPath, body, nil), 403)
	if _, err := svc.SetTeamMembers(ctx, admin.User.ID, teamID, []service.TeamMemberInput{{UserID: admin.User.ID, Role: entity.TeamOwner, Status: entity.ResourceActive}, {UserID: member.User.ID, Role: entity.TeamMember, Status: entity.ResourceActive}}); err != nil {
		t.Fatal(err)
	}
	var rejoined entity.TeamMembership
	if err := db.First(&rejoined, "team_id = ? AND user_id = ?", teamID, member.User.ID).Error; err != nil || rejoined.ID == "tmm_gateway_member" {
		t.Fatal("rejoin did not replace membership generation", rejoined, err)
	}
	rejoinedResponse := native(memberCookie, memberSession.CSRFToken, "POST", chatPath, body, nil)
	expectStatus(t, rejoinedResponse, 200)
	if err := svc.FlushCallRecorder(ctx); err != nil {
		t.Fatal(err)
	}
	var rejoinedFact entity.CallRecord
	if err := db.First(&rejoinedFact, "request_id = ?", rejoinedResponse.Header().Get("X-Request-ID")).Error; err != nil || rejoinedFact.TeamMembershipID != rejoined.ID {
		t.Fatal("new call reused historical relationship", err)
	}
	// Database outage does not force per-request authentication reads, but an
	// expired authorization lease prevents a new admission or dispatch.
	if err := svc.RefreshRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	databaseOutage.Store(true)
	outageResponse := native(memberCookie, memberSession.CSRFToken, "POST", chatPath, body, nil)
	expectStatus(t, outageResponse, 200)
	deadline := time.Now().Add(7 * time.Second)
	expired := false
	for time.Now().Before(deadline) {
		res := native(memberCookie, "", "GET", modelPath, "", nil)
		if res.Code == 503 {
			expired = true
			break
		}
		expectStatus(t, res, 200)
		time.Sleep(100 * time.Millisecond)
	}
	before = dispatches.Load()
	expectStatus(t, native(memberCookie, memberSession.CSRFToken, "POST", chatPath, body, nil), 503)
	databaseOutage.Store(false)
	if !expired || dispatches.Load() != before {
		t.Fatal("expired runtime admitted work during primary outage")
	}
	if err := svc.FlushCallRecorder(ctx); err != nil {
		t.Fatal(err)
	}
	if err := svc.RefreshRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	// The source Session cookie and exact Team ledger survive a new service.
	svc.StopRuntime() // Join the outgoing live publisher before service replacement.
	if err := svc.StopCallRecorder(); err != nil {
		t.Fatal(err)
	}
	queue, err := eventqueue.Open(spool, 4096, 64<<10)
	if err != nil {
		t.Fatal(err)
	}
	pairRaw, _ := json.Marshal([2]string{teamID, member.User.ID})
	pairDigest := sha256.Sum256(pairRaw)
	pairAccount := "team_member_" + base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(pairDigest[:])
	for _, account := range []string{"team_" + teamID, pairAccount} {
		usage, err := queue.AccountQuotaUsage(account, time.Now())
		if err != nil || usage.Month.TokensUsed != 20 || usage.Month.TokensUnknown != 0 {
			t.Fatal("Team settled journal/rejoin identity did not persist", usage, err)
		}
	}
	userUsage, err := queue.AccountQuotaUsage("user_"+member.User.ID, time.Now())
	if err != nil || userUsage.Month.TokensUsed != 0 {
		t.Fatal("Personal journal changed across Team restart", userUsage, err)
	}
	if err := queue.Close(); err != nil {
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
	expectStatus(t, native(memberCookie, "", "GET", modelPath, "", nil), 200)
	expectStatus(t, native(memberCookie, memberSession.CSRFToken, "POST", chatPath, body, nil), 200)
	if err := svc.FlushCallRecorder(ctx); err != nil {
		t.Fatal(err)
	}
	// An acknowledged dispatched call retains its historical Team identity even
	// when membership later changes; every subsequent invocation is rechecked.
	if _, err := svc.SetTeamMembers(ctx, admin.User.ID, teamID, []service.TeamMemberInput{{UserID: admin.User.ID, Role: entity.TeamOwner, Status: entity.ResourceActive}}); err != nil {
		t.Fatal(err)
	}
	before = dispatches.Load()
	expectStatus(t, native(memberCookie, memberSession.CSRFToken, "POST", chatPath, body, nil), 403)
	if dispatches.Load() != before {
		t.Fatal("post-restart membership revocation dispatched")
	}
}
