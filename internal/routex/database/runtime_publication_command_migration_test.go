package database

import (
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm/schema"
)

func TestRuntimePublicationV105FrozenSchema(t *testing.T) {
	frozen, err := schema.Parse(&runtimePublicationCommandV105{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	current, err := schema.Parse(&entity.RuntimePublicationCommand{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	names := []string{"request_id", "actor_id", "actor_created_at", "reason", "claim_instance_id", "claim_instance_started_at", "claimed_at", "finalized_at", "outcome", "failure_code"}
	if frozen.Table != "runtime_publication_commands" || current.Table != frozen.Table || len(frozen.Fields) != 10 || len(current.Fields) != 10 || len(frozen.Relationships.Relations) != 0 || len(current.Relationships.Relations) != 0 {
		t.Fatal("V105 table must remain a private ten-column receipt")
	}
	for i, name := range names {
		a, b := frozen.Fields[i], current.Fields[i]
		if a.DBName != name || b.DBName != name || a.FieldType != b.FieldType || a.Tag.Get("gorm") != b.Tag.Get("gorm") || a.AutoCreateTime != 0 || a.AutoUpdateTime != 0 || a.HasDefaultValue {
			t.Fatal("frozen/current receipt tuple differs", name)
		}
	}
	reason := frozen.FieldsByDBName["reason"]
	if reason == nil || string(reason.DataType) != "text" || reason.GORMDataType != schema.String || reason.TagSettings["TYPE"] != "text" {
		t.Fatal("explicit frozen Reason text must retain GORM text storage and string domain")
	}
	indexes := frozen.ParseIndexes()
	if len(indexes) != 1 || indexes[0].Name != "idx_runtime_publication_actor" || indexes[0].Class != "" || len(indexes[0].Fields) != 3 {
		t.Fatal("unexpected receipt index")
	}
	actual := []string{}
	for _, field := range indexes[0].Fields {
		actual = append(actual, field.DBName)
	}
	if !reflect.DeepEqual(actual, []string{"actor_id", "actor_created_at", "claimed_at"}) {
		t.Fatal("receipt actor index order differs")
	}
	checks := frozen.ParseCheckConstraints()
	c, ok := checks["ck_runtime_publication_outcome"]
	if !ok || len(checks) != 1 || !strings.Contains(c.Constraint, "finalized_at IS NOT NULL") || !strings.Contains(c.Constraint, "failure_code IS NOT NULL") || strings.Contains(c.Constraint, "outcome = '") {
		t.Fatal("receipt phase must reject null terminal facts and collating aliases")
	}
}
