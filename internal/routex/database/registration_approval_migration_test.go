package database

import (
	"database/sql"
	"errors"
	"gorm.io/gorm"
	"gorm.io/gorm/migrator"
	"gorm.io/gorm/schema"
	"strings"
	"sync"
	"testing"
)

func TestRegistrationApprovalFrozenSchema(t *testing.T) {
	// This references the private pending entry without assigning any release
	// number or executing schema work. Real-driver creation/repair remains separate.
	_ = registrationApprovalMigration
	app, err := schema.Parse(&registrationApprovalApplicationSchema{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	if app.Table != "registration_approval_applications" {
		t.Fatal(app.Table)
	}
	for _, n := range []string{"ck_registration_approval_id", "ck_registration_approval_state", "ck_registration_approval_decision", "ck_registration_approval_revision"} {
		if _, ok := app.ParseCheckConstraints()[n]; !ok {
			t.Fatalf("missing frozen check %s", n)
		}
	}
	if app.LookUpField("UserCreatedAt").Precision != 6 || app.LookUpField("DecidedAt").Precision != 6 {
		t.Fatal("creation/decision continuity precision changed")
	}
	if app.LookUpField("UserID").Size != 30 || !strings.Contains(app.LookUpField("UserID").TagSettings["UNIQUEINDEX"], "uidx_registration_approval_user") {
		t.Fatal("retained exact-user uniqueness missing")
	}
	relation := app.Relationships.Relations["User"]
	if relation == nil || relation.ParseConstraint() == nil || relation.ParseConstraint().OnDelete != "RESTRICT" {
		t.Fatal("retained application-to-User FK missing")
	}
	user, err := schema.Parse(&registrationApprovalUserSchema{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	if user.LookUpField("ApprovalApplicationID").NotNull || len(user.Relationships.Relations) != 0 {
		t.Fatal("historical link must stay nullable without circular FK")
	}
	setting, err := schema.Parse(&registrationApprovalGovernanceSchema{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	if setting.LookUpField("RegistrationApprovalRequired").DefaultValue != "false" {
		t.Fatal("approval default changed")
	}
}

type registrationShapeMigrator struct {
	gorm.Migrator
	column  gorm.ColumnType
	altered []string
	fail    error
}

func (m *registrationShapeMigrator) ColumnTypes(any) ([]gorm.ColumnType, error) {
	return []gorm.ColumnType{m.column}, nil
}
func (m *registrationShapeMigrator) AlterColumn(_ any, field string) error {
	m.altered = append(m.altered, field)
	return m.fail
}
func TestRegistrationPartialPolicyShapeUsesDeclaredRepairOnly(t *testing.T) {
	for _, mode := range []string{"complete", "nullable", "size", "default", "failed_repair"} {
		t.Run(mode, func(t *testing.T) {
			column := migrator.ColumnType{NameValue: sql.NullString{String: "registration_policy_revision", Valid: true}, NullableValue: sql.NullBool{Valid: true}, LengthValue: sql.NullInt64{Int64: 64, Valid: true}, DefaultValueValue: sql.NullString{String: strings.Repeat("0", 64), Valid: true}}
			switch mode {
			case "nullable":
				column.NullableValue.Bool = true
			case "size":
				column.LengthValue.Int64 = 32
			case "default":
				column.DefaultValueValue.String = ""
			case "failed_repair":
				column.NullableValue.Bool = true
			}
			m := &registrationShapeMigrator{column: column}
			if mode == "failed_repair" {
				m.fail = errors.New("controlled schema outage")
			}
			db := projectRateCheckDB(t, m)
			err := registrationApprovalRequiredColumn(db, &registrationApprovalGovernanceSchema{}, "RegistrationPolicyRevision")
			if mode == "complete" {
				if err != nil || len(m.altered) != 0 {
					t.Fatal("complete generation shape rewritten", err)
				}
				return
			}
			if len(m.altered) != 1 || m.altered[0] != "RegistrationPolicyRevision" || !errors.Is(err, m.fail) {
				t.Fatal("partial shape repair was skipped or concealed", err)
			}
		})
	}
	// Both documented false-default representations are a completed shape, never
	// grounds for repeated DDL. This is a metadata check, not a dialect branch.
	for _, value := range []string{"false", "0"} {
		m := &registrationShapeMigrator{column: migrator.ColumnType{NameValue: sql.NullString{String: "registration_approval_required", Valid: true}, NullableValue: sql.NullBool{Valid: true}, DefaultValueValue: sql.NullString{String: value, Valid: true}}}
		db := projectRateCheckDB(t, m)
		if err := registrationApprovalRequiredColumn(db, &registrationApprovalGovernanceSchema{}, "RegistrationApprovalRequired"); err != nil || len(m.altered) != 0 {
			t.Fatal("complete false-default shape rewritten", err)
		}
	}
}
