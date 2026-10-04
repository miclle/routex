package handler

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/pricing"
	"gorm.io/gorm"
)

// These private partial tables deliberately omit guards. They reproduce durable
// MySQL DDL prefixes without altering a released schema or using DropIndex.
type repositorySettingPartial49 struct {
	ID            int        `gorm:"primaryKey;autoIncrement:false"`
	Enabled       bool       `gorm:"not null"`
	ETag          string     `gorm:"column:etag;size:64;not null"`
	LastAttemptAt *time.Time `gorm:"precision:6"`
	LastSuccessAt *time.Time `gorm:"precision:6"`
	LastResult    *string    `gorm:"size:32"`
}

func (repositorySettingPartial49) TableName() string { return "repository_price_settings" }

type repositoryMappingPartial49 struct {
	ProviderModelID string `gorm:"primaryKey;size:30"`
	SourceModelKey  string `gorm:"size:128;not null"`
}

func (repositoryMappingPartial49) TableName() string { return "repository_price_mappings" }

type repositoryReceiptPartial49 struct {
	RequestID       string    `gorm:"primaryKey;size:36"`
	ActorID         string    `gorm:"size:30;not null"`
	RequestHash     string    `gorm:"size:64;not null"`
	SourceDigest    string    `gorm:"size:64;not null"`
	ReviewETag      string    `gorm:"column:review_etag;size:64;not null"`
	Mode            string    `gorm:"size:16;not null"`
	ConfigDigest    string    `gorm:"size:64;not null"`
	CatalogueDigest *string   `gorm:"size:64"`
	ConfigETag      string    `gorm:"column:config_etag;size:64;not null"`
	CatalogueETag   string    `gorm:"column:catalogue_etag;size:64;not null"`
	CreatedAt       time.Time `gorm:"precision:6;not null"`
}

func (repositoryReceiptPartial49) TableName() string { return "repository_price_receipts" }

type repositoryIDPartial49 struct {
	ID int `gorm:"primaryKey;autoIncrement:false"`
}

func (repositoryIDPartial49) TableName() string { return "repository_price_settings" }

func testPricingRepositoryMigration(t *testing.T, db *gorm.DB) {
	t.Helper()
	stamp := time.Now().UTC().Truncate(time.Microsecond)
	// Price schedules retain their existing live Provider-model foreign key.
	// Only repository mappings/receipts and immutable call history allow absent
	// current subjects; the migration must preserve both distinct contracts.
	provider := entity.Provider{ID: "prv_v49_history", Name: "Retained price Provider", CreatedAt: stamp}
	connection := entity.ProviderConnection{ID: "con_v49_history", ProviderID: provider.ID, Name: "Retained price Connection", BaseURL: "https://prices.example.invalid/v1", Protocol: entity.ProtocolOpenAIChat, CreatedAt: stamp}
	providerModel := entity.ProviderModel{ID: "pmo_v49_history", ConnectionID: connection.ID, UpstreamName: "retained-price-model", CreatedAt: stamp}
	price := entity.ModelPrice{ID: "prc_v49_history", ProviderModelID: providerModel.ID, UpdateSource: "csv", FollowRepository: true, CreatedAt: stamp, UpdatedAt: stamp}
	rate := entity.PriceRate{ID: "rat_v49_history", ModelPriceID: price.ID, Metric: pricing.Input, Tier: pricing.Base, Unit: pricing.Unit, Currency: "USD", Amount: "0", Enabled: false}
	charge, currency, basis := "100.000000000000000001", "USD", `{"captured":"immutable historical pricing"}`
	call := entity.CallRecord{RequestID: "req_v49_history", SnapshotID: "snp_v49_history", UserID: "usr_v49_absent", KeyID: "key_v49_absent", ModelID: "mdl_v49_absent", ProviderModelID: price.ProviderModelID, Protocol: entity.ProtocolOpenAIChat, Status: "success", StartedAt: stamp, CompletedAt: stamp, CallPricingFields: entity.CallPricingFields{PricingStatus: "priced", PriceETag: "historical-etag", ChargeAmount: &charge, ChargeCurrency: &currency, PricingSnapshotJSON: &basis}}
	for _, row := range []any{&provider, &connection, &providerModel, &price, &rate, &call} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	readHistory := func() []any {
		t.Helper()
		var p entity.ModelPrice
		var r entity.PriceRate
		var c entity.CallRecord
		for _, q := range []*gorm.DB{db.Take(&p, "id = ?", price.ID), db.Take(&r, "id = ?", rate.ID), db.Take(&c, "request_id = ?", call.RequestID)} {
			if q.Error != nil {
				t.Fatal(q.Error)
			}
		}
		// The historical aggregate hint is deliberately normalized from per-rate
		// proof. It must not convert a zero/disabled legacy rate into owned pricing.
		p.FollowRepository = false
		return []any{p, r, c}
	}
	baseline := readHistory()
	newReceipt := func() entity.RepositoryPriceReceipt {
		digest := strings.Repeat("f", 64)
		return entity.RepositoryPriceReceipt{ConfigDigest: strings.Repeat("e", 64), CatalogueDigest: &digest, RequestID: "49000000-1111-4111-8111-111111111111", ActorID: "usr_v49_missing", RequestHash: strings.Repeat("a", 64), SourceDigest: strings.Repeat("b", 64), ReviewETag: strings.Repeat("c", 64), Mode: "sync", ConfigETag: strings.Repeat("d", 64), CatalogueETag: "historical-catalogue", CreatedAt: stamp}
	}
	for prefix := 0; prefix < 3; prefix++ {
		t.Logf("V49 independent upgrade prefix %d", prefix)
		for _, schema := range []any{&entity.RepositoryPriceReceipt{}, &entity.RepositoryPriceMapping{}, &entity.RepositoryPriceSetting{}} {
			if err := db.Migrator().DropTable(schema); err != nil {
				t.Fatal(err)
			}
		}
		if db.Migrator().HasConstraint(&entity.PriceRate{}, "ck_price_rate_repository") {
			if err := db.Migrator().DropConstraint(&entity.PriceRate{}, "ck_price_rate_repository"); err != nil {
				t.Fatal(err)
			}
		}
		for _, c := range []struct {
			model any
			name  string
		}{{&entity.PriceRate{}, "RepositoryRateKey"}, {&entity.PriceRate{}, "RepositoryModelKey"}, {&entity.ModelPrice{}, "RepositoryThresholdKey"}} {
			if db.Migrator().HasColumn(c.model, c.name) {
				if err := db.Migrator().DropColumn(c.model, c.name); err != nil {
					t.Fatal(err)
				}
			}
		}
		if err := db.Table("schema_migrations").Where("version = ?", 49).Delete(&struct{}{}).Error; err != nil {
			t.Fatal(err)
		}
		var receiptBefore *entity.RepositoryPriceReceipt
		var configBefore *entity.RepositoryPriceSetting
		switch prefix {
		case 1:
			for _, schema := range []any{&repositorySettingPartial49{}, &repositoryMappingPartial49{}, &repositoryReceiptPartial49{}} {
				if err := db.Migrator().CreateTable(schema); err != nil {
					t.Fatal(err)
				}
			}
			outcome := "committed"
			current := entity.RepositoryPriceSetting{ID: 1, Enabled: true, ETag: strings.Repeat("e", 64), LastAttemptAt: &stamp, LastSuccessAt: &stamp, LastResult: &outcome}
			receipt := newReceipt()
			mapping := entity.RepositoryPriceMapping{ProviderModelID: "pmo_v49_orphan", SourceModelKey: "history/model"}
			for _, row := range []any{&current, &receipt, &mapping} {
				if err := db.Create(row).Error; err != nil {
					t.Fatal("absent live subjects must not prevent retained history", err)
				}
			}
			if err := db.Take(&receipt, "request_id = ?", receipt.RequestID).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Take(&current, 1).Error; err != nil {
				t.Fatal(err)
			}
			receiptBefore, configBefore = &receipt, &current
		case 2:
			if err := db.Migrator().CreateTable(&repositoryIDPartial49{}); err != nil {
				t.Fatal(err)
			}
			if err := db.Migrator().AddColumn(&entity.ModelPrice{}, "RepositoryThresholdKey"); err != nil {
				t.Fatal(err)
			}
			if err := db.Migrator().AddColumn(&entity.PriceRate{}, "RepositoryModelKey"); err != nil {
				t.Fatal(err)
			}
		}
		var wg sync.WaitGroup
		results := make(chan error, 2)
		for range 2 {
			wg.Go(func() { results <- database.Migrate(context.Background(), db) })
		}
		wg.Wait()
		close(results)
		for err := range results {
			if err != nil {
				t.Fatal("concurrent V49 partial repair", prefix, err)
			}
		}
		if err := database.Migrate(context.Background(), db); err != nil {
			t.Fatal("repeat V49", err)
		}
		for _, guard := range []struct {
			model any
			name  string
		}{{&entity.RepositoryPriceSetting{}, "ck_repository_price_singleton"}, {&entity.RepositoryPriceSetting{}, "ck_repository_price_etag"}, {&entity.RepositoryPriceMapping{}, "ck_repository_price_mapping"}, {&entity.RepositoryPriceReceipt{}, "ck_repository_price_receipt"}, {&entity.RepositoryPriceReceipt{}, "ck_repository_price_receipt_mode"}, {&entity.RepositoryPriceReceipt{}, "ck_repository_price_result_digest"}, {&entity.PriceRate{}, "ck_price_rate_repository"}} {
			if !db.Migrator().HasConstraint(guard.model, guard.name) {
				t.Fatal("missing V49 guard", prefix, guard.name)
			}
		}
		for _, schema := range []any{&entity.RepositoryPriceSetting{}, &entity.RepositoryPriceMapping{}, &entity.RepositoryPriceReceipt{}} {
			statement := &gorm.Statement{DB: db}
			if err := statement.Parse(schema); err != nil || len(statement.Schema.Relationships.Relations) != 0 {
				t.Fatal("unexpected live relation", err)
			}
		}
		if !reflect.DeepEqual(baseline, readHistory()) {
			t.Fatal("V49 changed historical rate, schedule or immutable call pricing", prefix)
		}
		var normalized entity.ModelPrice
		if err := db.Take(&normalized, "id = ?", price.ID).Error; err != nil || normalized.FollowRepository {
			t.Fatal("historical aggregate hint incorrectly became per-rate ownership", err)
		}
		var current entity.RepositoryPriceSetting
		if err := db.Take(&current, 1).Error; err != nil {
			t.Fatal(err)
		}
		if configBefore != nil {
			if !reflect.DeepEqual(*configBefore, current) {
				t.Fatal("V49 changed persisted configuration", prefix)
			}
		} else if current.Enabled || current.ETag != strings.Repeat("0", 64) || current.LastAttemptAt != nil || current.LastSuccessAt != nil || current.LastResult != nil {
			t.Fatal("V49 fabricated repository configuration", current)
		}
		if receiptBefore != nil {
			var after entity.RepositoryPriceReceipt
			if err := db.Take(&after, "request_id = ?", receiptBefore.RequestID).Error; err != nil || !reflect.DeepEqual(*receiptBefore, after) {
				t.Fatal("V49 changed persisted receipt", err)
			}
		}
	}
	// The receipt is global even when its actor and catalogue subjects disappeared.
	first := newReceipt()
	if err := db.Create(&first).Error; err != nil {
		t.Fatal(err)
	}
	other := first
	other.ActorID = "usr_v49_other"
	if err := db.Create(&other).Error; !errors.Is(err, gorm.ErrDuplicatedKey) {
		t.Fatal("global receipt UUID uniqueness", err)
	}
	for i, mutate := range []func(*entity.RepositoryPriceReceipt){func(r *entity.RepositoryPriceReceipt) { r.RequestID = "short" }, func(r *entity.RepositoryPriceReceipt) { r.RequestHash = strings.Repeat("a", 63) }, func(r *entity.RepositoryPriceReceipt) { r.SourceDigest = strings.Repeat("b", 63) }, func(r *entity.RepositoryPriceReceipt) { r.ReviewETag = strings.Repeat("c", 63) }, func(r *entity.RepositoryPriceReceipt) { r.Mode = "unknown" }, func(r *entity.RepositoryPriceReceipt) { r.ConfigDigest = strings.Repeat("f", 63) }, func(r *entity.RepositoryPriceReceipt) { r.CatalogueDigest = nil }, func(r *entity.RepositoryPriceReceipt) { r.Mode = "configure" }} {
		row := first
		row.RequestID = "49000000-2222-4222-8222-22222222222" + string(rune('0'+i))
		mutate(&row)
		if err := db.Create(&row).Error; err == nil {
			t.Fatal("invalid receipt accepted", i)
		}
	}
	for _, row := range []any{&entity.RepositoryPriceSetting{ID: 2, ETag: strings.Repeat("e", 64)}, &entity.RepositoryPriceMapping{ProviderModelID: "", SourceModelKey: "source/model"}, &entity.RepositoryPriceMapping{ProviderModelID: "pmo_v49_bad", SourceModelKey: ""}} {
		if err := db.Create(row).Error; err == nil {
			t.Fatal("invalid configuration accepted", row)
		}
	}
	if err := db.Model(&entity.RepositoryPriceSetting{}).Where("id = ?", 1).UpdateColumn("etag", strings.Repeat("e", 63)).Error; err == nil {
		t.Fatal("short config ETag accepted")
	}
	key := "source/model"
	if err := db.Model(&entity.PriceRate{}).Where("id = ?", rate.ID).UpdateColumn("repository_model_key", key).Error; err == nil {
		t.Fatal("partial rate provenance accepted")
	}
	if err := db.Model(&entity.PriceRate{}).Where("id = ?", rate.ID).Updates(map[string]any{"repository_model_key": key, "repository_rate_key": "source/input"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Table("schema_migrations").Where("version = ?", 49).Delete(&struct{}{}).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	var followed entity.ModelPrice
	if err := db.Take(&followed, "id = ?", price.ID).Error; err != nil || !followed.FollowRepository {
		t.Fatal("aggregate ownership not derived from actual complete rate provenance", err)
	}
}
