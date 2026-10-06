package handler

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"sync"
	"testing"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
)

// This private schema declares only a disposable fixture index. InnoDB may
// replace its implicit FK-supporting index with V69's covering reverse index,
// so the fixture must retain another covering key while simulating old DDL.
type providerBindingsFixtureFKIndex struct {
	ProviderModelID string `gorm:"size:30;index:idx_binding_fixture_fk"`
}

func (providerBindingsFixtureFKIndex) TableName() string { return "model_provider_bindings" }

func dropProviderBindingsFixtureIndex(t *testing.T, db *gorm.DB) {
	t.Helper()
	if !db.Migrator().HasIndex(&entity.ModelProviderBinding{}, providerBindingsFixtureIndex) {
		t.Fatal("reverse index missing before fixture removal")
	}
	if db.Name() == "postgres" {
		// Pinned GORM DropIndex emits invalid CURRENT_SCHEMA().index SQL. This
		// fixed fixture-only identifier follows the established adapter exception;
		// production V69 creation and interrupted migration repair remain GORM.
		if err := db.Exec("DROP INDEX idx_bindings_provider_model").Error; err != nil {
			t.Fatal(err)
		}
	} else {
		frozen := &providerBindingsFixtureFKIndex{}
		if db.Migrator().HasIndex(frozen, "idx_binding_fixture_fk") {
			t.Fatal("unexpected fixture FK index before removal")
		}
		if err := db.Migrator().CreateIndex(frozen, "idx_binding_fixture_fk"); err != nil {
			t.Fatal(err)
		}
		if err := db.Migrator().DropIndex(&entity.ModelProviderBinding{}, providerBindingsFixtureIndex); err != nil {
			t.Fatal(err)
		}
		if !db.Migrator().HasIndex(frozen, "idx_binding_fixture_fk") {
			t.Fatal("fixture removal lost FK-supporting index")
		}
	}
	if db.Migrator().HasIndex(&entity.ModelProviderBinding{}, providerBindingsFixtureIndex) {
		t.Fatal("reverse index not removed")
	}
}
func removeProviderBindingsFixtureFKIndex(t *testing.T, db *gorm.DB) {
	t.Helper()
	frozen := &providerBindingsFixtureFKIndex{}
	if !db.Migrator().HasIndex(frozen, "idx_binding_fixture_fk") {
		return
	}
	// V69 must restore the complete physical key before this temporary index is
	// removed; the original foreign key and pair constraints are never disabled.
	assertProviderBindingsFixtureIndex(t, db)
	if err := db.Migrator().DropIndex(frozen, "idx_binding_fixture_fk"); err != nil {
		t.Fatal(err)
	}
	if db.Migrator().HasIndex(frozen, "idx_binding_fixture_fk") {
		t.Fatal("temporary fixture index retained")
	}
}

func assertProviderBindingsFixtureIndex(t *testing.T, db *gorm.DB) {
	t.Helper()
	if !db.Migrator().HasIndex(&entity.ModelProviderBinding{}, providerBindingsFixtureIndex) {
		t.Fatal("binding reverse index absent")
	}
	indexes, err := db.Migrator().GetIndexes(&entity.ModelProviderBinding{})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, index := range indexes {
		if index.Name() == providerBindingsFixtureIndex {
			found = true
			if unique, known := index.Unique(); !known || unique {
				t.Fatal("reverse index must be explicitly nonunique")
			}
			if db.Name() == "mysql" && !slices.Equal(index.Columns(), []string{"provider_model_id", "model_id", "id"}) {
				t.Fatal("reverse key physical order", index.Columns())
			}
		}
	}
	if !found {
		t.Fatal("reverse index metadata missing")
	}
	if db.Name() == "postgres" {
		// Pinned GORM GetIndexes lacks PostgreSQL key ordinality. This static,
		// parameterized catalog assertion follows the existing migration fixtures.
		var columns []string
		if err := db.Raw(`SELECT a.attname FROM pg_index i
   JOIN pg_class x ON x.oid=i.indexrelid
   JOIN pg_class t ON t.oid=i.indrelid
   JOIN pg_namespace n ON n.oid=t.relnamespace
   CROSS JOIN LATERAL unnest(i.indkey) WITH ORDINALITY AS k(attnum,position)
   JOIN pg_attribute a ON a.attrelid=t.oid AND a.attnum=k.attnum
   WHERE n.nspname=current_schema() AND t.relname=? AND x.relname=?
   ORDER BY k.position`, "model_provider_bindings", providerBindingsFixtureIndex).Scan(&columns).Error; err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(columns, []string{"provider_model_id", "model_id", "id"}) {
			t.Fatal("reverse key physical order", columns)
		}
	}
}

// The harness creates a fresh database with V69 first. This scenario then
// replays only V69 over retained data, including interrupted MySQL DDL states.
func testProviderModelBindingIndexMigration(t *testing.T, db *gorm.DB) {
	var baseline []int
	if err := db.Table("schema_migrations").Order("version").Pluck("version", &baseline).Error; err != nil {
		t.Fatal(err)
	}
	without := []int{}
	own := 0
	for i, version := range baseline {
		if i > 0 && version <= baseline[i-1] {
			t.Fatal("migration ledger not ordered unique")
		}
		if version == 69 {
			own++
		} else {
			without = append(without, version)
		}
	}
	if own != 1 {
		t.Fatal("V69 not registered exactly once")
	}
	ledger := func(want []int) {
		t.Helper()
		var got []int
		if err := db.Table("schema_migrations").Order("version").Pluck("version", &got).Error; err != nil || !slices.Equal(got, want) {
			t.Fatal("unrelated migration ledger changed", err, got, want)
		}
	}
	remove := func() {
		t.Helper()
		q := db.Table("schema_migrations").Where("version = ?", 69).Delete(&struct{}{})
		if q.Error != nil || q.RowsAffected != 1 {
			t.Fatal("remove own ledger only", q.Error, q.RowsAffected)
		}
		ledger(without)
	}
	migrate := func() {
		t.Helper()
		if err := database.Migrate(context.Background(), db); err != nil {
			t.Fatal(err)
		}
		ledger(baseline)
		assertProviderBindingsFixtureIndex(t, db)
		removeProviderBindingsFixtureFKIndex(t, db)
	}
	concurrent := func() {
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
		assertProviderBindingsFixtureIndex(t, db)
		removeProviderBindingsFixtureFKIndex(t, db)
	}
	assertProviderBindingsFixtureIndex(t, db)
	p, c, pm := seedProviderBindingsFixture(t, db, "index_history")
	model := entity.Model{ID: "mdl_index_history", Status: "disabled", CreatedAt: p.CreatedAt}
	if err := db.Create(&model).Error; err != nil {
		t.Fatal(err)
	}
	current := entity.ModelName{Name: "index:history", ModelID: model.ID, CurrentModelID: &model.ID, CreatedAt: p.CreatedAt}
	binding := entity.ModelProviderBinding{ID: "bnd_index_history", ModelID: model.ID, ProviderModelID: pm.ID, Weight: 0, CreatedAt: p.CreatedAt}
	for _, row := range []any{&current, &binding} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	type retained struct {
		Provider   entity.Provider
		Connection entity.ProviderConnection
		Supply     entity.ProviderModel
		Model      entity.Model
		Name       entity.ModelName
		Binding    entity.ModelProviderBinding
	}
	read := func() retained {
		t.Helper()
		var got retained
		for _, query := range []struct {
			row        any
			key, value string
		}{{&got.Provider, "id", p.ID}, {&got.Connection, "id", c.ID}, {&got.Supply, "id", pm.ID}, {&got.Model, "id", model.ID}, {&got.Name, "name", current.Name}, {&got.Binding, "id", binding.ID}} {
			if err := db.Where(query.key+" = ?", query.value).Take(query.row).Error; err != nil {
				t.Fatal(err)
			}
		}
		return got
	}
	before := read()
	unchanged := func() {
		t.Helper()
		if !reflect.DeepEqual(read(), before) {
			t.Fatal("index migration changed retained identities/names/disabled state/weights")
		}
	}
	// Pre-index upgrade, repeat, recorded-DDL/unrecorded-ledger resume, and
	// concurrent startup from both absent and already-created index states.
	dropProviderBindingsFixtureIndex(t, db)
	if db.Migrator().HasIndex(&entity.ModelProviderBinding{}, providerBindingsFixtureIndex) {
		t.Fatal("fault index not removed")
	}
	remove()
	migrate()
	unchanged()
	migrate()
	unchanged()
	remove()
	concurrent()
	unchanged()
	dropProviderBindingsFixtureIndex(t, db)
	remove()
	concurrent()
	unchanged()
	migrate()
	unchanged()
	// Adding this nonunique reverse index must not alter the released pair/FK
	// constraints or forbid independent logical Models bound to the same supply.
	second := entity.Model{ID: "mdl_index_second", Status: "active", CreatedAt: p.CreatedAt}
	if err := db.Create(&second).Error; err != nil {
		t.Fatal(err)
	}
	next := entity.ModelProviderBinding{ID: "bnd_index_second", ModelID: second.ID, ProviderModelID: pm.ID, Weight: 100, CreatedAt: p.CreatedAt}
	if err := db.Create(&next).Error; err != nil {
		t.Fatal("reverse index incorrectly unique", err)
	}
	duplicate := binding
	duplicate.ID = "bnd_index_duplicate"
	if err := db.Create(&duplicate).Error; !errors.Is(err, gorm.ErrDuplicatedKey) {
		t.Fatal("released binding uniqueness lost", err)
	}
	orphan := next
	orphan.ID = "bnd_index_orphan"
	orphan.ModelID = "mdl_index_missing"
	if err := db.Create(&orphan).Error; !errors.Is(err, gorm.ErrForeignKeyViolated) {
		t.Fatal("released binding foreign key lost", err)
	}
	unchanged()
	ledger(baseline)
}
