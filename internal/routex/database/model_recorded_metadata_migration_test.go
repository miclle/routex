package database

import (
	"reflect"
	"sync"
	"testing"

	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm/schema"
)

func TestModelRecordedMetadataV86FrozenNullableAppend(t *testing.T) {
	frozen, err := schema.Parse(&modelRecordedMetadataV86{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	current, err := schema.Parse(&entity.Model{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	f, c := frozen.FieldsByName["ConfigUpdatedAt"], current.FieldsByName["ConfigUpdatedAt"]
	if frozen.Table != "models" || len(frozen.Fields) != 2 || f.DBName != c.DBName || f.Tag.Get("gorm") != c.Tag.Get("gorm") || f.FieldType != c.FieldType || f.NotNull || f.AutoCreateTime != 0 || f.AutoUpdateTime != 0 || f.HasDefaultValue {
		t.Fatal("V86 must add only a nullable explicit recorded timestamp without historical defaults")
	}
	for _, dialect := range []string{"postgres", "mysql"} {
		steps := migrationSteps(dialect)
		if len(steps) < 86 || reflect.ValueOf(steps[85]).Pointer() != reflect.ValueOf(modelRecordedMetadataMigration).Pointer() || reflect.ValueOf(steps[84]).Pointer() != reflect.ValueOf(projectKeyRollingQuotaWarningMigration).Pointer() {
			t.Fatal("V86 must follow the immutable V85 prefix")
		}
	}
}
