package database

import (
	"reflect"
	"strings"
	"sync"
	"testing"

	"gorm.io/gorm/schema"

	"github.com/miclle/routex/internal/routex/entity"
)

func TestFrozenProjectCreationReceiptSchema(t *testing.T) {
	frozen, err := schema.Parse(&projectCreationReceiptV45{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	current, err := schema.Parse(&entity.ProjectCreationReceipt{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	if frozen.Table != "project_creation_receipts" || frozen.Table != current.Table || len(frozen.Fields) != len(current.Fields) || len(frozen.Relationships.Relations) != 0 || len(current.Relationships.Relations) != 0 {
		t.Fatal("historical creation acquired a live schema relationship")
	}
	for _, field := range frozen.Fields {
		actual := current.LookUpField(field.Name)
		if actual == nil || actual.DBName != field.DBName || actual.Tag != field.Tag || actual.FieldType != field.FieldType {
			t.Fatal("creation receipt diverged from frozen V45", field.Name)
		}
	}
	if len(frozen.PrimaryFields) != 1 || frozen.PrimaryFields[0].DBName != "creation_id" || frozen.PrimaryFields[0].Size != 36 {
		t.Fatal("creation intent lost durable global uniqueness")
	}
	for _, field := range []string{"ActorID", "ProjectID"} {
		actual := frozen.LookUpField(field)
		if actual == nil || actual.Size != 30 || !actual.NotNull {
			t.Fatal("unbounded historical creation subject", field)
		}
	}
	for _, field := range []string{"RequestHash", "ReviewETag"} {
		actual := frozen.LookUpField(field)
		if actual == nil || actual.Size != 64 || !actual.NotNull {
			t.Fatal("creation receipt lost original reviewed intent", field)
		}
	}
	if !frozen.LookUpField("SnapshotJSON").NotNull || !frozen.LookUpField("CreatedAt").NotNull || frozen.LookUpField("CreatedAt").Precision != 6 {
		t.Fatal("receipt lacks immutable original application facts")
	}
	checks := frozen.ParseCheckConstraints()
	// A matching entity/frozen tag pair alone cannot catch a constraint that
	// references a nonexistent column. Assert actual GORM DBNames independently.
	for _, parsed := range []*schema.Schema{frozen, current} {
		for field, column := range map[string]string{"CreationID": "creation_id", "RequestHash": "request_hash", "ReviewETag": "review_etag"} {
			actual := parsed.LookUpField(field)
			if actual == nil || actual.DBName != column || parsed.FieldsByDBName[column] != actual {
				t.Fatal("creation constraint refers to a missing persisted column", field, column)
			}
			constraint := "CHAR_LENGTH(" + actual.DBName + ") = " + map[string]string{"CreationID": "36", "RequestHash": "64", "ReviewETag": "64"}[field]
			if !strings.Contains(parsed.ParseCheckConstraints()["ck_project_creation_intent"].Constraint, constraint) {
				t.Fatal("missing creation receipt shape guard", constraint)
			}
		}
	}
	if strings.Contains(checks["ck_project_creation_intent"].Constraint, "review_e_tag") {
		t.Fatal("creation receipt retained an unbound inferred column")
	}
	assertTeamQuotaFrozenIndex(t, frozen, "uq_project_creation_project", "UNIQUE", []string{"project_id"})
	assertTeamQuotaFrozenIndex(t, frozen, "idx_project_creation_actor", "", []string{"actor_id"})
	for _, dialect := range []string{"postgres", "mysql"} {
		steps := migrationSteps(dialect)
		if len(steps) < 45 || reflect.ValueOf(steps[44]).Pointer() != reflect.ValueOf(projectCreationMigration).Pointer() || reflect.ValueOf(steps[43]).Pointer() != reflect.ValueOf(teamModelRequestMigration).Pointer() {
			t.Fatal("V45 must append without replacing Team request history", dialect)
		}
	}
}
