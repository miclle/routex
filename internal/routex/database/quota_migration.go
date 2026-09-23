package database

import (
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type quotaLimitV19 struct {
	ScopeKind   string `gorm:"primaryKey;size:20"`
	ScopeID     string `gorm:"primaryKey;size:30"`
	Tokens5H    *int64
	Tokens7D    *int64
	TokensMonth *int64
	TPM         *int64
	MoneyMonth  *string `gorm:"size:40"`
	Currency    string  `gorm:"size:3;not null;default:''"`
}

func (quotaLimitV19) TableName() string { return "resource_limits" }

type quotaSettingV19 struct {
	AccountingStarted bool   `gorm:"not null;default:false"`
	ID                int    `gorm:"primaryKey;autoIncrement:false"`
	TimeZone          string `gorm:"size:100;not null"`
	ETag              string `gorm:"size:64;not null"`
	PreviousETag      string `gorm:"size:64;not null"`
	ActorID           string `gorm:"size:30;not null"`
	Reason            string `gorm:"size:2000;not null"`
	UpdatedAt         time.Time
}

func (quotaSettingV19) TableName() string { return "quota_settings" }

type quotaProviderModelV19 struct {
	ID string `gorm:"primaryKey;size:30"`
}

func (quotaProviderModelV19) TableName() string { return "provider_models" }

type reservationBoundV19 struct {
	ProviderModelID string `gorm:"primaryKey;size:30"`
	Protocol        string `gorm:"size:30;not null"`
	MaxInputTokens  int64  `gorm:"not null"`
	MaxOutputTokens int64  `gorm:"not null"`
	Evidence        string `gorm:"size:2000;not null"`
	ETag            string `gorm:"size:64;not null"`
	PreviousETag    string `gorm:"size:64;not null"`
	ActorID         string `gorm:"size:30;not null"`
	Reason          string `gorm:"size:2000;not null"`
	UpdatedAt       time.Time
	ProviderModel   quotaProviderModelV19 `gorm:"foreignKey:ProviderModelID;references:ID;belongsTo;constraint:OnDelete:RESTRICT"`
}

func (reservationBoundV19) TableName() string { return "reservation_bounds" }
func quotaMigration(db *gorm.DB) error {
	for _, field := range []string{"Tokens5H", "Tokens7D", "TokensMonth", "TPM", "MoneyMonth", "Currency"} {
		if !db.Migrator().HasColumn(&quotaLimitV19{}, field) {
			if err := db.Migrator().AddColumn(&quotaLimitV19{}, field); err != nil {
				return err
			}
		}
	}
	if err := migrateTables(db, &quotaSettingV19{}, &reservationBoundV19{}); err != nil {
		return err
	}
	if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&quotaSettingV19{ID: 1, TimeZone: "UTC", ETag: "0"}).Error; err != nil {
		return err
	}
	return db.Clauses(clause.OnConflict{DoNothing: true}).Create(&quotaPermissionV19{RoleID: "rol_admin", Permission: "limits.settings.write"}).Error
}

type quotaPermissionV19 struct {
	RoleID     string `gorm:"primaryKey;size:30"`
	Permission string `gorm:"primaryKey;size:80"`
}

func (quotaPermissionV19) TableName() string { return "role_permissions" }
