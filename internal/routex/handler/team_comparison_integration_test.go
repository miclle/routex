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

type teamComparisonGate struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
	mode    string
}

func (gate *teamComparisonGate) unblock() { gate.once.Do(func() { close(gate.release) }) }

type teamComparisonLane struct {
	protocol string
	cancel   context.CancelFunc
	done     chan *httptest.ResponseRecorder
}

// The real-driver harness owns execution. These are independent native HTTP
// requests, with no batch admission, shared request identity or automatic retry.
func testTeamComparisonLifecycle(t *testing.T, db *gorm.DB) {
	ctx := context.Background()
	protocols := []string{entity.ProtocolOpenAIChat, entity.ProtocolOpenAIResponses, entity.ProtocolAnthropicMessages, entity.ProtocolGeminiGenerateContent}
	var dispatches, storageReads atomic.Int64
	const callback = "test_team_comparison_storage_reads"
	if err := db.Callback().Query().Before("gorm:query").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table == "storage_objects" {
			storageReads.Add(1)
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Callback().Query().Remove(callback) }()
	var gates sync.Map
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dispatches.Add(1)
		protocol := ""
		for _, candidate := range protocols {
			path := teamNativeProtocolPath(candidate, true)
			path = strings.ReplaceAll(path, "team-native-", "native-")
			path, _, _ = strings.Cut(path, "?")
			prefix := "/v1"
			if candidate == entity.ProtocolGeminiGenerateContent {
				prefix = "/v1beta"
			}
			if r.URL.Path == prefix+path {
				protocol = candidate
				break
			}
		}
		if protocol == "" || r.Header.Get("Cookie") != "" || r.Header.Get("X-CSRF-Token") != "" || r.URL.Query().Get("key") != "" {
			t.Errorf("unexpected or privately authenticated native dispatch: %s", r.URL.Path)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		credentialHeader, credential := "Authorization", "Bearer comparison-upstream"
		switch protocol {
		case entity.ProtocolAnthropicMessages:
			credentialHeader, credential = "x-api-key", "comparison-upstream"
		case entity.ProtocolGeminiGenerateContent:
			credentialHeader, credential = "x-goog-api-key", "comparison-upstream"
		}
		if r.Header.Get(credentialHeader) != credential {
			t.Error("native lane lost its actual credential")
		}
		var body map[string]json.RawMessage
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			t.Error("invalid native lane body")
		}
		stream := string(body["stream"]) == "true"
		if protocol == entity.ProtocolGeminiGenerateContent {
			stream = strings.HasSuffix(r.URL.Path, ":streamGenerateContent")
		}
		mode := "completed"
		if value, ok := gates.Load(protocol); ok {
			gate := value.(*teamComparisonGate)
			mode = gate.mode
			// A canceled lane has already delivered native text, so cancellation
			// cannot be misclassified as known zero-work usage.
			if mode == "canceled" {
				contentType, partial := teamNativeProtocolResponse(protocol, "truncated", true)
				w.Header().Set("Content-Type", contentType)
				_, _ = io.WriteString(w, partial)
				w.(http.Flusher).Flush()
			}
			close(gate.entered)
			select {
			case <-gate.release:
			case <-r.Context().Done():
				return
			}
		}
		contentType, raw := teamNativeProtocolResponse(protocol, mode, stream)
		w.Header().Set("Content-Type", contentType)
		_, _ = io.WriteString(w, raw)
	}))
	defer upstream.Close()
	store, err := secretstore.New(bytes.Repeat([]byte{151}, 32))
	if err != nil {
		t.Fatal(err)
	}
	svc, err := service.New(ctx, db, service.WithCredentialStorage(store), service.WithUpstreamPolicy(true))
	if err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	var cancels []context.CancelFunc
	defer func() {
		for _, cancel := range cancels {
			cancel()
		}
		gates.Range(func(_, value any) bool { value.(*teamComparisonGate).unblock(); return true })
		workers.Wait()
		svc.StopRuntime()
		_ = svc.StopCallRecorder()
	}()
	admin, err := svc.Initialize(ctx, "comparison-admin@example.invalid", "comparison-password", "Comparison administrator")
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
	create(&entity.Provider{ID: "prv_comparison", Name: "Comparison native provider"})
	modelIDs := make([]string, 0, len(protocols))
	for _, protocol := range protocols {
		suffix := teamNativeSuffix(protocol)
		connectionID, credentialID, providerModelID, modelID := "con_compare_"+suffix, "crd_compare_"+suffix, "pmd_compare_"+suffix, "mdl_compare_"+suffix
		cipher, err := store.Seal(credentialID, "comparison-upstream")
		if err != nil {
			t.Fatal(err)
		}
		base := upstream.URL + "/v1"
		if protocol == entity.ProtocolGeminiGenerateContent {
			base = upstream.URL + "/v1beta"
		}
		create(
			&entity.ProviderConnection{ID: connectionID, ProviderID: "prv_comparison", Name: suffix, Protocol: protocol, BaseURL: base},
			&entity.ProviderCredential{ID: credentialID, ConnectionID: connectionID, Name: "Verified lane credential", Ciphertext: cipher, Enabled: true, VerificationStatus: "verified"},
			&entity.ProviderModel{ID: providerModelID, ConnectionID: connectionID, UpstreamName: "native-" + suffix},
			&entity.CredentialModelAccess{CredentialID: credentialID, ProviderModelID: providerModelID},
			&entity.Model{ID: modelID, Status: entity.ResourceActive},
			&entity.ModelName{Name: "team-native-" + suffix, ModelID: modelID, CurrentModelID: &modelID},
			&entity.ModelProviderBinding{ID: "mpb_compare_" + suffix, ModelID: modelID, ProviderModelID: providerModelID, Weight: 100},
			&entity.ModelPrice{ID: "price_compare_" + suffix, ProviderModelID: providerModelID, UpdateSource: "api"},
			&entity.ReservationBound{ProviderModelID: providerModelID, Protocol: protocol, MaxInputTokens: 4, MaxOutputTokens: 1, ETag: "bound_compare_" + suffix, Evidence: "Controlled native maximum", Reason: "Mixed lane acceptance"},
		)
		for i, metric := range []string{pricing.Input, pricing.Output, pricing.CacheRead, pricing.CacheWrite} {
			create(&entity.PriceRate{ID: fmt.Sprintf("rate_compare_%s_%d", suffix, i), ModelPriceID: "price_compare_" + suffix, Metric: metric, Tier: pricing.Base, Unit: pricing.Unit, Currency: "USD", Amount: "1000000", Enabled: true})
		}
		modelIDs = append(modelIDs, modelID)
	}
	// Start durable coverage before the Team exists; an old account cannot gain
	// complete monthly history merely by setting a finite policy.
	bearer := "rx_" + strings.Repeat("c", 43)
	create(&entity.UserModelGrant{UserID: admin.User.ID, ModelID: modelIDs[0]}, &entity.APIKey{ID: "key_compare_warmup", UserID: admin.User.ID, Name: "Coverage warmup", Prefix: "rx_masked", TokenHash: secret.SHA256Hex(bearer), Status: entity.KeyActive}, &entity.APIKeyModel{KeyID: "key_compare_warmup", ModelID: modelIDs[0]})
	router := fox.New()
	New(svc).RegisterRoutes(router)
	if err := svc.StartRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	svc.StopRuntime()
	if err := svc.StartCallRecorder(ctx, filepath.Join(t.TempDir(), "team-comparison.db")); err != nil {
		t.Fatal(err)
	}
	warmup := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(teamNativeProtocolBody(protocols[0], true)))
	warmup.Header.Set("Content-Type", "application/json")
	warmup.Header.Set("Authorization", "Bearer "+bearer)
	warmed := httptest.NewRecorder()
	router.ServeHTTP(warmed, warmup)
	expectStatus(t, warmed, 200)
	if err := svc.FlushCallRecorder(ctx); err != nil {
		t.Fatal(err)
	}
	member, cookie, csrf := createSystemStatusMember(t, svc, router, admin.User.ID, "comparison-member", nil)
	const teamID, membershipID = "tem_comparison", "tmm_comparison_member"
	create(&entity.Team{ID: teamID, Name: "Native comparison Team", Status: entity.ResourceActive}, &entity.TeamMembership{ID: "tmm_comparison_owner", TeamID: teamID, UserID: admin.User.ID, Role: entity.TeamOwner, Status: entity.ResourceActive}, &entity.TeamMembership{ID: membershipID, TeamID: teamID, UserID: member.User.ID, Role: entity.TeamMember, Status: entity.ResourceActive})
	for _, modelID := range modelIDs {
		create(&entity.TeamModelGrant{TeamID: teamID, ModelID: modelID})
	}
	refresh := func() {
		t.Helper()
		if err := svc.RefreshRuntime(ctx); err != nil {
			t.Fatal(err)
		}
	}
	refresh()
	read := func(userID string) *service.LimitRecord {
		t.Helper()
		value, err := svc.GetTeamResourceLimit(ctx, admin.User.ID, teamID, userID)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	write := func(userID, patch string) {
		t.Helper()
		var input service.TeamLimitInput
		if err := json.Unmarshal([]byte(patch), &input); err != nil {
			t.Fatal(err)
		}
		value, err := svc.SetTeamResourceLimit(ctx, admin.User.ID, teamID, userID, read(userID).ETag, input)
		if err != nil || !value.Enforced {
			t.Fatal("mixed native policy was not applied", value, err)
		}
	}
	zero := int64(0)
	personalTarget := service.LimitTarget{Kind: "user", ID: member.User.ID}
	personalBefore, err := svc.GetResourceLimit(ctx, admin.User.ID, personalTarget)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetResourceLimit(ctx, admin.User.ID, personalTarget, personalBefore.ETag, service.LimitInput{Policy: limits.Policy{TokensMonth: &zero, RPM: &zero}, Reason: "Independent Personal denial"}); err != nil {
		t.Fatal(err)
	}
	write("", `{"tokens_month":100,"money_month":"100","currency":"USD","tpm":1000,"rpm":100,"concurrency":4,"reason":"Shared finite Team comparison"}`)
	write(member.User.ID, `{"tokens_month":100,"money_month":"100","currency":"USD","rpm":100,"concurrency":4,"reason":"Stable finite comparison member"}`)
	pairAccount := read(member.User.ID).AccountID
	newGate := func(protocol, mode string) *teamComparisonGate {
		gate := &teamComparisonGate{entered: make(chan struct{}), release: make(chan struct{}), mode: mode}
		gates.Store(protocol, gate)
		return gate
	}
	start := func(protocol string) teamComparisonLane {
		t.Helper()
		// Renew the real lease before each dispatch; slow driver time must not
		// make an authorization or quota refusal pass by lease expiration.
		refresh()
		bounded, cancel := context.WithTimeout(ctx, 10*time.Second)
		cancels = append(cancels, cancel)
		request := httptest.NewRequestWithContext(bounded, "POST", "http://routex.test/api/v1/teams/"+teamID+teamNativeProtocolPath(protocol, true), strings.NewReader(teamNativeProtocolBody(protocol, true)))
		request.AddCookie(cookie)
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Origin", "http://routex.test")
		request.Header.Set("Sec-Fetch-Site", "same-origin")
		request.Header.Set("X-CSRF-Token", csrf)
		request.Header.Set("anthropic-version", "2023-06-01")
		lane := teamComparisonLane{protocol: protocol, cancel: cancel, done: make(chan *httptest.ResponseRecorder, 1)}
		workers.Add(1)
		go func() {
			defer workers.Done()
			defer cancel()
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			lane.done <- response
		}()
		return lane
	}
	waitEntered := func(gate *teamComparisonGate) {
		t.Helper()
		select {
		case <-gate.entered:
		case <-time.After(5 * time.Second):
			t.Fatal("native lane never entered controlled upstream hold")
		}
	}
	wait := func(lane teamComparisonLane) *httptest.ResponseRecorder {
		t.Helper()
		select {
		case response := <-lane.done:
			return response
		case <-time.After(5 * time.Second):
			t.Fatal("native lane did not finish independently")
			return nil
		}
	}
	assertAccounts := func(used, held, active, rpm int64) {
		t.Helper()
		if err := svc.FlushCallRecorder(ctx); err != nil {
			t.Fatal(err)
		}
		for _, userID := range []string{"", member.User.ID} {
			value := read(userID)
			if value.QuotaUsage == nil || value.QuotaUsage.Month == nil || value.QuotaUsage.Active == nil || !value.QuotaUsage.Month.Covered || value.QuotaUsage.Month.TokensUsed != used || value.QuotaUsage.Month.TokensHeld != held-active*5 || value.QuotaUsage.Active.TokensHeld != active*5 || value.QuotaUsage.Month.TokensUnknown != 0 || value.QuotaUsage.Month.MoneyUnknown != 0 || value.QuotaUsage.Active.TokensUnknown != 0 || value.QuotaUsage.Active.MoneyUnknown != 0 || value.Active == nil || *value.Active != active || value.RPMUsed == nil || *value.RPMUsed != rpm {
				t.Fatalf("mixed native accounts diverged for %q: %+v", userID, value)
			}
			for _, amount := range []struct {
				actual string
				want   string
			}{
				{value.QuotaUsage.Month.MoneyUsed["USD"], fmt.Sprint(used)},
				// Native capacity reserves the rounding margin independently
				// from the exact five-dollar settled charge for each call.
				{value.QuotaUsage.Month.MoneyHeld["USD"], teamComparisonHeldMoney((held - active*5) / 5)},
				{value.QuotaUsage.Active.MoneyHeld["USD"], teamComparisonHeldMoney(active)},
			} {
				if amount.actual != amount.want && (amount.want != "0" || amount.actual != "") {
					t.Fatalf("mixed native money diverged for %q: %+v", userID, value.QuotaUsage.Month)
				}
			}
		}
	}
	seen := map[string]bool{}
	var saved []entity.CallRecord
	assertFact := func(response *httptest.ResponseRecorder, protocol, membership, status, evidence string, known bool) entity.CallRecord {
		t.Helper()
		if err := svc.FlushCallRecorder(ctx); err != nil {
			t.Fatal(err)
		}
		requestID := response.Header().Get("X-Request-ID")
		if requestID == "" || seen[requestID] {
			t.Fatal("mixed lanes shared or omitted immutable server request identity", requestID)
		}
		seen[requestID] = true
		var call entity.CallRecord
		var attempts []entity.CallAttempt
		if err := db.Take(&call, "request_id = ?", requestID).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Where("request_id = ?", requestID).Find(&attempts).Error; err != nil {
			t.Fatal(err)
		}
		index := slices.Index(protocols, protocol)
		if call.TeamID != teamID || call.UserID != member.User.ID || call.TeamMembershipID != membership || call.KeyID != "" || call.ProjectID != "" || call.ModelID != modelIDs[index] || call.Protocol != protocol || !call.Stream || call.SnapshotID == "" || call.Status != status || len(attempts) != 1 || attempts[0].RequestID != requestID || attempts[0].NativeCompletionEvidence != evidence || attempts[0].Status != status || attempts[0].CredentialID != "crd_compare_"+teamNativeSuffix(protocol) || attempts[0].SnapshotID != call.SnapshotID || attempts[0].FinalUsageKnown != known {
			t.Fatalf("mixed native immutable facts changed: %+v %+v", call, attempts)
		}
		if known && (call.InputTokens == nil || *call.InputTokens != 4 || call.OutputTokens == nil || *call.OutputTokens != 1) || !known && (call.InputTokens != nil || call.OutputTokens != nil) {
			t.Fatal("mixed lane invented or changed terminal numeric usage", call)
		}
		saved = append(saved, call)
		return call
	}
	// Four simultaneous protocol-only lanes consume the same two accounts.
	lanes := make([]teamComparisonLane, 4)
	phaseGates := make([]*teamComparisonGate, 4)
	for i, protocol := range protocols {
		phaseGates[i] = newGate(protocol, "completed")
		lanes[i] = start(protocol)
		waitEntered(phaseGates[i])
	}
	assertAccounts(0, 20, 4, 4)
	for _, i := range []int{3, 1, 0, 2} {
		phaseGates[i].unblock()
		response := wait(lanes[i])
		expectStatus(t, response, 200)
		assertFact(response, protocols[i], membershipID, "success", "completed", true)
	}
	assertAccounts(20, 0, 0, 4)
	// The group is not atomic: two admitted lanes finish while later lanes are
	// refused without dispatch or partially charging either quota account.
	write("", `{"concurrency":2,"reason":"Bounded aggregate comparison"}`)
	for i := range 2 {
		mode := "completed"
		if i == 1 {
			mode = "incomplete"
		}
		phaseGates[i] = newGate(protocols[i], mode)
		lanes[i] = start(protocols[i])
		waitEntered(phaseGates[i])
	}
	beforeDispatch := dispatches.Load()
	for _, protocol := range protocols[2:] {
		expectStatus(t, wait(start(protocol)), 429)
	}
	if dispatches.Load() != beforeDispatch {
		t.Fatal("refused native comparison lane dispatched")
	}
	assertAccounts(20, 10, 2, 6)
	for i := range 2 {
		phaseGates[i].unblock()
		response := wait(lanes[i])
		expectStatus(t, response, 200)
		if i == 1 {
			// Native incomplete output has known terminal usage, but must not
			// be promoted to successful completion by its HTTP status.
			assertFact(response, protocols[i], membershipID, "error", "incomplete", true)
		} else {
			assertFact(response, protocols[i], membershipID, "success", "completed", true)
		}
	}
	assertAccounts(30, 0, 0, 6)
	write("", `{"concurrency":4,"reason":"Observe independent cancellation"}`)
	for i, protocol := range protocols {
		mode := "completed"
		switch i {
		case 0:
			mode = "canceled"
		case 1:
			mode = "truncated"
		}
		phaseGates[i] = newGate(protocol, mode)
		lanes[i] = start(protocol)
		waitEntered(phaseGates[i])
	}
	assertAccounts(30, 20, 4, 10)
	lanes[0].cancel()
	assertFact(wait(lanes[0]), protocols[0], membershipID, "canceled", "unknown", false)
	phaseGates[1].unblock()
	truncated := wait(lanes[1])
	expectStatus(t, truncated, 200)
	// HTTP 200 does not prove native completion or known usage.
	assertFact(truncated, protocols[1], membershipID, "error", "unknown", false)
	for _, i := range []int{2, 3} {
		select {
		case <-lanes[i].done:
			t.Fatal("cancellation or truncation prematurely finished a sibling")
		default:
		}
		phaseGates[i].unblock()
		response := wait(lanes[i])
		expectStatus(t, response, 200)
		assertFact(response, protocols[i], membershipID, "success", "completed", true)
	}
	assertAccounts(40, 10, 0, 10)
	// Revoking one lane's Model prevents new dispatch, while already dispatched
	// siblings retain their recorded authority and may finish normally.
	gate := newGate(protocols[2], "completed")
	sibling := start(protocols[2])
	waitEntered(gate)
	if _, err := svc.SetResourceModels(ctx, admin.User.ID, service.TeamResource, teamID, modelIDs[1:]); err != nil {
		t.Fatal(err)
	}
	beforeDispatch = dispatches.Load()
	expectStatus(t, wait(start(protocols[0])), 404)
	if dispatches.Load() != beforeDispatch {
		t.Fatal("revoked Model dispatched another comparison lane")
	}
	assertAccounts(40, 15, 1, 11)
	gate.unblock()
	response := wait(sibling)
	expectStatus(t, response, 200)
	assertFact(response, protocols[2], membershipID, "success", "completed", true)
	assertAccounts(45, 10, 0, 11)
	refresh()
	captured, err := svc.RuntimeAuthenticateTeamSession(ctx, cookie.Value, teamID)
	if err != nil {
		t.Fatal(err)
	}
	owner := service.TeamMemberInput{UserID: admin.User.ID, Role: entity.TeamOwner, Status: entity.ResourceActive}
	if _, err := svc.SetTeamMembers(ctx, admin.User.ID, teamID, []service.TeamMemberInput{owner}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetTeamMembers(ctx, admin.User.ID, teamID, []service.TeamMemberInput{owner, {UserID: member.User.ID, Role: entity.TeamMember, Status: entity.ResourceActive}}); err != nil {
		t.Fatal(err)
	}
	refresh()
	rejoined, err := svc.RuntimeAuthenticateTeamSession(ctx, cookie.Value, teamID)
	if err != nil || rejoined.TeamMembershipID == captured.TeamMembershipID {
		t.Fatal("rejoin revived the original comparison membership", rejoined, err)
	}
	beforeDispatch = dispatches.Load()
	for _, protocol := range protocols {
		var result *service.GatewayResult
		var err error
		body := []byte(teamNativeProtocolBody(protocol, true))
		switch protocol {
		case entity.ProtocolOpenAIChat:
			result, err = svc.TeamGatewayChat(ctx, captured, body, "req_compare_old_chat")
		case entity.ProtocolOpenAIResponses:
			result, err = svc.TeamGatewayResponses(ctx, captured, body, "req_compare_old_responses")
		case entity.ProtocolAnthropicMessages:
			result, err = svc.TeamGatewayMessages(ctx, captured, body, "req_compare_old_messages", service.MessagesHeaders{Version: "2023-06-01"})
		case entity.ProtocolGeminiGenerateContent:
			result, err = svc.TeamGatewayGemini(ctx, captured, body, "req_compare_old_gemini", "team-native-gemini", true)
		}
		if err == nil || result != nil && (result.Admitted || result.Response != nil) {
			t.Fatal("old comparison membership reserved or dispatched after rejoin", protocol, result, err)
		}
	}
	if dispatches.Load() != beforeDispatch || read(member.User.ID).AccountID != pairAccount {
		t.Fatal("rejoin borrowed old authority or reset stable pair accounting")
	}
	gates.Delete(protocols[3])
	response = wait(start(protocols[3]))
	expectStatus(t, response, 200)
	assertFact(response, protocols[3], rejoined.TeamMembershipID, "success", "completed", true)
	assertAccounts(50, 10, 0, 12)
	for _, original := range saved {
		var current entity.CallRecord
		if err := db.Take(&current, "request_id = ?", original.RequestID).Error; err != nil || !reflect.DeepEqual(original, current) {
			t.Fatal("comparison history changed after grant or membership replacement", original.RequestID, err)
		}
	}
	personalAfter, err := svc.GetResourceLimit(ctx, admin.User.ID, personalTarget)
	if err != nil || personalAfter.QuotaUsage == nil || personalAfter.QuotaUsage.Month == nil || personalAfter.QuotaUsage.Month.TokensUsed != 0 || personalAfter.QuotaUsage.Month.TokensHeld != 0 || personalAfter.QuotaUsage.Month.TokensUnknown != 0 || personalAfter.QuotaUsage.Month.MoneyUnknown != 0 || personalAfter.Stored.TokensMonth == nil || *personalAfter.Stored.TokensMonth != 0 || personalAfter.Stored.RPM == nil || *personalAfter.Stored.RPM != 0 {
		t.Fatal("comparison consumed or changed Personal policy", personalAfter, err)
	}
	for _, target := range []any{&entity.CallRecord{}, &entity.UserModelGrant{}, &entity.APIKey{}} {
		query := db.Model(target).Where("user_id = ?", member.User.ID)
		if _, ok := target.(*entity.CallRecord); ok {
			query = query.Where("team_id = ?", "")
		}
		var count int64
		if err := query.Count(&count).Error; err != nil || count != 0 {
			t.Fatal("comparison created Personal access or history", target, count, err)
		}
	}
	if storageReads.Load() != 0 {
		t.Fatal("text comparison resolved an attachment", storageReads.Load())
	}
	var keyModels []entity.APIKeyModel
	if err := db.Where("key_id = ?", "key_compare_warmup").Find(&keyModels).Error; err != nil || len(keyModels) != 1 || keyModels[0].ModelID != modelIDs[0] {
		t.Fatal("Team comparison expanded an existing Personal Key ceiling", keyModels, err)
	}
}

func teamComparisonHeldMoney(lanes int64) string {
	// Each supported text shape reaches at most four separately rounded rates:
	// 4 input + 1 output at $1/token, plus a two-unit 10^-18 rounding margin.
	return map[int64]string{0: "0", 1: "5.000000000000000002", 2: "10.000000000000000004", 4: "20.000000000000000008"}[lanes]
}
