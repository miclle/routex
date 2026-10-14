package database

import (
	"reflect"
	"strings"
	"testing"

	"github.com/miclle/routex/internal/routex/entity"
)

func TestModelDiscoveryV108FrozenSchemaAndClosedEnvelope(t *testing.T) {
	frozen := oidcV94Schema(t, &modelDiscoveryNoticeV108{})
	current := oidcV94Schema(t, &entity.ModelDiscoveryNotice{})
	if frozen.Table != current.Table || len(frozen.DBNames) != 6 || len(current.DBNames) != 6 {
		t.Fatal("discovery source schema changed")
	}
	for _, name := range frozen.DBNames {
		a, b := frozen.FieldsByDBName[name], current.FieldsByDBName[name]
		if b == nil || a.FieldType != b.FieldType || a.Size != b.Size || a.Precision != b.Precision || a.PrimaryKey != b.PrimaryKey || a.AutoCreateTime != 0 || a.AutoUpdateTime != 0 || a.HasDefaultValue {
			t.Fatal("unbounded discovery column", name)
		}
	}
	for _, driver := range []string{"postgres", "mysql"} {
		steps := migrationSteps(driver)
		if len(steps) != 116 || reflect.ValueOf(steps[106]).Pointer() != reflect.ValueOf(offboardingNotificationMigration).Pointer() || reflect.ValueOf(steps[107]).Pointer() != reflect.ValueOf(modelDiscoveryNoticeMigration).Pointer() {
			t.Fatal("discovery migration must append after V107")
		}
	}
	relation := frozen.Relationships.Relations["Audit"]
	if relation == nil {
		t.Fatal("missing retained discovery audit reference")
	}
	fk := relation.ParseConstraint()
	if fk == nil || fk.OnDelete != "RESTRICT" || fk.OnUpdate != "RESTRICT" {
		t.Fatal("discovery audit must remain restrictive")
	}
	for _, item := range []struct {
		model  any
		check  string
		legacy any
	}{
		{&modelDiscoveryAlertV108{}, "ck_operational_alerts_kind", &offboardingAlertV107{}},
		{&modelDiscoveryOccurrenceV108{}, "ck_alert_occurrences_source", &offboardingOccurrenceV107{}},
	} {
		check := oidcV94Schema(t, item.model).ParseCheckConstraints()[item.check].Constraint
		legacy := oidcV94Schema(t, item.legacy).ParseCheckConstraints()[item.check].Constraint
		if !strings.Contains(check, "("+legacy+") AND first_recorded_model_count = 0") || !strings.Contains(check, "ASCII(SUBSTRING(") || !strings.Contains(check, "BETWEEN 1 AND 2000") {
			t.Fatal("discovery weakened historical envelope")
		}
	}
	checks := frozen.ParseCheckConstraints()
	if len(checks) != 5 || !strings.Contains(checks["ck_model_discovery_count"].Constraint, "BETWEEN 1 AND 2000") {
		t.Fatal("discovery bounded source constraints missing")
	}
	if !strings.Contains(oidcV94Schema(t, &modelDiscoveryDeliveryV108{}).ParseCheckConstraints()["ck_notification_delivery_no_discovery"].Constraint, "models_first_recorded") {
		t.Fatal("inbox-only delivery constraint missing")
	}
}
