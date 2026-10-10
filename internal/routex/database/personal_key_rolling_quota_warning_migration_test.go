package database

import (
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm/schema"
)

func TestPersonalKeyRollingWarningV82FrozenSchemaAndAppend(t *testing.T) {
	for _, pair := range []struct{ frozen, current any }{{&personalKeyRollingQuotaWarningStateV82{}, &entity.PersonalKeyRollingQuotaWarningState{}}, {&personalKeyRollingQuotaWarningObservationV82{}, &entity.PersonalKeyRollingQuotaWarningObservation{}}, {&personalKeyRollingQuotaWarningInboxV82{}, &entity.PersonalKeyRollingQuotaWarningInbox{}}} {
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
		if f.Table == "personal_key_rolling_quota_warning_observations" {
			generation := f.ParseCheckConstraints()["ck_pkrqw_generation"].Constraint
			if !strings.Contains(generation, "OCTET_LENGTH(threshold_generation) = 29") || !strings.Contains(generation, "ASCII(SUBSTRING(threshold_generation,29,1)) = 49") {
				t.Fatal("exact generation constraint missing")
			}
			for _, name := range []string{"ck_pkrqw_root", "ck_pkrqw_name", "ck_pkrqw_window_time"} {
				if _, ok := f.ParseCheckConstraints()[name]; !ok {
					t.Fatal("required Key constraint missing", name)
				}
			}
			amount := f.ParseCheckConstraints()["ck_pkrqw_amount"].Constraint
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
		if len(steps) != 100 || reflect.ValueOf(steps[96]).Pointer() != reflect.ValueOf(samlMigration).Pointer() || reflect.ValueOf(steps[95]).Pointer() != reflect.ValueOf(ldapMigration).Pointer() || reflect.ValueOf(steps[94]).Pointer() != reflect.ValueOf(oauthMigration).Pointer() || reflect.ValueOf(steps[93]).Pointer() != reflect.ValueOf(oidcMigration).Pointer() || reflect.ValueOf(steps[92]).Pointer() != reflect.ValueOf(credentialAttemptStatisticsMigration).Pointer() || reflect.ValueOf(steps[91]).Pointer() != reflect.ValueOf(connectionTransportMigration).Pointer() || reflect.ValueOf(steps[90]).Pointer() != reflect.ValueOf(providerEnablementMigration).Pointer() || reflect.ValueOf(steps[89]).Pointer() != reflect.ValueOf(modelWeightHistoryMigration).Pointer() || reflect.ValueOf(steps[88]).Pointer() != reflect.ValueOf(credentialSourceClosedMigration).Pointer() || reflect.ValueOf(steps[83]).Pointer() != reflect.ValueOf(teamRollingQuotaWarningMigration).Pointer() || reflect.ValueOf(steps[82]).Pointer() != reflect.ValueOf(projectRollingQuotaWarningMigration).Pointer() || reflect.ValueOf(steps[81]).Pointer() != reflect.ValueOf(personalKeyRollingQuotaWarningMigration).Pointer() || reflect.ValueOf(steps[79]).Pointer() != reflect.ValueOf(providerCredentialCleanupMigration).Pointer() || reflect.ValueOf(steps[80]).Pointer() != reflect.ValueOf(personalRollingQuotaWarningMigration).Pointer() || reflect.ValueOf(steps[84]).Pointer() != reflect.ValueOf(projectKeyRollingQuotaWarningMigration).Pointer() || reflect.ValueOf(steps[85]).Pointer() != reflect.ValueOf(modelRecordedMetadataMigration).Pointer() || reflect.ValueOf(steps[86]).Pointer() != reflect.ValueOf(runtimeApplicationMigration).Pointer() || reflect.ValueOf(steps[87]).Pointer() != reflect.ValueOf(credentialSourceDrainMigration).Pointer() || reflect.ValueOf(steps[97]).Pointer() != reflect.ValueOf(namedIdentityMigration).Pointer() || reflect.ValueOf(steps[98]).Pointer() != reflect.ValueOf(googleIdentityMigration).Pointer() || reflect.ValueOf(steps[99]).Pointer() != reflect.ValueOf(runtimeInstallationMigration).Pointer() {
			t.Fatal("exact81 append required")
		}
	}
}
