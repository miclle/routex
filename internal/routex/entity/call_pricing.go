package entity

// CallPricingFields extends immutable call facts. Nullable money means no exact
// assessment was possible; a non-null decimal zero is an explicitly free call.
type CallPricingFields struct {
	CacheReadTokens     *int64
	CacheWriteTokens    *int64
	PricingStatus       string  `gorm:"size:30;not null;default:not_captured"`
	PriceETag           string  `gorm:"size:64;not null;default:''"`
	ChargeAmount        *string `gorm:"size:80"`
	ChargeCurrency      *string `gorm:"size:3"`
	PricingSnapshotJSON *string `gorm:"type:text"`
}
