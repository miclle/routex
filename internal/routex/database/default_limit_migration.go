package database

import (
	"crypto/sha256"
	"encoding/hex"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Version 42 stores creation templates independently of live resource policies.
// No existing user or Team is backfilled with a template restriction.
type defaultLimitRuleV42 struct {
	Kind         string    `gorm:"primaryKey;size:20;not null;check:ck_default_limit_kind,kind IN ('user','team')"`
	Tokens5H     *int64    `gorm:"column:tokens_5h;check:ck_default_limit_tokens_5h,tokens_5h IS NULL OR (tokens_5h >= 0 AND tokens_5h <= 9007199254740991)"`
	Tokens7D     *int64    `gorm:"column:tokens_7d;check:ck_default_limit_tokens_7d,tokens_7d IS NULL OR (tokens_7d >= 0 AND tokens_7d <= 9007199254740991)"`
	TokensMonth  *int64    `gorm:"check:ck_default_limit_tokens_month,tokens_month IS NULL OR (tokens_month >= 0 AND tokens_month <= 9007199254740991)"`
	TPM          *int64    `gorm:"check:ck_default_limit_tpm,tpm IS NULL OR (tpm >= 0 AND tpm <= 9007199254740991)"`
	MoneyMonth   *string   `gorm:"size:40;check:ck_default_limit_money_currency,(money_month IS NULL AND currency = '') OR (money_month IS NOT NULL AND CHAR_LENGTH(currency) = 3)"`
	Currency     string    `gorm:"size:3;not null;default:''"`
	RPM          *int64    `gorm:"check:ck_default_limit_rpm,rpm IS NULL OR (rpm >= 0 AND rpm <= 9007199254740991)"`
	Concurrency  *int64    `gorm:"check:ck_default_limit_concurrency,concurrency IS NULL OR (concurrency >= 0 AND concurrency <= 9007199254740991)"`
	RuleETag     string    `gorm:"column:rule_etag;size:64;not null;check:ck_default_limit_revision,CHAR_LENGTH(rule_etag) = 64"`
	PreviousETag *string   `gorm:"column:previous_etag;size:64;check:ck_default_limit_previous_revision,previous_etag IS NULL OR CHAR_LENGTH(previous_etag) = 64"`
	ActorID      string    `gorm:"size:30;not null"`
	Reason       string    `gorm:"size:1024;not null"`
	UpdatedAt    time.Time `gorm:"precision:6"`
}

func (defaultLimitRuleV42) TableName() string { return "default_limit_rules" }

type resourceDefaultProvenanceV42 struct {
	ScopeKind          string  `gorm:"primaryKey;size:20"`
	ScopeID            string  `gorm:"primaryKey;size:64;not null"`
	AppliedDefaultETag *string `gorm:"column:applied_default_etag;size:64;check:ck_resource_applied_default_revision,applied_default_etag IS NULL OR CHAR_LENGTH(applied_default_etag) = 64"`
	DefaultResetETag   *string `gorm:"column:default_reset_etag;size:64;check:ck_resource_default_reset_revision,default_reset_etag IS NULL OR CHAR_LENGTH(default_reset_etag) = 64"`
}

func (resourceDefaultProvenanceV42) TableName() string { return "resource_limits" }

func defaultLimitMigration(db *gorm.DB) error {
	if err := migrateTables(db, &defaultLimitRuleV42{}); err != nil {
		return err
	}
	provenance := &resourceDefaultProvenanceV42{}
	for _, field := range []string{"AppliedDefaultETag", "DefaultResetETag"} {
		if !db.Migrator().HasColumn(provenance, field) {
			if err := db.Migrator().AddColumn(provenance, field); err != nil {
				return err
			}
		}
	}
	// AddColumn and check creation may commit separately on MySQL. Reconcile
	// each guard independently before advancing the immutable migration ledger.
	for _, name := range []string{"ck_resource_applied_default_revision", "ck_resource_default_reset_revision"} {
		if !db.Migrator().HasConstraint(provenance, name) {
			if err := db.Migrator().CreateConstraint(provenance, name); err != nil {
				return err
			}
		}
	}
	for _, kind := range []string{"user", "team"} {
		revision := sha256.Sum256([]byte("routex.default-limits." + kind + ".v42"))
		seed := &defaultLimitRuleV42{Kind: kind, RuleETag: hex.EncodeToString(revision[:]), UpdatedAt: time.Unix(0, 0).UTC()}
		if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(seed).Error; err != nil {
			return err
		}
	}
	return nil
}
