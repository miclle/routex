package database

import (
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm/schema"
)

func TestPersonalRollingWarningV81FrozenSchemaAndAppend(t *testing.T) {
	for _, pair := range []struct{ frozen, current any }{{&personalRollingQuotaWarningStateV81{}, &entity.PersonalRollingQuotaWarningState{}}, {&personalRollingQuotaWarningObservationV81{}, &entity.PersonalRollingQuotaWarningObservation{}}, {&personalRollingQuotaWarningInboxV81{}, &entity.PersonalRollingQuotaWarningInbox{}}} {
		f, e := schema.Parse(pair.frozen, &sync.Map{}, schema.NamingStrategy{})
		if e != nil {
			t.Fatal(e)
		}
		c, e := schema.Parse(pair.current, &sync.Map{}, schema.NamingStrategy{})
		if e != nil {
			t.Fatal(e)
		}
		if f.Table != c.Table || len(f.Fields) != len(c.Fields) {
			t.Fatal("frozen shape changed")
		}
		for _, field := range f.Fields {
			other := c.FieldsByName[field.Name]
			if other == nil || field.DBName != other.DBName || field.Tag != other.Tag || field.FieldType != other.FieldType {
				t.Fatal("frozen column mismatch", field.Name)
			}
		}
		if f.Table == "personal_rolling_quota_warning_observations" {
			amount := f.ParseCheckConstraints()["ck_prqw_amount"].Constraint
			if strings.Contains(amount, "settled_value < limit_value") || !strings.Contains(amount, "settled_value >= 0") || !strings.Contains(amount, "settled_value <= 9223372036854775807") || !strings.Contains(amount, "limit_value > 0") {
				t.Fatal("rolling amount constraint suppresses100+ or loses int64 bounds")
			}
			if f.FieldsByName["Settled"].FieldType.Kind() != reflect.Int64 || f.FieldsByName["Limit"].FieldType.Kind() != reflect.Int64 {
				t.Fatal("rolling amount storage is not signed int64")
			}
		}
		if v := f.FieldsByName["WindowKind"]; v != nil {
			check := f.ParseCheckConstraints()
			found := false
			for _, x := range check {
				found = found || strings.Contains(x.Constraint, "OCTET_LENGTH(window_kind) = 2") && strings.Contains(x.Constraint, "ASCII(SUBSTRING(window_kind,2,1)) = 104") && strings.Contains(x.Constraint, "ASCII(SUBSTRING(window_kind,2,1)) = 100")
			}
			if !found {
				t.Fatal("window enum is not case exact across collations")
			}
		}
	}
	for _, dialect := range []string{"postgres", "mysql"} {
		steps := migrationSteps(dialect)
		if len(steps) != 90 || reflect.ValueOf(steps[89]).Pointer() != reflect.ValueOf(modelWeightHistoryMigration).Pointer() || reflect.ValueOf(steps[88]).Pointer() != reflect.ValueOf(credentialSourceClosedMigration).Pointer() || reflect.ValueOf(steps[83]).Pointer() != reflect.ValueOf(teamRollingQuotaWarningMigration).Pointer() || reflect.ValueOf(steps[82]).Pointer() != reflect.ValueOf(projectRollingQuotaWarningMigration).Pointer() || reflect.ValueOf(steps[81]).Pointer() != reflect.ValueOf(personalKeyRollingQuotaWarningMigration).Pointer() || reflect.ValueOf(steps[79]).Pointer() != reflect.ValueOf(providerCredentialCleanupMigration).Pointer() || reflect.ValueOf(steps[80]).Pointer() != reflect.ValueOf(personalRollingQuotaWarningMigration).Pointer() || reflect.ValueOf(steps[84]).Pointer() != reflect.ValueOf(projectKeyRollingQuotaWarningMigration).Pointer() || reflect.ValueOf(steps[85]).Pointer() != reflect.ValueOf(modelRecordedMetadataMigration).Pointer() || reflect.ValueOf(steps[86]).Pointer() != reflect.ValueOf(runtimeApplicationMigration).Pointer() || reflect.ValueOf(steps[87]).Pointer() != reflect.ValueOf(credentialSourceDrainMigration).Pointer() {
			t.Fatal("exact81 append required")
		}
	}
}
