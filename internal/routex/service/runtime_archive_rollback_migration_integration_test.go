package service

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
)

// Read every successor fact before the first destructive historical-fixture DDL.
// This helper is test-only and never removes retained rollback/publication facts.
func peelRuntimeArchiveRollbackFixture(t *testing.T, db *gorm.DB) {
	t.Helper()
	peelRuntimePublicationAcknowledgementFixture(t, db)
	var before []runtimeArchiveCleanupLedger
	if err := db.Order("version").Find(&before).Error; err != nil || len(before) != 116 {
		t.Fatal("V116 complete ledger required for bounded peel", err)
	}
	for i, row := range before {
		if row.Version != i+1 || row.AppliedAt == "" {
			t.Fatal("incomplete V116 ledger")
		}
	}
	model := &entity.RuntimeArchiveRollback{}
	st := &gorm.Statement{DB: db}
	if err := st.Parse(model); err != nil {
		t.Fatal(err)
	}
	if !db.Migrator().HasTable(model) {
		t.Fatal("missing V116 successor")
	}
	for _, name := range st.Schema.DBNames {
		if !db.Migrator().HasColumn(model, name) {
			t.Fatal("incomplete V116 successor", name)
		}
	}
	for _, index := range st.Schema.ParseIndexes() {
		if !db.Migrator().HasIndex(model, index.Name) {
			t.Fatal("incomplete V116 index", index.Name)
		}
	}
	for name := range st.Schema.ParseCheckConstraints() {
		if !db.Migrator().HasConstraint(model, name) {
			t.Fatal("incomplete V116 guard", name)
		}
	}
	for _, name := range []string{"fk_archive_rollback_archive", "fk_archive_rollback_publication"} {
		if !db.Migrator().HasConstraint(model, name) {
			t.Fatal("incomplete V116 reference", name)
		}
	}
	var count int64
	if db.Model(model).Count(&count).Error != nil || count != 0 {
		t.Fatal("retained rollback prevents historical reconstruction")
	}
	if err := db.Migrator().DropTable(model); err != nil {
		t.Fatal(err)
	}
	removed := db.Where("version = ? AND applied_at = ?", 116, before[115].AppliedAt).Delete(&runtimeArchiveTestLedger{})
	var after []runtimeArchiveCleanupLedger
	if removed.Error != nil || removed.RowsAffected != 1 || db.Order("version").Find(&after).Error != nil || !reflect.DeepEqual(after, before[:115]) {
		t.Fatal("V116 peel changed retained prefix", removed.Error)
	}
}

func testRuntimeArchiveRollbackMigration(t *testing.T, db *gorm.DB) {
	t.Helper()
	peelRuntimePublicationAcknowledgementFixture(t, db)
	t.Cleanup(func() {
		if err := database.Migrate(context.WithoutCancel(t.Context()), db); err != nil {
			t.Error("restore current head after frozen V116 fixture", err)
		}
	})
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	var original []runtimeArchiveCleanupLedger
	if err := db.Order("version").Find(&original).Error; err != nil || len(original) != 116 {
		t.Fatal("V116 head required", err)
	}
	prefix := append([]runtimeArchiveCleanupLedger(nil), original[:115]...)
	ledger := func(t *testing.T) []runtimeArchiveCleanupLedger {
		t.Helper()
		var rows []runtimeArchiveCleanupLedger
		if e := db.Order("version").Find(&rows).Error; e != nil {
			t.Fatal(e)
		}
		return rows
	}
	check := func(t *testing.T) {
		t.Helper()
		rows := ledger(t)
		if len(rows) != 116 || rows[115].Version != 116 || rows[115].AppliedAt == "" || !reflect.DeepEqual(rows[:115], prefix) {
			t.Fatal("V116 changed released prefix")
		}
	}
	remove := func(t *testing.T) {
		t.Helper()
		rows := ledger(t)
		if len(rows) != 116 {
			t.Fatal("missing V116 receipt")
		}
		r := db.Where("version = ? AND applied_at = ?", 116, rows[115].AppliedAt).Delete(&runtimeArchiveTestLedger{})
		if r.Error != nil || r.RowsAffected != 1 || !reflect.DeepEqual(ledger(t), prefix) {
			t.Fatal("remove exact V116 receipt", r.Error)
		}
	}
	migrate := func(t *testing.T) {
		t.Helper()
		if e := database.MigrateThrough(ctx, db, 116); e != nil {
			t.Fatal("V116 startup", e)
		}
		check(t)
	}
	t.Run("empty_upgrade_repeat_concurrent", func(t *testing.T) {
		peelRuntimeArchiveRollbackFixture(t, db)
		if e := database.MigrateThrough(ctx, db, 115); e != nil || db.Migrator().HasTable(&entity.RuntimeArchiveRollback{}) {
			t.Fatal("bounded V115 fabricated rollback", e)
		}
		var wg sync.WaitGroup
		failures := make(chan error, 2)
		for range 2 {
			wg.Go(func() { failures <- database.MigrateThrough(ctx, db, 116) })
		}
		wg.Wait()
		close(failures)
		for e := range failures {
			if e != nil {
				t.Fatal("concurrent V116 startup", e)
			}
		}
		check(t)
		stable := ledger(t)
		migrate(t)
		if !reflect.DeepEqual(stable, ledger(t)) {
			t.Fatal("repeat replaced immutable V116 receipt")
		}
	})
	t.Run("empty_partial_columns_and_missing_restrictive_reference", func(t *testing.T) {
		remove(t)
		if e := db.Migrator().DropConstraint(&entity.RuntimeArchiveRollback{}, "ck_archive_rollback_reason"); e != nil {
			t.Fatal(e)
		}
		if e := db.Migrator().DropColumn(&entity.RuntimeArchiveRollback{}, "Reason"); e != nil {
			t.Fatal(e)
		}
		migrate(t)
		if !db.Migrator().HasColumn(&entity.RuntimeArchiveRollback{}, "Reason") {
			t.Fatal("empty missing field not repaired")
		}
		remove(t)
		if e := db.Migrator().DropConstraint(&entity.RuntimeArchiveRollback{}, "fk_archive_rollback_archive"); e != nil {
			t.Fatal(e)
		}
		migrate(t)
		if !db.Migrator().HasConstraint(&entity.RuntimeArchiveRollback{}, "fk_archive_rollback_archive") {
			t.Fatal("restrictive reference not repaired")
		}
	})
	stamp := time.Now().UTC().Truncate(time.Microsecond)
	header := entity.RuntimeConfigurationArchive{ID: configurationTestIDV1("rcv", 960), CreatedAt: stamp, CodecVersion: 1, ContentDigest: strings.Repeat("a", 64), SourceDigest: strings.Repeat("b", 64), AuthorizationDigest: strings.Repeat("c", 64), NetworkDigest: strings.Repeat("d", 64), PricingDigest: strings.Repeat("e", 64), PlaintextBytes: 1, ChunkCount: 1}
	publication := entity.RuntimePublicationCommand{RequestID: "605670ca-5a6a-4f98-8021-000000000960", ActorID: configurationTestIDV1("usr", 960), ActorCreatedAt: stamp, Reason: "Retained rollback migration fixture", ClaimInstanceID: configurationTestIDV1("ins", 960), ClaimInstanceStartedAt: stamp, ClaimedAt: stamp, Outcome: "claimed"}
	row := entity.RuntimeArchiveRollback{RequestID: publication.RequestID, ActorID: publication.ActorID, ActorCreatedAt: stamp, ArchiveID: header.ID, ArchiveCreatedAt: stamp, ReviewETag: strings.Repeat("a", 64), InputDigest: strings.Repeat("b", 64), DesiredDigest: strings.Repeat("c", 64), Reason: publication.Reason, Changed: true, AppliedAt: stamp}
	t.Run("retained_original_intent_references_and_bounds", func(t *testing.T) {
		for _, target := range []any{&header, &publication, &row} {
			if e := db.Create(target).Error; e != nil {
				t.Fatal("retained intent fixture", e)
			}
		}
		// Cleanup is registered before assertions and respects child-first references.
		defer func() {
			for _, target := range []any{&row, &publication, &header} {
				if e := db.Delete(target).Error; e != nil {
					t.Error(e)
				}
			}
		}()
		remove(t)
		migrate(t)
		var saved entity.RuntimeArchiveRollback
		if e := db.Take(&saved, "request_id = ?", row.RequestID).Error; e != nil {
			t.Fatal(e)
		}
		saved.ActorCreatedAt = saved.ActorCreatedAt.UTC()
		saved.ArchiveCreatedAt = saved.ArchiveCreatedAt.UTC()
		saved.AppliedAt = saved.AppliedAt.UTC()
		if !reflect.DeepEqual(saved, row) {
			t.Fatal("retained original intent rewritten")
		}
		for _, target := range []any{&header, &publication} {
			if e := db.Delete(target).Error; !errors.Is(e, gorm.ErrForeignKeyViolated) {
				t.Fatal("protected parent deletion admitted", e)
			}
		}
		for _, which := range []string{"missing_archive", "missing_publication", "reason", "review", "time"} {
			bad := row
			bad.RequestID = "605670ca-5a6a-4f98-8021-000000000961"
			pub := publication
			pub.RequestID = bad.RequestID
			if which != "missing_publication" {
				if e := db.Create(&pub).Error; e != nil {
					t.Fatal(e)
				}
			}
			switch which {
			case "missing_archive":
				bad.ArchiveID = configurationTestIDV1("rcv", 961)
			case "reason":
				bad.Reason = strings.Repeat("x", 4001)
			case "review":
				bad.ReviewETag = "bad"
			case "time":
				bad.AppliedAt = stamp.Add(-time.Microsecond)
			}
			if e := db.Create(&bad).Error; e == nil {
				t.Fatal("invalid rollback receipt admitted", which)
			}
			if which != "missing_publication" {
				if e := db.Delete(&pub).Error; e != nil {
					t.Fatal(e)
				}
			}
		}
		// Nonempty partial history is never assigned a synthesized original reason.
		remove(t)
		if e := db.Migrator().DropConstraint(&entity.RuntimeArchiveRollback{}, "ck_archive_rollback_reason"); e != nil {
			t.Fatal(e)
		}
		if e := db.Migrator().DropColumn(&entity.RuntimeArchiveRollback{}, "Reason"); e != nil {
			t.Fatal(e)
		}
		if e := database.MigrateThrough(ctx, db, 116); e == nil || db.Migrator().HasColumn(&entity.RuntimeArchiveRollback{}, "Reason") || !reflect.DeepEqual(ledger(t), prefix) {
			t.Fatal("nonempty partial intent was synthesized")
		}
		if e := db.Delete(&row).Error; e != nil {
			t.Fatal(e)
		}
		migrate(t)
	})
}
