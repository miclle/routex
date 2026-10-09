package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"gorm.io/gorm"
)

// Registered by the integration owner only after composing the exact V93 tail.
// No upstream, runtime publisher or recorder is started by this read-only case.
func testCredentialAttemptStatistics(t *testing.T, db *gorm.DB) {
	ctx := context.Background()
	svc, err := service.New(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	router := fox.New()
	New(svc).RegisterRoutes(router)
	setup := identityRequest(router, "POST", "/api/v1/setup", `{"email":"attempt-stats@example.invalid","password":"test-only-attempt-stats-password","name":"Stats admin"}`, nil, "")
	expectStatus(t, setup, 201)
	admin, adminCookie := readIdentity(t, setup)
	_, readerCookie, _ := createSystemStatusMember(t, svc, router, admin.User.ID, "stats-reader", []string{"providers.read"})
	_, writerCookie, _ := createSystemStatusMember(t, svc, router, admin.User.ID, "stats-writer", []string{"providers.write"})
	provider := entity.Provider{ID: "prv_attempt_stats", Name: "Attempt statistics"}
	connection := entity.ProviderConnection{ID: "con_attempt_stats", ProviderID: provider.ID, Name: "Disabled stored transport", Protocol: entity.ProtocolOpenAIChat, BaseURL: "https://example.invalid/v1", EgressMode: "direct"}
	foreignProvider := entity.Provider{ID: "prv_attempt_foreign", Name: "Other supplier"}
	for _, row := range []any{&provider, &connection, &foreignProvider} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Model(&provider).UpdateColumn("enabled", false).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&connection).UpdateColumn("enabled", false).Error; err != nil {
		t.Fatal(err)
	}
	ids := []string{"crd_stats_a", "crd_stats_b", "crd_stats_c", "crd_stats_d", "crd_stats_empty"}
	allIDs := append([]string(nil), ids...)
	allIDs = append(allIDs, "crd_stats_future")
	for i := len(allIDs); i < 20; i++ {
		allIDs = append(allIDs, fmt.Sprintf("crd_stats_empty%02d", i))
	}
	for _, id := range allIDs {
		row := entity.ProviderCredential{ID: id, ConnectionID: connection.ID, Name: "Pending credential", Ciphertext: "test-only-private-ciphertext", Enabled: false, VerificationStatus: "pending"}
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now().UTC().Truncate(time.Microsecond).Add(-time.Minute)
	record := func(id, credentialID, snapshot, status, code string, completedAt time.Time) {
		t.Helper()
		fact := service.CallFact{RequestID: "req_" + id, SnapshotID: "cfg_final_unrelated", UserID: admin.User.ID, KeyID: "key_historical", ModelID: "mdl_historical", ConnectionID: "con_final_unrelated", Protocol: entity.ProtocolOpenAIChat, Status: "success", StartedAt: completedAt.Add(-time.Second), CompletedAt: completedAt,
			Attempts: []service.CallAttempt{{ID: id, CredentialID: credentialID, SnapshotID: snapshot, ConnectionID: connection.ID, ConnectionName: connection.Name, ProviderID: provider.ID, ProviderName: provider.Name, ProviderModelID: "pmd_stats_historical", UpstreamModelName: "stats-historical-upstream", Status: status, HTTPStatus: 200, ErrorCode: code, NativeCompletionEvidence: "completed", StartedAt: completedAt.Add(-time.Second), CompletedAt: completedAt}}}
		if err := svc.RecordCall(ctx, fact); err != nil {
			t.Fatalf("record attempt %s with status %s: %v", id, status, err)
		}
	}
	record("att_stats_Z", ids[0], "cfg_old", "error", "process_interrupted", now)
	record("att_stats_a", ids[0], "cfg_new", "success", "", now)
	record("att_stats_alias", strings.ToUpper(ids[0]), "cfg_alias", "error", "invalid_api_key", now.Add(time.Second))
	record("att_stats_unknown", "", "cfg_unknown", "error", "invalid_api_key", now.Add(time.Second))
	record("att_stats_b3", ids[1], "cfg_b3", "error", "upstream_timeout", now)
	record("att_stats_b2", ids[1], "cfg_b2", "canceled", "canceled", now.Add(-time.Second))
	record("att_stats_b1", ids[1], "", "error", "invalid_api_key", now.Add(-2*time.Second))
	for i := range 101 {
		record(fmt.Sprintf("att_stats_c%03d", i), ids[2], fmt.Sprintf("cfg_%03d", i), "error", "rate_limit_exceeded", now.Add(-time.Duration(i)*time.Second))
	}
	record("att_stats_d", ids[3], "cfg_d", "error", "upstream_error", now)
	if err := db.Model(&entity.CallAttempt{}).Where("id = ?", "att_stats_d").UpdateColumn("error_code", "test-only-private-upstream-body").Error; err != nil {
		t.Fatal(err)
	}
	record("att_stats_future", "crd_stats_future", "cfg_future", "error", "invalid_api_key", time.Now().UTC().Add(time.Hour))
	record("att_stats_prior_success", "crd_stats_future", "cfg_prior", "error", "process_interrupted", now)
	type domainState struct {
		Providers   []entity.Provider
		Connections []entity.ProviderConnection
		Credentials []entity.ProviderCredential
		Attempts    []entity.CallAttempt
		Audits      []entity.AuditEvent
	}
	read := func() domainState {
		t.Helper()
		var state domainState
		for _, value := range []any{&state.Providers, &state.Connections, &state.Credentials, &state.Attempts, &state.Audits} {
			if err := db.Session(&gorm.Session{QueryFields: true}).Order("id").Find(value).Error; err != nil {
				t.Fatal("read invariant state")
			}
		}
		return state
	}
	before := read()
	get := func(providerID string, selected []string, cookie *http.Cookie) *httptest.ResponseRecorder {
		t.Helper()
		q := url.Values{}
		for _, id := range selected {
			q.Add("credential_id", id)
		}
		return identityRequest(router, "GET", "/api/v1/admin/providers/"+providerID+"/credential-attempt-statistics?"+q.Encode(), "", cookie, "")
	}
	response := get(provider.ID, []string{ids[2], ids[0], ids[4], ids[1], ids[3]}, readerCookie)
	expectStatus(t, response, 200)
	if response.Header().Get("Cache-Control") != "private, no-store" || response.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("private statistics headers")
	}
	for _, private := range []string{"test-only-private", "cfg_", "att_stats", "key_historical", "invalid_api_key"} {
		if strings.Contains(response.Body.String(), private) {
			t.Fatal("private or unattributed attempt material leaked")
		}
	}
	var result service.CredentialAttemptStatisticsBatch
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.ProviderID != provider.ID || result.AttemptLimit != 100 || !result.RecordedOnly || result.ObservedAt.IsZero() || len(result.Items) != 5 {
		t.Fatal("batch scope")
	}
	c, a, empty, b, d := result.Items[0], result.Items[1], result.Items[2], result.Items[3], result.Items[4]
	if c.CredentialID != ids[2] || c.InspectedAttempts != 100 || !c.HasMore || c.FailureStreak.State != "lower_bound" || c.FailureStreak.Count != nil || c.FailureStreak.LowerBound != 100 {
		t.Fatal("cross-snapshot sentinel")
	}
	if a.CredentialID != ids[0] || a.InspectedAttempts != 2 || a.HasMore || a.FailureStreak.State != "exact" || a.FailureStreak.Count == nil || *a.FailureStreak.Count != 0 || a.RecentError.Code == nil || *a.RecentError.Code != "process_interrupted" {
		t.Fatal("byte tie, exact attribution and independent recent error")
	}
	if empty.CredentialID != ids[4] || empty.InspectedAttempts != 0 || empty.HasMore || empty.FailureStreak.State != "no_records" || empty.FailureStreak.Count != nil || empty.RecentError.State != "no_records" {
		t.Fatal("no records is not zero")
	}
	if b.CredentialID != ids[1] || b.FailureStreak.State != "unknown" || b.FailureStreak.Count != nil || b.FailureStreak.LowerBound != 1 || b.RecentError.Code == nil || *b.RecentError.Code != "upstream_timeout" {
		t.Fatal("cancellation boundary")
	}
	if d.CredentialID != ids[3] || d.RecentError.State != "recorded" || d.RecentError.Code != nil || d.RecentError.CompletedAt == nil {
		t.Fatal("unsafe retained code not echoed or replaced")
	}
	futureResponse := get(provider.ID, []string{"crd_stats_future"}, readerCookie)
	expectStatus(t, futureResponse, 200)
	var future service.CredentialAttemptStatisticsBatch
	if err := json.Unmarshal(futureResponse.Body.Bytes(), &future); err != nil {
		t.Fatal(err)
	}
	if len(future.Items) != 1 || future.Items[0].InspectedAttempts != 2 || future.Items[0].FailureStreak.State != "unknown" || future.Items[0].FailureStreak.Count != nil || future.Items[0].RecentError.State != "unknown" || future.Items[0].RecentError.Code != nil {
		t.Fatal("future attributable row was dropped or bridged")
	}
	twenty := get(provider.ID, allIDs, readerCookie)
	expectStatus(t, twenty, 200)
	var twentyResult service.CredentialAttemptStatisticsBatch
	if err := json.Unmarshal(twenty.Body.Bytes(), &twentyResult); err != nil || len(twentyResult.Items) != 20 {
		t.Fatal("complete bounded batch", err)
	}
	for i, item := range twentyResult.Items {
		if item.CredentialID != allIDs[i] || item.ConnectionID != connection.ID {
			t.Fatal("batch request order or parent changed")
		}
	}
	expectStatus(t, get(provider.ID, append(append([]string(nil), allIDs...), "crd_stats_21"), adminCookie), 400)
	for _, tc := range []struct {
		provider string
		selected []string
		cookie   *http.Cookie
		status   int
	}{
		{provider.ID, ids[:1], writerCookie, 403}, {provider.ID, ids[:1], nil, 401},
		{foreignProvider.ID, ids[:1], adminCookie, 404}, {provider.ID, []string{"crd_stats_missing"}, adminCookie, 404},
		{provider.ID, []string{strings.ToUpper(ids[0])}, adminCookie, 400}, {provider.ID, []string{ids[0], ids[0]}, adminCookie, 400},
	} {
		expectStatus(t, get(tc.provider, tc.selected, tc.cookie), tc.status)
	}
	if err := db.Callback().Query().Before("gorm:query").Register("attempt_statistics_controlled_failure", func(tx *gorm.DB) {
		if tx.Statement.Table == "call_attempts" {
			_ = tx.AddError(context.DeadlineExceeded)
		}
	}); err != nil {
		t.Fatal(err)
	}
	failed := get(provider.ID, ids[:1], adminCookie)
	if err := db.Callback().Query().Remove("attempt_statistics_controlled_failure"); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, failed, 503)
	if !reflect.DeepEqual(before, read()) {
		t.Fatal("statistics changed immutable history, catalogue, enablement, verification or audit")
	}
}
