package database

import (
	"reflect"
	"testing"

	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm/schema"
)

func TestRuntimePublicationAcknowledgementV117FrozenReferences(t *testing.T) {
	frozen, current := oidcV94Schema(t, &runtimePublicationAcknowledgementV117{}), oidcV94Schema(t, &entity.RuntimePublicationAcknowledgement{})
	if frozen.Table != "runtime_publication_acknowledgements" || frozen.Table != current.Table || len(frozen.DBNames) != 3 || len(current.DBNames) != 3 {
		t.Fatal("unexpected acknowledgement shape")
	}
	for _, name := range frozen.DBNames {
		a, b := frozen.FieldsByDBName[name], current.FieldsByDBName[name]
		if b == nil || a.FieldType != b.FieldType || a.Tag.Get("gorm") != b.Tag.Get("gorm") || a.Tag.Get("json") != "-" || b.Tag.Get("json") != "-" {
			t.Fatal("frozen acknowledgement drift", name)
		}
	}
	for _, pair := range []struct{ name, table, column, reference string }{{"Command", "runtime_publication_commands", "request_id", "request_id"}, {"Installation", "runtime_installation_observations", "installation_id", "id"}} {
		r := frozen.Relationships.Relations[pair.name]
		if r == nil || r.Type != schema.BelongsTo {
			t.Fatal("reference ownership", pair.name)
		}
		c := r.ParseConstraint()
		if c == nil || c.Schema.Table != frozen.Table || c.ReferenceSchema.Table != pair.table || len(c.ForeignKeys) != 1 || c.ForeignKeys[0].DBName != pair.column || len(c.References) != 1 || c.References[0].DBName != pair.reference || c.OnDelete != "RESTRICT" || c.OnUpdate != "RESTRICT" {
			t.Fatal("immutable reference direction", pair.name)
		}
	}
	for _, driver := range []string{"postgres", "mysql"} {
		steps := migrationSteps(driver)
		if len(steps) < 117 || reflect.ValueOf(steps[115]).Pointer() != reflect.ValueOf(runtimeArchiveRollbackMigration).Pointer() || reflect.ValueOf(steps[116]).Pointer() != reflect.ValueOf(runtimePublicationAcknowledgementMigration).Pointer() {
			t.Fatal("V117 must be an additive suffix")
		}
	}
}
