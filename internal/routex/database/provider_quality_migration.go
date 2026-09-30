package database

import (
	"time"

	"gorm.io/gorm"
)

type callAttemptQualityV30 struct {
	ID                string `gorm:"primaryKey;size:64;index:idx_attempts_provider_time,priority:3;index:idx_attempts_connection_time,priority:3;index:idx_attempts_provider_model_time,priority:3"`
	ProviderID        string `gorm:"size:30;not null;default:'';index:idx_attempts_provider_time,priority:1"`
	ProviderName      string `gorm:"size:100;not null;default:''"`
	ProviderModelID   string `gorm:"size:30;not null;index:idx_attempts_provider_model_time,priority:1"`
	ConnectionID      string `gorm:"size:30;not null;index:idx_attempts_connection_time,priority:1"`
	ConnectionName    string `gorm:"size:100;not null;default:''"`
	UpstreamModelName string `gorm:"size:255;not null;default:''"`
	DurationMS        *int64
	CompletedAt       time.Time `gorm:"precision:6;not null;index:idx_attempts_provider_time,priority:2;index:idx_attempts_connection_time,priority:2;index:idx_attempts_provider_model_time,priority:2"`
}

func (callAttemptQualityV30) TableName() string { return "call_attempts" }

type providerQualityProviderV30 struct {
	ID string `gorm:"primaryKey;size:30"`
}

func (providerQualityProviderV30) TableName() string { return "providers" }

type providerQualityPolicyV30 struct {
	ProviderID        string                      `gorm:"primaryKey;size:30"`
	Enabled           bool                        `gorm:"not null"`
	WindowMinutes     int                         `gorm:"not null;check:ck_provider_quality_policy_window,window_minutes >= 5 AND window_minutes <= 1440"`
	MinimumAttempts   int                         `gorm:"not null;check:ck_provider_quality_policy_minimum,minimum_attempts >= 1 AND minimum_attempts <= 100000"`
	MinSuccessRateBPS int                         `gorm:"not null;check:ck_provider_quality_policy_success_rate,min_success_rate_bps >= 0 AND min_success_rate_bps <= 10000"`
	MaxP95DurationMS  *int64                      `gorm:"check:ck_provider_quality_policy_duration,max_p95_duration_ms IS NULL OR (max_p95_duration_ms >= 1 AND max_p95_duration_ms <= 3600000)"`
	ETag              string                      `gorm:"size:30;not null"`
	UpdatedBy         string                      `gorm:"size:30;not null"`
	UpdateReason      string                      `gorm:"size:500;not null"`
	UpdatedAt         time.Time                   `gorm:"precision:6;not null"`
	Provider          *providerQualityProviderV30 `gorm:"foreignKey:ProviderID;references:ID;constraint:OnUpdate:RESTRICT,OnDelete:RESTRICT"`
}

func (providerQualityPolicyV30) TableName() string { return "provider_quality_policies" }

type providerQualityWindowV30 struct {
	ID                         string    `gorm:"primaryKey;size:30;index:idx_provider_quality_windows_policy_end,priority:4"`
	ProviderID                 string    `gorm:"size:30;not null;uniqueIndex:idx_provider_quality_windows_identity,priority:1;index:idx_provider_quality_windows_provider_end,priority:1;index:idx_provider_quality_windows_policy_end,priority:1"`
	ProviderName               string    `gorm:"size:100;not null"`
	WindowStart                time.Time `gorm:"precision:6;not null"`
	WindowEnd                  time.Time `gorm:"precision:6;not null;uniqueIndex:idx_provider_quality_windows_identity,priority:2;index:idx_provider_quality_windows_provider_end,priority:2;index:idx_provider_quality_windows_policy_end,priority:3"`
	PolicyETag                 string    `gorm:"size:30;not null;index:idx_provider_quality_windows_policy_end,priority:2"`
	EligibleAttempts           int64     `gorm:"not null;check:ck_provider_quality_windows_eligible,eligible_attempts >= 0"`
	ExcludedAttempts           int64     `gorm:"not null;check:ck_provider_quality_windows_excluded,excluded_attempts >= 0"`
	UnknownAttributionAttempts int64     `gorm:"not null;check:ck_provider_quality_windows_unknown,unknown_attribution_attempts >= 0"`
	Successes                  int64     `gorm:"not null;check:ck_provider_quality_windows_successes,successes >= 0 AND successes <= eligible_attempts"`
	HTTP429                    int64     `gorm:"column:http_429;not null;check:ck_provider_quality_windows_429,http_429 >= 0 AND http_429 <= eligible_attempts"`
	HTTP5XX                    int64     `gorm:"column:http_5xx;not null;check:ck_provider_quality_windows_5xx,http_5xx >= 0 AND http_5xx <= eligible_attempts"`
	KnownDurationAttempts      int64     `gorm:"not null;check:ck_provider_quality_windows_duration_count,known_duration_attempts >= 0 AND known_duration_attempts <= eligible_attempts"`
	P95DurationMS              *int64    `gorm:"check:ck_provider_quality_windows_duration,p95_duration_ms IS NULL OR p95_duration_ms >= 0"`
	State                      string    `gorm:"size:20;not null;check:ck_provider_quality_windows_state,state IN ('healthy','degraded','insufficient_data')"`
	DetailCode                 string    `gorm:"size:40;not null"`
	CreatedAt                  time.Time `gorm:"precision:6;not null"`
}

func (providerQualityWindowV30) TableName() string { return "provider_quality_windows" }

type providerQualityStateV30 struct {
	ProviderID    string                      `gorm:"primaryKey;size:30"`
	LastWindowEnd *time.Time                  `gorm:"precision:6"`
	LastState     string                      `gorm:"size:20;not null;check:ck_provider_quality_states_state,last_state IN ('healthy','degraded','insufficient_data')"`
	LastAlertID   string                      `gorm:"size:30;not null"`
	UpdatedAt     time.Time                   `gorm:"precision:6;not null"`
	Provider      *providerQualityProviderV30 `gorm:"foreignKey:ProviderID;references:ID;constraint:OnUpdate:RESTRICT,OnDelete:RESTRICT"`
}

func (providerQualityStateV30) TableName() string { return "provider_quality_states" }

type operationalAlertQualityV30 struct {
	ID          string `gorm:"primaryKey;size:30"`
	Kind        string `gorm:"size:40;not null;check:ck_operational_alerts_kind,kind IN ('system_job_failure','credential_verification_failure','provider_quality_degraded','route_unavailable')"`
	SubjectType string `gorm:"size:20;not null;default:'';check:ck_operational_alerts_subject,subject_type IN ('','provider','model')"`
	SubjectID   string `gorm:"size:64;not null;default:''"`
	SubjectName string `gorm:"size:128;not null;default:''"`
}

func (operationalAlertQualityV30) TableName() string { return "operational_alerts" }

type operationalAlertOccurrenceQualityV30 struct {
	ID          string `gorm:"primaryKey;size:30"`
	SourceType  string `gorm:"size:32;not null;check:ck_alert_occurrences_source,source_type IN ('system_job','credential_verification','provider_quality_window','gateway_call')"`
	SubjectType string `gorm:"size:20;not null;default:'';check:ck_alert_occurrences_subject,subject_type IN ('','provider','model')"`
	SubjectID   string `gorm:"size:64;not null;default:''"`
	SubjectName string `gorm:"size:128;not null;default:''"`
}

func (operationalAlertOccurrenceQualityV30) TableName() string {
	return "operational_alert_occurrences"
}

type notificationQualityV30 struct {
	ID          string `gorm:"primaryKey;size:30"`
	SubjectType string `gorm:"size:20;not null;default:'';check:ck_notifications_subject,subject_type IN ('','provider','model')"`
	SubjectID   string `gorm:"size:64;not null;default:''"`
	SubjectName string `gorm:"size:128;not null;default:''"`
}

func (notificationQualityV30) TableName() string { return "notifications" }

type notificationDeliveryQualityV30 struct {
	ID          string `gorm:"primaryKey;size:30"`
	SubjectType string `gorm:"size:20;not null;default:'';check:ck_notification_delivery_subject,subject_type IN ('','provider','model')"`
	SubjectID   string `gorm:"size:64;not null;default:''"`
	SubjectName string `gorm:"size:128;not null;default:''"`
}

func (notificationDeliveryQualityV30) TableName() string {
	return "notification_delivery_intents"
}

// Version 30 adds immutable provider-attempt facts, provider quality policy and
// evaluation state, and subject snapshots for operational notifications. New
// policy and quality tables intentionally have no seed rows.
func providerQualityMigration(db *gorm.DB) error {
	attempt := &callAttemptQualityV30{}
	for _, field := range []string{"ProviderID", "ProviderName", "ConnectionName", "UpstreamModelName", "DurationMS"} {
		if !db.Migrator().HasColumn(attempt, field) {
			if err := db.Migrator().AddColumn(attempt, field); err != nil {
				return err
			}
		}
	}
	for _, index := range []string{"idx_attempts_provider_time", "idx_attempts_connection_time", "idx_attempts_provider_model_time"} {
		if !db.Migrator().HasIndex(attempt, index) {
			if err := db.Migrator().CreateIndex(attempt, index); err != nil {
				return err
			}
		}
	}

	for _, model := range []any{
		&operationalAlertQualityV30{},
		&operationalAlertOccurrenceQualityV30{},
		&notificationQualityV30{},
		&notificationDeliveryQualityV30{},
	} {
		for _, field := range []string{"SubjectType", "SubjectID", "SubjectName"} {
			if !db.Migrator().HasColumn(model, field) {
				if err := db.Migrator().AddColumn(model, field); err != nil {
					return err
				}
			}
		}
	}

	if err := migrateTables(db, &providerQualityPolicyV30{}, &providerQualityWindowV30{}, &providerQualityStateV30{}); err != nil {
		return err
	}
	if err := replaceV30CheckConstraint(db, &operationalAlertQualityV30{}, "ck_operational_alerts_kind"); err != nil {
		return err
	}
	if err := replaceV30CheckConstraint(db, &operationalAlertOccurrenceQualityV30{}, "ck_alert_occurrences_source"); err != nil {
		return err
	}
	for _, item := range []struct {
		model any
		name  string
	}{
		{&operationalAlertQualityV30{}, "ck_operational_alerts_subject"},
		{&operationalAlertOccurrenceQualityV30{}, "ck_alert_occurrences_subject"},
		{&notificationQualityV30{}, "ck_notifications_subject"},
		{&notificationDeliveryQualityV30{}, "ck_notification_delivery_subject"},
	} {
		if !db.Migrator().HasConstraint(item.model, item.name) {
			if err := db.Migrator().CreateConstraint(item.model, item.name); err != nil {
				return err
			}
		}
	}
	return nil
}

func replaceV30CheckConstraint(db *gorm.DB, model any, name string) error {
	if db.Migrator().HasConstraint(model, name) {
		if err := db.Migrator().DropConstraint(model, name); err != nil {
			return err
		}
	}
	return db.Migrator().CreateConstraint(model, name)
}
