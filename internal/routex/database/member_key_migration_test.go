package database

import (
	"reflect"
	"sync"
	"testing"

	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm/schema"
)

func TestMemberKeyRevisionFrozenMigrationHasBoundedPrivateSchema(t *testing.T) {
	frozen, err := schema.Parse(&memberKeyRevisionV52{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	current, err := schema.Parse(&entity.APIKey{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	if frozen.Table != "api_keys" || len(frozen.Fields) != 2 || len(frozen.Relationships.Relations) != 0 {
		t.Fatal("revision migration acquired unrelated schema")
	}
	for _, name := range []string{"ID", "LifecycleRevision"} {
		a, b := frozen.LookUpField(name), current.LookUpField(name)
		if a == nil || b == nil || a.Tag != b.Tag || a.FieldType != b.FieldType {
			t.Fatal("frozen revision column diverged", name)
		}
	}
	revision := frozen.LookUpField("LifecycleRevision")
	if revision.Size != 30 || !revision.NotNull || !revision.HasDefaultValue || revision.DefaultValue != "" {
		t.Fatal("partial upgrade no longer permits bounded stable backfill")
	}
	permissions, err := schema.Parse(&memberKeyPermissionV52{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	if permissions.Table != "role_permissions" || len(permissions.Fields) != 2 || len(permissions.PrimaryFields) != 2 || len(permissions.Relationships.Relations) != 0 {
		t.Fatal("permission migration acquired mutable schema")
	}
	if reflect.ValueOf(memberKeyMigration).IsNil() {
		t.Fatal("missing registry-callable migration")
	}
}
