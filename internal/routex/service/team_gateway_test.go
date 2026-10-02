package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/eventqueue"
	"github.com/miclle/routex/pkg/limits"
	"github.com/miclle/routex/pkg/secret"
)

func teamGatewayFixture(t *testing.T, base string) (*Service, *TeamSessionIdentity) {
	t.Helper()
	svc, _, _ := runtimeFixture(t, base)
	cookie := strings.Repeat("s", 43)
	expires := time.Now().Add(time.Hour)
	auth := svc.runtime.auth.Load()
	auth.TeamSessions = map[string]runtimeTeamSession{secret.SHA256Hex(cookie): {ID: "ses_team", UserID: "usr_one", TokenHash: secret.SHA256Hex(cookie), ExpiresAt: expires}}
	auth.Teams = map[string]runtimeTeam{"tem_one": {CreatedAt: time.Now().Add(-time.Hour), Members: map[string]string{"usr_one": "tmb_one"}, Models: map[string]bool{"mdl_one": true}}}
	// Personal zero policies must never be inherited by Team Session inference.
	auth.LimitPolicies[limitAccount("user", "usr_one")] = limits.Policy{TokensMonth: limitNumber(0), RPM: limitNumber(0)}
	identity, err := svc.RuntimeAuthenticateTeamSession(context.Background(), cookie, "tem_one")
	if err != nil {
		t.Fatal(err)
	}
	return svc, identity
}

func TestTeamChatTextRejectsNativeMediaWithoutInspectingOpaqueTools(t *testing.T) {
	for _, raw := range []string{
		`{"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://example.invalid/image.png"}}]}]}`,
		`{"messages":[{"role":"user","content":[{"type":"file","file":{"file_data":"routex://attachments/obj_one"}}]}]}`,
		`{"messages":[{"role":"user","content":[{"type":"input_audio","input_audio":{"data":"a"}}]}]}`,
		`{"messages":[{"role":"user","content":[{"type":"future_media","url":"x"}]}]}`,
		`{"messages":[{"role":"assistant","audio":null,"content":"a"}]}`,
		`{"messages":[{"role":"user","content":"a"}],"modalities":["text","audio"]}`,
	} {
		var payload map[string]json.RawMessage
		if err := json.Unmarshal([]byte(raw), &payload); err != nil {
			t.Fatal(err)
		}
		if validateTeamChatText(payload) == nil {
			t.Fatal("accepted native media", raw)
		}
	}
	raw := `{"messages":[{"role":"assistant","content":null,"tool_calls":[{"type":"function","function":{"name":"opaque","arguments":"{\"image_url\":\"routex://attachments/obj_one\"}"}}]},{"role":"tool","content":"opaque image_url data"},{"role":"user","content":[{"type":"text","text":"routex://attachments/obj_one"}]}],"tools":[{"type":"function","function":{"name":"opaque","parameters":{"image_url":{"file_data":"data"}}}}]}`
	var payload map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatal(err)
	}
	if err := validateTeamChatText(payload); err != nil {
		t.Fatal("opaque tool/text data was interpreted as media", err)
	}
	after, _ := json.Marshal(payload)
	if !strings.Contains(string(after), `routex://attachments/obj_one`) {
		t.Fatal("opaque data rewritten")
	}
}

func TestTeamGatewayDiscoveryIsTextScopedAndRechecksRouteReadiness(t *testing.T) {
	svc, identity := teamGatewayFixture(t, "https://provider-one.example/v1")
	svc.db = nil
	svc.secrets = nil
	models, err := svc.TeamGatewayModels(context.Background(), identity)
	if err != nil || len(models) != 1 || models[0].ModelID != "mdl_one" || models[0].ID != "public-model" || models[0].AttachmentScope != "team" || models[0].PersonalAttachments || len(models[0].Protocols) != 1 || len(models[0].InputCapabilities[entity.ProtocolOpenAIChat]) != 0 {
		t.Fatal(models, err)
	}
	svc.egressGeneration.Add(1)
	models, err = svc.TeamGatewayModels(context.Background(), identity)
	if err != nil || len(models) != 0 {
		t.Fatal("stale transport advertised as eligible", models, err)
	}
}

func TestTeamJournalPairIdentityFitsBoundAndNeverInheritsPersonalPolicy(t *testing.T) {
	svc, identity := teamGatewayFixture(t, "https://provider-one.example/v1")
	result := (gatewayIdentity{team: identity}).result(entity.ProtocolOpenAIChat, "public-model", false)
	result.ModelID = "mdl_one"
	scopes, err := svc.gatewayLimits(context.Background(), result)
	if err != nil || len(scopes) != 2 || scopes[0].Account != "team_tem_one" || scopes[1].Account != teamMemberLimitAccount("tem_one", "usr_one") || scopes[0].RPM != nil || scopes[1].RPM != nil {
		t.Fatal(scopes, err)
	}
	policies, _, err := svc.gatewayQuotaPolicies(context.Background(), result, scopes)
	if err != nil || len(policies) != 2 {
		t.Fatal(policies, err)
	}
	for _, policy := range policies {
		if policy.TokensMonth != nil || policy.MoneyMonth != nil || policy.RPM != nil || policy.Concurrency != nil || policy.Revision != "0" || !policy.CreatedAt.Equal(identity.teamCreatedAt) {
			t.Fatal("invented or inherited Team policy", policy)
		}
	}
	pair := teamMemberLimitAccount(strings.Repeat("t", 30), strings.Repeat("u", 30))
	if len(pair) != 64 || !regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`).MatchString(pair) {
		t.Fatal("journal account exceeds established bound", pair)
	}
	if pair == teamMemberLimitAccount(strings.Repeat("u", 30), strings.Repeat("t", 30)) || teamMemberLimitAccount("ab", "c") == teamMemberLimitAccount("a", "bc") {
		t.Fatal("pair identity loses ordered exact scope")
	}
	identity.TeamMembershipID = "tmb_rejoined"
	if teamMemberLimitAccount(identity.TeamID, identity.UserID) != scopes[1].Account {
		t.Fatal("membership generation changes pair account")
	}
}

func TestTeamGatewayRevocationAfterAdmissionPreventsCheckpointAndDispatch(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"object":"chat.completion","choices":[]}`)
	}))
	defer upstream.Close()
	svc, identity := teamGatewayFixture(t, upstream.URL+"/v1")
	queue, err := eventqueue.Open(filepath.Join(t.TempDir(), "team.db"), 10, callQueuePayloadLimit)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = queue.Close() }()
	svc.recorder = &callRecorder{queue: queue}
	svc.afterGatewayAdmission = func() {
		svc.runtime.deniedTeamMembers.Store(teamMemberRuntimeKey(identity.TeamID, identity.UserID), uint64(1))
	}
	result, err := svc.TeamGatewayChat(context.Background(), identity, []byte(`{"model":"public-model","messages":[{"role":"user","content":"hi"}]}`), "req_team_revoked")
	if err == nil || result == nil || !result.Admitted || calls.Load() != 0 || result.AttemptID != "" || len(result.Attempts) != 0 || !result.NoUpstreamWork() {
		t.Fatalf("revoked subject dispatched or fabricated an attempt: %+v, %v, %d", result, err, calls.Load())
	}
	payload, err := gatewayFallbackPayload("req_team_revoked", result, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	var fact CallFact
	if json.Unmarshal(payload, &fact) != nil || fact.TeamID != identity.TeamID || fact.TeamMembershipID != identity.TeamMembershipID || fact.KeyID != "" || fact.ProjectID != "" {
		t.Fatal("interruption fallback loses Team subject")
	}
	for _, forbidden := range []string{identity.SessionID, identity.tokenHash, identity.csrfProofHash} {
		if strings.Contains(string(payload), forbidden) {
			t.Fatal("private Session proof entered journal")
		}
	}
}

func TestTeamGatewayNativeAdmissionAndJournalRestartKeepPersonalLedgerUntouched(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"Done"},"finish_reason":"stop"}],"usage":{"prompt_tokens":4,"completion_tokens":1,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0}}}`)
	}))
	defer upstream.Close()
	svc, identity := teamGatewayFixture(t, upstream.URL+"/v1")
	path := filepath.Join(t.TempDir(), "team.db")
	queue, err := eventqueue.Open(path, 10, callQueuePayloadLimit)
	if err != nil {
		t.Fatal(err)
	}
	svc.recorder = &callRecorder{queue: queue}
	result, err := svc.TeamGatewayChat(context.Background(), identity, []byte(`{"model":"public-model","messages":[{"role":"user","content":"hi"}]}`), "req_team_completed")
	if err != nil || result.Response == nil || result.KeyID != "" || result.ProjectID != "" || calls.Load() != 1 {
		t.Fatal(result, err)
	}
	response, err := io.ReadAll(result.Response.Body)
	_ = result.Response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	usage := ParseOpenAIUsage(response, false)
	fact := CallFact{RequestID: "req_team_completed", TeamID: identity.TeamID, TeamMembershipID: identity.TeamMembershipID, UserID: identity.UserID, ModelID: result.ModelID, ModelName: result.ModelName, Protocol: entity.ProtocolOpenAIChat, Status: "success", StartedAt: result.AttemptStartedAt, CompletedAt: time.Now().UTC(), InputTokens: usage.Input, OutputTokens: usage.Output, CacheReadTokens: usage.CacheRead, CacheWriteTokens: usage.CacheWrite, UsageComplete: usage.Complete}
	if err := svc.PersistGatewayCall(context.Background(), fact); err != nil {
		t.Fatal(err)
	}
	if err := queue.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := eventqueue.Open(path, 10, callQueuePayloadLimit)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reopened.Close() }()
	for _, account := range []string{limitAccount("team", identity.TeamID), teamMemberLimitAccount(identity.TeamID, identity.UserID)} {
		usage, err := reopened.AccountQuotaUsage(account, time.Now().UTC())
		if err != nil || usage.Month.TokensUsed != 5 || usage.Month.TokensUnknown != 0 {
			t.Fatal("Team settlement did not survive restart", usage, err)
		}
	}
	personal, err := reopened.AccountQuotaUsage(limitAccount("user", identity.UserID), time.Now().UTC())
	if err != nil || personal.Month.TokensUsed != 0 || personal.Month.TokensUnknown != 0 {
		t.Fatal("Team call debited Personal journal", personal, err)
	}
	receipt, err := reopened.QuotaReceipt("req_team_completed")
	if err != nil || len(receipt.Accounts) != 2 {
		t.Fatal(receipt, err)
	}
}
