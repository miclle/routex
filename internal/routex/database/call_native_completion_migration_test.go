package database

import (
	"strings"
	"sync"
	"testing"

	"gorm.io/gorm/schema"
)

func TestFrozenCallNativeCompletionSchema(t *testing.T) {
	parsed, err := schema.Parse(&callNativeCompletionV33{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Table != "call_attempts" || len(parsed.Relationships.Relations) != 0 {
		t.Fatal("native completion must not introduce mutable catalog relationships")
	}
	field := parsed.LookUpField("NativeCompletionEvidence")
	if field == nil || field.Size != 20 || !field.NotNull || !field.HasDefaultValue || field.DefaultValue != "unknown" {
		t.Fatalf("native completion schema lacks conservative bounded default: %+v", field)
	}
	check := parsed.ParseCheckConstraints()["ck_attempts_native_completion"]
	for _, allowed := range []string{"unknown", "completed", "handoff", "blocked", "incomplete"} {
		if !strings.Contains(check.Constraint, "'"+allowed+"'") {
			t.Fatalf("native completion check missing %s", allowed)
		}
	}
}
