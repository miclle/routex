package handler

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fox-gonic/fox"
	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
)

// Called by the single database lifecycle owner on a fresh migrated database.
func testCallLifecycle(t *testing.T, db *gorm.DB) {
	router := identityRouter(t, db)
	setup := identityRequest(router, "POST", "/api/v1/setup", `{"email":"calls@example.com","password":"calls-password","name":"Calls admin"}`, nil, "")
	expectStatus(t, setup, 201)
	admin, adminCookie := readIdentity(t, setup)
	var administrator entity.User
	if err := db.First(&administrator, "id = ?", admin.User.ID).Error; err != nil {
		t.Fatal(err)
	}
	member := entity.User{ID: "usr_calls_member", Email: "calls-member@example.com", Name: "Calls member", Role: entity.RoleMember, PasswordHash: administrator.PasswordHash}
	if err := db.Create(&member).Error; err != nil {
		t.Fatal(err)
	}
	memberLogin := identityRequest(router, "POST", "/api/v1/auth/login", `{"email":"calls-member@example.com","password":"calls-password"}`, nil, "")
	expectStatus(t, memberLogin, 200)
	_, memberCookie := readIdentity(t, memberLogin)
	svc, err := service.New(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now().UTC().Truncate(time.Microsecond).Add(-time.Hour)
	inputTokens, outputTokens := int64(12), int64(34)
	fact := service.CallFact{RequestID: "req_calls_dedup", UserID: admin.User.ID, KeyID: "key_historical", ModelID: "mdl_historical", ModelName: "historical-model", ProviderModelID: "pmd_historical", ConnectionID: "con_historical", Protocol: "openai_chat", Status: "success", StartedAt: started, CompletedAt: started.Add(150 * time.Millisecond), InputTokens: &inputTokens, OutputTokens: &outputTokens, Attempts: []service.CallAttempt{{ID: "att_calls_dedup", ProviderModelID: "pmd_historical", ConnectionID: "con_historical", Status: "success", StartedAt: started, CompletedAt: started.Add(150 * time.Millisecond), HTTPStatus: 200}}}
	errorsCh := make(chan error, 4)
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() { errorsCh <- svc.RecordCall(context.Background(), fact) })
	}
	wg.Wait()
	close(errorsCh)
	for err := range errorsCh {
		if err != nil {
			t.Fatalf("concurrent replay failed: %v", err)
		}
	}
	var records, attempts int64
	if err := db.Model(&entity.CallRecord{}).Count(&records).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&entity.CallAttempt{}).Count(&attempts).Error; err != nil {
		t.Fatal(err)
	}
	if records != 1 || attempts != 1 {
		t.Fatalf("duplicate event created %d facts/%d attempts", records, attempts)
	}
	changed := fact
	changed.Status = "error"
	changed.InputTokens = nil
	changed.ErrorCode = "upstream-secret-must-not-leak"
	if err := svc.RecordCall(context.Background(), changed); err != nil {
		t.Fatal(err)
	}
	original, err := svc.GetCall(context.Background(), "", fact.RequestID)
	if err != nil || original.Record.Status != "success" || original.Record.InputTokens == nil || *original.Record.InputTokens != 12 {
		t.Fatal("replay overwrote accepted fact")
	}
	for index, status := range []string{"error", "canceled"} {
		next := fact
		next.RequestID = fmt.Sprintf("req_calls_%d", index)
		next.Status = status
		next.Stream = true
		next.InputTokens = nil
		next.OutputTokens = nil
		next.ErrorCode = "private upstream body"
		next.Attempts = []service.CallAttempt{{ID: fmt.Sprintf("att_calls_%d", index), ProviderModelID: "pmd_historical", ConnectionID: "con_historical", Status: status, StartedAt: started, CompletedAt: started.Add(time.Second), HTTPStatus: 502, ErrorCode: "private upstream body"}}
		if err := svc.RecordCall(context.Background(), next); err != nil {
			t.Fatal(err)
		}
	}
	memberFact := fact
	memberFact.RequestID = "req_calls_member"
	memberFact.UserID = member.ID
	memberFact.Attempts = nil
	memberFact.Status = "error"
	memberFact.ErrorCode = "no_route"
	memberFact.InputTokens = nil
	memberFact.OutputTokens = nil
	if err := svc.RecordCall(context.Background(), memberFact); err != nil {
		t.Fatal(err)
	}
	// Attempt ID collisions roll back the new fact rather than silently losing
	// its attempts; only a repeated canonical RequestID is idempotent.
	collision := fact
	collision.RequestID = "req_calls_collision"
	if err := svc.RecordCall(context.Background(), collision); err == nil {
		t.Fatal("attempt collision must fail")
	}
	if err := db.Model(&entity.CallRecord{}).Where("request_id = ?", collision.RequestID).Count(&records).Error; err != nil || records != 0 {
		t.Fatal("attempt failure left a partial fact")
	}
	// A fresh service/router reads the persisted facts and ownership scope.
	restartedService, err := service.New(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	router = fox.New()
	New(restartedService).RegisterRoutes(router)
	expectStatus(t, identityRequest(router, "GET", "/api/v1/calls", "", nil, ""), 401)
	expectStatus(t, identityRequest(router, "GET", "/api/v1/admin/calls", "", memberCookie, ""), 403)
	expectStatus(t, identityRequest(router, "GET", "/api/v1/calls/"+fact.RequestID, "", memberCookie, ""), 404)
	expectStatus(t, identityRequest(router, "GET", "/api/v1/calls/req_missing", "", memberCookie, ""), 404)
	memberList := decodeCatalogResponse[CallsResponse](t, identityRequest(router, "GET", "/api/v1/calls", "", memberCookie, ""), 200)
	if len(memberList.Items) != 1 || memberList.Items[0].RequestID != memberFact.RequestID || memberList.Items[0].InputTokens != nil {
		t.Fatal("member call list crossed ownership or changed unknown usage to zero")
	}
	memberDetail := identityRequest(router, "GET", "/api/v1/calls/"+memberFact.RequestID, "", memberCookie, "")
	expectStatus(t, memberDetail, 200)
	for _, forbidden := range []string{"provider_model_id", "connection_id", "attempts", "error_code", "user_id", "credential"} {
		if strings.Contains(memberDetail.Body.String(), forbidden) {
			t.Fatalf("member detail exposed %s", forbidden)
		}
	}
	all := decodeCatalogResponse[AdminCallsResponse](t, identityRequest(router, "GET", "/api/v1/admin/calls", "", adminCookie, ""), 200)
	if len(all.Items) != 4 {
		t.Fatalf("administrator sees %d records, want4", len(all.Items))
	}
	adminDetail := decodeCatalogResponse[AdminCallDetailResponse](t, identityRequest(router, "GET", "/api/v1/admin/calls/req_calls_0", "", adminCookie, ""), 200)
	if len(adminDetail.Attempts) != 1 || adminDetail.ErrorCode != "upstream_error" || adminDetail.Attempts[0].ErrorCode != "upstream_error" {
		t.Fatal("admin diagnostic classification not sanitized")
	}
	first := decodeCatalogResponse[CallsResponse](t, identityRequest(router, "GET", "/api/v1/calls?limit=2", "", adminCookie, ""), 200)
	if len(first.Items) != 2 || first.NextCursor == nil {
		t.Fatal("first cursor page invalid")
	}
	second := decodeCatalogResponse[CallsResponse](t, identityRequest(router, "GET", "/api/v1/calls?limit=2&cursor="+url.QueryEscape(*first.NextCursor), "", adminCookie, ""), 200)
	if len(second.Items) != 1 || second.NextCursor != nil {
		t.Fatal("second cursor page invalid")
	}
	seen := map[string]bool{}
	for _, item := range append(first.Items, second.Items...) {
		if seen[item.RequestID] {
			t.Fatal("cursor pagination duplicated a fact")
		}
		seen[item.RequestID] = true
	}
	for _, query := range []string{"limit=101", "limit=-1", "cursor=invalid", "status=made-up", "from=not-a-date", "user_id=" + member.ID} {
		expectStatus(t, identityRequest(router, "GET", "/api/v1/calls?"+query, "", adminCookie, ""), 400)
	}
	filtered := decodeCatalogResponse[CallsResponse](t, identityRequest(router, "GET", "/api/v1/calls?status=canceled&model_id=mdl_historical&key_id=key_historical", "", adminCookie, ""), 200)
	if len(filtered.Items) != 1 || filtered.Items[0].Status != "canceled" {
		t.Fatal("server-side filters not applied")
	}
	future := url.QueryEscape(started.Add(time.Minute).Format(time.RFC3339Nano))
	empty := decodeCatalogResponse[CallsResponse](t, identityRequest(router, "GET", "/api/v1/calls?from="+future, "", adminCookie, ""), 200)
	if len(empty.Items) != 0 {
		t.Fatal("time filter not applied")
	}
	byUser := decodeCatalogResponse[AdminCallsResponse](t, identityRequest(router, "GET", "/api/v1/admin/calls?user_id="+member.ID, "", adminCookie, ""), 200)
	if len(byUser.Items) != 1 || byUser.Items[0].UserID != member.ID {
		t.Fatal("admin user filter not applied")
	}
}
