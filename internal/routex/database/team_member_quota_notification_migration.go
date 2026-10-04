package database

import (
	"errors"
	"gorm.io/gorm"
)

// V51 is reserved after the independently integrated V48/V49/V50 packages.
const teamMemberQuotaNotificationVersion = 51

// The pair identity is historical evidence, never a live Team/User relation.
// ASCII checks retain exact scope kinds under case-insensitive MySQL collations.
type quotaNotificationMemberV51 struct {
	ScopeKind    string  `gorm:"check:ck_quota_notification_scope_v51,(CHAR_LENGTH(scope_kind) = 4 AND ASCII(SUBSTRING(scope_kind,1,1)) = 117 AND ASCII(SUBSTRING(scope_kind,2,1)) = 115 AND ASCII(SUBSTRING(scope_kind,3,1)) = 101 AND ASCII(SUBSTRING(scope_kind,4,1)) = 114) OR (CHAR_LENGTH(scope_kind) = 7 AND ASCII(SUBSTRING(scope_kind,1,1)) = 112 AND ASCII(SUBSTRING(scope_kind,2,1)) = 114 AND ASCII(SUBSTRING(scope_kind,3,1)) = 111 AND ASCII(SUBSTRING(scope_kind,4,1)) = 106 AND ASCII(SUBSTRING(scope_kind,5,1)) = 101 AND ASCII(SUBSTRING(scope_kind,6,1)) = 99 AND ASCII(SUBSTRING(scope_kind,7,1)) = 116) OR (CHAR_LENGTH(scope_kind) = 4 AND ASCII(SUBSTRING(scope_kind,1,1)) = 116 AND ASCII(SUBSTRING(scope_kind,2,1)) = 101 AND ASCII(SUBSTRING(scope_kind,3,1)) = 97 AND ASCII(SUBSTRING(scope_kind,4,1)) = 109) OR (CHAR_LENGTH(scope_kind) = 11 AND ASCII(SUBSTRING(scope_kind,1,1)) = 116 AND ASCII(SUBSTRING(scope_kind,2,1)) = 101 AND ASCII(SUBSTRING(scope_kind,3,1)) = 97 AND ASCII(SUBSTRING(scope_kind,4,1)) = 109 AND ASCII(SUBSTRING(scope_kind,5,1)) = 95 AND ASCII(SUBSTRING(scope_kind,6,1)) = 109 AND ASCII(SUBSTRING(scope_kind,7,1)) = 101 AND ASCII(SUBSTRING(scope_kind,8,1)) = 109 AND ASCII(SUBSTRING(scope_kind,9,1)) = 98 AND ASCII(SUBSTRING(scope_kind,10,1)) = 101 AND ASCII(SUBSTRING(scope_kind,11,1)) = 114)"`
	ScopeID      string  `gorm:"size:64;not null"`
	MemberUserID *string `gorm:"size:30"`
	TeamID       *string `gorm:"size:30;check:ck_quota_notification_member_v51,(scope_kind = 'team_member' AND CHAR_LENGTH(scope_id) = 52 AND team_id IS NOT NULL AND CHAR_LENGTH(team_id) > 0 AND member_user_id IS NOT NULL AND CHAR_LENGTH(member_user_id) > 0) OR (scope_kind IN ('user','project','team') AND CHAR_LENGTH(scope_id) BETWEEN 1 AND 30 AND team_id IS NULL AND member_user_id IS NULL)"`
}

func (quotaNotificationMemberV51) TableName() string { return "quota_notification_observations" }

func teamMemberQuotaNotificationMigration(db *gorm.DB) error {
	model := &quotaNotificationMemberV51{}
	// Add the guard-bearing field last so every referenced column exists even
	// where the driver includes its check during AddColumn.
	for _, field := range []string{"MemberUserID", "TeamID"} {
		if !db.Migrator().HasColumn(model, field) {
			if err := db.Migrator().AddColumn(model, field); err != nil {
				return err
			}
		}
	}
	columns, err := db.Migrator().ColumnTypes(model)
	if err != nil {
		return err
	}
	found := false
	for _, column := range columns {
		if column.Name() != "scope_id" {
			continue
		}
		found = true
		if length, known := column.Length(); !known || length != 64 {
			if err := db.Migrator().AlterColumn(model, "ScopeID"); err != nil {
				return err
			}
		}
	}
	if !found {
		return errors.New("quota observation scope column is missing")
	}
	// New guards are installed before removing the released restrictive guard.
	// Every partially committed MySQL DDL prefix can therefore be retried.
	for _, name := range []string{"ck_quota_notification_member_v51", "ck_quota_notification_scope_v51"} {
		if !db.Migrator().HasConstraint(model, name) {
			if err := db.Migrator().CreateConstraint(model, name); err != nil {
				return err
			}
		}
	}
	for _, name := range []string{"ck_quota_notification_scope_v47", "ck_quota_notification_scope"} {
		if db.Migrator().HasConstraint(model, name) {
			if err := db.Migrator().DropConstraint(model, name); err != nil {
				return err
			}
		}
	}
	return nil
}
