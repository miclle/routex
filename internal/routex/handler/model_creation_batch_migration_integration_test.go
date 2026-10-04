package handler

import (
	"context"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
)

// These private fixture shapes reconstruct only the unreleased V50 boundary.
// Historical receipt pointers deliberately do not acquire live foreign keys.
type modelBatchReceiptV50Fixture struct {
	RequestID    string    `gorm:"primaryKey;size:36;check:ck_model_creation_batch_intent,CHAR_LENGTH(request_id) = 36 AND CHAR_LENGTH(request_hash) = 64 AND CHAR_LENGTH(review_etag) = 64"`
	ActorID      string    `gorm:"size:30;not null;index:idx_model_creation_batch_actor"`
	ConnectionID string    `gorm:"size:30;not null;index:idx_model_creation_batch_connection"`
	RequestHash  string    `gorm:"size:64;not null"`
	ReviewETag   string    `gorm:"column:review_etag;size:64;not null"`
	SnapshotJSON string    `gorm:"type:text;not null"`
	CreatedAt    time.Time `gorm:"precision:6;not null"`
}

func (modelBatchReceiptV50Fixture) TableName() string { return "model_creation_batch_receipts" }

type modelBatchReceiptPartialV50Fixture struct {
	RequestID string `gorm:"primaryKey;size:36"`
}

func (modelBatchReceiptPartialV50Fixture) TableName() string { return "model_creation_batch_receipts" }

func testModelCreationBatchMigration(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx := context.Background()
	model := &modelBatchReceiptV50Fixture{}
	retained := entity.Provider{ID: "prv_batch_upgrade", Name: "Retained catalogue before V50"}
	if err := db.Create(&retained).Error; err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := db.Delete(&retained).Error; err != nil {
			t.Error(err)
		}
	}()
	removeLedger := func() {
		t.Helper()
		result := db.Table("schema_migrations").Where("version = ?", 50).Delete(&struct{}{})
		if result.Error != nil || result.RowsAffected != 1 {
			t.Fatal("cannot reconstruct only V50 boundary", result.Error, result.RowsAffected)
		}
	}
	check := func() {
		t.Helper()
		statement := &gorm.Statement{DB: db}
		if err := statement.Parse(model); err != nil {
			t.Fatal(err)
		}
		if !db.Migrator().HasTable(model) || !db.Migrator().HasConstraint(model, "ck_model_creation_batch_intent") {
			t.Fatal("missing bounded receipt schema")
		}
		for _, field := range statement.Schema.Fields {
			if !db.Migrator().HasColumn(model, field.DBName) {
				t.Fatal("missing physical receipt column", field.DBName)
			}
		}
		for _, index := range []string{"idx_model_creation_batch_actor", "idx_model_creation_batch_connection"} {
			if !db.Migrator().HasIndex(model, index) {
				t.Fatal("missing receipt index", index)
			}
		}
		if db.Migrator().HasColumn(model, "review_e_tag") || len(statement.Schema.Relationships.Relations) != 0 {
			t.Fatal("incorrect physical review column or live foreign key")
		}
		var current entity.Provider
		if err := db.Where("id = ?", retained.ID).Take(&current).Error; err != nil || current.Name != retained.Name {
			t.Fatal("V50 changed preexisting catalogue", current, err)
		}
	}
	for _, partial := range []bool{false, true} {
		if err := db.Migrator().DropTable(model); err != nil {
			t.Fatal(err)
		}
		removeLedger()
		if partial {
			if err := db.Migrator().CreateTable(&modelBatchReceiptPartialV50Fixture{}); err != nil {
				t.Fatal(err)
			}
		}
		var wg sync.WaitGroup
		results := make(chan error, 2)
		for range 2 {
			wg.Go(func() { results <- database.Migrate(ctx, db) })
		}
		wg.Wait()
		close(results)
		for err := range results {
			if err != nil {
				t.Fatal("concurrent V50 migration", partial, err)
			}
		}
		check()
		if err := database.Migrate(ctx, db); err != nil {
			t.Fatal("repeat V50", err)
		}
		check()
	}
	stamp := time.Now().UTC().Truncate(time.Microsecond)
	historical := modelBatchReceiptV50Fixture{RequestID: "50000000-0000-4000-8000-000000000001", ActorID: "usr_batch_deleted", ConnectionID: "con_batch_deleted", RequestHash: strings.Repeat("a", 64), ReviewETag: strings.Repeat("b", 64), SnapshotJSON: `{"historical":true}`, CreatedAt: stamp}
	if err := db.Create(&historical).Error; err != nil {
		t.Fatal("historical pointers acquired live dependencies", err)
	}
	if err := db.Where("request_id = ?", historical.RequestID).Take(&historical).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&historical).Error; err == nil {
		t.Fatal("receipt UUID uniqueness missing")
	}
	for index, mutate := range []func(*modelBatchReceiptV50Fixture){func(r *modelBatchReceiptV50Fixture) { r.RequestHash = "bad" }, func(r *modelBatchReceiptV50Fixture) { r.ReviewETag = "bad" }, func(r *modelBatchReceiptV50Fixture) { r.RequestID = "bad" }} {
		invalid := historical
		invalid.RequestID = []string{"50000000-0000-4000-8000-000000000002", "50000000-0000-4000-8000-000000000003", "50000000-0000-4000-8000-000000000004"}[index]
		mutate(&invalid)
		if err := db.Create(&invalid).Error; err == nil {
			t.Fatal("receipt intent check missing", index)
		}
	}
	if err := db.Model(model).Where("request_id = ?", historical.RequestID).Update("actor_id", nil).Error; err == nil {
		t.Fatal("required historical actor accepted NULL")
	}
	// Repair the real interrupted CHECK/index DDL while preserving committed history.
	if err := db.Migrator().DropConstraint(model, "ck_model_creation_batch_intent"); err != nil {
		t.Fatal(err)
	}
	if !db.Migrator().HasIndex(model, "idx_model_creation_batch_connection") {
		t.Fatal("receipt fault-injection index missing before removal")
	}
	if db.Name() == "postgres" {
		// The pinned PostgreSQL GORM DropIndex emits invalid CURRENT_SCHEMA().index
		// SQL. This fixed test-only statement reconstructs interrupted index DDL;
		// production V50 creation and repair remain entirely GORM-based.
		if err := db.Exec("DROP INDEX idx_model_creation_batch_connection").Error; err != nil {
			t.Fatal("remove receipt fault-injection index", err)
		}
	} else if err := db.Migrator().DropIndex(model, "idx_model_creation_batch_connection"); err != nil {
		t.Fatal("remove receipt fault-injection index", err)
	}
	if db.Migrator().HasIndex(model, "idx_model_creation_batch_connection") {
		t.Fatal("receipt fault injection did not remove index")
	}
	removeLedger()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal("interrupted DDL repair", err)
	}
	check()
	var actual modelBatchReceiptV50Fixture
	if err := db.Where("request_id = ?", historical.RequestID).Take(&actual).Error; err != nil || !reflect.DeepEqual(actual, historical) {
		t.Fatal("DDL repair rewrote receipt", actual, historical, err)
	}
	if err := db.Delete(&historical).Error; err != nil {
		t.Fatal(err)
	}
}
