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
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/secretstore"
)

func testTeamModelRequestsLifecycle(t *testing.T, db *gorm.DB) {
	ctx := context.Background()
	var auditFailure, publicationFailure atomic.Bool
	const callback = "test_team_model_requests_failures"
	if err := db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
		if auditFailure.Load() && tx.Statement.Table == "audit_events" {
			_ = tx.AddError(errors.New("controlled Team request audit failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Query().Before("gorm:query").Register(callback, func(tx *gorm.DB) {
		if publicationFailure.Load() && tx.Statement.Table == "api_keys" {
			_ = tx.AddError(errors.New("controlled Team request publication failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	var nativeCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer team-request-test-secret" || r.Header.Get("Cookie") != "" || r.Header.Get("X-CSRF-Token") != "" {
			t.Error("native Team request forwarded incorrect credentials")
			w.WriteHeader(401)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if r.Method == "GET" && r.URL.Path == "/v1/models" {
			_, _ = io.WriteString(w, `{"data":[{"id":"team-request-native-a"},{"id":"team-request-native-b"}]}`)
			return
		}
		if r.Method != "POST" || r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		var payload struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil || (payload.Model != "team-request-native-a" && payload.Model != "team-request-native-b") {
			t.Error("incorrect native Team route", err)
			w.WriteHeader(400)
			return
		}
		nativeCalls.Add(1)
		_, _ = io.WriteString(w, `{"object":"chat.completion","model":"`+payload.Model+`","choices":[{"index":0,"message":{"role":"assistant","content":"Shared Team access"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0}}}`)
	}))
	defer upstream.Close()
	store, err := secretstore.New([]byte(strings.Repeat("t", 32)))
	if err != nil {
		t.Fatal(err)
	}
	svc, err := service.New(ctx, db, service.WithCredentialStorage(store), service.WithUpstreamPolicy(true), service.WithRuntimeRefreshInterval(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		auditFailure.Store(false)
		publicationFailure.Store(false)
		svc.StopRuntime()
		_ = svc.StopCallRecorder()
		_ = db.Callback().Create().Remove(callback)
		_ = db.Callback().Query().Remove(callback)
	}()
	router := fox.New()
	New(svc).RegisterRoutes(router)
	setup := identityRequest(router, "POST", "/api/v1/setup", `{"email":"team-model-admin@example.invalid","password":"team-model-admin-password","name":"Team request administrator"}`, nil, "")
	expectStatus(t, setup, 201)
	admin, adminCookie := readIdentity(t, setup)
	type actor struct {
		userID string
		cookie *http.Cookie
		csrf   string
	}
	administrator := actor{admin.User.ID, adminCookie, admin.CSRFToken}
	newActor := func(name string, permissions []string) actor {
		t.Helper()
		member, cookie, csrf := createSystemStatusMember(t, svc, router, admin.User.ID, "team-model-"+name, permissions)
		return actor{member.User.ID, cookie, csrf}
	}
	owner, applicant, reviewer, member := newActor("owner", nil), newActor("applicant", nil), newActor("reviewer", nil), newActor("member", nil)
	outsider, writer, platform := newActor("outsider", nil), newActor("writer", []string{"teams.write"}), newActor("platform", []string{"teams.models.write"})
	var adminRow entity.User
	if err := db.Take(&adminRow, "id = ?", admin.User.ID).Error; err != nil {
		t.Fatal(err)
	}
	legacyUser := entity.User{ID: "usr_team_model_legacy", Name: "Legacy Team applicant", Email: "team-model-legacy@example.invalid", PasswordHash: adminRow.PasswordHash, Role: entity.RoleMember}
	if err := db.Create(&legacyUser).Error; err != nil {
		t.Fatal(err)
	}
	legacySession, legacyCookie := readIdentity(t, identityRequest(router, "POST", "/api/v1/auth/login", `{"email":"team-model-legacy@example.invalid","password":"team-model-admin-password"}`, nil, ""))
	legacy := actor{legacyUser.ID, legacyCookie, legacySession.CSRFToken}
	sendRaw := func(who actor, method, path, body, etag string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, "http://routex.test"+path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if who.cookie != nil {
			req.AddCookie(who.cookie)
		}
		if who.csrf != "" {
			req.Header.Set("X-CSRF-Token", who.csrf)
		}
		if etag != "" {
			req.Header.Set("If-Match", `"`+etag+`"`)
		}
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		return res
	}
	send := func(who actor, method, path string, body any, etag string) *httptest.ResponseRecorder {
		t.Helper()
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		return sendRaw(who, method, path, string(encoded), etag)
	}
	const teamID = "tem_team_model_request"
	const ownPath = "/api/v1/team-model-requests"
	const pickerPath = "/api/v1/team-model-request-teams"
	reviewPath := "/api/v1/teams/" + teamID + "/model-requests"
	candidatePath := "/api/v1/teams/" + teamID + "/model-request-candidates"
	workspacePath := "/api/v1/teams/" + teamID + "/model-request-workspace"
	if err := db.Create(&entity.Team{ID: teamID, Name: "Shared request Team", Status: entity.ResourceActive}).Error; err != nil {
		t.Fatal(err)
	}
	members := []service.TeamMemberInput{}
	for _, who := range []actor{owner, applicant, reviewer, member, legacy} {
		role := entity.TeamMember
		if who.userID == owner.userID {
			role = entity.TeamOwner
		}
		members = append(members, service.TeamMemberInput{UserID: who.userID, Role: role, Status: entity.ResourceActive})
	}
	if _, err := svc.SetTeamMembers(ctx, admin.User.ID, teamID, members); err != nil {
		t.Fatal(err)
	}
	other, err := svc.CreateResource(ctx, admin.User.ID, service.TeamResource, "Other request Team", "No scope borrowing", []string{owner.userID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetTeamMembers(ctx, admin.User.ID, other.ID, []service.TeamMemberInput{{UserID: owner.userID, Role: entity.TeamOwner, Status: entity.ResourceActive}, {UserID: reviewer.userID, Role: entity.TeamMember, Status: entity.ResourceActive}}); err != nil {
		t.Fatal(err)
	}
	provider, err := svc.CreateProvider(ctx, admin.User.ID, "Team request provider", service.CreateConnectionInput{Name: "Native Chat", BaseURL: upstream.URL + "/v1", Protocol: entity.ProtocolOpenAIChat, CredentialName: "Controlled Team credential", Secret: "team-request-test-secret"})
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
	models := map[string]string{}
	for _, letter := range []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l", "m"} {
		modelID := "mdl_team_model_" + letter
		models[letter] = modelID
		for _, row := range []any{&entity.Model{ID: modelID, Status: entity.ResourceActive}, &entity.ModelName{Name: "team-request-" + letter, ModelID: modelID, CurrentModelID: &modelID}} {
			if err := db.Create(row).Error; err != nil {
				t.Fatal(err)
			}
		}
		if letter == "a" || letter == "b" {
			var pm entity.ProviderModel
			if err := db.Where("connection_id = ? AND upstream_name = ?", provider.Connections[0].Connection.ID, "team-request-native-"+letter).Take(&pm).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Create(&entity.ModelProviderBinding{ID: "mpb_team_model_" + letter, ModelID: modelID, ProviderModelID: pm.ID, Weight: 100}).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := svc.SetResourceModels(ctx, admin.User.ID, service.TeamResource, teamID, []string{models["a"]}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetModelGrants(ctx, admin.User.ID, models["a"], []string{applicant.userID}); err != nil {
		t.Fatal(err)
	}
	if err := svc.StartRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	if err := svc.StartCallRecorder(ctx, filepath.Join(t.TempDir(), "team-model-requests.db")); err != nil {
		t.Fatal(err)
	}
	personalKey, err := svc.CreatePersonalKey(ctx, applicant.userID, "Personal A ceiling", []string{models["a"]}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ConfirmKeyDelivery(ctx, applicant.userID, personalKey.Record.Key.ID); err != nil {
		t.Fatal(err)
	}
	native := func(who actor, name string) *httptest.ResponseRecorder {
		t.Helper()
		return send(who, "POST", "/api/v1/teams/"+teamID+"/chat/completions", map[string]any{"model": name, "messages": []map[string]string{{"role": "user", "content": "Shared acceptance"}}}, "")
	}
	// An existing Session and Personal Key cannot invoke the pending addition.
	expectStatus(t, native(applicant, "team-request-a"), 200)
	beforeApproval := nativeCalls.Load()
	expectStatus(t, native(applicant, "team-request-b"), 404)
	if nativeCalls.Load() != beforeApproval {
		t.Fatal("ungranted Team Model dispatched before approval")
	}
	var sequence int
	uuid := func() string { sequence++; return fmt.Sprintf("44000000-0000-4000-8000-%012d", sequence) }
	candidate := func(who actor, modelID string) service.TeamModelRequestCandidate {
		t.Helper()
		res := send(who, "GET", candidatePath+"/"+modelID, nil, "")
		value := decodeCatalogResponse[service.TeamModelRequestCandidate](t, res, 200)
		if res.Header().Get("ETag") != `"`+value.ReviewETag+`"` || value.TeamID != teamID {
			t.Fatal("candidate validator lost exact Team context")
		}
		return value
	}
	create := func(who actor, modelID string) (service.TeamModelRequestDetail, service.TeamModelRequestInput, string) {
		t.Helper()
		etag := candidate(who, modelID).ReviewETag
		input := service.TeamModelRequestInput{RequestID: uuid(), TeamID: teamID, ModelID: modelID, Reason: "Shared Team workload"}
		return decodeCatalogResponse[service.TeamModelRequestDetail](t, send(who, "POST", ownPath, input, etag), 201), input, etag
	}
	detail := func(who actor, requestID string, review bool) service.TeamModelRequestDetail {
		t.Helper()
		path := ownPath
		if review {
			path = reviewPath
		}
		return decodeCatalogResponse[service.TeamModelRequestDetail](t, send(who, "GET", path+"/"+requestID, nil, ""), 200)
	}
	decide := func(who actor, row service.TeamModelRequestDetail, input service.TeamModelDecisionInput, review bool) *httptest.ResponseRecorder {
		t.Helper()
		path := ownPath
		if review {
			path = reviewPath
		}
		return send(who, "POST", path+"/"+row.ID+"/decision", input, row.ReviewETag)
	}
	assertBool := func(value *bool, expected bool, label string) {
		t.Helper()
		if value == nil || *value != expected {
			t.Fatal("missing or incorrect authoritative fact", label, value)
		}
	}
	assertGrant := func(modelID string, expected bool, source *string) {
		t.Helper()
		var grant entity.TeamModelGrant
		err := db.Where("team_id = ? AND model_id = ?", teamID, modelID).Take(&grant).Error
		if !expected {
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				t.Fatal("request unexpectedly granted shared Model", grant, err)
			}
			return
		}
		if err != nil || !reflect.DeepEqual(grant.SourceRequestID, source) {
			t.Fatal("shared grant lost exact provenance", grant, err)
		}
	}
	expectStatus(t, send(actor{}, "GET", pickerPath, nil, ""), 401)
	picker := decodeCatalogResponse[service.TeamModelRequestTeamPage](t, send(applicant, "GET", pickerPath, nil, ""), 200)
	if len(picker.Items) != 1 || picker.Items[0].ID != teamID || picker.Items[0].MembershipID == "" {
		t.Fatal("Team picker fetched a global directory or invented membership")
	}
	if list := decodeCatalogResponse[service.TeamModelRequestTeamPage](t, send(platform, "GET", pickerPath, nil, ""), 200); len(list.Items) != 0 {
		t.Fatal("direct reviewer received implicit applicant membership")
	}
	expectStatus(t, send(outsider, "GET", candidatePath, nil, ""), 404)
	expectStatus(t, send(owner, "GET", workspacePath, nil, ""), 403)
	expectStatus(t, send(writer, "GET", workspacePath, nil, ""), 403)
	role, err := svc.SaveRole(ctx, admin.User.ID, "", "Scoped Model approver", []string{"teams.models.write"})
	if err != nil {
		t.Fatal(err)
	}
	roles, err := svc.GetTeamRoles(ctx, admin.User.ID, teamID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetTeamRoles(ctx, admin.User.ID, teamID, roles.ETag, service.TeamRoleInput{RoleIDs: []string{role.Role.ID}, Reason: "Explicit scoped review"}); err != nil {
		t.Fatal(err)
	}
	workspace := decodeCatalogResponse[service.TeamModelRequestWorkspace](t, send(reviewer, "GET", workspacePath, nil, ""), 200)
	if workspace.ModelCount != 1 || len(workspace.Models) != 1 || workspace.Models[0].ID != models["a"] {
		t.Fatal("explicit scoped review lost current Team grants")
	}
	expectStatus(t, send(reviewer, "GET", "/api/v1/teams/"+other.ID+"/model-request-workspace", nil, ""), 403)
	for _, query := range []string{"?limit=0", "?limit=51", "?limit=01", "?unknown=1", "?q=x&q=y"} {
		expectStatus(t, send(applicant, "GET", candidatePath+query, nil, ""), 400)
	}
	if list := decodeCatalogResponse[service.TeamModelRequestCandidatePage](t, send(applicant, "GET", candidatePath+"?q=%25", nil, ""), 200); len(list.Items) != 0 {
		t.Fatal("candidate search treated literal percent as wildcard")
	}
	bCandidate := candidate(applicant, models["b"])
	input := service.TeamModelRequestInput{RequestID: uuid(), TeamID: teamID, ModelID: models["b"], Reason: "Reviewed shared intent"}
	noCSRF := applicant
	noCSRF.csrf = ""
	expectStatus(t, send(noCSRF, "POST", ownPath, input, bCandidate.ReviewETag), 403)
	expectStatus(t, send(applicant, "POST", ownPath, input, ""), 400)
	for _, reason := range []string{"", "bad\nreason", strings.Repeat("界", 342), strings.Repeat(" ", 1025) + "why"} {
		bad := input
		bad.Reason = reason
		expectStatus(t, send(applicant, "POST", ownPath, bad, bCandidate.ReviewETag), 400)
	}
	for _, raw := range []string{`{"request_id":"` + input.RequestID + `","team_id":"` + teamID + `","model_id":"` + models["b"] + `","reason":"why","reason":"other"}`, `{"request_id":"` + input.RequestID + `","team_id":"` + teamID + `","model_id":"` + models["b"] + `","reason":"why","approved":true}`} {
		expectStatus(t, sendRaw(applicant, "POST", ownPath, raw, bCandidate.ReviewETag), 400)
	}
	badUUID := input
	badUUID.RequestID = "44000000-0000-1000-8000-000000000001"
	expectStatus(t, send(applicant, "POST", ownPath, badUUID, bCandidate.ReviewETag), 400)
	already := input
	already.RequestID = uuid()
	already.ModelID = models["a"]
	expectStatus(t, send(applicant, "POST", ownPath, already, candidate(applicant, models["a"]).ReviewETag), 409)
	// Creation audit rollback cannot reserve the shared pair or original UUID.
	auditFailure.Store(true)
	failedCreate := send(applicant, "POST", ownPath, input, bCandidate.ReviewETag)
	auditFailure.Store(false)
	expectStatus(t, failedCreate, 500)
	if candidate(applicant, models["b"]).PendingRequest {
		t.Fatal("failed creation retained pending shared slot")
	}
	var wg sync.WaitGroup
	outcomes := make(chan *httptest.ResponseRecorder, 2)
	for range 2 {
		wg.Go(func() { outcomes <- send(applicant, "POST", ownPath, input, bCandidate.ReviewETag) })
	}
	wg.Wait()
	close(outcomes)
	statuses := map[int]int{}
	var pending service.TeamModelRequestDetail
	for res := range outcomes {
		if res.Code != 200 && res.Code != 201 {
			t.Fatalf("concurrent shared creation %d: %s", res.Code, res.Body.String())
		}
		statuses[res.Code]++
		row := decodeCatalogResponse[service.TeamModelRequestDetail](t, res, res.Code)
		if pending.ID != "" && pending.ID != row.ID {
			t.Fatal("concurrent creation duplicated original intent")
		}
		pending = row
	}
	if statuses[201] != 1 || statuses[200] != 1 || pending.ApplicantMembershipID != picker.Items[0].MembershipID || pending.TeamName != "Shared request Team" || pending.Status != entity.TeamModelRequestPending {
		t.Fatal("submission lost captured membership or stable intent", statuses, pending)
	}
	assertBool(pending.CurrentGranted, false, "pending grant")
	assertGrant(models["b"], false, nil)
	memberCandidate := candidate(member, models["b"])
	if !memberCandidate.PendingRequest || memberCandidate.OwnPendingRequestID != nil {
		t.Fatal("shared pending slot exposed another applicant's request identity")
	}
	otherInput := input
	otherInput.RequestID = uuid()
	expectStatus(t, send(member, "POST", ownPath, otherInput, memberCandidate.ReviewETag), 409)
	changed := input
	changed.Reason = "Changed captured intent"
	expectStatus(t, send(applicant, "POST", ownPath, changed, bCandidate.ReviewETag), 409)
	expectStatus(t, send(member, "POST", ownPath, input, bCandidate.ReviewETag), 409)
	expectStatus(t, send(member, "GET", ownPath+"/"+pending.ID, nil, ""), 404)
	self := detail(applicant, pending.ID, true)
	if len(self.AllowedActions) != 0 {
		t.Fatal("Team role union enabled self-review")
	}
	decision := service.TeamModelDecisionInput{DecisionID: uuid(), Action: "approve", Reason: "Independent shared review"}
	expectStatus(t, decide(applicant, self, decision, true), 403)
	review := detail(reviewer, pending.ID, true)
	if !slices.Equal(review.AllowedActions, []string{"approve", "reject"}) || review.ReviewETag == pending.ReviewETag {
		t.Fatal("review validator lost actor scope")
	}
	expectStatus(t, send(reviewer, "POST", reviewPath+"/"+pending.ID+"/decision", decision, pending.ReviewETag), 409)
	expectStatus(t, decide(writer, review, decision, true), 403)
	badDecision := decision
	badDecision.Action, badDecision.Reason = "reject", ""
	expectStatus(t, decide(reviewer, review, badDecision, true), 400)
	auditFailure.Store(true)
	failedDecision := decide(reviewer, review, decision, true)
	auditFailure.Store(false)
	expectStatus(t, failedDecision, 500)
	if detail(applicant, pending.ID, false).Status != entity.TeamModelRequestPending || !candidate(applicant, models["b"]).PendingRequest {
		t.Fatal("failed decision partially consumed pending slot")
	}
	assertGrant(models["b"], false, nil)
	approved := decodeCatalogResponse[service.TeamModelDecisionRecord](t, decide(reviewer, review, decision, true), 200)
	if !approved.Committed || approved.SavedRequest.Status != entity.TeamModelRequestApproved || approved.SavedRequest.Decision == nil || approved.ApplicationStatus != "applied" {
		t.Fatal("approval lost historical receipt", approved)
	}
	assertBool(approved.RuntimeApplied, true, "shared runtime application")
	assertGrant(models["b"], true, &pending.ID)
	assertGrant(models["a"], true, nil)
	retry := decodeCatalogResponse[service.TeamModelDecisionRecord](t, decide(reviewer, review, decision, true), 200)
	if !reflect.DeepEqual(approved.SavedRequest, retry.SavedRequest) {
		t.Fatal("exact terminal retry changed receipt")
	}
	badDecision = decision
	badDecision.Reason = "Changed historical action"
	expectStatus(t, decide(reviewer, review, badDecision, true), 409)
	// A later complete set preserves exact provenance on retained shared grants.
	if _, err := svc.SetResourceModels(ctx, admin.User.ID, service.TeamResource, teamID, []string{models["a"], models["b"], models["c"]}); err != nil {
		t.Fatal(err)
	}
	assertGrant(models["b"], true, &pending.ID)
	if !svc.RuntimeTeamModelGrantApplied(teamID, models["b"], pending.ID) {
		t.Fatal("unrelated complete replacement erased retained approval application")
	}
	expectStatus(t, native(applicant, "team-request-b"), 200)
	expectStatus(t, native(member, "team-request-b"), 200)
	before := nativeCalls.Load()
	expectStatus(t, native(outsider, "team-request-b"), 403)
	if nativeCalls.Load() != before {
		t.Fatal("outsider Team inference reached upstream")
	}
	keyReq := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"team-request-b","messages":[{"role":"user","content":"Personal isolation"}]}`))
	keyReq.Header.Set("Content-Type", "application/json")
	keyReq.Header.Set("Authorization", "Bearer "+personalKey.Secret)
	keyRes := httptest.NewRecorder()
	router.ServeHTTP(keyRes, keyReq)
	if keyRes.Code != 403 && keyRes.Code != 404 {
		t.Fatal("shared Team approval broadened Personal Key", keyRes.Code)
	}
	if nativeCalls.Load() != before {
		t.Fatal("Personal Key borrowed shared Team grant")
	}
	var personalGrantCount int64
	if err := db.Model(&entity.UserModelGrant{}).Where("model_id = ?", models["b"]).Count(&personalGrantCount).Error; err != nil || personalGrantCount != 0 {
		t.Fatal("shared approval changed Personal grants", err)
	}
	// Pending membership removal cancels, whereas an approved grant remains shared.
	leavePending, _, _ := create(applicant, models["d"])
	withoutApplicant := []service.TeamMemberInput{}
	for _, membership := range members {
		if membership.UserID != applicant.userID {
			withoutApplicant = append(withoutApplicant, membership)
		}
	}
	auditFailure.Store(true)
	_, removeErr := svc.SetTeamMembers(ctx, admin.User.ID, teamID, withoutApplicant)
	auditFailure.Store(false)
	if removeErr == nil || detail(applicant, leavePending.ID, false).Status != entity.TeamModelRequestPending {
		t.Fatal("membership audit rollback changed request")
	}
	if _, err := svc.SetTeamMembers(ctx, admin.User.ID, teamID, withoutApplicant); err != nil {
		t.Fatal(err)
	}
	left := detail(applicant, pending.ID, false)
	if left.CurrentTeam != nil || left.CurrentModel != nil || left.CurrentGranted != nil || left.CurrentMembershipMatches != nil || left.RuntimeApplied != nil || left.ApplicationStatus != "unavailable" {
		t.Fatal("former member read current Team facts through historical receipt")
	}
	cancelled := detail(applicant, leavePending.ID, false)
	if cancelled.Status != entity.TeamModelRequestCancelled || cancelled.Decision != nil || cancelled.ResolvedAt == nil || cancelled.CancelledReason == nil {
		t.Fatal("membership removal left actionable pending request")
	}
	sharedRetry := decodeCatalogResponse[service.TeamModelDecisionRecord](t, decide(reviewer, review, decision, true), 200)
	assertBool(sharedRetry.CurrentGranted, true, "shared grant after applicant exit")
	assertBool(sharedRetry.CurrentMembershipMatches, false, "captured applicant membership after exit")
	assertBool(sharedRetry.RuntimeApplied, true, "shared application after applicant exit")
	expectStatus(t, native(member, "team-request-b"), 200)
	before = nativeCalls.Load()
	expectStatus(t, native(applicant, "team-request-b"), 403)
	if nativeCalls.Load() != before {
		t.Fatal("removed membership dispatched Team inference")
	}
	creationRetry := decodeCatalogResponse[service.TeamModelRequestDetail](t, send(applicant, "POST", ownPath, input, bCandidate.ReviewETag), 200)
	if creationRetry.ID != pending.ID || creationRetry.ApplicationStatus != "unavailable" {
		t.Fatal("creation receipt retry borrowed current Team access")
	}
	if _, err := svc.SetTeamMembers(ctx, admin.User.ID, teamID, members); err != nil {
		t.Fatal(err)
	}
	rejoined := detail(applicant, pending.ID, false)
	assertBool(rejoined.CurrentMembershipMatches, false, "rejoined original membership")
	if detail(applicant, leavePending.ID, false).Status != entity.TeamModelRequestCancelled {
		t.Fatal("rejoin revived cancelled intent")
	}
	expectStatus(t, native(applicant, "team-request-b"), 200)
	// Rejected/withdrawn requests release the pair with one immutable winner.
	racing, _, _ := create(applicant, models["e"])
	racingReview := detail(reviewer, racing.ID, true)
	reject := service.TeamModelDecisionInput{DecisionID: uuid(), Action: "reject", Reason: "Not needed"}
	withdraw := service.TeamModelDecisionInput{DecisionID: uuid(), Action: "withdraw"}
	outcomes = make(chan *httptest.ResponseRecorder, 2)
	wg.Go(func() { outcomes <- decide(reviewer, racingReview, reject, true) })
	wg.Go(func() { outcomes <- decide(applicant, racing, withdraw, false) })
	wg.Wait()
	close(outcomes)
	statuses = map[int]int{}
	for res := range outcomes {
		statuses[res.Code]++
		if res.Code != 200 && res.Code != 409 {
			t.Fatalf("concurrent Team decision %d: %s", res.Code, res.Body.String())
		}
	}
	if statuses[200] != 1 || statuses[409] != 1 {
		t.Fatal("competing Team decisions produced multiple winners", statuses)
	}
	terminal := detail(applicant, racing.ID, false)
	if terminal.Decision == nil || candidate(applicant, models["e"]).PendingRequest {
		t.Fatal("terminal race retained shared pending pair")
	}
	next, _, _ := create(applicant, models["e"])
	nextReview := detail(reviewer, next.ID, true)
	collision := reject
	collision.DecisionID = terminal.Decision.DecisionID
	expectStatus(t, decide(reviewer, nextReview, collision, true), 409)
	// Current role definitions and Team assignment are checked at dispatch.
	roleLoss, _, _ := create(applicant, models["f"])
	beforeRoleLoss := detail(reviewer, roleLoss.ID, true)
	roles, err = svc.GetTeamRoles(ctx, admin.User.ID, teamID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetTeamRoles(ctx, admin.User.ID, teamID, roles.ETag, service.TeamRoleInput{RoleIDs: []string{}, Reason: "Remove scoped review"}); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, decide(reviewer, beforeRoleLoss, service.TeamModelDecisionInput{DecisionID: uuid(), Action: "approve"}, true), 403)
	platformReview := detail(platform, roleLoss.ID, true)
	reject = service.TeamModelDecisionInput{DecisionID: uuid(), Action: "reject", Reason: "Independent direct reviewer"}
	decodeCatalogResponse[service.TeamModelDecisionRecord](t, decide(platform, platformReview, reject, true), 200)
	roles, err = svc.GetTeamRoles(ctx, admin.User.ID, teamID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetTeamRoles(ctx, admin.User.ID, teamID, roles.ETag, service.TeamRoleInput{RoleIDs: []string{role.Role.ID}, Reason: "Restore explicit scope"}); err != nil {
		t.Fatal(err)
	}
	// Publication failure preserves a known historical commit without application.
	unpublished, _, _ := create(applicant, models["g"])
	unpublishedReview := detail(reviewer, unpublished.ID, true)
	publishIntent := service.TeamModelDecisionInput{DecisionID: uuid(), Action: "approve"}
	publicationFailure.Store(true)
	publishRes := decide(reviewer, unpublishedReview, publishIntent, true)
	publicationFailure.Store(false)
	unapplied := decodeCatalogResponse[service.TeamModelDecisionRecord](t, publishRes, 200)
	if !unapplied.Committed || unapplied.ApplicationStatus != "pending" {
		t.Fatal("publication failure lost durable Team decision")
	}
	assertBool(unapplied.RuntimeApplied, false, "unpublished shared grant")
	published := decodeCatalogResponse[service.TeamModelDecisionRecord](t, decide(reviewer, unpublishedReview, publishIntent, true), 200)
	assertBool(published.RuntimeApplied, true, "reconciled shared grant")
	if !reflect.DeepEqual(unapplied.SavedRequest, published.SavedRequest) {
		t.Fatal("publication retry changed first terminal receipt")
	}
	// Revocation is immediate and original retries never restore the grant.
	if _, err := svc.SetResourceModels(ctx, admin.User.ID, service.TeamResource, teamID, []string{models["a"], models["c"], models["g"]}); err != nil {
		t.Fatal(err)
	}
	before = nativeCalls.Load()
	expectStatus(t, native(member, "team-request-b"), 404)
	if nativeCalls.Load() != before {
		t.Fatal("revoked shared grant dispatched upstream")
	}
	superseded := decodeCatalogResponse[service.TeamModelDecisionRecord](t, decide(reviewer, review, decision, true), 200)
	if !superseded.Committed || superseded.ApplicationStatus != "superseded" {
		t.Fatal("historical approval restored removed shared grant")
	}
	assertBool(superseded.CurrentGranted, false, "removed shared grant")
	assertGrant(models["b"], false, nil)
	if _, err := svc.SetResourceModels(ctx, admin.User.ID, service.TeamResource, teamID, []string{models["a"], models["b"], models["c"], models["g"]}); err != nil {
		t.Fatal(err)
	}
	assertGrant(models["b"], true, nil)
	superseded = decodeCatalogResponse[service.TeamModelDecisionRecord](t, decide(reviewer, review, decision, true), 200)
	assertBool(superseded.RuntimeApplied, false, "independently restored grant")
	// Independent direct additions invalidate reviewed requests and retain their
	// own unknown provenance when a refreshed decision commits the history.
	independent, _, _ := create(applicant, models["m"])
	independentReview := detail(reviewer, independent.ID, true)
	if _, err := svc.SetResourceModels(ctx, admin.User.ID, service.TeamResource, teamID, []string{models["a"], models["b"], models["c"], models["g"], models["m"]}); err != nil {
		t.Fatal(err)
	}
	independentDecision := service.TeamModelDecisionInput{DecisionID: uuid(), Action: "approve"}
	expectStatus(t, decide(reviewer, independentReview, independentDecision, true), 409)
	freshIndependent := detail(reviewer, independent.ID, true)
	independentReceipt := decodeCatalogResponse[service.TeamModelDecisionRecord](t, decide(reviewer, freshIndependent, independentDecision, true), 200)
	if !independentReceipt.Committed || independentReceipt.ApplicationStatus != "superseded" {
		t.Fatal("approval claimed independently granted Team access")
	}
	assertGrant(models["m"], true, nil)
	// An inactive Model blocks approval while each terminal path stays usable.
	for _, testCase := range []struct{ letter, action string }{{"k", "reject"}, {"l", "withdraw"}} {
		inactive, _, _ := create(applicant, models[testCase.letter])
		if err := db.Model(&entity.Model{}).Where("id = ?", inactive.ModelID).Update("status", entity.ResourceDisabled).Error; err != nil {
			t.Fatal(err)
		}
		inactiveReview := detail(reviewer, inactive.ID, true)
		inactiveOwn := detail(applicant, inactive.ID, false)
		if !slices.Equal(inactiveReview.AllowedActions, []string{"reject"}) || !slices.Equal(inactiveOwn.AllowedActions, []string{"withdraw"}) {
			t.Fatal("inactive Model trapped request or exposed approval")
		}
		inactiveDecision := service.TeamModelDecisionInput{DecisionID: uuid(), Action: "approve"}
		expectStatus(t, decide(reviewer, inactiveReview, inactiveDecision, true), 409)
		inactiveDecision.Action = testCase.action
		who, read, reviewing := applicant, inactiveOwn, false
		if testCase.action == "reject" {
			who, read, reviewing = reviewer, inactiveReview, true
			inactiveDecision.Reason = "Current Model unavailable"
		}
		receipt := decodeCatalogResponse[service.TeamModelDecisionRecord](t, decide(who, read, inactiveDecision, reviewing), 200)
		replayed := decodeCatalogResponse[service.TeamModelDecisionRecord](t, decide(who, read, inactiveDecision, reviewing), 200)
		if !receipt.Committed || !reflect.DeepEqual(receipt.SavedRequest, replayed.SavedRequest) {
			t.Fatal("inactive terminal retry lost immutable receipt")
		}
		assertGrant(inactive.ModelID, false, nil)
	}
	// User, membership and Team lifecycle cancellation supports legacy identities.
	legacyPending, _, _ := create(legacy, models["h"])
	disabled := true
	if _, err := svc.UpdateMember(ctx, admin.User.ID, legacy.userID, &disabled, nil); err != nil {
		t.Fatal("legacy applicant disablement rejected canonical persisted identity", err)
	}
	if detail(reviewer, legacyPending.ID, true).Status != entity.TeamModelRequestCancelled {
		t.Fatal("legacy applicant disablement retained pending request")
	}
	enabled := false
	if _, err := svc.UpdateMember(ctx, admin.User.ID, legacy.userID, &enabled, nil); err != nil {
		t.Fatal(err)
	}
	legacySession, legacyCookie = readIdentity(t, identityRequest(router, "POST", "/api/v1/auth/login", `{"email":"team-model-legacy@example.invalid","password":"team-model-admin-password"}`, nil, ""))
	legacy.cookie, legacy.csrf = legacyCookie, legacySession.CSRFToken
	legacyOffboard, _, _ := create(legacy, models["h"])
	if _, err := svc.EmergencyOffboarding(ctx, admin.User.ID, legacy.userID, service.OffboardingEmergencyInput{RequestID: uuid(), CurrentPassword: "team-model-admin-password", Reason: "Legacy Team departure"}); err != nil {
		t.Fatal(err)
	}
	if detail(administrator, legacyOffboard.ID, true).Status != entity.TeamModelRequestCancelled {
		t.Fatal("offboarding retained pending shared request")
	}
	membershipPending, _, _ := create(applicant, models["i"])
	disabledMembers := []service.TeamMemberInput{}
	for _, membership := range members {
		if membership.UserID == legacy.userID {
			continue
		}
		if membership.UserID == applicant.userID {
			membership.Status = entity.ResourceDisabled
		}
		disabledMembers = append(disabledMembers, membership)
	}
	if _, err := svc.SetTeamMembers(ctx, admin.User.ID, teamID, disabledMembers); err != nil {
		t.Fatal(err)
	}
	if detail(administrator, membershipPending.ID, true).Status != entity.TeamModelRequestCancelled {
		t.Fatal("membership disablement retained pending shared request")
	}
	activeMembers := []service.TeamMemberInput{}
	for _, m := range members {
		if m.UserID != legacy.userID {
			activeMembers = append(activeMembers, m)
		}
	}
	if _, err := svc.SetTeamMembers(ctx, admin.User.ID, teamID, activeMembers); err != nil {
		t.Fatal(err)
	}
	teamPending, _, _ := create(applicant, models["j"])
	disabledStatus, activeStatus, archivedStatus := entity.ResourceDisabled, entity.ResourceActive, entity.ResourceArchived
	auditFailure.Store(true)
	_, disableErr := svc.UpdateResource(ctx, admin.User.ID, service.TeamResource, teamID, service.ResourceUpdate{Status: &disabledStatus})
	auditFailure.Store(false)
	if disableErr == nil || detail(applicant, teamPending.ID, false).Status != entity.TeamModelRequestPending {
		t.Fatal("Team lifecycle audit rollback changed request")
	}
	if _, err := svc.UpdateResource(ctx, admin.User.ID, service.TeamResource, teamID, service.ResourceUpdate{Status: &disabledStatus}); err != nil {
		t.Fatal(err)
	}
	if detail(administrator, teamPending.ID, true).Status != entity.TeamModelRequestCancelled {
		t.Fatal("Team disablement retained pending shared request")
	}
	if _, err := svc.UpdateResource(ctx, admin.User.ID, service.TeamResource, teamID, service.ResourceUpdate{Status: &activeStatus}); err != nil {
		t.Fatal(err)
	}
	if detail(applicant, teamPending.ID, false).Status != entity.TeamModelRequestCancelled {
		t.Fatal("Team reactivation revived cancelled intent")
	}
	// Pagination never crosses actor, reviewer, target or status boundaries.
	page := decodeCatalogResponse[service.TeamModelRequestPage](t, send(applicant, "GET", ownPath+"?limit=1", nil, ""), 200)
	if page.NextCursor == nil || len(page.Items) != 1 || page.Total < 5 {
		t.Fatal("history lacks bounded cursor pagination")
	}
	cursor := url.QueryEscape(*page.NextCursor)
	expectStatus(t, send(member, "GET", ownPath+"?cursor="+cursor, nil, ""), 400)
	expectStatus(t, send(reviewer, "GET", reviewPath+"?cursor="+cursor, nil, ""), 400)
	expectStatus(t, send(applicant, "GET", ownPath+"?status=pending&cursor="+cursor, nil, ""), 400)
	for _, query := range []string{"?limit=0", "?limit=51", "?status=completed", "?user_id=" + member.userID} {
		expectStatus(t, send(applicant, "GET", ownPath+query, nil, ""), 400)
	}
	// Alias identities never borrow exact receipts under MySQL collations.
	aliasResult, _, aliasErr := svc.CreateTeamModelRequest(ctx, strings.ToUpper(applicant.userID), bCandidate.ReviewETag, input)
	var denial *apperrors.Error
	if aliasResult != nil || !errors.As(aliasErr, &denial) || denial.Code != 401 {
		t.Fatal("actor alias obtained Team creation receipt", aliasErr)
	}
	aliasDecision, aliasErr := svc.DecideTeamModelRequest(ctx, strings.ToUpper(reviewer.userID), teamID, pending.ID, review.ReviewETag, decision, true)
	denial = nil
	if aliasDecision != nil || !errors.As(aliasErr, &denial) || denial.Code != 401 {
		t.Fatal("reviewer alias obtained Team decision receipt", aliasErr)
	}
	if _, err := svc.GetTeamModelRequestCandidate(ctx, applicant.userID, strings.ToUpper(teamID), models["k"]); err == nil {
		t.Fatal("Team alias borrowed candidate scope")
	}
	wire := send(applicant, "GET", ownPath+"/"+pending.ID, nil, "").Body.String()
	for _, private := range []string{"request_hash", "decision_request_hash", "ciphertext", "token_hash", "team-request-test-secret"} {
		if strings.Contains(wire, private) {
			t.Fatal("request DTO leaked private implementation fact", private)
		}
	}
	var audits int64
	if err := db.Model(&entity.AuditEvent{}).Where("resource_id = ? AND action = ?", pending.ID, "team.model_request.approve").Count(&audits).Error; err != nil || audits != 1 {
		t.Fatal("approval retry duplicated audit", audits, err)
	}
	if _, err := svc.UpdateResource(ctx, admin.User.ID, service.TeamResource, teamID, service.ResourceUpdate{Status: &disabledStatus}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdateResource(ctx, admin.User.ID, service.TeamResource, teamID, service.ResourceUpdate{Status: &archivedStatus}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdateResource(ctx, admin.User.ID, service.TeamResource, teamID, service.ResourceUpdate{Status: &activeStatus}); err == nil {
		t.Fatal("archived Team accepted reactivation")
	}
	assertGrant(models["b"], true, nil)
}
