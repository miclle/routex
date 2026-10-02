package database

import (
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

type teamResourceLimitV39 struct {
	ScopeKind    string `gorm:"primaryKey;size:20"`
	ScopeID      string `gorm:"primaryKey;size:64;not null"`
	ETag         string `gorm:"size:64;not null"`
	PreviousETag string `gorm:"size:64;not null"`
	ActorID      string `gorm:"size:30;not null"`
	Reason       string `gorm:"size:2000;not null"`
	Tokens5H     *int64
	Tokens7D     *int64
	TokensMonth  *int64
	TPM          *int64
	MoneyMonth   *string `gorm:"size:40"`
	Currency     string  `gorm:"size:3;not null;default:''"`
	RPM          *int64
	Concurrency  *int64
	IPMode       string `gorm:"size:20;not null"`
	IPRangesJSON string `gorm:"type:text;not null"`
	UpdatedAt    time.Time
}

func (teamResourceLimitV39) TableName() string { return "resource_limits" }

type teamLimitPermissionV39 struct {
	RoleID     string `gorm:"primaryKey;size:30"`
	Permission string `gorm:"primaryKey;size:80"`
}

func (teamLimitPermissionV39) TableName() string { return "role_permissions" }

func teamResourceLimitMigration(db *gorm.DB) error {
	// Stable Team/User pairs use a full 52-character digest. Widening the policy
	// identity preserves existing journal account bytes and never resets usage.
	// Explicit NOT NULL retains the existing primary-key property when GORM
	// alters the column; primaryKey alone does not describe nullability.
	if err := db.Migrator().AlterColumn(&teamResourceLimitV39{}, "ScopeID"); err != nil {
		return err
	}
	for _, permission := range []string{"teams.tokens.write", "teams.money.write", "teams.rates.write"} {
		if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&teamLimitPermissionV39{RoleID: "rol_admin", Permission: permission}).Error; err != nil {
			return err
		}
	}
	return nil
}
