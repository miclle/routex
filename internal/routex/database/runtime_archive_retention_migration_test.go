package database

import (
	"reflect"
	"testing"

	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm/schema"
)

func TestRuntimeArchiveV113FrozenRetentionAndProvenance(t *testing.T) {
	for _, pair := range [][2]any{{&runtimeArchiveRetentionV113{}, &entity.RuntimeArchiveRetention{}}, {&runtimeArchiveContentV113{}, &entity.RuntimeArchiveContent{}}, {&runtimeArchiveCaptureV113{}, &entity.RuntimeArchiveCapture{}}} {
		frozen, current := oidcV94Schema(t, pair[0]), oidcV94Schema(t, pair[1])
		if frozen.Table != current.Table || len(frozen.DBNames) != len(current.DBNames) {
			t.Fatal("frozen bounded archive state")
		}
		for _, name := range frozen.DBNames {
			a, b := frozen.FieldsByDBName[name], current.FieldsByDBName[name]
			if b == nil || a.FieldType != b.FieldType || a.Tag.Get("gorm") != b.Tag.Get("gorm") || a.Tag.Get("json") != "-" || b.Tag.Get("json") != "-" {
				t.Fatal("private schema drift", name)
			}
		}
		if frozen.Table != "runtime_archive_retention" {
			relation := frozen.Relationships.Relations["Archive"]
			if relation == nil || relation.Type != schema.BelongsTo {
				t.Fatal("retained target FK")
			}
			fk := relation.ParseConstraint()
			if fk.ReferenceSchema.Table != "runtime_configuration_archives" || fk.OnDelete != "RESTRICT" || fk.OnUpdate != "RESTRICT" {
				t.Fatal("history must not cascade")
			}
		}
	}
	for _, driver := range []string{"postgres", "mysql"} {
		steps := migrationSteps(driver)
		if len(steps) != 118 || reflect.ValueOf(steps[112]).Pointer() != reflect.ValueOf(runtimeArchiveRetentionMigration).Pointer() || reflect.ValueOf(steps[111]).Pointer() != reflect.ValueOf(runtimeConfigurationArchiveMigration).Pointer() {
			t.Fatal("immutable V113 append")
		}
	}
}
