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

func TestMemberRoleRevisionV56FrozenBoundedColumns(t *testing.T) {
	if memberRoleRevisionVersion != 56 || reflect.ValueOf(memberRoleRevisionMigration).IsNil() {
		t.Fatal("reserved migration")
	}
	for _, test := range []struct {
		frozen, current     any
		table, field, check string
	}{{&memberRoleUserV56{}, &entity.User{}, "users", "MemberRoleRevision", "ck_users_member_role_revision"}, {&memberRoleDefinitionV56{}, &entity.Role{}, "roles", "DefinitionRevision", "ck_roles_definition_revision"}} {
		frozen, err := schema.Parse(test.frozen, &sync.Map{}, schema.NamingStrategy{})
		if err != nil {
			t.Fatal(err)
		}
		live, err := schema.Parse(test.current, &sync.Map{}, schema.NamingStrategy{})
		if err != nil {
			t.Fatal(err)
		}
		field := frozen.LookUpField(test.field)
		current := live.LookUpField(test.field)
		if frozen.Table != test.table || len(frozen.Fields) != 2 || len(frozen.Relationships.Relations) != 0 || len(frozen.ParseIndexes()) != 0 || field.Tag != current.Tag || field.Size != 64 || !field.NotNull || field.DefaultValue != strings.Repeat("0", 64) || field.AutoCreateTime != 0 || field.AutoUpdateTime != 0 || field.Tag.Get("json") != "-" {
			t.Fatal("unbounded/mismatched private schema", test.field)
		}
		constraint := frozen.ParseCheckConstraints()[test.check].Constraint
		if !strings.Contains(constraint, "CHAR_LENGTH("+field.DBName+") = 64") || strings.Count(constraint, "ASCII(") != 128 || strings.Count(constraint, "BETWEEN 97 AND 102") != 64 {
			t.Fatal("case-foldable hex guard")
		}
	}
}

type roleRevisionColumnMethods interface{ gorm.ColumnType }
type roleRevisionColumn struct {
	roleRevisionColumnMethods
	name         string
	nullable     bool
	size         int64
	defaultValue string
}

func (c roleRevisionColumn) Name() string           { return c.name }
func (c roleRevisionColumn) Length() (int64, bool)  { return c.size, true }
func (c roleRevisionColumn) Nullable() (bool, bool) { return c.nullable, true }
func (c roleRevisionColumn) DefaultValue() (string, bool) {
	return c.defaultValue, c.defaultValue != ""
}

type roleRevisionMigrator struct {
	gorm.Migrator
	columns    map[string]bool
	checks     map[string]bool
	shapes     map[string]roleRevisionColumn
	operations []string
	fail       string
}

func (m *roleRevisionMigrator) HasColumn(_ any, field string) bool { return m.columns[field] }
func (m *roleRevisionMigrator) AddColumn(_ any, field string) error {
	m.operations = append(m.operations, "add:"+field)
	if m.fail == "add:"+field {
		return errInterruptedRateDDL
	}
	m.columns[field] = true
	return nil
}
func (m *roleRevisionMigrator) ColumnTypes(model any) ([]gorm.ColumnType, error) {
	if m.fail == "columns" {
		return nil, errInterruptedRateDDL
	}
	field := "MemberRoleRevision"
	if _, ok := model.(*memberRoleDefinitionV56); ok {
		field = "DefinitionRevision"
	}
	return []gorm.ColumnType{m.shapes[field]}, nil
}
func (m *roleRevisionMigrator) AlterColumn(_ any, field string) error {
	m.operations = append(m.operations, "alter:"+field)
	if m.fail == "alter:"+field {
		return errInterruptedRateDDL
	}
	c := m.shapes[field]
	c.nullable = false
	c.size = 64
	c.defaultValue = strings.Repeat("0", 64)
	m.shapes[field] = c
	return nil
}
func (m *roleRevisionMigrator) HasConstraint(_ any, check string) bool { return m.checks[check] }
func (m *roleRevisionMigrator) CreateConstraint(_ any, check string) error {
	m.operations = append(m.operations, "check:"+check)
	if m.fail == "check:"+check {
		return errInterruptedRateDDL
	}
	m.checks[check] = true
	return nil
}
func TestMemberRoleRevisionInterruptedDDLAndRepeat(t *testing.T) {
	for _, failure := range []string{"add:MemberRoleRevision", "add:DefinitionRevision", "alter:MemberRoleRevision", "alter:DefinitionRevision", "check:ck_users_member_role_revision", "check:ck_roles_definition_revision", "columns"} {
		t.Run(failure, func(t *testing.T) {
			m := &roleRevisionMigrator{columns: map[string]bool{}, checks: map[string]bool{}, shapes: map[string]roleRevisionColumn{"MemberRoleRevision": {name: "member_role_revision", nullable: true, size: 64}, "DefinitionRevision": {name: "definition_revision", nullable: true, size: 64}}, fail: failure}
			db := projectRateCheckDB(t, m)
			db.SkipDefaultTransaction = true
			if !errors.Is(memberRoleRevisionMigration(db), errInterruptedRateDDL) {
				t.Fatal("DDL failure not propagated")
			}
			m.fail = ""
			if err := memberRoleRevisionMigration(db); err != nil {
				t.Fatal(err)
			}
			before := slicesForRoleMigration(m.operations)
			if err := memberRoleRevisionMigration(db); err != nil || !reflect.DeepEqual(before, m.operations) {
				t.Fatal("repeat rewrote DDL", err)
			}
			if !m.columns["MemberRoleRevision"] || !m.columns["DefinitionRevision"] || !m.checks["ck_users_member_role_revision"] || !m.checks["ck_roles_definition_revision"] {
				t.Fatal("partial shape not repaired")
			}
		})
	}
}
func slicesForRoleMigration(values []string) []string { return append([]string(nil), values...) }
