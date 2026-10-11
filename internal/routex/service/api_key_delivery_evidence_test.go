package service

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func TestAPIKeyDeliveryReplacementCallStatusUsesExactBoundedProof(t *testing.T) {
	birth := time.Unix(1700000000, 0).UTC()
	for _, kind := range []string{"personal", "project"} {
		t.Run(kind, func(t *testing.T) {
			owner := apiKeyDeliveryOwner{Kind: kind, ID: "owner_exact", Birth: birth}
			key := entity.APIKey{ID: "key_exact", CreatedAt: birth}
			user, project := "owner_exact", ""
			if kind == "project" {
				user, project = "", "owner_exact"
			}
			valid := replacementKeyCallEvidence{RequestID: "req_exact", AttemptRequestID: "req_exact", UserID: user, ProjectID: project, KeyID: key.ID, CallStatus: "success", AttemptStatus: "success", NativeCompletionEvidence: "completed", StartedAt: birth, AttemptNumber: 1}
			candidate := valid
			rows := int64(1)
			var queryError error
			queries := 0
			db, err := gorm.Open(projectQuotaScopeDialector{}, &gorm.Config{DryRun: true, DisableAutomaticPing: true})
			if err != nil {
				t.Fatal(err)
			}
			if err = db.Callback().Query().Register("test:delivery-replacement-evidence", func(tx *gorm.DB) {
				queries++
				where, ok := tx.Statement.Clauses["WHERE"].Expression.(clause.Where)
				if !ok || len(where.Exprs) != 3 {
					t.Fatal("missing bounded native proof predicates")
				}
				scope, ok := where.Exprs[0].(clause.Expr)
				if !ok || !reflect.DeepEqual(scope.Vars, []any{user, project, key.ID}) {
					t.Fatal("receipt query crossed immutable owner/key scope")
				}
				eligibility, ok := where.Exprs[1].(clause.Expr)
				if !ok || !reflect.DeepEqual(eligibility.Vars, []any{"success", birth, "success", "completed"}) {
					t.Fatal("receipt query lost creation/native-completion bound")
				}
				latest, ok := where.Exprs[2].(clause.Expr)
				if !ok || latest.SQL != "NOT EXISTS (?)" || len(latest.Vars) != 1 {
					t.Fatal("superseded-attempt exclusion absent")
				}
				limit, ok := tx.Statement.Clauses["LIMIT"].Expression.(clause.Limit)
				if !ok || limit.Limit == nil || *limit.Limit != 1 {
					t.Fatal("unbounded replacement call history")
				}
				if queryError != nil {
					_ = tx.AddError(queryError)
					return
				}
				dest, ok := tx.Statement.Dest.(*replacementKeyCallEvidence)
				if !ok {
					t.Fatal("unexpected native proof projection")
				}
				*dest = candidate
				tx.RowsAffected = rows
			}); err != nil {
				t.Fatal(err)
			}
			for _, tc := range []struct {
				name   string
				mutate func(*replacementKeyCallEvidence)
				rows   int64
				want   string
			}{
				{"completed", func(*replacementKeyCallEvidence) {}, 1, "completed"},
				{"not_recorded", func(*replacementKeyCallEvidence) {}, 0, "not_recorded"},
				{"HTTP_success_only", func(e *replacementKeyCallEvidence) { e.NativeCompletionEvidence = "" }, 1, "not_recorded"},
				{"handoff", func(e *replacementKeyCallEvidence) { e.NativeCompletionEvidence = "handoff" }, 1, "not_recorded"},
				{"unknown", func(e *replacementKeyCallEvidence) { e.NativeCompletionEvidence = "unknown" }, 1, "not_recorded"},
				{"precreation", func(e *replacementKeyCallEvidence) { e.StartedAt = birth.Add(-time.Microsecond) }, 1, "not_recorded"},
				{"other_owner", func(e *replacementKeyCallEvidence) {
					if kind == "personal" {
						e.UserID = "owner_other"
					} else {
						e.ProjectID = "owner_other"
					}
				}, 1, "not_recorded"},
				{"owner_collation", func(e *replacementKeyCallEvidence) {
					if kind == "personal" {
						e.UserID = strings.ToUpper(user)
					} else {
						e.ProjectID = strings.ToUpper(project)
					}
				}, 1, "not_recorded"},
				{"key_collation", func(e *replacementKeyCallEvidence) { e.KeyID = strings.ToUpper(key.ID) }, 1, "not_recorded"},
				{"request_collation", func(e *replacementKeyCallEvidence) { e.AttemptRequestID = strings.ToUpper(e.RequestID) }, 1, "not_recorded"},
				{"completion_collation", func(e *replacementKeyCallEvidence) { e.NativeCompletionEvidence = "COMPLETED" }, 1, "not_recorded"},
			} {
				candidate = valid
				tc.mutate(&candidate)
				rows = tc.rows
				queries = 0
				got, err := apiKeyDeliveryReplacementCallStatus(db, owner, "rotate", key)
				if err != nil || got != tc.want || queries != 1 {
					t.Fatal("replacement-call receipt inferred proof", tc.name, got, err, queries)
				}
			}
			queryError = errors.New("controlled native-proof database failure")
			queries = 0
			got, err := apiKeyDeliveryReplacementCallStatus(db, owner, "rotate", key)
			if got != "" || !errors.Is(err, queryError) || apiKeyDeliveryError(err) != apiKeyDeliveryUnavailable || queries != 1 {
				t.Fatal("query failure became false proof or no-record fact")
			}
			queries = 0
			if got, err := apiKeyDeliveryReplacementCallStatus(db, owner, "create", key); err != nil || got != "not_applicable" || queries != 0 {
				t.Fatal("create receipt consulted replacement history")
			}
		})
	}
	if got, err := apiKeyDeliveryReplacementCallStatus(nil, apiKeyDeliveryOwner{Kind: "unknown"}, "rotate", entity.APIKey{}); got != "" || !errors.Is(err, apiKeyDeliveryUnavailable) {
		t.Fatal("unknown owner kind admitted")
	}
	if got, err := apiKeyDeliveryReplacementCallStatus(nil, apiKeyDeliveryOwner{}, "ROTATE", entity.APIKey{}); got != "" || !errors.Is(err, apiKeyDeliveryUnavailable) {
		t.Fatal("unknown purpose admitted")
	}
}
