package database

import (
	"slices"
	"sync"
	"testing"

	"gorm.io/gorm/schema"
)

func TestFrozenCallCredentialAttributionSchema(t *testing.T) {
	parsed, err := schema.Parse(&callCredentialAttributionV32{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Table != "call_attempts" || len(parsed.Relationships.Relations) != 0 {
		t.Fatal("attempt attribution must remain historical without live relations")
	}
	for _, name := range []string{"CredentialID", "SnapshotID"} {
		field := parsed.LookUpField(name)
		if field == nil || field.Size != 30 || !field.NotNull || !field.HasDefaultValue || field.DefaultValue != "" {
			t.Fatalf("%s must be bounded and default to unknown: %+v", name, field)
		}
	}
	var columns []string
	for _, index := range parsed.ParseIndexes() {
		if index.Name != "idx_attempts_credential_snapshot_time" {
			continue
		}
		for _, field := range index.Fields {
			columns = append(columns, field.DBName)
		}
	}
	if !slices.Equal(columns, []string{"credential_id", "snapshot_id", "completed_at", "id"}) {
		t.Fatalf("exact attribution index columns: %v", columns)
	}
}
