package database

import (
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Frozen version 18 adds explicit managed egress and preserves direct routing.
type egressV18 struct {
	ID               string `gorm:"primaryKey;size:30"`
	Name             string `gorm:"size:100;not null"`
	Kind             string `gorm:"size:12;not null"`
	Host             string `gorm:"size:253;not null"`
	Port             int    `gorm:"not null"`
	Enabled          bool   `gorm:"not null"`
	ETag             string `gorm:"size:30;not null"`
	SecretGeneration string `gorm:"size:30;not null"`
	AuthCiphertext   string `gorm:"type:text;not null"`
	LastDiagnostic   string `gorm:"type:text;not null"`
	LastCheckedAt    *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

func (egressV18) TableName() string { return "egresses" }

type egressSettingV18 struct {
	ID              int        `gorm:"primaryKey;autoIncrement:false"`
	DefaultEgressID *string    `gorm:"size:30"`
	DefaultEgress   *egressV18 `gorm:"foreignKey:DefaultEgressID;references:ID;constraint:fk_egress_setting_default,OnUpdate:RESTRICT,OnDelete:RESTRICT"`
	ETag            string     `gorm:"size:30;not null"`
	UpdatedAt       time.Time
}

func (egressSettingV18) TableName() string { return "egress_settings" }

type connectionEgressV18 struct {
	EgressMode string     `gorm:"size:12;not null;default:default"`
	EgressID   *string    `gorm:"size:30;index:idx_connections_egress"`
	Egress     *egressV18 `gorm:"foreignKey:EgressID;references:ID;constraint:fk_connection_egress,OnUpdate:RESTRICT,OnDelete:RESTRICT"`
	ETag       string     `gorm:"size:30;not null;default:0"`
}

func (connectionEgressV18) TableName() string { return "provider_connections" }

type egressPermissionV18 struct {
	RoleID     string `gorm:"primaryKey;size:30"`
	Permission string `gorm:"primaryKey;size:80"`
}

func (egressPermissionV18) TableName() string { return "role_permissions" }
func egressMigration(db *gorm.DB) error {
	if err := migrateTables(db, &egressV18{}, &egressSettingV18{}); err != nil {
		return err
	}
	for _, field := range []string{"EgressMode", "EgressID", "ETag"} {
		if !db.Migrator().HasColumn(&connectionEgressV18{}, field) {
			if err := db.Migrator().AddColumn(&connectionEgressV18{}, field); err != nil {
				return err
			}
		}
	}
	if err := migrateTables(db, &connectionEgressV18{}); err != nil {
		return err
	}
	if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&egressSettingV18{ID: 1, ETag: "0", UpdatedAt: time.Now().UTC()}).Error; err != nil {
		return err
	}
	for _, permission := range []string{"egress.read", "egress.write", "egress.test"} {
		if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&egressPermissionV18{RoleID: "rol_admin", Permission: permission}).Error; err != nil {
			return err
		}
	}
	return nil
}
