package database

import (
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
)

// V107 admits one recipient-only completion source without changing historical
// operational predicates. Byte-exact new enum branches reject collating aliases.
// These private structs are frozen; business entities never define startup DDL.
type offboardingAlertV107 struct {
	ID   string `gorm:"primaryKey;size:30"`
	Kind string `gorm:"size:40;not null;check:ck_operational_alerts_kind,kind IN ('system_job_failure','credential_verification_failure','provider_quality_degraded','route_unavailable') OR ((OCTET_LENGTH(kind) = 21 AND ASCII(SUBSTRING(kind,1,1)) = 111 AND ASCII(SUBSTRING(kind,2,1)) = 102 AND ASCII(SUBSTRING(kind,3,1)) = 102 AND ASCII(SUBSTRING(kind,4,1)) = 98 AND ASCII(SUBSTRING(kind,5,1)) = 111 AND ASCII(SUBSTRING(kind,6,1)) = 97 AND ASCII(SUBSTRING(kind,7,1)) = 114 AND ASCII(SUBSTRING(kind,8,1)) = 100 AND ASCII(SUBSTRING(kind,9,1)) = 105 AND ASCII(SUBSTRING(kind,10,1)) = 110 AND ASCII(SUBSTRING(kind,11,1)) = 103 AND ASCII(SUBSTRING(kind,12,1)) = 95 AND ASCII(SUBSTRING(kind,13,1)) = 99 AND ASCII(SUBSTRING(kind,14,1)) = 111 AND ASCII(SUBSTRING(kind,15,1)) = 109 AND ASCII(SUBSTRING(kind,16,1)) = 112 AND ASCII(SUBSTRING(kind,17,1)) = 108 AND ASCII(SUBSTRING(kind,18,1)) = 101 AND ASCII(SUBSTRING(kind,19,1)) = 116 AND ASCII(SUBSTRING(kind,20,1)) = 101 AND ASCII(SUBSTRING(kind,21,1)) = 100) AND (OCTET_LENGTH(severity) = 6 AND ASCII(SUBSTRING(severity,1,1)) = 109 AND ASCII(SUBSTRING(severity,2,1)) = 101 AND ASCII(SUBSTRING(severity,3,1)) = 100 AND ASCII(SUBSTRING(severity,4,1)) = 105 AND ASCII(SUBSTRING(severity,5,1)) = 117 AND ASCII(SUBSTRING(severity,6,1)) = 109) AND (OCTET_LENGTH(detail_code) = 9 AND ASCII(SUBSTRING(detail_code,1,1)) = 99 AND ASCII(SUBSTRING(detail_code,2,1)) = 111 AND ASCII(SUBSTRING(detail_code,3,1)) = 109 AND ASCII(SUBSTRING(detail_code,4,1)) = 112 AND ASCII(SUBSTRING(detail_code,5,1)) = 108 AND ASCII(SUBSTRING(detail_code,6,1)) = 101 AND ASCII(SUBSTRING(detail_code,7,1)) = 116 AND ASCII(SUBSTRING(detail_code,8,1)) = 101 AND ASCII(SUBSTRING(detail_code,9,1)) = 100) AND (OCTET_LENGTH(state) = 8 AND ASCII(SUBSTRING(state,1,1)) = 114 AND ASCII(SUBSTRING(state,2,1)) = 101 AND ASCII(SUBSTRING(state,3,1)) = 115 AND ASCII(SUBSTRING(state,4,1)) = 111 AND ASCII(SUBSTRING(state,5,1)) = 108 AND ASCII(SUBSTRING(state,6,1)) = 118 AND ASCII(SUBSTRING(state,7,1)) = 101 AND ASCII(SUBSTRING(state,8,1)) = 100) AND occurrence_count = 1 AND OCTET_LENGTH(subject_type) = 0 AND OCTET_LENGTH(subject_id) = 0 AND OCTET_LENGTH(subject_name) = 0 AND first_seen_at = last_seen_at)"`
}

func (offboardingAlertV107) TableName() string { return "operational_alerts" }

type offboardingOccurrenceV107 struct {
	ID         string `gorm:"primaryKey;size:30"`
	SourceType string `gorm:"size:32;not null;check:ck_alert_occurrences_source,source_type IN ('system_job','credential_verification','provider_quality_window','gateway_call') OR ((OCTET_LENGTH(source_type) = 21 AND ASCII(SUBSTRING(source_type,1,1)) = 111 AND ASCII(SUBSTRING(source_type,2,1)) = 102 AND ASCII(SUBSTRING(source_type,3,1)) = 102 AND ASCII(SUBSTRING(source_type,4,1)) = 98 AND ASCII(SUBSTRING(source_type,5,1)) = 111 AND ASCII(SUBSTRING(source_type,6,1)) = 97 AND ASCII(SUBSTRING(source_type,7,1)) = 114 AND ASCII(SUBSTRING(source_type,8,1)) = 100 AND ASCII(SUBSTRING(source_type,9,1)) = 105 AND ASCII(SUBSTRING(source_type,10,1)) = 110 AND ASCII(SUBSTRING(source_type,11,1)) = 103 AND ASCII(SUBSTRING(source_type,12,1)) = 95 AND ASCII(SUBSTRING(source_type,13,1)) = 99 AND ASCII(SUBSTRING(source_type,14,1)) = 111 AND ASCII(SUBSTRING(source_type,15,1)) = 109 AND ASCII(SUBSTRING(source_type,16,1)) = 112 AND ASCII(SUBSTRING(source_type,17,1)) = 108 AND ASCII(SUBSTRING(source_type,18,1)) = 101 AND ASCII(SUBSTRING(source_type,19,1)) = 116 AND ASCII(SUBSTRING(source_type,20,1)) = 101 AND ASCII(SUBSTRING(source_type,21,1)) = 100) AND (OCTET_LENGTH(detail_code) = 9 AND ASCII(SUBSTRING(detail_code,1,1)) = 99 AND ASCII(SUBSTRING(detail_code,2,1)) = 111 AND ASCII(SUBSTRING(detail_code,3,1)) = 109 AND ASCII(SUBSTRING(detail_code,4,1)) = 112 AND ASCII(SUBSTRING(detail_code,5,1)) = 108 AND ASCII(SUBSTRING(detail_code,6,1)) = 101 AND ASCII(SUBSTRING(detail_code,7,1)) = 116 AND ASCII(SUBSTRING(detail_code,8,1)) = 101 AND ASCII(SUBSTRING(detail_code,9,1)) = 100) AND OCTET_LENGTH(subject_type) = 0 AND OCTET_LENGTH(subject_id) = 0 AND OCTET_LENGTH(subject_name) = 0 AND OCTET_LENGTH(source_id) > 0)"`
}

func (offboardingOccurrenceV107) TableName() string { return "operational_alert_occurrences" }

type offboardingCaseV107 struct {
	ID string `gorm:"primaryKey;size:30"`
}

func (offboardingCaseV107) TableName() string { return "offboarding_cases" }

type offboardingUserV107 struct {
	ID string `gorm:"primaryKey;size:30"`
}

func (offboardingUserV107) TableName() string { return "users" }

type offboardingInboxV107 struct {
	ID string `gorm:"primaryKey;size:30"`
}

func (offboardingInboxV107) TableName() string { return "notifications" }

type offboardingNotificationRecipientV107 struct {
	CaseID             string                     `gorm:"primaryKey;size:30"`
	RecipientID        string                     `gorm:"primaryKey;size:30;uniqueIndex:idx_offboarding_notice_occurrence_recipient,priority:2"`
	OccurrenceID       string                     `gorm:"size:30;not null;uniqueIndex:idx_offboarding_notice_occurrence_recipient,priority:1"`
	NotificationID     string                     `gorm:"size:30;not null;uniqueIndex"`
	RecipientCreatedAt time.Time                  `gorm:"precision:6;not null;check:ck_offboarding_notice_birth,recipient_created_at > '0001-01-01 00:00:00'"`
	Case               *offboardingCaseV107       `gorm:"foreignKey:CaseID;references:ID;constraint:fk_offboarding_notice_case,OnUpdate:RESTRICT,OnDelete:RESTRICT"`
	Recipient          *offboardingUserV107       `gorm:"foreignKey:RecipientID;references:ID;constraint:fk_offboarding_notice_recipient,OnUpdate:RESTRICT,OnDelete:RESTRICT"`
	Occurrence         *offboardingOccurrenceV107 `gorm:"foreignKey:OccurrenceID;references:ID;constraint:fk_offboarding_notice_occurrence,OnUpdate:RESTRICT,OnDelete:RESTRICT"`
	Notification       *offboardingInboxV107      `gorm:"foreignKey:NotificationID;references:ID;constraint:fk_offboarding_notice_notification,OnUpdate:RESTRICT,OnDelete:RESTRICT"`
}

func (offboardingNotificationRecipientV107) TableName() string {
	return "offboarding_notification_recipients"
}

func offboardingNotificationMigration(db *gorm.DB) error {
	model := &offboardingNotificationRecipientV107{}
	stmt := &gorm.Statement{DB: db}
	if err := stmt.Parse(model); err != nil {
		return err
	}
	if db.Migrator().HasTable(model) {
		// Refuse incompatible retained data before any destructive CHECK replacement.
		var count int64
		if err := db.Model(model).Count(&count).Error; err != nil {
			return err
		}
		for _, field := range stmt.Schema.Fields {
			if field.DBName == "" {
				continue
			}
			if !db.Migrator().HasColumn(model, field.DBName) {
				if count != 0 {
					return fmt.Errorf("nonempty partial offboarding notification receipt")
				}
				if err := db.Migrator().AddColumn(model, field.Name); err != nil {
					return err
				}
			}
		}
		if err := validateOffboardingNotificationColumnsV107(db); err != nil {
			return err
		}
	}
	checks := []struct {
		model any
		name  string
	}{{&offboardingAlertV107{}, "ck_operational_alerts_kind"}, {&offboardingOccurrenceV107{}, "ck_alert_occurrences_source"}}
	for _, item := range checks {
		s := &gorm.Statement{DB: db}
		if err := s.Parse(item.model); err != nil {
			return err
		}
		predicate := s.Schema.ParseCheckConstraints()[item.name].Constraint
		var invalid int64
		// Frozen CHECK syntax is necessary for cross-column enum predicates; no input
		// contributes SQL. CHECK UNKNOWN is refused before partially applied DDL resumes.
		if err := db.Model(item.model).Where("NOT (" + predicate + ") OR (" + predicate + ") IS NULL").Count(&invalid).Error; err != nil {
			return err
		}
		if invalid != 0 {
			return fmt.Errorf("invalid retained offboarding notification source")
		}
	}
	if !db.Migrator().HasTable(model) {
		if err := db.Migrator().CreateTable(model); err != nil {
			return err
		}
	}
	if err := validateOffboardingNotificationColumnsV107(db); err != nil {
		return err
	}
	birth := stmt.Schema.ParseCheckConstraints()["ck_offboarding_notice_birth"].Constraint
	var invalid int64
	if err := db.Model(model).Where("NOT (" + birth + ") OR (" + birth + ") IS NULL").Count(&invalid).Error; err != nil {
		return err
	}
	if invalid != 0 {
		return fmt.Errorf("invalid retained offboarding recipient birth")
	}
	for _, index := range stmt.Schema.ParseIndexes() {
		if !db.Migrator().HasIndex(model, index.Name) {
			if err := db.Migrator().CreateIndex(model, index.Name); err != nil {
				return err
			}
		}
		columns, err := credentialAttemptStatisticsIndexColumns(db, stmt.Schema.Table, index.Name)
		if err != nil {
			return err
		}
		if len(columns) != len(index.Fields) {
			return fmt.Errorf("invalid offboarding notification index")
		}
		for i, c := range columns {
			if !c.ColumnName.Valid || c.ColumnName.String != index.Fields[i].DBName || !c.Position.Valid || c.Position.Int64 != int64(i+1) || !c.IsUnique.Valid || !c.IsUnique.Bool || c.PrefixLength.Valid {
				return fmt.Errorf("invalid offboarding notification index")
			}
		}
	}
	// Missing/weak named constraints are repaired on replay. FK recreation checks
	// retained references too; no reliance on MySQL transactional DDL rollback.
	for _, name := range []string{"ck_offboarding_notice_birth", "fk_offboarding_notice_case", "fk_offboarding_notice_recipient", "fk_offboarding_notice_occurrence", "fk_offboarding_notice_notification"} {
		if db.Migrator().HasConstraint(model, name) {
			if err := db.Migrator().DropConstraint(model, name); err != nil {
				return err
			}
		}
		if err := db.Migrator().CreateConstraint(model, name); err != nil {
			return err
		}
	}
	for _, item := range checks {
		if db.Migrator().HasConstraint(item.model, item.name) {
			if err := db.Migrator().DropConstraint(item.model, item.name); err != nil {
				return err
			}
		}
		if err := db.Migrator().CreateConstraint(item.model, item.name); err != nil {
			return err
		}
	}
	return nil
}

func validateOffboardingNotificationColumnsV107(db *gorm.DB) error {
	model := &offboardingNotificationRecipientV107{}
	stmt := &gorm.Statement{DB: db}
	if err := stmt.Parse(model); err != nil {
		return err
	}
	columns, err := db.Migrator().ColumnTypes(model)
	if err != nil {
		return err
	}
	if len(columns) != 5 {
		return fmt.Errorf("invalid offboarding notification columns")
	}
	seen := map[string]bool{}
	for _, c := range columns {
		field := stmt.Schema.FieldsByDBName[c.Name()]
		if field == nil || seen[c.Name()] {
			return fmt.Errorf("invalid offboarding notification columns")
		}
		seen[c.Name()] = true
		primary, known := c.PrimaryKey()
		if !known || primary != field.PrimaryKey {
			return fmt.Errorf("invalid offboarding notification primary key")
		}
		nullable, known := c.Nullable()
		if !known || nullable {
			return fmt.Errorf("invalid offboarding notification nullability")
		}
		if _, has := c.DefaultValue(); has {
			return fmt.Errorf("invalid offboarding notification default")
		}
		kind := strings.ToLower(c.DatabaseTypeName())
		if string(field.DataType) == "time" {
			precision, _, known := c.DecimalSize()
			if !known || precision != 6 || kind != "timestamp" && kind != "timestamptz" && kind != "datetime" {
				return fmt.Errorf("invalid offboarding notification birth column")
			}
		} else {
			width, known := c.Length()
			if !known || width != 30 || kind != "varchar" && kind != "character varying" {
				return fmt.Errorf("invalid offboarding notification identity column")
			}
		}
	}
	return nil
}
