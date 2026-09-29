package handler

import (
	"context"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
)

type callMediaPricingV24Integration struct {
	RequestID   string `gorm:"primaryKey;size:64"`
	ImageInputs *int64
	PDFInputs   *int64
}

func (callMediaPricingV24Integration) TableName() string { return "call_records" }

func testCallMediaPricingMigration(t *testing.T, db *gorm.DB) {
	t.Helper()
	now := time.Now().UTC()
	legacy := entity.CallRecord{
		SnapshotID:      "cfg_media_migration",
		RequestID:       "req_media_migration",
		UserID:          "usr_media_migration",
		KeyID:           "key_media_migration",
		ModelID:         "mdl_media_migration",
		ModelName:       "media-migration",
		ProviderModelID: "pmd_media_migration",
		ConnectionID:    "con_media_migration",
		Protocol:        entity.ProtocolOpenAIChat,
		Status:          "success",
		StartedAt:       now,
		CompletedAt:     now,
	}
	if err := db.Create(&legacy).Error; err != nil {
		t.Fatal(err)
	}

	model := &callMediaPricingV24Integration{}
	for _, field := range []string{"ImageInputs", "PDFInputs"} {
		if err := db.Migrator().DropColumn(model, field); err != nil {
			t.Fatal(err)
		}
	}
	removeCallMediaPricingMigrationLedger(t, db)
	if err := database.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	assertCallMediaPricingSchema(t, db)
	var upgraded entity.CallRecord
	if err := db.First(&upgraded, "request_id = ?", legacy.RequestID).Error; err != nil {
		t.Fatal(err)
	}
	if upgraded.ImageInputs != nil || upgraded.PDFInputs != nil {
		t.Fatalf("legacy media counts became known: image=%v pdf=%v", upgraded.ImageInputs, upgraded.PDFInputs)
	}

	images := int64(2)
	if err := db.Model(model).Where("request_id = ?", legacy.RequestID).Update("image_inputs", images).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Migrator().DropColumn(model, "PDFInputs"); err != nil {
		t.Fatal(err)
	}
	removeCallMediaPricingMigrationLedger(t, db)
	if err := database.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	assertCallMediaPricingSchema(t, db)
	upgraded = entity.CallRecord{}
	if err := db.First(&upgraded, "request_id = ?", legacy.RequestID).Error; err != nil || upgraded.ImageInputs == nil || *upgraded.ImageInputs != images || upgraded.PDFInputs != nil {
		t.Fatalf("partial media migration changed facts: %+v err=%v", upgraded, err)
	}

	pdfs := int64(1)
	if err := db.Model(model).Where("request_id = ?", legacy.RequestID).Update("pdf_inputs", pdfs).Error; err != nil {
		t.Fatal(err)
	}
	removeCallMediaPricingMigrationLedger(t, db)
	if err := database.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	upgraded = entity.CallRecord{}
	if err := db.First(&upgraded, "request_id = ?", legacy.RequestID).Error; err != nil || upgraded.ImageInputs == nil || *upgraded.ImageInputs != images || upgraded.PDFInputs == nil || *upgraded.PDFInputs != pdfs {
		t.Fatalf("repeated media migration changed facts: %+v err=%v", upgraded, err)
	}
}

func removeCallMediaPricingMigrationLedger(t *testing.T, db *gorm.DB) {
	t.Helper()
	result := db.Table("schema_migrations").Where("version = ?", 24).Delete(&struct{}{})
	if result.Error != nil || result.RowsAffected != 1 {
		t.Fatalf("remove migration 24 ledger: affected=%d err=%v", result.RowsAffected, result.Error)
	}
}

func assertCallMediaPricingSchema(t *testing.T, db *gorm.DB) {
	t.Helper()
	model := &callMediaPricingV24Integration{}
	for _, field := range []string{"ImageInputs", "PDFInputs"} {
		if !db.Migrator().HasColumn(model, field) {
			t.Fatalf("call media pricing migration did not restore %s", field)
		}
	}
}
