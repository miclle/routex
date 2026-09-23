package database

import (
	"github.com/miclle/routex/pkg/secret"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

type pricingSettingV11 struct {
	ID               int    `gorm:"primaryKey;autoIncrement:false"`
	ETag             string `gorm:"size:64;not null"`
	PlatformCurrency string `gorm:"size:3;not null"`
}

func (pricingSettingV11) TableName() string { return "pricing_settings" }

type pricingFXV11 struct {
	Currency string `gorm:"primaryKey;size:3"`
	Rate     string `gorm:"size:40;not null"`
}

func (pricingFXV11) TableName() string { return "pricing_exchange_rates" }

type pricingProviderModelV11 struct {
	ID string `gorm:"primaryKey;size:30"`
}

func (pricingProviderModelV11) TableName() string { return "provider_models" }

type pricingModelV11 struct {
	ID               string `gorm:"primaryKey;size:30"`
	ProviderModelID  string `gorm:"size:30;not null;uniqueIndex:uq_model_price_provider_model"`
	ContextThreshold int64  `gorm:"not null"`
	UpdateSource     string `gorm:"size:20;not null"`
	FollowRepository bool   `gorm:"not null"`
	CreatedAt        time.Time
	UpdatedAt        time.Time
	ProviderModel    pricingProviderModelV11 `gorm:"belongsTo:ProviderModel;foreignKey:ProviderModelID;references:ID;constraint:fk_model_price_provider_model,OnDelete:RESTRICT"`
}

func (pricingModelV11) TableName() string { return "model_prices" }

type pricingRateV11 struct {
	ID           string          `gorm:"primaryKey;size:30"`
	ModelPriceID string          `gorm:"size:30;not null;uniqueIndex:uq_price_rate_metric_tier,priority:1"`
	Metric       string          `gorm:"size:30;not null;uniqueIndex:uq_price_rate_metric_tier,priority:2"`
	Tier         string          `gorm:"size:20;not null;uniqueIndex:uq_price_rate_metric_tier,priority:3"`
	Unit         string          `gorm:"size:20;not null"`
	Currency     string          `gorm:"size:3;not null"`
	Amount       string          `gorm:"size:40;not null"`
	Enabled      bool            `gorm:"not null"`
	Price        pricingModelV11 `gorm:"belongsTo:Price;foreignKey:ModelPriceID;references:ID;constraint:fk_price_rate_price,OnDelete:RESTRICT"`
}

func (pricingRateV11) TableName() string { return "price_rates" }

type pricingAuditV11 struct {
	ID          string  `gorm:"primaryKey;size:30"`
	DetailsJSON *string `gorm:"type:text"`
}

func (pricingAuditV11) TableName() string { return "audit_events" }
func pricingMigration(db *gorm.DB) error {
	if err := migrateTables(db, &pricingSettingV11{}, &pricingFXV11{}, &pricingModelV11{}, &pricingRateV11{}); err != nil {
		return err
	}
	if !db.Migrator().HasColumn(&pricingAuditV11{}, "DetailsJSON") {
		if err := db.Migrator().AddColumn(&pricingAuditV11{}, "DetailsJSON"); err != nil {
			return err
		}
	}
	etag, err := secret.RandomURLSafe(24)
	if err != nil {
		return err
	}
	if err = db.Clauses(clause.OnConflict{DoNothing: true}).Create(&pricingSettingV11{ID: 1, ETag: etag, PlatformCurrency: "USD"}).Error; err != nil {
		return err
	}
	for _, permission := range []string{"prices.read", "prices.write"} {
		if err = db.Clauses(clause.OnConflict{DoNothing: true}).Create(&governanceV6Permission{RoleID: "rol_admin", Permission: permission}).Error; err != nil {
			return err
		}
	}
	return nil
}
