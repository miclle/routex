package database

import (
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"strings"
	"time"
)

type repositoryPriceSettingV49 struct {
	ID            int        `gorm:"primaryKey;autoIncrement:false;check:ck_repository_price_singleton,id = 1"`
	Enabled       bool       `gorm:"not null"`
	ETag          string     `gorm:"column:etag;size:64;not null;check:ck_repository_price_etag,CHAR_LENGTH(etag) = 64"`
	LastAttemptAt *time.Time `gorm:"precision:6"`
	LastSuccessAt *time.Time `gorm:"precision:6"`
	LastResult    *string    `gorm:"size:32"`
}
type repositoryPriceMappingV49 struct {
	ProviderModelID string `gorm:"primaryKey;size:30"`
	SourceModelKey  string `gorm:"size:128;not null;check:ck_repository_price_mapping,CHAR_LENGTH(provider_model_id) > 0 AND CHAR_LENGTH(source_model_key) > 0"`
}
type repositoryPriceReceiptV49 struct {
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

func (repositoryPriceSettingV49) TableName() string { return "repository_price_settings" }
func (repositoryPriceMappingV49) TableName() string { return "repository_price_mappings" }
func (repositoryPriceReceiptV49) TableName() string { return "repository_price_receipts" }

type repositoryModelPriceV49 struct {
	FollowRepository       bool    `gorm:"not null"`
	ID                     string  `gorm:"primaryKey;size:30"`
	RepositoryThresholdKey *string `gorm:"size:128"`
}

func (repositoryModelPriceV49) TableName() string { return "model_prices" }

type repositoryRateV49 struct {
	ID                 string  `gorm:"primaryKey;size:30"`
	RepositoryModelKey *string `gorm:"size:128;check:ck_price_rate_repository,(repository_model_key IS NULL AND repository_rate_key IS NULL) OR (repository_model_key IS NOT NULL AND repository_rate_key IS NOT NULL AND CHAR_LENGTH(repository_model_key) > 0 AND CHAR_LENGTH(repository_rate_key) > 0)"`
	RepositoryRateKey  *string `gorm:"size:128"`
}

func (repositoryRateV49) TableName() string { return "price_rates" }
func repositoryPriceMigration(db *gorm.DB) error {
	for _, m := range []struct {
		model   any
		columns []string
	}{{&repositoryModelPriceV49{}, []string{"RepositoryThresholdKey"}}, {&repositoryRateV49{}, []string{"RepositoryModelKey", "RepositoryRateKey"}}} {
		for _, c := range m.columns {
			if !db.Migrator().HasColumn(m.model, c) {
				if err := db.Migrator().AddColumn(m.model, c); err != nil {
					return err
				}
			}
		}
	}
	if !db.Migrator().HasConstraint(&repositoryRateV49{}, "ck_price_rate_repository") {
		if err := db.Migrator().CreateConstraint(&repositoryRateV49{}, "ck_price_rate_repository"); err != nil {
			return err
		}
	}
	for _, model := range []any{&repositoryPriceSettingV49{}, &repositoryPriceMappingV49{}, &repositoryPriceReceiptV49{}} {
		if db.Migrator().HasTable(model) {
			statement := &gorm.Statement{DB: db}
			if err := statement.Parse(model); err != nil {
				return err
			}
			for _, field := range statement.Schema.Fields {
				if field.DBName != "" && !db.Migrator().HasColumn(model, field.DBName) {
					if err := db.Migrator().AddColumn(model, field.Name); err != nil {
						return err
					}
				}
			}
		}
	}
	if err := migrateTables(db, &repositoryPriceSettingV49{}, &repositoryPriceMappingV49{}, &repositoryPriceReceiptV49{}); err != nil {
		return err
	}
	if err := db.Model(&repositoryModelPriceV49{}).Where("1 = 1").UpdateColumn("follow_repository", false).Error; err != nil {
		return err
	}
	followed := db.Model(&repositoryRateV49{}).Select("model_price_id").Where("repository_model_key IS NOT NULL AND repository_rate_key IS NOT NULL")
	if err := db.Model(&repositoryModelPriceV49{}).Where("id IN (?)", followed).UpdateColumn("follow_repository", true).Error; err != nil {
		return err
	}
	return db.Clauses(clause.OnConflict{DoNothing: true}).Create(&repositoryPriceSettingV49{ID: 1, ETag: strings.Repeat("0", 64)}).Error
}
