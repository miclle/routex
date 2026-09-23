package database

import (
	"sync"
	"testing"

	"gorm.io/gorm/schema"
)

func TestFrozenCallAttemptForeignKeyDirection(t *testing.T) {
	parsed, err := schema.Parse(&callAttemptV5{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	relation := parsed.Relationships.Relations["Request"]
	if relation == nil || relation.Type != schema.BelongsTo {
		t.Fatal("attempt must belong to a request, never reverse the foreign key")
	}
	constraint := relation.ParseConstraint()
	if constraint.Schema.Table != "call_attempts" || constraint.ReferenceSchema.Table != "call_records" || len(constraint.ForeignKeys) != 1 || constraint.ForeignKeys[0].DBName != "request_id" || constraint.References[0].DBName != "request_id" {
		t.Fatalf("wrong historical FK: %+v", constraint)
	}
}
