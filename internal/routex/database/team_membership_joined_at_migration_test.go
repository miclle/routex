package database

import (
	"reflect"
	"sync"
	"testing"

	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm/schema"
)

func TestTeamMembershipJoinedAtFrozenSchemaPreservesHistoricalUnknown(t *testing.T) {
	frozen, err := schema.Parse(&teamMembershipJoinedAtV53{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	current, err := schema.Parse(&entity.TeamMembership{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	if frozen.Table != "team_memberships" || len(frozen.Fields) != 2 || len(frozen.Relationships.Relations) != 0 {
		t.Fatal("joined-at migration acquired unrelated schema")
	}
	for _, name := range []string{"ID", "JoinedAt"} {
		a, b := frozen.LookUpField(name), current.LookUpField(name)
		if a == nil || b == nil || a.Tag != b.Tag || a.FieldType != b.FieldType {
			t.Fatal("frozen timestamp schema diverged", name)
		}
	}
	field := frozen.LookUpField("JoinedAt")
	if field.NotNull || field.HasDefaultValue || field.AutoCreateTime != 0 || field.AutoUpdateTime != 0 {
		t.Fatal("historical unknown would acquire an invented timestamp")
	}
}

func TestTeamMembershipJoinedAtMigrationAvailableForVersionedIntegration(t *testing.T) {
	if reflect.ValueOf(teamMembershipJoinedAtMigration).IsNil() {
		t.Fatal("missing future versioned migration")
	}
}
