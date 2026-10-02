package database

import (
	"strings"
	"sync"
	"testing"

	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

func TestFrozenProjectQuotaRequestSchema(t *testing.T) {
	parsed, err := schema.Parse(&projectQuotaRequestV36{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Table != "project_model_requests" || len(parsed.PrimaryFields) != 1 || parsed.PrimaryFields[0].DBName != "id" || len(parsed.Relationships.Relations) != 0 || len(parsed.ParseIndexes()) != 0 {
		t.Fatal("V36 must retain request identity without new tables, indexes or FK")
	}
	for name, size := range map[string]int{"Kind": 20, "BaselinePolicyETag": 64, "DecisionReviewETag": 64, "ApprovedPolicyETag": 64, "DecisionRequestHash": 64} {
		field := parsed.FieldsByName[name]
		if field == nil || !field.NotNull || !field.HasDefaultValue || field.Size != size {
			t.Fatal("missing bounded required additive column", name)
		}
		expected := ""
		if name == "Kind" {
			expected = "MODEL_ACCESS"
		}
		if field.DefaultValueInterface != expected {
			t.Fatal("historical request default changed", name)
		}
	}
	// Explicit names prevent GORM initialism splitting from producing *_e_tag
	// columns that differ from the names used by the approval constraint.
	for field, column := range map[string]string{"BaselinePolicyETag": "baseline_policy_etag", "DecisionReviewETag": "decision_review_etag", "ApprovedPolicyETag": "approved_policy_etag"} {
		if parsed.FieldsByName[field].DBName != column {
			t.Fatal("immutable validator column changed", field)
		}
	}
	policy := parsed.FieldsByName["ApprovedPolicyJSON"]
	if policy == nil || !policy.NotNull || !policy.HasDefaultValue || policy.DataType != "text" || policy.DefaultValue != "('')" || policy.DefaultValueInterface != nil {
		t.Fatal("frozen policy must retain portable TEXT expression default")
	}
	checks := parsed.ParseCheckConstraints()
	if len(checks) != 2 || checks["ck_project_request_kind"].Constraint != "kind IN ('MODEL_ACCESS','QUOTA')" {
		t.Fatal("request kinds must remain bounded")
	}
	approval := checks["ck_project_quota_approval"].Constraint
	for _, required := range []string{"kind <> 'QUOTA'", "status <> 'approved'", "decision_review_etag <> ''", "approved_policy_etag <> ''", "decision_request_hash <> ''", "approved_policy_json <> ''"} {
		if !strings.Contains(approval, required) {
			t.Fatal("approved quota evidence guard absent", required)
		}
	}
}

func TestProjectQuotaPolicyDefaultExpression(t *testing.T) {
	parsed, err := schema.Parse(&projectQuotaRequestV36{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	for _, fixture := range []struct {
		name      string
		dialector gorm.Dialector
	}{
		{"postgres", postgres.New(postgres.Config{DSN: "host=localhost user=test dbname=test sslmode=disable"})},
		{"mysql", mysql.New(mysql.Config{DSN: "test:test@tcp(localhost:3306)/test", SkipInitializeWithVersion: true})},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			db, err := gorm.Open(fixture.dialector, &gorm.Config{DryRun: true, DisableAutomaticPing: true})
			if err != nil {
				t.Fatal(err)
			}
			expression := db.Migrator().FullDataTypeOf(parsed.FieldsByName["ApprovedPolicyJSON"])
			if !strings.Contains(expression.SQL, "NOT NULL DEFAULT ('')") || len(expression.Vars) != 0 {
				t.Fatalf("immutable TEXT default must remain a portable SQL expression: %s", expression.SQL)
			}
			pool, err := db.DB()
			if err != nil {
				t.Fatal(err)
			}
			if err := pool.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
