package database

import (
	"slices"
	"sync"
	"testing"

	"gorm.io/gorm/schema"
)

func TestFrozenCallTeamAttributionSchema(t *testing.T) {
	parsed, err := schema.Parse(&callTeamAttributionV38{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Table != "call_records" || len(parsed.Relationships.Relations) != 0 {
		t.Fatal("Team call attribution must have no mutable relationship or foreign key")
	}
	for _, name := range []string{"TeamID", "TeamMembershipID"} {
		field := parsed.LookUpField(name)
		if field == nil || field.Size != 30 || !field.NotNull || !field.HasDefaultValue || field.DefaultValue != "" {
			t.Fatalf("%s must preserve unknown historical attribution: %+v", name, field)
		}
	}
	var columns []string
	for _, index := range parsed.ParseIndexes() {
		if index.Name == "idx_calls_team_actor_time" {
			for _, field := range index.Fields {
				columns = append(columns, field.DBName)
			}
		}
	}
	if !slices.Equal(columns, []string{"team_id", "user_id", "started_at", "request_id"}) {
		t.Fatalf("Team actor cursor index: %v", columns)
	}
	check := parsed.ParseCheckConstraints()["ck_calls_team_subject"].Constraint
	if check != "(team_id = '' AND team_membership_id = '') OR (team_id <> '' AND team_membership_id <> '' AND user_id <> '' AND project_id = '' AND key_id = '')" {
		t.Fatalf("Team subject must not share Project/Key attribution: %s", check)
	}
}
