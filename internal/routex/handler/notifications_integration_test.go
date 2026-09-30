package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/fox-gonic/fox"
	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
)

func testNotificationHTTPLifecycle(t *testing.T, db *gorm.DB) {
	ctx := context.Background()
	svc, err := service.New(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	router := fox.New()
	New(svc).RegisterRoutes(router)
	setup := identityRequest(router, http.MethodPost, "/api/v1/setup", `{"email":"notification-admin@example.invalid","password":"test-only-notification-password","name":"Notification Admin"}`, nil, "")
	expectStatus(t, setup, http.StatusCreated)
	admin, adminCookie := readIdentity(t, setup)
	adminRequest := systemStatusRequest(t, router, adminCookie, admin.CSRFToken)
	now := time.Now().UTC()
	if err := db.Model(&entity.SMTPSetting{}).Where("id = ?", 1).Updates(map[string]any{
		"enabled": true, "host": "smtp.example.invalid", "port": 587, "security": "STARTTLS",
		"sender_name": "RouteX", "sender_email": "notifications@example.invalid", "e_tag": "rev_notification_smtp", "updated_at": now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&entity.NotificationSetting{UserID: admin.User.ID, ExternalEmail: admin.User.Email, EmailHigh: true, ETag: "rev_notification_initial", UpdatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	completedAt := now.Add(-time.Minute)
	source := entity.SystemJob{
		ID: "job_notification_replay", Code: service.SystemJobRuntimePublication, Status: "failed",
		ItemsTotal: 1, DetailCode: "publication_failed", StartedAt: completedAt, UpdatedAt: completedAt, CompletedAt: &completedAt,
	}
	if err := db.Create(&source).Error; err != nil {
		t.Fatal(err)
	}
	startReplay := make(chan struct{})
	responses := make(chan int, 2)
	var replay sync.WaitGroup
	for range 2 {
		replay.Go(func() {
			<-startReplay
			responses <- adminRequest(http.MethodGet, "/api/v1/admin/system/jobs", nil).Code
		})
	}
	close(startReplay)
	replay.Wait()
	close(responses)
	for status := range responses {
		if status != http.StatusOK {
			t.Fatalf("concurrent source reconciliation returned %d", status)
		}
	}
	for _, item := range []struct {
		model any
		want  int64
	}{{&entity.OperationalAlert{}, 1}, {&entity.OperationalAlertOccurrence{}, 1}, {&entity.Notification{}, 1}, {&entity.NotificationDeliveryIntent{}, 1}} {
		var count int64
		if err := db.Model(item.model).Count(&count).Error; err != nil || count != item.want {
			t.Fatalf("concurrent exact-source replay produced %d rows for %T, want %d: %v", count, item.model, item.want, err)
		}
	}
	for _, model := range []any{&entity.NotificationDeliveryIntent{}, &entity.Notification{}, &entity.OperationalAlertOccurrence{}, &entity.OperationalAlert{}, &entity.NotificationSetting{}, &entity.SystemJob{}} {
		if err := db.Where("1 = 1").Delete(model).Error; err != nil {
			t.Fatal(err)
		}
	}

	// A late reconciliation of an older durable source must update the count and
	// earliest occurrence without replacing the current inbox event or reopening
	// a notification that the recipient already reviewed.
	credentialID := "crd_notification_order"
	newerAudit := entity.AuditEvent{
		ID: "aud_notification_newer", ActorID: admin.User.ID, Action: "credential.verify.failed",
		ResourceType: "credential", ResourceID: credentialID, CreatedAt: now.Add(-10 * time.Minute),
	}
	if err := db.Create(&newerAudit).Error; err != nil {
		t.Fatal(err)
	}
	expectStatus(t, adminRequest(http.MethodGet, "/api/v1/admin/alerts", nil), http.StatusOK)
	var orderedAlert entity.OperationalAlert
	if err := db.First(&orderedAlert, "group_key = ?", "credential_verification:"+credentialID).Error; err != nil {
		t.Fatal(err)
	}
	var orderedNotification entity.Notification
	if err := db.First(&orderedNotification, "recipient_id = ? AND alert_id = ?", admin.User.ID, orderedAlert.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&orderedNotification).Updates(map[string]any{"read": true, "read_at": now}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&orderedAlert).Updates(map[string]any{"state": "resolved"}).Error; err != nil {
		t.Fatal(err)
	}
	olderAudit := entity.AuditEvent{
		ID: "aud_notification_older", ActorID: admin.User.ID, Action: "credential.verify.failed",
		ResourceType: "credential", ResourceID: credentialID, CreatedAt: now.Add(-20 * time.Minute),
	}
	if err := db.Create(&olderAudit).Error; err != nil {
		t.Fatal(err)
	}
	expectStatus(t, adminRequest(http.MethodGet, "/api/v1/admin/alerts", nil), http.StatusOK)
	if err := db.First(&orderedAlert, "id = ?", orderedAlert.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&orderedNotification, "id = ?", orderedNotification.ID).Error; err != nil {
		t.Fatal(err)
	}
	var reconciledLatest entity.OperationalAlertOccurrence
	if err := db.First(&reconciledLatest, "id = ?", orderedNotification.LatestOccurrenceID).Error; err != nil {
		t.Fatal(err)
	}
	if orderedAlert.OccurrenceCount != 2 || orderedNotification.OccurrenceCount != 2 || orderedAlert.State != "resolved" || !orderedNotification.Read || reconciledLatest.SourceID != newerAudit.ID || orderedAlert.FirstSeenAt.Sub(olderAudit.CreatedAt).Abs() > time.Millisecond || orderedAlert.LastSeenAt.Sub(newerAudit.CreatedAt).Abs() > time.Millisecond {
		t.Fatalf("older reconciliation replaced newer inbox state: alert=%+v notification=%+v latest=%+v", orderedAlert, orderedNotification, reconciledLatest)
	}
	tieWinner := entity.OperationalAlertOccurrence{
		ID: "occ_zzzz_notification_order", AlertID: orderedAlert.ID, SourceType: "credential_verification",
		SourceID: "aud_notification_tie_winner", DetailCode: "verification_failed", OccurredAt: newerAudit.CreatedAt,
	}
	if err := db.Create(&tieWinner).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&orderedAlert).Updates(map[string]any{"occurrence_count": 3, "state": "resolved"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&orderedNotification).Updates(map[string]any{"latest_occurrence_id": tieWinner.ID, "occurrence_count": 3, "read": true, "read_at": now}).Error; err != nil {
		t.Fatal(err)
	}
	equalAudit := entity.AuditEvent{
		ID: "aud_notification_equal", ActorID: admin.User.ID, Action: "credential.verify.failed",
		ResourceType: "credential", ResourceID: credentialID, CreatedAt: newerAudit.CreatedAt,
	}
	if err := db.Create(&equalAudit).Error; err != nil {
		t.Fatal(err)
	}
	expectStatus(t, adminRequest(http.MethodGet, "/api/v1/admin/alerts", nil), http.StatusOK)
	if err := db.First(&orderedAlert, "id = ?", orderedAlert.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&orderedNotification, "id = ?", orderedNotification.ID).Error; err != nil {
		t.Fatal(err)
	}
	if orderedAlert.OccurrenceCount != 4 || orderedNotification.OccurrenceCount != 4 || orderedAlert.State != "resolved" || !orderedNotification.Read || orderedNotification.LatestOccurrenceID != tieWinner.ID {
		t.Fatalf("equal-time reconciliation ignored the canonical occurrence order: alert=%+v notification=%+v", orderedAlert, orderedNotification)
	}
	for _, model := range []any{&entity.NotificationDeliveryIntent{}, &entity.Notification{}, &entity.OperationalAlertOccurrence{}, &entity.OperationalAlert{}} {
		if err := db.Where("1 = 1").Delete(model).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Delete(&[]entity.AuditEvent{newerAudit, olderAudit, equalAudit}).Error; err != nil {
		t.Fatal(err)
	}

	ignoredJobs := make([]entity.SystemJob, 0, 101)
	ignoredAt := now.Add(-2 * time.Hour)
	for index := range 101 {
		completed := ignoredAt.Add(time.Duration(index) * time.Second)
		ignoredJobs = append(ignoredJobs, entity.SystemJob{
			ID: fmt.Sprintf("job_ignored_%03d", index), Code: service.SystemJobRuntimePublication, Status: "failed",
			ItemsTotal: 1, DetailCode: "canceled", StartedAt: completed, UpdatedAt: completed, CompletedAt: &completed,
		})
	}
	if err := db.Create(&ignoredJobs).Error; err != nil {
		t.Fatal(err)
	}
	actionableAt := now.Add(-time.Minute)
	actionableJob := entity.SystemJob{
		ID: "job_after_ignored", Code: service.SystemJobRuntimePublication, Status: "failed",
		ItemsTotal: 1, DetailCode: "publication_failed", StartedAt: actionableAt, UpdatedAt: actionableAt, CompletedAt: &actionableAt,
	}
	if err := db.Create(&actionableJob).Error; err != nil {
		t.Fatal(err)
	}
	expectStatus(t, adminRequest(http.MethodGet, "/api/v1/admin/system/jobs", nil), http.StatusOK)
	var actionableOccurrence entity.OperationalAlertOccurrence
	if err := db.First(&actionableOccurrence, "source_type = ? AND source_id = ?", "system_job", actionableJob.ID).Error; err != nil {
		t.Fatalf("non-alertable history starved an actionable failure: %v", err)
	}
	if err := db.Where("alert_id = ?", actionableOccurrence.AlertID).Delete(&entity.Notification{}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Delete(&actionableOccurrence).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Delete(&entity.OperationalAlert{}, "id = ?", actionableOccurrence.AlertID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Where("1 = 1").Delete(&entity.SystemJob{}).Error; err != nil {
		t.Fatal(err)
	}
	reader, readerCookie, readerCSRF := createSystemStatusMember(t, svc, router, admin.User.ID, "notification-reader", []string{"system.read"})
	_, writerCookie, writerCSRF := createSystemStatusMember(t, svc, router, admin.User.ID, "notification-writer", []string{"system.write"})
	readerRequest := systemStatusRequest(t, router, readerCookie, readerCSRF)
	writerRequest := systemStatusRequest(t, router, writerCookie, writerCSRF)

	alert := entity.OperationalAlert{
		ID: "alr_notification_http", GroupKey: "test:http", Kind: "system_job_failure", Severity: "high",
		DetailCode: "publication_failed", State: "open", ETag: "rev_notification_http", OccurrenceCount: 1,
		FirstSeenAt: now, LastSeenAt: now, UpdatedAt: now,
	}
	if err := db.Create(&alert).Error; err != nil {
		t.Fatal(err)
	}
	olderOccurrence := entity.OperationalAlertOccurrence{ID: "occ_notification_old", AlertID: alert.ID, SourceType: "system_job", SourceID: "job_notification_old", DetailCode: alert.DetailCode, OccurredAt: now.Add(-time.Minute)}
	latestOccurrence := entity.OperationalAlertOccurrence{ID: "occ_notification_latest", AlertID: alert.ID, SourceType: "system_job", SourceID: "job_notification_latest", DetailCode: alert.DetailCode, OccurredAt: now}
	if err := db.Create(&[]entity.OperationalAlertOccurrence{olderOccurrence, latestOccurrence}).Error; err != nil {
		t.Fatal(err)
	}
	rows := []entity.Notification{
		{ID: "ntf_notification_admin", RecipientID: admin.User.ID, AlertID: alert.ID, LatestOccurrenceID: latestOccurrence.ID, Kind: alert.Kind, Severity: alert.Severity, DetailCode: alert.DetailCode, OccurrenceCount: 1, FirstSeenAt: now, LastSeenAt: now},
		{ID: "ntf_notification_reader", RecipientID: reader.User.ID, AlertID: alert.ID, LatestOccurrenceID: latestOccurrence.ID, Kind: alert.Kind, Severity: alert.Severity, DetailCode: alert.DetailCode, OccurrenceCount: 1, FirstSeenAt: now, LastSeenAt: now},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	completedDelivery := now.Add(-30 * time.Second)
	if err := db.Create(&entity.NotificationDeliveryIntent{
		ID: "ndl_notification_old", OccurrenceID: olderOccurrence.ID, RecipientID: admin.User.ID,
		RecipientEmail: admin.User.Email, Kind: alert.Kind, Severity: alert.Severity, DetailCode: alert.DetailCode,
		SMTPETag: "rev_notification_smtp", Status: "accepted", Attempts: 1, NextAttemptAt: olderOccurrence.OccurredAt,
		ResultCode: "accepted", CreatedAt: olderOccurrence.OccurredAt, UpdatedAt: completedDelivery, CompletedAt: &completedDelivery,
	}).Error; err != nil {
		t.Fatal(err)
	}

	expectStatus(t, identityRequest(router, http.MethodGet, "/api/v1/notifications", "", nil, ""), http.StatusUnauthorized)
	expectStatus(t, adminRequest(http.MethodGet, "/api/v1/notifications?unexpected=true", nil), http.StatusBadRequest)
	expectStatus(t, adminRequest(http.MethodGet, "/api/v1/notifications?status=read", nil), http.StatusBadRequest)
	adminPage := decodeCatalogResponse[service.NotificationPage](t, adminRequest(http.MethodGet, "/api/v1/notifications?status=unread", nil), http.StatusOK)
	if len(adminPage.Items) != 1 || adminPage.Items[0].ID != rows[0].ID || adminPage.UnreadCount != 1 || adminPage.Items[0].DeliveryStatus != "" {
		t.Fatalf("notification list crossed recipient scope: %+v", adminPage)
	}
	readerPage := decodeCatalogResponse[service.NotificationPage](t, readerRequest(http.MethodGet, "/api/v1/notifications?status=all", nil), http.StatusOK)
	if len(readerPage.Items) != 1 || readerPage.Items[0].ID != rows[1].ID {
		t.Fatalf("reader notification list crossed recipient scope: %+v", readerPage)
	}
	expectStatus(t, readerRequest(http.MethodPost, "/api/v1/notifications/"+rows[0].ID+"/read", nil), http.StatusNotFound)
	expectStatus(t, identityRequest(router, http.MethodPost, "/api/v1/notifications/"+rows[1].ID+"/read", "", readerCookie, ""), http.StatusForbidden)
	expectStatus(t, readerRequest(http.MethodPost, "/api/v1/notifications/"+rows[1].ID+"/read", nil), http.StatusOK)
	expectStatus(t, adminRequest(http.MethodPost, "/api/v1/notifications/read-all", nil), http.StatusNoContent)
	var readerRow entity.Notification
	if err := db.First(&readerRow, "id = ?", rows[1].ID).Error; err != nil || !readerRow.Read {
		t.Fatalf("reader notification was not updated: %+v err=%v", readerRow, err)
	}

	extraAlerts := make([]entity.OperationalAlert, 0, 21)
	extraOccurrences := make([]entity.OperationalAlertOccurrence, 0, 21)
	extraNotifications := make([]entity.Notification, 0, 21)
	for index := range 21 {
		observed := now.Add(-time.Duration(index+1) * time.Hour)
		alertID := fmt.Sprintf("alr_notification_page_%02d", index)
		extraAlerts = append(extraAlerts, entity.OperationalAlert{
			ID: alertID, GroupKey: "test:page:" + alertID, Kind: "system_job_failure", Severity: "medium",
			DetailCode: "delete_failed", State: "open", ETag: "rev_" + alertID, OccurrenceCount: 1,
			FirstSeenAt: observed, LastSeenAt: observed, UpdatedAt: observed,
		})
		occurrenceID := fmt.Sprintf("occ_notification_page_%02d", index)
		extraOccurrences = append(extraOccurrences, entity.OperationalAlertOccurrence{
			ID: occurrenceID, AlertID: alertID, SourceType: "system_job", SourceID: fmt.Sprintf("job_notification_page_%02d", index),
			DetailCode: "delete_failed", OccurredAt: observed,
		})
		extraNotifications = append(extraNotifications, entity.Notification{
			ID: fmt.Sprintf("ntf_notification_page_%02d", index), RecipientID: admin.User.ID, AlertID: alertID, LatestOccurrenceID: occurrenceID,
			Kind: "system_job_failure", Severity: "medium", DetailCode: "delete_failed", OccurrenceCount: 1,
			FirstSeenAt: observed, LastSeenAt: observed,
		})
	}
	if err := db.Create(&extraAlerts).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&extraOccurrences).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&extraNotifications).Error; err != nil {
		t.Fatal(err)
	}
	recurredAt := now.Add(time.Minute)
	recurredOccurrence := entity.OperationalAlertOccurrence{
		ID: "occ_notification_recur", AlertID: extraAlerts[20].ID, SourceType: "system_job", SourceID: "job_notification_recur",
		DetailCode: "delete_failed", OccurredAt: recurredAt,
	}
	if err := db.Create(&recurredOccurrence).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&entity.OperationalAlert{}).Where("id = ?", extraAlerts[20].ID).Updates(map[string]any{"last_seen_at": recurredAt, "updated_at": recurredAt, "occurrence_count": 2}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&entity.Notification{}).Where("id = ?", extraNotifications[20].ID).Updates(map[string]any{"latest_occurrence_id": recurredOccurrence.ID, "last_seen_at": recurredAt, "occurrence_count": 2, "read": false, "read_at": nil}).Error; err != nil {
		t.Fatal(err)
	}
	firstPage := decodeCatalogResponse[service.NotificationPage](t, adminRequest(http.MethodGet, "/api/v1/notifications?status=all&limit=20", nil), http.StatusOK)
	if len(firstPage.Items) != 20 || firstPage.Items[0].ID != extraNotifications[20].ID || firstPage.NextCursor == "" {
		t.Fatalf("recurring notification was not promoted with a usable cursor: %+v", firstPage)
	}
	secondPage := decodeCatalogResponse[service.NotificationPage](t, adminRequest(http.MethodGet, "/api/v1/notifications?status=all&limit=20&cursor="+url.QueryEscape(firstPage.NextCursor), nil), http.StatusOK)
	seen := map[string]bool{}
	for _, item := range firstPage.Items {
		seen[item.ID] = true
	}
	for _, item := range secondPage.Items {
		if seen[item.ID] {
			t.Fatalf("notification cursor repeated %q", item.ID)
		}
	}
	if len(secondPage.Items) != 2 {
		t.Fatalf("notification cursor omitted remaining rows: %+v", secondPage)
	}
	firstAlertPage := decodeCatalogResponse[service.OperationalAlertPage](t, adminRequest(http.MethodGet, "/api/v1/admin/alerts?limit=20", nil), http.StatusOK)
	if len(firstAlertPage.Items) != 20 || firstAlertPage.Items[0].ID != extraAlerts[20].ID || firstAlertPage.NextCursor == "" {
		t.Fatalf("recurring alert was not promoted with a usable cursor: %+v", firstAlertPage)
	}
	secondAlertPage := decodeCatalogResponse[service.OperationalAlertPage](t, adminRequest(http.MethodGet, "/api/v1/admin/alerts?limit=20&cursor="+url.QueryEscape(firstAlertPage.NextCursor), nil), http.StatusOK)
	seenAlerts := map[string]bool{}
	for _, item := range firstAlertPage.Items {
		seenAlerts[item.ID] = true
	}
	for _, item := range secondAlertPage.Items {
		if seenAlerts[item.ID] {
			t.Fatalf("alert cursor repeated %q", item.ID)
		}
	}
	if len(secondAlertPage.Items) != 2 {
		t.Fatalf("alert cursor omitted remaining rows: %+v", secondAlertPage)
	}
	for _, item := range extraNotifications {
		if err := db.Delete(&entity.Notification{}, "id = ?", item.ID).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Delete(&entity.OperationalAlertOccurrence{}, "id = ?", recurredOccurrence.ID).Error; err != nil {
		t.Fatal(err)
	}
	for _, item := range extraOccurrences {
		if err := db.Delete(&entity.OperationalAlertOccurrence{}, "id = ?", item.ID).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, item := range extraAlerts {
		if err := db.Delete(&entity.OperationalAlert{}, "id = ?", item.ID).Error; err != nil {
			t.Fatal(err)
		}
	}

	settings := decodeCatalogResponse[service.NotificationSettingsView](t, adminRequest(http.MethodGet, "/api/v1/notification-settings", nil), http.StatusOK)
	if !settings.InAppEnabled || settings.ExternalEmail != admin.User.Email || settings.ETag != "0" {
		t.Fatalf("default notification settings were not derived from the actor: %+v", settings)
	}
	expectStatus(t, adminRequest(http.MethodPut, "/api/v1/notification-settings", map[string]any{"external_email": "alerts@example.invalid", "email_high": true, "email_medium": true, "etag": settings.ETag, "recipient_id": reader.User.ID}), http.StatusBadRequest)
	expectStatus(t, identityRequest(router, http.MethodPut, "/api/v1/notification-settings", `{"external_email":"alerts@example.invalid","email_high":true,"email_medium":true,"etag":"0"}`, adminCookie, ""), http.StatusForbidden)
	savedSettings := decodeCatalogResponse[service.NotificationSettingsView](t, adminRequest(http.MethodPut, "/api/v1/notification-settings", map[string]any{"external_email": "alerts@example.invalid", "email_high": true, "email_medium": true, "etag": settings.ETag}), http.StatusOK)
	if savedSettings.ExternalEmail != "alerts@example.invalid" || !savedSettings.EmailHigh || !savedSettings.EmailMedium || savedSettings.ETag == settings.ETag {
		t.Fatalf("notification settings were not saved: %+v", savedSettings)
	}
	settingsStart := make(chan struct{})
	settingsResponses := make(chan int, 2)
	var settingsRace sync.WaitGroup
	for index := range 2 {
		index := index
		settingsRace.Go(func() {
			<-settingsStart
			settingsResponses <- adminRequest(http.MethodPut, "/api/v1/notification-settings", map[string]any{
				"external_email": fmt.Sprintf("alerts-%d@example.invalid", index), "email_high": true, "email_medium": true, "etag": savedSettings.ETag,
			}).Code
		})
	}
	close(settingsStart)
	settingsRace.Wait()
	close(settingsResponses)
	settingsOK, settingsConflict := 0, 0
	for status := range settingsResponses {
		switch status {
		case http.StatusOK:
			settingsOK++
		case http.StatusConflict:
			settingsConflict++
		default:
			t.Fatalf("concurrent settings update returned %d", status)
		}
	}
	if settingsOK != 1 || settingsConflict != 1 {
		t.Fatalf("settings CAS allowed %d writes and %d conflicts", settingsOK, settingsConflict)
	}
	expectStatus(t, readerRequest(http.MethodPut, "/api/v1/notification-settings", map[string]any{"external_email": "reader@example.invalid", "email_high": true, "email_medium": false, "etag": "0"}), http.StatusForbidden)
	expectStatus(t, writerRequest(http.MethodGet, "/api/v1/notification-settings", nil), http.StatusForbidden)

	inputTokens, outputTokens := int64(12), int64(8)
	if err := db.Create(&entity.CallRecord{
		SnapshotID: "cfg_notification_http", RequestID: "req_notification_http", UserID: admin.User.ID,
		KeyID: "key_notification_http", ModelID: "mdl_notification_http", ModelName: "Notification model",
		Protocol: entity.ProtocolOpenAIChat, Status: "success", StartedAt: now.Add(-time.Minute), CompletedAt: now,
		InputTokens: &inputTokens, OutputTokens: &outputTokens,
	}).Error; err != nil {
		t.Fatal(err)
	}
	zeroTokens := int64(0)
	if err := db.Create(&entity.CallRecord{
		SnapshotID: "cfg_notification_project", RequestID: "req_notification_project", ProjectID: "prj_notification_http",
		KeyID: "pky_notification_http", ModelID: "mdl_notification_http", ModelName: "Notification model",
		Protocol: entity.ProtocolOpenAIChat, Status: "success", StartedAt: now.Add(-30 * time.Second), CompletedAt: now,
		InputTokens: &zeroTokens, OutputTokens: &zeroTokens,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&entity.Provider{ID: "prv_notification_http", Name: "Notification provider", CreatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	expectStatus(t, adminRequest(http.MethodGet, "/api/v1/admin/overview?unexpected=true", nil), http.StatusBadRequest)
	overview := decodeCatalogResponse[service.AdminOverview](t, adminRequest(http.MethodGet, "/api/v1/admin/overview", nil), http.StatusOK)
	if overview.Today.Calls != 2 || overview.Today.Tokens.Known != "20" || overview.Today.SuccessRate == nil || *overview.Today.SuccessRate != 1 || overview.Today.ActivePrincipals != 3 || len(overview.TokenTrend) != 14 {
		encoded, _ := json.Marshal(overview.Today)
		t.Fatalf("overview did not aggregate persisted call facts: %s", encoded)
	}
	if overview.ProviderReadiness.Providers != 1 || len(overview.ProviderReadiness.Items) != 1 || overview.ProviderReadiness.Items[0].Status != "unconfigured" || len(overview.TopModels) != 1 || overview.TopModels[0].Tokens.Known != "20" || overview.TopModels[0].Calls != 2 || len(overview.Alerts) != 1 {
		t.Fatalf("overview omitted persisted operational facts: %+v", overview)
	}
	expectStatus(t, writerRequest(http.MethodGet, "/api/v1/admin/overview", nil), http.StatusForbidden)
	expectStatus(t, readerRequest(http.MethodPatch, "/api/v1/admin/alerts/"+alert.ID, map[string]any{"state": "handling", "etag": alert.ETag}), http.StatusForbidden)
	expectStatus(t, writerRequest(http.MethodPatch, "/api/v1/admin/alerts/"+alert.ID, map[string]any{"state": "handling", "etag": alert.ETag, "recipient_id": admin.User.ID}), http.StatusBadRequest)
	updated := decodeCatalogResponse[service.OperationalAlertRecord](t, writerRequest(http.MethodPatch, "/api/v1/admin/alerts/"+alert.ID, map[string]any{"state": "handling", "etag": alert.ETag}), http.StatusOK)
	if updated.State != "handling" || updated.ETag == alert.ETag {
		t.Fatalf("alert state was not updated: %+v", updated)
	}

	// Notification publication is secondary to the authoritative source state.
	// Removing the inbox table forces reconciliation to fail after the durable
	// source is already terminal; listing jobs must still report that fact.
	if err := db.Migrator().DropTable(&entity.Notification{}); err != nil {
		t.Fatal(err)
	}
	failedAt := time.Now().UTC()
	terminal := entity.SystemJob{
		ID: "job_ntf_publish_failure", Code: service.SystemJobRuntimePublication, Status: "failed",
		ItemsTotal: 1, DetailCode: "publication_failed", StartedAt: failedAt, UpdatedAt: failedAt, CompletedAt: &failedAt,
	}
	if err := db.Create(&terminal).Error; err != nil {
		t.Fatal(err)
	}
	outageJobs := make([]entity.SystemJob, 0, 269)
	for index := range 269 {
		completed := failedAt.Add(-time.Duration(index+1) * time.Second)
		outageJobs = append(outageJobs, entity.SystemJob{
			ID: fmt.Sprintf("job_ntf_outage_%03d", index), Code: service.SystemJobRuntimePublication, Status: "failed",
			ItemsTotal: 1, DetailCode: "publication_failed", StartedAt: completed, UpdatedAt: completed, CompletedAt: &completed,
		})
	}
	if err := db.Create(&outageJobs).Error; err != nil {
		t.Fatal(err)
	}
	expectStatus(t, adminRequest(http.MethodGet, "/api/v1/admin/system/jobs", nil), http.StatusOK)
	var persisted entity.SystemJob
	if err := db.First(&persisted, "id = ?", terminal.ID).Error; err != nil || persisted.Status != "failed" || persisted.DetailCode != "publication_failed" || persisted.CompletedAt == nil {
		t.Fatalf("notification failure rewrote the authoritative terminal source: %+v err=%v", persisted, err)
	}
	var partialOccurrences int64
	if err := db.Model(&entity.OperationalAlertOccurrence{}).Where("source_type = ? AND source_id = ?", "system_job", terminal.ID).Count(&partialOccurrences).Error; err != nil || partialOccurrences != 0 {
		t.Fatalf("failed notification transaction retained %d partial occurrences: %v", partialOccurrences, err)
	}
	var outageSources int64
	if err := db.Model(&entity.SystemJob{}).Where("id = ? OR id LIKE ?", terminal.ID, "job_ntf_outage_%").Count(&outageSources).Error; err != nil || outageSources != 270 {
		t.Fatalf("retention removed unreconciled alert sources during notification outage: count=%d err=%v", outageSources, err)
	}
	if err := db.Table("schema_migrations").Where("version = ?", 29).Delete(&struct{}{}).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatalf("restore notification schema after forced failure: %v", err)
	}
	for range 3 {
		expectStatus(t, adminRequest(http.MethodGet, "/api/v1/admin/system/jobs", nil), http.StatusOK)
	}
	var recoveredOccurrence entity.OperationalAlertOccurrence
	if err := db.First(&recoveredOccurrence, "source_type = ? AND source_id = ?", "system_job", terminal.ID).Error; err != nil {
		t.Fatalf("restored notification schema did not reconcile the durable source: %v", err)
	}
	var recoveredNotifications int64
	if err := db.Model(&entity.Notification{}).Where("alert_id = ?", recoveredOccurrence.AlertID).Count(&recoveredNotifications).Error; err != nil || recoveredNotifications == 0 {
		t.Fatalf("restored notification schema did not publish the inbox row: count=%d err=%v", recoveredNotifications, err)
	}
	var recoveredOutageOccurrences int64
	if err := db.Model(&entity.OperationalAlertOccurrence{}).Where("source_type = ? AND (source_id = ? OR source_id LIKE ?)", "system_job", terminal.ID, "job_ntf_outage_%").Count(&recoveredOutageOccurrences).Error; err != nil || recoveredOutageOccurrences != 270 {
		t.Fatalf("restored notification schema did not reconcile every retained source: count=%d err=%v", recoveredOutageOccurrences, err)
	}

	// Retention is also secondary: if notification metadata is unavailable, the
	// authoritative job read remains usable and preserves the source for repair.
	for _, model := range []any{&entity.NotificationDeliveryIntent{}, &entity.Notification{}, &entity.OperationalAlertOccurrence{}} {
		if err := db.Migrator().DropTable(model); err != nil {
			t.Fatal(err)
		}
	}
	occurrenceOutageAt := time.Now().UTC()
	occurrenceOutageJob := entity.SystemJob{
		ID: "job_ntf_occurrence_outage", Code: service.SystemJobRuntimePublication, Status: "failed",
		ItemsTotal: 1, DetailCode: "publication_failed", StartedAt: occurrenceOutageAt, UpdatedAt: occurrenceOutageAt, CompletedAt: &occurrenceOutageAt,
	}
	if err := db.Create(&occurrenceOutageJob).Error; err != nil {
		t.Fatal(err)
	}
	expectStatus(t, adminRequest(http.MethodGet, "/api/v1/admin/system/jobs", nil), http.StatusOK)
	persisted = entity.SystemJob{}
	if err := db.First(&persisted, "id = ?", occurrenceOutageJob.ID).Error; err != nil || persisted.Status != "failed" {
		t.Fatalf("notification metadata outage hid or removed the authoritative source: %+v err=%v", persisted, err)
	}
	if err := db.Table("schema_migrations").Where("version = ?", 29).Delete(&struct{}{}).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatalf("restore occurrence schema after forced failure: %v", err)
	}
	for range 3 {
		expectStatus(t, adminRequest(http.MethodGet, "/api/v1/admin/system/jobs", nil), http.StatusOK)
	}
	recoveredOccurrence = entity.OperationalAlertOccurrence{}
	if err := db.First(&recoveredOccurrence, "source_type = ? AND source_id = ?", "system_job", occurrenceOutageJob.ID).Error; err != nil {
		t.Fatalf("restored occurrence schema did not reconcile the retained source: %v", err)
	}
}
