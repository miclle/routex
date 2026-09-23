package entity

import "time"

const (
	RoleAdmin  = "admin"
	RoleMember = "member"
)

// User is a local platform identity. HTTP handlers expose a separate safe DTO.
type User struct {
	ID           string `gorm:"primaryKey;size:30"`
	Email        string `gorm:"size:254;not null;uniqueIndex"`
	Name         string `gorm:"size:100;not null"`
	PasswordHash string `gorm:"size:60;not null"`
	Role         string `gorm:"size:20;not null"`
	Disabled     bool   `gorm:"not null"`
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// Session stores only a digest of the bearer secret.
type Session struct {
	ID        string    `gorm:"primaryKey;size:30"`
	UserID    string    `gorm:"size:30;not null;index"`
	TokenHash string    `gorm:"size:64;not null;uniqueIndex"`
	ExpiresAt time.Time `gorm:"not null;index"`
	CreatedAt time.Time
}

// Installation is a singleton row locked during first administrator creation.
type Installation struct {
	ID          int  `gorm:"primaryKey"`
	Initialized bool `gorm:"not null"`
}
