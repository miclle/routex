package database

import (
	"sync"
	"testing"

	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm/schema"
)

func TestRoleDescriptionFrozenPortableSchema(t *testing.T) {
	frozen, err := schema.Parse(&roleDescriptionV67{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	current, err := schema.Parse(&entity.Role{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	a, b := frozen.LookUpField("Description"), current.LookUpField("Description")
	if frozen.Table != "roles" || len(frozen.Relationships.Relations) != 0 || a.Tag != b.Tag || a.DBName != "description" || a.Size != 2000 || !a.NotNull || !a.HasDefaultValue || a.DefaultValue != "" {
		t.Fatal("historical empty description or frozen schema changed")
	}
	check := frozen.ParseCheckConstraints()["ck_role_description_bytes"]
	if check.Constraint != "OCTET_LENGTH(description) <= 2000" {
		t.Fatal("portable byte constraint missing")
	}
}
