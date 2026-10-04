package database

import (
	"time"

	"gorm.io/gorm"
)

// V46 adds historical creator proofs without foreign keys. Cleanup scheduling
// can change independently of the immutable Team readability deadline.
type storageTeamOwnerV46 struct {
	OwnerKind           string     `gorm:"size:16;not null;default:user;check:ck_storage_objects_owner_kind,owner_kind IN ('user','project','team')"`
	CreatorUserID       *string    `gorm:"size:30;check:ck_storage_objects_team_creator,(owner_kind = 'team' AND creator_user_id IS NOT NULL AND CHAR_LENGTH(creator_user_id) > 0 AND creator_membership_id IS NOT NULL AND CHAR_LENGTH(creator_membership_id) > 0 AND expires_at IS NOT NULL) OR (owner_kind IN ('user','project') AND creator_user_id IS NULL AND creator_membership_id IS NULL AND expires_at IS NULL)"`
	CreatorMembershipID *string    `gorm:"size:30"`
	ExpiresAt           *time.Time `gorm:"precision:6"`
}

func (storageTeamOwnerV46) TableName() string { return "storage_objects" }

func teamAttachmentMigration(db *gorm.DB) error {
	model := &storageTeamOwnerV46{}
	for _, field := range []string{"CreatorMembershipID", "ExpiresAt", "CreatorUserID"} {
		if !db.Migrator().HasColumn(model, field) {
			if err := db.Migrator().AddColumn(model, field); err != nil {
				return err
			}
		}
	}
	if !db.Migrator().HasConstraint(model, "ck_storage_objects_team_creator") {
		if err := db.Migrator().CreateConstraint(model, "ck_storage_objects_team_creator"); err != nil {
			return err
		}
	}
	// The released V23 guard must be replaced. Drop/create is independently
	// retryable after partially committed MySQL DDL; startup remains serialized.
	if db.Migrator().HasConstraint(model, "ck_storage_objects_owner_kind") {
		if err := db.Migrator().DropConstraint(model, "ck_storage_objects_owner_kind"); err != nil {
			return err
		}
	}
	for _, name := range []string{"ck_storage_objects_owner_kind", "ck_storage_objects_team_creator"} {
		if !db.Migrator().HasConstraint(model, name) {
			if err := db.Migrator().CreateConstraint(model, name); err != nil {
				return err
			}
		}
	}
	return nil
}
