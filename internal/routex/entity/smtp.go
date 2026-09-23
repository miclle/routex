package entity

import "time"

type SMTPSetting struct {
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

type SMTPTest struct {
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

func (SMTPSetting) TableName() string { return "smtp_settings" }
func (SMTPTest) TableName() string    { return "smtp_tests" }
