package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"gorm.io/gorm"
)

func testTeamQuotaRequestLifecycle(t *testing.T, db *gorm.DB) {
	ctx := context.Background()
	var failPublication atomic.Bool
	callback := "test_team_quota_request_publication"
	if err := db.Callback().Query().Before("gorm:query").Register(callback, func(tx *gorm.DB) {
		if failPublication.Load() && tx.Statement.Table == "models" {
			_ = tx.AddError(errors.New("controlled request publication outage"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Callback().Query().Remove(callback) })
	svc, err := service.New(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	router := fox.New()
	New(svc).RegisterRoutes(router)
	setup := identityRequest(router, "POST", "/api/v1/setup", `{"email":"team-request-admin@example.invalid","password":"test-only-team-request-password","name":"Request administrator"}`, nil, "")
	expectStatus(t, setup, 201)
	admin, adminCookie := readIdentity(t, setup)
	owner, ownerCookie, ownerCSRF := createSystemStatusMember(t, svc, router, admin.User.ID, "request-owner", nil)
	otherOwner, otherOwnerCookie, otherOwnerCSRF := createSystemStatusMember(t, svc, router, admin.User.ID, "request-other-owner", nil)
	applicant, applicantCookie, applicantCSRF := createSystemStatusMember(t, svc, router, admin.User.ID, "request-applicant", nil)
	_, tokenCookie, tokenCSRF := createSystemStatusMember(t, svc, router, admin.User.ID, "request-token-admin", []string{"teams.tokens.write"})
	_, moneyCookie, moneyCSRF := createSystemStatusMember(t, svc, router, admin.User.ID, "request-money-admin", []string{"teams.money.write"})
	_, readerCookie, readerCSRF := createSystemStatusMember(t, svc, router, admin.User.ID, "request-global-reader", []string{"teams.quota_requests.read_all"})
	_, strangerCookie, _ := createSystemStatusMember(t, svc, router, admin.User.ID, "request-stranger", nil)
	if err := svc.StartCallRecorder(ctx, filepath.Join(t.TempDir(), "request-journal.db")); err != nil {
		t.Fatal(err)
	}
	defer func() {
		svc.StopRuntime()
		if err := svc.StopCallRecorder(); err != nil {
			t.Error(err)
		}
	}()
	if err := svc.StartRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	svc.StopRuntime()
	team, err := svc.CreateResource(ctx, admin.User.ID, service.TeamResource, "Monthly requests", "Current owner first", []string{owner.User.ID, otherOwner.User.ID})
	if err != nil {
		t.Fatal(err)
	}
	members := []service.TeamMemberInput{{UserID: owner.User.ID, Role: entity.TeamOwner, Status: entity.ResourceActive}, {UserID: otherOwner.User.ID, Role: entity.TeamOwner, Status: entity.ResourceActive}, {UserID: applicant.User.ID, Role: entity.TeamMember, Status: entity.ResourceActive}}
	if _, err := svc.SetTeamMembers(ctx, admin.User.ID, team.ID, members); err != nil {
		t.Fatal(err)
	}
	refresh := func() {
		t.Helper()
		if err := svc.RefreshRuntime(ctx); err != nil {
			t.Fatal(err)
		}
	}
	request := func(method, path, body, etag string, cookie *http.Cookie, csrf string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, "http://routex.test"+path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if etag != "" {
			req.Header.Set("If-Match", `"`+etag+`"`)
		}
		if cookie != nil {
			req.AddCookie(cookie)
		}
		if csrf != "" {
			req.Header.Set("X-CSRF-Token", csrf)
		}
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		return res
	}
	decode := func(res *httptest.ResponseRecorder, target any) {
		t.Helper()
		if err := json.Unmarshal(res.Body.Bytes(), target); err != nil {
			t.Fatal(err, res.Body.String())
		}
	}
	parentPath := "/api/v1/teams/" + team.ID + "/limits"
	childPath := "/api/v1/teams/" + team.ID + "/members/" + applicant.User.ID + "/limits"
	getLimit := func(path string) service.LimitRecord {
		t.Helper()
		refresh()
		res := request("GET", path, "", "", adminCookie, "")
		expectStatus(t, res, 200)
		var result service.LimitRecord
		decode(res, &result)
		return result
	}
	putLimit := func(path, body string) {
		t.Helper()
		record := getLimit(path)
		expectStatus(t, request("PUT", path, body, record.ETag, adminCookie, admin.CSRFToken), 200)
	}
	putLimit(parentPath, `{"tokens_month":10,"money_month":"20","currency":"USD","rpm":100,"reason":"Initial aggregate"}`)
	putLimit(childPath, `{"tokens_month":5,"money_month":"5","currency":"USD","rpm":90,"reason":"Initial member"}`)
	putLimit(parentPath, `{"rpm":10,"reason":"Reduced rate leaves stored child override"}`)
	var sequence int
	uuid := func() string { sequence++; return fmt.Sprintf("00000000-0000-4000-8000-%012d", sequence) }
	contextPath := "/api/v1/teams/" + team.ID + "/quota-request-context"
	getContext := func(dimension string) service.TeamQuotaRequestContext {
		t.Helper()
		refresh()
		res := request("GET", contextPath+"?dimension="+dimension, "", "", applicantCookie, "")
		expectStatus(t, res, 200)
		var result service.TeamQuotaRequestContext
		decode(res, &result)
		if res.Header().Get("ETag") != `"`+result.ETag+`"` || !result.Eligible {
			t.Fatal("incoherent current context", res.Body.String())
		}
		return result
	}
	getDetail := func(id string, cookie *http.Cookie) service.TeamQuotaRequestDetail {
		t.Helper()
		refresh()
		res := request("GET", "/api/v1/quota-requests/"+id, "", "", cookie, "")
		expectStatus(t, res, 200)
		var result service.TeamQuotaRequestDetail
		decode(res, &result)
		if res.Header().Get("ETag") != `"`+result.ETag+`"` {
			t.Fatal("review validator differs from body")
		}
		return result
	}
	create := func(dimension, target string) (service.TeamQuotaRequestDetail, string, string) {
		t.Helper()
		current := getContext(dimension)
		body := fmt.Sprintf(`{"request_id":%q,"dimension":%q,"target_value":%q,"reason":"Monthly increase"}`, uuid(), dimension, target)
		res := request("POST", "/api/v1/teams/"+team.ID+"/quota-requests", body, current.ETag, applicantCookie, applicantCSRF)
		expectStatus(t, res, 201)
		var result service.TeamQuotaRequestDetail
		decode(res, &result)
		return result, body, current.ETag
	}
	decisionBody := func(detail service.TeamQuotaRequestDetail, action, reason string) string {
		t.Helper()
		if detail.CurrentStepID == nil {
			t.Fatal("missing pending step")
		}
		return fmt.Sprintf(`{"decision_id":%q,"step_id":%q,"action":%q,"reason":%q}`, uuid(), *detail.CurrentStepID, action, reason)
	}
	decide := func(detail service.TeamQuotaRequestDetail, body string, cookie *http.Cookie, csrf string) service.TeamQuotaDecisionRecord {
		t.Helper()
		res := request("POST", "/api/v1/quota-requests/"+detail.ID+"/decision", body, detail.ETag, cookie, csrf)
		expectStatus(t, res, 200)
		var result service.TeamQuotaDecisionRecord
		decode(res, &result)
		if !result.Committed {
			t.Fatal("missing durable receipt")
		}
		return result
	}
	list := func(path string, cookie *http.Cookie) service.TeamQuotaRequestPage {
		t.Helper()
		res := request("GET", path, "", "", cookie, "")
		expectStatus(t, res, 200)
		var result service.TeamQuotaRequestPage
		decode(res, &result)
		return result
	}
	auditCount := func(id, action string) int64 {
		t.Helper()
		var count int64
		if err := db.Model(&entity.AuditEvent{}).Where("resource_id = ? AND action = ?", id, action).Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		return count
	}
	// Read and write boundaries, strict body/header/query and exact canonical paths.
	expectStatus(t, request("GET", contextPath+"?dimension=tokens", "", "", nil, ""), 401)
	expectStatus(t, request("GET", contextPath+"?dimension=tokens", "", "", strangerCookie, ""), 404)
	expectStatus(t, request("GET", contextPath+"?dimension=tokens&dimension=money", "", "", applicantCookie, ""), 400)
	expectStatus(t, request("GET", contextPath+"?dimension=tokens&user_id="+owner.User.ID, "", "", applicantCookie, ""), 400)
	expectStatus(t, request("GET", "/api/v1/teams/"+strings.ToUpper(team.ID)+"/quota-request-context?dimension=tokens", "", "", applicantCookie, ""), 404)
	initial := getContext("tokens")
	if initial.MemberStored == nil || *initial.MemberStored != "5" || initial.MemberEffective == nil || *initial.MemberEffective != "5" || initial.TeamEffective == nil || *initial.TeamEffective != "10" || initial.MemberUsage == nil || initial.TeamUsage == nil {
		t.Fatal("missing stored/effective/authoritative snapshot", initial)
	}
	for _, body := range []string{`{"request_id":"00000000-0000-4000-8000-000000000099","dimension":"tokens","target_value":10,"reason":"Typed"}`, `{"request_id":"00000000-0000-4000-8000-000000000099","dimension":"tokens","target_value":"10","currency":"USD","reason":"Unknown"}`, `{"request_id":"00000000-0000-4000-8000-000000000099","dimension":"tokens","target_value":"10","target_value":"11","reason":"Duplicate"}`} {
		expectStatus(t, request("POST", "/api/v1/teams/"+team.ID+"/quota-requests", body, initial.ETag, applicantCookie, applicantCSRF), 400)
	}
	valid := fmt.Sprintf(`{"request_id":%q,"dimension":"tokens","target_value":"10","reason":"Reviewed"}`, uuid())
	expectStatus(t, request("POST", "/api/v1/teams/"+team.ID+"/quota-requests", valid, initial.ETag, applicantCookie, ""), 403)
	expectStatus(t, request("POST", "/api/v1/teams/"+team.ID+"/quota-requests", valid, "", applicantCookie, applicantCSRF), 400)
	expectStatus(t, request("POST", "/api/v1/teams/"+team.ID+"/quota-requests", valid, strings.Repeat("a", 64), applicantCookie, applicantCSRF), 409)
	// Owner approval remains monthly-only despite an unrelated retained rate override.
	first, firstBody, firstReview := create("tokens", "10")
	if first.Status != entity.TeamQuotaRequestPendingOwner || len(first.Steps) != 1 || first.Steps[0].Stage != entity.TeamQuotaStageOwner {
		t.Fatal("owner stage skipped", first)
	}
	// Database domain checks cannot express cross-row workflow coherence. A raw
	// admin step on a pending-owner request must not grant a platform bypass.
	beforeMalformedChild, beforeMalformedParent := getLimit(childPath), getLimit(parentPath)
	if err := db.Model(&entity.TeamQuotaRequestStep{}).Where("id = ?", first.Steps[0].ID).Update("Stage", entity.TeamQuotaStageAdmin).Error; err != nil {
		t.Fatal(err)
	}
	malformed := getDetail(first.ID, tokenCookie)
	if len(malformed.AllowedActions) != 0 || malformed.WorkspaceAvailable || malformed.ApprovalPreview != nil {
		t.Fatal("malformed current stage acquired approval authority", malformed)
	}
	expectStatus(t, request("POST", "/api/v1/quota-requests/"+first.ID+"/decision", decisionBody(malformed, "approve", ""), malformed.ETag, tokenCookie, tokenCSRF), 409)
	afterMalformedChild, afterMalformedParent := getLimit(childPath), getLimit(parentPath)
	if afterMalformedChild.ETag != beforeMalformedChild.ETag || afterMalformedParent.ETag != beforeMalformedParent.ETag || auditCount(first.ID, "team.quota_request.approve") != 0 {
		t.Fatal("malformed stage changed policy or audit")
	}
	if err := db.Model(&entity.TeamQuotaRequestStep{}).Where("id = ?", first.Steps[0].ID).Update("Stage", entity.TeamQuotaStageOwner).Error; err != nil {
		t.Fatal(err)
	}
	replay := request("POST", "/api/v1/teams/"+team.ID+"/quota-requests", firstBody, firstReview, applicantCookie, applicantCSRF)
	expectStatus(t, replay, 200)
	var recreated service.TeamQuotaRequestDetail
	decode(replay, &recreated)
	if recreated.ID != first.ID || auditCount(first.ID, "team.quota_request.create") != 1 {
		t.Fatal("creation replay duplicated request")
	}
	_, duplicateBody, _ := func() (service.TeamQuotaRequestDetail, string, string) {
		current := getContext("tokens")
		body := fmt.Sprintf(`{"request_id":%q,"dimension":"tokens","target_value":"11","reason":"One pending slot"}`, uuid())
		expectStatus(t, request("POST", "/api/v1/teams/"+team.ID+"/quota-requests", body, current.ETag, applicantCookie, applicantCSRF), 409)
		return service.TeamQuotaRequestDetail{}, body, current.ETag
	}()
	_ = duplicateBody
	expectStatus(t, request("GET", "/api/v1/quota-requests/"+first.ID, "", "", tokenCookie, ""), 404)
	expectStatus(t, request("GET", "/api/v1/quota-requests/"+first.ID, "", "", strangerCookie, ""), 404)
	own := getDetail(first.ID, applicantCookie)
	if own.WorkspaceAvailable || own.ApprovalPreview != nil {
		t.Fatal("applicant withdraw received reviewer link/preview", own)
	}
	expectStatus(t, request("POST", "/api/v1/quota-requests/"+first.ID+"/decision", decisionBody(own, "approve", ""), own.ETag, applicantCookie, applicantCSRF), 403)
	review := getDetail(first.ID, ownerCookie)
	if !review.WorkspaceAvailable || review.ApprovalPreview == nil || review.ApprovalPreview.Escalates || review.ApprovalPreview.MemberAfter != "10" || review.ApprovalPreview.TeamAfter == nil || *review.ApprovalPreview.TeamAfter != "10" {
		t.Fatal("within-parent review preview incorrect", review)
	}
	firstDecision := decisionBody(review, "approve", "")
	firstSaved := decide(review, firstDecision, ownerCookie, ownerCSRF)
	if firstSaved.Request.Status != entity.TeamQuotaRequestApproved || firstSaved.Request.Application == nil || !firstSaved.Request.Application.RuntimeApplied {
		t.Fatal("monthly owner approval not applied", firstSaved)
	}
	child := getLimit(childPath)
	if child.Stored.TokensMonth == nil || *child.Stored.TokensMonth != 10 || child.Stored.RPM == nil || *child.Stored.RPM != 90 || child.Effective.RPM == nil || *child.Effective.RPM != 10 {
		t.Fatal("monthly approval rewrote retained rates", child)
	}
	decide(review, firstDecision, ownerCookie, ownerCSRF)
	if auditCount(first.ID, "team.quota_request.approve") != 1 {
		t.Fatal("decision replay duplicated audit")
	}
	// Overflow still starts with an owner; its saved receipt cannot advance stage two.
	overflow, _, _ := create("tokens", "15")
	review = getDetail(overflow.ID, ownerCookie)
	if review.ApprovalPreview == nil || !review.ApprovalPreview.Escalates || review.ApprovalPreview.MemberAfter != "10" || review.ApprovalPreview.TeamAfter == nil || *review.ApprovalPreview.TeamAfter != "10" {
		t.Fatal("owner overflow preview falsely changes caps", review)
	}
	ownerIntent := decisionBody(review, "approve", "Reviewed overflow")
	ownerSaved := decide(review, ownerIntent, ownerCookie, ownerCSRF)
	if ownerSaved.Request.Status != entity.TeamQuotaRequestPendingAdmin || ownerSaved.SavedStep.Status != entity.TeamQuotaStepApproved || ownerSaved.Request.CurrentStepID == nil || *ownerSaved.Request.CurrentStepID == ownerSaved.SavedStep.ID {
		t.Fatal("overflow stage transition incorrect", ownerSaved)
	}
	child = getLimit(childPath)
	parent := getLimit(parentPath)
	if *child.Stored.TokensMonth != 10 || *parent.Stored.TokensMonth != 10 {
		t.Fatal("owner overflow changed policy before platform review")
	}
	replaySaved := decide(review, ownerIntent, ownerCookie, ownerCSRF)
	if replaySaved.SavedStep.ID != ownerSaved.SavedStep.ID || replaySaved.Request.Status != entity.TeamQuotaRequestPendingAdmin {
		t.Fatal("historical owner receipt advanced new stage")
	}
	expectStatus(t, request("POST", "/api/v1/quota-requests/"+overflow.ID+"/decision", strings.Replace(ownerIntent, "Reviewed overflow", "Changed intent", 1), review.ETag, ownerCookie, ownerCSRF), 409)
	otherReview := getDetail(overflow.ID, otherOwnerCookie)
	expectStatus(t, request("POST", "/api/v1/quota-requests/"+overflow.ID+"/decision", decisionBody(otherReview, "approve", ""), otherReview.ETag, otherOwnerCookie, otherOwnerCSRF), 403)
	platformReview := getDetail(overflow.ID, tokenCookie)
	if !platformReview.WorkspaceAvailable || platformReview.ApprovalPreview == nil || platformReview.ApprovalPreview.Escalates || platformReview.ApprovalPreview.MemberAfter != "15" || platformReview.ApprovalPreview.TeamAfter == nil || *platformReview.ApprovalPreview.TeamAfter != "15" {
		t.Fatal("platform atomic preview missing", platformReview)
	}
	globalPending := request("GET", "/api/v1/admin/quota-requests/"+overflow.ID, "", "", readerCookie, "")
	expectStatus(t, globalPending, 200)
	var globalPendingDetail service.TeamQuotaRequestDetail
	decode(globalPending, &globalPendingDetail)
	if globalPendingDetail.WorkspaceAvailable || globalPendingDetail.ApprovalPreview != nil || len(globalPendingDetail.AllowedActions) != 0 {
		t.Fatal("global read-all acquired approval preview/link", globalPendingDetail)
	}
	globalPending = request("GET", "/api/v1/admin/quota-requests/"+overflow.ID, "", "", adminCookie, "")
	expectStatus(t, globalPending, 200)
	decode(globalPending, &globalPendingDetail)
	if !globalPendingDetail.WorkspaceAvailable || globalPendingDetail.ApprovalPreview != nil || len(globalPendingDetail.AllowedActions) != 0 {
		t.Fatal("actual assigned platform reviewer link missing or global actions exposed", globalPendingDetail)
	}
	platformIntent := decisionBody(platformReview, "approve", "")
	platformSaved := decide(platformReview, platformIntent, tokenCookie, tokenCSRF)
	if platformSaved.Request.Status != entity.TeamQuotaRequestApproved || platformSaved.Request.Application == nil || !platformSaved.Request.Application.RuntimeApplied {
		t.Fatal("final atomic approval not applied", platformSaved)
	}
	child = getLimit(childPath)
	parent = getLimit(parentPath)
	if *child.Stored.TokensMonth != 15 || *parent.Stored.TokensMonth != 15 {
		t.Fatal("aggregate/member raise not atomic")
	}
	// Global records permission stays read-only and separate from workflow authority.
	global := list("/api/v1/admin/quota-requests", readerCookie)
	if global.Total != 2 || len(global.Items) != 2 {
		t.Fatal("global history incomplete", global)
	}
	globalDetail := request("GET", "/api/v1/admin/quota-requests/"+overflow.ID, "", "", readerCookie, "")
	expectStatus(t, globalDetail, 200)
	var gd service.TeamQuotaRequestDetail
	decode(globalDetail, &gd)
	if len(gd.AllowedActions) != 0 || gd.WorkspaceAvailable || gd.ApprovalPreview != nil {
		t.Fatal("read-all grants review authority")
	}
	expectStatus(t, request("GET", "/api/v1/admin/quota-requests", "", "", ownerCookie, ""), 403)
	expectStatus(t, request("POST", "/api/v1/quota-requests/"+overflow.ID+"/decision", platformIntent, platformReview.ETag, readerCookie, readerCSRF), 404)
	pending := list("/api/v1/quota-requests?view=pending", ownerCookie)
	if pending.Total != 0 {
		t.Fatal("terminal record still pending")
	}
	history := list("/api/v1/quota-requests?view=my&limit=1", applicantCookie)
	if history.Total != 2 || history.NextCursor == nil {
		t.Fatal("bounded history cursor missing", history)
	}
	next := list("/api/v1/quota-requests?view=my&limit=1&cursor="+*history.NextCursor, applicantCookie)
	if len(next.Items) != 1 || next.Items[0].ID == history.Items[0].ID {
		t.Fatal("history cursor duplicates rows")
	}
	expectStatus(t, request("GET", "/api/v1/quota-requests?view=pending&cursor="+*history.NextCursor, "", "", ownerCookie, ""), 400)
	// Money authority and exact decimal values remain independent from token approval.
	money, _, _ := create("money", "25.000000000000000001")
	moneyReview := getDetail(money.ID, ownerCookie)
	decide(moneyReview, decisionBody(moneyReview, "approve", ""), ownerCookie, ownerCSRF)
	moneyReview = getDetail(money.ID, moneyCookie)
	expectStatus(t, request("GET", "/api/v1/quota-requests/"+money.ID, "", "", tokenCookie, ""), 404)
	moneySaved := decide(moneyReview, decisionBody(moneyReview, "approve", "Exact denomination"), moneyCookie, moneyCSRF)
	if moneySaved.Request.Currency == nil || *moneySaved.Request.Currency != "USD" || moneySaved.Request.TargetValue != "25.000000000000000001" {
		t.Fatal("decimal intent changed", moneySaved)
	}
	child = getLimit(childPath)
	parent = getLimit(parentPath)
	if child.Stored.MoneyMonth == nil || *child.Stored.MoneyMonth != "25.000000000000000001" || parent.Stored.MoneyMonth == nil || *parent.Stored.MoneyMonth != "25.000000000000000001" || *child.Stored.TokensMonth != 15 {
		t.Fatal("money approval changed precision or token dimension")
	}
	// Publication failure acknowledges saved history, never claims applied; retry reconciles.
	unpublished, _, _ := create("tokens", "20")
	r := getDetail(unpublished.ID, ownerCookie)
	decide(r, decisionBody(r, "approve", ""), ownerCookie, ownerCSRF)
	r = getDetail(unpublished.ID, tokenCookie)
	intent := decisionBody(r, "approve", "")
	failPublication.Store(true)
	saved := decide(r, intent, tokenCookie, tokenCSRF)
	if saved.Request.Status != entity.TeamQuotaRequestApproved || saved.Request.Application == nil || saved.Request.Application.RuntimeApplied || saved.Request.Application.ApplicationStatus != "pending" {
		t.Fatal("failed publication claimed enforcement", saved)
	}
	failPublication.Store(false)
	saved = decide(r, intent, tokenCookie, tokenCSRF)
	if !saved.Request.Application.RuntimeApplied || auditCount(unpublished.ID, "team.quota_request.approve") != 2 {
		t.Fatal("retry failed to reconcile exactly once per stage", saved)
	}
	putLimit(childPath, `{"tokens_month":12,"rpm":10,"reason":"Later policy supersedes approved request and narrows retained rate"}`)
	saved = decide(r, intent, tokenCookie, tokenCSRF)
	child = getLimit(childPath)
	if saved.Request.Application.ApplicationStatus != "superseded" || *child.Stored.TokensMonth != 12 {
		t.Fatal("historical receipt restored superseded policy")
	}
	// Pending membership loss is terminal; rejoining the same stable pair cannot revive it.
	cancelled, _, _ := create("tokens", "16")
	if _, err := svc.SetTeamMembers(ctx, admin.User.ID, team.ID, members[:2]); err != nil {
		t.Fatal(err)
	}
	cancelledDetail := getDetail(cancelled.ID, applicantCookie)
	if cancelledDetail.Status != entity.TeamQuotaRequestCancelled || cancelledDetail.CurrentContext != nil || cancelledDetail.CurrentStepID != nil {
		t.Fatal("removed membership left actionable request", cancelledDetail)
	}
	if _, err := svc.SetTeamMembers(ctx, admin.User.ID, team.ID, members); err != nil {
		t.Fatal(err)
	}
	cancelledDetail = getDetail(cancelled.ID, applicantCookie)
	if cancelledDetail.Status != entity.TeamQuotaRequestCancelled || cancelledDetail.CurrentContext != nil {
		t.Fatal("rejoin revived old application")
	}
	replacement, _, _ := create("tokens", "16")
	withdrawReview := getDetail(replacement.ID, applicantCookie)
	withdraw := decide(withdrawReview, decisionBody(withdrawReview, "withdraw", ""), applicantCookie, applicantCSRF)
	if withdraw.Request.Status != entity.TeamQuotaRequestWithdrawn {
		t.Fatal("withdraw failed")
	}
	rejected, _, _ := create("tokens", "16")
	rejectReview := getDetail(rejected.ID, ownerCookie)
	expectStatus(t, request("POST", "/api/v1/quota-requests/"+rejected.ID+"/decision", decisionBody(rejectReview, "reject", ""), rejectReview.ETag, ownerCookie, ownerCSRF), 400)
	reject := decide(rejectReview, decisionBody(rejectReview, "reject", "Not required"), ownerCookie, ownerCSRF)
	if reject.Request.Status != entity.TeamQuotaRequestRejected {
		t.Fatal("rejection failed")
	}
	// A lost owner advances the original pending application instead of leaving it stranded.
	ownerLost, _, _ := create("tokens", "16")
	noOwners := []service.TeamMemberInput{{UserID: applicant.User.ID, Role: entity.TeamOwner, Status: entity.ResourceActive}}
	if _, err := svc.SetTeamMembers(ctx, admin.User.ID, team.ID, noOwners); err != nil {
		t.Fatal(err)
	}
	transitioned := getDetail(ownerLost.ID, tokenCookie)
	if transitioned.Status != entity.TeamQuotaRequestPendingAdmin || len(transitioned.Steps) != 2 || transitioned.Steps[0].Status != entity.TeamQuotaStepCancelled || transitioned.EscalationReason == nil || *transitioned.EscalationReason != "owner_unavailable" {
		t.Fatal("lost owner not escalated", transitioned)
	}
	ownReview := getDetail(ownerLost.ID, applicantCookie)
	if slices.Contains(ownReview.AllowedActions, "approve") {
		t.Fatal("self owner received approve authority")
	}
	decide(transitioned, decisionBody(transitioned, "reject", "Owner unavailable"), tokenCookie, tokenCSRF)
	// No non-self owner at submission uses only the platform step, never self review.
	direct, _, _ := create("tokens", "16")
	if direct.Status != entity.TeamQuotaRequestPendingAdmin || len(direct.Steps) != 1 || direct.Steps[0].Stage != entity.TeamQuotaStageAdmin {
		t.Fatal("no-owner initial stage incorrect", direct)
	}
	noOwnerReview := getDetail(direct.ID, tokenCookie)
	decide(noOwnerReview, decisionBody(noOwnerReview, "approve", ""), tokenCookie, tokenCSRF)
	// Team and account lifecycle ABA never revives the original pending intent.
	teamABA, _, _ := create("tokens", "18")
	disabledStatus, activeStatus := entity.ResourceDisabled, entity.ResourceActive
	if _, err := svc.UpdateResource(ctx, admin.User.ID, service.TeamResource, team.ID, service.ResourceUpdate{Status: &disabledStatus}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdateResource(ctx, admin.User.ID, service.TeamResource, team.ID, service.ResourceUpdate{Status: &activeStatus}); err != nil {
		t.Fatal(err)
	}
	abaDetail := getDetail(teamABA.ID, applicantCookie)
	if abaDetail.Status != entity.TeamQuotaRequestCancelled || abaDetail.CurrentStepID != nil {
		t.Fatal("Team lifecycle ABA revived pending application", abaDetail)
	}
	if _, err := svc.SetTeamMembers(ctx, admin.User.ID, team.ID, members); err != nil {
		t.Fatal(err)
	}
	userABA, _, _ := create("tokens", "18")
	disabled, enabled := true, false
	if _, err := svc.UpdateMember(ctx, admin.User.ID, applicant.User.ID, &disabled, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdateMember(ctx, admin.User.ID, applicant.User.ID, &enabled, nil); err != nil {
		t.Fatal(err)
	}
	login := identityRequest(router, "POST", "/api/v1/auth/login", `{"email":"system-request-applicant@example.invalid","password":"test-only-system-password"}`, nil, "")
	expectStatus(t, login, 200)
	newAuthentication, newCookie := readIdentity(t, login)
	applicantCookie, applicantCSRF = newCookie, newAuthentication.CSRFToken
	abaDetail = getDetail(userABA.ID, applicantCookie)
	if abaDetail.Status != entity.TeamQuotaRequestCancelled || abaDetail.CurrentStepID != nil {
		t.Fatal("account lifecycle ABA revived pending application", abaDetail)
	}
	var slots int64
	if err := db.Model(&entity.TeamQuotaPendingSlot{}).Count(&slots).Error; err != nil || slots != 0 {
		t.Fatal("terminal pending slots retained", slots, err)
	}
	// Public DTOs contain only bounded typed snapshots, not policy JSON or credentials.
	body := request("GET", "/api/v1/quota-requests/"+direct.ID, "", "", applicantCookie, "").Body.String()
	for _, forbidden := range []string{"approved_team_policy_json", "approved_member_policy_json", "request_hash", "decision_hash", "token_hash", "ciphertext", "test-only-team-request-password"} {
		if strings.Contains(body, forbidden) {
			t.Fatal("private data in request projection", forbidden)
		}
	}
	// Emergency offboarding cancels pending workflow without deleting historical
	// pair policy or moving its durable quota account.
	offboardPending, _, _ := create("tokens", "18")
	beforePair := getLimit(childPath)
	var beforeRow entity.ResourceLimit
	if err := db.Where("scope_kind = ? AND scope_id = ?", "team_member", strings.TrimPrefix(beforePair.AccountID, "team_member_")).First(&beforeRow).Error; err != nil {
		t.Fatal(err)
	}
	beforePolicy, err := json.Marshal(beforeRow)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.EmergencyOffboarding(ctx, admin.User.ID, applicant.User.ID, service.OffboardingEmergencyInput{RequestID: uuid(), CurrentPassword: "test-only-team-request-password", Reason: "Controlled member departure"}); err != nil {
		t.Fatal(err)
	}
	offboardDetailResponse := request("GET", "/api/v1/admin/quota-requests/"+offboardPending.ID, "", "", adminCookie, "")
	expectStatus(t, offboardDetailResponse, 200)
	var offboardDetail service.TeamQuotaRequestDetail
	decode(offboardDetailResponse, &offboardDetail)
	if offboardDetail.Status != entity.TeamQuotaRequestCancelled || offboardDetail.CurrentContext != nil || offboardDetail.WorkspaceAvailable || offboardDetail.ApprovalPreview != nil || len(offboardDetail.AllowedActions) != 0 {
		t.Fatal("offboarding left actionable application", offboardDetail)
	}
	var afterRow entity.ResourceLimit
	if err := db.Where("scope_kind = ? AND scope_id = ?", "team_member", strings.TrimPrefix(beforePair.AccountID, "team_member_")).First(&afterRow).Error; err != nil {
		t.Fatal(err)
	}
	afterPolicy, err := json.Marshal(afterRow)
	if err != nil || string(beforePolicy) != string(afterPolicy) {
		t.Fatal("offboarding reset historical pair policy", err)
	}
	if err := db.Model(&entity.TeamQuotaPendingSlot{}).Where("request_id = ?", offboardPending.ID).Count(&slots).Error; err != nil || slots != 0 {
		t.Fatal("offboarding retained pending slot", slots, err)
	}
}
