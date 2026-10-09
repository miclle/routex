package database

import (
	"reflect"
	"sync"
	"testing"

	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm/schema"
)

func TestCredentialSourceDrainV88FrozenSchemaAndPrefix(t *testing.T) {
	for _, pair := range [][2]any{{&credentialSourceProcessV88{}, &entity.CredentialSourceProcess{}}, {&credentialSourceDenialV88{}, &entity.CredentialSourceDenial{}}, {&credentialSourceUseV88{}, &entity.CredentialSourceUse{}}, {&credentialPublishedCleanupV88{}, &entity.CredentialPublishedCleanup{}}} {
		f, err := schema.Parse(pair[0], &sync.Map{}, schema.NamingStrategy{})
		if err != nil {
			t.Fatal(err)
		}
		c, err := schema.Parse(pair[1], &sync.Map{}, schema.NamingStrategy{})
		if err != nil {
			t.Fatal(err)
		}
		extra := 0
		if f.Table == "credential_source_processes" {
			extra = 1
			if c.FieldsByName["ClosedAt"] == nil || f.FieldsByName["ClosedAt"] != nil {
				t.Fatal("V89 must be additive to frozen V88")
			}
		}
		if f.Table != c.Table || len(f.Fields)+extra != len(c.Fields) {
			t.Fatal("frozen tables changed")
		}
		for name, field := range f.FieldsByName {
			current := c.FieldsByName[name]
			if current == nil || field.DBName != current.DBName || field.Tag.Get("gorm") != current.Tag.Get("gorm") || field.FieldType != current.FieldType || field.AutoCreateTime != 0 || field.AutoUpdateTime != 0 {
				t.Fatal("frozen proof changed", name)
			}
		}
	}
	for _, driver := range []string{"postgres", "mysql"} {
		steps := migrationSteps(driver)
		if len(steps) != 91 || reflect.ValueOf(steps[90]).Pointer() != reflect.ValueOf(providerEnablementMigration).Pointer() || reflect.ValueOf(steps[89]).Pointer() != reflect.ValueOf(modelWeightHistoryMigration).Pointer() || reflect.ValueOf(steps[88]).Pointer() != reflect.ValueOf(credentialSourceClosedMigration).Pointer() || reflect.ValueOf(steps[86]).Pointer() != reflect.ValueOf(runtimeApplicationMigration).Pointer() || reflect.ValueOf(steps[87]).Pointer() != reflect.ValueOf(credentialSourceDrainMigration).Pointer() {
			t.Fatal("V88 append changed released prefix")
		}
	}
	f, err := schema.Parse(&credentialSourceDenialV88{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	indexes := f.ParseIndexes()
	seen := map[string]bool{}
	for _, index := range indexes {
		if index.Class == "UNIQUE" {
			seen[index.Name] = true
		}
	}
	if !seen["idx_credential_source_denial_creation"] || !seen["idx_credential_source_denial_command"] {
		t.Fatal("one-command ownership constraints missing")
	}
}
