package database

import (
	"reflect"
	"sync"
	"testing"

	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm/schema"
)

func TestFrozenPersonalKeyQuotaWarningCandidateSchema(t *testing.T) {
	if personalKeyQuotaWarningMigrationCandidateVersion != 64 || reflect.ValueOf(personalKeyQuotaWarningMigration).Kind() != reflect.Func {
		t.Fatal("private candidate seam changed")
	}
	pairs := [][2]any{{&personalKeyQuotaWarningObservationV64{}, &entity.PersonalKeyQuotaWarningObservation{}}, {&personalKeyQuotaWarningInboxV64{}, &entity.PersonalKeyQuotaWarningInbox{}}}
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
		if frozen.Table == "personal_key_quota_warning_observations" {
			checks := frozen.ParseCheckConstraints()
			for _, name := range []string{"ck_personal_key_quota_warning_root", "ck_personal_key_quota_warning_owner", "ck_personal_key_quota_warning_owner_birth", "ck_personal_key_quota_warning_dimension", "ck_personal_key_quota_warning_calendar", "ck_personal_key_quota_warning_currency", "ck_personal_key_quota_warning_level", "ck_personal_key_quota_warning_generation"} {
				if _, ok := checks[name]; !ok {
					t.Fatal("missing guard", name)
				}
			}
			indexes := frozen.ParseIndexes()
			if len(indexes) != 1 || indexes[0].Class != "UNIQUE" || len(indexes[0].Fields) != 10 {
				t.Fatal("warning level/generation dedup missing")
			}
			if frozen.FieldsByDBName["limit_value"].Size != 40 || frozen.FieldsByDBName["settled_value"].Size != 80 {
				t.Fatal("decimal bound changed")
			}
		}
	}
}

func TestPersonalKeyWarningOwnerIndexFrozenCandidate(t *testing.T) {
	s, err := schema.Parse(&personalKeyWarningOwnerIndexV64{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	if s.Table != "api_keys" || len(s.Fields) != 2 || len(s.Relationships.Relations) != 0 {
		t.Fatal("index stub modifies Key schema")
	}
	indexes := s.ParseIndexes()
	if len(indexes) != 1 || indexes[0].Name != "idx_personal_key_warning_owner" || indexes[0].Class != "" || len(indexes[0].Fields) != 2 || indexes[0].Fields[0].DBName != "user_id" || indexes[0].Fields[1].DBName != "id" {
		t.Fatal("owner candidate index order")
	}
}
