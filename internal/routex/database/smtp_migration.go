package database

import (
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type smtpSettingV20 struct {
	ID                int    `gorm:"primaryKey;autoIncrement:false"`
	Enabled           bool   `gorm:"not null"`
	Host              string `gorm:"size:253;not null"`
	Port              int    `gorm:"not null"`
	Security          string `gorm:"size:12;not null"`
	SecretGeneration  string `gorm:"size:30;not null" json:"-"`
	AuthCiphertext    string `gorm:"type:text;not null" json:"-"`
	SenderName        string `gorm:"size:100;not null"`
	SenderEmail       string `gorm:"size:254;not null"`
	ReplyTo           string `gorm:"size:254;not null"`
	ETag              string `gorm:"size:30;not null"`
	LastTestStartedAt *time.Time
	UpdatedAt         time.Time
}

type smtpTestV20 struct {
	ID              string    `gorm:"primaryKey;size:30"`
	RequestID       string    `gorm:"size:80;not null"`
	RequestHash     string    `gorm:"size:64;not null;uniqueIndex:idx_smtp_tests_request" json:"-"`
	ActorID         string    `gorm:"size:30;not null"`
	ConfigETag      string    `gorm:"size:30;not null"`
	RecipientDigest string    `gorm:"size:64;not null" json:"-"`
	Status          string    `gorm:"size:12;not null"`
	ResultJSON      string    `gorm:"type:text;not null"`
	StartedAt       time.Time `gorm:"not null;index:idx_smtp_tests_started"`
	CompletedAt     *time.Time
}

func (smtpSettingV20) TableName() string { return "smtp_settings" }
func (smtpTestV20) TableName() string    { return "smtp_tests" }

type smtpPermissionV20 struct {
	RoleID     string `gorm:"primaryKey;size:30"`
	Permission string `gorm:"size:80;not null"`
}

func (smtpPermissionV20) TableName() string { return "role_permissions" }

// Frozen version 20 adds SMTP configuration and bounded idempotent test attempts.
func smtpMigration(db *gorm.DB) error {
	if err := migrateTables(db, &smtpSettingV20{}, &smtpTestV20{}); err != nil {
		return err
	}
	if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&smtpSettingV20{ID: 1, Port: 587, Security: "STARTTLS", ETag: "0", UpdatedAt: time.Now().UTC()}).Error; err != nil {
		return err
	}
	for _, permission := range []string{"smtp.read", "smtp.write", "smtp.test"} {
		if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&smtpPermissionV20{RoleID: "rol_admin", Permission: permission}).Error; err != nil {
			return err
		}
	}
	return nil
}
