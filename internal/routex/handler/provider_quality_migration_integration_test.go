package handler

import (
	"context"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
)

type operationalAlertV29Integration struct {
	ID   string `gorm:"primaryKey;size:30"`
	Kind string `gorm:"size:40;not null;check:ck_operational_alerts_kind,kind IN ('system_job_failure','credential_verification_failure')"`
}

func (operationalAlertV29Integration) TableName() string { return "operational_alerts" }

type operationalAlertOccurrenceV29Integration struct {
	ID         string `gorm:"primaryKey;size:30"`
	SourceType string `gorm:"size:32;not null;check:ck_alert_occurrences_source,source_type IN ('system_job','credential_verification')"`
}

func (operationalAlertOccurrenceV29Integration) TableName() string {
	return "operational_alert_occurrences"
}

func testProviderQualityMigration(t *testing.T, db *gorm.DB) {
	t.Helper()
	qualityModels := []any{
		&entity.ProviderQualityPolicy{},
		&entity.ProviderQualityWindow{},
		&entity.ProviderQualityState{},
	}
	for _, model := range qualityModels {
		if !db.Migrator().HasTable(model) {
			t.Fatalf("provider quality migration omitted table for %T", model)
		}
		var count int64
		if err := db.Model(model).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("fresh provider quality migration seeded %T rows: count=%d err=%v", model, count, err)
		}
	}
	assertProviderQualitySchema(t, db)

	now := time.Now().UTC().Truncate(time.Microsecond)
	record := entity.CallRecord{
		SnapshotID: "cfg_quality_migration", RequestID: "req_quality_migration", UserID: "usr_quality_migration",
		KeyID: "key_quality_migration", ModelID: "mdl_quality_migration", ModelName: "quality-migration",
		ProviderModelID: "pmd_quality_migration", ConnectionID: "con_quality_migration", Protocol: entity.ProtocolOpenAIChat,
		Status: "success", StartedAt: now, CompletedAt: now,
	}
	if err := db.Create(&record).Error; err != nil {
		t.Fatal(err)
	}
	attempt := entity.CallAttempt{
		ID: "att_quality_migration", RequestID: record.RequestID, ProviderModelID: record.ProviderModelID,
		ConnectionID: record.ConnectionID, Status: "success", FailureClass: "success", WorkEvidence: "completed",
		HTTPStatus: 200, StartedAt: now, CompletedAt: now,
	}
	if err := db.Create(&attempt).Error; err != nil {
		t.Fatal(err)
	}
	alert := entity.OperationalAlert{
		ID: "alr_quality_migration", GroupKey: "quality-migration", Kind: "system_job_failure", Severity: "high",
		DetailCode: "publication_failed", State: "open", OccurrenceCount: 1, FirstSeenAt: now, LastSeenAt: now,
		ETag: "rev_quality_migration", UpdatedAt: now,
	}
	if err := db.Create(&alert).Error; err != nil {
		t.Fatal(err)
	}
	occurrence := entity.OperationalAlertOccurrence{
		ID: "occ_quality_migration", AlertID: alert.ID, SourceType: "system_job", SourceID: "job_quality_migration",
		DetailCode: alert.DetailCode, OccurredAt: now,
	}
	if err := db.Create(&occurrence).Error; err != nil {
		t.Fatal(err)
	}

	// Reconstruct the last released V29 shape. Existing attempt and alert facts
	// must survive without acquiring mutable catalog attribution.
	for _, model := range []any{&entity.ProviderQualityState{}, &entity.ProviderQualityWindow{}, &entity.ProviderQualityPolicy{}} {
		if err := db.Migrator().DropTable(model); err != nil {
			t.Fatal(err)
		}
	}
	for _, field := range []string{"ProviderID", "ProviderName", "ConnectionName", "UpstreamModelName", "DurationMS"} {
		if err := db.Migrator().DropColumn(&entity.CallAttempt{}, field); err != nil {
			t.Fatal(err)
		}
	}
	for _, model := range []any{&entity.OperationalAlert{}, &entity.OperationalAlertOccurrence{}, &entity.Notification{}, &entity.NotificationDeliveryIntent{}} {
		for _, field := range []string{"SubjectType", "SubjectID", "SubjectName"} {
			if err := db.Migrator().DropColumn(model, field); err != nil {
				t.Fatal(err)
			}
		}
	}
	restoreV29NotificationChecks(t, db)
	removeProviderQualityMigrationLedger(t, db)
	if err := database.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	assertProviderQualitySchema(t, db)

	var upgradedAttempt entity.CallAttempt
	if err := db.First(&upgradedAttempt, "id = ?", attempt.ID).Error; err != nil {
		t.Fatal(err)
	}
	if upgradedAttempt.ProviderID != "" || upgradedAttempt.ProviderName != "" || upgradedAttempt.ConnectionName != "" || upgradedAttempt.UpstreamModelName != "" || upgradedAttempt.DurationMS != nil {
		t.Fatalf("legacy attempt gained provider quality facts: %+v", upgradedAttempt)
	}
	var upgradedAlert entity.OperationalAlert
	if err := db.First(&upgradedAlert, "id = ?", alert.ID).Error; err != nil {
		t.Fatal(err)
	}
	if upgradedAlert.SubjectType != "" || upgradedAlert.SubjectID != "" || upgradedAlert.SubjectName != "" {
		t.Fatalf("legacy alert gained subject attribution: %+v", upgradedAlert)
	}

	provider := entity.Provider{ID: "prv_quality_migration", Name: "Quality Provider", CreatedAt: now}
	if err := db.Create(&provider).Error; err != nil {
		t.Fatal(err)
	}
	orphanPolicy := entity.ProviderQualityPolicy{
		ProviderID: "prv_missing", Enabled: true, WindowMinutes: 15, MinimumAttempts: 10,
		MinSuccessRateBPS: 9500, ETag: "rev_policy_orphan", UpdatedBy: "usr_quality_migration",
		UpdateReason: "migration test", UpdatedAt: now,
	}
	if err := db.Create(&orphanPolicy).Error; err == nil {
		t.Fatal("provider quality policy accepted a missing Provider")
	}
	policy := orphanPolicy
	policy.ProviderID = provider.ID
	policy.ETag = "rev_policy_quality"
	if err := db.Create(&policy).Error; err != nil {
		t.Fatal(err)
	}
	invalidProvider := entity.Provider{ID: "prv_quality_invalid", Name: "Invalid Quality Provider", CreatedAt: now}
	if err := db.Create(&invalidProvider).Error; err != nil {
		t.Fatal(err)
	}
	invalidPolicy := policy
	invalidPolicy.ProviderID = invalidProvider.ID
	invalidPolicy.MinimumAttempts = 0
	if err := db.Create(&invalidPolicy).Error; err == nil {
		t.Fatal("provider quality policy accepted zero minimum attempts")
	}
	for _, test := range []struct {
		providerID string
		etag       string
		mutate     func(*entity.ProviderQualityPolicy)
	}{
		{"prv_quality_window_too_large", "rev_bound_window", func(row *entity.ProviderQualityPolicy) { row.WindowMinutes = 1441 }},
		{"prv_quality_minimum_too_large", "rev_bound_minimum", func(row *entity.ProviderQualityPolicy) { row.MinimumAttempts = 100001 }},
		{"prv_quality_duration_too_large", "rev_bound_duration", func(row *entity.ProviderQualityPolicy) {
			value := int64(3_600_001)
			row.MaxP95DurationMS = &value
		}},
	} {
		if err := db.Create(&entity.Provider{ID: test.providerID, Name: "Invalid policy bounds", CreatedAt: now}).Error; err != nil {
			t.Fatal(err)
		}
		candidate := policy
		candidate.ProviderID = test.providerID
		candidate.ETag = test.etag
		test.mutate(&candidate)
		if err := db.Create(&candidate).Error; err == nil {
			t.Fatalf("provider quality policy accepted out-of-bounds values: %+v", candidate)
		}
	}

	window := entity.ProviderQualityWindow{
		ID: "pqw_quality_migration", ProviderID: "prv_historical_snapshot", ProviderName: "Historical Provider",
		WindowStart: now.Add(-15 * time.Minute), WindowEnd: now, PolicyETag: policy.ETag,
		EligibleAttempts: 10, ExcludedAttempts: 2, UnknownAttributionAttempts: 1, Successes: 9,
		HTTP429: 1, KnownDurationAttempts: 10, State: "degraded", DetailCode: "success_rate_below_threshold", CreatedAt: now,
	}
	if err := db.Create(&window).Error; err != nil {
		t.Fatalf("historical quality window must not require a mutable Provider: %v", err)
	}
	previousGeneration := window
	previousGeneration.ID = "pqw_quality_prev_gen"
	previousGeneration.PolicyETag = "rev_policy_previous"
	previousGeneration.WindowStart = now.Add(-30 * time.Minute)
	previousGeneration.WindowEnd = now.Add(-15 * time.Minute)
	if err := db.Create(&previousGeneration).Error; err != nil {
		t.Fatalf("historical policy generation was rejected: %v", err)
	}
	for etag, wantID := range map[string]string{policy.ETag: window.ID, previousGeneration.PolicyETag: previousGeneration.ID} {
		var generation entity.ProviderQualityWindow
		if err := db.Where("provider_id = ? AND policy_e_tag = ?", window.ProviderID, etag).Order("window_end DESC, id DESC").First(&generation).Error; err != nil || generation.ID != wantID {
			t.Fatalf("generation-aware provider quality lookup for %s = %+v, error = %v", etag, generation, err)
		}
	}
	duplicateWindow := window
	duplicateWindow.ID = "pqw_quality_duplicate"
	duplicateWindow.PolicyETag = "rev_policy_changed"
	if err := db.Create(&duplicateWindow).Error; err == nil {
		t.Fatal("provider quality window accepted a retroactive evaluation for the same provider window")
	}
	invalidWindow := window
	invalidWindow.ID = "pqw_quality_invalid"
	invalidWindow.ProviderID = "prv_historical_invalid"
	invalidWindow.WindowEnd = now.Add(time.Minute)
	invalidWindow.State = "unknown"
	if err := db.Create(&invalidWindow).Error; err == nil {
		t.Fatal("provider quality window accepted an unsupported state")
	}
	orphanState := entity.ProviderQualityState{ProviderID: "prv_missing", LastState: "degraded", UpdatedAt: now}
	if err := db.Create(&orphanState).Error; err == nil {
		t.Fatal("provider quality state accepted a missing Provider")
	}
	state := entity.ProviderQualityState{ProviderID: provider.ID, LastWindowEnd: &now, LastState: "degraded", LastAlertID: alert.ID, UpdatedAt: now}
	if err := db.Create(&state).Error; err != nil {
		t.Fatal(err)
	}
	invalidState := entity.ProviderQualityState{ProviderID: invalidProvider.ID, LastState: "unknown", UpdatedAt: now}
	if err := db.Create(&invalidState).Error; err == nil {
		t.Fatal("provider quality state accepted an unsupported state")
	}

	qualityAlert := entity.OperationalAlert{
		ID: "alr_provider_quality", GroupKey: "provider-quality", Kind: "provider_quality_degraded", Severity: "high",
		DetailCode: "multiple_thresholds_breached", SubjectType: "provider", SubjectID: provider.ID, SubjectName: provider.Name,
		State: "open", OccurrenceCount: 1, FirstSeenAt: now, LastSeenAt: now, ETag: "rev_provider_quality", UpdatedAt: now,
	}
	if err := db.Create(&qualityAlert).Error; err != nil {
		t.Fatalf("V30 provider quality alert kind was rejected: %v", err)
	}
	qualityOccurrence := entity.OperationalAlertOccurrence{
		ID: "occ_provider_quality", AlertID: qualityAlert.ID, SourceType: "provider_quality_window", SourceID: window.ID,
		DetailCode: qualityAlert.DetailCode, SubjectType: qualityAlert.SubjectType, SubjectID: qualityAlert.SubjectID,
		SubjectName: qualityAlert.SubjectName, OccurredAt: now,
	}
	if err := db.Create(&qualityOccurrence).Error; err != nil {
		t.Fatalf("V30 provider quality occurrence source was rejected: %v", err)
	}
	invalidSource := qualityOccurrence
	invalidSource.ID = "occ_quality_source_invalid"
	invalidSource.SourceType = "unsupported"
	invalidSource.SourceID = "source_quality_invalid"
	if err := db.Create(&invalidSource).Error; err == nil {
		t.Fatal("V30 occurrence constraint accepted an unsupported source")
	}
	invalidAlert := qualityAlert
	invalidAlert.ID = "alr_quality_invalid"
	invalidAlert.GroupKey = "provider-quality-invalid"
	invalidAlert.Kind = "unsupported"
	if err := db.Create(&invalidAlert).Error; err == nil {
		t.Fatal("V30 alert constraint accepted an unsupported kind")
	}
	invalidSubject := qualityAlert
	invalidSubject.ID = "alr_subject_invalid"
	invalidSubject.GroupKey = "provider-quality-subject-invalid"
	invalidSubject.SubjectType = "connection"
	if err := db.Create(&invalidSubject).Error; err == nil {
		t.Fatal("V30 alert constraint accepted an unsupported subject type")
	}

	// Reproduce interrupted MySQL DDL at independent V30 boundaries. A retry
	// must restore columns, indexes, and the widened check without losing rows.
	if err := db.Migrator().DropColumn(&entity.Notification{}, "SubjectName"); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrator().DropColumn(&entity.CallAttempt{}, "DurationMS"); err != nil {
		t.Fatal(err)
	}
	dropProviderQualityTestIndex(t, db, &entity.CallAttempt{}, "idx_attempts_provider_time")
	dropProviderQualityTestIndex(t, db, &entity.ProviderQualityWindow{}, "idx_provider_quality_windows_policy_end")
	if err := db.Migrator().DropConstraint(&operationalAlertV29Integration{}, "ck_operational_alerts_kind"); err != nil {
		t.Fatal(err)
	}
	removeProviderQualityMigrationLedger(t, db)
	if err := database.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	assertProviderQualitySchema(t, db)
	if err := db.First(&state, "provider_id = ?", provider.ID).Error; err != nil {
		t.Fatalf("partial V30 reentry lost provider quality state: %v", err)
	}

	removeProviderQualityMigrationLedger(t, db)
	if err := database.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	assertProviderQualitySchema(t, db)
}

func restoreV29NotificationChecks(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, item := range []struct {
		model any
		name  string
	}{
		{&operationalAlertV29Integration{}, "ck_operational_alerts_kind"},
		{&operationalAlertOccurrenceV29Integration{}, "ck_alert_occurrences_source"},
	} {
		if db.Migrator().HasConstraint(item.model, item.name) {
			if err := db.Migrator().DropConstraint(item.model, item.name); err != nil {
				t.Fatal(err)
			}
		}
		if err := db.Migrator().CreateConstraint(item.model, item.name); err != nil {
			t.Fatal(err)
		}
	}
}

func removeProviderQualityMigrationLedger(t *testing.T, db *gorm.DB) {
	t.Helper()
	result := db.Table("schema_migrations").Where("version = ?", 30).Delete(&struct{}{})
	if result.Error != nil || result.RowsAffected != 1 {
		t.Fatalf("remove migration 30 ledger: affected=%d err=%v", result.RowsAffected, result.Error)
	}
}

func dropProviderQualityTestIndex(t *testing.T, db *gorm.DB, model any, name string) {
	t.Helper()
	if !db.Migrator().HasIndex(model, name) {
		return
	}
	statements := map[string]map[string]string{
		"postgres": {
			"idx_attempts_provider_time":              "DROP INDEX idx_attempts_provider_time",
			"idx_provider_quality_windows_policy_end": "DROP INDEX idx_provider_quality_windows_policy_end",
		},
		"mysql": {
			"idx_attempts_provider_time":              "DROP INDEX idx_attempts_provider_time ON call_attempts",
			"idx_provider_quality_windows_policy_end": "DROP INDEX idx_provider_quality_windows_policy_end ON provider_quality_windows",
		},
	}
	statement := statements[db.Name()][name]
	if statement == "" {
		t.Fatalf("unsupported test driver %s", db.Name())
	}
	if err := db.Exec(statement).Error; err != nil {
		t.Fatal(err)
	}
}

func assertProviderQualitySchema(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, model := range []any{&entity.ProviderQualityPolicy{}, &entity.ProviderQualityWindow{}, &entity.ProviderQualityState{}} {
		if !db.Migrator().HasTable(model) {
			t.Fatalf("provider quality table missing for %T", model)
		}
	}
	for _, field := range []string{"ProviderID", "ProviderName", "ConnectionName", "UpstreamModelName", "DurationMS"} {
		if !db.Migrator().HasColumn(&entity.CallAttempt{}, field) {
			t.Fatalf("provider quality migration omitted call attempt field %s", field)
		}
	}
	for _, model := range []any{&entity.OperationalAlert{}, &entity.OperationalAlertOccurrence{}, &entity.Notification{}, &entity.NotificationDeliveryIntent{}} {
		for _, field := range []string{"SubjectType", "SubjectID", "SubjectName"} {
			if !db.Migrator().HasColumn(model, field) {
				t.Fatalf("provider quality migration omitted %T field %s", model, field)
			}
		}
	}
	for _, item := range []struct {
		model any
		name  string
	}{
		{&entity.CallAttempt{}, "idx_attempts_provider_time"},
		{&entity.CallAttempt{}, "idx_attempts_connection_time"},
		{&entity.CallAttempt{}, "idx_attempts_provider_model_time"},
		{&entity.ProviderQualityWindow{}, "idx_provider_quality_windows_identity"},
		{&entity.ProviderQualityWindow{}, "idx_provider_quality_windows_provider_end"},
		{&entity.ProviderQualityWindow{}, "idx_provider_quality_windows_policy_end"},
	} {
		if !db.Migrator().HasIndex(item.model, item.name) {
			t.Fatalf("provider quality migration omitted index %s", item.name)
		}
	}
	for _, item := range []struct {
		model any
		name  string
	}{
		{&entity.OperationalAlert{}, "ck_operational_alerts_kind"},
		{&entity.OperationalAlert{}, "ck_operational_alerts_subject"},
		{&entity.OperationalAlertOccurrence{}, "ck_alert_occurrences_source"},
		{&entity.OperationalAlertOccurrence{}, "ck_alert_occurrences_subject"},
		{&entity.Notification{}, "ck_notifications_subject"},
		{&entity.NotificationDeliveryIntent{}, "ck_notification_delivery_subject"},
		{&entity.ProviderQualityPolicy{}, "fk_provider_quality_policies_provider"},
		{&entity.ProviderQualityState{}, "fk_provider_quality_states_provider"},
	} {
		if !db.Migrator().HasConstraint(item.model, item.name) {
			t.Fatalf("provider quality migration omitted constraint %s", item.name)
		}
	}
}
