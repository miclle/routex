package database

import (
	"sync"
	"testing"

	"gorm.io/gorm/schema"
)

func TestFrozenCredentialReplacementSchema(t *testing.T) {
	lineage, err := schema.Parse(&credentialLineageV31{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	field := lineage.FieldsByDBName["replaces_credential_id"]
	if field == nil || field.NotNull || field.Size != 30 || len(lineage.Relationships.Relations) != 0 {
		t.Fatal("lineage must be nullable historical ID with no live relationship")
	}
	receipt, err := schema.Parse(&credentialReplacementReceiptV31{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Table != "credential_replacement_receipts" || len(receipt.Relationships.Relations) != 0 || len(receipt.PrimaryFields) != 1 || receipt.PrimaryFields[0].DBName != "request_id" {
		t.Fatal("receipt must retain request identity without live FKs")
	}
	indexes := map[string]bool{}
	for _, index := range receipt.ParseIndexes() {
		indexes[index.Name] = true
		if index.Name == "idx_credential_replacement_result" && index.Class != "UNIQUE" {
			t.Fatal("a result must identify at most one creation receipt")
		}
	}
	if !indexes["idx_credential_replacement_result"] || !indexes["idx_credential_replacement_source"] {
		t.Fatal("receipt indexes missing")
	}
}
