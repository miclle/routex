package entity

import "time"

type PricingSetting struct {
	ID               int    `gorm:"primaryKey;autoIncrement:false"`
	ETag             string `gorm:"size:64;not null"`
	PlatformCurrency string `gorm:"size:3;not null"`
}
type PricingExchangeRate struct {
	Currency string `gorm:"primaryKey;size:3"`
	Rate     string `gorm:"size:40;not null"`
}
type ModelPrice struct {
	ID               string `gorm:"primaryKey;size:30"`
	ProviderModelID  string `gorm:"size:30;not null;uniqueIndex"`
	ContextThreshold int64  `gorm:"not null"`
	UpdateSource     string `gorm:"size:20;not null"`
	FollowRepository bool   `gorm:"not null"`
	CreatedAt        time.Time
	UpdatedAt        time.Time
}
type PriceRate struct {
	ID           string `gorm:"primaryKey;size:30"`
	ModelPriceID string `gorm:"size:30;not null"`
	Metric       string `gorm:"size:30;not null"`
	Tier         string `gorm:"size:20;not null"`
	Unit         string `gorm:"size:20;not null"`
	Currency     string `gorm:"size:3;not null"`
	Amount       string `gorm:"size:40;not null"`
	Enabled      bool   `gorm:"not null"`
}
