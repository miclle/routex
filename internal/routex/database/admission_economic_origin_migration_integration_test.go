package database

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"os"
	"reflect"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Dedicated candidate-only source: one initially empty, exclusively owned
// routex_test database per driver. It is not part of the complete matrix and
// cannot initialize a production origin or register migration V120.
func TestAdmissionEconomicOriginV120Integration(t *testing.T) {
	if os.Getenv("ROUTEX_TEST_ECONOMIC_ORIGIN_SCHEMA") != "1" {
		t.Skip("dedicated economic origin schema worker is not enabled")
	}
	driverName := os.Getenv("ROUTEX_TEST_ECONOMIC_ORIGIN_SCHEMA_DRIVER")
	dsn := os.Getenv("ROUTEX_TEST_ECONOMIC_ORIGIN_SCHEMA_DSN")
	if (driverName != "postgres" && driverName != "mysql") || dsn == "" || os.Getenv("ROUTEX_TEST_ECONOMIC_ORIGIN_SCHEMA_OWNED") != "1" {
		t.Fatal("choose one explicit owned postgres/mysql database")
	}
	db := migrationLockTestDB(t, driverName, dsn)
	nameQuery := "SELECT current_database()"
	if driverName == "mysql" {
		nameQuery = "SELECT DATABASE()"
	}
	var name string
	if err := db.Raw(nameQuery).Scan(&name).Error; err != nil || name != "routex_test" {
		t.Fatal("dedicated routex_test database required", err, name)
	}
	tables, err := db.Migrator().GetTables()
	if err != nil || len(tables) != 0 {
		t.Fatal("origin worker requires an initially empty database", err, tables)
	}
	model := &admissionEconomicOriginV120{}
	t.Run("missing_predecessor_refuses_without_creation", func(t *testing.T) {
		if err := applyEconomicOriginCandidateV120(t.Context(), db); err == nil {
			t.Fatal("origin migration accepted an absent predecessor")
		}
		if db.Migrator().HasTable(model) {
			t.Fatal("failed predecessor check created an origin table")
		}
	})
	if err := applyEconomicCandidateV119(t.Context(), db); err != nil {
		t.Fatal("separately explicit dormant predecessor", err)
	}
	t.Run("empty_creation_and_repeat_seed_nothing", func(t *testing.T) {
		for range 2 {
			if err := applyEconomicOriginCandidateV120(t.Context(), db); err != nil {
				t.Fatal(err)
			}
		}
		for _, table := range append(admissionEconomicModelsV119(), model) {
			var count int64
			if err := db.Model(table).Count(&count).Error; err != nil || count != 0 {
				t.Fatal("schema creation seeded economic or origin facts", count, err)
			}
		}
	})
	t.Run("empty_partial_ddl_missing_guarded_and_unguarded_columns", func(t *testing.T) {
		for _, check := range []string{"ck_economic_origin_calendar", "ck_economic_origin_body", "ck_economic_origin_time"} {
			if err := db.Migrator().DropConstraint(model, check); err != nil {
				t.Fatal(err)
			}
		}
		// Re-adding TimeZone installs a CHECK referencing the unguarded
		// CalendarRevisionAtFreeze; repair must add that dependency first.
		for _, field := range []string{"TimeZone", "CalendarRevisionAtFreeze", "ClockHighWaterAtFreeze", "EvidenceBody"} {
			if err := db.Migrator().DropColumn(model, field); err != nil {
				t.Fatal(err)
			}
		}
		if err := applyEconomicOriginCandidateV120(t.Context(), db); err != nil {
			t.Fatal("repair empty interrupted DDL", err)
		}
		if err := validateAdmissionEconomicColumnsV119(db, model, true); err != nil {
			t.Fatal(err)
		}
		if err := db.Migrator().DropConstraint(model, "fk_economic_origin_domain"); err != nil {
			t.Fatal(err)
		}
		if err := applyEconomicOriginCandidateV120(t.Context(), db); err != nil || !db.Migrator().HasConstraint(model, "fk_economic_origin_domain") {
			t.Fatal("missing restrictive FK was not repaired", err)
		}
	})
	var priorInstallation limitInstallationV14
	var state economicSchemaStateV119
	t.Run("existing_data_upgrade_does_not_infer_origin", func(t *testing.T) {
		if err := MigrateThrough(t.Context(), db, 118); err != nil {
			t.Fatal("released prefix", err)
		}
		if err := db.Take(&priorInstallation, 1).Error; err != nil {
			t.Fatal(err)
		}
		state = economicSchemaFixtureV119(t, db)
		if err := applyEconomicOriginCandidateV120(t.Context(), db); err != nil {
			t.Fatal(err)
		}
		economicSchemaAssertFixtureV119(t, db, state)
		var after limitInstallationV14
		if err := db.Take(&after, 1).Error; err != nil || after != priorInstallation {
			t.Fatal("origin migration modified installation/mode boundary", err)
		}
		var count int64
		if err := db.Model(model).Count(&count).Error; err != nil || count != 0 {
			t.Fatal("existing initialized domain manufactured calendar provenance", count, err)
		}
	})
	// Raw test-only storage fixture. Its presence is not constructor or genuine
	// fresh-installation evidence; service canonical validation is independent.
	origin := admissionEconomicOriginV120{InstallationID: state.domain.InstallationID, FormatVersion: 1, SourceKind: "fresh", TimeZone: "UTC",
		CalendarRevisionAtFreeze: "0", CoverageStart: state.domain.CoverageStart, ClockHighWaterAtFreeze: state.domain.CoverageStart,
		BoundaryAt: state.domain.CoverageStart, EvidenceDigest: economicDigestV119("source-only origin fixture"), EvidenceBody: []byte(`{"source_only_fixture":true}`)}
	if err := db.Omit(clause.Associations).Create(&origin).Error; err != nil {
		t.Fatal("test-only origin row", err)
	}
	requireOrigin := func(t *testing.T) {
		t.Helper()
		var actual admissionEconomicOriginV120
		if err := db.Take(&actual, "installation_id = ?", origin.InstallationID).Error; err != nil || !reflect.DeepEqual(actual, origin) {
			t.Fatal("migration changed original calendar/coverage/body/digest", err)
		}
		economicSchemaAssertFixtureV119(t, db, state)
	}
	t.Run("populated_repeat_and_concurrent_candidate_startup", func(t *testing.T) {
		peer := migrationLockTestDB(t, driverName, dsn)
		start, results := make(chan struct{}), make(chan error, 2)
		var workers sync.WaitGroup
		for _, handle := range []*gorm.DB{db, peer} {
			workers.Go(func() { <-start; results <- applyEconomicOriginCandidateV120(t.Context(), handle) })
		}
		close(start)
		workers.Wait()
		close(results)
		for err := range results {
			if err != nil {
				t.Fatal("serialized candidate repeat", err)
			}
		}
		requireOrigin(t)
	})
	t.Run("scalar_body_time_source_and_foreign_key_constraints", func(t *testing.T) {
		for _, change := range []map[string]any{
			{"format_version": int64(2)}, {"source_kind": "local_v2"}, {"source_kind": "FRESH"}, {"source_kind": "fresh "},
			{"time_zone": ""}, {"calendar_revision_at_freeze": ""}, {"coverage_start": int64(0)},
			{"coverage_start": int64(9222767236854775808)}, {"clock_high_water_at_freeze": origin.CoverageStart + 1},
			{"boundary_at": origin.CoverageStart - 1}, {"evidence_digest": "short"}, {"evidence_body": []byte{}},
			{"evidence_body": bytes.Repeat([]byte{'x'}, 131073)},
		} {
			economicSchemaRejectV119(t, db, func(tx *gorm.DB) error {
				return tx.Model(model).Where("installation_id = ?", origin.InstallationID).Updates(change).Error
			})
		}
		orphan := origin
		orphan.InstallationID = "led_01jjjjjjjjjjjjjjjjjjjjjjjk"
		economicSchemaRejectV119(t, db, func(tx *gorm.DB) error { return tx.Omit(clause.Associations).Create(&orphan).Error })
		economicSchemaRejectV119(t, db, func(tx *gorm.DB) error { return tx.Omit(clause.Associations).Create(&origin).Error })
		// A separate parent without other history proves that origin's own FK,
		// rather than another V119 receipt FK, prevents parent deletion/update.
		parent := state.domain
		parent.InstallationID = "led_01jjjjjjjjjjjjjjjjjjjjjjjm"
		parent.ReceiptCount = 0
		if err := db.Create(&parent).Error; err != nil {
			t.Fatal(err)
		}
		child := origin
		child.InstallationID = parent.InstallationID
		if err := db.Omit(clause.Associations).Create(&child).Error; err != nil {
			t.Fatal(err)
		}
		economicSchemaRejectV119(t, db, func(tx *gorm.DB) error {
			return tx.Where("installation_id = ?", parent.InstallationID).Delete(&admissionEconomicDomainV119{}).Error
		})
		economicSchemaRejectV119(t, db, func(tx *gorm.DB) error {
			return tx.Model(&admissionEconomicDomainV119{}).Where("installation_id = ?", parent.InstallationID).
				Update("installation_id", "led_01jjjjjjjjjjjjjjjjjjjjjjjn").Error
		})
		requireOrigin(t)
	})
	t.Run("wrong_existing_scalar_shape_is_rejected_without_rewrite", func(t *testing.T) {
		if err := db.Migrator().AlterColumn(&economicOriginWrongWidthV120{}, "CalendarRevisionAtFreeze"); err != nil {
			t.Fatal(err)
		}
		if err := applyEconomicOriginCandidateV120(t.Context(), db); err == nil {
			t.Fatal("wrong existing scalar representation was accepted")
		}
		if err := db.Migrator().AlterColumn(model, "CalendarRevisionAtFreeze"); err != nil {
			t.Fatal("restore explicit test-only scalar shape", err)
		}
		if err := applyEconomicOriginCandidateV120(t.Context(), db); err != nil {
			t.Fatal(err)
		}
		requireOrigin(t)
	})
	t.Run("populated_missing_body_is_not_synthesized", func(t *testing.T) {
		if err := db.Migrator().DropConstraint(model, "ck_economic_origin_body"); err != nil {
			t.Fatal(err)
		}
		if err := db.Migrator().DropColumn(model, "EvidenceBody"); err != nil {
			t.Fatal(err)
		}
		if err := applyEconomicOriginCandidateV120(t.Context(), db); err == nil {
			t.Fatal("missing populated provenance body was manufactured")
		}
		if db.Migrator().HasColumn(model, "EvidenceBody") {
			t.Fatal("failed migration repaired populated provenance")
		}
		var retained struct {
			EvidenceDigest string
			CoverageStart  int64
		}
		if err := db.Model(model).Select("evidence_digest", "coverage_start").Where("installation_id = ?", origin.InstallationID).Take(&retained).Error; err != nil || retained.EvidenceDigest != origin.EvidenceDigest || retained.CoverageStart != origin.CoverageStart {
			t.Fatal("failed repair changed remaining retained facts", err)
		}
		economicSchemaAssertFixtureV119(t, db, state)
	})
}

type economicOriginWrongWidthV120 struct {
	CalendarRevisionAtFreeze string `gorm:"size:65;not null"`
}

func (economicOriginWrongWidthV120) TableName() string { return "admission_economic_origins" }

// Candidate-only same-connection lock mirrors the existing migration runner.
// The dialect SQL is required because GORM has no session advisory-lock API.
// It creates no ledger version, and never invokes a released migration itself.
func applyEconomicOriginCandidateV120(ctx context.Context, db *gorm.DB) error {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	return db.WithContext(ctx).Connection(func(conn *gorm.DB) (result error) {
		conn = conn.Session(&gorm.Session{NewDB: true})
		release := "SELECT pg_advisory_unlock(724683901)"
		switch conn.Name() {
		case "postgres":
			if err := conn.Exec("SELECT pg_advisory_lock(724683901)").Error; err != nil {
				return err
			}
		case "mysql":
			var locked sql.NullInt64
			wait, err := migrationLockWaitSeconds(ctx)
			if err != nil {
				return err
			}
			if err := conn.Raw("SELECT GET_LOCK('routex_schema_migrations', ?)", wait).Scan(&locked).Error; err != nil {
				return err
			}
			if !locked.Valid || locked.Int64 != 1 {
				return fmt.Errorf("economic origin candidate migration lock unavailable")
			}
			release = "SELECT RELEASE_LOCK('routex_schema_migrations')"
		default:
			return fmt.Errorf("unsupported economic origin schema driver")
		}
		defer func() {
			releaseCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
			defer stop()
			if err := conn.WithContext(releaseCtx).Exec(release).Error; err != nil {
				result = errors.Join(result, err)
				if sqlConn, ok := conn.Statement.ConnPool.(*sql.Conn); ok {
					_ = sqlConn.Raw(func(any) error { return driver.ErrBadConn })
				}
			}
		}()
		return admissionEconomicOriginMigration(conn)
	})
}
