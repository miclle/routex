package database

import (
	"sync"
	"testing"

	"gorm.io/gorm/schema"
)

func TestFrozenCredentialRetirementSchema(t *testing.T) {
	parsed, err := schema.Parse(&credentialRetirementReceiptV34{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Table != "credential_retirement_receipts" || len(parsed.Relationships.Relations) != 0 || len(parsed.PrimaryFields) != 1 || parsed.PrimaryFields[0].DBName != "request_id" || len(parsed.ParseIndexes()) != 0 {
		t.Fatal("retirement receipt requires only its request primary key and no live relations")
	}
	for column, size := range map[string]int{"request_id": 36, "actor_id": 30, "source_credential_id": 30, "replacement_credential_id": 30, "connection_id": 30, "request_hash": 64, "readiness_e_tag": 64, "source_e_tag": 64, "replacement_e_tag": 64, "pre_disable_snapshot_id": 30, "evidence_attempt_id": 64} {
		field := parsed.FieldsByDBName[column]
		if field == nil || field.Size != size || (!field.PrimaryKey && !field.NotNull) {
			t.Fatalf("historical field %s is not bounded and required", column)
		}
	}
	if field := parsed.FieldsByDBName["committed_at"]; field == nil || !field.NotNull || field.Precision != 6 {
		t.Fatal("commit timestamp must preserve UTC microseconds")
	}
	if constraint, ok := parsed.ParseCheckConstraints()["ck_credential_retirement_distinct"]; !ok || constraint.Constraint != "source_credential_id <> replacement_credential_id" {
		t.Fatal("missing portable distinct-credential constraint")
	}
}
