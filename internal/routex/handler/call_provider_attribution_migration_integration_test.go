package handler

import (
	"context"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
)

type callProviderAttributionV26Integration struct {
	RequestID         string    `gorm:"column:request_id;primaryKey;size:64;index:idx_calls_provider_time,priority:3"`
	ProviderID        string    `gorm:"size:30;not null;default:'';index:idx_calls_provider_time,priority:1"`
	ProviderName      string    `gorm:"size:100;not null;default:''"`
	ConnectionName    string    `gorm:"size:100;not null;default:''"`
	UpstreamModelName string    `gorm:"size:255;not null;default:''"`
	StartedAt         time.Time `gorm:"precision:6;not null;index:idx_calls_provider_time,priority:2"`
}

func (callProviderAttributionV26Integration) TableName() string { return "call_records" }

func testCallProviderAttributionMigration(t *testing.T, db *gorm.DB) {
	t.Helper()
	now := time.Now().UTC()
	legacy := entity.CallRecord{
		SnapshotID:      "cfg_provider_migration",
		RequestID:       "req_provider_migration",
		UserID:          "usr_provider_migration",
		KeyID:           "key_provider_migration",
		ModelID:         "mdl_provider_migration",
		ModelName:       "provider-migration",
		ProviderModelID: "pmd_provider_migration",
		ConnectionID:    "con_provider_migration",
		Protocol:        entity.ProtocolOpenAIChat,
		Status:          "success",
		StartedAt:       now,
		CompletedAt:     now,
	}
	if err := db.Create(&legacy).Error; err != nil {
		t.Fatal(err)
	}

	model := &callProviderAttributionV26Integration{}
	for _, field := range []string{"ProviderID", "ProviderName", "ConnectionName", "UpstreamModelName"} {
		if err := db.Migrator().DropColumn(model, field); err != nil {
			t.Fatal(err)
		}
	}
	removeCallProviderAttributionMigrationLedger(t, db)
	if err := database.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	assertCallProviderAttributionSchema(t, db)
	var upgraded callProviderAttributionV26Integration
	if err := db.Select("request_id", "provider_id", "provider_name", "connection_name", "upstream_model_name", "started_at").First(&upgraded, "request_id = ?", legacy.RequestID).Error; err != nil {
		t.Fatal(err)
	}
	if upgraded.ProviderID != "" || upgraded.ProviderName != "" || upgraded.ConnectionName != "" || upgraded.UpstreamModelName != "" {
		t.Fatalf("legacy call gained provider attribution: %+v", upgraded)
	}

	values := map[string]any{
		"provider_id":         "prv_historical",
		"provider_name":       "Historical Provider",
		"connection_name":     "Historical Connection",
		"upstream_model_name": "historical-model",
	}
	if err := db.Model(model).Where("request_id = ?", legacy.RequestID).Updates(values).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Migrator().DropColumn(model, "ConnectionName"); err != nil {
		t.Fatal(err)
	}
	removeCallProviderAttributionMigrationLedger(t, db)
	if err := database.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	assertCallProviderAttributionSchema(t, db)
	upgraded = callProviderAttributionV26Integration{}
	if err := db.Select("request_id", "provider_id", "provider_name", "connection_name", "upstream_model_name", "started_at").First(&upgraded, "request_id = ?", legacy.RequestID).Error; err != nil {
		t.Fatal(err)
	}
	if upgraded.ProviderID != "prv_historical" || upgraded.ProviderName != "Historical Provider" || upgraded.ConnectionName != "" || upgraded.UpstreamModelName != "historical-model" {
		t.Fatalf("partial provider attribution migration changed preserved facts: %+v", upgraded)
	}

	removeCallProviderAttributionMigrationLedger(t, db)
	if err := database.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	assertCallProviderAttributionSchema(t, db)
}

func removeCallProviderAttributionMigrationLedger(t *testing.T, db *gorm.DB) {
	t.Helper()
	result := db.Table("schema_migrations").Where("version = ?", 26).Delete(&struct{}{})
	if result.Error != nil || result.RowsAffected != 1 {
		t.Fatalf("remove migration 26 ledger: affected=%d err=%v", result.RowsAffected, result.Error)
	}
}

func assertCallProviderAttributionSchema(t *testing.T, db *gorm.DB) {
	t.Helper()
	model := &callProviderAttributionV26Integration{}
	wantLengths := map[string]int64{"ProviderID": 30, "ProviderName": 100, "ConnectionName": 100, "UpstreamModelName": 255}
	columns, err := db.Migrator().ColumnTypes(model)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, column := range columns {
		for field, length := range wantLengths {
			if column.Name() != db.NamingStrategy.ColumnName("", field) {
				continue
			}
			seen[field] = true
			if nullable, ok := column.Nullable(); !ok || nullable {
				t.Fatalf("%s must be non-null, nullable=%v known=%v", field, nullable, ok)
			}
			if actual, ok := column.Length(); !ok || actual != length {
				t.Fatalf("%s length=%d known=%v, want %d", field, actual, ok, length)
			}
		}
	}
	for field := range wantLengths {
		if !seen[field] {
			t.Fatalf("call provider attribution migration did not restore %s", field)
		}
	}
	if !db.Migrator().HasIndex(model, "idx_calls_provider_time") {
		t.Fatal("call provider attribution migration did not restore idx_calls_provider_time")
	}
}
