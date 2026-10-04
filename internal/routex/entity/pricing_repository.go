package entity

import "time"

type RepositoryPriceSetting struct {
	ID            int        `gorm:"primaryKey;autoIncrement:false;check:ck_repository_price_singleton,id = 1"`
	Enabled       bool       `gorm:"not null"`
	ETag          string     `gorm:"column:etag;size:64;not null;check:ck_repository_price_etag,CHAR_LENGTH(etag) = 64"`
	LastAttemptAt *time.Time `gorm:"precision:6"`
	LastSuccessAt *time.Time `gorm:"precision:6"`
	LastResult    *string    `gorm:"size:32"`
}
type RepositoryPriceMapping struct {
	ProviderModelID string `gorm:"primaryKey;size:30"`
	SourceModelKey  string `gorm:"size:128;not null;check:ck_repository_price_mapping,CHAR_LENGTH(provider_model_id) > 0 AND CHAR_LENGTH(source_model_key) > 0"`
}
type RepositoryPriceReceipt struct {
	RequestID       string    `gorm:"primaryKey;size:36;check:ck_repository_price_receipt,CHAR_LENGTH(request_id) = 36 AND CHAR_LENGTH(request_hash) = 64 AND CHAR_LENGTH(source_digest) = 64 AND CHAR_LENGTH(review_etag) = 64"`
	ActorID         string    `gorm:"size:30;not null"`
	RequestHash     string    `gorm:"size:64;not null" json:"-"`
	SourceDigest    string    `gorm:"size:64;not null"`
	ReviewETag      string    `gorm:"column:review_etag;size:64;not null" json:"-"`
	Mode            string    `gorm:"size:16;not null;check:ck_repository_price_receipt_mode,((CHAR_LENGTH(mode) = 9 AND ASCII(SUBSTRING(mode,1,1)) = 99 AND ASCII(SUBSTRING(mode,2,1)) = 111 AND ASCII(SUBSTRING(mode,3,1)) = 110 AND ASCII(SUBSTRING(mode,4,1)) = 102 AND ASCII(SUBSTRING(mode,5,1)) = 105 AND ASCII(SUBSTRING(mode,6,1)) = 103 AND ASCII(SUBSTRING(mode,7,1)) = 117 AND ASCII(SUBSTRING(mode,8,1)) = 114 AND ASCII(SUBSTRING(mode,9,1)) = 101) OR (CHAR_LENGTH(mode) = 4 AND ASCII(SUBSTRING(mode,1,1)) = 115 AND ASCII(SUBSTRING(mode,2,1)) = 121 AND ASCII(SUBSTRING(mode,3,1)) = 110 AND ASCII(SUBSTRING(mode,4,1)) = 99) OR (CHAR_LENGTH(mode) = 7 AND ASCII(SUBSTRING(mode,1,1)) = 114 AND ASCII(SUBSTRING(mode,2,1)) = 101 AND ASCII(SUBSTRING(mode,3,1)) = 115 AND ASCII(SUBSTRING(mode,4,1)) = 116 AND ASCII(SUBSTRING(mode,5,1)) = 111 AND ASCII(SUBSTRING(mode,6,1)) = 114 AND ASCII(SUBSTRING(mode,7,1)) = 101))"`
	ConfigDigest    string    `gorm:"size:64;not null;check:ck_repository_price_result_digest,CHAR_LENGTH(config_digest) = 64 AND ((mode = 'configure' AND catalogue_digest IS NULL) OR (mode IN ('sync','restore') AND catalogue_digest IS NOT NULL AND CHAR_LENGTH(catalogue_digest) = 64))" json:"-"`
	CatalogueDigest *string   `gorm:"size:64" json:"-"`
	ConfigETag      string    `gorm:"column:config_etag;size:64;not null" json:"-"`
	CatalogueETag   string    `gorm:"column:catalogue_etag;size:64;not null" json:"-"`
	CreatedAt       time.Time `gorm:"precision:6;not null"`
}
