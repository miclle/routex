package database

import (
	"time"

	"gorm.io/gorm"
)

// teamMemberQuotaWarningObservationV62 records a current known settled level, not a crossing
// or an admission guarantee. Private Team-member warnings retain their original facts.
type teamMemberQuotaWarningObservationV62 struct {
	ID                  string    `gorm:"primaryKey;size:30"`
	ScopeID             string    `gorm:"size:52;not null;uniqueIndex:uq_team_member_quota_warning,priority:1;check:ck_team_member_quota_warning_scope,CHAR_LENGTH(scope_id) = 52 AND CHAR_LENGTH(team_id) BETWEEN 1 AND 30 AND CHAR_LENGTH(member_user_id) BETWEEN 1 AND 30"`
	TeamID              string    `gorm:"size:30;not null"`
	MemberUserID        string    `gorm:"size:30;not null"`
	TeamName            string    `gorm:"size:200;not null"`
	Dimension           string    `gorm:"size:20;not null;uniqueIndex:uq_team_member_quota_warning,priority:2;check:ck_team_member_quota_warning_dimension,dimension IN ('tokens','money')"`
	MonthStart          time.Time `gorm:"precision:6;not null;uniqueIndex:uq_team_member_quota_warning,priority:3;check:ck_team_member_quota_warning_calendar,month_end > month_start AND as_of >= month_start AND as_of < month_end AND coverage_start <= as_of AND resource_created_at <= as_of AND user_created_at <= as_of"`
	MonthEnd            time.Time `gorm:"precision:6;not null"`
	PolicyRevision      string    `gorm:"size:64;not null;uniqueIndex:uq_team_member_quota_warning,priority:4"`
	Currency            string    `gorm:"size:3;not null;uniqueIndex:uq_team_member_quota_warning,priority:5;check:ck_team_member_quota_warning_currency,(dimension = 'tokens' AND currency = '') OR (dimension = 'money' AND CHAR_LENGTH(currency) = 3)"`
	Level               string    `gorm:"size:10;not null;uniqueIndex:uq_team_member_quota_warning,priority:6;check:ck_team_member_quota_warning_level,(level = 'near' AND threshold = 80) OR (level = 'critical' AND threshold = 90)"`
	Threshold           int       `gorm:"not null"`
	ThresholdGeneration string    `gorm:"size:40;not null;uniqueIndex:uq_team_member_quota_warning,priority:7;check:ck_team_member_quota_warning_generation,threshold_generation = 'team-member-monthly-80-90-v1'"`
	TimeZone            string    `gorm:"size:100;not null"`
	AsOf                time.Time `gorm:"precision:6;not null"`
	Limit               string    `gorm:"column:limit_value;size:40;not null"`
	Settled             string    `gorm:"column:settled_value;size:80;not null"`
	CoverageStart       time.Time `gorm:"precision:6;not null"`
	ResourceCreatedAt   time.Time `gorm:"precision:6;not null;uniqueIndex:uq_team_member_quota_warning,priority:8"`
	UserCreatedAt       time.Time `gorm:"precision:6;not null;uniqueIndex:uq_team_member_quota_warning,priority:9"`
}

type teamMemberQuotaWarningInboxV62 struct {
	ID                 string     `gorm:"primaryKey;size:30;index:idx_team_member_quota_warning_recipient_created,priority:3"`
	ObservationID      string     `gorm:"size:30;not null;uniqueIndex:uq_team_member_quota_warning_inbox,priority:1"`
	RecipientID        string     `gorm:"size:30;not null;uniqueIndex:uq_team_member_quota_warning_inbox,priority:2;index:idx_team_member_quota_warning_recipient_created,priority:1"`
	RecipientCreatedAt time.Time  `gorm:"precision:6;not null;check:ck_team_member_quota_warning_recipient_birth,recipient_created_at <= created_at"`
	ReadAt             *time.Time `gorm:"precision:6"`
	CreatedAt          time.Time  `gorm:"precision:6;not null;index:idx_team_member_quota_warning_recipient_created,priority:2"`
}

func (teamMemberQuotaWarningObservationV62) TableName() string {
	return "team_member_quota_warning_observations"
}
func (teamMemberQuotaWarningInboxV62) TableName() string { return "team_member_quota_warning_inboxes" }

// Version 62 follows the preceding warning release. These private schema structs
// never follow evolving business entities.
const teamMemberQuotaWarningMigrationCandidateVersion = 62

func teamMemberQuotaWarningMigration(db *gorm.DB) error {
	for _, model := range []any{&teamMemberQuotaWarningObservationV62{}, &teamMemberQuotaWarningInboxV62{}} {
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
	return migrateTables(db, &teamMemberQuotaWarningObservationV62{}, &teamMemberQuotaWarningInboxV62{})
}
