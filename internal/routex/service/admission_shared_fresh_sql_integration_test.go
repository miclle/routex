package service

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
)

// One explicitly enabled driver and exclusively owned, initially empty
// routex_test schema per worker. This source does not qualify V119/V120 DDL,
// authenticate deployment exclusion, or test an active shared runtime.
func TestSharedFreshDomainSQLIntegration(t *testing.T) {
	if os.Getenv("ROUTEX_TEST_SHARED_FRESH_SQL") != "1" {
		t.Skip("dedicated fresh shared domain worker is not enabled")
	}
	driver := os.Getenv("ROUTEX_TEST_SHARED_FRESH_SQL_DRIVER")
	if (driver != "postgres" && driver != "mysql") || os.Getenv("ROUTEX_TEST_SHARED_FRESH_SQL_OWNED") != "1" {
		t.Fatal("choose one exclusively owned postgres or mysql worker")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 180*time.Second)
	defer cancel()
	db := runtimeArchiveIntegrationDB(t, driver).WithContext(ctx)
	tables, err := db.Migrator().GetTables()
	if err != nil || len(tables) != 0 {
		t.Fatal("fresh worker requires an initially empty database", err)
	}
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal("complete registered predecessor migrations", err)
	}
	// Registered V118 supplies mode/owner tables without fresh mode rows.
	// These and all observable-history dependencies must precede the candidate
	// economic tables; an empty census cannot compensate for a missing table.
	for _, model := range []any{&entity.Installation{}, &entity.LimitInstallation{}, &entity.AdmissionMode{}, &entity.AdmissionSpoolOwner{},
		&entity.QuotaSetting{}, &entity.User{}, &entity.Session{}, &entity.Team{}, &entity.Project{}, &entity.APIKey{},
		&entity.CallRecord{}, &entity.CallAttempt{}, &entity.ResourceLimit{}, &entity.AuditEvent{}, &entity.SystemInstance{},
		&entity.CredentialSourceProcess{}, &entity.RuntimePublication{}, &entity.RuntimeRoutingApplication{}, &entity.RuntimeInstallationObservation{}} {
		if !db.Migrator().HasTable(model) {
			t.Fatal("registered predecessor dependency is missing")
		}
	}
	for _, model := range []any{&entity.AdmissionEconomicDomain{}, &entity.AdmissionEconomicOrigin{}, &entity.AdmissionEconomicReceipt{},
		&entity.AdmissionEconomicAttempt{}, &entity.AdmissionEconomicAccount{}, &entity.AdmissionEconomicCounter{},
		&entity.AdmissionEconomicContribution{}, &entity.AdmissionEconomicInvalidBound{}} {
		if db.Migrator().HasTable(model) {
			t.Fatal("registered migrations unexpectedly created candidate tables")
		}
		// Bounded test-only schema preparation; frozen migration qualification is
		// a separate worker. No candidate migration is added to the ledger here.
		if err := db.Migrator().CreateTable(model); err != nil {
			t.Fatal("test-only candidate table", err)
		}
	}
	var quota entity.QuotaSetting
	if err := db.Take(&quota, 1).Error; err != nil {
		t.Fatal(err)
	}
	intent := sharedFreshDomainIntent{InstallationID: "led_01arz3ndektsv4rrffq69g5fav", TimeZone: quota.TimeZone, CalendarETag: quota.ETag,
		BoundaryAt: time.Now().UTC().Add(-time.Second).UnixNano(), ReceiptCapacity: 100}
	original := sharedFreshSQLSnapshot(t, db)
	rollback := errors.New("test-owned rollback")

	for _, test := range []struct {
		name   string
		change func(*gorm.DB) error
	}{
		{"first_admin_already_initialized", func(tx *gorm.DB) error {
			return tx.Model(&entity.Installation{}).Where("id = ?", 1).Update("initialized", true).Error
		}},
		{"provisional_local_identity", func(tx *gorm.DB) error {
			return tx.Model(&entity.LimitInstallation{}).Where("id = ?", 1).Update("journal_id", intent.InstallationID).Error
		}},
		{"existing_local_mode", func(tx *gorm.DB) error {
			if err := tx.Model(&entity.LimitInstallation{}).Where("id = ?", 1).
				Updates(map[string]any{"initialized": true, "journal_id": intent.InstallationID}).Error; err != nil {
				return err
			}
			return tx.Omit("Installation").Create(&entity.AdmissionMode{ID: 1, InstallationID: intent.InstallationID, Mode: "local", Revision: 1}).Error
		}},
		{"quota_already_started", func(tx *gorm.DB) error {
			return tx.Model(&entity.QuotaSetting{}).Where("id = ?", 1).Update("accounting_started", true).Error
		}},
		{"changed_reviewed_calendar", func(tx *gorm.DB) error {
			return tx.Model(&entity.QuotaSetting{}).Where("id = ?", 1).Update("ETag", "qst_changed").Error
		}},
		{"economic_domain_without_origin", func(tx *gorm.DB) error {
			return tx.Create(&entity.AdmissionEconomicDomain{InstallationID: intent.InstallationID, FormatVersion: 1, Initialized: true,
				CoverageStart: intent.BoundaryAt, ClockHighWater: intent.BoundaryAt, ReceiptCapacity: 100}).Error
		}},
		{"retained_archived_resource", func(tx *gorm.DB) error {
			return tx.Create(&entity.Team{ID: "tem_01arz3ndektsv4rrffq69g5fav", Name: "retained", Status: entity.ResourceArchived}).Error
		}},
		{"retained_process_even_closed", func(tx *gorm.DB) error {
			at := time.Unix(0, intent.BoundaryAt).UTC().Truncate(time.Microsecond)
			return tx.Create(&entity.CredentialSourceProcess{ProcessID: "ins_01arz3ndektsv4rrffq69g5fav", Generation: strings.Repeat("a", 64), Birth: at, RegisteredAt: at, ClosedAt: &at}).Error
		}},
		{"previous_runtime_publication", func(tx *gorm.DB) error {
			return tx.Create(&entity.RuntimePublication{ID: "pub_01arz3ndektsv4rrffq69g5fav", SnapshotID: "snp_01arz3ndektsv4rrffq69g5fav", Status: "published"}).Error
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := db.Transaction(func(tx *gorm.DB) error {
				if err := lockGovernance(sharedAuthorizationDB(tx)); err != nil {
					return err
				}
				if err := test.change(tx); err != nil {
					return err
				}
				if proof, err := initializeFreshSharedDomainSQL(tx, intent); proof != nil || !errors.Is(err, errSharedAdmissionConflict) {
					t.Error("existing history became a fresh origin", err)
				}
				return rollback
			}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
			if !errors.Is(err, rollback) {
				t.Fatal("history fixture failed", err)
			}
			if !reflect.DeepEqual(original, sharedFreshSQLSnapshot(t, db)) {
				t.Fatal("refused constructor or fixture escaped rollback")
			}
		})
	}

	t.Run("caller_rollback_leaves_no_partial_origin", func(t *testing.T) {
		err := db.Transaction(func(tx *gorm.DB) error {
			if err := lockGovernance(sharedAuthorizationDB(tx)); err != nil {
				return err
			}
			if _, err := initializeFreshSharedDomainSQL(tx, intent); err != nil {
				return err
			}
			return rollback
		}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
		if !errors.Is(err, rollback) || !reflect.DeepEqual(original, sharedFreshSQLSnapshot(t, db)) {
			t.Fatal("successful uncommitted result left partial initialization", err)
		}
	})

	t.Run("concurrent_identical_initialization_and_lost_reply", func(t *testing.T) {
		type outcome struct {
			proof *sharedEconomicCalendarProof
			err   error
		}
		start := make(chan struct{})
		results := make(chan outcome, 2)
		for range 2 {
			go func() {
				<-start
				proof, err := sharedFreshSQLInitializeTransaction(db, intent)
				results <- outcome{proof, err}
			}()
		}
		close(start)
		for range 2 {
			select {
			case result := <-results:
				if result.err != nil || result.proof == nil || result.proof.CoverageStart != intent.BoundaryAt || result.proof.ModeRevision != 1 {
					t.Fatal("exact concurrent initialization failed", result.err)
				}
			case <-ctx.Done():
				t.Fatal("concurrent constructor exceeded worker bound", ctx.Err())
			}
		}
		state := sharedFreshSQLSnapshot(t, db)
		if len(state.Domains) != 1 || len(state.Origins) != 1 || len(state.Modes) != 1 || !state.Binding.Initialized || !state.Quota.AccountingStarted {
			t.Fatal("constructor duplicated or omitted atomic coordinates")
		}
		if _, err := sharedFreshSQLInitializeTransaction(db, intent); err != nil || !reflect.DeepEqual(state, sharedFreshSQLSnapshot(t, db)) {
			t.Fatal("lost-reply retry changed original initialization", err)
		}
	})

	target := sharedUsageReadTarget{InstallationID: intent.InstallationID, ModeRevision: 1}
	t.Run("calendar_read_is_coherent_and_read_only", func(t *testing.T) {
		before := sharedFreshSQLSnapshot(t, db)
		proof, err := sharedFreshSQLReadTransaction(db, target)
		if err != nil || proof == nil || proof.TimeZone != intent.TimeZone || proof.CalendarRevisionAtFreeze != intent.CalendarETag ||
			!reflect.DeepEqual(before, sharedFreshSQLSnapshot(t, db)) {
			t.Fatal("calendar reader changed storage or lost provenance", err)
		}
	})

	t.Run("setup_birth_after_origin_and_local_startup_refusal", func(t *testing.T) {
		svc, err := New(ctx, db)
		if err != nil {
			t.Fatal(err)
		}
		journal := filepath.Join(t.TempDir(), "journal.db")
		if err := svc.prepareLocalRecorderIdentity(ctx, journal); err == nil {
			t.Fatal("local recorder accepted initialized shared mode")
		}
		if _, err := os.Lstat(journal); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("refused local startup touched its journal", err)
		}
		if _, err := svc.Initialize(ctx, "fresh-admin@example.test", "test-only-password-20261015", "Fresh administrator"); err != nil {
			t.Fatal("real setup after origin", err)
		}
		var user entity.User
		if err := db.Take(&user).Error; err != nil || user.CreatedAt.UnixNano() < intent.BoundaryAt {
			t.Fatal("setup birth preceded real fresh coverage", err)
		}
		// Existing resources after a successful commit do not invalidate the
		// original same-intent acknowledgement.
		if _, err := sharedFreshSQLInitializeTransaction(db, intent); err != nil {
			t.Fatal("historical exact retry required resources to remain empty", err)
		}
	})

	t.Run("progressed_clock_and_same_zone_calendar_save_preserve_origin", func(t *testing.T) {
		if err := db.Transaction(func(tx *gorm.DB) error {
			if err := lockGovernance(sharedAuthorizationDB(tx)); err != nil {
				return err
			}
			if err := tx.Model(&entity.QuotaSetting{}).Where("id = ?", 1).Update("ETag", "qst_later").Error; err != nil {
				return err
			}
			return tx.Model(&entity.AdmissionEconomicDomain{}).Where("installation_id = ?", intent.InstallationID).Update("clock_high_water", intent.BoundaryAt+int64(time.Hour)).Error
		}); err != nil {
			t.Fatal(err)
		}
		state := sharedFreshSQLSnapshot(t, db)
		proof, err := sharedFreshSQLInitializeTransaction(db, intent)
		if err != nil || proof == nil || proof.CalendarETag != "qst_later" || proof.CalendarRevisionAtFreeze != intent.CalendarETag ||
			proof.ClockHighWater != intent.BoundaryAt+int64(time.Hour) || !reflect.DeepEqual(state, sharedFreshSQLSnapshot(t, db)) {
			t.Fatal("historical retry reset clock or required historical ETag to be current", err)
		}
	})

	for _, change := range []func(*sharedFreshDomainIntent){
		func(in *sharedFreshDomainIntent) { in.ReceiptCapacity++ },
		func(in *sharedFreshDomainIntent) { in.BoundaryAt++ },
		func(in *sharedFreshDomainIntent) { in.CalendarETag = "qst_later" },
		func(in *sharedFreshDomainIntent) { in.TimeZone = "Etc/UTC" },
		func(in *sharedFreshDomainIntent) { in.InstallationID = "led_01arz3ndektsv4rrffq69g5faw" },
	} {
		changed := intent
		change(&changed)
		before := sharedFreshSQLSnapshot(t, db)
		if proof, err := sharedFreshSQLInitializeTransaction(db, changed); proof != nil || !errors.Is(err, errSharedAdmissionConflict) ||
			!reflect.DeepEqual(before, sharedFreshSQLSnapshot(t, db)) {
			t.Fatal("different retry reinitialized or modified domain", err)
		}
	}

	for _, test := range []struct {
		name   string
		change func(*gorm.DB) error
	}{
		{"missing_origin", func(tx *gorm.DB) error {
			return tx.Where("installation_id = ?", intent.InstallationID).Delete(&entity.AdmissionEconomicOrigin{}).Error
		}},
		{"corrupt_origin_digest", func(tx *gorm.DB) error {
			return tx.Model(&entity.AdmissionEconomicOrigin{}).Where("installation_id = ?", intent.InstallationID).Update("evidence_digest", strings.Repeat("a", 64)).Error
		}},
		{"calendar_spelling_changed", func(tx *gorm.DB) error {
			return tx.Model(&entity.QuotaSetting{}).Where("id = ?", 1).Update("time_zone", "Etc/UTC").Error
		}},
		{"stale_mode", func(tx *gorm.DB) error {
			return tx.Model(&entity.AdmissionMode{}).Where("id = ?", 1).Update("revision", int64(2)).Error
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			before := sharedFreshSQLSnapshot(t, db)
			err := db.Transaction(func(tx *gorm.DB) error {
				if err := lockGovernance(sharedAuthorizationDB(tx)); err != nil {
					return err
				}
				if err := test.change(tx); err != nil {
					return err
				}
				if proof, err := readSharedEconomicCalendarSQL(tx, target); proof != nil || err == nil {
					t.Error("missing or inconsistent current provenance was accepted", err)
				}
				return rollback
			})
			if !errors.Is(err, rollback) || !reflect.DeepEqual(before, sharedFreshSQLSnapshot(t, db)) {
				t.Fatal("failed calendar read changed retained state", err)
			}
		})
	}
}

func sharedFreshSQLInitializeTransaction(db *gorm.DB, intent sharedFreshDomainIntent) (*sharedEconomicCalendarProof, error) {
	var proof *sharedEconomicCalendarProof
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(sharedAuthorizationDB(tx)); err != nil {
			return err
		}
		var err error
		proof, err = initializeFreshSharedDomainSQL(tx, intent)
		return err
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, err
	}
	return proof, nil
}

func sharedFreshSQLReadTransaction(db *gorm.DB, target sharedUsageReadTarget) (*sharedEconomicCalendarProof, error) {
	var proof *sharedEconomicCalendarProof
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(sharedAuthorizationDB(tx)); err != nil {
			return err
		}
		var err error
		proof, err = readSharedEconomicCalendarSQL(tx, target)
		return err
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, err
	}
	return proof, nil
}

type sharedFreshSQLState struct {
	Binding entity.LimitInstallation
	Quota   entity.QuotaSetting
	Modes   []entity.AdmissionMode
	Domains []entity.AdmissionEconomicDomain
	Origins []entity.AdmissionEconomicOrigin
}

func sharedFreshSQLSnapshot(t *testing.T, db *gorm.DB) sharedFreshSQLState {
	t.Helper()
	var state sharedFreshSQLState
	for _, target := range []any{&state.Binding, &state.Quota, &state.Modes, &state.Domains, &state.Origins} {
		if err := db.Find(target).Error; err != nil {
			t.Fatal("fresh worker snapshot", err)
		}
	}
	return state
}
