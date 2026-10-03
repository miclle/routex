package database

import (
	"reflect"
	"strings"
	"sync"
	"testing"

	"gorm.io/gorm/schema"

	"github.com/miclle/routex/internal/routex/entity"
)

func TestFrozenDefaultLimitSchema(t *testing.T) {
	parsed, err := schema.Parse(&defaultLimitRuleV42{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Table != "default_limit_rules" || len(parsed.PrimaryFields) != 1 || parsed.PrimaryFields[0].DBName != "kind" || len(parsed.Relationships.Relations) != 0 {
		t.Fatal("creation templates need one exact kind identity and no live relationships")
	}
	checks := parsed.ParseCheckConstraints()
	if len(checks) != 10 || !strings.Contains(checks["ck_default_limit_kind"].Constraint, "'user','team'") {
		t.Fatal("frozen template domain is incomplete", checks)
	}
	for _, name := range []string{"Tokens5H", "Tokens7D", "TokensMonth", "TPM", "RPM", "Concurrency"} {
		field := parsed.LookUpField(name)
		if field == nil || field.NotNull || field.HasDefaultValue || field.IndirectFieldType.Kind() != reflect.Int64 {
			t.Fatal("finite caps must preserve null, zero and exact safe integers", name)
		}
	}
	// Numeric suffixes and ETag are not GORM initialisms. The physical columns
	// must match the frozen checks and persistence expressions, independently
	// of the Go field names and public JSON policy names.
	for _, model := range []any{&defaultLimitRuleV42{}, &entity.DefaultLimitRule{}} {
		columns, err := schema.Parse(model, &sync.Map{}, schema.NamingStrategy{})
		if err != nil {
			t.Fatal(err)
		}
		for field, expected := range map[string]string{"Tokens5H": "tokens_5h", "Tokens7D": "tokens_7d", "RuleETag": "rule_etag", "PreviousETag": "previous_etag"} {
			if actual := columns.LookUpField(field); actual == nil || actual.DBName != expected {
				t.Fatal("template column does not match frozen checks", field, expected)
			}
		}
	}
	for _, name := range []string{"RuleETag", "PreviousETag"} {
		field := parsed.LookUpField(name)
		if field == nil || field.Size != 64 || field.NotNull != (name == "RuleETag") {
			t.Fatal("template revisions need bounded nullable predecessor semantics", name)
		}
	}
	provenance, err := schema.Parse(&resourceDefaultProvenanceV42{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	if len(provenance.Fields) != 4 || len(provenance.Relationships.Relations) != 0 || len(provenance.ParseCheckConstraints()) != 2 {
		t.Fatal("additive provenance must not redefine evolving live policy columns")
	}
	for _, name := range []string{"AppliedDefaultETag", "DefaultResetETag"} {
		field := provenance.LookUpField(name)
		if field.Size != 64 || field.NotNull || field.HasDefaultValue {
			t.Fatal("legacy policies must retain unknown default provenance", name)
		}
	}
	currentPolicy, err := schema.Parse(&entity.ResourceLimit{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	for field, column := range map[string]string{"AppliedDefaultETag": "applied_default_etag", "DefaultResetETag": "default_reset_etag"} {
		if provenance.LookUpField(field).DBName != column || currentPolicy.LookUpField(field).DBName != column {
			t.Fatal("provenance column does not match additive guards", field, column)
		}
	}
	for field, column := range map[string]string{"ETag": "e_tag", "PreviousETag": "previous_e_tag", "Tokens5H": "tokens5_h", "Tokens7D": "tokens7_d"} {
		if currentPolicy.LookUpField(field).DBName != column {
			t.Fatal("V42 renamed a released resource-policy column", field, column)
		}
	}
	for _, dialect := range []string{"postgres", "mysql"} {
		steps := migrationSteps(dialect)
		if len(steps) < 42 || reflect.ValueOf(steps[41]).Pointer() != reflect.ValueOf(defaultLimitMigration).Pointer() {
			t.Fatal("creation templates must remain at immutable V42", dialect)
		}
	}
}
