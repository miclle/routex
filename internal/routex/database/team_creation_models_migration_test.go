package database

import (
	"sync"
	"testing"

	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm/schema"
)

func TestTeamCreationModelsFrozenPortableSchemas(t *testing.T) {
	for _, pair := range [][2]any{{&teamCreationModelsReceiptV66{}, &entity.TeamCreationReceipt{}}, {&teamCreationModelGrantV66{}, &entity.TeamModelGrant{}}, {&teamCreationReceiptModelV66{}, &entity.TeamCreationReceiptModel{}}} {
		frozen, err := schema.Parse(pair[0], &sync.Map{}, schema.NamingStrategy{})
		if err != nil {
			t.Fatal(err)
		}
		current, err := schema.Parse(pair[1], &sync.Map{}, schema.NamingStrategy{})
		if err != nil {
			t.Fatal(err)
		}
		if frozen.Table != current.Table || len(frozen.Relationships.Relations) != 0 || len(current.Relationships.Relations) != 0 {
			t.Fatal("live relationship in receipt proof")
		}
		for _, name := range []string{"ModelSnapshotVersion", "ModelCount", "ModelDigest", "SourceCreationReceiptID", "ModelCreatedAt"} {
			f := frozen.LookUpField(name)
			if f == nil {
				continue
			}
			c := current.LookUpField(name)
			if c == nil || f.Tag != c.Tag || f.DBName != c.DBName || f.FieldType != c.FieldType {
				t.Fatal("frozen/current divergence", name)
			}
		}
	}
	child, err := schema.Parse(&teamCreationReceiptModelV66{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	if len(child.PrimaryFields) != 2 || child.PrimaryFields[0].DBName != "creation_id" || child.PrimaryFields[0].Size != 36 || child.PrimaryFields[1].DBName != "model_id" || child.PrimaryFields[1].Size != 30 || child.LookUpField("ModelCreatedAt").Precision != 6 {
		t.Fatal("normalized identity or precision lost")
	}
	parent, err := schema.Parse(&teamCreationModelsReceiptV66{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	if parent.LookUpField("ModelCount").DefaultValue != "0" || parent.LookUpField("ModelSnapshotVersion").DefaultValue != "0" || parent.LookUpField("ModelDigest").NotNull {
		t.Fatal("legacy empty provenance inferred")
	}
	if len(parent.ParseCheckConstraints()) != 1 || len(child.ParseCheckConstraints()) != 1 {
		t.Fatal("portable constraints missing")
	}
}
