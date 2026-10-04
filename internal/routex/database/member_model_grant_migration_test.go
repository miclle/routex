package database

import (
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm/schema"
)

func TestMemberModelsFrozenV54NamesAndExactHexGuard(t *testing.T) {
	frozen, err := schema.Parse(&memberModelGrantRevisionV54{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	current, err := schema.Parse(&entity.User{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	if frozen.Table != "users" || len(frozen.Fields) != 2 || len(frozen.Relationships.Relations) != 0 {
		t.Fatal("migration acquired evolving User fields")
	}
	field := frozen.LookUpField("PersonalGrantRevision")
	live := current.LookUpField("PersonalGrantRevision")
	if field.DBName != "personal_grant_revision" || field.Tag.Get("gorm") != live.Tag.Get("gorm") || field.Size != 64 || !field.NotNull || field.DefaultValue != strings.Repeat("0", 64) || live.Tag.Get("json") != "-" {
		t.Fatal(field)
	}
	constraint, exists := frozen.ParseCheckConstraints()["ck_users_personal_grant_revision"]
	if !exists || !strings.Contains(constraint.Constraint, "CHAR_LENGTH(personal_grant_revision) = 64") || strings.Count(constraint.Constraint, "ASCII(SUBSTRING(personal_grant_revision,") != 128 || strings.Contains(constraint.Constraint, "LIKE") {
		t.Fatal("check lost actual field or exact ASCII bounds", constraint)
	}
	for _, position := range []string{"1", "32", "64"} {
		if !strings.Contains(constraint.Constraint, "personal_grant_revision,"+position+",1)) BETWEEN 97 AND 102") {
			t.Fatal("hex guard missing position", position)
		}
	}
	if reflect.ValueOf(memberModelGrantMigration).IsNil() {
		t.Fatal("missing registry-callable frozen migration")
	}
}
