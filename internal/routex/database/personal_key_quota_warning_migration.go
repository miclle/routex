package database

import (
	"time"

	"gorm.io/gorm"
)

// personalKeyQuotaWarningObservationV64 records a current known settled level, not a crossing
// or an admission guarantee. Personal Key shared-account warnings retain their original facts.
type personalKeyQuotaWarningObservationV64 struct {
	ID                  string    `gorm:"primaryKey;size:30"`
	RootKeyID           string    `gorm:"size:30;not null;uniqueIndex:uq_personal_key_quota_warning,priority:1;check:ck_personal_key_quota_warning_root,CHAR_LENGTH(root_key_id) BETWEEN 1 AND 30"`
	RootKeyName         string    `gorm:"size:100;not null"`
	OwnerID             string    `gorm:"size:30;not null;uniqueIndex:uq_personal_key_quota_warning,priority:9;check:ck_personal_key_quota_warning_owner,CHAR_LENGTH(owner_id) BETWEEN 1 AND 30"`
	OwnerCreatedAt      time.Time `gorm:"precision:6;not null;uniqueIndex:uq_personal_key_quota_warning,priority:10;check:ck_personal_key_quota_warning_owner_birth,owner_created_at <= resource_created_at AND resource_created_at <= as_of"`
	Dimension           string    `gorm:"size:20;not null;uniqueIndex:uq_personal_key_quota_warning,priority:2;check:ck_personal_key_quota_warning_dimension,dimension IN ('tokens','money')"`
	MonthStart          time.Time `gorm:"precision:6;not null;uniqueIndex:uq_personal_key_quota_warning,priority:3;check:ck_personal_key_quota_warning_calendar,month_end > month_start AND as_of >= month_start AND as_of < month_end AND coverage_start <= as_of AND resource_created_at <= as_of"`
	MonthEnd            time.Time `gorm:"precision:6;not null"`
	PolicyRevision      string    `gorm:"size:64;not null;uniqueIndex:uq_personal_key_quota_warning,priority:4"`
	Currency            string    `gorm:"size:3;not null;uniqueIndex:uq_personal_key_quota_warning,priority:5;check:ck_personal_key_quota_warning_currency,(dimension = 'tokens' AND currency = '') OR (dimension = 'money' AND CHAR_LENGTH(currency) = 3)"`
	Level               string    `gorm:"size:10;not null;uniqueIndex:uq_personal_key_quota_warning,priority:6;check:ck_personal_key_quota_warning_level,(level = 'near' AND threshold = 80) OR (level = 'critical' AND threshold = 90)"`
	Threshold           int       `gorm:"not null"`
	ThresholdGeneration string    `gorm:"size:40;not null;uniqueIndex:uq_personal_key_quota_warning,priority:7;check:ck_personal_key_quota_warning_generation,threshold_generation = 'personal-key-monthly-80-90-v1'"`
	TimeZone            string    `gorm:"size:100;not null"`
	AsOf                time.Time `gorm:"precision:6;not null"`
	Limit               string    `gorm:"column:limit_value;size:40;not null"`
	Settled             string    `gorm:"column:settled_value;size:80;not null"`
	CoverageStart       time.Time `gorm:"precision:6;not null"`
	ResourceCreatedAt   time.Time `gorm:"precision:6;not null;uniqueIndex:uq_personal_key_quota_warning,priority:8"`
}

type personalKeyQuotaWarningInboxV64 struct {
	ID                 string     `gorm:"primaryKey;size:30;index:idx_personal_key_quota_warning_recipient_created,priority:3"`
	ObservationID      string     `gorm:"size:30;not null;uniqueIndex:uq_personal_key_quota_warning_inbox,priority:1"`
	RecipientID        string     `gorm:"size:30;not null;uniqueIndex:uq_personal_key_quota_warning_inbox,priority:2;index:idx_personal_key_quota_warning_recipient_created,priority:1"`
	RecipientCreatedAt time.Time  `gorm:"precision:6;not null;check:ck_personal_key_quota_warning_recipient_birth,recipient_created_at <= created_at"`
	ReadAt             *time.Time `gorm:"precision:6"`
	CreatedAt          time.Time  `gorm:"precision:6;not null;index:idx_personal_key_quota_warning_recipient_created,priority:2"`
}

func (personalKeyQuotaWarningObservationV64) TableName() string {
	return "personal_key_quota_warning_observations"
}
func (personalKeyQuotaWarningInboxV64) TableName() string {
	return "personal_key_quota_warning_inboxes"
}

// Candidate 64 is deliberately unregistered until the preceding release is
// checked. These private schema structs never follow evolving business entities.
const personalKeyQuotaWarningMigrationCandidateVersion = 64

// Only an index is added to the retained Key table. PostgreSQL does not create
// an owner index for its foreign key; the equality candidate keeps the complete
// owner graph read indexed while ExactText independently enforces identity.
type personalKeyWarningOwnerIndexV64 struct {
	ID     string `gorm:"primaryKey;size:30;index:idx_personal_key_warning_owner,priority:2"`
	UserID string `gorm:"size:30;not null;index:idx_personal_key_warning_owner,priority:1"`
}

func (personalKeyWarningOwnerIndexV64) TableName() string { return "api_keys" }
func personalKeyQuotaWarningMigration(db *gorm.DB) error {
	for _, model := range []any{&personalKeyQuotaWarningObservationV64{}, &personalKeyQuotaWarningInboxV64{}} {
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
	if err := migrateTables(db, &personalKeyQuotaWarningObservationV64{}, &personalKeyQuotaWarningInboxV64{}); err != nil {
		return err
	}
	model := &personalKeyWarningOwnerIndexV64{}
	if !db.Migrator().HasIndex(model, "idx_personal_key_warning_owner") {
		return db.Migrator().CreateIndex(model, "idx_personal_key_warning_owner")
	}
	return nil
}
