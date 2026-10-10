package database

import (
	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm/schema"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func TestFrozenRootRotationSchemasKeepExactColumnsAndNoLiveHistory(t *testing.T) {
	for _, pair := range [][2]any{{&secretWritePolicyV48{}, &entity.SecretWritePolicy{}}, {&secretRootKeyV48{}, &entity.SecretRootKey{}}, {&secretRotationJobV48{}, &entity.SecretRotationJob{}}, {&secretRotationItemV48{}, &entity.SecretRotationItem{}}, {&secretRotationReceiptV48{}, &entity.SecretRotationReceipt{}}, {&secretProcessVerificationV48{}, &entity.SecretProcessVerification{}}} {
		frozen, err := schema.Parse(pair[0], &sync.Map{}, schema.NamingStrategy{})
		if err != nil {
			t.Fatal(err)
		}
		current, err := schema.Parse(pair[1], &sync.Map{}, schema.NamingStrategy{})
		if err != nil {
			t.Fatal(err)
		}
		extra := 0
		var upgrade *schema.Schema
		switch frozen.Table {
		case "secret_rotation_jobs":
			extra = 1
			upgrade, err = schema.Parse(&googleIdentityRootJobV99{}, &sync.Map{}, schema.NamingStrategy{})
		case "secret_process_verifications":
			extra = 1
			upgrade, err = schema.Parse(&googleIdentityRootProcessV99{}, &sync.Map{}, schema.NamingStrategy{})
		case "secret_rotation_items":
			upgrade, err = schema.Parse(&namedIdentityRootItemV98{}, &sync.Map{}, schema.NamingStrategy{})
		}
		if err != nil {
			t.Fatal(err)
		}
		if extra != 0 {
			field := current.LookUpField("InventoryVersion")
			expected := upgrade.LookUpField("InventoryVersion")
			if field == nil || field.Tag != expected.Tag || field.FieldType != expected.FieldType {
				t.Fatal("unreviewed V99 inventory field")
			}
		}
		if frozen.Table != current.Table || len(frozen.Relationships.Relations) != 0 || len(current.Relationships.Relations) != 0 || len(frozen.Fields)+extra != len(current.Fields) {
			t.Fatal("schema acquired mutable relationship", frozen.Table)
		}
		for _, field := range frozen.Fields {
			expected := field
			if upgrade != nil && field.Name == "Domain" {
				expected = upgrade.LookUpField(field.Name)
			}
			actual := current.LookUpField(field.Name)
			if actual == nil || actual.Tag != expected.Tag || actual.DBName != field.DBName || actual.FieldType != field.FieldType {
				t.Fatal("frozen schema divergence", frozen.Table, field.Name)
			}
		}
		if len(frozen.ParseCheckConstraints()) == 0 {
			t.Fatal("missing durable shape guards", frozen.Table)
		}
		for _, field := range frozen.Fields {
			if field.Name == "ETag" && field.DBName != "etag" {
				t.Fatal("etag check missing exact persisted column")
			}
			if field.Name == "ReviewETag" && field.DBName != "review_etag" {
				t.Fatal("receipt check refers to nonexistent reviewed column")
			}
		}
	}
	// The migration remains a real, independently registry-callable step.
	if reflect.ValueOf(rootKeyRotationMigration).IsNil() {
		t.Fatal("missing V48 entry")
	}
	receipt, err := schema.Parse(&secretRotationReceiptV48{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	if len(receipt.PrimaryFields) != 1 || receipt.PrimaryFields[0].DBName != "request_id" || receipt.PrimaryFields[0].Size != 36 {
		t.Fatal("UUID lacks global uniqueness")
	}
	jobs, err := schema.Parse(&secretRotationJobV48{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(jobs.ParseCheckConstraints()["ck_secret_rotation_terminal"].Constraint, "completed_at IS NOT NULL") {
		t.Fatal("terminal job missing coherent timestamp")
	}
}
