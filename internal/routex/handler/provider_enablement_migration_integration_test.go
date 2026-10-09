package handler

import (
	"context"
	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
	"reflect"
	"sync"
	"testing"
)

type providerEnabledV91Fixture struct {
	Enabled bool   `gorm:"not null;default:true"`
	ETag    string `gorm:"size:30;not null;default:0"`
}

func (providerEnabledV91Fixture) TableName() string { return "providers" }

type providerEnabledBadV91Fixture struct {
	Enabled bool `gorm:"not null;default:false"`
}

func (providerEnabledBadV91Fixture) TableName() string { return "providers" }
func testProviderEnablementMigration(t *testing.T, db *gorm.DB) {
	ctx := context.Background()
	before := personalKeyBehaviorLedger(t, db)
	if len(before) != 91 || before[90].Version != 91 || before[89].Version != 90 {
		t.Fatal("exact current V91 ledger")
	}
	for i, row := range before {
		if row.Version != i+1 {
			t.Fatal("noncontiguous retained ledger")
		}
	}
	original := entity.Provider{ID: "prv_v91_retained", Name: "Retained"}
	if err := db.Create(&original).Error; err != nil {
		t.Fatal(err)
	}
	read := func() entity.Provider {
		t.Helper()
		var row entity.Provider
		if err := db.Session(&gorm.Session{QueryFields: true}).Where("id = ?", original.ID).Take(&row).Error; err != nil {
			t.Fatal(err)
		}
		return row
	}
	original = read()
	if !original.Enabled || original.ETag != "0" {
		t.Fatal("new Provider default", original)
	}
	remove := func() {
		t.Helper()
		result := db.Table("schema_migrations").Where("version = ?", 91).Delete(&struct{}{})
		if result.Error != nil || result.RowsAffected != 1 {
			t.Fatal("remove only V91", result.Error)
		}
	}
	migrate := func() {
		t.Helper()
		if err := database.Migrate(ctx, db); err != nil {
			t.Fatal(err)
		}
	}
	for _, field := range []string{"Enabled", "ETag"} {
		if err := db.Migrator().DropColumn(&providerEnabledV91Fixture{}, field); err != nil {
			t.Fatal(err)
		}
	}
	remove()
	migrate()
	if !reflect.DeepEqual(original, read()) {
		t.Fatal("existing-data upgrade rewrote prior Provider facts")
	}
	if err := db.Model(&entity.Provider{}).Where("id = ?", original.ID).Updates(map[string]any{"enabled": false, "ETag": "rev_retained"}).Error; err != nil {
		t.Fatal(err)
	}
	stopped := read()
	if stopped.Enabled || stopped.ETag != "rev_retained" {
		t.Fatal("explicit false/revision not persisted")
	}
	// Committed MySQL DDL can outlive its ledger write; replay cannot reset data.
	remove()
	migrate()
	migrate()
	if !reflect.DeepEqual(stopped, read()) {
		t.Fatal("partial-DDL replay rewrote stored status")
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		wg.Go(func() { errs <- database.Migrate(ctx, db) })
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if !personalKeyBehaviorLedgerPreserved(before, personalKeyBehaviorLedger(t, db), 91) {
		t.Fatal("unrelated ledger changed")
	}
	// Each independently surviving column is reconciled without altering the other.
	if err := db.Migrator().DropColumn(&providerEnabledV91Fixture{}, "ETag"); err != nil {
		t.Fatal(err)
	}
	remove()
	migrate()
	if read().Enabled || read().ETag != "0" {
		t.Fatal("revision-only partial upgrade altered explicit false")
	}
	if err := db.Model(&entity.Provider{}).Where("id = ?", original.ID).Update("ETag", "rev_preserved").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Migrator().DropColumn(&providerEnabledV91Fixture{}, "Enabled"); err != nil {
		t.Fatal(err)
	}
	remove()
	migrate()
	if !read().Enabled || read().ETag != "rev_preserved" {
		t.Fatal("enabled-only upgrade lost surviving revision")
	}
	if err := db.Migrator().AlterColumn(&providerEnabledBadV91Fixture{}, "Enabled"); err != nil {
		t.Fatal(err)
	}
	remove()
	defer func() {
		if err := db.Migrator().AlterColumn(&providerEnabledV91Fixture{}, "Enabled"); err != nil {
			t.Error(err)
			return
		}
		if err := database.Migrate(ctx, db); err != nil {
			t.Error(err)
		}
	}()
	if err := database.Migrate(ctx, db); err == nil {
		t.Fatal("wrong surviving default accepted")
	}
	var n int64
	if err := db.Table("schema_migrations").Where("version = ?", 91).Count(&n).Error; err != nil || n != 0 {
		t.Fatal("invalid shape fabricated ledger", err, n)
	}
	if read().ETag != "rev_preserved" {
		t.Fatal("validation failure rewrote revision")
	}
}
