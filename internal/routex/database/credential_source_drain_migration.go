package database

import (
	"fmt"
	"gorm.io/gorm"
	"time"
)

type credentialSourceProcessV88 struct {
	ProcessID    string    `gorm:"primaryKey;size:30"`
	Generation   string    `gorm:"size:64;not null"`
	Birth        time.Time `gorm:"precision:6;not null"`
	RegisteredAt time.Time `gorm:"precision:6;not null"`
}

func (credentialSourceProcessV88) TableName() string { return "credential_source_processes" }

type credentialSourceDenialV88 struct {
	RemoteRequestID   *string   `gorm:"size:36"`
	PhysicalObject    string    `gorm:"primaryKey;size:64"`
	CreationRequestID string    `gorm:"size:36;not null;uniqueIndex:idx_credential_source_denial_creation"`
	RequestID         string    `gorm:"size:36;not null;uniqueIndex:idx_credential_source_denial_command"`
	CreatedAt         time.Time `gorm:"precision:6;not null;autoCreateTime:false"`
}

func (credentialSourceDenialV88) TableName() string { return "credential_source_denials" }

type credentialSourceUseV88 struct {
	PhysicalObject string     `gorm:"primaryKey;size:64"`
	ProcessID      string     `gorm:"primaryKey;size:30;index:idx_credential_source_use_process"`
	Generation     string     `gorm:"size:64;not null"`
	Birth          time.Time  `gorm:"precision:6;not null"`
	Exposed        bool       `gorm:"not null"`
	CreatedAt      time.Time  `gorm:"precision:6;not null;autoCreateTime:false"`
	JoinedAt       *time.Time `gorm:"precision:6"`
}

func (credentialSourceUseV88) TableName() string { return "credential_source_uses" }

type credentialPublishedCleanupV88 struct {
	PhysicalObject    string     `gorm:"size:64;not null"`
	KnownNoEffect     bool       `gorm:"not null;check:ck_credential_published_no_effect_v88,known_no_effect = false OR state = 'failed'"`
	CreationRequestID string     `gorm:"size:36;not null;index:idx_credential_published_creation"`
	RequestID         string     `gorm:"primaryKey;size:36"`
	ActorID           string     `gorm:"size:30;not null"`
	ActorBirth        time.Time  `gorm:"precision:6;not null"`
	IntegrationID     string     `gorm:"size:30;not null;index:idx_credential_published_integration"`
	IntegrationBirth  time.Time  `gorm:"precision:6;not null"`
	RevisionID        string     `gorm:"size:30;not null"`
	OperationProof    string     `gorm:"size:64;not null"`
	ReviewedETag      string     `gorm:"column:reviewed_etag;size:129;not null"`
	Reason            string     `gorm:"type:text;not null"`
	State             string     `gorm:"size:16;not null;check:ck_credential_published_cleanup_state_v88,(OCTET_LENGTH(state) = 7 AND ASCII(SUBSTRING(state,1,1)) = 112 AND ASCII(SUBSTRING(state,2,1)) = 101 AND ASCII(SUBSTRING(state,3,1)) = 110 AND ASCII(SUBSTRING(state,4,1)) = 100 AND ASCII(SUBSTRING(state,5,1)) = 105 AND ASCII(SUBSTRING(state,6,1)) = 110 AND ASCII(SUBSTRING(state,7,1)) = 103) OR (OCTET_LENGTH(state) = 7 AND ASCII(SUBSTRING(state,1,1)) = 117 AND ASCII(SUBSTRING(state,2,1)) = 110 AND ASCII(SUBSTRING(state,3,1)) = 107 AND ASCII(SUBSTRING(state,4,1)) = 110 AND ASCII(SUBSTRING(state,5,1)) = 111 AND ASCII(SUBSTRING(state,6,1)) = 119 AND ASCII(SUBSTRING(state,7,1)) = 110) OR (OCTET_LENGTH(state) = 6 AND ASCII(SUBSTRING(state,1,1)) = 102 AND ASCII(SUBSTRING(state,2,1)) = 97 AND ASCII(SUBSTRING(state,3,1)) = 105 AND ASCII(SUBSTRING(state,4,1)) = 108 AND ASCII(SUBSTRING(state,5,1)) = 101 AND ASCII(SUBSTRING(state,6,1)) = 100) OR (OCTET_LENGTH(state) = 12 AND ASCII(SUBSTRING(state,1,1)) = 97 AND ASCII(SUBSTRING(state,2,1)) = 99 AND ASCII(SUBSTRING(state,3,1)) = 107 AND ASCII(SUBSTRING(state,4,1)) = 110 AND ASCII(SUBSTRING(state,5,1)) = 111 AND ASCII(SUBSTRING(state,6,1)) = 119 AND ASCII(SUBSTRING(state,7,1)) = 108 AND ASCII(SUBSTRING(state,8,1)) = 101 AND ASCII(SUBSTRING(state,9,1)) = 100 AND ASCII(SUBSTRING(state,10,1)) = 103 AND ASCII(SUBSTRING(state,11,1)) = 101 AND ASCII(SUBSTRING(state,12,1)) = 100)"`
	OwnershipJSON     string     `gorm:"type:text;not null"`
	CleanupJSON       string     `gorm:"type:text;not null"`
	RootEpoch         uint64     `gorm:"not null"`
	StartedAt         time.Time  `gorm:"precision:6;not null"`
	Deadline          time.Time  `gorm:"precision:6;not null"`
	FinishedAt        *time.Time `gorm:"precision:6"`
}

func (credentialPublishedCleanupV88) TableName() string { return "credential_published_cleanups" }

// Frozen additive V88 never backfills exposure or successful drain. Missing
// fields on nonempty interrupted DDL cannot be repaired by inventing proof.
func credentialSourceDrainMigration(db *gorm.DB) error {
	models := []any{&credentialSourceProcessV88{}, &credentialSourceDenialV88{}, &credentialSourceUseV88{}, &credentialPublishedCleanupV88{}}
	for _, model := range models {
		if !db.Migrator().HasTable(model) {
			continue
		}
		statement := &gorm.Statement{DB: db}
		if err := statement.Parse(model); err != nil {
			return err
		}
		for _, field := range statement.Schema.Fields {
			if db.Migrator().HasColumn(model, field.DBName) {
				continue
			}
			var count int64
			if err := db.Model(model).Count(&count).Error; err != nil {
				return err
			}
			if count != 0 {
				return fmt.Errorf("credential source drain partial schema lacks proof field")
			}
			if err := db.Migrator().AddColumn(model, field.Name); err != nil {
				return err
			}
		}
	}
	return migrateTables(db, models...)
}
