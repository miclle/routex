package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/fox-gonic/fox"
	"net/http"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func credentialFailureFact(credentialID, status, suffix string, completedAt time.Time) service.CallFact {
	return service.CallFact{RequestID: "req_failure_" + suffix, UserID: "usr_historical", Protocol: entity.ProtocolOpenAIChat,
		Status: status, StartedAt: completedAt.Add(-time.Second), CompletedAt: completedAt,
		Attempts: []service.CallAttempt{{ID: "att_failure_" + suffix, CredentialID: credentialID, SnapshotID: "cfg_recorded",
			Status: status, StartedAt: completedAt.Add(-time.Second), CompletedAt: completedAt}}}
}

func credentialFailureState(t *testing.T, db *gorm.DB, credentialID string) entity.CredentialInferenceFailureState {
	t.Helper()
	digest := sha256.Sum256([]byte("routex.credential-inference-failure:" + credentialID))
	var state entity.CredentialInferenceFailureState
	if err := db.Where("credential_digest = ?", hex.EncodeToString(digest[:])).Take(&state).Error; err != nil || state.CredentialID != credentialID {
		t.Fatal("exact Credential state unavailable", credentialID, state, err)
	}
	return state
}

// Registered by the shared owner on both supported databases. No test-side
// observer, synthetic delivery engine or alternative acceptance order is used.
func testCredentialInferenceFailureLifecycle(t *testing.T, db *gorm.DB) {
	ctx := context.Background()
	svc, err := service.New(ctx, db, service.WithSMTPPolicy(true))
	if err != nil {
		t.Fatal(err)
	}
	router := fox.New()
	New(svc).RegisterRoutes(router)
	setup := identityRequest(router, http.MethodPost, "/api/v1/setup", `{"email":"credential-notice-admin@example.invalid","password":"test-only-credential-notice-password","name":"Credential Notice Admin"}`, nil, "")
	expectStatus(t, setup, http.StatusCreated)
	admin, adminCookie := readIdentity(t, setup)
	adminRequest := systemStatusRequest(t, router, adminCookie, admin.CSRFToken)
	if err := db.Create(&entity.NotificationSetting{UserID: admin.User.ID, ExternalEmail: admin.User.Email, EmailMedium: false, ETag: "rev_credential_notice_settings", UpdatedAt: time.Now().UTC()}).Error; err != nil {
		t.Fatal(err)
	}

	at := time.Now().UTC().Truncate(time.Microsecond).Add(-time.Hour)
	record := func(fact service.CallFact) {
		t.Helper()
		if err := svc.RecordCall(ctx, fact); err != nil {
			t.Fatal("record canonical observer fact", err)
		}
	}
	episodes := func(credentialID string) []entity.CredentialInferenceFailureEpisode {
		t.Helper()
		state := credentialFailureState(t, db, credentialID)
		var rows []entity.CredentialInferenceFailureEpisode
		if err := db.Where("credential_digest = ?", state.CredentialDigest).Order("acceptance_sequence").Find(&rows).Error; err != nil {
			t.Fatal(err)
		}
		return rows
	}
	const target = "crd_failure_exact"
	for i := 1; i <= 4; i++ {
		record(credentialFailureFact(target, "error", fmt.Sprintf("threshold_%d", i), at))
	}
	if len(episodes(target)) != 0 {
		t.Fatal("four failures triggered an episode")
	}
	// Durable manual-check audit facts are not inference
	// observations. Neither successful nor failed manual checks rearm this state.
	beforeVerify := credentialFailureState(t, db, target)
	for i, action := range []string{"credential.verify", "credential.verify.failed"} {
		if err := db.Create(&entity.AuditEvent{ID: fmt.Sprintf("aud_failure_manual_%d", i), ActorID: "usr_historical", Action: action,
			ResourceType: "credential", ResourceID: target, CreatedAt: at}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if got := credentialFailureState(t, db, target); !reflect.DeepEqual(beforeVerify, got) {
		t.Fatal("manual verification changed inference state")
	}
	fifth := credentialFailureFact(target, "error", "threshold_5", at.Add(-time.Minute))
	record(fifth)
	first := episodes(target)
	if len(first) != 1 || first[0].FailureCount != 5 || first[0].AcceptanceSequence != 5 || first[0].TriggerAttemptID != fifth.Attempts[0].ID || first[0].CredentialID != target || first[0].LastSuccessAttemptID != "" {
		t.Fatal("threshold source lost exact acceptance evidence", first)
	}
	state := credentialFailureState(t, db, target)
	for range 2 {
		record(fifth)
	}
	if !reflect.DeepEqual(state, credentialFailureState(t, db, target)) || !reflect.DeepEqual(first, episodes(target)) {
		t.Fatal("duplicate delivery changed accepted state/source")
	}
	var originalAttempt entity.CallAttempt
	if err := db.Take(&originalAttempt, "id = ?", fifth.Attempts[0].ID).Error; err != nil {
		t.Fatal(err)
	}
	changedReplay := fifth
	changedReplay.Attempts = append([]service.CallAttempt(nil), fifth.Attempts...)
	changedReplay.Status, changedReplay.Attempts[0].Status = "success", "success"
	record(changedReplay)
	var unchanged entity.CallAttempt
	if err := db.Take(&unchanged, "id = ?", originalAttempt.ID).Error; err != nil || !reflect.DeepEqual(originalAttempt, unchanged) || !reflect.DeepEqual(state, credentialFailureState(t, db, target)) {
		t.Fatal("replay changed immutable inference outcome", err)
	}
	for i := 6; i <= 10; i++ {
		record(credentialFailureFact(target, "error", fmt.Sprintf("threshold_%d", i), at.Add(-time.Duration(i)*time.Minute)))
	}
	if len(episodes(target)) != 1 {
		t.Fatal("additional failures retriggered the latched episode")
	}
	record(credentialFailureFact(target, "canceled", "latched_cancel", at))
	// A future completion is an inspected unknown boundary, not omitted history
	// and not inferred success/error. It cannot clear the triggered latch.
	record(credentialFailureFact(target, "error", "latched_unknown", time.Now().UTC().Add(time.Hour)))
	for i := 1; i <= 5; i++ {
		record(credentialFailureFact(target, "error", fmt.Sprintf("latched_after_%d", i), at))
	}
	if got := credentialFailureState(t, db, target); !got.Latched || len(episodes(target)) != 1 {
		t.Fatal("unknown/canceled observations rearmed the episode", got)
	}
	lateSuccess := credentialFailureFact(target, "success", "late_success", at.Add(-2*time.Hour))
	record(lateSuccess)
	if got := credentialFailureState(t, db, target); got.Latched || got.ConsecutiveFailures != 0 || got.LastSuccessAttemptID != lateSuccess.Attempts[0].ID {
		t.Fatal("late success was not processed in acceptance order", got)
	}
	for i := 1; i <= 5; i++ {
		record(credentialFailureFact(target, "error", fmt.Sprintf("rearmed_%d", i), at.Add(-3*time.Hour)))
	}
	if got := episodes(target); len(got) != 2 || !reflect.DeepEqual(first[0], got[0]) || got[1].LastSuccessAttemptID != lateSuccess.Attempts[0].ID {
		t.Fatal("rearming lost retained history or success anchor", got)
	}

	for index, credentialID := range []string{"crd_failure_case", "CRD_FAILURE_CASE", "crd_failure_replacement"} {
		for i := 1; i <= 4; i++ {
			record(credentialFailureFact(credentialID, "error", fmt.Sprintf("isolated_%d_%d", index, i), at))
		}
		if got := credentialFailureState(t, db, credentialID); got.ConsecutiveFailures != 4 || got.Latched || len(episodes(credentialID)) != 0 {
			t.Fatal("different exact Credentials borrowed history", got)
		}
	}
	// Missing attribution must not borrow the logical Call's Connection or final
	// route. There is no live Credential row for any of these historical facts.
	var stateCountBefore, stateCountAfter int64
	if err := db.Model(&entity.CredentialInferenceFailureState{}).Count(&stateCountBefore).Error; err != nil {
		t.Fatal(err)
	}
	record(credentialFailureFact("", "error", "unattributed", at))
	if err := db.Model(&entity.CredentialInferenceFailureState{}).Count(&stateCountAfter).Error; err != nil || stateCountAfter != stateCountBefore {
		t.Fatal("missing Credential attribution created inferred state", err)
	}

	// The Attempt slice deliberately disagrees with AttemptNumber. Acceptance
	// must process errors 1..4, success 5, then error 6, producing no episode.
	ordered := credentialFailureFact("crd_failure_order", "error", "ordered", at)
	ordered.Attempts = nil
	for i := 6; i >= 1; i-- {
		status := "error"
		if i == 5 {
			status = "success"
		}
		attempt := credentialFailureFact("crd_failure_order", status, fmt.Sprintf("ordered_%d", i), at).Attempts[0]
		attempt.AttemptNumber = i
		ordered.Attempts = append(ordered.Attempts, attempt)
	}
	record(ordered)
	if got := credentialFailureState(t, db, "crd_failure_order"); got.AcceptanceSequence != 6 || got.ConsecutiveFailures != 1 || got.Latched || len(episodes(got.CredentialID)) != 0 {
		t.Fatal("same-call Attempt order was inferred from slice order", got)
	}

	// A failed source insert must roll back the Call, Attempt, counter and latch.
	const rollbackTarget = "crd_failure_rollback"
	for i := 1; i <= 4; i++ {
		record(credentialFailureFact(rollbackTarget, "error", fmt.Sprintf("rollback_%d", i), at))
	}
	rollbackBefore := credentialFailureState(t, db, rollbackTarget)
	rollbackFact := credentialFailureFact(rollbackTarget, "error", "rollback_5", at)
	injected := errors.New("controlled episode insert failure")
	if err := db.Callback().Create().Before("gorm:create").Register("test:credential-failure-source-rollback", func(tx *gorm.DB) {
		if tx.Statement.Table == "credential_inference_failure_episodes" {
			_ = tx.AddError(injected)
		}
	}); err != nil {
		t.Fatal(err)
	}
	rollbackErr := svc.RecordCall(ctx, rollbackFact)
	if err := db.Callback().Create().Remove("test:credential-failure-source-rollback"); err != nil {
		t.Fatal(err)
	}
	if rollbackErr == nil || !reflect.DeepEqual(rollbackBefore, credentialFailureState(t, db, rollbackTarget)) || len(episodes(rollbackTarget)) != 0 {
		t.Fatal("failed source write changed observer state", rollbackErr)
	}
	for _, item := range []struct {
		model      any
		key, value string
	}{
		{&entity.CallRecord{}, "request_id", rollbackFact.RequestID},
		{&entity.CallAttempt{}, "id", rollbackFact.Attempts[0].ID},
	} {
		var count int64
		if err := db.Model(item.model).Where(item.key+" = ?", item.value).Count(&count).Error; err != nil || count != 0 {
			t.Fatal("failed source write left a partial accepted fact", count, err)
		}
	}
	record(rollbackFact)
	if len(episodes(rollbackTarget)) != 1 {
		t.Fatal("retry did not recover the exact threshold source")
	}

	// Concurrent different Calls share one serial state and exactly one threshold
	// source. Concurrent duplicate delivery must not add another sequence step.
	const concurrentTarget = "crd_failure_concurrent"
	start := make(chan struct{})
	results := make(chan error, 6)
	var workers sync.WaitGroup
	for i := 1; i <= 6; i++ {
		fact := credentialFailureFact(concurrentTarget, "error", fmt.Sprintf("concurrent_%d", min(i, 5)), at)
		workers.Go(func() {
			<-start
			results <- svc.RecordCall(ctx, fact)
		})
	}
	close(start)
	workers.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal("concurrent acceptance failed", err)
		}
	}
	if got := credentialFailureState(t, db, concurrentTarget); got.AcceptanceSequence != 5 || got.ConsecutiveFailures != 5 || !got.Latched || len(episodes(concurrentTarget)) != 1 {
		t.Fatal("concurrency duplicated or lost accepted attempts", got)
	}
	// A failed projection rolls back all projection rows, retaining durable
	// episode sources for ordinary reconciliation after Service recreation.
	const projectionFault = "test:credential-notice-projection"
	if err := db.Callback().Create().Before("gorm:create").Register(projectionFault, func(tx *gorm.DB) {
		if row, ok := tx.Statement.Dest.(*entity.OperationalAlertOccurrence); ok && row.SourceType == "credential_inference" {
			_ = tx.AddError(errors.New("controlled Credential projection failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Callback().Create().Remove(projectionFault) }()
	expectStatus(t, adminRequest(http.MethodGet, "/api/v1/admin/alerts", nil), http.StatusInternalServerError)
	if err := db.Callback().Create().Remove(projectionFault); err != nil {
		t.Fatal(err)
	}
	var projected int64
	if err := db.Model(&entity.OperationalAlertOccurrence{}).Where("source_type = ?", "credential_inference").Count(&projected).Error; err != nil || projected != 0 {
		t.Fatal("failed projection retained partial occurrences", err)
	}
	restarted, err := service.New(ctx, db, service.WithSMTPPolicy(true))
	if err != nil {
		t.Fatal(err)
	}
	if err := restarted.RecordCall(ctx, rollbackFact); err != nil || !reflect.DeepEqual(first[0], episodes(target)[0]) || len(episodes(rollbackTarget)) != 1 {
		t.Fatal("restart replay changed retained episode evidence", err)
	}
	recoveredRouter := fox.New()
	New(restarted).RegisterRoutes(recoveredRouter)
	recoveredRequest := systemStatusRequest(t, recoveredRouter, adminCookie, admin.CSRFToken)
	for range 2 {
		expectStatus(t, recoveredRequest(http.MethodGet, "/api/v1/admin/alerts", nil), http.StatusOK)
	}
	var alert entity.OperationalAlert
	digest := credentialFailureState(t, db, target).CredentialDigest
	if err := db.Where("group_key = ?", "credential_inference:"+digest).Take(&alert).Error; err != nil || alert.CredentialID != target || alert.OccurrenceCount != 2 || alert.Severity != "medium" || alert.SubjectType != "" {
		t.Fatal("recovery lost exact Credential episodes or inferred catalog identity", err, alert)
	}
	page, err := restarted.ListNotifications(ctx, admin.User.ID, service.NotificationFilter{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	var noticeID string
	for _, notice := range page.Items {
		if notice.AlertID == alert.ID {
			noticeID = notice.ID
			if notice.CredentialID != target || notice.OccurrenceCount != 2 || notice.Severity != "medium" {
				t.Fatal("inbox lost immutable Credential identity", notice)
			}
		}
	}
	if noticeID == "" {
		t.Fatal("recovery did not create recipient inbox")
	}
	read, err := restarted.MarkNotificationRead(ctx, admin.User.ID, noticeID)
	if err != nil || !read.Read || read.CredentialID != target {
		t.Fatal("read lost Credential identity", err)
	}
	expectStatus(t, recoveredRequest(http.MethodGet, "/api/v1/admin/alerts", nil), http.StatusOK)
	var readRow entity.Notification
	if err := db.Take(&readRow, "id = ?", noticeID).Error; err != nil || !readRow.Read {
		t.Fatal("source replay reset retained read state", err)
	}
	testCredentialInferenceRecovery(t, db, restarted, admin.User.ID)
	testCredentialInferenceProjectionSnapshot(t, db, restarted, admin.User.ID)
	var adminRow entity.User
	if err := db.Take(&adminRow, "id = ?", admin.User.ID).Error; err != nil {
		t.Fatal(err)
	}
	member := entity.User{ID: "usr_credential_notice_other", Email: "credential-notice-other@example.invalid", Name: "Other recipient", PasswordHash: adminRow.PasswordHash, Role: entity.RoleMember}
	if err := db.Create(&member).Error; err != nil {
		t.Fatal(err)
	}
	login := identityRequest(recoveredRouter, http.MethodPost, "/api/v1/auth/login", `{"email":"credential-notice-other@example.invalid","password":"test-only-credential-notice-password"}`, nil, "")
	expectStatus(t, login, http.StatusOK)
	other, otherCookie := readIdentity(t, login)
	otherRequest := systemStatusRequest(t, recoveredRouter, otherCookie, other.CSRFToken)
	expectStatus(t, otherRequest(http.MethodGet, "/api/v1/admin/alerts", nil), http.StatusForbidden)
	others, err := restarted.ListNotifications(ctx, other.User.ID, service.NotificationFilter{Limit: 100})
	if err != nil || len(others.Items) != 0 {
		t.Fatal("operational Credential notice leaked without system.read", err)
	}
	if _, err := restarted.MarkNotificationRead(ctx, other.User.ID, noticeID); err == nil {
		t.Fatal("other recipient read exact notice")
	}
	if err := db.Model(&entity.NotificationDeliveryIntent{}).Count(&projected).Error; err != nil || projected != 0 {
		t.Fatal("default medium preference created email", err)
	}
	host, port, accepted := smtpDeliveryFixture(t)
	unknownHost, unknownPort := smtpUnknownAcceptanceFixture(t)
	if err := db.Model(&entity.NotificationSetting{}).Where("user_id = ?", admin.User.ID).Updates(map[string]any{"external_email": admin.User.Email, "email_medium": true}).Error; err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []struct {
		name, host, status string
		port               int
	}{{"accepted", host, "accepted", port}, {"unknown", unknownHost, "unknown", unknownPort}} {
		smtpETag := "rev_credential_notice_" + scenario.name
		if err := db.Model(&entity.SMTPSetting{}).Where("id = ?", 1).Updates(map[string]any{"enabled": true, "host": scenario.host, "port": scenario.port, "security": "NONE", "sender_name": "RouteX", "sender_email": "notices@example.invalid", "e_tag": smtpETag}).Error; err != nil {
			t.Fatal(err)
		}
		mailCredential := "crd_notice_" + scenario.name
		for i := 1; i <= 5; i++ {
			record(credentialFailureFact(mailCredential, "error", fmt.Sprintf("mail_%s_%d", scenario.name, i), at))
		}
		expectStatus(t, recoveredRequest(http.MethodGet, "/api/v1/admin/alerts", nil), http.StatusOK)
		if _, err := restarted.FlushNotificationDelivery(ctx, 100); err != nil {
			t.Fatal(err)
		}
		var intent entity.NotificationDeliveryIntent
		if err := db.Where("credential_id = ? AND recipient_id = ?", mailCredential, admin.User.ID).Take(&intent).Error; err != nil || intent.Status != scenario.status || intent.Attempts != 1 || intent.Kind != "credential_inference_failure" || intent.Severity != "medium" || intent.SMTPETag != smtpETag {
			t.Fatal("medium Credential email lost durable terminal result", scenario.name, err, intent)
		}
		record(credentialFailureFact(mailCredential, "success", "mail_"+scenario.name+"_recovery", at))
		expectStatus(t, recoveredRequest(http.MethodGet, "/api/v1/admin/alerts", nil), http.StatusOK)
		if count, err := restarted.FlushNotificationDelivery(ctx, 100); err != nil || count != 0 {
			t.Fatal("accepted or unknown Credential delivery replayed after recovery", count, err)
		}
		var after entity.NotificationDeliveryIntent
		if err := db.Take(&after, "id = ?", intent.ID).Error; err != nil || !reflect.DeepEqual(intent, after) || accepted.Load() != 1 {
			t.Fatal("terminal Credential delivery changed or resent", err)
		}
	}

}

// Reuses the existing controlled catalog peer, encryption store, router and
// actual Verify endpoint. Manual success/failure must not rearm an inference
// episode, even though the existing explicit verification policy changes status.
func testCredentialFailureManualVerification(t *testing.T, db *gorm.DB, svc *service.Service, credentialID string, verify func(bool)) {
	t.Helper()
	at := time.Now().UTC().Add(-time.Minute)
	if err := svc.RecordCall(t.Context(), credentialFailureFact(credentialID, "success", "manual_verify_anchor", at)); err != nil {
		t.Fatal(err)
	}
	baselineState := credentialFailureState(t, db, credentialID)
	var baselineEpisodes int64
	if err := db.Model(&entity.CredentialInferenceFailureEpisode{}).Where("credential_digest = ?", baselineState.CredentialDigest).Count(&baselineEpisodes).Error; err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 5; i++ {
		if err := svc.RecordCall(t.Context(), credentialFailureFact(credentialID, "error", fmt.Sprintf("manual_verify_failure_%d", i), at)); err != nil {
			t.Fatal(err)
		}
	}
	before := credentialFailureState(t, db, credentialID)
	if !before.Latched || before.ConsecutiveFailures != 5 {
		t.Fatal("manual verification control lacks triggered inference baseline")
	}
	var episodes []entity.CredentialInferenceFailureEpisode
	if err := db.Where("credential_digest = ?", before.CredentialDigest).Order("acceptance_sequence").Find(&episodes).Error; err != nil || int64(len(episodes)) != baselineEpisodes+1 {
		t.Fatal("manual Verify control lacks immutable episode", err)
	}
	for _, failed := range []bool{false, true} {
		verify(failed)
		if got := credentialFailureState(t, db, credentialID); !reflect.DeepEqual(before, got) {
			t.Fatal("actual manual verification changed inference observer", failed, got)
		}
		var after []entity.CredentialInferenceFailureEpisode
		if err := db.Where("credential_digest = ?", before.CredentialDigest).Order("acceptance_sequence").Find(&after).Error; err != nil || !reflect.DeepEqual(episodes, after) {
			t.Fatal("actual manual Verify changed immutable inference episode", failed, err)
		}
	}
}

// Uses the same registered database leaf, Service and administrator. These are
// real RecordCall and reconciliation transactions, not test-side projection.
func testCredentialInferenceRecovery(t *testing.T, db *gorm.DB, svc *service.Service, actor string) {
	t.Helper()
	ctx := t.Context()
	at := time.Now().UTC().Add(-4 * time.Hour)
	record := func(credentialID, status, suffix string) service.CallFact {
		t.Helper()
		fact := credentialFailureFact(credentialID, status, suffix, at)
		if err := svc.RecordCall(ctx, fact); err != nil {
			t.Fatal("record recovery control", err)
		}
		return fact
	}
	failures := func(credentialID, prefix string) {
		t.Helper()
		for i := 1; i <= 5; i++ {
			record(credentialID, "error", fmt.Sprintf("%s_%d", prefix, i))
		}
	}
	reconcile := func() {
		t.Helper()
		if _, err := svc.ListOperationalAlerts(ctx, actor, service.OperationalAlertFilter{Limit: 100}); err != nil {
			t.Fatal("reconcile recovery control", err)
		}
	}
	alertFor := func(credentialID string) entity.OperationalAlert {
		t.Helper()
		state := credentialFailureState(t, db, credentialID)
		var alert entity.OperationalAlert
		if err := db.Where("group_key = ?", "credential_inference:"+state.CredentialDigest).Take(&alert).Error; err != nil {
			t.Fatal(err)
		}
		return alert
	}
	noticeFor := func(alertID string) entity.Notification {
		t.Helper()
		var notice entity.Notification
		if err := db.Where("alert_id = ? AND recipient_id = ?", alertID, actor).Take(&notice).Error; err != nil {
			t.Fatal(err)
		}
		return notice
	}
	const target = "crd_recovery_current"
	failures(target, "recovery_initial")
	reconcile()
	initial := alertFor(target)
	if initial.State != "open" || initial.OccurrenceCount != 1 {
		t.Fatal("recovery control lacks open episode", initial)
	}
	notice := noticeFor(initial.ID)
	if _, err := svc.MarkNotificationRead(ctx, actor, notice.ID); err != nil {
		t.Fatal(err)
	}
	read := noticeFor(initial.ID)
	if _, err := svc.UpdateOperationalAlertState(ctx, actor, initial.ID, service.OperationalAlertStateInput{State: "handling", ETag: initial.ETag}); err != nil {
		t.Fatal(err)
	}
	handling := alertFor(target)
	reconcile()
	if got := alertFor(target); !reflect.DeepEqual(handling, got) {
		t.Fatal("repeat projection reopened administrator handling state", got)
	}
	record(target, "canceled", "recovery_cancel")
	unknown := credentialFailureFact(target, "success", "recovery_unknown_success", time.Now().UTC().Add(time.Hour))
	if err := svc.RecordCall(ctx, unknown); err != nil {
		t.Fatal(err)
	}
	reconcile()
	if got := alertFor(target); !reflect.DeepEqual(handling, got) || !reflect.DeepEqual(read, noticeFor(initial.ID)) {
		t.Fatal("canceled or unknown observation resolved/reset history", got)
	}
	// A resolution write failure rolls back the success and its observer state.
	beforeSuccess := credentialFailureState(t, db, target)
	success := credentialFailureFact(target, "success", "recovery_success", at.Add(-time.Hour))
	const failureCallback = "test:credential-success-resolution"
	if err := db.Callback().Update().Before("gorm:update").Register(failureCallback, func(tx *gorm.DB) {
		if tx.Statement.Table == "operational_alerts" {
			_ = tx.AddError(errors.New("controlled success resolution failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	err := svc.RecordCall(ctx, success)
	if removeErr := db.Callback().Update().Remove(failureCallback); removeErr != nil {
		t.Fatal(removeErr)
	}
	if err == nil || !reflect.DeepEqual(beforeSuccess, credentialFailureState(t, db, target)) || !reflect.DeepEqual(handling, alertFor(target)) {
		t.Fatal("resolution failure did not roll back accepted success", err)
	}
	if err := svc.RecordCall(ctx, success); err != nil {
		t.Fatal(err)
	}
	resolved := alertFor(target)
	if resolved.State != "resolved" || resolved.ETag == handling.ETag || resolved.OccurrenceCount != 1 || !reflect.DeepEqual(read, noticeFor(initial.ID)) {
		t.Fatal("late accepted success failed to resolve without rewriting inbox", resolved)
	}
	for range 2 {
		if err := svc.RecordCall(ctx, success); err != nil {
			t.Fatal(err)
		}
		reconcile()
	}
	if !reflect.DeepEqual(resolved, alertFor(target)) || !reflect.DeepEqual(read, noticeFor(initial.ID)) {
		t.Fatal("success replay rewrote resolved alert/read history")
	}
	// Existing manual state API supplies a real already-projected recovery
	// candidate; the durable success proof resolves it on the next read.
	if _, err := svc.UpdateOperationalAlertState(ctx, actor, resolved.ID, service.OperationalAlertStateInput{State: "handling", ETag: resolved.ETag}); err != nil {
		t.Fatal(err)
	}
	reconcile()
	if got := alertFor(target); got.State != "resolved" || !reflect.DeepEqual(read, noticeFor(initial.ID)) {
		t.Fatal("already-projected success resolution was not recovered", got)
	}
	failures(target, "recovery_next")
	reconcile()
	reopened := alertFor(target)
	if reopened.State != "open" || reopened.OccurrenceCount != 2 || noticeFor(initial.ID).Read {
		t.Fatal("new current trigger did not reopen/reset unread once", reopened)
	}
	if _, err := svc.UpdateOperationalAlertState(ctx, actor, reopened.ID, service.OperationalAlertStateInput{State: "resolved", ETag: reopened.ETag}); err != nil {
		t.Fatal(err)
	}
	manualResolved := alertFor(target)
	record(target, "error", "recovery_latched_error")
	reconcile()
	if !reflect.DeepEqual(manualResolved, alertFor(target)) {
		t.Fatal("latched replay reopened administrator resolved state")
	}

	// Projection delayed until after success is historical from its first write.
	const pending = "crd_recovery_pending"
	failures(pending, "recovery_pending")
	record(pending, "success", "recovery_pending_success")
	reconcile()
	if got := alertFor(pending); got.State != "resolved" || got.OccurrenceCount != 1 {
		t.Fatal("delayed historical episode reopened recovered alert", got)
	}

	// Reversed recorded times model timestamps captured before concurrent row
	// waits. The higher accepted sequence must own the latest subject and inbox.
	const reversed = "crd_recovery_reversed"
	failures(reversed, "recovery_old")
	record(reversed, "success", "recovery_between")
	for i := 1; i <= 5; i++ {
		fact := credentialFailureFact(reversed, "error", fmt.Sprintf("recovery_new_%d", i), at)
		fact.Attempts[0].ProviderID, fact.Attempts[0].ProviderName = "prv_recorded_new", "Recorded newer episode"
		fact.Attempts[0].ProviderModelID, fact.Attempts[0].ConnectionID = "pmd_recorded_new", "con_recorded_new"
		fact.Attempts[0].ConnectionName, fact.Attempts[0].UpstreamModelName = "Recorded connection", "recorded-model"
		if err := svc.RecordCall(ctx, fact); err != nil {
			t.Fatal(err)
		}
	}
	var sourceRows []entity.CredentialInferenceFailureEpisode
	if err := db.Where("credential_digest = ?", credentialFailureState(t, db, reversed).CredentialDigest).Order("acceptance_sequence").Find(&sourceRows).Error; err != nil || len(sourceRows) != 2 {
		t.Fatal("reversed timestamp control lacks two episodes", err)
	}
	for i, source := range sourceRows {
		if err := db.Model(&source).Update("recorded_at", at.Add(time.Duration(2-i)*time.Hour)).Error; err != nil {
			t.Fatal(err)
		}
	}
	reconcile()
	latest := alertFor(reversed)
	latestNotice := noticeFor(latest.ID)
	var latestOccurrence entity.OperationalAlertOccurrence
	if err := db.Take(&latestOccurrence, "id = ?", latestNotice.LatestOccurrenceID).Error; err != nil {
		t.Fatal(err)
	}
	if latest.State != "open" || latest.OccurrenceCount != 2 || latest.SubjectName != "Recorded newer episode" || latestNotice.SubjectName != latest.SubjectName || latestOccurrence.SourceID != sourceRows[1].TriggerAttemptID {
		t.Fatal("wall-clock order replaced current acceptance proof", latest, latestNotice)
	}
	if _, err := svc.MarkNotificationRead(ctx, actor, latestNotice.ID); err != nil {
		t.Fatal(err)
	}
	latestRead := noticeFor(latest.ID)
	reconcile()
	if !reflect.DeepEqual(latestRead, noticeFor(latest.ID)) || !reflect.DeepEqual(latest, alertFor(reversed)) {
		t.Fatal("historical projection replay changed latest read/snapshot")
	}
	// A real acceptance racing reconciliation must converge to success-resolved
	// proof without deadlocking on episode/alert locks acquired before state.
	results := make(chan error, 2)
	start := make(chan struct{})
	var concurrent sync.WaitGroup
	concurrent.Go(func() {
		<-start
		results <- svc.RecordCall(ctx, credentialFailureFact(reversed, "success", "recovery_race_success", at))
	})
	concurrent.Go(func() {
		<-start
		_, err := svc.ListOperationalAlerts(ctx, actor, service.OperationalAlertFilter{Limit: 100})
		results <- err
	})
	close(start)
	concurrent.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal("concurrent success/reconciliation failed", err)
		}
	}
	reconcile()
	if got := alertFor(reversed); got.State != "resolved" || got.SubjectName != latest.SubjectName || !reflect.DeepEqual(latestRead, noticeFor(latest.ID)) {
		t.Fatal("concurrent recovery reopened or replaced immutable history", got)
	}
}

// Both publishers use real reconciliation. A narrowly scoped query barrier
// holds an older candidate snapshot while another transaction projects the newer
// episode and its administrator records a resolved/read state. MySQL must use
// current reads after state-lock acquisition, not the older transaction snapshot.
func testCredentialInferenceProjectionSnapshot(t *testing.T, db *gorm.DB, svc *service.Service, actor string) {
	t.Helper()
	ctx := t.Context()
	const target = "crd_recovery_snapshot"
	at := time.Now().UTC().Add(-time.Hour)
	for episode := range 2 {
		if episode != 0 {
			if err := svc.RecordCall(ctx, credentialFailureFact(target, "success", "snapshot_success", at)); err != nil {
				t.Fatal(err)
			}
		}
		for i := 1; i <= 5; i++ {
			if err := svc.RecordCall(ctx, credentialFailureFact(target, "error", fmt.Sprintf("snapshot_%d_%d", episode, i), at)); err != nil {
				t.Fatal(err)
			}
		}
	}
	var sources []entity.CredentialInferenceFailureEpisode
	if err := db.Where("credential_digest = ?", credentialFailureState(t, db, target).CredentialDigest).Order("acceptance_sequence").Find(&sources).Error; err != nil || len(sources) != 2 {
		t.Fatal("snapshot control lacks two accepted episodes", err)
	}
	type projectionControlKey struct{}
	ready, release := make(chan struct{}), make(chan struct{})
	var releaseOnce, readyOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	const selection = "test:credential-projection-selection"
	const barrier = "test:credential-projection-snapshot"
	oldResult := make(chan error, 1)
	oldStarted, oldConsumed := false, false
	t.Cleanup(func() {
		unblock()
		if oldStarted && !oldConsumed {
			<-oldResult
			oldConsumed = true
		}
		_ = db.Callback().Query().Remove(selection)
		_ = db.Callback().Query().Remove(barrier)
	})
	if err := db.Callback().Query().Before("gorm:query").Register(selection, func(tx *gorm.DB) {
		if _, ok := tx.Statement.Dest.(*[]entity.CredentialInferenceFailureEpisode); !ok {
			return
		}
		index, ok := tx.Statement.Context.Value(projectionControlKey{}).(int)
		if ok {
			tx.Statement.AddClause(clause.Where{Exprs: []clause.Expression{clause.Eq{Column: "trigger_attempt_id", Value: sources[index].TriggerAttemptID}}})
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Query().After("gorm:query").Register(barrier, func(tx *gorm.DB) {
		if _, ok := tx.Statement.Dest.(*[]entity.CredentialInferenceFailureEpisode); !ok {
			return
		}
		index, ok := tx.Statement.Context.Value(projectionControlKey{}).(int)
		if ok && index == 0 {
			readyOnce.Do(func() { close(ready) })
			<-release
		}
	}); err != nil {
		t.Fatal(err)
	}
	oldStarted = true
	go func() {
		_, err := svc.ListOperationalAlerts(context.WithValue(ctx, projectionControlKey{}, 0), actor, service.OperationalAlertFilter{Limit: 100})
		oldResult <- err
	}()
	select {
	case <-ready:
	case err := <-oldResult:
		oldConsumed = true
		t.Fatal("older projection exited before snapshot barrier", err)
	case <-ctx.Done():
		t.Fatal("snapshot control canceled before candidate read", ctx.Err())
	}
	if _, err := svc.ListOperationalAlerts(context.WithValue(ctx, projectionControlKey{}, 1), actor, service.OperationalAlertFilter{Limit: 100}); err != nil {
		t.Fatal("publish newer exact episode", err)
	}
	var alert entity.OperationalAlert
	if err := db.Where("group_key = ?", "credential_inference:"+sources[1].CredentialDigest).Take(&alert).Error; err != nil || alert.OccurrenceCount != 1 || alert.State != "open" {
		t.Fatal("snapshot control lacks newer-only open projection", err, alert)
	}
	var notice entity.Notification
	if err := db.Where("alert_id = ? AND recipient_id = ?", alert.ID, actor).Take(&notice).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.MarkNotificationRead(ctx, actor, notice.ID); err != nil {
		t.Fatal(err)
	}
	if err := db.Take(&notice, "id = ?", notice.ID).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdateOperationalAlertState(ctx, actor, alert.ID, service.OperationalAlertStateInput{State: "resolved", ETag: alert.ETag}); err != nil {
		t.Fatal(err)
	}
	unblock()
	err := <-oldResult
	oldConsumed = true
	if err != nil {
		t.Fatal("delayed older projection failed", err)
	}
	var after entity.Notification
	if err := db.Take(&after, "id = ?", notice.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Take(&alert, "id = ?", alert.ID).Error; err != nil || alert.State != "resolved" || alert.OccurrenceCount != 2 || after.OccurrenceCount != 2 || after.LatestOccurrenceID != notice.LatestOccurrenceID || !after.Read || !reflect.DeepEqual(after.ReadAt, notice.ReadAt) || !after.LastSeenAt.Equal(notice.LastSeenAt) {
		t.Fatal("older snapshot reopened/replaced newer resolved/read projection", err, alert, after)
	}
}
