package database

import (
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

func TestFrozenTeamAttachmentSchema(t *testing.T) {
	frozen, err := schema.Parse(&storageTeamOwnerV46{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	current, err := schema.Parse(&entity.StorageObject{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	if frozen.Table != "storage_objects" || len(frozen.Fields) != 4 || len(frozen.Relationships.Relations) != 0 || len(current.Relationships.Relations) != 0 {
		t.Fatal("Team storage must retain historical proofs without live relationships")
	}
	for _, field := range frozen.Fields {
		actual := current.LookUpField(field.Name)
		if actual == nil || actual.DBName != field.DBName || actual.FieldType != field.FieldType || actual.Size != field.Size || actual.Precision != field.Precision {
			t.Fatal("frozen/current column mismatch", field.Name)
		}
	}
	for _, name := range []string{"CreatorUserID", "CreatorMembershipID", "ExpiresAt"} {
		field := frozen.LookUpField(name)
		if field.NotNull || field.HasDefaultValue || field.FieldType.Kind() != reflect.Pointer {
			t.Fatal("historical user/project proof must remain NULL", name)
		}
	}
	if frozen.LookUpField("ExpiresAt").DBName != "expires_at" || frozen.LookUpField("ExpiresAt").Precision != 6 {
		t.Fatal("deadline must persist at supported precision")
	}
	for _, parsed := range []*schema.Schema{frozen, current} {
		checks := parsed.ParseCheckConstraints()
		if checks["ck_storage_objects_owner_kind"].Constraint != "owner_kind IN ('user','project','team')" {
			t.Fatal("unbounded owner kind")
		}
		proof := checks["ck_storage_objects_team_creator"].Constraint
		for _, column := range []string{"creator_user_id", "creator_membership_id", "expires_at"} {
			if parsed.FieldsByDBName[column] == nil || !strings.Contains(proof, column+" IS NULL") || !strings.Contains(proof, column+" IS NOT NULL") {
				t.Fatal("check refers to nonexistent column or permits borrowed historical proof", column)
			}
		}
		for _, column := range []string{"creator_user_id", "creator_membership_id"} {
			if !strings.Contains(proof, "CHAR_LENGTH("+column+") > 0") {
				t.Fatal("empty creator proof allowed")
			}
		}
	}
	released, err := schema.Parse(&storageOwnerV23{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil || released.ParseCheckConstraints()["ck_storage_objects_owner_kind"].Constraint != "owner_kind IN ('user','project')" {
		t.Fatal("released V23 changed", err)
	}
	for _, dialect := range []string{"postgres", "mysql"} {
		steps := migrationSteps(dialect)
		if len(steps) != 46 || reflect.ValueOf(steps[45]).Pointer() != reflect.ValueOf(teamAttachmentMigration).Pointer() || reflect.ValueOf(steps[44]).Pointer() != reflect.ValueOf(projectCreationMigration).Pointer() {
			t.Fatal("V46 must append to V45", dialect)
		}
	}
}

type teamAttachmentMigrator struct {
	gorm.Migrator
	columns, checks map[string]bool
	fail            string
}

func (m *teamAttachmentMigrator) HasColumn(_ any, name string) bool { return m.columns[name] }
func (m *teamAttachmentMigrator) AddColumn(_ any, name string) error {
	if m.fail == "add:"+name {
		return errInterruptedRateDDL
	}
	m.columns[name] = true
	return nil
}
func (m *teamAttachmentMigrator) HasConstraint(_ any, name string) bool { return m.checks[name] }
func (m *teamAttachmentMigrator) CreateConstraint(_ any, name string) error {
	if !m.columns["CreatorUserID"] || !m.columns["CreatorMembershipID"] || !m.columns["ExpiresAt"] {
		return errors.New("guard created before its columns")
	}
	if m.fail == "create:"+name {
		return errInterruptedRateDDL
	}
	m.checks[name] = true
	return nil
}
func (m *teamAttachmentMigrator) DropConstraint(_ any, name string) error {
	if !m.checks["ck_storage_objects_team_creator"] {
		return errors.New("owner guard dropped before creator guard")
	}
	if m.fail == "drop:"+name {
		return errInterruptedRateDDL
	}
	delete(m.checks, name)
	return nil
}

func TestTeamAttachmentMigrationRepairsInterruptedDDL(t *testing.T) {
	for _, failure := range []string{"add:CreatorMembershipID", "add:ExpiresAt", "add:CreatorUserID", "create:ck_storage_objects_team_creator", "drop:ck_storage_objects_owner_kind", "create:ck_storage_objects_owner_kind"} {
		t.Run(failure, func(t *testing.T) {
			m := &teamAttachmentMigrator{columns: map[string]bool{}, checks: map[string]bool{"ck_storage_objects_owner_kind": true}, fail: failure}
			db := projectRateCheckDB(t, m)
			if err := teamAttachmentMigration(db); !errors.Is(err, errInterruptedRateDDL) {
				t.Fatal("interrupted DDL not propagated", err)
			}
			m.fail = ""
			if err := teamAttachmentMigration(db); err != nil {
				t.Fatal("resume failed", err)
			}
			if len(m.columns) != 3 || !m.checks["ck_storage_objects_team_creator"] || !m.checks["ck_storage_objects_owner_kind"] {
				t.Fatal("resume lost guard or proof")
			}
			if err := teamAttachmentMigration(db); err != nil {
				t.Fatal("repeat failed", err)
			}
		})
	}
}
