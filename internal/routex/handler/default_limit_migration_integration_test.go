package handler

import (
	"context"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
)

// Bare V42 reconstructs a MySQL prefix before independent GORM check repair.
type defaultLimitBareV42Fixture struct {
	Kind         string `gorm:"primaryKey;size:20;not null"`
	Tokens5H     *int64 `gorm:"column:tokens_5h"`
	Tokens7D     *int64 `gorm:"column:tokens_7d"`
	TokensMonth  *int64
	TPM          *int64
	MoneyMonth   *string `gorm:"size:40"`
	Currency     string  `gorm:"size:3;not null;default:''"`
	RPM          *int64
	Concurrency  *int64
	RuleETag     string    `gorm:"column:rule_etag;size:64;not null"`
	PreviousETag *string   `gorm:"column:previous_etag;size:64"`
	ActorID      string    `gorm:"size:30;not null"`
	Reason       string    `gorm:"size:1024;not null"`
	UpdatedAt    time.Time `gorm:"precision:6"`
}

func (defaultLimitBareV42Fixture) TableName() string { return "default_limit_rules" }

type defaultProvenanceV42Fixture struct {
	AppliedDefaultETag *string `gorm:"column:applied_default_etag;size:64;check:ck_resource_applied_default_revision,applied_default_etag IS NULL OR CHAR_LENGTH(applied_default_etag) = 64"`
	DefaultResetETag   *string `gorm:"column:default_reset_etag;size:64;check:ck_resource_default_reset_revision,default_reset_etag IS NULL OR CHAR_LENGTH(default_reset_etag) = 64"`
}

func (defaultProvenanceV42Fixture) TableName() string { return "resource_limits" }

func testDefaultLimitMigration(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx := context.Background()
	var originalRules []entity.DefaultLimitRule
	if err := db.Order("kind").Find(&originalRules).Error; err != nil || len(originalRules) != 2 {
		t.Fatal("V42 must seed exactly two unlimited creation templates", err)
	}
	for _, rule := range originalRules {
		if rule.Kind != "user" && rule.Kind != "team" || len(rule.RuleETag) != 64 || rule.PreviousETag != nil || rule.Tokens5H != nil || rule.Tokens7D != nil || rule.TokensMonth != nil || rule.TPM != nil || rule.MoneyMonth != nil || rule.Currency != "" || rule.RPM != nil || rule.Concurrency != nil {
			t.Fatal("upgrade cannot invent finite defaults", rule)
		}
	}
	tokens := int64(17)
	money := "12.3400"
	legacy := entity.ResourceLimit{ScopeKind: "user", ScopeID: "usr_default_upgrade", ETag: "legacy-policy", PreviousETag: "previous-policy", ActorID: "usr_historical", Reason: "Preserve independent policy", TokensMonth: &tokens, MoneyMonth: &money, Currency: "USD", IPMode: "allow", IPRangesJSON: `["192.0.2.0/24"]`, UpdatedAt: time.Now().UTC().Truncate(time.Microsecond)}
	if err := db.Create(&legacy).Error; err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := db.Where("scope_kind = ? AND scope_id = ?", legacy.ScopeKind, legacy.ScopeID).Delete(&entity.ResourceLimit{}).Error; err != nil {
			t.Error(err)
		}
		for _, rule := range originalRules {
			if err := db.Save(&rule).Error; err != nil {
				t.Error("restore owned default migration fixtures", err)
			}
		}
	}()
	var before entity.ResourceLimit
	if err := db.First(&before, "scope_kind = ? AND scope_id = ?", legacy.ScopeKind, legacy.ScopeID).Error; err != nil {
		t.Fatal(err)
	}
	provenance := &defaultProvenanceV42Fixture{}
	checks := []string{"ck_resource_applied_default_revision", "ck_resource_default_reset_revision"}
	for prefix := 0; prefix <= 4; prefix++ {
		for _, name := range checks {
			if db.Migrator().HasConstraint(provenance, name) {
				if err := db.Migrator().DropConstraint(provenance, name); err != nil {
					t.Fatal(err)
				}
			}
		}
		for _, field := range []string{"AppliedDefaultETag", "DefaultResetETag"} {
			if err := db.Migrator().DropColumn(provenance, field); err != nil {
				t.Fatal(err)
			}
		}
		if err := db.Migrator().DropTable(&entity.DefaultLimitRule{}); err != nil {
			t.Fatal(err)
		}
		if result := db.Table("schema_migrations").Where("version = ?", 42).Delete(&struct{}{}); result.Error != nil || result.RowsAffected != 1 {
			t.Fatal("reconstruct V41 ledger", result.Error)
		}
		custom := entity.DefaultLimitRule{Kind: "user", TokensMonth: &tokens, RuleETag: strings.Repeat("a", 64), ActorID: "usr_historical", Reason: "Preserve completed template write", UpdatedAt: before.UpdatedAt}
		if prefix > 0 {
			if err := db.Migrator().CreateTable(&defaultLimitBareV42Fixture{}); err != nil {
				t.Fatal(err)
			}
			if err := db.Create(&custom).Error; err != nil {
				t.Fatal(err)
			}
		}
		if prefix > 1 {
			if err := db.Migrator().AddColumn(provenance, "AppliedDefaultETag"); err != nil {
				t.Fatal(err)
			}
		}
		if prefix > 2 {
			if err := db.Migrator().AddColumn(provenance, "DefaultResetETag"); err != nil {
				t.Fatal(err)
			}
		}
		if prefix > 3 && !db.Migrator().HasConstraint(provenance, checks[0]) {
			if err := db.Migrator().CreateConstraint(provenance, checks[0]); err != nil {
				t.Fatal(err)
			}
		}
		var wg sync.WaitGroup
		outcomes := make(chan error, 2)
		for range 2 {
			wg.Go(func() { outcomes <- database.Migrate(ctx, db) })
		}
		wg.Wait()
		close(outcomes)
		for err := range outcomes {
			if err != nil {
				t.Fatal("concurrent V42 prefix repair", prefix, err)
			}
		}
		if err := database.Migrate(ctx, db); err != nil {
			t.Fatal("V42 repeat execution", err)
		}
		var after entity.ResourceLimit
		if err := db.First(&after, "scope_kind = ? AND scope_id = ?", legacy.ScopeKind, legacy.ScopeID).Error; err != nil || !reflect.DeepEqual(before, after) {
			t.Fatal("V42 changed historical policy, IP, provenance or timestamp", err)
		}
		var rules []entity.DefaultLimitRule
		if err := db.Order("kind").Find(&rules).Error; err != nil || len(rules) != 2 {
			t.Fatal("V42 missing exact seeds", err)
		}
		if prefix > 0 {
			var saved entity.DefaultLimitRule
			if err := db.First(&saved, "kind = ?", "user").Error; err != nil || !reflect.DeepEqual(saved, custom) {
				t.Fatal("seed reconciliation rewrote existing template", err)
			}
		}
		for _, name := range checks {
			if !db.Migrator().HasConstraint(provenance, name) {
				t.Fatal("missing additive provenance guard", name)
			}
		}
	}
	for _, column := range []string{"tokens_5h", "tokens_7d", "tokens_month", "tpm", "rpm", "concurrency"} {
		for _, invalid := range []int64{-1, 9007199254740992} {
			if err := db.Model(&entity.DefaultLimitRule{}).Where("kind = ?", "team").Update(column, invalid).Error; err == nil {
				t.Fatal("template accepted out-of-domain cap", column, invalid)
			}
		}
		if err := db.Model(&entity.DefaultLimitRule{}).Where("kind = ?", "team").Update(column, int64(0)).Error; err != nil {
			t.Fatal("zero must remain a real template cap", column, err)
		}
		if err := db.Model(&entity.DefaultLimitRule{}).Where("kind = ?", "team").Update(column, nil).Error; err != nil {
			t.Fatal("null must remain unlimited", column, err)
		}
	}
	for _, change := range []map[string]any{{"money_month": nil, "currency": "USD"}, {"money_month": "0", "currency": ""}, {"rule_etag": "short"}, {"previous_etag": "short"}} {
		if err := db.Model(&entity.DefaultLimitRule{}).Where("kind = ?", "team").Updates(change).Error; err == nil {
			t.Fatal("template accepted incoherent policy/revision", change)
		}
	}
	for _, column := range []string{"applied_default_etag", "default_reset_etag"} {
		if err := db.Model(&entity.ResourceLimit{}).Where("scope_kind = ? AND scope_id = ?", legacy.ScopeKind, legacy.ScopeID).Update(column, "short").Error; err == nil {
			t.Fatal("resource accepted unbounded provenance", column)
		}
	}
	invalidKind := originalRules[0]
	invalidKind.Kind = "project"
	if err := db.Create(&invalidKind).Error; err == nil {
		t.Fatal("creation defaults cannot extend into unsupported resource kinds")
	}
	if err := db.Create(&originalRules[0]).Error; err == nil {
		t.Fatal("duplicate canonical template identity accepted")
	}
}
