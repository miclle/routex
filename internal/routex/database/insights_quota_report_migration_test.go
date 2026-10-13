package database

import (
	"strings"
	"testing"

	"github.com/miclle/routex/internal/routex/entity"
)

func TestInsightsQuotaReportV111FrozenDiscriminatedSchema(t *testing.T) {
	frozen := oidcV94Schema(t, &insightsReportV111{})
	current := oidcV94Schema(t, &entity.InsightsReport{})
	legacy := oidcV94Schema(t, &insightsReportV110{})
	if frozen.Table != "insights_reports" || len(frozen.DBNames) != 13 || len(current.DBNames) != 13 || len(legacy.DBNames) != 10 {
		t.Fatal("bounded frozen schema")
	}
	for _, name := range frozen.DBNames {
		a, b := frozen.FieldsByDBName[name], current.FieldsByDBName[name]
		if b == nil || a.FieldType != b.FieldType || a.Size != b.Size || a.Precision != b.Precision || a.PrimaryKey != b.PrimaryKey || a.DefaultValue != b.DefaultValue {
			t.Fatal("frozen quota report mismatch", name)
		}
	}
	for _, name := range legacy.DBNames {
		a, b := legacy.FieldsByDBName[name], frozen.FieldsByDBName[name]
		if a.FieldType != b.FieldType || a.Size != b.Size || a.Precision != b.Precision || a.PrimaryKey != b.PrimaryKey || a.DefaultValue != b.DefaultValue {
			t.Fatal("released column changed", name)
		}
	}
	checks := frozen.ParseCheckConstraints()
	if len(checks) != 3 || checks["ck_insights_report_definition_v111"].Constraint == "" || checks["ck_insights_report_definition"].Constraint != "" {
		t.Fatal("distinct complete discriminator guard")
	}
	check := checks["ck_insights_report_definition_v111"].Constraint
	for _, needle := range []string{"OCTET_LENGTH(dimension) = 0", "OCTET_LENGTH(scope_kind) = 0", "OCTET_LENGTH(scope_id) = 0", "OCTET_LENGTH(range_from) = 0", "OCTET_LENGTH(scope_id) BETWEEN 1 AND 30", "ASCII(SUBSTRING(dimension,", "ASCII(SUBSTRING(scope_kind,"} {
		if !strings.Contains(check, needle) {
			t.Fatal("missing portable branch guard", needle)
		}
	}
	if strings.Contains(legacy.ParseCheckConstraints()["ck_insights_report_definition"].Constraint, "dimension") {
		t.Fatal("released V110 guard changed")
	}
	oldIndexes, newIndexes := legacy.ParseIndexes(), frozen.ParseIndexes()
	if len(oldIndexes) != len(newIndexes) {
		t.Fatal("owner and intent indexes changed")
	}
	for _, old := range oldIndexes {
		found := false
		for _, next := range newIndexes {
			if old.Name != next.Name {
				continue
			}
			found = true
			if old.Class != next.Class || len(old.Fields) != len(next.Fields) {
				t.Fatal("index shape changed")
			}
			for j, field := range old.Fields {
				if field.DBName != next.Fields[j].DBName || field.Priority != next.Fields[j].Priority {
					t.Fatal("index order changed")
				}
			}
		}
		if !found {
			t.Fatal("retained index missing", old.Name)
		}
	}
	if frozen.FieldsByDBName["dimension"].Size != len("tokens") {
		t.Fatal("Tokens dimension must fit without truncation")
	}
	for _, name := range []string{"dimension", "scope_kind", "scope_id"} {
		field := frozen.FieldsByDBName[name]
		if !field.NotNull || !field.HasDefaultValue || field.DefaultValue != "" {
			t.Fatal("existing definitions need empty default", name)
		}
	}
}
