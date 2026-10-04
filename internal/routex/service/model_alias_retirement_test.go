package service

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/callbacks"
	"gorm.io/gorm/clause"

	"github.com/miclle/routex/internal/routex/entity"
)

func aliasSubjectFixture() modelAliasSubject {
	created := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	future := created.Add(time.Hour)
	modelID := "mdl_alias"
	return modelAliasSubject{Model: entity.Model{ID: modelID, Status: "active", CreatedAt: created}, Current: entity.ModelName{Name: "current", ModelID: modelID, CurrentModelID: &modelID, CreatedAt: created}, Selected: entity.ModelName{Name: "old/name", ModelID: modelID, CreatedAt: created, ExpiresAt: &future}}
}
func TestModelAliasReviewStateAndExactETag(t *testing.T) {
	subject := aliasSubjectFixture()
	now := subject.Model.CreatedAt
	first := modelAliasReview(subject, now, true)
	if first.State != "compatibility" || !first.CanRetire || first.Alias.IsCurrent || !personalModelETag(first.ETag) {
		t.Fatal(first)
	}
	atDeadline := modelAliasReview(subject, *subject.Selected.ExpiresAt, true)
	if atDeadline.State != "retired" || atDeadline.CanRetire || atDeadline.ETag != first.ETag {
		t.Fatal("clock changed reviewed identity", atDeadline)
	}
	noPermission := modelAliasReview(subject, now, false)
	if noPermission.CanRetire || noPermission.ETag != first.ETag {
		t.Fatal("permissions changed reviewed identity")
	}
	zone := time.FixedZone("other", 9*3600)
	same := subject
	same.Model.CreatedAt = same.Model.CreatedAt.In(zone)
	same.Current.CreatedAt = same.Current.CreatedAt.In(zone)
	same.Selected.CreatedAt = same.Selected.CreatedAt.In(zone)
	sameExpiry := same.Selected.ExpiresAt.In(zone)
	same.Selected.ExpiresAt = &sameExpiry
	if modelAliasReview(same, now, true).ETag != first.ETag {
		t.Fatal("equivalent driver time zone changed ETag")
	}
	for _, mutate := range []func(*modelAliasSubject){
		func(s *modelAliasSubject) { s.Model.Status = "inactive" },
		func(s *modelAliasSubject) { s.Model.CreatedAt = s.Model.CreatedAt.Add(time.Millisecond) },
		func(s *modelAliasSubject) { s.Current.Name = "renamed" },
		func(s *modelAliasSubject) { s.Selected.Name = "OLD/name" },
		func(s *modelAliasSubject) { s.Selected.ExpiresAt = nil },
		func(s *modelAliasSubject) { v := s.Selected.ExpiresAt.Add(-time.Second); s.Selected.ExpiresAt = &v },
		func(s *modelAliasSubject) { s.Selected.ModelID = "mdl_other" },
	} {
		changed := subject
		mutate(&changed)
		if modelAliasReview(changed, now, true).ETag == first.ETag {
			t.Fatal("review ignored changed exact context")
		}
	}
	subject.Selected = subject.Current
	current := modelAliasReview(subject, now, true)
	if current.State != "current" || !current.Alias.IsCurrent || current.CanRetire {
		t.Fatal(current)
	}
	subject.Selected = aliasSubjectFixture().Selected
	subject.Selected.ExpiresAt = nil
	if got := modelAliasReview(subject, now, true); got.State != "retired" || got.CanRetire {
		t.Fatal(got)
	}
}
func TestModelAliasInputAndServiceRejectMalformedBeforeDatabase(t *testing.T) {
	valid := `{"name":"old/name","reason":" explicit stop "}`
	var input ModelAliasRetirementInput
	if err := json.Unmarshal([]byte(valid), &input); err != nil || input.Reason != " explicit stop " {
		t.Fatal(input, err)
	}
	for _, raw := range []string{`{}`, `null`, `[]`, `{"name":"old","reason":null}`, `{"name":"old","reason":" "}`, `{"name":"old","reason":"\nstop"}`, `{"name":"old","reason":"ok","name":"other"}`, `{"name":"old","reason":"ok","extra":"x"}`, valid + ` {}`, `{"name":" old","reason":"ok"}`, `{"name":"old","reason":"` + strings.Repeat("界", 342) + `"}`, "{\"name\":\"old\",\"reason\":\"\xff\"}"} {
		if json.Unmarshal([]byte(raw), &input) == nil {
			t.Fatal("invalid intent accepted", raw)
		}
	}
	svc := &Service{}
	for _, tc := range []struct{ model, etag, name, reason string }{
		{"mdl_alias", strings.Repeat("A", 64), "old", "ok"},
		{"MDL_alias", strings.Repeat("a", 64), "old", "ok"},
		{"mdl_alias ", strings.Repeat("a", 64), "old", "ok"},
		{"mdl_alias", strings.Repeat("a", 64), "old ", "ok"},
		{"mdl_alias", strings.Repeat("a", 64), "old", "\nstop"},
		{"mdl_alias", strings.Repeat("a", 64), "old", " "},
	} {
		if _, err := svc.RetireModelAlias(context.Background(), "usr_actor", tc.model, tc.etag, ModelAliasRetirementInput{Name: tc.name, Reason: tc.reason}); err == nil {
			t.Fatal("invalid service intent accepted", tc)
		}
	}
}
func TestModelAliasAuditTypedProjection(t *testing.T) {
	subject := aliasSubjectFixture()
	before := subject.Selected.ExpiresAt
	after := subject.Model.CreatedAt
	raw, _ := json.Marshal(modelAliasRetirementAudit{subject.Model.ID, subject.Selected.Name, before, &after, "stop"})
	value := string(raw[:len(raw)-1]) + `,"ciphertext":"never expose"}`
	row := entity.AuditEvent{Action: "model.alias.retire", ResourceType: "model", ResourceID: subject.Model.ID, DetailsJSON: &value}
	record := auditRecord(row)
	if len(record.Changes) == 0 || strings.Contains(string(record.Changes), "ciphertext") {
		t.Fatal(string(record.Changes))
	}
	row.ResourceID = "mdl_other"
	if auditRecord(row).Changes != nil {
		t.Fatal("foreign target audit projected")
	}
	row.ResourceID = subject.Model.ID
	invalid := `{"model_id":"mdl_alias","name":"old","reason":"stop"}`
	row.DetailsJSON = &invalid
	if auditRecord(row).Changes != nil {
		t.Fatal("missing deadline proof projected")
	}
}

func TestModelAliasSubjectQueriesRetainExactLocksAndIndependentStatements(t *testing.T) {
	for _, dialect := range []gorm.Dialector{projectQuotaScopeDialector{}, personalLifecycleMySQLDialector{}} {
		t.Run(dialect.Name(), func(t *testing.T) {
			db, err := gorm.Open(dialect, &gorm.Config{DryRun: true, DisableAutomaticPing: true})
			if err != nil {
				t.Fatal(err)
			}
			subject := aliasSubjectFixture()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			type read struct {
				table, sql string
				vars       []any
			}
			var reads []read
			if err := db.Callback().Query().Register("test:alias-subject", func(query *gorm.DB) {
				query.Statement.BuildClauses = []string{"SELECT", "FROM", "WHERE", "LIMIT", "FOR"}
				callbacks.BuildQuerySQL(query)
				reads = append(reads, read{query.Statement.Table, query.Statement.SQL.String(), append([]any(nil), query.Statement.Vars...)})
				if query.Statement.Context != ctx {
					t.Fatal("lost transaction context")
				}
				switch row := query.Statement.Dest.(type) {
				case *entity.Model:
					*row = subject.Model
				case *entity.ModelName:
					if len(reads) == 2 {
						*row = subject.Current
					} else {
						*row = subject.Selected
					}
				default:
					t.Fatalf("wrong subject %T", row)
				}
				query.Statement.SQL.Reset()
				query.Statement.Vars = nil
			}); err != nil {
				t.Fatal(err)
			}
			origin := db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"})
			got, err := loadModelAliasSubject(origin, subject.Model.ID, subject.Selected.Name, true)
			if err != nil || !reflect.DeepEqual(got, subject) {
				t.Fatal(got, err)
			}
			expected := [][]any{{subject.Model.ID, 1}, {subject.Model.ID, 1}, {subject.Selected.Name, subject.Model.ID, 1}}
			if len(reads) != 3 {
				t.Fatal(reads)
			}
			for i, row := range reads {
				table := "model_names"
				if i == 0 {
					table = "models"
				}
				if row.table != table || !reflect.DeepEqual(row.vars, expected[i]) || !strings.HasSuffix(row.sql, " FOR UPDATE") {
					t.Fatal("subject query borrowed predicates or lost lock", row)
				}
				if dialect.Name() == "mysql" && strings.Count(row.sql, "AS BINARY") != 2*(len(expected[i])-1) {
					t.Fatal("lost exact collation", row)
				}
			}
			if origin.Statement.Model != nil || origin.Statement.Dest != nil {
				t.Fatal("polluted caller")
			}
			if _, exists := origin.Statement.Clauses["WHERE"]; exists {
				t.Fatal("leaked predicates")
			}
		})
	}
}
