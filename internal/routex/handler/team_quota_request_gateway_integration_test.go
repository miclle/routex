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
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/limits"
	"github.com/miclle/routex/pkg/pricing"
	"github.com/miclle/routex/pkg/secret"
	"github.com/miclle/routex/pkg/secretstore"
	"gorm.io/gorm"
)

func testTeamQuotaApprovalNativeLifecycle(t *testing.T, db *gorm.DB) {
	ctx := context.Background()
	var failAudit atomic.Bool
	var injectedAudits atomic.Int32
	callback := "test_team_quota_native_audit_failure"
	// Register once before any runtime/recorder starts; toggling an atomic flag
	// preserves GORM's callback list while background delivery is running.
	if err := db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
		event, ok := tx.Statement.Dest.(*entity.AuditEvent)
		if failAudit.Load() && tx.Statement.Table == "audit_events" && ok && event.Action == "team.quota_request.approve" {
			injectedAudits.Add(1)
			_ = tx.AddError(errors.New("controlled final approval audit failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Callback().Create().Remove(callback) })
	store, err := secretstore.New(bytes.Repeat([]byte{126}, 32))
	if err != nil {
		t.Fatal(err)
	}
	var dispatched atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		dispatched.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"Done"},"finish_reason":"stop"}],"usage":{"prompt_tokens":4,"completion_tokens":1,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0}}}`)
	}))
	defer upstream.Close()
	var instances []*service.Service
	defer func() {
		for _, instance := range instances {
			instance.StopRuntime()
			if err := instance.StopCallRecorder(); err != nil {
				t.Error(err)
			}
		}
	}()
	newService := func() *service.Service {
		t.Helper()
		instance, err := service.New(ctx, db, service.WithCredentialStorage(store), service.WithUpstreamPolicy(true))
		if err != nil {
			t.Fatal(err)
		}
		instances = append(instances, instance)
		return instance
	}
	svc := newService()
	admin, err := svc.Initialize(ctx, "quota-native-admin@example.invalid", "quota-native-password", "Quota native administrator")
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
	modelID := "mdl_quota_native"
	bearer := "rx_" + strings.Repeat("q", 43)
	cipher, err := store.Seal("crd_quota_native", "controlled-quota-native-secret")
	if err != nil {
		t.Fatal(err)
	}
	create(
		&entity.Provider{ID: "prv_quota_native", Name: "Controlled quota provider"},
		&entity.ProviderConnection{ID: "con_quota_native", ProviderID: "prv_quota_native", Name: "Native Chat", Protocol: entity.ProtocolOpenAIChat, BaseURL: upstream.URL + "/v1"},
		&entity.ProviderCredential{ID: "crd_quota_native", ConnectionID: "con_quota_native", Name: "Verified", Ciphertext: cipher, Enabled: true, VerificationStatus: "verified"},
		&entity.ProviderModel{ID: "pmd_quota_native", ConnectionID: "con_quota_native", UpstreamName: "native"},
		&entity.CredentialModelAccess{CredentialID: "crd_quota_native", ProviderModelID: "pmd_quota_native"},
		&entity.Model{ID: modelID, Status: entity.ResourceActive},
		&entity.ModelName{Name: "quota-native", ModelID: modelID, CurrentModelID: &modelID},
		&entity.ModelProviderBinding{ID: "bnd_quota_native", ModelID: modelID, ProviderModelID: "pmd_quota_native", Weight: 100},
		&entity.UserModelGrant{UserID: admin.User.ID, ModelID: modelID},
		&entity.APIKey{ID: "key_quota_native", UserID: admin.User.ID, Name: "Coverage warmup", Prefix: "rx_masked", TokenHash: secret.SHA256Hex(bearer), Status: entity.KeyActive},
		&entity.APIKeyModel{KeyID: "key_quota_native", ModelID: modelID},
		&entity.ModelPrice{ID: "price_quota_native", ProviderModelID: "pmd_quota_native", UpdateSource: "api"},
		&entity.ReservationBound{ProviderModelID: "pmd_quota_native", Protocol: entity.ProtocolOpenAIChat, MaxInputTokens: 4, MaxOutputTokens: 1, ETag: "bnd_quota_native", Evidence: "Controlled five-token response", Reason: "Native approval acceptance"},
	)
	for _, metric := range []string{pricing.Input, pricing.Output, pricing.CacheRead, pricing.CacheWrite} {
		create(&entity.PriceRate{ID: "qnr_" + metric, ModelPriceID: "price_quota_native", Metric: metric, Tier: pricing.Base, Unit: pricing.Unit, Currency: "USD", Amount: "0", Enabled: true})
	}
	spool := filepath.Join(t.TempDir(), "team-quota-approval.db")
	if err := svc.StartCallRecorder(ctx, spool); err != nil {
		t.Fatal(err)
	}
	if err := svc.StartRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	router := fox.New()
	New(svc).RegisterRoutes(router)
	body := `{"model":"quota-native","messages":[{"role":"user","content":"Hello"}],"max_completion_tokens":1}`
	warmup := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	warmup.Header.Set("Authorization", "Bearer "+bearer)
	warmup.Header.Set("Content-Type", "application/json")
	warmupResponse := httptest.NewRecorder()
	router.ServeHTTP(warmupResponse, warmup)
	expectStatus(t, warmupResponse, 200)
	if err := svc.FlushCallRecorder(ctx); err != nil {
		t.Fatal(err)
	}
	// Actual activation precedes these identities and Team birth; no inferred
	// historical zero or membership-created coverage is used for finite admission.
	applicant, err := svc.CreateMember(ctx, admin.User.ID, "quota-native-applicant@example.invalid", "quota-native-password", "Quota caller", entity.RoleMember)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := svc.CreateMember(ctx, admin.User.ID, "quota-native-owner@example.invalid", "quota-native-password", "Independent owner", entity.RoleMember)
	if err != nil {
		t.Fatal(err)
	}
	teamID := "tem_quota_native"
	create(
		&entity.Team{ID: teamID, Name: "Native quota Team", Status: entity.ResourceActive},
		&entity.TeamMembership{ID: "tmm_quota_admin", TeamID: teamID, UserID: admin.User.ID, Role: entity.TeamOwner, Status: entity.ResourceActive},
		&entity.TeamMembership{ID: "tmm_quota_owner", TeamID: teamID, UserID: owner.User.ID, Role: entity.TeamOwner, Status: entity.ResourceActive},
		&entity.TeamMembership{ID: "tmm_quota_applicant", TeamID: teamID, UserID: applicant.User.ID, Role: entity.TeamMember, Status: entity.ResourceActive},
		&entity.TeamModelGrant{TeamID: teamID, ModelID: modelID},
	)
	identity, cookie := readIdentity(t, identityRequest(router, "POST", "/api/v1/auth/login", `{"email":"quota-native-applicant@example.invalid","password":"quota-native-password"}`, nil, ""))
	zero := int64(0)
	if _, err := svc.SetResourceLimit(ctx, admin.User.ID, service.LimitTarget{Kind: "user", ID: applicant.User.ID}, "0", service.LimitInput{Policy: limits.Policy{TokensMonth: &zero}, Reason: "Personal isolation"}); err != nil {
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
	write := func(userID string, target int64) {
		t.Helper()
		before := read(userID)
		input := service.TeamLimitInput{Fields: map[string]json.RawMessage{"tokens_month": json.RawMessage(strconv.FormatInt(target, 10))}, Reason: "Controlled monthly ceiling"}
		if _, err := svc.SetTeamResourceLimit(ctx, admin.User.ID, teamID, userID, before.ETag, input); err != nil {
			t.Fatal(err)
		}
	}
	write("", 10)
	write(applicant.User.ID, 5)
	native := func(want int) {
		t.Helper()
		requestCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		request := httptest.NewRequestWithContext(requestCtx, "POST", "http://routex.test/api/v1/teams/"+teamID+"/chat/completions", strings.NewReader(body))
		request.AddCookie(cookie)
		request.Header.Set("X-CSRF-Token", identity.CSRFToken)
		request.Header.Set("Origin", "http://routex.test")
		request.Header.Set("Sec-Fetch-Site", "same-origin")
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		expectStatus(t, response, want)
	}
	assertUsage := func(tokens int64) {
		t.Helper()
		if err := svc.FlushCallRecorder(ctx); err != nil {
			t.Fatal(err)
		}
		for _, userID := range []string{"", applicant.User.ID} {
			value := read(userID)
			if value.QuotaUsage == nil || value.QuotaUsage.Month == nil || !value.QuotaUsage.Month.Covered || value.QuotaUsage.Month.TokensUsed != tokens || value.QuotaUsage.Month.TokensHeld != 0 || value.QuotaUsage.Month.TokensUnknown != 0 {
				t.Fatal("approval did not preserve authoritative settled usage", userID, value)
			}
		}
	}
	native(200)
	assertUsage(5)
	reviewContext, err := svc.GetTeamQuotaRequestContext(ctx, applicant.User.ID, teamID, "tokens")
	if err != nil || !reviewContext.Eligible {
		t.Fatal("finite covered context is not eligible", reviewContext, err)
	}
	created, fresh, err := svc.CreateTeamQuotaRequest(ctx, applicant.User.ID, teamID, reviewContext.ETag, service.TeamQuotaRequestInput{RequestID: "33333333-3333-4333-8333-333333333333", Dimension: "tokens", TargetValue: "15", Reason: "Raise monthly member ceiling"})
	if err != nil || !fresh || created.Status != entity.TeamQuotaRequestPendingOwner {
		t.Fatal("request did not enter owner stage", created, err)
	}
	beforeDispatch := dispatched.Load()
	native(429)
	assertUsage(5)
	if dispatched.Load() != beforeDispatch {
		t.Fatal("pending request dispatched or debited beyond current member cap")
	}
	owners := []string{admin.User.ID, owner.User.ID}
	intents := make([]service.TeamQuotaDecisionInput, len(owners))
	reviews := make([]*service.TeamQuotaRequestDetail, len(owners))
	for i, actorID := range owners {
		reviews[i], err = svc.GetTeamQuotaRequest(ctx, actorID, created.ID, false)
		if err != nil || reviews[i].CurrentStepID == nil {
			t.Fatal(err)
		}
		decisionID := "44444444-4444-4444-8444-444444444444"
		if i == 1 {
			decisionID = "55555555-5555-4555-8555-555555555555"
		}
		intents[i] = service.TeamQuotaDecisionInput{DecisionID: decisionID, StepID: *reviews[i].CurrentStepID, Action: "approve", Reason: "Owner approves platform escalation"}
	}
	start := make(chan struct{})
	outcomes := make(chan error, len(owners))
	for i, actorID := range owners {
		go func() {
			<-start
			callCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			_, err := svc.DecideTeamQuotaRequest(callCtx, actorID, created.ID, reviews[i].ETag, intents[i])
			outcomes <- err
		}()
	}
	close(start)
	won := 0
	var unexpected []error
	for range owners {
		select {
		case err := <-outcomes:
			if err == nil {
				won++
			} else {
				var conflict *apperrors.Error
				if !errors.As(err, &conflict) || conflict.Code != http.StatusConflict {
					unexpected = append(unexpected, err)
				}
			}
		case <-time.After(10 * time.Second):
			t.Fatal("concurrent owner decisions did not finish")
		}
	}
	if len(unexpected) != 0 {
		t.Fatal("concurrent owner decision failed outside conflict", unexpected)
	}
	if won != 1 {
		t.Fatal("first valid owner decision must win exactly once", won)
	}
	platformReview, err := svc.GetTeamQuotaRequest(ctx, admin.User.ID, created.ID, false)
	if err != nil || platformReview.Status != entity.TeamQuotaRequestPendingAdmin || len(platformReview.Steps) != 2 || platformReview.CurrentStepID == nil {
		t.Fatal("owner race did not produce one bounded platform stage", platformReview, err)
	}
	parentBefore, childBefore := read(""), read(applicant.User.ID)
	if *parentBefore.Stored.TokensMonth != 10 || *childBefore.Stored.TokensMonth != 5 {
		t.Fatal("owner escalation changed policy before platform approval")
	}
	native(429)
	assertUsage(5)
	if dispatched.Load() != beforeDispatch {
		t.Fatal("owner escalation dispatched above the unchanged member cap")
	}
	finalIntent := service.TeamQuotaDecisionInput{DecisionID: "66666666-6666-4666-8666-666666666666", StepID: *platformReview.CurrentStepID, Action: "approve", Reason: "Atomic aggregate and member raise"}
	var originalRequest entity.TeamQuotaRequest
	var originalStep entity.TeamQuotaRequestStep
	if err := db.First(&originalRequest, "id = ?", created.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&originalStep, "id = ?", finalIntent.StepID).Error; err != nil {
		t.Fatal(err)
	}
	var originalAudits int64
	if err := db.Model(&entity.AuditEvent{}).Count(&originalAudits).Error; err != nil {
		t.Fatal(err)
	}
	failAudit.Store(true)
	_, failure := svc.DecideTeamQuotaRequest(ctx, admin.User.ID, created.ID, platformReview.ETag, finalIntent)
	failAudit.Store(false)
	if failure == nil || injectedAudits.Load() != 1 {
		t.Fatal("controlled final approval audit failure was ignored")
	}
	if !reflect.DeepEqual(parentBefore.Stored, read("").Stored) || !reflect.DeepEqual(childBefore.Stored, read(applicant.User.ID).Stored) || parentBefore.ETag != read("").ETag || childBefore.ETag != read(applicant.User.ID).ETag {
		t.Fatal("failed final transaction partially changed aggregate/member policies")
	}
	var retainedRequest entity.TeamQuotaRequest
	var retainedStep entity.TeamQuotaRequestStep
	if err := db.First(&retainedRequest, "id = ?", created.ID).Error; err != nil || !reflect.DeepEqual(originalRequest, retainedRequest) {
		t.Fatal("failed final transaction changed request receipt", err)
	}
	if err := db.First(&retainedStep, "id = ?", finalIntent.StepID).Error; err != nil || !reflect.DeepEqual(originalStep, retainedStep) {
		t.Fatal("failed final transaction changed stage receipt", err)
	}
	var slots int64
	if err := db.Model(&entity.TeamQuotaPendingSlot{}).Where("request_id = ?", created.ID).Count(&slots).Error; err != nil || slots != 1 {
		t.Fatal("failed final transaction released pending slot", err)
	}
	var retainedAudits int64
	if err := db.Model(&entity.AuditEvent{}).Count(&retainedAudits).Error; err != nil || retainedAudits != originalAudits {
		t.Fatal("failed final transaction retained partial policy audits", err)
	}
	approved, err := svc.DecideTeamQuotaRequest(ctx, admin.User.ID, created.ID, platformReview.ETag, finalIntent)
	if err != nil || !approved.Committed || approved.Request.Status != entity.TeamQuotaRequestApproved || approved.Request.Application == nil || !approved.Request.Application.RuntimeApplied {
		t.Fatal("identical final intent was not applied after rollback", approved, err)
	}
	if *read("").Stored.TokensMonth != 15 || *read(applicant.User.ID).Stored.TokensMonth != 15 {
		t.Fatal("platform approval did not raise both exact monthly ceilings")
	}
	native(200)
	native(200)
	assertUsage(15)
	beforeDispatch = dispatched.Load()
	native(429)
	if dispatched.Load() != beforeDispatch {
		t.Fatal("approved aggregate/monthly ceiling did not prevent dispatch")
	}
	write("", 25)
	write(applicant.User.ID, 20)
	assertSuperseded := func() {
		t.Helper()
		receipt, err := svc.DecideTeamQuotaRequest(ctx, admin.User.ID, created.ID, platformReview.ETag, finalIntent)
		if err != nil || !receipt.Committed || receipt.Request.Application == nil || receipt.Request.Application.RuntimeApplied || receipt.Request.Application.ApplicationStatus != "superseded" || *read("").Stored.TokensMonth != 25 || *read(applicant.User.ID).Stored.TokensMonth != 20 {
			t.Fatal("historical receipt restored superseded policy", receipt, err)
		}
	}
	assertSuperseded()
	native(200)
	assertUsage(20)
	beforeDispatch = dispatched.Load()
	native(429)
	svc.StopRuntime()
	if err := svc.StopCallRecorder(); err != nil {
		t.Fatal(err)
	}
	svc = newService()
	if err := svc.StartCallRecorder(ctx, spool); err != nil {
		t.Fatal(err)
	}
	if err := svc.StartRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	router = fox.New()
	New(svc).RegisterRoutes(router)
	assertSuperseded()
	assertUsage(20)
	native(429)
	if dispatched.Load() != beforeDispatch {
		t.Fatal("journal restart reset used quota or dispatched a rejected call")
	}
	personal, err := svc.GetResourceLimit(ctx, admin.User.ID, service.LimitTarget{Kind: "user", ID: applicant.User.ID})
	if err != nil || personal.QuotaUsage == nil || personal.QuotaUsage.Month == nil || personal.QuotaUsage.Month.TokensUsed != 0 {
		t.Fatal("Team approvals or inference debited the Personal account", personal, err)
	}
	// Opposite decisions compete for the same owner step after restart. Neither
	// outcome changes live policy: approval only escalates, rejection is terminal.
	reviewContext, err = svc.GetTeamQuotaRequestContext(ctx, applicant.User.ID, teamID, "tokens")
	if err != nil || !reviewContext.Eligible {
		t.Fatal("restarted finite context is unavailable", reviewContext, err)
	}
	competing, fresh, err := svc.CreateTeamQuotaRequest(ctx, applicant.User.ID, teamID, reviewContext.ETag, service.TeamQuotaRequestInput{
		RequestID: "77777777-7777-4777-8777-777777777777", Dimension: "tokens", TargetValue: "30", Reason: "Competing monthly decisions",
	})
	if err != nil || !fresh || competing.Status != entity.TeamQuotaRequestPendingOwner {
		t.Fatal("competing request did not enter the owner stage", competing, err)
	}
	parentBefore, childBefore = read(""), read(applicant.User.ID)
	beforeDispatch = dispatched.Load()
	for i, actorID := range owners {
		reviews[i], err = svc.GetTeamQuotaRequest(ctx, actorID, competing.ID, false)
		if err != nil || reviews[i].CurrentStepID == nil {
			t.Fatal("owner cannot review competing stage", err)
		}
		intents[i] = service.TeamQuotaDecisionInput{
			DecisionID: "88888888-8888-4888-8888-888888888888", StepID: *reviews[i].CurrentStepID, Action: "approve", Reason: "Approve escalation",
		}
		if i == 1 {
			intents[i].DecisionID = "99999999-9999-4999-8999-999999999999"
			intents[i].Action, intents[i].Reason = "reject", "Reject monthly adjustment"
		}
	}
	type competingDecision struct {
		actorID string
		intent  service.TeamQuotaDecisionInput
		receipt *service.TeamQuotaDecisionRecord
		err     error
	}
	competingStart := make(chan struct{})
	competingOutcomes := make(chan competingDecision, len(owners))
	for i, actorID := range owners {
		go func() {
			<-competingStart
			callCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			receipt, err := svc.DecideTeamQuotaRequest(callCtx, actorID, competing.ID, reviews[i].ETag, intents[i])
			competingOutcomes <- competingDecision{actorID: actorID, intent: intents[i], receipt: receipt, err: err}
		}()
	}
	close(competingStart)
	var winner competingDecision
	won, unexpected = 0, nil
	for range owners {
		select {
		case outcome := <-competingOutcomes:
			if outcome.err == nil {
				won++
				winner = outcome
			} else {
				var conflict *apperrors.Error
				if !errors.As(outcome.err, &conflict) || conflict.Code != http.StatusConflict {
					unexpected = append(unexpected, outcome.err)
				}
			}
		case <-time.After(10 * time.Second):
			t.Fatal("competing approval/rejection did not finish")
		}
	}
	if won != 1 || len(unexpected) != 0 || winner.receipt == nil || !winner.receipt.Committed {
		t.Fatal("competing approval/rejection must commit exactly one decision", won, unexpected)
	}
	result, err := svc.GetTeamQuotaRequest(ctx, applicant.User.ID, competing.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	wantStatus, wantStepStatus, wantSlots, wantSteps := entity.TeamQuotaRequestRejected, entity.TeamQuotaStepRejected, int64(0), 1
	if winner.intent.Action == "approve" {
		wantStatus, wantStepStatus, wantSlots, wantSteps = entity.TeamQuotaRequestPendingAdmin, entity.TeamQuotaStepApproved, 1, 2
	}
	if result.Status != wantStatus || len(result.Steps) != wantSteps || result.Steps[0].Status != wantStepStatus {
		t.Fatal("workflow does not match the winning owner decision", winner.intent.Action, result)
	}
	var decided entity.TeamQuotaRequestStep
	if err := db.Where("request_id = ? AND ordinal = ?", competing.ID, 1).First(&decided).Error; err != nil || decided.DecisionID == nil || *decided.DecisionID != winner.intent.DecisionID || decided.ActorID != winner.actorID || decided.Action != winner.intent.Action {
		t.Fatal("stage did not retain the exact winning human receipt", decided, err)
	}
	var decisions, audits int64
	if err := db.Model(&entity.TeamQuotaRequestStep{}).Where("request_id = ? AND decision_id IS NOT NULL", competing.ID).Count(&decisions).Error; err != nil || decisions != 1 {
		t.Fatal("competing decisions recorded multiple human winners", decisions, err)
	}
	if err := db.Model(&entity.AuditEvent{}).Where("resource_id = ? AND action IN ?", competing.ID, []string{"team.quota_request.approve", "team.quota_request.reject"}).Count(&audits).Error; err != nil || audits != 1 {
		t.Fatal("competing decisions recorded multiple winning audits", audits, err)
	}
	if err := db.Model(&entity.TeamQuotaPendingSlot{}).Where("request_id = ?", competing.ID).Count(&slots).Error; err != nil || slots != wantSlots {
		t.Fatal("pending slot does not follow the winning decision", slots, err)
	}
	if !reflect.DeepEqual(parentBefore.Stored, read("").Stored) || !reflect.DeepEqual(childBefore.Stored, read(applicant.User.ID).Stored) || parentBefore.ETag != read("").ETag || childBefore.ETag != read(applicant.User.ID).ETag {
		t.Fatal("owner decision race changed aggregate/member policy")
	}
	native(429)
	assertUsage(20)
	if dispatched.Load() != beforeDispatch || *read("").Stored.TokensMonth != 25 || *read(applicant.User.ID).Stored.TokensMonth != 20 {
		t.Fatal("competing owner decisions changed caps, usage or upstream dispatch")
	}
}
