package database

import (
	"reflect"
	"strings"
	"sync"
	"testing"

	"gorm.io/gorm/schema"

	"github.com/miclle/routex/internal/routex/entity"
)

func TestFrozenTeamModelRequestSchema(t *testing.T) {
	for _, pair := range [][2]any{{&teamModelRequestV44{}, &entity.TeamModelRequest{}}, {&teamModelRequestPendingSlotV44{}, &entity.TeamModelRequestPendingSlot{}}, {&teamModelGrantProvenanceV44{}, &entity.TeamModelGrant{}}} {
		frozen, err := schema.Parse(pair[0], &sync.Map{}, schema.NamingStrategy{})
		if err != nil {
			t.Fatal(err)
		}
		current, err := schema.Parse(pair[1], &sync.Map{}, schema.NamingStrategy{})
		if err != nil {
			t.Fatal(err)
		}
		if frozen.Table != current.Table || len(frozen.Relationships.Relations) != 0 || len(current.Relationships.Relations) != 0 {
			t.Fatal("immutable Team history acquired a live relationship")
		}
		for _, field := range frozen.Fields {
			actual := current.LookUpField(field.Name)
			if actual == nil || actual.DBName != field.DBName || actual.Tag != field.Tag || actual.FieldType != field.FieldType {
				t.Fatal("current schema diverged from frozen V44", field.Name)
			}
		}
	}
	for _, model := range []any{&teamModelRequestV44{}, &entity.TeamModelRequest{}} {
		parsed, err := schema.Parse(model, &sync.Map{}, schema.NamingStrategy{})
		if err != nil {
			t.Fatal(err)
		}
		if len(parsed.PrimaryFields) != 1 || parsed.PrimaryFields[0].DBName != "id" {
			t.Fatal("shared Team requests require independent immutable identities")
		}
		for _, field := range []string{"TeamID", "ApplicantUserID", "ApplicantMembershipID", "ModelID"} {
			actual := parsed.LookUpField(field)
			if actual == nil || actual.Size != 30 || !actual.NotNull {
				t.Fatal("unbounded submitted subject", field)
			}
		}
		for _, field := range []string{"DecisionID", "DecisionActorID", "DecisionActorName", "ResolvedAt", "DecidedAt"} {
			actual := parsed.LookUpField(field)
			if actual == nil || actual.NotNull || actual.HasDefaultValue || actual.FieldType.Kind() != reflect.Pointer {
				t.Fatal("pending requests invented terminal facts", field)
			}
		}
		checks := parsed.ParseCheckConstraints()
		for _, name := range []string{"ck_team_model_request_intent", "ck_team_model_request_status", "ck_team_model_request_pending", "ck_team_model_request_cancelled", "ck_team_model_request_receipt"} {
			if _, ok := checks[name]; !ok {
				t.Fatal("missing first-terminal guard", name)
			}
		}
		for _, condition := range []string{"decision_actor_id IS NULL", "decision_actor_name IS NULL", "decided_at IS NULL", "resolved_at IS NULL", "decision_action = ''", "decision_request_hash = ''"} {
			if !strings.Contains(checks["ck_team_model_request_pending"].Constraint, condition) {
				t.Fatal("pending guard retained terminal material", condition)
			}
		}
		for _, condition := range []string{"decision_id IS NOT NULL", "decision_actor_name IS NOT NULL", "CHAR_LENGTH(decision_review_etag) = 64", "CHAR_LENGTH(decision_request_hash) = 64", "decision_actor_id <> applicant_user_id", "decision_actor_id = applicant_user_id", "decision_reason <> ''", "decision_reason = ''"} {
			if !strings.Contains(checks["ck_team_model_request_receipt"].Constraint, condition) {
				t.Fatal("terminal guard lost independently authorized receipt evidence", condition)
			}
		}
		assertTeamQuotaFrozenIndex(t, parsed, "uq_team_model_request_intent", "UNIQUE", []string{"request_id"})
		assertTeamQuotaFrozenIndex(t, parsed, "uq_team_model_request_decision", "UNIQUE", []string{"decision_id"})
		assertTeamQuotaFrozenIndex(t, parsed, "idx_team_model_request_team_cursor", "", []string{"team_id", "created_at", "id"})
		assertTeamQuotaFrozenIndex(t, parsed, "idx_team_model_request_user_cursor", "", []string{"applicant_user_id", "created_at", "id"})
	}
	for _, model := range []any{&teamModelRequestPendingSlotV44{}, &entity.TeamModelRequestPendingSlot{}} {
		parsed, err := schema.Parse(model, &sync.Map{}, schema.NamingStrategy{})
		if err != nil {
			t.Fatal(err)
		}
		var keys []string
		for _, field := range parsed.PrimaryFields {
			keys = append(keys, field.DBName)
		}
		if !reflect.DeepEqual(keys, []string{"team_id", "model_id"}) {
			t.Fatal("different applicants can reserve duplicate shared Model additions", keys)
		}
		assertTeamQuotaFrozenIndex(t, parsed, "uq_team_model_pending_request", "UNIQUE", []string{"request_id"})
	}
	for _, model := range []any{&teamModelGrantProvenanceV44{}, &entity.TeamModelGrant{}} {
		parsed, err := schema.Parse(model, &sync.Map{}, schema.NamingStrategy{})
		if err != nil {
			t.Fatal(err)
		}
		field := parsed.LookUpField("SourceRequestID")
		if field == nil || field.DBName != "source_request_id" || field.Size != 30 || field.NotNull || field.HasDefaultValue || field.FieldType.Kind() != reflect.Pointer {
			t.Fatal("legacy/direct Team grants require nullable bounded provenance")
		}
	}
	for _, dialect := range []string{"postgres", "mysql"} {
		steps := migrationSteps(dialect)
		if len(steps) < 44 || reflect.ValueOf(steps[43]).Pointer() != reflect.ValueOf(teamModelRequestMigration).Pointer() || reflect.ValueOf(steps[42]).Pointer() != reflect.ValueOf(personalModelRequestMigration).Pointer() {
			t.Fatal("V44 must append without replacing Personal requests", dialect)
		}
	}
}
