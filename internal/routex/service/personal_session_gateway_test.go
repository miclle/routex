package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/eventqueue"
	"github.com/miclle/routex/pkg/secret"
)

func personalAnalysisRequest(t *testing.T) []byte {
	t.Helper()
	body, err := json.Marshal(map[string]any{"model": "public-model", "stream": false, "max_completion_tokens": 2048, "messages": []map[string]string{{"role": "system", "content": "Analyze aggregates only."}, {"role": "user", "content": "M1: 12 known tokens."}}})
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func personalSessionFixture(t *testing.T, baseURL string) (*Service, *runtimeData, string, *PersonalSessionIdentity) {
	t.Helper()
	svc, data, _ := runtimeFixture(t, baseURL)
	data.Keys, data.Scopes = nil, nil
	data.Users[0].PersonalGrantRevision = strings.Repeat("a", 64)
	data.Grants[0].CreatedAt = data.Users[0].CreatedAt
	cookie := strings.Repeat("p", 43)
	// The explicit stored off policy is valid source evidence, not a fallback
	// for an unavailable policy. RouteX local primary auth uses the empty method.
	data.TeamSessionData = &teamSessionRuntimeData{
		AuthenticationPolicy: entity.GovernanceSetting{ID: 1, AuthenticationPolicyRevision: strings.Repeat("0", 64)},
		Sessions:             []entity.Session{{ID: "ses_personal", UserID: "usr_one", TokenHash: secret.SHA256Hex(cookie), CreatedAt: data.Users[0].CreatedAt.Add(time.Microsecond), PrimaryMethod: "", ExpiresAt: time.Now().Add(time.Hour)}},
	}
	data.Quota.Created[limitAccount("user", "usr_one")] = data.Users[0].CreatedAt
	if err := compileRuntimeLimits(data); err != nil {
		t.Fatal(err)
	}
	svc.runtime.auth.Store(buildRuntimeAuthorization(data, time.Now().Add(time.Minute)))
	identity, err := svc.RuntimeAuthenticatePersonalSession(context.Background(), cookie)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(svc.upstream.CloseIdleConnections)
	return svc, data, cookie, identity
}

func personalSessionJournal(t *testing.T, svc *Service, data *runtimeData) (*eventqueue.Queue, string) {
	t.Helper()
	journal := filepath.Join(t.TempDir(), "personal-native.db")
	queue, err := eventqueue.Open(journal, 20, callQueuePayloadLimit)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = queue.Close() })
	if err := queue.EnableQuota("UTC", data.Users[0].CreatedAt.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	svc.admission = newLocalAdmissionStore(queue)
	svc.recorder = &callRecorder{queue: queue}
	return queue, journal
}

func TestPersonalSessionExactManualAndRequestedGrantsWithoutKeyOrTeam(t *testing.T) {
	svc, data, cookie, identity := personalSessionFixture(t, "http://127.0.0.1/v1")
	if identity.UserID != "usr_one" || identity.SessionID != "ses_personal" || len(identity.ModelIDs) != 1 || identity.ModelIDs[0] != "mdl_one" || len(svc.runtime.auth.Load().KeysByID) != 0 || len(svc.runtime.auth.Load().Teams) != 0 {
		t.Fatal("Personal Session acquired a synthetic Key or Team context")
	}
	if !ValidatePersonalSessionCSRF(identity, (&Authentication{Token: cookie}).CSRFToken()) || ValidatePersonalSessionCSRF(identity, strings.Repeat("a", 64)) || ValidatePersonalSessionCSRF(nil, "") {
		t.Fatal("Personal Session CSRF contract changed")
	}
	identity.ModelIDs[0] = "mdl_foreign"
	if err := svc.ReauthorizePersonalSession(context.Background(), identity, "mdl_one"); err != nil {
		t.Fatal("advisory list replaced private authority", err)
	}
	source := "mar_requested"
	data.Models = append(data.Models, entity.Model{ID: "mdl_requested", Status: "active", CreatedAt: time.Now().UTC()}, entity.Model{ID: "mdl_team_only", Status: "active", CreatedAt: time.Now().UTC()})
	data.Grants = append(data.Grants, entity.UserModelGrant{UserID: "usr_one", ModelID: "mdl_requested", CreatedAt: time.Now().UTC(), SourceRequestID: &source})
	data.TeamSessionData.Teams = []entity.Team{{ID: "tem_one", CreatedAt: time.Now().UTC(), Status: entity.ResourceActive}}
	data.TeamSessionData.Memberships = []entity.TeamMembership{{ID: "tmm_one", TeamID: "tem_one", UserID: "usr_one", Role: entity.TeamMember, Status: entity.ResourceActive}}
	data.TeamSessionData.Grants = []entity.TeamModelGrant{{TeamID: "tem_one", ModelID: "mdl_team_only"}}
	svc.runtime.auth.Store(buildRuntimeAuthorization(data, time.Now().Add(time.Minute)))
	identity, err := svc.RuntimeAuthenticatePersonalSession(context.Background(), cookie)
	if err != nil {
		t.Fatal(err)
	}
	for _, modelID := range []string{"mdl_one", "mdl_requested"} {
		if err := svc.ReauthorizePersonalSession(context.Background(), identity, modelID); err != nil {
			t.Fatal("exact personal grant denied", modelID, err)
		}
	}
	requireTeamSessionError(t, svc.ReauthorizePersonalSession(context.Background(), identity, "mdl_team_only"), 404, "model_not_found")
	requireTeamSessionError(t, svc.ReauthorizePersonalSession(context.Background(), identity, "MDL_one"), 404, "model_not_found")
}

func TestPersonalSessionCapturedProofAndImmediateRevocation(t *testing.T) {
	cases := map[string]func(*Service, *runtimeData){
		"session removed": func(_ *Service, d *runtimeData) { d.TeamSessionData.Sessions = nil },
		"session expiry": func(_ *Service, d *runtimeData) {
			d.TeamSessionData.Sessions[0].ExpiresAt = time.Now().Add(-time.Second)
		},
		"session replacement":          func(_ *Service, d *runtimeData) { d.TeamSessionData.Sessions[0].ID = "ses_replacement" },
		"user birth":                   func(_ *Service, d *runtimeData) { d.Users[0].CreatedAt = d.Users[0].CreatedAt.Add(time.Second) },
		"disabled user":                func(_ *Service, d *runtimeData) { d.Users[0].Disabled = true },
		"offboarded user":              func(_ *Service, d *runtimeData) { now := time.Now().UTC(); d.Users[0].OffboardedAt = &now },
		"grant revision":               func(_ *Service, d *runtimeData) { d.Users[0].PersonalGrantRevision = strings.Repeat("b", 64) },
		"grant birth without revision": func(_ *Service, d *runtimeData) { d.Grants[0].CreatedAt = d.Grants[0].CreatedAt.Add(time.Second) },
		"grant removal":                func(_ *Service, d *runtimeData) { d.Grants = nil },
		"logout tombstone":             func(s *Service, _ *runtimeData) { s.invalidateRuntimeSession("ses_personal") },
		"session user tombstone":       func(s *Service, _ *runtimeData) { s.invalidateRuntimeSessionUser("usr_one") },
		"user tombstone":               func(s *Service, _ *runtimeData) { s.InvalidateRuntimeUser("usr_one") },
		"grant tombstone":              func(s *Service, _ *runtimeData) { s.runtime.deniedPersonalGrants.Store("usr_one", uint64(1)) },
		"model tombstone":              func(s *Service, _ *runtimeData) { s.InvalidateRuntimeModel("mdl_one") },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			svc, data, _, identity := personalSessionFixture(t, "http://127.0.0.1/v1")
			mutate(svc, data)
			svc.runtime.auth.Store(buildRuntimeAuthorization(data, time.Now().Add(time.Minute)))
			if err := svc.ReauthorizePersonalSession(context.Background(), identity, "mdl_one"); err == nil {
				t.Fatal("stale personal authority survived", name)
			}
		})
	}
	for _, field := range []string{"OIDC policy", "OIDC binding", "OAuth policy", "OAuth binding", "LDAP policy", "LDAP binding", "SAML policy", "SAML binding", "named policy", "named binding", "admission revision"} {
		t.Run(field, func(t *testing.T) {
			svc, _, _, identity := personalSessionFixture(t, "http://127.0.0.1/v1")
			auth := svc.runtime.auth.Load()
			proof := auth.TeamSessions[identity.session.TokenHash]
			switch field {
			case "OIDC policy":
				proof.OIDCPolicyRevision = "changed"
			case "OIDC binding":
				proof.OIDCBindingKey = "changed"
			case "OAuth policy":
				proof.OAuthPolicyRevision = "changed"
			case "OAuth binding":
				proof.OAuthBindingKey = "changed"
			case "LDAP policy":
				proof.LDAPPolicyRevision = "changed"
			case "LDAP binding":
				proof.LDAPBindingKey = "changed"
			case "SAML policy":
				proof.SAMLPolicyRevision = "changed"
			case "SAML binding":
				proof.SAMLBindingKey = "changed"
			case "named policy":
				proof.NamedIdentityPolicyKey = "changed"
			case "named binding":
				proof.NamedIdentityBindingKey = "changed"
			case "admission revision":
				admission := auth.UserAdmissions[identity.UserID]
				admission.Revision = "changed"
				auth.UserAdmissions[identity.UserID] = admission
			}
			auth.TeamSessions[identity.session.TokenHash] = proof
			if err := svc.ReauthorizePersonalSession(context.Background(), identity, "mdl_one"); err == nil {
				t.Fatal("captured authentication or admission proof was not bound")
			}
		})
	}
	svc, _, cookie, identity := personalSessionFixture(t, "http://127.0.0.1/v1")
	svc.runtime.auth.Load().ValidUntil = time.Now().Add(-time.Second)
	requireTeamSessionError(t, svc.ReauthorizePersonalSession(context.Background(), identity, "mdl_one"), 503, "service_unavailable")
	svc.runtime = nil
	_, err := svc.RuntimeAuthenticatePersonalSession(context.Background(), cookie)
	requireTeamSessionError(t, err, 503, "service_unavailable")
	if err := svc.ReauthorizePersonalSession(context.Background(), &PersonalSessionIdentity{SessionID: "ses_personal", UserID: "usr_one", ModelIDs: []string{"mdl_one"}}, "mdl_one"); err == nil {
		t.Fatal("fabricated public identity accepted")
	}
}

func TestPersonalSessionModelsRequireEligibleChatRoutes(t *testing.T) {
	svc, data, _, identity := personalSessionFixture(t, "http://127.0.0.1/v1")
	models, err := svc.PersonalSessionGatewayModels(context.Background(), identity)
	if err != nil || len(models) != 1 || models[0].ModelID != "mdl_one" || len(models[0].Protocols) != 1 || models[0].Protocols[0] != entity.ProtocolOpenAIChat || models[0].PersonalAttachments {
		t.Fatal("exact text Chat catalog unavailable", models, err)
	}
	data.Connections[0].Protocol = entity.ProtocolOpenAIResponses
	routes, err := svc.buildRuntimeRoutes(data)
	if err != nil {
		t.Fatal(err)
	}
	svc.runtime.routes.Store(&runtimeRoutes{ID: "cfg_responses", Models: routes, PublishedAt: time.Now()})
	models, err = svc.PersonalSessionGatewayModels(context.Background(), identity)
	if err != nil || len(models) != 0 {
		t.Fatal("Responses-only model advertised for Chat analysis", models, err)
	}
}

func TestPersonalSessionNativeChatBindsCanonicalModelAndTrueAttribution(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var payload map[string]any
		if json.NewDecoder(r.Body).Decode(&payload) != nil || payload["model"] != "provider-model" || r.Header.Get("Authorization") != "Bearer test-upstream-secret" {
			t.Error("native route or credential changed")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"model": "provider-model", "choices": []any{map[string]any{"message": map[string]string{"role": "assistant", "content": "A bounded observation."}, "finish_reason": "stop"}}, "usage": map[string]int{"prompt_tokens": 10, "completion_tokens": 4}})
	}))
	defer server.Close()
	svc, data, _, identity := personalSessionFixture(t, server.URL+"/v1")
	if _, err := svc.PersonalSessionGatewayChat(context.Background(), identity, "mdl_one", personalAnalysisRequest(t), "req_personal_no_journal"); err == nil || calls.Load() != 0 {
		t.Fatal("unmetered Personal Session was dispatched")
	}
	queue, journal := personalSessionJournal(t, svc, data)
	result, err := svc.PersonalSessionGatewayChat(context.Background(), identity, "mdl_one", personalAnalysisRequest(t), "req_personal_chat")
	if err != nil || result == nil || result.Response == nil {
		t.Fatal("real Personal Session native Chat failed", err)
	}
	_ = result.Response.Body.Close()
	if calls.Load() != 1 || result.UserID != "usr_one" || result.KeyID != "" || result.TeamID != "" || result.TeamMembershipID != "" || result.ProjectID != "" || result.CredentialID != "crd_one" || result.SnapshotID != "cfg_test" || result.AttemptID == "" {
		t.Fatalf("personal attempt lost true attribution: %+v", result)
	}
	if err := queue.Close(); err != nil {
		t.Fatal(err)
	}
	recovered, err := eventqueue.Open(journal, 20, callQueuePayloadLimit)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = recovered.Close() })
	svc.recorder.queue = recovered
	svc.admission = newLocalAdmissionStore(recovered)
	entries, readErr := recovered.Read(10)
	var pending CallFact
	if readErr != nil || len(entries) != 1 || json.Unmarshal(entries[0].Payload, &pending) != nil || pending.UserID != "usr_one" || pending.KeyID != "" || pending.ProjectID != "" || pending.TeamID != "" || len(pending.Attempts) != 1 || pending.Attempts[0].CredentialID != "crd_one" || pending.Attempts[0].SnapshotID != "cfg_test" {
		t.Fatal("durable fallback lost true personal attribution", readErr)
	}
	other := "mdl_other"
	data.Models = append(data.Models, entity.Model{ID: other, CreatedAt: time.Now().UTC(), Status: "active"})
	data.Grants = append(data.Grants, entity.UserModelGrant{UserID: "usr_one", ModelID: other, CreatedAt: time.Now().UTC()})
	data.Names[0].ModelID, data.Names[0].CurrentModelID = other, &other
	svc.runtime.auth.Store(buildRuntimeAuthorization(data, time.Now().Add(time.Minute)))
	fresh, authErr := svc.RuntimeAuthenticatePersonalSession(context.Background(), strings.Repeat("p", 43))
	if authErr != nil {
		t.Fatal(authErr)
	}
	_, err = svc.PersonalSessionGatewayChat(context.Background(), fresh, "mdl_one", personalAnalysisRequest(t), "req_reused_name")
	requireTeamSessionError(t, err, 404, "model_not_found")
	if calls.Load() != 1 {
		t.Fatal("reused name dispatched another granted canonical model")
	}
}

func TestPersonalSessionAdmissionUsesOnlyUserQuotaAndIP(t *testing.T) {
	svc, data, _, identity := personalSessionFixture(t, "http://127.0.0.1/v1")
	ranges, err := json.Marshal([]string{"192.0.2.0/24"})
	if err != nil {
		t.Fatal(err)
	}
	data.Quota.Bounds["pmd_one"] = entity.ReservationBound{ProviderModelID: "pmd_one", Protocol: entity.ProtocolOpenAIChat, MaxInputTokens: 100, MaxOutputTokens: 2048, ETag: "cap_personal"}
	data.Limits = []entity.ResourceLimit{{ScopeKind: "user", ScopeID: "usr_one", Tokens5H: limitNumber(0), RPM: limitNumber(7), Concurrency: limitNumber(1), IPMode: "allowlist", IPRangesJSON: string(ranges)}, {ScopeKind: "key", ScopeID: "key_unused", RPM: limitNumber(0), IPMode: "none", IPRangesJSON: "[]"}}
	if err := compileRuntimeLimits(data); err != nil {
		t.Fatal(err)
	}
	svc.runtime.auth.Store(buildRuntimeAuthorization(data, time.Now().Add(time.Minute)))
	result := gatewayIdentity{personal: identity, selectedModelID: "mdl_one"}.result(entity.ProtocolOpenAIChat, "public-model", false)
	result.ModelID, result.ProviderModelID = "mdl_one", "pmd_one"
	result.quotaRequest = quotaRequest{Supported: true, MaxOutput: 2048}
	ctx := WithGatewayClientIP(context.Background(), netip.MustParseAddr("192.0.2.7"))
	accounts, err := svc.gatewayLimits(ctx, result)
	if err != nil || len(accounts) != 1 || accounts[0].Account != "user_usr_one" || *accounts[0].RPM != 7 || *accounts[0].Concurrency != 1 {
		t.Fatal("personal admission borrowed another account", accounts, err)
	}
	quota, _, err := svc.gatewayQuotaPolicies(ctx, result, accounts)
	if err != nil || len(quota) != 1 || quota[0].Tokens5H == nil || *quota[0].Tokens5H != 0 || quota[0].PersonalKey || quota[0].ProjectKey || quota[0].TeamMember || !quota[0].CreatedAt.Equal(data.Users[0].CreatedAt) {
		t.Fatal("personal quota identity or zero policy changed", quota, err)
	}
	_, err = svc.gatewayLimits(WithGatewayClientIP(context.Background(), netip.MustParseAddr("198.51.100.1")), result)
	requireTeamSessionError(t, err, 403, "ip_not_allowed")
	queue, err := eventqueue.Open(filepath.Join(t.TempDir(), "personal-calls.db"), 10, callQueuePayloadLimit)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = queue.Close() }()
	if err := queue.EnableQuota("UTC", data.Users[0].CreatedAt.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	svc.admission = newLocalAdmissionStore(queue)
	svc.recorder = &callRecorder{queue: queue}
	requireTeamSessionError(t, svc.admitLimitedGatewayCall(ctx, "req_personal_zero_quota", result), 429, "quota_exceeded")
	if depth, _ := queue.Depth(); depth != 0 {
		t.Fatal("rejected personal call reserved journal")
	}
	for _, mutate := range []func(*GatewayResult){func(r *GatewayResult) { r.KeyID = "key_unused" }, func(r *GatewayResult) { r.TeamID = "tem_one" }, func(r *GatewayResult) { r.ProjectID = "prj_one" }, func(r *GatewayResult) { r.UserID = "usr_other" }} {
		copy := *result
		mutate(&copy)
		if _, err := svc.gatewayLimits(ctx, &copy); !errors.Is(err, runtimeUnavailable) {
			t.Fatal("forged personal account accepted", err)
		}
	}
	svc.denyLimitScope("user", "usr_one")
	if _, err := svc.gatewayLimits(ctx, result); !errors.Is(err, runtimeUnavailable) {
		t.Fatal("unpublished user reduction bypassed", err)
	}
}

func TestPersonalSessionPreparedAttemptAndFinalCheckpointReauthorize(t *testing.T) {
	svc, data, _, identity := personalSessionFixture(t, "http://127.0.0.1/v1")
	plan, err := svc.gatewayAttemptPlan("mdl_one", entity.ProtocolOpenAIChat)
	if err != nil {
		t.Fatal(err)
	}
	defer plan.releaseSources()
	plan.personal, plan.userID = identity, identity.UserID
	candidate := plan.Candidates()[0]
	filtered, err := plan.filtered(plan.Candidates())
	if err != nil {
		t.Fatal(err)
	}
	if filtered.personal != identity {
		t.Fatal("quota filtering discarded personal proof")
	}
	if eligible, err := svc.gatewayAttemptEligible(context.Background(), filtered, candidate.attempt); err != nil || !eligible {
		t.Fatal("initial candidate denied", err)
	}
	data.Users[0].PersonalGrantRevision = strings.Repeat("b", 64)
	svc.runtime.auth.Store(buildRuntimeAuthorization(data, time.Now().Add(time.Minute)))
	if eligible, err := svc.gatewayAttemptEligible(context.Background(), filtered, candidate.attempt); err == nil || eligible {
		t.Fatal("prepared attempt survived grant generation change")
	}
	result := gatewayIdentity{personal: identity, selectedModelID: "mdl_one"}.result(entity.ProtocolOpenAIChat, "public-model", false)
	result.ModelID = "mdl_one"
	if err := svc.reauthorizeGatewayPublicName(context.Background(), result); err == nil {
		t.Fatal("final dispatch accepted stale personal authority")
	}
	if _, err := svc.gatewayLimits(context.Background(), result); err == nil {
		t.Fatal("final admission accepted stale personal authority")
	}
}

func TestPersonalSessionAnalysisRejectsExtraNativeCapabilities(t *testing.T) {
	svc, _, _, identity := personalSessionFixture(t, "http://127.0.0.1/v1")
	var valid map[string]any
	if json.Unmarshal(personalAnalysisRequest(t), &valid) != nil {
		t.Fatal("invalid fixture")
	}
	for name, mutate := range map[string]func(map[string]any){
		"stream":                 func(p map[string]any) { p["stream"] = true },
		"tool":                   func(p map[string]any) { p["tools"] = []any{} },
		"workspace":              func(p map[string]any) { p["team_id"] = "tem_one" },
		"missing finite maximum": func(p map[string]any) { delete(p, "max_completion_tokens") },
		"unbounded maximum":      func(p map[string]any) { p["max_completion_tokens"] = 4097 },
		"history": func(p map[string]any) {
			p["messages"] = []map[string]string{{"role": "system", "content": "fixed"}, {"role": "assistant", "content": "old"}, {"role": "user", "content": "new"}}
		},
		"attachment": func(p map[string]any) {
			p["messages"] = []any{map[string]string{"role": "system", "content": "fixed"}, map[string]any{"role": "user", "content": []any{map[string]any{"type": "image_url", "image_url": map[string]string{"url": "https://example.invalid/image.png"}}}}}
		},
	} {
		t.Run(name, func(t *testing.T) {
			payload := map[string]any{}
			for key, value := range valid {
				payload[key] = value
			}
			mutate(payload)
			body, err := json.Marshal(payload)
			if err != nil {
				t.Fatal(err)
			}
			result, err := svc.PersonalSessionGatewayChat(context.Background(), identity, "mdl_one", body, "req_personal_rejected")
			if err == nil || result != nil && result.AttemptID != "" {
				t.Fatal("unsupported payload reached native dispatch")
			}
		})
	}
}

func TestPersonalSessionRetryRechecksGrantRevocation(t *testing.T) {
	var calls atomic.Int32
	var svc *Service
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		svc.runtime.deniedPersonalGrants.Store("usr_one", uint64(1))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": "invalid_api_key"}})
	}))
	defer server.Close()
	var data *runtimeData
	var identity *PersonalSessionIdentity
	svc, data, _, identity = personalSessionFixture(t, server.URL+"/v1")
	addSecondGatewayAttemptRoute(t, svc, data, server.URL+"/v1")
	_, _ = personalSessionJournal(t, svc, data)
	result, err := svc.PersonalSessionGatewayChat(context.Background(), identity, "mdl_one", personalAnalysisRequest(t), "req_personal_revoked_retry")
	if err == nil || calls.Load() != 1 || result == nil || result.Response != nil || len(result.Attempts) != 1 || result.Attempts[0].CredentialID == "" || result.Attempts[0].SnapshotID != "cfg_attempt_execution" || result.Attempts[0].WorkEvidence != "rejected_without_work" {
		t.Fatal("retry survived personal grant revocation or lost first attempt", calls.Load(), err)
	}
}

func TestPersonalSessionCancellationSettlesOnlyActualNoWork(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer server.Close()
	svc, data, _, identity := personalSessionFixture(t, server.URL+"/v1")
	data.Limits = []entity.ResourceLimit{{ScopeKind: "user", ScopeID: "usr_one", ETag: "policy_personal", Tokens5H: limitNumber(3000)}}
	data.Quota.Bounds["pmd_one"] = entity.ReservationBound{ProviderModelID: "pmd_one", Protocol: entity.ProtocolOpenAIChat, MaxInputTokens: 100, MaxOutputTokens: 2048, ETag: "bound_personal"}
	if err := compileRuntimeLimits(data); err != nil {
		t.Fatal(err)
	}
	svc.runtime.auth.Store(buildRuntimeAuthorization(data, time.Now().Add(time.Minute)))
	queue, _ := personalSessionJournal(t, svc, data)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc.afterGatewayAdmission = cancel
	started := time.Now().UTC()
	result, err := svc.PersonalSessionGatewayChat(ctx, identity, "mdl_one", personalAnalysisRequest(t), "req_personal_cancel")
	if err == nil || result == nil || !result.Admitted || !result.NoUpstreamWork() || calls.Load() != 0 || result.KeyID != "" || result.TeamID != "" || result.ProjectID != "" || result.RouteStopReason != "canceled" {
		t.Fatal("personal cancellation lost no-work evidence", calls.Load(), err)
	}
	fact := CallFact{RequestID: "req_personal_cancel", SnapshotID: result.SnapshotID, UserID: result.UserID, ModelID: result.ModelID, ModelName: result.ModelName, RouteStopReason: result.RouteStopReason, Protocol: result.NativeProtocol(), Status: "canceled", StartedAt: started, CompletedAt: time.Now().UTC(), NoWork: result.NoUpstreamWork(), PricingUnsupported: result.PricingUnsupported, PricingDimensions: result.PricingDimensions, PriceBasis: result.PriceBasis, ErrorCode: "canceled"}
	if err := svc.PersistGatewayCall(context.Background(), fact); err != nil {
		t.Fatal(err)
	}
	receipt, err := queue.QuotaReceipt(fact.RequestID)
	if err != nil || receipt.State != "complete" || receipt.Actual.Tokens == nil || *receipt.Actual.Tokens != 0 || len(receipt.Accounts) != 1 || receipt.Accounts[0].Account != "user_usr_one" {
		t.Fatal("personal cancellation failed exact user settlement", receipt, err)
	}
}
