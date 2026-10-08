package handler

import (
	"context"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
)

func testModelRecordedMetadataMigration(t *testing.T, db *gorm.DB) {
	var baseline []int
	if err := db.Table("schema_migrations").Order("version").Pluck("version", &baseline).Error; err != nil {
		t.Fatal(err)
	}
	if len(baseline) < 86 || baseline[85] != 86 {
		t.Fatal("V86 must follow the complete immutable migration prefix")
	}
	without := append(append([]int{}, baseline[:85]...), baseline[86:]...)
	ledger := func(want []int) {
		t.Helper()
		var got []int
		if err := db.Table("schema_migrations").Order("version").Pluck("version", &got).Error; err != nil || !reflect.DeepEqual(got, want) {
			t.Fatal("unrelated migration ledger changed", err)
		}
	}
	remove := func() {
		t.Helper()
		q := db.Table("schema_migrations").Where("version = ?", 86).Delete(&struct{}{})
		if q.Error != nil || q.RowsAffected != 1 {
			t.Fatal("V86 ledger removal failed", q.Error)
		}
		ledger(without)
	}
	migrate := func() {
		t.Helper()
		var wg sync.WaitGroup
		failures := make(chan error, 2)
		for range 2 {
			wg.Go(func() { failures <- database.Migrate(context.Background(), db) })
		}
		wg.Wait()
		close(failures)
		for err := range failures {
			if err != nil {
				t.Fatal(err)
			}
		}
		ledger(baseline)
		if err := database.Migrate(context.Background(), db); err != nil {
			t.Fatal(err)
		}
		ledger(baseline)
	}
	birth := time.Date(2026, 10, 1, 0, 0, 0, 123000000, time.UTC)
	model := entity.Model{ID: "mdl_metadata_history", Status: entity.ResourceActive, CreatedAt: birth}
	if err := db.Create(&model).Error; err != nil {
		t.Fatal(err)
	}
	name := entity.ModelName{Name: "recorded-history", ModelID: model.ID, CurrentModelID: &model.ID}
	if err := db.Create(&name).Error; err != nil {
		t.Fatal(err)
	}
	type originalModel struct {
		ID, Status string
		CreatedAt  time.Time
	}
	readOriginal := func() originalModel {
		t.Helper()
		var got originalModel
		if err := db.Table("models").Select("id", "status", "created_at").Take(&got, "id = ?", model.ID).Error; err != nil {
			t.Fatal(err)
		}
		got.CreatedAt = got.CreatedAt.UTC()
		return got
	}
	original := readOriginal()
	if !original.CreatedAt.Equal(birth) {
		t.Fatal("historical birth fixture did not persist exactly")
	}
	if err := db.Migrator().DropColumn(&entity.Model{}, "ConfigUpdatedAt"); err != nil {
		t.Fatal(err)
	}
	remove()
	if readOriginal() != original {
		t.Fatal("removing V86 changed original Model")
	}
	migrate()
	var got entity.Model
	read := func() {
		t.Helper()
		// GORM leaves a reused pointer-time field unchanged when scanning SQL NULL.
		got = entity.Model{}
		if err := db.Take(&got, "id = ?", model.ID).Error; err != nil {
			t.Fatal(err)
		}
		if readOriginal() != original {
			t.Fatal("migration changed original Model identity or configuration")
		}
		var names []entity.ModelName
		if err := db.Where("model_id = ?", model.ID).Find(&names).Error; err != nil || len(names) != 1 || names[0].Name != name.Name || names[0].CurrentModelID == nil || *names[0].CurrentModelID != model.ID {
			t.Fatal("migration changed historical names", err)
		}
	}
	read()
	if got.ConfigUpdatedAt != nil {
		t.Fatal("migration invented a historical configuration timestamp")
	}
	recorded := birth.Add(time.Second).Add(456 * time.Microsecond)
	q := db.Model(&entity.Model{}).Where("id = ?", model.ID).UpdateColumn("config_updated_at", recorded)
	if q.Error != nil || q.RowsAffected != 1 {
		t.Fatal("recorded timestamp write failed", q.Error)
	}
	read()
	if got.ConfigUpdatedAt == nil || !got.ConfigUpdatedAt.Equal(recorded) {
		t.Fatal("microsecond configuration timestamp did not round-trip")
	}
	// Resume MySQL's potentially committed DDL without clearing recorded data.
	remove()
	migrate()
	read()
	if got.ConfigUpdatedAt == nil || !got.ConfigUpdatedAt.Equal(recorded) {
		t.Fatal("partial migration replay replaced recorded timestamp")
	}
	if err := db.Model(&entity.Model{}).Where("id = ?", model.ID).UpdateColumn("config_updated_at", nil).Error; err != nil {
		t.Fatal(err)
	}
	var nullCount int64
	if err := db.Model(&entity.Model{}).Where("id = ? AND config_updated_at IS NULL", model.ID).Count(&nullCount).Error; err != nil || nullCount != 1 {
		t.Fatal("legacy unknown timestamp is not persisted SQL NULL", err)
	}
	read()
	if got.ConfigUpdatedAt != nil {
		t.Fatal("legacy unknown timestamp cannot remain null")
	}
}
