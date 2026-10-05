package database

import (
	"database/sql"
	"errors"
	"strings"
	"sync"
	"testing"

	"gorm.io/gorm"
	"gorm.io/gorm/migrator"
	"gorm.io/gorm/schema"
)

func TestRegistrationEmailDomainsFrozenV58(t *testing.T) {
	_ = registrationEmailDomainsMigration
	if registrationEmailDomainsVersion != 58 {
		t.Fatal("wrong proposed version")
	}
	desired, err := schema.Parse(&registrationEmailDomainsV58{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	field := desired.LookUpField("RegistrationAllowedEmailDomains")
	if desired.Table != "governance_settings" || len(desired.Fields) != 2 || field.Size != 2048 || !field.NotNull || field.DefaultValue != "[]" {
		t.Fatal("frozen domain-only schema changed")
	}
	nullable, err := schema.Parse(&registrationEmailDomainsNullableV58{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil || nullable.LookUpField("RegistrationAllowedEmailDomains").NotNull || nullable.LookUpField("RegistrationAllowedEmailDomains").HasDefaultValue {
		t.Fatal("historical NULL add must precede backfill and shape repair")
	}
	for _, raw := range []string{`[]`, `["example.invalid"]`, `["a.invalid","b.invalid"]`} {
		if !registrationEmailDomainsValueV58(raw) {
			t.Fatal("valid retained policy rejected")
		}
	}
	for _, raw := range []string{``, `null`, `["EXAMPLE.invalid"]`, `["b.invalid","a.invalid"]`, `["a.invalid","a.invalid"]`, `["*.invalid"]`, `["127.0.0.1"]`, `["127.0.0.01"]`, `["127.1"]`, `["a.invalid."]`, `["a.invalid"] `, strings.Repeat(" ", 2049)} {
		if registrationEmailDomainsValueV58(raw) {
			t.Fatal("invalid history repaired to unrestricted")
		}
	}
}

type registrationDomainShapeMigrator struct {
	gorm.Migrator
	column gorm.ColumnType
	alters int
	err    error
}

func (m *registrationDomainShapeMigrator) ColumnTypes(any) ([]gorm.ColumnType, error) {
	return []gorm.ColumnType{m.column}, nil
}
func (m *registrationDomainShapeMigrator) AlterColumn(any, string) error { m.alters++; return m.err }
func TestRegistrationEmailDomainsPartialShapeRepair(t *testing.T) {
	for _, mode := range []string{"complete", "nullable", "length", "default", "kind", "failure"} {
		t.Run(mode, func(t *testing.T) {
			column := migrator.ColumnType{NameValue: sql.NullString{String: "registration_allowed_email_domains", Valid: true}, NullableValue: sql.NullBool{Valid: true}, LengthValue: sql.NullInt64{Int64: 2048, Valid: true}, DefaultValueValue: sql.NullString{String: "[]", Valid: true}, DataTypeValue: sql.NullString{String: "varchar", Valid: true}}
			switch mode {
			case "nullable", "failure":
				column.NullableValue.Bool = true
			case "length":
				column.LengthValue.Int64 = 4096
			case "default":
				column.DefaultValueValue.String = "[[]]"
			case "kind":
				column.DataTypeValue.String = "text"
			}
			m := &registrationDomainShapeMigrator{column: column}
			if mode == "failure" {
				m.err = errors.New("controlled DDL failure")
			}
			err := registrationEmailDomainsShapeV58(projectRateCheckDB(t, m))
			if mode == "complete" {
				if err != nil || m.alters != 0 {
					t.Fatal("valid retained shape changed", err)
				}
			} else if m.alters != 1 || mode == "failure" && !errors.Is(err, m.err) || mode != "failure" && err != nil {
				t.Fatal("partial shape repair/error propagation changed", err)
			}
		})
	}
}
