package database

import "gorm.io/gorm"

// Version 13 adds immutable assessment facts without changing earlier schemas.
type callPricingV13 struct {
	RequestID           string `gorm:"primaryKey;size:64"`
	CacheReadTokens     *int64
	CacheWriteTokens    *int64
	PricingStatus       string  `gorm:"size:30;not null;default:not_captured"`
	PriceETag           string  `gorm:"size:64;not null;default:''"`
	ChargeAmount        *string `gorm:"size:80"`
	ChargeCurrency      *string `gorm:"size:3"`
	PricingSnapshotJSON *string `gorm:"type:text"`
}

func (callPricingV13) TableName() string { return "call_records" }
func callPricingMigration(db *gorm.DB) error {
	for _, field := range []string{"CacheReadTokens", "CacheWriteTokens", "PricingStatus", "PriceETag", "ChargeAmount", "ChargeCurrency", "PricingSnapshotJSON"} {
		if !db.Migrator().HasColumn(&callPricingV13{}, field) {
			if err := db.Migrator().AddColumn(&callPricingV13{}, field); err != nil {
				return err
			}
		}
	}
	return nil
}
