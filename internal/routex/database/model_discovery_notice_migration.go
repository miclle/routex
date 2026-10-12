package database

import (
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
)

// V108 freezes a successful discovery source and extends only closed notification
// envelopes. Cross-column byte-exact CHECK expressions are the documented GORM
// tag exception; all DDL uses Migrator and no runtime input contributes SQL.
type modelDiscoveryAlertV108 struct {
	ID                      string `gorm:"primaryKey;size:30"`
	FirstRecordedModelCount int    `gorm:"not null;default:0"`
	Kind                    string `gorm:"size:40;not null;check:ck_operational_alerts_kind,((kind IN ('system_job_failure','credential_verification_failure','provider_quality_degraded','route_unavailable') OR ((OCTET_LENGTH(kind) = 21 AND ASCII(SUBSTRING(kind,1,1)) = 111 AND ASCII(SUBSTRING(kind,2,1)) = 102 AND ASCII(SUBSTRING(kind,3,1)) = 102 AND ASCII(SUBSTRING(kind,4,1)) = 98 AND ASCII(SUBSTRING(kind,5,1)) = 111 AND ASCII(SUBSTRING(kind,6,1)) = 97 AND ASCII(SUBSTRING(kind,7,1)) = 114 AND ASCII(SUBSTRING(kind,8,1)) = 100 AND ASCII(SUBSTRING(kind,9,1)) = 105 AND ASCII(SUBSTRING(kind,10,1)) = 110 AND ASCII(SUBSTRING(kind,11,1)) = 103 AND ASCII(SUBSTRING(kind,12,1)) = 95 AND ASCII(SUBSTRING(kind,13,1)) = 99 AND ASCII(SUBSTRING(kind,14,1)) = 111 AND ASCII(SUBSTRING(kind,15,1)) = 109 AND ASCII(SUBSTRING(kind,16,1)) = 112 AND ASCII(SUBSTRING(kind,17,1)) = 108 AND ASCII(SUBSTRING(kind,18,1)) = 101 AND ASCII(SUBSTRING(kind,19,1)) = 116 AND ASCII(SUBSTRING(kind,20,1)) = 101 AND ASCII(SUBSTRING(kind,21,1)) = 100) AND (OCTET_LENGTH(severity) = 6 AND ASCII(SUBSTRING(severity,1,1)) = 109 AND ASCII(SUBSTRING(severity,2,1)) = 101 AND ASCII(SUBSTRING(severity,3,1)) = 100 AND ASCII(SUBSTRING(severity,4,1)) = 105 AND ASCII(SUBSTRING(severity,5,1)) = 117 AND ASCII(SUBSTRING(severity,6,1)) = 109) AND (OCTET_LENGTH(detail_code) = 9 AND ASCII(SUBSTRING(detail_code,1,1)) = 99 AND ASCII(SUBSTRING(detail_code,2,1)) = 111 AND ASCII(SUBSTRING(detail_code,3,1)) = 109 AND ASCII(SUBSTRING(detail_code,4,1)) = 112 AND ASCII(SUBSTRING(detail_code,5,1)) = 108 AND ASCII(SUBSTRING(detail_code,6,1)) = 101 AND ASCII(SUBSTRING(detail_code,7,1)) = 116 AND ASCII(SUBSTRING(detail_code,8,1)) = 101 AND ASCII(SUBSTRING(detail_code,9,1)) = 100) AND (OCTET_LENGTH(state) = 8 AND ASCII(SUBSTRING(state,1,1)) = 114 AND ASCII(SUBSTRING(state,2,1)) = 101 AND ASCII(SUBSTRING(state,3,1)) = 115 AND ASCII(SUBSTRING(state,4,1)) = 111 AND ASCII(SUBSTRING(state,5,1)) = 108 AND ASCII(SUBSTRING(state,6,1)) = 118 AND ASCII(SUBSTRING(state,7,1)) = 101 AND ASCII(SUBSTRING(state,8,1)) = 100) AND occurrence_count = 1 AND OCTET_LENGTH(subject_type) = 0 AND OCTET_LENGTH(subject_id) = 0 AND OCTET_LENGTH(subject_name) = 0 AND first_seen_at = last_seen_at)) AND first_recorded_model_count = 0 AND severity IN ('high','medium')) OR ((OCTET_LENGTH(kind) = 21 AND ASCII(SUBSTRING(kind,1,1)) = 109 AND ASCII(SUBSTRING(kind,2,1)) = 111 AND ASCII(SUBSTRING(kind,3,1)) = 100 AND ASCII(SUBSTRING(kind,4,1)) = 101 AND ASCII(SUBSTRING(kind,5,1)) = 108 AND ASCII(SUBSTRING(kind,6,1)) = 115 AND ASCII(SUBSTRING(kind,7,1)) = 95 AND ASCII(SUBSTRING(kind,8,1)) = 102 AND ASCII(SUBSTRING(kind,9,1)) = 105 AND ASCII(SUBSTRING(kind,10,1)) = 114 AND ASCII(SUBSTRING(kind,11,1)) = 115 AND ASCII(SUBSTRING(kind,12,1)) = 116 AND ASCII(SUBSTRING(kind,13,1)) = 95 AND ASCII(SUBSTRING(kind,14,1)) = 114 AND ASCII(SUBSTRING(kind,15,1)) = 101 AND ASCII(SUBSTRING(kind,16,1)) = 99 AND ASCII(SUBSTRING(kind,17,1)) = 111 AND ASCII(SUBSTRING(kind,18,1)) = 114 AND ASCII(SUBSTRING(kind,19,1)) = 100 AND ASCII(SUBSTRING(kind,20,1)) = 101 AND ASCII(SUBSTRING(kind,21,1)) = 100) AND (OCTET_LENGTH(severity) = 3 AND ASCII(SUBSTRING(severity,1,1)) = 108 AND ASCII(SUBSTRING(severity,2,1)) = 111 AND ASCII(SUBSTRING(severity,3,1)) = 119) AND (OCTET_LENGTH(detail_code) = 14 AND ASCII(SUBSTRING(detail_code,1,1)) = 102 AND ASCII(SUBSTRING(detail_code,2,1)) = 105 AND ASCII(SUBSTRING(detail_code,3,1)) = 114 AND ASCII(SUBSTRING(detail_code,4,1)) = 115 AND ASCII(SUBSTRING(detail_code,5,1)) = 116 AND ASCII(SUBSTRING(detail_code,6,1)) = 95 AND ASCII(SUBSTRING(detail_code,7,1)) = 114 AND ASCII(SUBSTRING(detail_code,8,1)) = 101 AND ASCII(SUBSTRING(detail_code,9,1)) = 99 AND ASCII(SUBSTRING(detail_code,10,1)) = 111 AND ASCII(SUBSTRING(detail_code,11,1)) = 114 AND ASCII(SUBSTRING(detail_code,12,1)) = 100 AND ASCII(SUBSTRING(detail_code,13,1)) = 101 AND ASCII(SUBSTRING(detail_code,14,1)) = 100) AND first_recorded_model_count BETWEEN 1 AND 2000 AND (OCTET_LENGTH(subject_type) = 8 AND ASCII(SUBSTRING(subject_type,1,1)) = 112 AND ASCII(SUBSTRING(subject_type,2,1)) = 114 AND ASCII(SUBSTRING(subject_type,3,1)) = 111 AND ASCII(SUBSTRING(subject_type,4,1)) = 118 AND ASCII(SUBSTRING(subject_type,5,1)) = 105 AND ASCII(SUBSTRING(subject_type,6,1)) = 100 AND ASCII(SUBSTRING(subject_type,7,1)) = 101 AND ASCII(SUBSTRING(subject_type,8,1)) = 114) AND OCTET_LENGTH(subject_id) BETWEEN 1 AND 30 AND OCTET_LENGTH(subject_name) BETWEEN 1 AND 400)"`
}

func (modelDiscoveryAlertV108) TableName() string { return "operational_alerts" }

type modelDiscoveryOccurrenceV108 struct {
	ID                      string `gorm:"primaryKey;size:30"`
	FirstRecordedModelCount int    `gorm:"not null;default:0"`
	SourceType              string `gorm:"size:32;not null;check:ck_alert_occurrences_source,((source_type IN ('system_job','credential_verification','provider_quality_window','gateway_call') OR ((OCTET_LENGTH(source_type) = 21 AND ASCII(SUBSTRING(source_type,1,1)) = 111 AND ASCII(SUBSTRING(source_type,2,1)) = 102 AND ASCII(SUBSTRING(source_type,3,1)) = 102 AND ASCII(SUBSTRING(source_type,4,1)) = 98 AND ASCII(SUBSTRING(source_type,5,1)) = 111 AND ASCII(SUBSTRING(source_type,6,1)) = 97 AND ASCII(SUBSTRING(source_type,7,1)) = 114 AND ASCII(SUBSTRING(source_type,8,1)) = 100 AND ASCII(SUBSTRING(source_type,9,1)) = 105 AND ASCII(SUBSTRING(source_type,10,1)) = 110 AND ASCII(SUBSTRING(source_type,11,1)) = 103 AND ASCII(SUBSTRING(source_type,12,1)) = 95 AND ASCII(SUBSTRING(source_type,13,1)) = 99 AND ASCII(SUBSTRING(source_type,14,1)) = 111 AND ASCII(SUBSTRING(source_type,15,1)) = 109 AND ASCII(SUBSTRING(source_type,16,1)) = 112 AND ASCII(SUBSTRING(source_type,17,1)) = 108 AND ASCII(SUBSTRING(source_type,18,1)) = 101 AND ASCII(SUBSTRING(source_type,19,1)) = 116 AND ASCII(SUBSTRING(source_type,20,1)) = 101 AND ASCII(SUBSTRING(source_type,21,1)) = 100) AND (OCTET_LENGTH(detail_code) = 9 AND ASCII(SUBSTRING(detail_code,1,1)) = 99 AND ASCII(SUBSTRING(detail_code,2,1)) = 111 AND ASCII(SUBSTRING(detail_code,3,1)) = 109 AND ASCII(SUBSTRING(detail_code,4,1)) = 112 AND ASCII(SUBSTRING(detail_code,5,1)) = 108 AND ASCII(SUBSTRING(detail_code,6,1)) = 101 AND ASCII(SUBSTRING(detail_code,7,1)) = 116 AND ASCII(SUBSTRING(detail_code,8,1)) = 101 AND ASCII(SUBSTRING(detail_code,9,1)) = 100) AND OCTET_LENGTH(subject_type) = 0 AND OCTET_LENGTH(subject_id) = 0 AND OCTET_LENGTH(subject_name) = 0 AND OCTET_LENGTH(source_id) > 0)) AND first_recorded_model_count = 0) OR ((OCTET_LENGTH(source_type) = 15 AND ASCII(SUBSTRING(source_type,1,1)) = 109 AND ASCII(SUBSTRING(source_type,2,1)) = 111 AND ASCII(SUBSTRING(source_type,3,1)) = 100 AND ASCII(SUBSTRING(source_type,4,1)) = 101 AND ASCII(SUBSTRING(source_type,5,1)) = 108 AND ASCII(SUBSTRING(source_type,6,1)) = 95 AND ASCII(SUBSTRING(source_type,7,1)) = 100 AND ASCII(SUBSTRING(source_type,8,1)) = 105 AND ASCII(SUBSTRING(source_type,9,1)) = 115 AND ASCII(SUBSTRING(source_type,10,1)) = 99 AND ASCII(SUBSTRING(source_type,11,1)) = 111 AND ASCII(SUBSTRING(source_type,12,1)) = 118 AND ASCII(SUBSTRING(source_type,13,1)) = 101 AND ASCII(SUBSTRING(source_type,14,1)) = 114 AND ASCII(SUBSTRING(source_type,15,1)) = 121) AND (OCTET_LENGTH(detail_code) = 14 AND ASCII(SUBSTRING(detail_code,1,1)) = 102 AND ASCII(SUBSTRING(detail_code,2,1)) = 105 AND ASCII(SUBSTRING(detail_code,3,1)) = 114 AND ASCII(SUBSTRING(detail_code,4,1)) = 115 AND ASCII(SUBSTRING(detail_code,5,1)) = 116 AND ASCII(SUBSTRING(detail_code,6,1)) = 95 AND ASCII(SUBSTRING(detail_code,7,1)) = 114 AND ASCII(SUBSTRING(detail_code,8,1)) = 101 AND ASCII(SUBSTRING(detail_code,9,1)) = 99 AND ASCII(SUBSTRING(detail_code,10,1)) = 111 AND ASCII(SUBSTRING(detail_code,11,1)) = 114 AND ASCII(SUBSTRING(detail_code,12,1)) = 100 AND ASCII(SUBSTRING(detail_code,13,1)) = 101 AND ASCII(SUBSTRING(detail_code,14,1)) = 100) AND first_recorded_model_count BETWEEN 1 AND 2000 AND (OCTET_LENGTH(subject_type) = 8 AND ASCII(SUBSTRING(subject_type,1,1)) = 112 AND ASCII(SUBSTRING(subject_type,2,1)) = 114 AND ASCII(SUBSTRING(subject_type,3,1)) = 111 AND ASCII(SUBSTRING(subject_type,4,1)) = 118 AND ASCII(SUBSTRING(subject_type,5,1)) = 105 AND ASCII(SUBSTRING(subject_type,6,1)) = 100 AND ASCII(SUBSTRING(subject_type,7,1)) = 101 AND ASCII(SUBSTRING(subject_type,8,1)) = 114) AND OCTET_LENGTH(subject_id) BETWEEN 1 AND 30 AND OCTET_LENGTH(subject_name) BETWEEN 1 AND 400 AND OCTET_LENGTH(source_id) = 30)"`
}

func (modelDiscoveryOccurrenceV108) TableName() string { return "operational_alert_occurrences" }

type modelDiscoveryInboxV108 struct {
	ID                      string `gorm:"primaryKey;size:30"`
	FirstRecordedModelCount int    `gorm:"not null;default:0"`
	Kind                    string `gorm:"size:40;not null;check:ck_notifications_discovery,((OCTET_LENGTH(kind) = 21 AND ASCII(SUBSTRING(kind,1,1)) = 109 AND ASCII(SUBSTRING(kind,2,1)) = 111 AND ASCII(SUBSTRING(kind,3,1)) = 100 AND ASCII(SUBSTRING(kind,4,1)) = 101 AND ASCII(SUBSTRING(kind,5,1)) = 108 AND ASCII(SUBSTRING(kind,6,1)) = 115 AND ASCII(SUBSTRING(kind,7,1)) = 95 AND ASCII(SUBSTRING(kind,8,1)) = 102 AND ASCII(SUBSTRING(kind,9,1)) = 105 AND ASCII(SUBSTRING(kind,10,1)) = 114 AND ASCII(SUBSTRING(kind,11,1)) = 115 AND ASCII(SUBSTRING(kind,12,1)) = 116 AND ASCII(SUBSTRING(kind,13,1)) = 95 AND ASCII(SUBSTRING(kind,14,1)) = 114 AND ASCII(SUBSTRING(kind,15,1)) = 101 AND ASCII(SUBSTRING(kind,16,1)) = 99 AND ASCII(SUBSTRING(kind,17,1)) = 111 AND ASCII(SUBSTRING(kind,18,1)) = 114 AND ASCII(SUBSTRING(kind,19,1)) = 100 AND ASCII(SUBSTRING(kind,20,1)) = 101 AND ASCII(SUBSTRING(kind,21,1)) = 100) AND (OCTET_LENGTH(severity) = 3 AND ASCII(SUBSTRING(severity,1,1)) = 108 AND ASCII(SUBSTRING(severity,2,1)) = 111 AND ASCII(SUBSTRING(severity,3,1)) = 119) AND (OCTET_LENGTH(detail_code) = 14 AND ASCII(SUBSTRING(detail_code,1,1)) = 102 AND ASCII(SUBSTRING(detail_code,2,1)) = 105 AND ASCII(SUBSTRING(detail_code,3,1)) = 114 AND ASCII(SUBSTRING(detail_code,4,1)) = 115 AND ASCII(SUBSTRING(detail_code,5,1)) = 116 AND ASCII(SUBSTRING(detail_code,6,1)) = 95 AND ASCII(SUBSTRING(detail_code,7,1)) = 114 AND ASCII(SUBSTRING(detail_code,8,1)) = 101 AND ASCII(SUBSTRING(detail_code,9,1)) = 99 AND ASCII(SUBSTRING(detail_code,10,1)) = 111 AND ASCII(SUBSTRING(detail_code,11,1)) = 114 AND ASCII(SUBSTRING(detail_code,12,1)) = 100 AND ASCII(SUBSTRING(detail_code,13,1)) = 101 AND ASCII(SUBSTRING(detail_code,14,1)) = 100) AND first_recorded_model_count BETWEEN 1 AND 2000 AND (OCTET_LENGTH(subject_type) = 8 AND ASCII(SUBSTRING(subject_type,1,1)) = 112 AND ASCII(SUBSTRING(subject_type,2,1)) = 114 AND ASCII(SUBSTRING(subject_type,3,1)) = 111 AND ASCII(SUBSTRING(subject_type,4,1)) = 118 AND ASCII(SUBSTRING(subject_type,5,1)) = 105 AND ASCII(SUBSTRING(subject_type,6,1)) = 100 AND ASCII(SUBSTRING(subject_type,7,1)) = 101 AND ASCII(SUBSTRING(subject_type,8,1)) = 114) AND OCTET_LENGTH(subject_id) BETWEEN 1 AND 30 AND OCTET_LENGTH(subject_name) BETWEEN 1 AND 400) OR (NOT (OCTET_LENGTH(kind) = 21 AND ASCII(SUBSTRING(kind,1,1)) = 109 AND ASCII(SUBSTRING(kind,2,1)) = 111 AND ASCII(SUBSTRING(kind,3,1)) = 100 AND ASCII(SUBSTRING(kind,4,1)) = 101 AND ASCII(SUBSTRING(kind,5,1)) = 108 AND ASCII(SUBSTRING(kind,6,1)) = 115 AND ASCII(SUBSTRING(kind,7,1)) = 95 AND ASCII(SUBSTRING(kind,8,1)) = 102 AND ASCII(SUBSTRING(kind,9,1)) = 105 AND ASCII(SUBSTRING(kind,10,1)) = 114 AND ASCII(SUBSTRING(kind,11,1)) = 115 AND ASCII(SUBSTRING(kind,12,1)) = 116 AND ASCII(SUBSTRING(kind,13,1)) = 95 AND ASCII(SUBSTRING(kind,14,1)) = 114 AND ASCII(SUBSTRING(kind,15,1)) = 101 AND ASCII(SUBSTRING(kind,16,1)) = 99 AND ASCII(SUBSTRING(kind,17,1)) = 111 AND ASCII(SUBSTRING(kind,18,1)) = 114 AND ASCII(SUBSTRING(kind,19,1)) = 100 AND ASCII(SUBSTRING(kind,20,1)) = 101 AND ASCII(SUBSTRING(kind,21,1)) = 100) AND first_recorded_model_count = 0)"`
}

func (modelDiscoveryInboxV108) TableName() string { return "notifications" }

type modelDiscoverySeverityV108 struct {
	Severity string `gorm:"size:10;not null;check:ck_operational_alerts_severity,severity IN ('high','medium') OR (OCTET_LENGTH(severity) = 3 AND ASCII(SUBSTRING(severity,1,1)) = 108 AND ASCII(SUBSTRING(severity,2,1)) = 111 AND ASCII(SUBSTRING(severity,3,1)) = 119)"`
}

func (modelDiscoverySeverityV108) TableName() string { return "operational_alerts" }

type modelDiscoveryAuditV108 struct {
	ID string `gorm:"primaryKey;size:30"`
}

func (modelDiscoveryAuditV108) TableName() string { return "audit_events" }

type modelDiscoveryNoticeV108 struct {
	AuditID      string                   `gorm:"primaryKey;size:30"`
	ConnectionID string                   `gorm:"size:30;not null;check:ck_model_discovery_connection,OCTET_LENGTH(connection_id) BETWEEN 1 AND 30"`
	ProviderID   string                   `gorm:"size:30;not null;check:ck_model_discovery_provider,OCTET_LENGTH(provider_id) BETWEEN 1 AND 30"`
	ProviderName string                   `gorm:"size:100;not null;check:ck_model_discovery_name,OCTET_LENGTH(provider_name) BETWEEN 1 AND 400"`
	ModelCount   int                      `gorm:"not null;check:ck_model_discovery_count,model_count BETWEEN 1 AND 2000"`
	RecordedAt   time.Time                `gorm:"precision:6;not null;check:ck_model_discovery_time,recorded_at > '0001-01-01 00:00:00'"`
	Audit        *modelDiscoveryAuditV108 `gorm:"foreignKey:AuditID;references:ID;constraint:fk_model_discovery_audit,OnUpdate:RESTRICT,OnDelete:RESTRICT"`
}

func (modelDiscoveryNoticeV108) TableName() string { return "model_discovery_notices" }

type modelDiscoveryDeliveryV108 struct {
	Kind string `gorm:"size:40;not null;check:ck_notification_delivery_no_discovery,kind NOT IN ('models_first_recorded')"`
}

func (modelDiscoveryDeliveryV108) TableName() string { return "notification_delivery_intents" }

func modelDiscoveryNoticeMigration(db *gorm.DB) error {
	source := &modelDiscoveryNoticeV108{}
	if db.Migrator().HasTable(source) {
		stmt := &gorm.Statement{DB: db}
		if err := stmt.Parse(source); err != nil {
			return err
		}
		var count int64
		if err := db.Model(source).Count(&count).Error; err != nil {
			return err
		}
		for _, name := range stmt.Schema.DBNames {
			if !db.Migrator().HasColumn(source, name) {
				if count != 0 {
					return fmt.Errorf("nonempty partial model discovery source")
				}
				if err := db.Migrator().AddColumn(source, stmt.Schema.FieldsByDBName[name].Name); err != nil {
					return err
				}
			}
		}
		if err := validateModelDiscoveryColumnsV108(db); err != nil {
			return err
		}
	}
	if err := migrateTables(db, &modelDiscoveryNoticeV108{}); err != nil {
		return err
	}
	if err := validateModelDiscoveryColumnsV108(db); err != nil {
		return err
	}
	statement := &gorm.Statement{DB: db}
	if err := statement.Parse(source); err != nil {
		return err
	}
	for _, check := range statement.Schema.ParseCheckConstraints() {
		var count int64
		if err := db.Model(source).Where("NOT (" + check.Constraint + ") OR (" + check.Constraint + ") IS NULL").Count(&count).Error; err != nil {
			return err
		}
		if count != 0 {
			return fmt.Errorf("invalid retained model discovery source")
		}
	}
	for name := range statement.Schema.ParseCheckConstraints() {
		if err := replaceV30CheckConstraint(db, source, name); err != nil {
			return err
		}
	}
	if db.Migrator().HasConstraint(source, "fk_model_discovery_audit") {
		if err := db.Migrator().DropConstraint(source, "fk_model_discovery_audit"); err != nil {
			return err
		}
	}
	if err := db.Migrator().CreateConstraint(source, "fk_model_discovery_audit"); err != nil {
		return err
	}
	for _, model := range []any{&modelDiscoveryAlertV108{}, &modelDiscoveryOccurrenceV108{}, &modelDiscoveryInboxV108{}} {
		if !db.Migrator().HasColumn(model, "FirstRecordedModelCount") {
			if err := db.Migrator().AddColumn(model, "FirstRecordedModelCount"); err != nil {
				return err
			}
		}
		if err := validateModelDiscoveryCountColumnV108(db, model); err != nil {
			return err
		}
	}
	checks := []struct {
		model any
		name  string
	}{
		{&modelDiscoveryAlertV108{}, "ck_operational_alerts_kind"},
		{&modelDiscoveryOccurrenceV108{}, "ck_alert_occurrences_source"},
		{&modelDiscoveryInboxV108{}, "ck_notifications_discovery"},
		{&modelDiscoverySeverityV108{}, "ck_operational_alerts_severity"},
		{&modelDiscoveryDeliveryV108{}, "ck_notification_delivery_no_discovery"},
	}
	// Refuse incompatible retained rows before replacing any existing CHECK.
	for _, item := range checks {
		stmt := &gorm.Statement{DB: db}
		if err := stmt.Parse(item.model); err != nil {
			return err
		}
		predicate := stmt.Schema.ParseCheckConstraints()[item.name].Constraint
		var count int64
		if err := db.Model(item.model).Where("NOT (" + predicate + ") OR (" + predicate + ") IS NULL").Count(&count).Error; err != nil {
			return err
		}
		if count != 0 {
			return fmt.Errorf("invalid retained model discovery envelope")
		}
	}
	for _, item := range checks {
		if err := replaceV30CheckConstraint(db, item.model, item.name); err != nil {
			return err
		}
	}
	return nil
}

func validateModelDiscoveryColumnsV108(db *gorm.DB) error {
	model := &modelDiscoveryNoticeV108{}
	stmt := &gorm.Statement{DB: db}
	if err := stmt.Parse(model); err != nil {
		return err
	}
	columns, err := db.Migrator().ColumnTypes(model)
	if err != nil {
		return err
	}
	if len(columns) != 6 {
		return fmt.Errorf("invalid model discovery source columns")
	}
	seen := map[string]bool{}
	for _, c := range columns {
		f := stmt.Schema.FieldsByDBName[c.Name()]
		if f == nil || seen[c.Name()] {
			return fmt.Errorf("invalid model discovery source column")
		}
		seen[c.Name()] = true
		primary, known := c.PrimaryKey()
		if !known || primary != f.PrimaryKey {
			return fmt.Errorf("invalid model discovery primary key")
		}
		nullable, known := c.Nullable()
		if !known || nullable {
			return fmt.Errorf("invalid model discovery nullability")
		}
		if _, has := c.DefaultValue(); has {
			return fmt.Errorf("invalid model discovery default")
		}
		fullType, fullTypeKnown := c.ColumnType()
		if !fullTypeKnown || strings.Contains(strings.ToLower(fullType), "unsigned") {
			return fmt.Errorf("invalid model discovery source signed metadata")
		}
		kind := strings.ToLower(c.DatabaseTypeName())
		switch string(f.DataType) {
		case "string":
			width, known := c.Length()
			if !known || width != int64(f.Size) || kind != "varchar" && kind != "character varying" {
				return fmt.Errorf("invalid model discovery text column")
			}
		case "time":
			precision, _, known := c.DecimalSize()
			if !known || precision != 6 || kind != "timestamp" && kind != "timestamptz" && kind != "datetime" {
				return fmt.Errorf("invalid model discovery time column")
			}
		case "int":
			if kind != "bigint" && kind != "int8" {
				return fmt.Errorf("invalid model discovery count column")
			}
		default:
			return fmt.Errorf("invalid model discovery column type")
		}
	}
	return nil
}

// An existing column may be left by interrupted DDL. Even an empty table must
// retain the frozen integer, non-null, explicit-zero baseline.
func validateModelDiscoveryCountColumnV108(db *gorm.DB, model any) error {
	columns, err := db.Migrator().ColumnTypes(model)
	if err != nil {
		return err
	}
	found := false
	for _, column := range columns {
		if column.Name() != "first_recorded_model_count" {
			continue
		}
		if found {
			return fmt.Errorf("duplicate model discovery envelope count")
		}
		found = true
		nullable, nullKnown := column.Nullable()
		value, defaulted := column.DefaultValue()
		fullType, fullTypeKnown := column.ColumnType()
		if !fullTypeKnown || strings.Contains(strings.ToLower(fullType), "unsigned") {
			return fmt.Errorf("invalid model discovery envelope signed metadata")
		}
		kind := strings.ToLower(column.DatabaseTypeName())
		if !nullKnown || nullable || (kind != "bigint" && kind != "int8") || !defaulted || (value != "0" && value != "'0'" && value != "0::bigint" && value != "'0'::bigint") {
			return fmt.Errorf("invalid model discovery envelope count column")
		}
	}
	if !found {
		return fmt.Errorf("missing model discovery envelope count column")
	}
	return nil
}
