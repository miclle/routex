package database

import (
	"gorm.io/gorm/schema"
	"sync"
	"testing"
)

func TestFrozenTeamResourceLimitSchema(t *testing.T) {
	parsed, err := schema.Parse(&teamResourceLimitV39{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	scope := parsed.LookUpField("ScopeID")
	if parsed.Table != "resource_limits" || scope == nil || !scope.PrimaryKey || !scope.NotNull || scope.Size != 64 || len(parsed.Relationships.Relations) != 0 {
		t.Fatalf("Team policy identities must fit stable pair digests without mutable relationships: %+v", scope)
	}
	for _, field := range []string{"Tokens5H", "Tokens7D", "TokensMonth", "TPM", "MoneyMonth", "Currency", "RPM", "Concurrency", "IPMode", "IPRangesJSON"} {
		if parsed.LookUpField(field) == nil {
			t.Fatal("frozen migration omitted historical policy field", field)
		}
	}
	if parsed.LookUpField("Currency").DefaultValue != "" {
		t.Fatal("migration cannot infer historical denomination")
	}
}
