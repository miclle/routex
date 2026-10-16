package database

import (
	"reflect"
	"testing"

	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm/schema"
)

func TestRuntimeArchiveRollbackV116FrozenIntentAndProtection(t *testing.T) {
	frozen, current := oidcV94Schema(t, &runtimeArchiveRollbackV116{}), oidcV94Schema(t, &entity.RuntimeArchiveRollback{})
	if frozen.Table != "runtime_archive_rollbacks" || frozen.Table != current.Table || len(frozen.DBNames) != len(current.DBNames) {
		t.Fatal("rollback frozen identity drift")
	}
	for _, name := range frozen.DBNames {
		a, b := frozen.FieldsByDBName[name], current.FieldsByDBName[name]
		if b == nil || a.FieldType != b.FieldType || a.Tag.Get("gorm") != b.Tag.Get("gorm") || a.Tag.Get("json") != "-" || b.Tag.Get("json") != "-" {
			t.Fatal("private frozen intent drift", name)
		}
	}
	for _, pair := range []struct{ name, table, column, reference string }{{"Archive", "runtime_configuration_archives", "archive_id", "id"}, {"Publication", "runtime_publication_commands", "request_id", "request_id"}} {
		r := frozen.Relationships.Relations[pair.name]
		if r == nil || r.Type != schema.BelongsTo {
			t.Fatal("rollback protection must be child-owned", pair.name)
		}
		c := r.ParseConstraint()
		if c == nil || c.Schema.Table != frozen.Table || c.ReferenceSchema.Table != pair.table || len(c.ForeignKeys) != 1 || c.ForeignKeys[0].DBName != pair.column || len(c.References) != 1 || c.References[0].DBName != pair.reference || c.OnDelete != "RESTRICT" || c.OnUpdate != "RESTRICT" {
			t.Fatal("rollback FK direction/lifetime", pair.name)
		}
	}
	if len(frozen.ParseCheckConstraints()) != 3 {
		t.Fatal("missing bounded original intent")
	}
	for _, driver := range []string{"postgres", "mysql"} {
		steps := migrationSteps(driver)
		if len(steps) != 118 || reflect.ValueOf(steps[114]).Pointer() != reflect.ValueOf(runtimeArchiveCleanupMigration).Pointer() || reflect.ValueOf(steps[115]).Pointer() != reflect.ValueOf(runtimeArchiveRollbackMigration).Pointer() {
			t.Fatal("V116 is not an exact additive suffix")
		}
	}
}
