package database

import (
	"reflect"
	"sync"
	"testing"

	"gorm.io/gorm/schema"
)

func TestFrozenTeamRoleSchema(t *testing.T) {
	parsed, err := schema.Parse(&teamRoleV41{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Table != "team_roles" || len(parsed.PrimaryFields) != 2 || len(parsed.Relationships.Relations) != 2 {
		t.Fatal("Team assignments need a bounded composite identity and two live references")
	}
	for i, name := range []string{"team_id", "role_id"} {
		field := parsed.PrimaryFields[i]
		if field.DBName != name || field.Size != 30 || !field.NotNull || field.HasDefaultValue {
			t.Fatal("Team role identity cannot be nullable, synthesized or widened", field.DBName)
		}
	}
	indexes := parsed.ParseIndexes()
	if len(indexes) != 1 || indexes[0].Name != "idx_team_roles_role" || len(indexes[0].Fields) != 1 || indexes[0].Fields[0].DBName != "role_id" {
		t.Fatal("role removal must have its own assignment lookup index")
	}
	for _, test := range []struct {
		relation, name, table, column string
	}{
		{"Team", "fk_team_roles_team", "teams", "team_id"},
		{"Role", "fk_team_roles_role", "roles", "role_id"},
	} {
		relation := parsed.Relationships.Relations[test.relation]
		if relation == nil || relation.Type != schema.BelongsTo {
			t.Fatal("assignment must belong to a live reference", test.relation)
		}
		constraint := relation.ParseConstraint()
		if constraint == nil || constraint.Name != test.name || constraint.Schema.Table != "team_roles" || constraint.ReferenceSchema.Table != test.table || len(constraint.ForeignKeys) != 1 || constraint.ForeignKeys[0].DBName != test.column || len(constraint.References) != 1 || constraint.References[0].DBName != "id" || constraint.OnDelete != "RESTRICT" || constraint.OnUpdate != "RESTRICT" {
			t.Fatal("Team assignments must retain restrictive live FK direction", test.relation)
		}
		if len(relation.FieldSchema.Fields) != 1 || relation.FieldSchema.Fields[0].DBName != "id" || len(relation.FieldSchema.Relationships.Relations) != 0 {
			t.Fatal("frozen reference stubs cannot recursively migrate current entities")
		}
	}
	for _, dialect := range []string{"postgres", "mysql"} {
		steps := migrationSteps(dialect)
		if len(steps) < 41 || reflect.ValueOf(steps[40]).Pointer() != reflect.ValueOf(teamRoleMigration).Pointer() {
			t.Fatal("Team assignment migration must remain at immutable V41", dialect)
		}
	}
}
