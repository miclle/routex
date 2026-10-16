package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
)

// This explicitly gated worker requires two FRESH disposable routex_test
// databases. It is not wired into the maintained runner yet, and must never
// share a schema with its handler/archive workers or a running application.
func TestAdmissionOwnerStoreIntegration(t *testing.T) {
	if os.Getenv("ROUTEX_TEST_ADMISSION_OWNERS") != "1" {
		t.Skip("requires the exclusive disposable owner-foundation worker")
	}
	for _, driver := range []string{"postgres", "mysql"} {
		t.Run(driver, func(t *testing.T) { testAdmissionOwnerDatabase(t, driver) })
	}
}

type admissionOwnerTestLedger struct {
	Version   int    `gorm:"primaryKey;autoIncrement:false"`
	AppliedAt string `gorm:"size:40;not null"`
}

func (admissionOwnerTestLedger) TableName() string { return "schema_migrations" }

type admissionOwnerPartialMode struct {
	ID int `gorm:"primaryKey;autoIncrement:false"`
}

func (admissionOwnerPartialMode) TableName() string { return "admission_modes" }

func testAdmissionOwnerDatabase(t *testing.T, driver string) {
	db := runtimeArchiveIntegrationDB(t, driver)
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	if db.Migrator().HasTable(&entity.AdmissionMode{}) || db.Migrator().HasTable(&entity.AdmissionSpoolOwner{}) {
		t.Fatal("owner worker requires a fresh schema")
	}
	if err := database.MigrateThrough(ctx, db, 117); err != nil {
		t.Fatal("released V117 empty creation", err)
	}
	var history []admissionOwnerTestLedger
	if err := db.Order("version").Find(&history).Error; err != nil || len(history) != 117 {
		t.Fatal("released prefix unavailable", err)
	}
	requirePrefix := func(t *testing.T) {
		t.Helper()
		var got []admissionOwnerTestLedger
		if err := db.Where("version <= ?", 117).Order("version").Find(&got).Error; err != nil || !reflect.DeepEqual(history, got) {
			t.Fatal("released migration history changed", err)
		}
	}
	var binding entity.LimitInstallation
	if err := db.Take(&binding, 1).Error; err != nil || binding.Initialized || binding.JournalID != "" {
		t.Fatal("fresh journal unexpectedly initialized", err)
	}
	// A malformed established binding must fail without inventing a mode or
	// advancing the ledger; interrupted empty DDL remains repeatable afterward.
	if err := db.Model(&binding).Update("initialized", true).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(ctx, db); err == nil {
		t.Fatal("missing initialized journal identity admitted")
	}
	var premature int64
	if err := db.Model(&entity.AdmissionMode{}).Count(&premature).Error; err != nil || premature != 0 {
		t.Fatal("invalid initialized binding seeded mode", err)
	}
	if err := db.Model(&admissionOwnerTestLedger{}).Where("version = ?", 118).Count(&premature).Error; err != nil || premature != 0 {
		t.Fatal("invalid initialized binding advanced ledger", err)
	}
	requirePrefix(t)
	if err := db.Model(&binding).Update("initialized", false).Error; err != nil {
		t.Fatal(err)
	}
	// Concurrent migration calls use the existing dedicated-connection migration
	// lock; no owner/mode activation is smuggled into empty database creation.
	var wg sync.WaitGroup
	migrationErrors := make(chan error, 3)
	for range 3 {
		wg.Go(func() { migrationErrors <- database.Migrate(ctx, db) })
	}
	wg.Wait()
	close(migrationErrors)
	for err := range migrationErrors {
		if err != nil {
			t.Fatal("concurrent V118 empty creation", err)
		}
	}
	var count int64
	if err := db.Model(&entity.AdmissionMode{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatal("uninitialized journal received a mode identity", err)
	}
	requirePrefix(t)
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal("repeat empty V118", err)
	}
	in := admissionOwnerFixture()
	if err := db.Model(&binding).Updates(map[string]any{"journal_id": in.Owner.InstallationID, "initialized": true}).Error; err != nil {
		t.Fatal(err)
	}
	// Reconstruct only this unreleased empty V118 suffix to test a genuine V117
	// initialized-journal upgrade, leaving every released row byte-for-byte.
	resetSuffix := func(t *testing.T) {
		t.Helper()
		if err := db.Migrator().DropTable(&entity.AdmissionSpoolOwner{}, &entity.AdmissionMode{}); err != nil {
			t.Fatal(err)
		}
		if err := db.Where("version = ?", 118).Delete(&admissionOwnerTestLedger{}).Error; err != nil {
			t.Fatal(err)
		}
	}
	resetSuffix(t)
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal("initialized V117 upgrade", err)
	}
	var mode entity.AdmissionMode
	if err := db.Take(&mode, 1).Error; err != nil || mode.Mode != "local" || mode.Revision != 1 || mode.InstallationID != in.Owner.InstallationID {
		t.Fatal("upgrade did not retain exact local journal", err)
	}
	requirePrefix(t)
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal("repeat initialized V118", err)
	}
	store := &admissionOwnerStore{db: db}
	if _, err := store.RegisterOwner(ctx, in); !errors.Is(err, errAdmissionOwnerConflict) {
		t.Fatal("local default activated shared registration", err)
	}
	// Partial MySQL DDL policy: complete only an EMPTY table. Never synthesize
	// identity/history for a retained row with missing scalar facts.
	resetSuffix(t)
	if err := db.Migrator().CreateTable(&admissionOwnerPartialMode{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&admissionOwnerPartialMode{ID: 1}).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(ctx, db); err == nil {
		t.Fatal("nonempty partial mode accepted")
	}
	var partial []admissionOwnerPartialMode
	if err := db.Select("id").Find(&partial).Error; err != nil || len(partial) != 1 || partial[0].ID != 1 {
		t.Fatal("failed partial repair rewrote retained row", err)
	}
	requirePrefix(t)
	if err := db.Where("id = ?", 1).Delete(&admissionOwnerPartialMode{}).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal("empty partial mode did not reconcile", err)
	}
	requirePrefix(t)
	// Shared mode is TEST FIXTURE ONLY. This slice exposes no activation path.
	if err := db.Model(&entity.AdmissionMode{}).Where("id = ?", 1).Updates(map[string]any{"mode": "shared", "revision": int64(2)}).Error; err != nil {
		t.Fatal(err)
	}
	started := time.Now().UTC().Add(-time.Minute).Truncate(time.Microsecond)
	in.InstanceStartedAt = started
	process := entity.SystemInstance{ID: in.Owner.IncarnationID, LeaseToken: "lck_01jjjjjjjjjjjjjjjjjjjjjjjj", HeartbeatRevision: 1, Role: "combined", StartedAt: started, LastHeartbeatAt: started, LeaseExpiresAt: started.Add(time.Second)}
	if err := db.Create(&process).Error; err != nil {
		t.Fatal("captured process fixture", err)
	}
	current := func(t *testing.T, in admissionOwnerRegistration) error {
		t.Helper()
		return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error { return store.CheckCurrentOwner(tx, in.Owner, in.InstanceStartedAt) }, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	}

	t.Run("exact_registration_and_lost_response", func(t *testing.T) {
		first, err := store.RegisterOwner(ctx, in)
		if err != nil || first == nil {
			t.Fatal("registration", err)
		}
		// Discard the first returned acknowledgement and retry the original UUID.
		// This proves durable retry, not synthetic SQL commit-outcome certainty.
		again, err := store.RegisterOwner(ctx, in)
		if err != nil || !reflect.DeepEqual(first, again) {
			t.Fatal("exact retry changed original receipt", err)
		}
		if err := current(t, in); err != nil {
			t.Fatal("current owner guard", err)
		}
		for _, mutate := range []func(*admissionOwnerRegistration){
			func(x *admissionOwnerRegistration) { x.Owner.SpoolID = "another_spool" },
			func(x *admissionOwnerRegistration) { x.Owner.ModeRevision++ },
			func(x *admissionOwnerRegistration) { x.InstanceStartedAt = x.InstanceStartedAt.Add(time.Microsecond) },
		} {
			bad := in
			mutate(&bad)
			if _, err := store.RegisterOwner(ctx, bad); !errors.Is(err, errAdmissionOwnerConflict) {
				t.Fatal("UUID accepted changed body", err)
			}
		}
		another := in
		another.RegistrationID = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
		if _, err := store.RegisterOwner(ctx, another); !errors.Is(err, errAdmissionOwnerConflict) {
			t.Fatal("expired heartbeat allowed takeover", err)
		}
	})
	t.Run("terminal_fence_retains_historical_registration", func(t *testing.T) {
		original, err := store.RegisterOwner(ctx, in)
		if err != nil {
			t.Fatal(err)
		}
		intent := admissionOwnerFence{FencingID: "cccccccc-cccc-4ccc-8ccc-cccccccccccc", Owner: in.Owner, InstanceStartedAt: in.InstanceStartedAt}
		first, err := store.FenceOwner(ctx, intent)
		if err != nil || first == nil || first.ResultFence != 2 {
			t.Fatal("terminal fence", err)
		}
		again, err := store.FenceOwner(ctx, intent)
		if err != nil || !reflect.DeepEqual(first, again) {
			t.Fatal("fencing retry rewrote history", err)
		}
		history, err := store.RegisterOwner(ctx, in)
		if err != nil || !reflect.DeepEqual(original, history) {
			t.Fatal("fence erased historical registration", err)
		}
		// Current mode/revision and process birth are not part of the original
		// acknowledgement's continued validity as historical intent evidence.
		if err := db.Model(&entity.AdmissionMode{}).Where("id = ?", 1).Updates(map[string]any{"mode": "local", "revision": int64(3)}).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Model(&process).Update("started_at", started.Add(time.Microsecond)).Error; err != nil {
			t.Fatal(err)
		}
		historical, err := store.RegisterOwner(ctx, in)
		if err != nil || !reflect.DeepEqual(original, historical) {
			t.Fatal("mode/birth rollover blocked historical registration", err)
		}
		fenced, err := store.FenceOwner(ctx, intent)
		if err != nil || !reflect.DeepEqual(first, fenced) {
			t.Fatal("mode/birth rollover blocked historical fencing receipt", err)
		}
		if err := current(t, in); !errors.Is(err, errAdmissionOwnerConflict) {
			t.Fatal("historical receipt became current authority", err)
		}
		if err := db.Model(&entity.AdmissionMode{}).Where("id = ?", 1).Updates(map[string]any{"mode": "shared", "revision": int64(2)}).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Model(&process).Update("started_at", started).Error; err != nil {
			t.Fatal(err)
		}
		if err := current(t, in); !errors.Is(err, errAdmissionOwnerConflict) {
			t.Fatal("fenced owner remains current", err)
		}
		changed := intent
		changed.FencingID = "dddddddd-dddd-4ddd-8ddd-dddddddddddd"
		if _, err := store.FenceOwner(ctx, changed); !errors.Is(err, errAdmissionOwnerConflict) {
			t.Fatal("second terminal intent admitted", err)
		}
		changed = intent
		changed.InstanceStartedAt = changed.InstanceStartedAt.Add(time.Microsecond)
		if _, err := store.FenceOwner(ctx, changed); !errors.Is(err, errAdmissionOwnerConflict) {
			t.Fatal("fencing retry accepted foreign process birth", err)
		}
	})
	t.Run("concurrent_registration_and_cancellation", func(t *testing.T) {
		a := in
		a.Owner.SpoolID = "contended_spool"
		a.RegistrationID = "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee"
		b := a
		b.RegistrationID = "ffffffff-ffff-4fff-8fff-ffffffffffff"
		errs := make(chan error, 2)
		for _, intent := range []admissionOwnerRegistration{a, b} {
			wg.Go(func() { _, err := store.RegisterOwner(ctx, intent); errs <- err })
		}
		wg.Wait()
		close(errs)
		successes, conflicts := 0, 0
		for err := range errs {
			if err == nil {
				successes++
			} else if errors.Is(err, errAdmissionOwnerConflict) {
				conflicts++
			} else {
				t.Fatal("unexpected concurrent result", err)
			}
		}
		if successes != 1 || conflicts != 1 {
			t.Fatal("slot admitted competing registration", successes, conflicts)
		}
		canceled, stop := context.WithCancel(ctx)
		stop()
		denied := in
		denied.Owner.SpoolID = "canceled_spool"
		denied.RegistrationID = "11111111-1111-4111-8111-111111111111"
		if _, err := store.RegisterOwner(canceled, denied); !errors.Is(err, context.Canceled) {
			t.Fatal("canceled registration accepted", err)
		}
		var n int64
		if err := db.Model(&entity.AdmissionSpoolOwner{}).Where("spool_id = ?", denied.Owner.SpoolID).Count(&n).Error; err != nil || n != 0 {
			t.Fatal("canceled registration persisted", err)
		}
	})
	t.Run("foreign_mode_birth_alias_and_constraints", func(t *testing.T) {
		base := in
		base.Owner.SpoolID = "foreign_control"
		base.RegistrationID = "22222222-2222-4222-8222-222222222222"
		for _, mutate := range []func(*admissionOwnerRegistration){
			func(x *admissionOwnerRegistration) { x.InstanceStartedAt = x.InstanceStartedAt.Add(time.Microsecond) },
			func(x *admissionOwnerRegistration) { x.Owner.ModeRevision = 3 },
			func(x *admissionOwnerRegistration) { x.Owner.InstallationID = "led_02jjjjjjjjjjjjjjjjjjjjjjjj" },
		} {
			bad := base
			mutate(&bad)
			if _, err := store.RegisterOwner(ctx, bad); !errors.Is(err, errAdmissionOwnerConflict) {
				t.Fatal("foreign durable identity admitted", err)
			}
		}
		alias := base
		alias.Owner.IncarnationID = "INS_01JJJJJJJJJJJJJJJJJJJJJJJJ"
		if _, err := store.RegisterOwner(ctx, alias); !errors.Is(err, errAdmissionOwnerInvalid) {
			t.Fatal("collation alias admitted", err)
		}
		accepted, err := store.RegisterOwner(ctx, base)
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Model(&process).Update("started_at", started.Add(time.Microsecond)).Error; err != nil {
			t.Fatal(err)
		}
		if err := current(t, base); !errors.Is(err, errAdmissionOwnerConflict) {
			t.Fatal("reborn same process ID remained current", err)
		}
		if err := db.Model(&process).Update("started_at", started).Error; err != nil {
			t.Fatal(err)
		}
		stopped := time.Now().UTC().Truncate(time.Microsecond)
		if err := db.Model(&process).Updates(map[string]any{"stopped_at": stopped, "lease_expires_at": started}).Error; err != nil {
			t.Fatal(err)
		}
		if err := current(t, base); err != nil {
			t.Fatal("advisory lease metadata changed owner authority", err)
		}
		if err := db.Delete(&process).Error; err != nil {
			t.Fatal("owner unexpectedly pins process cleanup", err)
		}
		if err := current(t, base); !errors.Is(err, errAdmissionOwnerConflict) {
			t.Fatal("missing current process admitted", err)
		}
		retained, err := store.RegisterOwner(ctx, base)
		if err != nil || !reflect.DeepEqual(accepted, retained) {
			t.Fatal("process metadata cleanup erased original receipt", err)
		}
		if err := db.Delete(&entity.AdmissionMode{}, 1).Error; err == nil {
			t.Fatal("owner lost its mode FK")
		}
		for _, alias := range []string{"LOCAL", "Local", "local ", "local           ", "SHARED", "shared ", "shared          "} {
			if err := db.Model(&entity.AdmissionMode{}).Where("id = ?", 1).Update("mode", alias).Error; err == nil {
				t.Fatal("collation-alias mode accepted", alias)
			}
		}
		var currentMode entity.AdmissionMode
		if err := db.Take(&currentMode, 1).Error; err != nil || currentMode.Mode != "shared" || currentMode.Revision != 2 {
			t.Fatal("rejected mode aliases rewrote current mode", err)
		}
		if err := db.Delete(&entity.LimitInstallation{}, 1).Error; err == nil {
			t.Fatal("mode lost its installation FK")
		}
		for _, values := range []map[string]any{{"current_fence": 3}, {"fencing_id": "invalid"}, {"registered_fence": 0}, {"mode_revision": 0}} {
			err := db.Model(&entity.AdmissionSpoolOwner{}).Where("installation_id = ? AND spool_id = ?", base.Owner.InstallationID, base.Owner.SpoolID).Updates(values).Error
			if err == nil {
				t.Fatal("invalid owner constraint accepted", fmt.Sprint(values))
			}
		}
		var retainedRow entity.AdmissionSpoolOwner
		if err := db.Where("installation_id = ? AND spool_id = ?", base.Owner.InstallationID, base.Owner.SpoolID).Take(&retainedRow).Error; err != nil || !admissionOwnerRowValid(retainedRow) {
			t.Fatal("constraint failures damaged retained owner", err)
		}
	})
	requirePrefix(t)
}
