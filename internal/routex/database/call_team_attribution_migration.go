package database

import (
	"time"

	"gorm.io/gorm"
)

// Team invocation retains the dispatched actor and membership independently of
// mutable resource relationships. Existing records remain explicitly unknown.
type callTeamAttributionV38 struct {
	RequestID        string    `gorm:"primaryKey;size:64;index:idx_calls_team_actor_time,priority:4"`
	TeamID           string    `gorm:"size:30;not null;default:'';index:idx_calls_team_actor_time,priority:1;check:ck_calls_team_subject,(team_id = '' AND team_membership_id = '') OR (team_id <> '' AND team_membership_id <> '' AND user_id <> '' AND project_id = '' AND key_id = '')"`
	TeamMembershipID string    `gorm:"size:30;not null;default:''"`
	UserID           string    `gorm:"size:30;not null;index:idx_calls_team_actor_time,priority:2"`
	StartedAt        time.Time `gorm:"not null;index:idx_calls_team_actor_time,priority:3"`
}

func (callTeamAttributionV38) TableName() string { return "call_records" }

func callTeamAttributionMigration(db *gorm.DB) error {
	model := &callTeamAttributionV38{}
	for _, field := range []string{"TeamID", "TeamMembershipID"} {
		if !db.Migrator().HasColumn(model, field) {
			if err := db.Migrator().AddColumn(model, field); err != nil {
				return err
			}
		}
	}
	if !db.Migrator().HasConstraint(model, "ck_calls_team_subject") {
		if err := db.Migrator().CreateConstraint(model, "ck_calls_team_subject"); err != nil {
			return err
		}
	}
	if !db.Migrator().HasIndex(model, "idx_calls_team_actor_time") {
		return db.Migrator().CreateIndex(model, "idx_calls_team_actor_time")
	}
	return nil
}
