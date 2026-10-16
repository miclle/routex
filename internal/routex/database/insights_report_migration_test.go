package database

import (
	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm/schema"
	"reflect"
	"testing"
)

func TestInsightsReportV110FrozenAppendAndPrivateReceipt(t *testing.T) {
	frozen := oidcV94Schema(t, &insightsReportV110{})
	current := oidcV94Schema(t, &entity.InsightsReport{})
	if frozen.Table != "insights_reports" || current.Table != frozen.Table || len(frozen.DBNames) != 10 || len(current.DBNames) != 13 {
		t.Fatal("bounded saved report schema")
	}
	for _, name := range frozen.DBNames {
		a, b := frozen.FieldsByDBName[name], current.FieldsByDBName[name]
		if b == nil || a.FieldType != b.FieldType || a.Size != b.Size || a.Precision != b.Precision || a.PrimaryKey != b.PrimaryKey || a.AutoCreateTime != 0 || a.AutoUpdateTime != 0 {
			t.Fatal("frozen saved report mismatch", name)
		}
	}
	for _, name := range []string{"dimension", "scope_kind", "scope_id"} {
		if frozen.FieldsByDBName[name] != nil || current.FieldsByDBName[name] == nil {
			t.Fatal("only explicit quota fields may extend current schema", name)
		}
	}
	for _, driver := range []string{"postgres", "mysql"} {
		steps := migrationSteps(driver)
		if len(steps) != 118 || reflect.ValueOf(steps[108]).Pointer() != reflect.ValueOf(credentialInferenceFailureMigration).Pointer() || reflect.ValueOf(steps[109]).Pointer() != reflect.ValueOf(insightsReportMigration).Pointer() {
			t.Fatal("V110 must append")
		}
	}
	checks := frozen.ParseCheckConstraints()
	if len(checks) != 3 || checks["ck_insights_report_definition"].Constraint == "" || len(frozen.Relationships.Relations) != 0 {
		t.Fatal("private cleared receipt constraints")
	}
	for _, name := range []string{"range_from", "range_to"} {
		if frozen.FieldsByDBName[name].DataType != schema.String || frozen.FieldsByDBName[name].Size != 30 {
			t.Fatal("instants must remain exact strings")
		}
	}
}
