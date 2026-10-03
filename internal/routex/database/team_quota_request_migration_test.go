package database

import (
	"reflect"
	"sync"
	"testing"

	"gorm.io/gorm/schema"
)

func TestFrozenTeamQuotaRequestSchema(t *testing.T) {
	models := []struct {
		model any
		table string
		keys  []string
	}{
		{&teamQuotaRequestV40{}, "team_quota_requests", []string{"id"}},
		{&teamQuotaRequestStepV40{}, "team_quota_request_steps", []string{"id"}},
		{&teamQuotaPendingSlotV40{}, "team_quota_pending_slots", []string{"team_id", "applicant_user_id", "dimension"}},
	}
	for _, test := range models {
		parsed, err := schema.Parse(test.model, &sync.Map{}, schema.NamingStrategy{})
		if err != nil {
			t.Fatal(err)
		}
		if parsed.Table != test.table || len(parsed.Relationships.Relations) != 0 {
			t.Fatal("history must use its frozen table without live relationships", parsed.Table)
		}
		keys := []string{}
		for _, key := range parsed.PrimaryFields {
			keys = append(keys, key.DBName)
		}
		if !reflect.DeepEqual(keys, test.keys) {
			t.Fatal("frozen request identity changed", keys)
		}
		for _, field := range parsed.Fields {
			nullable := field.DBName == "current_step_id" || field.DBName == "resolved_at" || field.DBName == "decision_id" || field.DBName == "decided_at"
			if !field.PrimaryKey && field.NotNull == nullable {
				t.Fatal("unexpected immutable field nullability", parsed.Table, field.DBName)
			}
		}
		checks := parsed.ParseCheckConstraints()
		switch parsed.Table {
		case "team_quota_requests":
			for _, name := range []string{"ck_team_quota_request_dimension", "ck_team_quota_request_currency", "ck_team_quota_request_status", "ck_team_quota_request_approval"} {
				if _, ok := checks[name]; !ok {
					t.Fatal("missing request guard", name)
				}
			}
			for _, name := range []string{"creation_review_etag", "approved_team_policy_etag", "approved_member_policy_etag"} {
				if parsed.FieldsByDBName[name] == nil || parsed.FieldsByDBName[name].Size != 64 {
					t.Fatal("explicit reviewed revision column changed", name)
				}
			}
			assertTeamQuotaFrozenIndex(t, parsed, "uq_team_quota_request_intent", "UNIQUE", []string{"request_id"})
			assertTeamQuotaFrozenIndex(t, parsed, "idx_team_quota_request_team_cursor", "", []string{"team_id", "created_at", "id"})
			assertTeamQuotaFrozenIndex(t, parsed, "idx_team_quota_request_applicant_cursor", "", []string{"applicant_user_id", "created_at", "id"})
		case "team_quota_request_steps":
			if len(checks) != 4 || checks["ck_team_quota_step_ordinal"].Constraint != "ordinal >= 1 AND ordinal <= 2" {
				t.Fatal("approval history must have at most two bounded stages")
			}
			if parsed.FieldsByDBName["decision_id"].NotNull || parsed.FieldsByDBName["decision_id"].HasDefaultValue {
				t.Fatal("pending stages need NULL decisions rather than colliding empty UUIDs")
			}
			assertTeamQuotaFrozenIndex(t, parsed, "uq_team_quota_request_step", "UNIQUE", []string{"request_id", "ordinal"})
			assertTeamQuotaFrozenIndex(t, parsed, "uq_team_quota_step_decision", "UNIQUE", []string{"decision_id"})
		case "team_quota_pending_slots":
			if len(checks) != 1 {
				t.Fatal("pending dimension guard absent")
			}
			assertTeamQuotaFrozenIndex(t, parsed, "uq_team_quota_pending_request", "UNIQUE", []string{"request_id"})
		}
	}
	steps := migrationSteps("postgres")
	if len(steps) != 40 {
		t.Fatal("V40 must be appended without replacing released versions", len(steps))
	}
}

func assertTeamQuotaFrozenIndex(t *testing.T, parsed *schema.Schema, name, class string, columns []string) {
	t.Helper()
	for _, index := range parsed.ParseIndexes() {
		if index.Name != name {
			continue
		}
		actual := []string{}
		for _, field := range index.Fields {
			actual = append(actual, field.DBName)
		}
		if index.Class != class || !reflect.DeepEqual(actual, columns) {
			t.Fatal("frozen uniqueness or paging order changed", name, actual)
		}
		return
	}
	t.Fatal("missing frozen index", name)
}
