package database

import (
	"reflect"
	"sync"
	"testing"

	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm/schema"
)

func TestFrozenModelCreationBatchReceiptSchema(t *testing.T) {
	frozen, err := schema.Parse(&modelCreationBatchReceiptV50{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	current, err := schema.Parse(&entity.ModelCreationBatchReceipt{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	if frozen.Table != "model_creation_batch_receipts" || frozen.Table != current.Table || len(frozen.Relationships.Relations) != 0 || len(current.Relationships.Relations) != 0 || len(frozen.Fields) != len(current.Fields) {
		t.Fatal("receipt gained live relationship")
	}
	for _, field := range frozen.Fields {
		other := current.LookUpField(field.Name)
		if other == nil || field.DBName != other.DBName || field.FieldType != other.FieldType || field.Tag != other.Tag {
			t.Fatal("frozen schema diverged", field.Name)
		}
	}
	for field, column := range map[string]string{"RequestID": "request_id", "RequestHash": "request_hash", "ReviewETag": "review_etag"} {
		if frozen.LookUpField(field).DBName != column {
			t.Fatal("constraint column is not persisted", field)
		}
	}
	if len(frozen.PrimaryFields) != 1 || frozen.PrimaryFields[0].DBName != "request_id" || frozen.PrimaryFields[0].Size != 36 || frozen.LookUpField("CreatedAt").Precision != 6 || frozen.LookUpField("SnapshotJSON").DataType != "text" {
		t.Fatal("immutable receipt lost durable shape")
	}
	check := frozen.ParseCheckConstraints()["ck_model_creation_batch_intent"].Constraint
	if check != "CHAR_LENGTH(request_id) = 36 AND CHAR_LENGTH(request_hash) = 64 AND CHAR_LENGTH(review_etag) = 64" {
		t.Fatal("missing complete original-intent guards", check)
	}
	assertTeamQuotaFrozenIndex(t, frozen, "idx_model_creation_batch_actor", "", []string{"actor_id"})
	assertTeamQuotaFrozenIndex(t, frozen, "idx_model_creation_batch_connection", "", []string{"connection_id"})
}

func TestModelCreationBatchMigrationReservedVersion(t *testing.T) {
	if modelCreationBatchMigrationVersion != 50 {
		t.Fatal("batch receipts changed reserved migration version")
	}
	for _, driver := range []string{"postgres", "mysql"} {
		steps := migrationSteps(driver)
		if len(steps) >= 50 && reflect.ValueOf(steps[49]).Pointer() != reflect.ValueOf(modelCreationBatchMigration).Pointer() {
			t.Fatal("V50 replaced another historical step", driver)
		}
		if len(steps) < 50 {
			t.Log("V50 registration waits for integrated V48/V49 baseline", driver)
		}
	}
}
