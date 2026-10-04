package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
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

type nativeFailoverDispatch struct{ connection, credential, protocol string }

// Root registers this entry in the isolated real-driver harness. Configuration
// rows are setup, not seeded dispatch evidence: every saved attempt below comes
// from a real controlled HTTP exchange through the published runtime.
func testNativeFailoverLifecycle(t *testing.T, db *gorm.DB) {
	ctx := context.Background()
	protocols := []string{entity.ProtocolOpenAIChat, entity.ProtocolOpenAIResponses, entity.ProtocolAnthropicMessages, entity.ProtocolGeminiGenerateContent}
	var deliveryUnavailable atomic.Bool
	const callback = "native_failover_delivery_outage"
	if err := db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
		if deliveryUnavailable.Load() && tx.Statement.Table == "call_records" {
			_ = tx.AddError(errors.New("controlled call delivery outage"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	// Callback lifetime encloses every recorder/runtime lifetime, including restart.
	defer func() { _ = db.Callback().Create().Remove(callback) }()
	store, err := secretstore.New(bytes.Repeat([]byte{173}, 32))
	if err != nil {
		t.Fatal(err)
	}
	var svc *service.Service
	stop := func() {
		if svc != nil {
			svc.StopRuntime()
			if err := svc.StopCallRecorder(); err != nil {
				t.Error(err)
			}
			svc = nil
		}
	}
	defer stop()
	newService := func() {
		t.Helper()
		stop()
		var err error
		svc, err = service.New(ctx, db, service.WithCredentialStorage(store), service.WithUpstreamPolicy(true))
		if err != nil {
			t.Fatal(err)
		}
	}
	newService()
	admin, err := svc.Initialize(ctx, "failover-admin@example.invalid", "failover-test-password", "Failover administrator")
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
	var observationMu sync.Mutex
	var mode string
	var revocation func() error
	var observed []nativeFailoverDispatch // ServeHTTP is synchronous; only the controlled handler writes this slice.
	var total atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observationMu.Lock()
		defer observationMu.Unlock()
		total.Add(1)
		pieces := strings.Split(strings.TrimPrefix(r.URL.Path, "/"), "/")
		if len(pieces) < 3 {
			t.Error("invalid controlled upstream path")
			w.WriteHeader(400)
			return
		}
		connection := pieces[0]
		protocol := ""
		switch {
		case strings.HasSuffix(r.URL.Path, "/chat/completions"):
			protocol = entity.ProtocolOpenAIChat
		case strings.HasSuffix(r.URL.Path, "/responses"):
			protocol = entity.ProtocolOpenAIResponses
		case strings.HasSuffix(r.URL.Path, "/messages"):
			protocol = entity.ProtocolAnthropicMessages
		case strings.Contains(r.URL.Path, "/models/native-gemini:"):
			protocol = entity.ProtocolGeminiGenerateContent
		}
		credential := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if protocol == entity.ProtocolAnthropicMessages {
			credential = r.Header.Get("x-api-key")
			if r.Header.Get("anthropic-version") != "2023-06-01" {
				t.Error("Messages version missing")
			}
		}
		if protocol == entity.ProtocolGeminiGenerateContent {
			credential = r.Header.Get("x-goog-api-key")
		}
		if protocol == "" || r.Header.Get("Cookie") != "" || r.Header.Get("X-CSRF-Token") != "" || !strings.HasPrefix(credential, "crd_fail_") {
			t.Error("protocol or private credential transport changed")
			w.WriteHeader(400)
			return
		}
		var body map[string]json.RawMessage
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			t.Error("invalid native body")
		}
		if protocol != entity.ProtocolGeminiGenerateContent && string(body["model"]) != fmt.Sprintf("%q", "native-"+teamNativeSuffix(protocol)) {
			t.Error("native public Model rewrite changed")
		}
		observed = append(observed, nativeFailoverDispatch{connection, credential, protocol})
		w.Header().Set("Content-Type", "application/json")
		first := len(observed) == 1
		if first && revocation != nil {
			if err := revocation(); err != nil {
				t.Error(err)
			}
		}
		if mode == "exhaust" || first && (mode == "auth" || mode == "rate" || mode == "revoke" || mode == "contradiction" || mode == "duplicate") {
			status := 401
			if mode == "rate" {
				status = 429
			}
			w.WriteHeader(status)
			_, _ = io.WriteString(w, nativeFailoverRejection(protocol, mode))
			return
		}
		stream := string(body["stream"]) == "true" || strings.Contains(r.URL.Path, ":streamGenerateContent")
		contentType, raw := teamNativeProtocolResponse(protocol, "completed", stream)
		if mode == "truncated" {
			contentType, raw = teamNativeProtocolResponse(protocol, "truncated", true)
		}
		w.Header().Set("Content-Type", contentType)
		_, _ = io.WriteString(w, raw)
	}))
	defer upstream.Close()
	create(&entity.Provider{ID: "prv_failover", Name: "Controlled failover Provider"})
	modelIDs := []string{}
	for _, protocol := range protocols {
		suffix := teamNativeSuffix(protocol)
		modelID := "mdl_fail_" + suffix
		modelIDs = append(modelIDs, modelID)
		create(&entity.Model{ID: modelID, Status: entity.ResourceActive}, &entity.ModelName{Name: "team-native-" + suffix, ModelID: modelID, CurrentModelID: &modelID})
		for lane := range 2 {
			connection := fmt.Sprintf("con_fail_%s_%d", suffix, lane)
			pm := fmt.Sprintf("pmd_fail_%s_%d", suffix, lane)
			base := upstream.URL + "/" + connection + "/v1"
			if protocol == entity.ProtocolGeminiGenerateContent {
				base = upstream.URL + "/" + connection + "/v1beta"
			}
			create(&entity.ProviderConnection{ID: connection, ProviderID: "prv_failover", Name: connection, Protocol: protocol, BaseURL: base}, &entity.ProviderModel{ID: pm, ConnectionID: connection, UpstreamName: "native-" + suffix}, &entity.ModelProviderBinding{ID: fmt.Sprintf("mpb_fail_%s_%d", suffix, lane), ModelID: modelID, ProviderModelID: pm, Weight: 50}, &entity.ModelPrice{ID: "price_" + pm, ProviderModelID: pm, UpdateSource: "api"}, &entity.ReservationBound{ProviderModelID: pm, Protocol: protocol, MaxInputTokens: 4, MaxOutputTokens: 1, ETag: "bound_" + pm, Evidence: "Controlled native 4 input and 1 output", Reason: "Failover quota acceptance"})
			for slot := range 2 {
				id := fmt.Sprintf("crd_fail_%s_%d_%d", suffix, lane, slot)
				cipher, err := store.Seal(id, id)
				if err != nil {
					t.Fatal(err)
				}
				create(&entity.ProviderCredential{ID: id, ConnectionID: connection, Name: id, Priority: slot, Ciphertext: cipher, Enabled: true, VerificationStatus: "verified"}, &entity.CredentialModelAccess{CredentialID: id, ProviderModelID: pm})
			}
			for i, metric := range []string{pricing.Input, pricing.Output, pricing.CacheRead, pricing.CacheWrite} {
				create(&entity.PriceRate{ID: fmt.Sprintf("rate_fail_%s_%d_%d", suffix, lane, i), ModelPriceID: "price_" + pm, Metric: metric, Tier: pricing.Base, Unit: pricing.Unit, Currency: "USD", Amount: "1000000", Enabled: true})
			}
		}
	}
	spool := filepath.Join(t.TempDir(), "native-failover.db")
	start := func() *fox.Engine {
		t.Helper()
		if err := svc.StartRuntime(ctx); err != nil {
			t.Fatal(err)
		}
		if err := svc.StartCallRecorder(ctx, spool); err != nil {
			t.Fatal(err)
		}
		router := fox.New()
		New(svc).RegisterRoutes(router)
		return router
	}
	bearer := "rx_" + strings.Repeat("f", 43)
	create(&entity.UserModelGrant{UserID: admin.User.ID, ModelID: modelIDs[0]}, &entity.APIKey{ID: "key_fail_warmup", UserID: admin.User.ID, Name: "Coverage activation", Prefix: "rx_masked", TokenHash: secret.SHA256Hex(bearer), Status: entity.KeyActive}, &entity.APIKeyModel{KeyID: "key_fail_warmup", ModelID: modelIDs[0]})
	router := start()
	mode = "completed"
	warm := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(teamNativeProtocolBody(protocols[0], false)))
	warm.Header.Set("Content-Type", "application/json")
	warm.Header.Set("Authorization", "Bearer "+bearer)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, warm)
	expectStatus(t, res, 200)
	if err := svc.FlushCallRecorder(ctx); err != nil {
		t.Fatal(err)
	}
	member, cookie, csrf := createSystemStatusMember(t, svc, router, admin.User.ID, "failover-member", nil)
	const teamID = "tem_failover"
	const projectID = "prj_failover"
	const membership = "tmm_failover_member"
	personal := "rx_" + strings.Repeat("p", 43)
	project := "rxp_" + strings.Repeat("j", 43)
	create(&entity.Team{ID: teamID, Name: "Failover Team", Status: entity.ResourceActive}, &entity.TeamMembership{ID: "tmm_failover_owner", TeamID: teamID, UserID: admin.User.ID, Role: entity.TeamOwner, Status: entity.ResourceActive}, &entity.TeamMembership{ID: membership, TeamID: teamID, UserID: member.User.ID, Role: entity.TeamMember, Status: entity.ResourceActive}, &entity.Project{ID: projectID, Name: "Failover Project", Status: entity.ResourceActive, CreatorID: admin.User.ID}, &entity.ProjectManager{ID: "pjm_failover", ProjectID: projectID, UserID: member.User.ID}, &entity.APIKey{ID: "key_fail_personal", UserID: member.User.ID, Name: "Failover Personal", Prefix: "rx_masked", TokenHash: secret.SHA256Hex(personal), Status: entity.KeyActive}, &entity.ProjectKey{ID: "key_fail_project", ProjectID: projectID, CreatorID: member.User.ID, Name: "Failover Project", Prefix: "rxp_masked", TokenHash: secret.SHA256Hex(project), Status: entity.KeyActive, DeliveryMode: "manual"})
	for _, id := range modelIDs {
		create(&entity.UserModelGrant{UserID: member.User.ID, ModelID: id}, &entity.APIKeyModel{KeyID: "key_fail_personal", ModelID: id}, &entity.ProjectKeyModel{KeyID: "key_fail_project", ModelID: id}, &entity.ProjectModelGrant{ProjectID: projectID, ModelID: id}, &entity.TeamModelGrant{TeamID: teamID, ModelID: id})
	}
	if err := svc.RefreshRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	money := "10000"
	finite, rpm, concurrency := int64(10000), int64(1000), int64(1)
	for _, target := range []service.LimitTarget{{Kind: "user", ID: member.User.ID}, {Kind: "project", ID: projectID}} {
		value, err := svc.GetResourceLimit(ctx, admin.User.ID, target)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := svc.SetResourceLimit(ctx, admin.User.ID, target, value.ETag, service.LimitInput{Policy: limits.Policy{TokensMonth: &finite, MoneyMonth: &money, Currency: "USD", RPM: &rpm, Concurrency: &concurrency}, Reason: "One logical native admission"}); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{"", member.User.ID} {
		value, err := svc.GetTeamResourceLimit(ctx, admin.User.ID, teamID, id)
		if err != nil {
			t.Fatal(err)
		}
		var input service.TeamLimitInput
		if err := json.Unmarshal([]byte(`{"tokens_month":10000,"money_month":"10000","currency":"USD","rpm":1000,"concurrency":1,"reason":"One logical native admission"}`), &input); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.SetTeamResourceLimit(ctx, admin.User.ID, teamID, id, value.ETag, input); err != nil {
			t.Fatal(err)
		}
	}
	type savedFact struct {
		call     entity.CallRecord
		attempts []entity.CallAttempt
	}
	var saved []savedFact
	quota := func(scope string) []*service.LimitRecord {
		t.Helper()
		var rows []*service.LimitRecord
		if scope == "team" {
			for _, id := range []string{"", member.User.ID} {
				row, err := svc.GetTeamResourceLimit(ctx, admin.User.ID, teamID, id)
				if err != nil {
					t.Fatal(err)
				}
				rows = append(rows, row)
			}
		} else {
			target := service.LimitTarget{Kind: "user", ID: member.User.ID}
			if scope == "project" {
				target = service.LimitTarget{Kind: "project", ID: projectID}
			}
			row, err := svc.GetResourceLimit(ctx, admin.User.ID, target)
			if err != nil {
				t.Fatal(err)
			}
			rows = append(rows, row)
		}
		return rows
	}
	run := func(t *testing.T, scope, protocol, scenario string, pauseDelivery bool) {
		t.Helper()
		newService()
		router = start()
		observationMu.Lock()
		mode = scenario
		observed = nil
		revocation = nil
		before := total.Load()
		var beforeQuota []*service.LimitRecord
		if scenario == "auth" || scenario == "rate" {
			beforeQuota = quota(scope)
		}
		if scenario == "revoke" {
			revocation = func() error {
				switch scope {
				case "personal":
					return svc.RevokePersonalKey(ctx, member.User.ID, "key_fail_personal")
				case "project":
					return svc.RevokeProjectKey(ctx, member.User.ID, projectID, "key_fail_project")
				default:
					_, err := svc.SetTeamMembers(ctx, admin.User.ID, teamID, []service.TeamMemberInput{{UserID: admin.User.ID, Role: entity.TeamOwner, Status: entity.ResourceActive}})
					return err
				}
			}
		}
		observationMu.Unlock()
		path := "/v1" + teamNativeProtocolPath(protocol, scenario == "truncated")
		if protocol == entity.ProtocolGeminiGenerateContent {
			path = "/v1beta" + teamNativeProtocolPath(protocol, scenario == "truncated")
		}
		if scope == "team" {
			path = "/api/v1/teams/" + teamID + teamNativeProtocolPath(protocol, scenario == "truncated")
		}
		bounded, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		request := httptest.NewRequestWithContext(bounded, "POST", "http://routex.test"+path, strings.NewReader(teamNativeProtocolBody(protocol, scenario == "truncated")))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("anthropic-version", "2023-06-01")
		if scope == "team" {
			request.AddCookie(cookie)
			request.Header.Set("Origin", "http://routex.test")
			request.Header.Set("Sec-Fetch-Site", "same-origin")
			request.Header.Set("X-CSRF-Token", csrf)
		} else {
			key := personal
			if scope == "project" {
				key = project
			}
			switch protocol {
			case entity.ProtocolAnthropicMessages:
				request.Header.Set("x-api-key", key)
			case entity.ProtocolGeminiGenerateContent:
				request.Header.Set("x-goog-api-key", key)
			default:
				request.Header.Set("Authorization", "Bearer "+key)
			}
		}
		deliveryUnavailable.Store(pauseDelivery)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		observationMu.Lock()
		dispatches := append([]nativeFailoverDispatch(nil), observed...)
		observationMu.Unlock()
		expected := int64(1)
		if scenario == "auth" || scenario == "rate" {
			expected = 2
		}
		if scenario == "exhaust" {
			expected = 4
		}
		if total.Load()-before != expected || int64(len(dispatches)) != expected {
			t.Fatalf("%s/%s/%s dispatched %d, expected %d", scope, protocol, scenario, total.Load()-before, expected)
		}
		if scenario == "auth" || scenario == "rate" || scenario == "truncated" {
			expectStatus(t, response, 200)
		} else if response.Code < 400 {
			t.Fatalf("negative native scenario became HTTP success: %s", scenario)
		}
		if scenario == "auth" && (dispatches[0].connection != dispatches[1].connection || dispatches[0].credential == dispatches[1].credential) {
			t.Fatal("auth rejection did not select a different exact credential on same Connection")
		}
		if scenario == "rate" && dispatches[0].connection == dispatches[1].connection {
			t.Fatal("rate rejection reused excluded Connection")
		}
		for _, dispatch := range dispatches {
			if dispatch.protocol != protocol {
				t.Fatal("failover crossed native protocol")
			}
		}
		if pauseDelivery {
			if err := svc.FlushCallRecorder(ctx); err == nil {
				t.Fatal("controlled delivery outage falsely acknowledged completion")
			}
			stop()
			deliveryUnavailable.Store(false)
			newService()
			router = start()
		}
		if err := svc.FlushCallRecorder(ctx); err != nil {
			t.Fatal(err)
		}
		id := response.Header().Get("X-Request-ID")
		if id == "" {
			t.Fatal("missing immutable request ID")
		}
		var call entity.CallRecord
		var attempts []entity.CallAttempt
		if err := db.Take(&call, "request_id = ?", id).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Where("request_id = ?", id).Order("attempt_number").Find(&attempts).Error; err != nil {
			t.Fatal(err)
		}
		if len(attempts) != int(expected) || call.Protocol != protocol || call.ModelID != "mdl_fail_"+teamNativeSuffix(protocol) || call.SnapshotID == "" {
			t.Fatal("logical call lost native attempt plan", call, attempts)
		}
		if scope == "team" {
			if call.TeamID != teamID || call.TeamMembershipID != membership || call.UserID != member.User.ID || call.KeyID != "" || call.ProjectID != "" {
				t.Fatal("Team failover changed captured identity", call)
			}
		} else if scope == "project" {
			if call.ProjectID != projectID || call.KeyID != "key_fail_project" || call.TeamID != "" {
				t.Fatal("Project failover borrowed Personal identity", call)
			}
		} else if call.UserID != member.User.ID || call.KeyID != "key_fail_personal" || call.TeamID != "" || call.ProjectID != "" {
			t.Fatal("Personal attribution changed", call)
		}
		for i, attempt := range attempts {
			if attempt.AttemptNumber != i+1 || attempt.CredentialID != dispatches[i].credential || attempt.ConnectionID != dispatches[i].connection || attempt.SnapshotID == "" || attempt.ID == "" || attempt.RequestID != id || attempt.ProviderModelID == "" {
				t.Fatal("immutable actual attempt attribution changed", attempt)
			}
		}
		switch scenario {
		case "auth", "rate":
			if call.Status != "success" || call.InputTokens == nil || *call.InputTokens != 4 || call.OutputTokens == nil || *call.OutputTokens != 1 || attempts[0].WorkEvidence != "rejected_without_work" || attempts[0].NativeCompletionEvidence != "unknown" || attempts[0].FinalUsageKnown || call.ChargeAmount == nil || *call.ChargeAmount != "5" || call.ChargeCurrency == nil || *call.ChargeCurrency != "USD" || attempts[1].NativeCompletionEvidence != "completed" || !attempts[1].FinalUsageKnown {
				t.Fatal("native rejection or completion was inferred", call, attempts)
			}
			after := quota(scope)
			for i, row := range after {
				prior := beforeQuota[i]
				if prior.RPMUsed == nil || row.RPMUsed == nil || *row.RPMUsed-*prior.RPMUsed != 1 || row.Active == nil || *row.Active != 0 || row.QuotaUsage == nil || row.QuotaUsage.Month == nil || prior.QuotaUsage == nil || prior.QuotaUsage.Month == nil || row.QuotaUsage.Month.TokensUsed-prior.QuotaUsage.Month.TokensUsed != 5 || row.QuotaUsage.Month.TokensHeld != 0 || !row.QuotaUsage.Month.Covered || row.QuotaUsage.Month.MoneyUsed["USD"] != fmt.Sprint(row.QuotaUsage.Month.TokensUsed) || row.QuotaUsage.Month.TokensUnknown != 0 || row.QuotaUsage.Month.MoneyUnknown != 0 {
					t.Fatal("two native attempts duplicated admission/settlement or invented coverage", prior, row)
				}
			}
		case "contradiction", "duplicate":
			if call.RouteStopReason != "unsafe_to_replay" || attempts[0].WorkEvidence != "unknown" || attempts[0].FinalUsageKnown || attempts[0].NativeCompletionEvidence != "unknown" || call.InputTokens != nil || call.OutputTokens != nil {
				t.Fatal("ambiguous rejection manufactured work-free or known usage", call, attempts)
			}
		case "truncated":
			if call.Status == "success" || attempts[0].NativeCompletionEvidence != "unknown" || attempts[0].FinalUsageKnown {
				t.Fatal("accepted truncated stream replayed or became native completion", call, attempts)
			}
		case "exhaust":
			if call.RouteStopReason != "attempt_budget_exhausted" {
				t.Fatal("four-attempt budget not enforced", call)
			}
		}
		saved = append(saved, savedFact{call, attempts})
		memberPath := "/api/v1/calls/" + id
		if scope == "project" {
			memberPath = "/api/v1/projects/" + projectID + "/calls/" + id
		}
		if scope == "team" && scenario != "revoke" {
			memberPath = "/api/v1/teams/" + teamID + "/calls/" + id
		}
		if scenario != "revoke" {
			private := identityRequest(router, "GET", memberPath, "", cookie, "")
			expectStatus(t, private, 200)
			for _, field := range []string{"credential_id", "snapshot_id", "failure_class", "work_evidence", "native_completion_evidence"} {
				if strings.Contains(private.Body.String(), field) {
					t.Fatal("member call DTO exposed private attempt diagnostics", field)
				}
			}
		}
		if scenario == "revoke" && scope == "team" {
			_, err := svc.SetTeamMembers(ctx, admin.User.ID, teamID, []service.TeamMemberInput{{UserID: admin.User.ID, Role: entity.TeamOwner, Status: entity.ResourceActive}, {UserID: member.User.ID, Role: entity.TeamMember, Status: entity.ResourceActive}})
			if err != nil {
				t.Fatal(err)
			}
			var current entity.TeamMembership
			if err := db.Take(&current, "team_id = ? AND user_id = ?", teamID, member.User.ID).Error; err != nil || current.ID == membership {
				t.Fatal("rejoin revived the captured membership", err)
			}
		}
	}
	for _, scope := range []string{"personal", "project", "team"} {
		for _, protocol := range protocols {
			t.Run(scope+"/"+protocol+"/auth", func(t *testing.T) {
				run(t, scope, protocol, "auth", scope == "team" && protocol == entity.ProtocolGeminiGenerateContent)
			})
		}
	}
	for _, protocol := range protocols {
		t.Run(protocol+"/rate", func(t *testing.T) { run(t, "team", protocol, "rate", false) })
	}
	for _, protocol := range protocols {
		for _, scenario := range []string{"contradiction", "duplicate"} {
			t.Run(protocol+"/"+scenario, func(t *testing.T) { run(t, "personal", protocol, scenario, false) })
		}
		t.Run(protocol+"/truncated", func(t *testing.T) { run(t, "team", protocol, "truncated", false) })
	}
	t.Run("bounded_four_attempts", func(t *testing.T) { run(t, "personal", protocols[0], "exhaust", false) })
	for _, scope := range []string{"personal", "project", "team"} {
		t.Run(scope+"/between_attempt_revocation", func(t *testing.T) { run(t, scope, protocols[0], "revoke", false) })
	}
	// Restart and repeated delivery operate only on genuine accepted events. No
	// synthetic RecordCall facts or mutating historical rows stand in for replay.
	beforeRestart := total.Load()
	newService()
	_ = start()
	for range 2 {
		if err := svc.FlushCallRecorder(ctx); err != nil {
			t.Fatal(err)
		}
	}
	for _, original := range saved {
		var call entity.CallRecord
		var attempts []entity.CallAttempt
		if err := db.Take(&call, "request_id = ?", original.call.RequestID).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Where("request_id = ?", call.RequestID).Order("attempt_number").Find(&attempts).Error; err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(call, original.call) || !reflect.DeepEqual(attempts, original.attempts) {
			t.Fatal("restart/rejoin/replay changed immutable native facts", call.RequestID)
		}
	}
	if total.Load() != beforeRestart || total.Load() != 52 {
		t.Fatalf("restart or scenario dispatch accounting changed: got %d, want 52", total.Load())
	}
	var count int64
	if err := db.Model(&entity.CallRecord{}).Count(&count).Error; err != nil || count != 33 {
		t.Fatal("logical native events were duplicated or lost", count, err)
	}
}

func nativeFailoverRejection(protocol, mode string) string {
	errorBody := `{"error":{"code":"invalid_api_key","type":"authentication_error"}}`
	switch protocol {
	case entity.ProtocolAnthropicMessages:
		errorBody = `{"type":"error","error":{"type":"authentication_error","message":"Controlled rejection"}}`
	case entity.ProtocolGeminiGenerateContent:
		errorBody = `{"error":{"code":401,"status":"UNAUTHENTICATED","message":"Controlled rejection"}}`
	}
	if mode == "rate" {
		return strings.NewReplacer("invalid_api_key", "rate_limit_exceeded", "authentication_error", "rate_limit_error", "UNAUTHENTICATED", "RESOURCE_EXHAUSTED", `"code":401`, `"code":429`).Replace(errorBody)
	}
	if mode == "duplicate" {
		switch protocol {
		case entity.ProtocolAnthropicMessages:
			return `{"type":"error","error":{"type":"authentication_error","type":"authentication_error"}}`
		case entity.ProtocolGeminiGenerateContent:
			return `{"error":{"status":"UNAUTHENTICATED","status":"UNAUTHENTICATED"}}`
		default:
			return `{"error":{"code":"invalid_api_key","code":"invalid_api_key"}}`
		}
	}
	if mode == "contradiction" {
		marker := `"usage":{"prompt_tokens":4,"completion_tokens":1},"choices":[{"message":{"content":"Already worked"}}]`
		switch protocol {
		case entity.ProtocolOpenAIResponses:
			marker = `"type":"response.completed","response":{"id":"resp_controlled","object":"response","status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"Already worked"}]}],"usage":{"input_tokens":4,"output_tokens":1}}`
		case entity.ProtocolAnthropicMessages:
			marker = `"usage":{"input_tokens":4,"output_tokens":1},"content":[{"type":"text","text":"Already worked"}]`
		case entity.ProtocolGeminiGenerateContent:
			marker = `"usageMetadata":{"promptTokenCount":4},"candidates":[{"content":{"parts":[{"text":"Already worked"}]}}]`
		}
		return strings.TrimSuffix(errorBody, "}") + "," + marker + "}"
	}
	return errorBody
}
