package database

import (
	"reflect"
	"sync"
	"testing"

	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm/schema"
)

func TestFrozenQuotaWarningCandidateSchema(t *testing.T) {
	if quotaWarningMigrationCandidateVersion != 59 || reflect.ValueOf(quotaWarningMigration).Kind() != reflect.Func {
		t.Fatal("private candidate seam changed")
	}
	pairs := [][2]any{{&quotaWarningObservationV59{}, &entity.QuotaWarningObservation{}}, {&quotaWarningInboxV59{}, &entity.QuotaWarningInbox{}}}
	for _, pair := range pairs {
		frozen, err := schema.Parse(pair[0], &sync.Map{}, schema.NamingStrategy{})
		if err != nil {
			t.Fatal(err)
		}
		current, err := schema.Parse(pair[1], &sync.Map{}, schema.NamingStrategy{})
		if err != nil {
			t.Fatal(err)
		}
		if frozen.Table != current.Table || len(frozen.Relationships.Relations) != 0 || len(frozen.PrimaryFields) != 1 {
			t.Fatal("unexpected FK or source identity")
		}
		if len(frozen.Fields) != len(current.Fields) {
			t.Fatal("frozen/current drift")
		}
		for _, field := range frozen.Fields {
			other := current.FieldsByDBName[field.DBName]
			if other == nil || field.Size != other.Size || field.NotNull != other.NotNull || !reflect.DeepEqual(field.TagSettings, other.TagSettings) {
				t.Fatal("frozen tags changed", field.DBName)
			}
			if field.DBName != "id" && field.DBName != "read_at" && !field.NotNull {
				t.Fatal("nullable immutable snapshot", field.DBName)
			}
		}
		if frozen.Table == "quota_warning_observations" {
			checks := frozen.ParseCheckConstraints()
			for _, name := range []string{"ck_quota_warning_owner", "ck_quota_warning_dimension", "ck_quota_warning_calendar", "ck_quota_warning_currency", "ck_quota_warning_level", "ck_quota_warning_generation"} {
				if _, ok := checks[name]; !ok {
					t.Fatal("missing guard", name)
				}
			}
			indexes := frozen.ParseIndexes()
			if len(indexes) != 1 || indexes[0].Class != "UNIQUE" || len(indexes[0].Fields) != 8 {
				t.Fatal("warning level/generation dedup missing")
			}
			if frozen.FieldsByDBName["limit_value"].Size != 40 || frozen.FieldsByDBName["settled_value"].Size != 80 {
				t.Fatal("decimal bound changed")
			}
		}
	}
}
