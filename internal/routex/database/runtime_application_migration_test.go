package database

import (
	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm/schema"
	"reflect"
	"sync"
	"testing"
)

func TestRuntimeApplicationV87FrozenSchemaAndPrefix(t *testing.T) {
	f, err := schema.Parse(&runtimeRoutingApplicationV87{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	c, err := schema.Parse(&entity.RuntimeRoutingApplication{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	if f.Table != "runtime_routing_applications" || len(f.Fields) != 7 || len(c.Fields) != 7 {
		t.Fatal("unexpected public schema")
	}
	for name, field := range f.FieldsByName {
		current := c.FieldsByName[name]
		if current == nil || field.DBName != current.DBName || field.Tag.Get("gorm") != current.Tag.Get("gorm") || field.FieldType != current.FieldType || field.AutoCreateTime != 0 || field.AutoUpdateTime != 0 {
			t.Fatal("frozen proof changed", name)
		}
	}
	indexes := f.ParseIndexes()
	seen := map[string]bool{}
	for _, idx := range indexes {
		seen[idx.Name] = true
		if idx.Name == "idx_runtime_routing_application" && (idx.Class != "UNIQUE" || len(idx.Fields) != 2) {
			t.Fatal("process/snapshot uniqueness absent")
		}
	}
	if !seen["idx_runtime_routing_application"] || !seen["idx_runtime_routing_application_instance"] {
		t.Fatal("bounded read indexes absent")
	}
	for _, driver := range []string{"postgres", "mysql"} {
		steps := migrationSteps(driver)
		if len(steps) != 94 || reflect.ValueOf(steps[93]).Pointer() != reflect.ValueOf(oidcMigration).Pointer() || reflect.ValueOf(steps[92]).Pointer() != reflect.ValueOf(credentialAttemptStatisticsMigration).Pointer() || reflect.ValueOf(steps[91]).Pointer() != reflect.ValueOf(connectionTransportMigration).Pointer() || reflect.ValueOf(steps[90]).Pointer() != reflect.ValueOf(providerEnablementMigration).Pointer() || reflect.ValueOf(steps[89]).Pointer() != reflect.ValueOf(modelWeightHistoryMigration).Pointer() || reflect.ValueOf(steps[88]).Pointer() != reflect.ValueOf(credentialSourceClosedMigration).Pointer() || reflect.ValueOf(steps[86]).Pointer() != reflect.ValueOf(runtimeApplicationMigration).Pointer() || reflect.ValueOf(steps[85]).Pointer() != reflect.ValueOf(modelRecordedMetadataMigration).Pointer() || reflect.ValueOf(steps[87]).Pointer() != reflect.ValueOf(credentialSourceDrainMigration).Pointer() {
			t.Fatal("V87 prefix altered")
		}
	}
}
