package database

import (
	"time"

	"gorm.io/gorm"
)

type userMFAV16 struct {
	UserID           string `gorm:"primaryKey;size:30"`
	Enabled          bool   `gorm:"not null"`
	Generation       string `gorm:"size:64;not null"`
	SecretCiphertext string `gorm:"type:text;not null"`
	LastTOTPStep     int64  `gorm:"not null"`
	FailedAttempts   int    `gorm:"not null"`
	LockedUntil      *time.Time
	UpdatedAt        time.Time
}

func (userMFAV16) TableName() string { return "user_mfa" }

type mfaChallengeV16 struct {
	UserID         string    `gorm:"primaryKey;size:30"`
	Purpose        string    `gorm:"primaryKey;size:20"`
	TokenHash      string    `gorm:"size:64;not null;uniqueIndex"`
	PasswordDigest string    `gorm:"size:64;not null"`
	Generation     string    `gorm:"size:64;not null"`
	SessionID      string    `gorm:"size:30;not null"`
	Attempts       int       `gorm:"not null"`
	ExpiresAt      time.Time `gorm:"not null"`
}

func (mfaChallengeV16) TableName() string { return "mfa_challenges" }

type mfaRecoveryCodeV16 struct {
	UserID   string `gorm:"primaryKey;size:30"`
	CodeHash string `gorm:"primaryKey;size:64"`
	UsedAt   *time.Time
}

func (mfaRecoveryCodeV16) TableName() string { return "mfa_recovery_codes" }
func mfaMigration(db *gorm.DB) error {
	return migrateTables(db, &userMFAV16{}, &mfaChallengeV16{}, &mfaRecoveryCodeV16{})
}
