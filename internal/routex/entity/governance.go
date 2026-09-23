package entity

import "time"

// GovernanceSetting serializes platform identity policy changes and persists the
// registration switch independently of bootstrap configuration.
type GovernanceSetting struct {
	ID                  int  `gorm:"primaryKey"`
	RegistrationEnabled bool `gorm:"not null"`
}

type Role struct {
	ID        string `gorm:"primaryKey;size:30"`
	Name      string `gorm:"size:100;not null"`
	NameKey   string `gorm:"size:64;not null;uniqueIndex"`
	Builtin   bool   `gorm:"not null"`
	CreatedAt time.Time
}

type RolePermission struct {
	RoleID     string `gorm:"primaryKey;size:30"`
	Permission string `gorm:"primaryKey;size:80"`
}

type UserRole struct {
	UserID string `gorm:"primaryKey;size:30"`
	RoleID string `gorm:"primaryKey;size:30"`
}
