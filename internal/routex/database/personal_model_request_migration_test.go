package database

import (
	"reflect"
	"strings"
	"sync"
	"testing"

	"gorm.io/gorm/schema"

	"github.com/miclle/routex/internal/routex/entity"
)

func TestFrozenPersonalModelRequestSchema(t *testing.T) {
	for _, model := range []any{&personalModelRequestV43{}, &entity.PersonalModelRequest{}} {
		parsed, err := schema.Parse(model, &sync.Map{}, schema.NamingStrategy{})
		if err != nil {
			t.Fatal(err)
		}
		if parsed.Table != "personal_model_requests" || len(parsed.Relationships.Relations) != 0 ||
			len(parsed.PrimaryFields) != 1 || parsed.PrimaryFields[0].DBName != "id" {
			t.Fatal("request history must retain its own identity without live relationships")
		}
		for field, column := range map[string]string{
			"ReviewETag": "review_etag", "DecisionReviewETag": "decision_review_etag",
			"DecisionRequestHash": "decision_request_hash", "DecisionID": "decision_id",
		} {
			if actual := parsed.LookUpField(field); actual == nil || actual.DBName != column {
				t.Fatal("frozen receipt column disagrees with guards", field, column)
			}
		}
		for _, name := range []string{"DecisionID", "DecisionActorID", "DecisionActorName", "DecidedAt", "ResolvedAt"} {
			field := parsed.LookUpField(name)
			if field == nil || field.NotNull || field.HasDefaultValue || field.FieldType.Kind() != reflect.Pointer {
				t.Fatal("pending history must keep absent decision facts null", name)
			}
		}
		checks := parsed.ParseCheckConstraints()
		for _, name := range []string{"ck_personal_model_request_intent", "ck_personal_model_request_status", "ck_personal_model_request_pending", "ck_personal_model_request_receipt", "ck_personal_model_request_cancelled"} {
			if _, exists := checks[name]; !exists {
				t.Fatal("missing immutable request boundary", name)
			}
		}
		receipt := checks["ck_personal_model_request_receipt"].Constraint
		for _, clause := range []string{"decision_id IS NOT NULL", "decision_actor_id IS NOT NULL", "CHAR_LENGTH(decision_request_hash) = 64", "CHAR_LENGTH(decision_review_etag) = 64", "decided_at IS NOT NULL", "resolved_at IS NOT NULL", "decision_action = 'approve'", "decision_action = 'reject'", "decision_action = 'withdraw'"} {
			if !strings.Contains(receipt, clause) {
				t.Fatal("terminal history lacks its independently reviewed first decision", clause)
			}
		}
		assertTeamQuotaFrozenIndex(t, parsed, "uq_personal_model_request_intent", "UNIQUE", []string{"request_id"})
		assertTeamQuotaFrozenIndex(t, parsed, "uq_personal_model_request_decision", "UNIQUE", []string{"decision_id"})
		assertTeamQuotaFrozenIndex(t, parsed, "idx_personal_model_request_user_cursor", "", []string{"applicant_user_id", "created_at", "id"})
		assertTeamQuotaFrozenIndex(t, parsed, "idx_personal_model_request_status_cursor", "", []string{"status", "created_at", "id"})
	}
	for _, model := range []any{&personalModelRequestPendingSlotV43{}, &entity.PersonalModelRequestPendingSlot{}} {
		parsed, err := schema.Parse(model, &sync.Map{}, schema.NamingStrategy{})
		if err != nil {
			t.Fatal(err)
		}
		if parsed.Table != "personal_model_request_pending_slots" || len(parsed.Relationships.Relations) != 0 {
			t.Fatal("pending slots must not borrow live request or identity references")
		}
		var keys []string
		for _, field := range parsed.PrimaryFields {
			keys = append(keys, field.DBName)
		}
		if !reflect.DeepEqual(keys, []string{"applicant_user_id", "model_id"}) {
			t.Fatal("one pending applicant/model pair must have portable uniqueness", keys)
		}
		assertTeamQuotaFrozenIndex(t, parsed, "uq_personal_model_pending_request", "UNIQUE", []string{"request_id"})
	}
	for _, model := range []any{&personalModelGrantProvenanceV43{}, &entity.UserModelGrant{}} {
		parsed, err := schema.Parse(model, &sync.Map{}, schema.NamingStrategy{})
		if err != nil {
			t.Fatal(err)
		}
		field := parsed.LookUpField("SourceRequestID")
		if parsed.Table != "user_model_grants" || len(parsed.Relationships.Relations) != 0 ||
			field == nil || field.DBName != "source_request_id" || field.Size != 30 || field.NotNull || field.HasDefaultValue ||
			field.FieldType.Kind() != reflect.Pointer {
			t.Fatal("legacy and direct grants require nullable, bounded request provenance")
		}
	}
	for _, dialect := range []string{"postgres", "mysql"} {
		steps := migrationSteps(dialect)
		if len(steps) < 43 || reflect.ValueOf(steps[42]).Pointer() != reflect.ValueOf(personalModelRequestMigration).Pointer() ||
			reflect.ValueOf(steps[41]).Pointer() != reflect.ValueOf(defaultLimitMigration).Pointer() {
			t.Fatal("V43 must append without replacing released defaults", dialect)
		}
	}
}
