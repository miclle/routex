package database

import (
	"gorm.io/gorm"
	"time"
)

// Frozen V53: historical joins have no trustworthy timestamp to backfill.
type teamMembershipJoinedAtV53 struct {
	ID       string     `gorm:"primaryKey;size:30"`
	JoinedAt *time.Time `json:"-"`
}

func (teamMembershipJoinedAtV53) TableName() string { return "team_memberships" }
func teamMembershipJoinedAtMigration(db *gorm.DB) error {
	model := &teamMembershipJoinedAtV53{}
	if db.Migrator().HasColumn(model, "JoinedAt") {
		return nil
	}
	return db.Migrator().AddColumn(model, "JoinedAt")
}
