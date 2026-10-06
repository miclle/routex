package database

import (
	"sync"
	"testing"

	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm/schema"
)

func TestTeamCreationReceiptFrozenPortableBoundedSchema(t *testing.T) {
	frozen, err := schema.Parse(&teamCreationReceiptV63{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	current, err := schema.Parse(&entity.TeamCreationReceipt{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	if frozen.Table != "team_creation_receipts" || frozen.Table != current.Table || len(frozen.Fields) != len(current.Fields) || len(frozen.Relationships.Relations) != 0 || len(current.Relationships.Relations) != 0 {
		t.Fatal("historical receipt acquired live relationships")
	}
	for _, field := range frozen.Fields {
		actual := current.LookUpField(field.Name)
		if actual == nil || actual.Tag != field.Tag || actual.DBName != field.DBName || actual.FieldType != field.FieldType {
			t.Fatal("frozen/current divergence", field.Name)
		}
	}
	if len(frozen.PrimaryFields) != 1 || frozen.PrimaryFields[0].DBName != "creation_id" || frozen.PrimaryFields[0].Size != 36 {
		t.Fatal("intent uniqueness lost")
	}
	for _, name := range []string{"ActorCreatedAt", "TeamCreatedAt", "CreatedAt"} {
		field := frozen.LookUpField(name)
		if !field.NotNull || field.Precision != 6 {
			t.Fatal("birth precision lost", name)
		}
	}
	snapshot := frozen.LookUpField("SnapshotJSON")
	if !snapshot.NotNull || snapshot.Size != 131072 || snapshot.TagSettings["TYPE"] != "" {
		t.Fatal("snapshot portable GORM size lost")
	}
	if got := (mysql.Dialector{Config: &mysql.Config{}}).DataTypeOf(snapshot); got != "mediumtext" {
		t.Fatal("MySQL storage below128KiB", got)
	}
	if got := (postgres.Dialector{}).DataTypeOf(snapshot); got != "varchar(131072)" {
		t.Fatal("PostgreSQL bounded storage diverged", got)
	}
	check := frozen.ParseCheckConstraints()["ck_team_creation_snapshot"]
	if check.Constraint != "OCTET_LENGTH(snapshot_json) <= 131072" {
		t.Fatal("UTF-8 byte bound lost", check)
	}
	assertTeamQuotaFrozenIndex(t, frozen, "uq_team_creation_team", "UNIQUE", []string{"team_id"})
	assertTeamQuotaFrozenIndex(t, frozen, "idx_team_creation_actor", "", []string{"actor_id"})
}
