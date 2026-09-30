package handler

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/fox-gonic/fox"
	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
)

func testProviderQualityHTTPLifecycle(t *testing.T, db *gorm.DB) {
	ctx := context.Background()
	svc, err := service.New(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	router := fox.New()
	New(svc).RegisterRoutes(router)
	setup := identityRequest(router, http.MethodPost, "/api/v1/setup", `{"email":"quality-admin@example.invalid","password":"test-only-quality-password","name":"Quality Admin"}`, nil, "")
	expectStatus(t, setup, http.StatusCreated)
	admin, adminCookie := readIdentity(t, setup)
	adminRequest := systemStatusRequest(t, router, adminCookie, admin.CSRFToken)
	_, providerCookie, providerCSRF := createSystemStatusMember(t, svc, router, admin.User.ID, "quality-provider-reader", []string{"providers.read"})
	_, readerCookie, readerCSRF := createSystemStatusMember(t, svc, router, admin.User.ID, "quality-system-reader", []string{"system.read"})
	_, writerCookie, writerCSRF := createSystemStatusMember(t, svc, router, admin.User.ID, "quality-system-writer", []string{"system.write"})
	providerRequest := systemStatusRequest(t, router, providerCookie, providerCSRF)
	readerRequest := systemStatusRequest(t, router, readerCookie, readerCSRF)
	writerRequest := systemStatusRequest(t, router, writerCookie, writerCSRF)

	now := time.Now().UTC().Truncate(time.Microsecond)
	provider := entity.Provider{ID: "prv_quality_http", Name: "Quality provider", CreatedAt: now}
	if err := db.Create(&provider).Error; err != nil {
		t.Fatal(err)
	}
	attempts := []service.CallAttempt{
		qualityAttempt("att_quality_1", provider, "success", "success", "completed", 200, now.Add(-8*time.Minute), 100),
		qualityAttempt("att_quality_2", provider, "success", "success", "completed", 200, now.Add(-7*time.Minute), 200),
		qualityAttempt("att_quality_3", provider, "success", "success", "completed", 200, now.Add(-6*time.Minute), 300),
		qualityAttempt("att_quality_4", provider, "error", "rate_limited", "rejected_without_work", 429, now.Add(-5*time.Minute), 400),
		qualityAttempt("att_quality_5", provider, "error", "permanent_failure", "completed", 503, now.Add(-4*time.Minute), 500),
		qualityAttempt("att_quality_6", provider, "error", "credential_rejected", "rejected_without_work", 401, now.Add(-3*time.Minute), 50),
		qualityAttempt("att_quality_7", provider, "error", "connection_failure", "not_sent", 0, now.Add(-2*time.Minute), 10),
		qualityAttempt("att_quality_8", provider, "canceled", "permanent_failure", "unknown", 0, now.Add(-time.Minute), 20),
	}
	fact := service.CallFact{
		RequestID: "req_quality_http", UserID: admin.User.ID, KeyID: "key_quality_http",
		ModelID: "mdl_quality_http", ModelName: "Quality model", ProviderID: provider.ID, ProviderName: provider.Name,
		ProviderModelID: "pmd_quality_http", ConnectionID: "con_quality_http", ConnectionName: "Quality connection",
		UpstreamModelName: "quality-upstream", Protocol: entity.ProtocolOpenAIChat, Status: "error",
		StartedAt: attempts[0].StartedAt, CompletedAt: now, Attempts: attempts,
	}
	if err := svc.RecordCall(ctx, fact); err != nil {
		t.Fatal(err)
	}
	legacy := fact
	legacy.RequestID = "req_quality_legacy"
	legacy.Attempts = []service.CallAttempt{{ID: "att_quality_legacy", ProviderModelID: "pmd_quality_http", ConnectionID: "con_quality_http", AttemptNumber: 1, Status: "error", FailureClass: "permanent_failure", WorkEvidence: "unknown", StartedAt: now.Add(-time.Minute), CompletedAt: now}}
	if err := svc.RecordCall(ctx, legacy); err != nil {
		t.Fatal(err)
	}

	qualityPath := "/api/v1/admin/providers/" + provider.ID + "/quality"
	expectStatus(t, readerRequest(http.MethodGet, qualityPath, nil), http.StatusForbidden)
	quality := decodeCatalogResponse[service.ProviderQuality](t, providerRequest(http.MethodGet, qualityPath, nil), http.StatusOK)
	if quality.Requests != 8 || quality.EligibleAttempts != 5 || quality.ExcludedAttempts != 2 || quality.CredentialRejectedAttempts != 1 || quality.UnknownAttributionAttempts != 1 || quality.Successes != 3 || quality.SuccessRateBPS == nil || *quality.SuccessRateBPS != 6000 || quality.P95DurationMS == nil || *quality.P95DurationMS != 500 || quality.HTTP429 != 1 || quality.HTTP5XX != 1 || quality.Status != "unconfigured" || quality.MayLag {
		t.Fatalf("quality summary = %+v", quality)
	}

	policyPath := "/api/v1/admin/providers/" + provider.ID + "/quality-policy"
	expectStatus(t, providerRequest(http.MethodGet, policyPath, nil), http.StatusForbidden)
	defaults := decodeCatalogResponse[service.ProviderQualityPolicy](t, readerRequest(http.MethodGet, policyPath, nil), http.StatusOK)
	if defaults.ETag != "0" || defaults.Enabled || defaults.WindowMinutes != 60 || defaults.MinimumAttempts != 20 {
		t.Fatalf("default policy = %+v", defaults)
	}
	body := map[string]any{"enabled": true, "window_minutes": 15, "minimum_attempts": 5, "min_success_rate_bps": 9000, "max_p95_duration_ms": 450, "etag": defaults.ETag, "reason": "Enable tested quality thresholds"}
	expectStatus(t, identityRequest(router, http.MethodPut, policyPath, `{"enabled":true,"window_minutes":60,"minimum_attempts":5,"min_success_rate_bps":9000,"max_p95_duration_ms":450,"etag":"0","reason":"Enable tested quality thresholds"}`, writerCookie, ""), http.StatusForbidden)
	unknown := map[string]any{"enabled": true, "window_minutes": 60, "minimum_attempts": 5, "min_success_rate_bps": 9000, "etag": "0", "reason": "Strict body", "recipient": "usr_other"}
	expectStatus(t, writerRequest(http.MethodPut, policyPath, unknown), http.StatusBadRequest)
	saved := decodeCatalogResponse[service.ProviderQualityPolicy](t, writerRequest(http.MethodPut, policyPath, body), http.StatusOK)
	if saved.ETag == "0" || !saved.Enabled || saved.UpdateReason != body["reason"] {
		t.Fatalf("saved policy = %+v", saved)
	}
	expectStatus(t, writerRequest(http.MethodPut, policyPath, body), http.StatusConflict)
	configured := decodeCatalogResponse[service.ProviderQuality](t, providerRequest(http.MethodGet, qualityPath, nil), http.StatusOK)
	if configured.Status != "degraded" || configured.WindowEnd.Sub(configured.WindowStart) != 15*time.Minute {
		t.Fatalf("configured quality state = %+v", configured)
	}
	if err := svc.FlushProviderQualityEvaluation(ctx); err != nil {
		t.Fatal(err)
	}
	var insufficient entity.ProviderQualityWindow
	if err := db.First(&insufficient, "provider_id = ?", provider.ID).Error; err != nil || insufficient.State != "insufficient_data" {
		t.Fatalf("insufficient window = %+v, error = %v", insufficient, err)
	}
	var qualityOccurrences int64
	if err := db.Model(&entity.OperationalAlertOccurrence{}).Where("source_type = ?", "provider_quality_window").Count(&qualityOccurrences).Error; err != nil || qualityOccurrences != 0 {
		t.Fatalf("insufficient window emitted %d alerts: %v", qualityOccurrences, err)
	}
	if err := db.Where("provider_id = ?", provider.ID).Delete(&entity.ProviderQualityWindow{}).Error; err != nil {
		t.Fatal(err)
	}
	window := func(id, state, detail string, end time.Time) entity.ProviderQualityWindow {
		return entity.ProviderQualityWindow{
			ID: id, ProviderID: provider.ID, ProviderName: provider.Name, WindowStart: end.Add(-time.Hour), WindowEnd: end,
			PolicyETag: saved.ETag, EligibleAttempts: 5, Successes: 2, KnownDurationAttempts: 5,
			State: state, DetailCode: detail, CreatedAt: end,
		}
	}
	degradedOne := window("pqw_quality_bad_1", "degraded", "success_rate_below_threshold", now.Add(-3*time.Hour))
	insufficientBetween := window("pqw_quality_mid_insuf", "insufficient_data", "", now.Add(-150*time.Minute))
	degradedTwo := window("pqw_quality_bad_2", "degraded", "success_rate_below_threshold", now.Add(-2*time.Hour))
	if err := db.Create(&[]entity.ProviderQualityWindow{degradedOne, insufficientBetween, degradedTwo}).Error; err != nil {
		t.Fatal(err)
	}
	expectStatus(t, readerRequest(http.MethodGet, "/api/v1/admin/alerts", nil), http.StatusOK)
	expectStatus(t, readerRequest(http.MethodGet, "/api/v1/admin/alerts", nil), http.StatusOK)
	if err := db.Model(&entity.OperationalAlertOccurrence{}).Where("source_type = ?", "provider_quality_window").Count(&qualityOccurrences).Error; err != nil || qualityOccurrences != 1 {
		t.Fatalf("continued degradation emitted %d occurrences: %v", qualityOccurrences, err)
	}
	var qualityAlert entity.OperationalAlert
	if err := db.First(&qualityAlert, "group_key = ?", "provider_quality:"+provider.ID).Error; err != nil || qualityAlert.SubjectType != "provider" || qualityAlert.SubjectID != provider.ID {
		t.Fatalf("quality alert subject = %+v, error = %v", qualityAlert, err)
	}
	healthy := window("pqw_quality_good", "healthy", "", now.Add(-time.Hour))
	if err := db.Create(&healthy).Error; err != nil {
		t.Fatal(err)
	}
	expectStatus(t, readerRequest(http.MethodGet, "/api/v1/admin/alerts", nil), http.StatusOK)
	if err := db.First(&qualityAlert, "id = ?", qualityAlert.ID).Error; err != nil || qualityAlert.State != "resolved" {
		t.Fatalf("quality recovery did not resolve alert: %+v, error = %v", qualityAlert, err)
	}
	var resolvedNotification entity.Notification
	if err := db.First(&resolvedNotification, "alert_id = ? AND recipient_id = ?", qualityAlert.ID, admin.User.ID).Error; err != nil || !resolvedNotification.Read {
		t.Fatalf("quality recovery did not resolve inbox projection: %+v, error = %v", resolvedNotification, err)
	}
	rebreach := window("pqw_quality_bad_3", "degraded", "p95_duration_above_threshold", now)
	if err := db.Create(&rebreach).Error; err != nil {
		t.Fatal(err)
	}
	expectStatus(t, readerRequest(http.MethodGet, "/api/v1/admin/alerts", nil), http.StatusOK)
	if err := db.Model(&entity.OperationalAlertOccurrence{}).Where("source_type = ?", "provider_quality_window").Count(&qualityOccurrences).Error; err != nil || qualityOccurrences != 2 {
		t.Fatalf("re-breach occurrence count = %d, error = %v", qualityOccurrences, err)
	}
	if err := db.First(&qualityAlert, "id = ?", qualityAlert.ID).Error; err != nil || qualityAlert.State != "open" || qualityAlert.OccurrenceCount != 2 || qualityAlert.DetailCode != "p95_duration_above_threshold" {
		t.Fatalf("quality re-breach did not reopen exactly once: %+v, error = %v", qualityAlert, err)
	}
	if err := db.First(&resolvedNotification, "id = ?", resolvedNotification.ID).Error; err != nil || resolvedNotification.Read || resolvedNotification.OccurrenceCount != 2 || resolvedNotification.DetailCode != "p95_duration_above_threshold" {
		t.Fatalf("quality re-breach did not reopen inbox projection: %+v, error = %v", resolvedNotification, err)
	}
	revisedBody := map[string]any{"enabled": true, "window_minutes": 15, "minimum_attempts": 5, "min_success_rate_bps": 8500, "max_p95_duration_ms": 450, "etag": saved.ETag, "reason": "Revise quality incident thresholds"}
	revised := decodeCatalogResponse[service.ProviderQualityPolicy](t, writerRequest(http.MethodPut, policyPath, revisedBody), http.StatusOK)
	if !revised.Enabled || revised.ETag == saved.ETag {
		t.Fatalf("revised policy = %+v", revised)
	}
	if err := db.First(&qualityAlert, "id = ?", qualityAlert.ID).Error; err != nil || qualityAlert.State != "resolved" {
		t.Fatalf("policy revision did not resolve the previous generation: %+v, error = %v", qualityAlert, err)
	}
	var qualityState entity.ProviderQualityState
	if err := db.First(&qualityState, "provider_id = ?", provider.ID).Error; err != nil || qualityState.LastState != "insufficient_data" || qualityState.LastAlertID != "" {
		t.Fatalf("policy revision did not reset transition state: %+v, error = %v", qualityState, err)
	}
	expectStatus(t, readerRequest(http.MethodGet, "/api/v1/admin/alerts", nil), http.StatusOK)
	if err := db.First(&qualityAlert, "id = ?", qualityAlert.ID).Error; err != nil || qualityAlert.State != "resolved" {
		t.Fatalf("old-policy window reopened after policy revision: %+v, error = %v", qualityAlert, err)
	}
	revisedBreach := window("pqw_quality_bad_revised", "degraded", "success_rate_below_threshold", now.Add(30*time.Minute))
	revisedBreach.PolicyETag = revised.ETag
	if err := db.Create(&revisedBreach).Error; err != nil {
		t.Fatal(err)
	}
	expectStatus(t, readerRequest(http.MethodGet, "/api/v1/admin/alerts", nil), http.StatusOK)
	if err := db.First(&qualityAlert, "id = ?", qualityAlert.ID).Error; err != nil || qualityAlert.State != "open" || qualityAlert.OccurrenceCount != 3 {
		t.Fatalf("breach under revised policy did not reopen once: %+v, error = %v", qualityAlert, err)
	}
	disabledBody := map[string]any{"enabled": false, "window_minutes": 15, "minimum_attempts": 5, "min_success_rate_bps": 8500, "max_p95_duration_ms": 450, "etag": revised.ETag, "reason": "Disable quality incident evaluation"}
	disabled := decodeCatalogResponse[service.ProviderQualityPolicy](t, writerRequest(http.MethodPut, policyPath, disabledBody), http.StatusOK)
	if disabled.Enabled || disabled.ETag == revised.ETag {
		t.Fatalf("disabled policy = %+v", disabled)
	}
	if err := db.First(&qualityAlert, "id = ?", qualityAlert.ID).Error; err != nil || qualityAlert.State != "resolved" {
		t.Fatalf("disabling policy did not resolve the current incident: %+v, error = %v", qualityAlert, err)
	}
	if err := db.First(&qualityState, "provider_id = ?", provider.ID).Error; err != nil || qualityState.LastState != "insufficient_data" || qualityState.LastAlertID != "" {
		t.Fatalf("disabling policy did not reset transition state: %+v, error = %v", qualityState, err)
	}
	reenabledBody := map[string]any{"enabled": true, "window_minutes": 15, "minimum_attempts": 5, "min_success_rate_bps": 8500, "max_p95_duration_ms": 450, "etag": disabled.ETag, "reason": "Re-enable quality incident evaluation"}
	reenabled := decodeCatalogResponse[service.ProviderQualityPolicy](t, writerRequest(http.MethodPut, policyPath, reenabledBody), http.StatusOK)
	expectStatus(t, readerRequest(http.MethodGet, "/api/v1/admin/alerts", nil), http.StatusOK)
	if err := db.First(&qualityAlert, "id = ?", qualityAlert.ID).Error; err != nil || qualityAlert.State != "resolved" {
		t.Fatalf("old-policy window reopened a stale incident: %+v, error = %v", qualityAlert, err)
	}
	futureBreach := window("pqw_quality_bad_reenabled", "degraded", "success_rate_below_threshold", now.Add(time.Hour))
	futureBreach.PolicyETag = reenabled.ETag
	if err := db.Create(&futureBreach).Error; err != nil {
		t.Fatal(err)
	}
	expectStatus(t, readerRequest(http.MethodGet, "/api/v1/admin/alerts", nil), http.StatusOK)
	if err := db.First(&qualityAlert, "id = ?", qualityAlert.ID).Error; err != nil || qualityAlert.State != "open" || qualityAlert.OccurrenceCount != 4 {
		t.Fatalf("future breach after re-enable did not reopen once: %+v, error = %v", qualityAlert, err)
	}

	routeCall := service.CallFact{
		RequestID: "req_quality_no_route", UserID: admin.User.ID, KeyID: "key_quality_http",
		ModelID: "mdl_quality_http", ModelName: "Quality model", Protocol: entity.ProtocolOpenAIChat,
		RouteStopReason: "no_candidates", Status: "error", StartedAt: now, CompletedAt: now,
	}
	if err := svc.RecordCall(ctx, routeCall); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, readerRequest(http.MethodGet, "/api/v1/admin/alerts", nil), http.StatusOK)
	expectStatus(t, readerRequest(http.MethodGet, "/api/v1/admin/alerts", nil), http.StatusOK)
	var routeOccurrences int64
	if err := db.Model(&entity.OperationalAlertOccurrence{}).Where("source_type = ? AND source_id = ?", "gateway_call", routeCall.RequestID).Count(&routeOccurrences).Error; err != nil || routeOccurrences != 1 {
		t.Fatalf("route exhaustion dedupe = %d, error = %v", routeOccurrences, err)
	}
	var routeAlert entity.OperationalAlert
	if err := db.First(&routeAlert, "group_key = ?", "route_unavailable:"+routeCall.ModelID+":"+routeCall.Protocol+":"+routeCall.RouteStopReason).Error; err != nil || routeAlert.SubjectType != "model" || routeAlert.SubjectID != routeCall.ModelID || routeAlert.DetailCode != "no_candidates" {
		t.Fatalf("route alert = %+v, error = %v", routeAlert, err)
	}
	var audits int64
	if err := db.Model(&entity.AuditEvent{}).Where("action = ? AND resource_id = ?", "provider_quality.policy.update", provider.ID).Count(&audits).Error; err != nil || audits != 4 {
		t.Fatalf("quality policy audit count = %d, error = %v", audits, err)
	}
	expectStatus(t, adminRequest(http.MethodGet, qualityPath+"?unexpected=true", nil), http.StatusBadRequest)
}

func qualityAttempt(id string, provider entity.Provider, status, failureClass, workEvidence string, httpStatus int, started time.Time, durationMS int64) service.CallAttempt {
	return service.CallAttempt{
		ID: id, ProviderID: provider.ID, ProviderName: provider.Name, ProviderModelID: "pmd_quality_http",
		ConnectionID: "con_quality_http", ConnectionName: "Quality connection", UpstreamModelName: "quality-upstream",
		Status: status, FailureClass: failureClass, WorkEvidence: workEvidence,
		StartedAt: started, CompletedAt: started.Add(time.Duration(durationMS) * time.Millisecond), HTTPStatus: httpStatus,
	}
}
