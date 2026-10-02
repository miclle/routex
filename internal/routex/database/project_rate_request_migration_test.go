package database

import (
	"errors"
	"reflect"
	"sync"
	"testing"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

func TestFrozenProjectRateRequestSchema(t *testing.T) {
	parsed, err := schema.Parse(&projectRateRequestV37{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Table != "project_model_requests" || len(parsed.Fields) != 2 || len(parsed.PrimaryFields) != 0 || len(parsed.Relationships.Relations) != 0 || len(parsed.ParseIndexes()) != 0 {
		t.Fatal("V37 must only define checks on existing request columns")
	}
	for _, field := range parsed.Fields {
		if field.HasDefaultValue || field.NotNull || field.Size != 0 {
			t.Fatal("check-only migration must not redefine column types or defaults", field.Name)
		}
	}
	checks := parsed.ParseCheckConstraints()
	if len(checks) != 2 || checks["ck_project_request_kind_v37"].Constraint != "kind IN ('MODEL_ACCESS','QUOTA','RATE_LIMIT')" {
		t.Fatal("new kind guard must be distinct from released V36 and bounded")
	}
	if checks["ck_project_rate_approval"].Constraint != "kind <> 'RATE_LIMIT' OR status <> 'approved' OR (decision_review_etag <> '' AND approved_policy_etag <> '' AND decision_request_hash <> '' AND approved_policy_json <> '')" {
		t.Fatal("approved rate requests must retain all immutable approval evidence")
	}
	// Released quota checks remain frozen rather than inheriting the new kind.
	released, err := schema.Parse(&projectQuotaRequestV36{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil || released.ParseCheckConstraints()["ck_project_request_kind"].Constraint != "kind IN ('MODEL_ACCESS','QUOTA')" {
		t.Fatal("V37 must not rewrite the released quota migration", err)
	}
}

func TestProjectRateRequestInterruptedGuards(t *testing.T) {
	for _, failure := range []string{"create:ck_project_rate_approval", "create:ck_project_request_kind_v37", "drop:ck_project_request_kind"} {
		t.Run(failure, func(t *testing.T) {
			migrator := &projectRateCheckMigrator{checks: map[string]bool{"ck_project_request_kind": true, "ck_project_quota_approval": true}, fail: failure}
			db := projectRateCheckDB(t, migrator)
			if err := projectRateRequestMigration(db); !errors.Is(err, errInterruptedRateDDL) {
				t.Fatal("injected interrupted DDL was not propagated", err)
			}
			if !migrator.checks["ck_project_request_kind"] || !migrator.checks["ck_project_quota_approval"] {
				t.Fatal("failed upgrade removed a released guard")
			}
			migrator.fail = ""
			if err := projectRateRequestMigration(db); err != nil {
				t.Fatal("restart failed to repair interrupted guard upgrade", err)
			}
			if migrator.checks["ck_project_request_kind"] || !migrator.checks["ck_project_request_kind_v37"] || !migrator.checks["ck_project_rate_approval"] || !migrator.checks["ck_project_quota_approval"] {
				t.Fatal("resumed upgrade lost a guard or retained the two-kind restriction")
			}
			operations := append([]string(nil), migrator.operations...)
			if err := projectRateRequestMigration(db); err != nil || !reflect.DeepEqual(operations, migrator.operations) {
				t.Fatal("completed upgrade must not rewrite checks on repeat", err)
			}
		})
	}
}

var errInterruptedRateDDL = errors.New("interrupted rate request DDL")

type projectRateCheckMigrator struct {
	gorm.Migrator
	checks     map[string]bool
	operations []string
	fail       string
}

func (m *projectRateCheckMigrator) HasConstraint(_ any, name string) bool { return m.checks[name] }

func (m *projectRateCheckMigrator) CreateConstraint(_ any, name string) error {
	m.operations = append(m.operations, "create:"+name)
	if m.fail == "create:"+name {
		return errInterruptedRateDDL
	}
	m.checks[name] = true
	return nil
}

func (m *projectRateCheckMigrator) DropConstraint(_ any, name string) error {
	m.operations = append(m.operations, "drop:"+name)
	if !m.checks["ck_project_rate_approval"] || !m.checks["ck_project_request_kind_v37"] {
		return errors.New("old kind guard removed before installing both new guards")
	}
	if m.fail == "drop:"+name {
		return errInterruptedRateDDL
	}
	delete(m.checks, name)
	return nil
}

type projectRateCheckDialector struct {
	gorm.Dialector
	migrator gorm.Migrator
}

func (d projectRateCheckDialector) Migrator(*gorm.DB) gorm.Migrator { return d.migrator }

func projectRateCheckDB(t *testing.T, migrator gorm.Migrator) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(postgres.New(postgres.Config{DSN: "host=localhost user=test dbname=test sslmode=disable"}), &gorm.Config{DryRun: true, DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := pool.Close(); err != nil {
			t.Error(err)
		}
	})
	db.Dialector = projectRateCheckDialector{Dialector: db.Dialector, migrator: migrator}
	return db
}
