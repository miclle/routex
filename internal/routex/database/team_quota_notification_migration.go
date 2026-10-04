package database

import "gorm.io/gorm"

// Version 47 expands only the released scope check. Observation and recipient
// snapshots retain their original columns, indexes, and absence of live relations.
type quotaNotificationScopeV47 struct {
	ScopeKind string `gorm:"check:ck_quota_notification_scope_v47,scope_kind IN ('user','project','team')"`
}

func (quotaNotificationScopeV47) TableName() string { return "quota_notification_observations" }

func teamQuotaNotificationMigration(db *gorm.DB) error {
	model := &quotaNotificationScopeV47{}
	// Install the new guard before removing the released check. An interrupted
	// MySQL DDL prefix remains conservative and can be resumed without data edits.
	if !db.Migrator().HasConstraint(model, "ck_quota_notification_scope_v47") {
		if err := db.Migrator().CreateConstraint(model, "ck_quota_notification_scope_v47"); err != nil {
			return err
		}
	}
	if db.Migrator().HasConstraint(model, "ck_quota_notification_scope") {
		return db.Migrator().DropConstraint(model, "ck_quota_notification_scope")
	}
	return nil
}
