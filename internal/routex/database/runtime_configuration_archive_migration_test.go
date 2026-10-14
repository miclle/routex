package database

import (
	"reflect"
	"strings"
	"testing"

	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm/schema"
)

func TestRuntimeArchiveV112FrozenSchemaAndForeignKey(t *testing.T) {
	for _, pair := range [][2]any{{&runtimeConfigurationArchiveV112{}, &entity.RuntimeConfigurationArchive{}}, {&runtimeConfigurationArchiveChunkV112{}, &entity.RuntimeConfigurationArchiveChunk{}}} {
		frozen, current := oidcV94Schema(t, pair[0]), oidcV94Schema(t, pair[1])
		if frozen.Table != current.Table || len(frozen.DBNames) != len(current.DBNames) {
			t.Fatal("frozen archive boundary")
		}
		for _, name := range frozen.DBNames {
			a, b := frozen.FieldsByDBName[name], current.FieldsByDBName[name]
			if b == nil || a.FieldType != b.FieldType || a.Tag.Get("gorm") != b.Tag.Get("gorm") || a.Tag.Get("json") != "-" || b.Tag.Get("json") != "-" {
				t.Fatal("private frozen archive parity", name)
			}
		}
	}
	header := oidcV94Schema(t, &runtimeConfigurationArchiveV112{})
	chunk := oidcV94Schema(t, &runtimeConfigurationArchiveChunkV112{})
	if len(header.DBNames) != 10 || len(chunk.DBNames) != 7 || len(header.ParseCheckConstraints()) != 3 || len(chunk.ParseCheckConstraints()) != 2 {
		t.Fatal("bounded header/chunk schemas")
	}
	relation := chunk.Relationships.Relations["Archive"]
	if relation == nil || relation.Type != schema.BelongsTo {
		t.Fatal("chunk must belong to header")
	}
	fk := relation.ParseConstraint()
	if fk.Schema.Table != chunk.Table || fk.ReferenceSchema.Table != header.Table || len(fk.ForeignKeys) != 1 || fk.ForeignKeys[0].DBName != "archive_id" || fk.References[0].DBName != "id" || fk.OnDelete != "RESTRICT" || fk.OnUpdate != "RESTRICT" {
		t.Fatal("archive FK direction and lifecycle")
	}
	indexes := chunk.ParseIndexes()
	if len(indexes) != 1 || indexes[0].Name != "idx_runtime_archive_chunk_order" || indexes[0].Class != "UNIQUE" || len(indexes[0].Fields) != 2 || indexes[0].Fields[0].DBName != "archive_id" || indexes[0].Fields[1].DBName != "ordinal" {
		t.Fatal("ordered unique chunks")
	}
	bounds := header.ParseCheckConstraints()["ck_runtime_archive_bounds"].Constraint
	for _, required := range []string{"codec_version = 1", "plaintext_bytes <= 16777216", "chunk_count <= 512", "plaintext_bytes > (chunk_count - 1) * 32768", "plaintext_bytes <= chunk_count * 32768"} {
		if !strings.Contains(bounds, required) {
			t.Fatal("missing portable bound", required)
		}
	}
	if strings.Contains(bounds, " / ") {
		t.Fatal("driver-dependent integer division")
	}
}

func TestRuntimeArchiveV112RootGuardsAreExplicitAdditions(t *testing.T) {
	for _, tc := range []struct {
		old, next, current any
		name, suffix       string
	}{
		{&telegramIdentityRootJobV106{}, &runtimeArchiveRootJobV112{}, &entity.SecretRotationJob{}, "ck_secret_inventory_version", " OR inventory_version = 11"},
		{&telegramIdentityRootJobV106{}, &runtimeArchiveRootJobV112{}, &entity.SecretRotationJob{}, "ck_secret_rotation_domain", " OR (inventory_version = 11 AND domain >= 0 AND domain <= 13)"},
		{&telegramIdentityRootProcessV106{}, &runtimeArchiveRootProcessV112{}, &entity.SecretProcessVerification{}, "ck_secret_process_inventory_version", " OR inventory_version = 11"},
	} {
		before, after := oidcV94Check(t, tc.old, tc.name), oidcV94Check(t, tc.next, tc.name)
		if after != before+tc.suffix || oidcV94Check(t, tc.current, tc.name) != after {
			t.Fatal("historical inventory rewritten", tc.name)
		}
	}
	before := oidcV94Check(t, &apiKeyDeliveryRootItemV102{}, "ck_secret_item_domain")
	after := oidcV94Check(t, &runtimeArchiveRootItemV112{}, "ck_secret_item_domain")
	if after != "("+before+") OR ("+oidcV94ASCII("domain", "runtime_config_chunks")+")" || after != oidcV94Check(t, &entity.SecretRotationItem{}, "ck_secret_item_domain") {
		t.Fatal("root domain exact historical prefix")
	}
	for _, driver := range []string{"postgres", "mysql"} {
		steps := migrationSteps(driver)
		if len(steps) != 116 || reflect.ValueOf(steps[111]).Pointer() != reflect.ValueOf(runtimeConfigurationArchiveMigration).Pointer() || reflect.ValueOf(steps[110]).Pointer() != reflect.ValueOf(insightsQuotaReportMigration).Pointer() {
			t.Fatal("immutable migration append")
		}
	}
}
