package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
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
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/id"
	"github.com/miclle/routex/pkg/secretstore"
)

func testPersonalModelRequestsLifecycle(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx := context.Background()
	var nativeCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer personal-request-test-secret" {
			t.Error("incorrect controlled upstream authentication")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet && r.URL.Path == "/v1/models" {
			_, _ = io.WriteString(w, `{"data":[{"id":"request-upstream-a"},{"id":"request-upstream-b"}]}`)
			return
		}
		if r.Method != http.MethodPost || r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		var payload struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil || (payload.Model != "request-upstream-a" && payload.Model != "request-upstream-b") {
			t.Error("controlled native request did not preserve exact route", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		nativeCalls.Add(1)
		_, _ = io.WriteString(w, `{"id":"chat-personal-request","object":"chat.completion","model":"`+payload.Model+`","choices":[{"index":0,"message":{"role":"assistant","content":"Accepted"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}}`)
	}))
	defer upstream.Close()
	store, err := secretstore.New([]byte(strings.Repeat("p", 32)))
	if err != nil {
		t.Fatal(err)
	}
	svc, err := service.New(ctx, db, service.WithCredentialStorage(store), service.WithUpstreamPolicy(true), service.WithRuntimeRefreshInterval(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	router := fox.New()
	New(svc).RegisterRoutes(router)
	setup := identityRequest(router, "POST", "/api/v1/setup", `{"email":"personal-request-admin@example.invalid","password":"personal-request-password","name":"Request administrator"}`, nil, "")
	admin, adminCookie := readIdentity(t, setup)
	sendRaw := func(cookie *http.Cookie, csrf, method, path, body, etag string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "http://routex.test"+path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if cookie != nil {
			req.AddCookie(cookie)
		}
		if csrf != "" {
			req.Header.Set("X-CSRF-Token", csrf)
		}
		if etag != "" {
			req.Header.Set("If-Match", `"`+etag+`"`)
		}
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		return res
	}
	send := func(cookie *http.Cookie, csrf, method, path string, body any, etag string) *httptest.ResponseRecorder {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		return sendRaw(cookie, csrf, method, path, string(encoded), etag)
	}
	type requestActor struct {
		auth   SessionResponse
		cookie *http.Cookie
	}
	newActor := func(name string) requestActor {
		t.Helper()
		email := "personal-request-" + name + "@example.invalid"
		if _, err := svc.CreateMember(ctx, admin.User.ID, email, "personal-request-password", name, entity.RoleMember); err != nil {
			t.Fatal(err)
		}
		login := identityRequest(router, "POST", "/api/v1/auth/login", `{"email":"`+email+`","password":"personal-request-password"}`, nil, "")
		auth, cookie := readIdentity(t, login)
		return requestActor{auth, cookie}
	}
	applicant, reviewer, writer, outsider := newActor("applicant"), newActor("reviewer"), newActor("writer"), newActor("outsider")
	as := func(actor requestActor, method, path string, body any, etag string) *httptest.ResponseRecorder {
		return send(actor.cookie, actor.auth.CSRFToken, method, path, body, etag)
	}
	reviewRole, err := svc.SaveRole(ctx, admin.User.ID, "", "Personal model reviewer", []string{"members.models.write"})
	if err != nil {
		t.Fatal(err)
	}
	writeRole, err := svc.SaveRole(ctx, admin.User.ID, "", "Member writer", []string{"members.write"})
	if err != nil {
		t.Fatal(err)
	}
	for _, assignment := range []struct{ user, role string }{{reviewer.auth.User.ID, reviewRole.Role.ID}, {writer.auth.User.ID, writeRole.Role.ID}} {
		if _, err := svc.SetMemberRoles(ctx, admin.User.ID, assignment.user, []string{assignment.role}); err != nil {
			t.Fatal(err)
		}
	}
	provider, err := svc.CreateProvider(ctx, admin.User.ID, "Personal request provider", service.CreateConnectionInput{Name: "Personal request connection", BaseURL: upstream.URL + "/v1", Protocol: entity.ProtocolOpenAIChat, CredentialName: "Controlled request credential", Secret: "personal-request-test-secret"})
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
	modelIDs := map[string]string{}
	for _, letter := range []string{"a", "b"} {
		var pm entity.ProviderModel
		if err := db.Where("connection_id = ? AND upstream_name = ?", provider.Connections[0].Connection.ID, "request-upstream-"+letter).Take(&pm).Error; err != nil {
			t.Fatal(err)
		}
		model, err := svc.CreateModel(ctx, admin.User.ID, "personal-request-"+letter, pm.ID)
		if err != nil {
			t.Fatal(err)
		}
		modelIDs[letter] = model.Model.ID
		if _, err := svc.SetModelWeights(ctx, admin.User.ID, model.Model.ID, []service.ModelWeight{{BindingID: model.Bindings[0].Binding.ID, Weight: 100}}); err != nil {
			t.Fatal(err)
		}
		grantees := []string{}
		if letter == "a" {
			grantees = append(grantees, applicant.auth.User.ID)
		}
		if _, err := svc.SetModelGrants(ctx, admin.User.ID, model.Model.ID, grantees); err != nil {
			t.Fatal(err)
		}
	}
	for _, letter := range []string{"c", "d", "e", "f", "g", "h", "i"} {
		modelID, err := id.NewPrefixed("mdl")
		if err != nil {
			t.Fatal(err)
		}
		modelIDs[letter] = modelID
		for _, row := range []any{&entity.Model{ID: modelID, Status: entity.ResourceActive}, &entity.ModelName{Name: "personal-request-" + letter, ModelID: modelID, CurrentModelID: &modelID}} {
			if err := db.Create(row).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := svc.StartRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	defer svc.StopRuntime()
	const candidates = "/api/v1/model-access-candidates"
	const personal = "/api/v1/personal-model-requests"
	memberPath := func(userID string) string { return "/api/v1/admin/members/" + userID + "/model-requests" }
	workspacePath := "/api/v1/admin/members/" + applicant.auth.User.ID + "/model-access-workspace"
	candidate := func(actor requestActor, modelID string) service.ModelAccessCandidate {
		t.Helper()
		res := as(actor, "GET", candidates+"/"+modelID, nil, "")
		value := decodeCatalogResponse[service.ModelAccessCandidate](t, res, 200)
		if res.Header().Get("ETag") != `"`+value.ReviewETag+`"` || len(value.ReviewETag) != 64 {
			t.Fatal("candidate lacks coherent reviewed validator")
		}
		return value
	}
	detail := func(actor requestActor, userID, requestID string, review bool) service.PersonalModelRequestDetail {
		t.Helper()
		path := personal
		if review {
			path = memberPath(userID)
		}
		return decodeCatalogResponse[service.PersonalModelRequestDetail](t, as(actor, "GET", path+"/"+requestID, nil, ""), 200)
	}
	var intentSequence int
	uuid := func() string { intentSequence++; return fmt.Sprintf("90000000-0000-4000-8000-%012d", intentSequence) }
	create := func(actor requestActor, modelID string) (service.PersonalModelRequestDetail, service.PersonalModelRequestInput, string) {
		t.Helper()
		etag := candidate(actor, modelID).ReviewETag
		input := service.PersonalModelRequestInput{RequestID: uuid(), ModelID: modelID, Reason: "Native application workload"}
		value := decodeCatalogResponse[service.PersonalModelRequestDetail](t, as(actor, "POST", personal, input, etag), 201)
		return value, input, etag
	}
	assertGrant := func(modelID string, expected bool, source *string) {
		t.Helper()
		var grant entity.UserModelGrant
		err := db.Where("user_id = ? AND model_id = ?", applicant.auth.User.ID, modelID).Take(&grant).Error
		if !expected {
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				t.Fatal("request unexpectedly changed grant", grant, err)
			}
			return
		}
		if err != nil || !reflect.DeepEqual(grant.SourceRequestID, source) {
			t.Fatal("grant lost exact request provenance", grant, err)
		}
	}
	expectStatus(t, send(nil, "", "GET", candidates, nil, ""), 401)
	expectStatus(t, as(writer, "GET", workspacePath, nil, ""), 403)
	expectStatus(t, as(outsider, "GET", memberPath(applicant.auth.User.ID), nil, ""), 403)
	workspace := decodeCatalogResponse[service.MemberModelAccessWorkspace](t, as(reviewer, "GET", workspacePath, nil, ""), 200)
	if workspace.UserID != applicant.auth.User.ID || workspace.ModelCount != 1 || len(workspace.Models) != 1 || workspace.Models[0].ID != modelIDs["a"] || !workspace.CanReviewRequests {
		t.Fatal("review permission did not independently authorize bounded access workspace")
	}
	literal := decodeCatalogResponse[service.ModelAccessCandidatePage](t, as(applicant, "GET", candidates+"?q=%25", nil, ""), 200)
	if len(literal.Items) != 0 {
		t.Fatal("literal candidate search became a wildcard")
	}
	for _, query := range []string{"?unknown=1", "?limit=0", "?limit=51", "?limit=01", "?q=a&q=b", "?cursor=" + strings.ToUpper(modelIDs["b"])} {
		expectStatus(t, as(applicant, "GET", candidates+query, nil, ""), 400)
	}
	alreadyGranted := service.PersonalModelRequestInput{RequestID: uuid(), ModelID: modelIDs["a"], Reason: "Already callable"}
	expectStatus(t, as(applicant, "POST", personal, alreadyGranted, candidate(applicant, modelIDs["a"]).ReviewETag), 409)
	bCandidate := candidate(applicant, modelIDs["b"])
	if bCandidate.PersonalGranted || bCandidate.PendingRequestID != nil || !slices.Equal(bCandidate.Protocols, []string{entity.ProtocolOpenAIChat}) {
		t.Fatal("request directory confused visibility and current Personal grant")
	}
	badInput := service.PersonalModelRequestInput{RequestID: uuid(), ModelID: modelIDs["b"], Reason: "Reviewed intent"}
	expectStatus(t, send(applicant.cookie, "", "POST", personal, badInput, bCandidate.ReviewETag), 403)
	expectStatus(t, as(applicant, "POST", personal, badInput, ""), 400)
	expectStatus(t, as(applicant, "POST", personal, badInput, strings.Repeat("F", 64)), 400)
	for _, reason := range []string{"", " \t ", "control\ncharacter", strings.Repeat("界", 342), strings.Repeat(" ", 1025) + "why"} {
		input := badInput
		input.Reason = reason
		expectStatus(t, as(applicant, "POST", personal, input, bCandidate.ReviewETag), 400)
	}
	for _, malformed := range []string{"not-a-uuid", "90000000-0000-1000-8000-000000000000"} {
		input := badInput
		input.RequestID = malformed
		expectStatus(t, as(applicant, "POST", personal, input, bCandidate.ReviewETag), 400)
	}
	alias := badInput
	alias.ModelID = strings.ToUpper(alias.ModelID)
	expectStatus(t, as(applicant, "POST", personal, alias, bCandidate.ReviewETag), 400)
	expectStatus(t, as(applicant, "POST", personal, map[string]any{"request_id": badInput.RequestID, "model_id": badInput.ModelID, "reason": badInput.Reason, "approved": true}, bCandidate.ReviewETag), 400)
	for _, raw := range []string{
		`{"request_id":"` + badInput.RequestID + `","request_id":"` + badInput.RequestID + `","model_id":"` + badInput.ModelID + `","reason":"why"}`,
		`{"request_id":"` + badInput.RequestID + `","model_id":"` + badInput.ModelID + `","reason":"why","reason":"different"}`,
		"{\"request_id\":\"" + badInput.RequestID + "\",\"model_id\":\"" + badInput.ModelID + "\",\"reason\":\"\xff\"}",
	} {
		expectStatus(t, sendRaw(applicant.cookie, applicant.auth.CSRFToken, "POST", personal, raw, bCandidate.ReviewETag), 400)
	}
	// An audit failure during submission cannot reserve a pending pair or UUID.
	creationRollbackID := uuid()
	creationRollbackETag := candidate(applicant, modelIDs["c"]).ReviewETag
	creationRollbackInput := service.PersonalModelRequestInput{RequestID: creationRollbackID, ModelID: modelIDs["c"], Reason: "Atomic submission"}
	creationCallback := "test_personal_request_creation_audit_failure"
	if err := db.Callback().Create().Before("gorm:create").Register(creationCallback, func(tx *gorm.DB) {
		if tx.Statement.Table == "audit_events" {
			_ = tx.AddError(errors.New("test-only request creation audit failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	creationFailure := as(applicant, "POST", personal, creationRollbackInput, creationRollbackETag)
	if err := db.Callback().Create().Remove(creationCallback); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, creationFailure, 500)
	var rollbackCount int64
	if err := db.Model(&entity.PersonalModelRequest{}).Where("request_id = ?", creationRollbackID).Count(&rollbackCount).Error; err != nil || rollbackCount != 0 {
		t.Fatal("failed submission retained historical intent", err)
	}
	if candidate(applicant, modelIDs["c"]).PendingRequestID != nil {
		t.Fatal("failed submission retained unique pending slot")
	}
	oldKey, err := svc.CreatePersonalKey(ctx, applicant.auth.User.ID, "Preapproval A scope", []string{modelIDs["a"]}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ConfirmKeyDelivery(ctx, applicant.auth.User.ID, oldKey.Record.Key.ID); err != nil {
		t.Fatal(err)
	}
	native := func(bearer, modelName string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"`+modelName+`","messages":[{"role":"user","content":"Request acceptance"}]}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+bearer)
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		return res
	}
	expectStatus(t, native(oldKey.Secret, "personal-request-a"), 200)
	assertNoDispatch := func(res *httptest.ResponseRecorder, before int32) {
		t.Helper()
		if res.Code != 403 && res.Code != 404 {
			t.Fatalf("unauthorized native call status %d: %s", res.Code, res.Body.String())
		}
		if nativeCalls.Load() != before {
			t.Fatal("unauthorized native call reached upstream")
		}
	}
	before := nativeCalls.Load()
	assertNoDispatch(native(oldKey.Secret, "personal-request-b"), before)
	createETag := candidate(applicant, modelIDs["b"]).ReviewETag
	createInput := service.PersonalModelRequestInput{RequestID: uuid(), ModelID: modelIDs["b"], Reason: "Native application workload"}
	var createWG sync.WaitGroup
	creationOutcomes := make(chan *httptest.ResponseRecorder, 2)
	for range 2 {
		createWG.Go(func() { creationOutcomes <- as(applicant, "POST", personal, createInput, createETag) })
	}
	createWG.Wait()
	close(creationOutcomes)
	creationStatuses := map[int]int{}
	var pending service.PersonalModelRequestDetail
	for response := range creationOutcomes {
		if response.Code != 201 && response.Code != 200 {
			t.Fatalf("unexpected concurrent creation %d: %s", response.Code, response.Body.String())
		}
		creationStatuses[response.Code]++
		value := decodeCatalogResponse[service.PersonalModelRequestDetail](t, response, response.Code)
		if pending.ID != "" && pending.ID != value.ID {
			t.Fatal("concurrent exact creation duplicated historical intent")
		}
		pending = value
	}
	if creationStatuses[201] != 1 || creationStatuses[200] != 1 {
		t.Fatal("exact creation retry did not report one new request", creationStatuses)
	}

	if pending.Status != entity.PersonalModelRequestPending || pending.Decision != nil || !slices.Equal(pending.AllowedActions, []string{"withdraw"}) || pending.CurrentGranted || pending.RuntimeApplied {
		t.Fatal("submission claimed grant, decision or runtime application")
	}
	assertGrant(modelIDs["b"], false, nil)
	replayed := decodeCatalogResponse[service.PersonalModelRequestDetail](t, as(applicant, "POST", personal, createInput, createETag), 200)
	if replayed.ID != pending.ID {
		t.Fatal("exact creation retry duplicated request")
	}
	changed := createInput
	changed.Reason = "Changed original intent"
	expectStatus(t, as(applicant, "POST", personal, changed, createETag), 409)
	expectStatus(t, as(applicant, "POST", personal, createInput, strings.Repeat("b", 64)), 409)
	expectStatus(t, as(outsider, "POST", personal, createInput, createETag), 409)
	changed = createInput
	changed.RequestID = uuid()
	expectStatus(t, as(applicant, "POST", personal, changed, candidate(applicant, modelIDs["b"]).ReviewETag), 409)
	if candidate(applicant, modelIDs["b"]).PendingRequestID == nil {
		t.Fatal("pending slot was not visible")
	}
	expectStatus(t, as(outsider, "GET", personal+"/"+pending.ID, nil, ""), 404)
	for _, path := range []string{personal + "/" + strings.ToUpper(pending.ID), memberPath(strings.ToUpper(applicant.auth.User.ID)) + "/" + pending.ID, memberPath(applicant.auth.User.ID) + "/" + strings.ToUpper(pending.ID)} {
		expectStatus(t, as(reviewer, "GET", path, nil, ""), 400)
	}
	review := detail(reviewer, applicant.auth.User.ID, pending.ID, true)
	if review.ReviewETag == pending.ReviewETag || !slices.Equal(review.AllowedActions, []string{"approve", "reject"}) {
		t.Fatal("review ETag/actions did not bind reviewer identity")
	}
	decision := service.PersonalModelDecisionInput{DecisionID: uuid(), Action: "approve", Reason: "Reviewed Personal access"}
	decisionPath := memberPath(applicant.auth.User.ID) + "/" + pending.ID + "/decision"
	for _, raw := range []string{
		`{"decision_id":"` + decision.DecisionID + `","action":"approve","action":"reject"}`,
		`{"decision_id":"` + decision.DecisionID + `","action":"approve","reason":null}`,
		`{"decision_id":"` + decision.DecisionID + `","action":"approve","model_id":"` + modelIDs["c"] + `"}`,
	} {
		expectStatus(t, sendRaw(reviewer.cookie, reviewer.auth.CSRFToken, "POST", decisionPath, raw, review.ReviewETag), 400)
	}
	expectStatus(t, as(writer, "POST", decisionPath, decision, review.ReviewETag), 403)
	expectStatus(t, as(reviewer, "POST", decisionPath, decision, pending.ReviewETag), 409)
	if _, err := svc.SetMemberRoles(ctx, admin.User.ID, applicant.auth.User.ID, []string{reviewRole.Role.ID}); err != nil {
		t.Fatal(err)
	}
	selfReview := detail(applicant, applicant.auth.User.ID, pending.ID, true)
	if len(selfReview.AllowedActions) != 0 {
		t.Fatal("applicant gained self-review controls")
	}
	expectStatus(t, as(applicant, "POST", decisionPath, decision, selfReview.ReviewETag), 403)
	if _, err := svc.SetMemberRoles(ctx, admin.User.ID, applicant.auth.User.ID, nil); err != nil {
		t.Fatal(err)
	}
	for _, reason := range []string{"", "bad\nreason", strings.Repeat("x", 1025)} {
		invalid := decision
		invalid.Action, invalid.Reason = "reject", reason
		expectStatus(t, as(reviewer, "POST", decisionPath, invalid, review.ReviewETag), 400)
	}
	// The grant, terminal receipt and pending-slot deletion must roll back together.
	callback := "test_personal_request_audit_failure"
	if err := db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table == "audit_events" {
			_ = tx.AddError(errors.New("test-only Personal request audit failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	failed := as(reviewer, "POST", decisionPath, decision, review.ReviewETag)
	if err := db.Callback().Create().Remove(callback); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, failed, 500)
	assertGrant(modelIDs["b"], false, nil)
	afterRollback := detail(reviewer, applicant.auth.User.ID, pending.ID, true)
	if afterRollback.Status != entity.PersonalModelRequestPending || afterRollback.Decision != nil || afterRollback.ReviewETag != review.ReviewETag || candidate(applicant, modelIDs["b"]).PendingRequestID == nil {
		t.Fatal("audit failure persisted a partial terminal request")
	}
	approved := decodeCatalogResponse[service.PersonalModelDecisionRecord](t, as(reviewer, "POST", decisionPath, decision, review.ReviewETag), 200)
	if !approved.Committed || approved.SavedRequest.Status != entity.PersonalModelRequestApproved || approved.SavedRequest.Decision == nil || approved.SavedRequest.Decision.DecisionID != decision.DecisionID || !approved.CurrentGranted || !approved.RuntimeApplied || approved.ApplicationStatus != "applied" {
		t.Fatalf("approval lost durable receipt or current publication: %+v", approved)
	}
	assertGrant(modelIDs["b"], true, &pending.ID)
	assertGrant(modelIDs["a"], true, nil)
	approvalRetry := decodeCatalogResponse[service.PersonalModelDecisionRecord](t, as(reviewer, "POST", decisionPath, decision, review.ReviewETag), 200)
	if !reflect.DeepEqual(approved.SavedRequest, approvalRetry.SavedRequest) {
		t.Fatal("exact decision retry rewrote historical receipt")
	}
	changedDecision := decision
	changedDecision.Reason = "Changed historical intent"
	expectStatus(t, as(reviewer, "POST", decisionPath, changedDecision, review.ReviewETag), 409)
	expectStatus(t, as(reviewer, "POST", decisionPath, decision, strings.Repeat("c", 64)), 409)
	before = nativeCalls.Load()
	assertNoDispatch(native(oldKey.Secret, "personal-request-b"), before)
	newKey, err := svc.CreatePersonalKey(ctx, applicant.auth.User.ID, "Approved B scope", []string{modelIDs["b"]}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ConfirmKeyDelivery(ctx, applicant.auth.User.ID, newKey.Record.Key.ID); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, native(newKey.Secret, "personal-request-b"), 200)
	var ceiling []string
	if err := db.Model(&entity.APIKeyModel{}).Where("key_id = ?", oldKey.Record.Key.ID).Pluck("model_id", &ceiling).Error; err != nil || !slices.Equal(ceiling, []string{modelIDs["a"]}) {
		t.Fatal("approval expanded immutable existing Key ceiling", ceiling, err)
	}
	if _, err := svc.SetModelGrants(ctx, admin.User.ID, modelIDs["b"], nil); err != nil {
		t.Fatal(err)
	}
	before = nativeCalls.Load()
	assertNoDispatch(native(newKey.Secret, "personal-request-b"), before)
	superseded := decodeCatalogResponse[service.PersonalModelDecisionRecord](t, as(reviewer, "POST", decisionPath, decision, review.ReviewETag), 200)
	if !superseded.Committed || superseded.CurrentGranted || superseded.RuntimeApplied || superseded.ApplicationStatus != "superseded" || !reflect.DeepEqual(approved.SavedRequest, superseded.SavedRequest) {
		t.Fatal("known approval retry restored revoked grant or erased historical commit")
	}
	assertGrant(modelIDs["b"], false, nil)
	terminalCreate := decodeCatalogResponse[service.PersonalModelRequestDetail](t, as(applicant, "POST", personal, createInput, createETag), 200)
	if terminalCreate.ID != pending.ID || terminalCreate.Status != entity.PersonalModelRequestApproved {
		t.Fatal("creation receipt retry lost terminal history")
	}
	// A later direct grant remains distinct from the historical approved request.
	if _, err := svc.SetModelGrants(ctx, admin.User.ID, modelIDs["b"], []string{applicant.auth.User.ID}); err != nil {
		t.Fatal(err)
	}
	superseded = decodeCatalogResponse[service.PersonalModelDecisionRecord](t, as(reviewer, "POST", decisionPath, decision, review.ReviewETag), 200)
	if !superseded.CurrentGranted || superseded.RuntimeApplied || superseded.ApplicationStatus != "superseded" {
		t.Fatal("direct grant was misattributed to old approval")
	}
	assertGrant(modelIDs["b"], true, nil)
	if _, err := svc.SetModelGrants(ctx, admin.User.ID, modelIDs["b"], nil); err != nil {
		t.Fatal(err)
	}
	// A reviewed target change invalidates the old validator. A fresh approval
	// retains an independently added grant and never stamps it as request-owned.
	independentlyGranted, _, _ := create(applicant, modelIDs["i"])
	oldIndependentReview := detail(reviewer, applicant.auth.User.ID, independentlyGranted.ID, true)
	if _, err := svc.SetModelGrants(ctx, admin.User.ID, modelIDs["i"], []string{applicant.auth.User.ID}); err != nil {
		t.Fatal(err)
	}
	independentInput := service.PersonalModelDecisionInput{DecisionID: uuid(), Action: "approve"}
	independentPath := memberPath(applicant.auth.User.ID) + "/" + independentlyGranted.ID + "/decision"
	expectStatus(t, as(reviewer, "POST", independentPath, independentInput, oldIndependentReview.ReviewETag), 409)
	newIndependentReview := detail(reviewer, applicant.auth.User.ID, independentlyGranted.ID, true)
	if newIndependentReview.ReviewETag == oldIndependentReview.ReviewETag {
		t.Fatal("current target grant did not invalidate reviewed ETag")
	}
	independentReceipt := decodeCatalogResponse[service.PersonalModelDecisionRecord](t, as(reviewer, "POST", independentPath, independentInput, newIndependentReview.ReviewETag), 200)
	if !independentReceipt.Committed || !independentReceipt.CurrentGranted || independentReceipt.RuntimeApplied || independentReceipt.ApplicationStatus != "superseded" {
		t.Fatal("approval replaced or claimed an independent grant")
	}
	assertGrant(modelIDs["i"], true, nil)
	// Current inactive Models remain rejectable/withdrawable without admitting an
	// approval. Direct status writes here create fixture states, not a product API.
	for _, testCase := range []struct{ letter, action string }{{"g", "reject"}, {"h", "withdraw"}} {
		inactive, _, _ := create(applicant, modelIDs[testCase.letter])
		if err := db.Model(&entity.Model{}).Where("id = ?", inactive.ModelID).Update("status", entity.ResourceDisabled).Error; err != nil {
			t.Fatal(err)
		}
		inactiveReview := detail(reviewer, applicant.auth.User.ID, inactive.ID, true)
		inactiveOwn := detail(applicant, applicant.auth.User.ID, inactive.ID, false)
		if !slices.Equal(inactiveReview.AllowedActions, []string{"reject"}) || !slices.Equal(inactiveOwn.AllowedActions, []string{"withdraw"}) {
			t.Fatal("inactive Model trapped a pending request or offered approval")
		}
		inactiveDecision := service.PersonalModelDecisionInput{DecisionID: uuid(), Action: "approve"}
		inactiveReviewPath := memberPath(applicant.auth.User.ID) + "/" + inactive.ID + "/decision"
		expectStatus(t, as(reviewer, "POST", inactiveReviewPath, inactiveDecision, inactiveReview.ReviewETag), 409)
		inactiveDecision.Action = testCase.action
		actor, terminalPath, terminalETag := applicant, personal+"/"+inactive.ID+"/decision", inactiveOwn.ReviewETag
		if testCase.action == "reject" {
			inactiveDecision.Reason = "Model is unavailable"
			actor, terminalPath, terminalETag = reviewer, inactiveReviewPath, inactiveReview.ReviewETag
		}
		receipt := decodeCatalogResponse[service.PersonalModelDecisionRecord](t, as(actor, "POST", terminalPath, inactiveDecision, terminalETag), 200)
		retry := decodeCatalogResponse[service.PersonalModelDecisionRecord](t, as(actor, "POST", terminalPath, inactiveDecision, terminalETag), 200)
		if !receipt.Committed || receipt.SavedRequest.Status == entity.PersonalModelRequestPending || !reflect.DeepEqual(receipt.SavedRequest, retry.SavedRequest) {
			t.Fatal("inactive Model terminal action lost immutable receipt")
		}
		assertGrant(inactive.ModelID, false, nil)
	}
	// Two incompatible terminal decisions serialize to exactly one receipt.
	racing, _, _ := create(applicant, modelIDs["c"])
	racingReview := detail(reviewer, applicant.auth.User.ID, racing.ID, true)
	reject := service.PersonalModelDecisionInput{DecisionID: uuid(), Action: "reject", Reason: "Not required for current workload"}
	withdraw := service.PersonalModelDecisionInput{DecisionID: uuid(), Action: "withdraw"}
	var wg sync.WaitGroup
	outcomes := make(chan *httptest.ResponseRecorder, 2)
	wg.Go(func() {
		outcomes <- as(reviewer, "POST", memberPath(applicant.auth.User.ID)+"/"+racing.ID+"/decision", reject, racingReview.ReviewETag)
	})
	wg.Go(func() {
		outcomes <- as(applicant, "POST", personal+"/"+racing.ID+"/decision", withdraw, racing.ReviewETag)
	})
	wg.Wait()
	close(outcomes)
	wins, losses := 0, 0
	for response := range outcomes {
		switch response.Code {
		case 200:
			wins++
		case 409:
			losses++
		default:
			t.Fatalf("unexpected concurrent terminal response %d: %s", response.Code, response.Body.String())
		}
	}
	if wins != 1 || losses != 1 {
		t.Fatal("concurrent terminal decisions did not have one winner", wins, losses)
	}
	terminal := detail(applicant, applicant.auth.User.ID, racing.ID, false)
	if terminal.Decision == nil || (terminal.Status != entity.PersonalModelRequestRejected && terminal.Status != entity.PersonalModelRequestWithdrawn) || candidate(applicant, modelIDs["c"]).PendingRequestID != nil {
		t.Fatal("terminal race retained pending slot or lost receipt")
	}
	var winningRetry *httptest.ResponseRecorder
	if terminal.Status == entity.PersonalModelRequestRejected {
		winningRetry = as(reviewer, "POST", memberPath(applicant.auth.User.ID)+"/"+racing.ID+"/decision", reject, racingReview.ReviewETag)
	} else {
		winningRetry = as(applicant, "POST", personal+"/"+racing.ID+"/decision", withdraw, racing.ReviewETag)
	}
	winning := decodeCatalogResponse[service.PersonalModelDecisionRecord](t, winningRetry, 200)
	if winning.DecisionID != terminal.Decision.DecisionID || !winning.Committed {
		t.Fatal("winning terminal retry did not reconcile exact receipt")
	}
	assertGrant(modelIDs["c"], false, nil)
	next, _, _ := create(applicant, modelIDs["c"])
	nextReview := detail(reviewer, applicant.auth.User.ID, next.ID, true)
	collision := reject
	collision.DecisionID = terminal.Decision.DecisionID
	expectStatus(t, as(reviewer, "POST", memberPath(applicant.auth.User.ID)+"/"+next.ID+"/decision", collision, nextReview.ReviewETag), 409)
	if detail(applicant, applicant.auth.User.ID, next.ID, false).Status != entity.PersonalModelRequestPending {
		t.Fatal("decision UUID collision changed another request")
	}
	// Explicit publication failure acknowledges commit independently of application.
	publication, _, _ := create(applicant, modelIDs["d"])
	publicationReview := detail(reviewer, applicant.auth.User.ID, publication.ID, true)
	publicationInput := service.PersonalModelDecisionInput{DecisionID: uuid(), Action: "approve"}
	var outage atomic.Bool
	publicationCallback := "test_personal_request_publication_failure"
	if err := db.Callback().Query().Before("gorm:query").Register(publicationCallback, func(tx *gorm.DB) {
		if outage.Load() && tx.Statement.Table == "api_keys" {
			_ = tx.AddError(errors.New("test-only authorization publication outage"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Create().After("gorm:create").Register(publicationCallback, func(tx *gorm.DB) {
		if tx.Statement.Table == "audit_events" && tx.Error == nil {
			outage.Store(true)
		}
	}); err != nil {
		t.Fatal(err)
	}
	publicationPath := memberPath(applicant.auth.User.ID) + "/" + publication.ID + "/decision"
	unapplied := as(reviewer, "POST", publicationPath, publicationInput, publicationReview.ReviewETag)
	outage.Store(false)
	if err := db.Callback().Query().Remove(publicationCallback); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Create().Remove(publicationCallback); err != nil {
		t.Fatal(err)
	}
	publicationReceipt := decodeCatalogResponse[service.PersonalModelDecisionRecord](t, unapplied, 200)
	if !publicationReceipt.Committed || !publicationReceipt.CurrentGranted || publicationReceipt.RuntimeApplied || publicationReceipt.ApplicationStatus != "pending" {
		t.Fatal("publication outage confused durable grant with current runtime")
	}
	published := decodeCatalogResponse[service.PersonalModelDecisionRecord](t, as(reviewer, "POST", publicationPath, publicationInput, publicationReview.ReviewETag), 200)
	if !published.RuntimeApplied || published.ApplicationStatus != "applied" || !reflect.DeepEqual(published.SavedRequest, publicationReceipt.SavedRequest) {
		t.Fatal("publication retry rewrote durable receipt or failed to publish exact provenance")
	}
	// Histories and cursors are actor-, target- and filter-scoped.
	page := decodeCatalogResponse[service.PersonalModelRequestPage](t, as(applicant, "GET", personal+"?limit=1", nil, ""), 200)
	if len(page.Items) != 1 || page.Total < 4 || page.NextCursor == nil {
		t.Fatal("request history lacks bounded cursor pagination")
	}
	cursor := url.QueryEscape(*page.NextCursor)
	nextPage := decodeCatalogResponse[service.PersonalModelRequestPage](t, as(applicant, "GET", personal+"?limit=1&cursor="+cursor, nil, ""), 200)
	if len(nextPage.Items) != 1 || nextPage.Items[0].ID == page.Items[0].ID {
		t.Fatal("cursor repeated first history item")
	}
	expectStatus(t, as(outsider, "GET", personal+"?cursor="+cursor, nil, ""), 400)
	expectStatus(t, as(reviewer, "GET", memberPath(applicant.auth.User.ID)+"?cursor="+cursor, nil, ""), 400)
	expectStatus(t, as(applicant, "GET", personal+"?status=pending&cursor="+cursor, nil, ""), 400)
	for _, query := range []string{"?limit=0", "?limit=51", "?status=completed", "?user_id=" + outsider.auth.User.ID} {
		expectStatus(t, as(applicant, "GET", personal+query, nil, ""), 400)
	}
	var auditCount int64
	if err := db.Model(&entity.AuditEvent{}).Where("resource_id = ? AND action = ?", pending.ID, "personal.model_request.approve").Count(&auditCount).Error; err != nil || auditCount != 1 {
		t.Fatal("approval retries duplicated audit", auditCount, err)
	}
	if err := db.Model(&entity.AuditEvent{}).Where("resource_id = ? AND action IN ?", racing.ID, []string{"personal.model_request.reject", "personal.model_request.withdraw"}).Count(&auditCount).Error; err != nil || auditCount != 1 {
		t.Fatal("terminal race duplicated audit", auditCount, err)
	}
	// Applicant lifecycle cancellation shares the transaction with identity changes.
	if err := db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table == "audit_events" {
			_ = tx.AddError(errors.New("test-only lifecycle audit failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	disabled := true
	_, disableErr := svc.UpdateMember(ctx, admin.User.ID, applicant.auth.User.ID, &disabled, nil)
	if err := db.Callback().Create().Remove(callback); err != nil {
		t.Fatal(err)
	}
	if disableErr == nil {
		t.Fatal("member disablement committed without cancellation audit")
	}
	var applicantRow entity.User
	if err := db.Take(&applicantRow, "id = ?", applicant.auth.User.ID).Error; err != nil || applicantRow.Disabled || detail(applicant, applicant.auth.User.ID, next.ID, false).Status != entity.PersonalModelRequestPending {
		t.Fatal("audit rollback partially disabled applicant or cancelled request", err)
	}
	if _, err := svc.UpdateMember(ctx, admin.User.ID, applicant.auth.User.ID, &disabled, nil); err != nil {
		t.Fatal(err)
	}
	cancelled := detail(reviewer, applicant.auth.User.ID, next.ID, true)
	if cancelled.Status != entity.PersonalModelRequestCancelled || cancelled.CancelledReason == nil || *cancelled.CancelledReason != "applicant_unavailable" || cancelled.Decision != nil || cancelled.ResolvedAt == nil || len(cancelled.AllowedActions) != 0 {
		t.Fatal("disablement did not retain immutable cancellation evidence")
	}
	expectStatus(t, as(applicant, "GET", personal, nil, ""), 401)
	aliasedCreation, aliasedCreated, aliasErr := svc.CreatePersonalModelRequest(ctx, strings.ToUpper(applicant.auth.User.ID), createETag, createInput)
	var aliasDenial *apperrors.Error
	if aliasedCreation != nil || aliasedCreated || !errors.As(aliasErr, &aliasDenial) || aliasDenial.Code != http.StatusUnauthorized {
		t.Fatal("actor alias was not denied before returning a creation receipt", aliasErr)
	}
	aliasedDecision, aliasErr := svc.DecidePersonalModelRequest(ctx, strings.ToUpper(reviewer.auth.User.ID), applicant.auth.User.ID, pending.ID, review.ReviewETag, decision, true)
	aliasDenial = nil
	if aliasedDecision != nil || !errors.As(aliasErr, &aliasDenial) || aliasDenial.Code != http.StatusUnauthorized {
		t.Fatal("reviewer alias was not denied before returning a decision receipt", aliasErr)
	}
	if _, err := svc.SetMemberRoles(ctx, admin.User.ID, reviewer.auth.User.ID, nil); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, as(reviewer, "POST", decisionPath, decision, review.ReviewETag), 403)
	// Emergency offboarding also cancels its current pending slot atomically.
	departing, _, _ := create(outsider, modelIDs["e"])
	if _, err := svc.EmergencyOffboarding(ctx, admin.User.ID, outsider.auth.User.ID, service.OffboardingEmergencyInput{RequestID: "personal-request-offboard", CurrentPassword: "personal-request-password", Reason: "Departure acceptance"}); err != nil {
		t.Fatal(err)
	}
	departed := detail(requestActor{admin, adminCookie}, outsider.auth.User.ID, departing.ID, true)
	if departed.Status != entity.PersonalModelRequestCancelled || departed.Decision != nil || departed.ResolvedAt == nil || departed.CancelledReason == nil {
		t.Fatal("offboarding failed to cancel pending Personal request")
	}
	// Frozen receipts survive unavailable current catalogue records. The fixture
	// invokes the lifecycle hook directly; it does not introduce a Model status API.
	modelPending, _, _ := create(writer, modelIDs["f"])
	if err := db.Transaction(func(tx *gorm.DB) error {
		return service.CancelPersonalModelRequestsForModel(tx, admin.User.ID, modelIDs["f"], "model_unavailable")
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Where("model_id = ?", modelIDs["f"]).Delete(&entity.ModelName{}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Delete(&entity.Model{}, "id = ?", modelIDs["f"]).Error; err != nil {
		t.Fatal(err)
	}
	missingModel := detail(requestActor{admin, adminCookie}, writer.auth.User.ID, modelPending.ID, true)
	if missingModel.CurrentModel != nil || missingModel.ModelName != "personal-request-f" || missingModel.Status != entity.PersonalModelRequestCancelled {
		t.Fatal("historical cancellation depended on live model identity")
	}
}
