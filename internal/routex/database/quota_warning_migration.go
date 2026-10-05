package database

import (
	"time"

	"gorm.io/gorm"
)

// quotaWarningObservationV59 records a current known settled level, not a crossing
// or an admission guarantee. Personal-only warnings retain their original facts.
type quotaWarningObservationV59 struct {
	ID                  string    `gorm:"primaryKey;size:30"`
	OwnerID             string    `gorm:"size:30;not null;uniqueIndex:uq_quota_warning,priority:1;check:ck_quota_warning_owner,CHAR_LENGTH(owner_id) BETWEEN 1 AND 30"`
	Dimension           string    `gorm:"size:20;not null;uniqueIndex:uq_quota_warning,priority:2;check:ck_quota_warning_dimension,dimension IN ('tokens','money')"`
	MonthStart          time.Time `gorm:"precision:6;not null;uniqueIndex:uq_quota_warning,priority:3;check:ck_quota_warning_calendar,month_end > month_start AND as_of >= month_start AND as_of < month_end AND coverage_start <= as_of AND resource_created_at <= as_of"`
	MonthEnd            time.Time `gorm:"precision:6;not null"`
	PolicyRevision      string    `gorm:"size:64;not null;uniqueIndex:uq_quota_warning,priority:4"`
	Currency            string    `gorm:"size:3;not null;uniqueIndex:uq_quota_warning,priority:5;check:ck_quota_warning_currency,(dimension = 'tokens' AND currency = '') OR (dimension = 'money' AND CHAR_LENGTH(currency) = 3)"`
	Level               string    `gorm:"size:10;not null;uniqueIndex:uq_quota_warning,priority:6;check:ck_quota_warning_level,(level = 'near' AND threshold = 80) OR (level = 'critical' AND threshold = 90)"`
	Threshold           int       `gorm:"not null"`
	ThresholdGeneration string    `gorm:"size:40;not null;uniqueIndex:uq_quota_warning,priority:7;check:ck_quota_warning_generation,threshold_generation = 'personal-monthly-80-90-v1'"`
	TimeZone            string    `gorm:"size:100;not null"`
	AsOf                time.Time `gorm:"precision:6;not null"`
	Limit               string    `gorm:"column:limit_value;size:40;not null"`
	Settled             string    `gorm:"column:settled_value;size:80;not null"`
	CoverageStart       time.Time `gorm:"precision:6;not null"`
	ResourceCreatedAt   time.Time `gorm:"precision:6;not null;uniqueIndex:uq_quota_warning,priority:8"`
}

type quotaWarningInboxV59 struct {
	ID            string     `gorm:"primaryKey;size:30;index:idx_quota_warning_recipient_created,priority:3"`
	ObservationID string     `gorm:"size:30;not null;uniqueIndex:uq_quota_warning_inbox,priority:1"`
	RecipientID   string     `gorm:"size:30;not null;uniqueIndex:uq_quota_warning_inbox,priority:2;index:idx_quota_warning_recipient_created,priority:1"`
	ReadAt        *time.Time `gorm:"precision:6"`
	CreatedAt     time.Time  `gorm:"precision:6;not null;index:idx_quota_warning_recipient_created,priority:2"`
}

func (quotaWarningObservationV59) TableName() string { return "quota_warning_observations" }
func (quotaWarningInboxV59) TableName() string       { return "quota_warning_inboxes" }

// Candidate 59 is deliberately unregistered until the preceding release is
// checked. These private schema structs never follow evolving business entities.
const quotaWarningMigrationCandidateVersion = 59

func quotaWarningMigration(db *gorm.DB) error {
	for _, model := range []any{&quotaWarningObservationV59{}, &quotaWarningInboxV59{}} {
		if db.Migrator().HasTable(model) {
			stmt := &gorm.Statement{DB: db}
			if err := stmt.Parse(model); err != nil {
				return err
			}
			for _, field := range stmt.Schema.Fields {
				if !db.Migrator().HasColumn(model, field.DBName) {
					if err := db.Migrator().AddColumn(model, field.Name); err != nil {
						return err
					}
				}
			}
		}
	}
	return migrateTables(db, &quotaWarningObservationV59{}, &quotaWarningInboxV59{})
}
