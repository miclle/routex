package handler

import (
	"context"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
)

// A complete, non-secret retained row for real-driver migration and historical
// refusal controls. Its scope is fixture data and carries no current authority.
func deliveryControllerMigrationGrantFixture(birth time.Time) entity.APIKeyDeliveryControllerGrant {
	suffix := "01arz3ndektsv4rrffq69g5fav"
	uuid := "00000000-0000-4000-8000-000000000001"
	digest := strings.Repeat("a", 64)
	return entity.APIKeyDeliveryControllerGrant{ID: "kcg_" + suffix, Name: "Migration fixture", EnrollmentRequestID: uuid, EnrollmentIntentDigest: digest, EnrollmentReview: digest, CredentialDigest: digest, OwnerKind: "personal", OwnerID: "usr_" + suffix, OwnerBirth: birth, OwnerDigest: digest, SlotID: "kds_" + suffix, SlotBirth: birth, ProfileID: "vap_" + suffix, ProfileBirth: birth, PrincipalID: "vpb_" + suffix, PrincipalBirth: birth, ExpiresAt: birth.Add(time.Hour), ReviewRevision: digest, AssignmentRequestID: uuid, AssignmentIntentDigest: digest, AssignmentReview: digest, ReviewedDescriptorRevision: 1, ReviewedBindingID: "kvb_" + suffix, ReviewedBindingBirth: birth, ReviewedKeyID: "key_" + suffix, ReviewedKeyBirth: birth, ConfigurationDigest: digest, ConfigurationReviewedAt: birth, ConfigurationReviewActorID: "usr_" + suffix, ConfigurationReviewBirth: birth, ConfigurationReviewReason: "Reviewed exact configuration", CreatedBy: "usr_" + suffix, CreatedByBirth: birth, CreationReason: "Explicit controller enrollment", CreatedAt: birth, UpdatedAt: birth}
}

func testAPIKeyDeliveryControllerMigration(t *testing.T, db *gorm.DB) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	ledger := personalKeyBehaviorLedger(t, db)
	if len(ledger) != 118 {
		t.Fatal("controller migration requires exact V116 registry before successor peel")
	}
	// This bounded V114 scenario must not retain a successor receipt while
	// reconstructing its predecessor. Refuse retained cleanup history first.
	if err := runtimeArchiveCleanupFixtureEmpty(db); err != nil {
		t.Fatal("cleanup history prevents bounded controller fixture", err)
	}
	if err := runtimeArchiveRollbackPeelV116(db, ledger); err != nil {
		t.Fatal("rollback history prevents bounded controller fixture", err)
	}
	ledger = ledger[:115]
	if err := db.Migrator().DropTable(&entity.RuntimeArchiveCleanup{}); err != nil {
		t.Fatal(err)
	}
	removed := db.Table("schema_migrations").Where("version = ? AND applied_at = ?", 115, ledger[114].AppliedAt).Delete(&struct{}{})
	if removed.Error != nil || removed.RowsAffected != 1 || !reflect.DeepEqual(personalKeyBehaviorLedger(t, db), ledger[:114]) {
		t.Fatal("exact successor peel changed controller prefix")
	}
	ledger = ledger[:114]
	t.Cleanup(func() {
		restore, release := context.WithTimeout(context.WithoutCancel(t.Context()), 2*time.Minute)
		defer release()
		if err := database.Migrate(restore, db); err != nil {
			t.Error("restore cleanup successor", err)
			return
		}
		if current := personalKeyBehaviorLedger(t, db); len(current) != 118 || !reflect.DeepEqual(current[:113], ledger[:113]) {
			t.Error("cleanup successor restore changed retained prefix")
		}
	})
	dropReceipt := func() {
		t.Helper()
		r := db.Table("schema_migrations").Where("version = ?", 114).Delete(&struct{}{})
		if r.Error != nil || r.RowsAffected != 1 {
			t.Fatal("remove only V114 receipt")
		}
	}
	dropTables := func() {
		t.Helper()
		for _, model := range []any{&entity.APIKeyDeliveryControllerAcknowledgement{}, &entity.APIKeyDeliveryControllerGrant{}} {
			if db.Migrator().DropTable(model) != nil {
				t.Fatal("remove only controller tables")
			}
		}
	}
	checkPrefix := func() {
		t.Helper()
		current := personalKeyBehaviorLedger(t, db)
		if len(current) != 114 || !reflect.DeepEqual(current[:113], ledger[:113]) {
			t.Fatal("V114 rewrote predecessor receipts")
		}
	}
	t.Run("empty_upgrade_repeat_concurrent", func(t *testing.T) {
		dropReceipt()
		dropTables()
		if err := database.MigrateThrough(ctx, db, 114); err != nil {
			t.Fatal("empty V114 upgrade failed")
		}
		checkPrefix()
		stable := personalKeyBehaviorLedger(t, db)
		if err := database.MigrateThrough(ctx, db, 114); err != nil {
			t.Fatal("V114 repeat failed")
		}
		if !reflect.DeepEqual(stable, personalKeyBehaviorLedger(t, db)) {
			t.Fatal("repeat changed receipts")
		}
		dropReceipt()
		var wg sync.WaitGroup
		results := make(chan error, 2)
		for range 2 {
			wg.Go(func() { results <- database.MigrateThrough(ctx, db, 114) })
		}
		wg.Wait()
		close(results)
		for err := range results {
			if err != nil {
				t.Fatal("concurrent V114 startup failed")
			}
		}
		checkPrefix()
	})
	t.Run("partial_empty_schema_repairs", func(t *testing.T) {
		if db.Migrator().DropConstraint(&entity.APIKeyDeliveryControllerGrant{}, "ck_delivery_controller_identity") != nil || db.Migrator().DropColumn(&entity.APIKeyDeliveryControllerGrant{}, "Name") != nil {
			t.Fatal("construct empty partial controller schema")
		}
		dropReceipt()
		if database.MigrateThrough(ctx, db, 114) != nil || !db.Migrator().HasColumn(&entity.APIKeyDeliveryControllerGrant{}, "Name") {
			t.Fatal("empty partial column not repaired")
		}
		checkPrefix()
	})
	birth := time.Now().UTC().Truncate(time.Microsecond)
	suffix := "01arz3ndektsv4rrffq69g5fav"
	uuid := "00000000-0000-4000-8000-000000000001"
	digest := strings.Repeat("a", 64)
	g := deliveryControllerMigrationGrantFixture(birth)
	t.Run("digest_uniqueness_owner_and_ack_state_constraints", func(t *testing.T) {
		if db.Create(&g).Error != nil {
			t.Fatal("seed exact bounded controller row")
		}
		// Compare the persisted row before and after replay. Drivers may return
		// an equivalent timestamp with a different time.Location from the input.
		var before entity.APIKeyDeliveryControllerGrant
		if db.Take(&before, "id = ?", g.ID).Error != nil {
			t.Fatal("read retained grant before migration replay")
		}
		duplicate := g
		duplicate.ID = "kcg_01arz3ndektsv4rrffq69g5faw"
		duplicate.EnrollmentRequestID = "00000000-0000-4000-8000-000000000002"
		if db.Create(&duplicate).Error == nil {
			t.Fatal("duplicate bearer verifier accepted")
		}
		for _, bad := range []string{"PERSONAL", "personal ", "unknown"} {
			if db.Model(&entity.APIKeyDeliveryControllerGrant{}).Where("id = ?", g.ID).Update("owner_kind", bad).Error == nil {
				t.Fatal("aliased owner kind accepted")
			}
		}
		ack := entity.APIKeyDeliveryControllerAcknowledgement{ID: "kca_" + suffix, GrantID: g.ID, GrantBirth: birth, RequestID: uuid, IntentDigest: digest, DescriptorRevision: 1, ConfigurationDigest: digest, BindingID: g.ReviewedBindingID, BindingBirth: birth, KeyID: g.ReviewedKeyID, KeyBirth: birth, AgentState: "running", OutputState: "written", RecordedAt: birth}
		if db.Create(&ack).Error != nil {
			t.Fatal("seed advisory controller report")
		}
		for _, bad := range []string{"completed", "WRITTEN", "written ", "applied"} {
			if db.Model(&entity.APIKeyDeliveryControllerAcknowledgement{}).Where("id = ?", ack.ID).Update("output_state", bad).Error == nil {
				t.Fatal("output promoted or aliased")
			}
		}
		dup := ack
		dup.ID = "kca_01arz3ndektsv4rrffq69g5faw"
		if db.Create(&dup).Error == nil {
			t.Fatal("duplicate acknowledgement UUID accepted")
		}
		dropReceipt()
		if database.MigrateThrough(ctx, db, 114) != nil {
			t.Fatal("retained controller replay failed")
		}
		checkPrefix()
		var retained entity.APIKeyDeliveryControllerGrant
		if db.Take(&retained, "id = ?", g.ID).Error != nil || !reflect.DeepEqual(retained, before) {
			t.Fatal("migration changed retained grant")
		}
	})
	t.Run("nonempty_partial_schema_never_synthesizes_birth", func(t *testing.T) {
		if db.Migrator().DropConstraint(&entity.APIKeyDeliveryControllerGrant{}, "ck_delivery_controller_lifetime") != nil || db.Migrator().DropColumn(&entity.APIKeyDeliveryControllerGrant{}, "RevocationActorBirth") != nil {
			t.Fatal("construct retained partial controller schema")
		}
		dropReceipt()
		if database.MigrateThrough(ctx, db, 114) == nil || db.Migrator().HasColumn(&entity.APIKeyDeliveryControllerGrant{}, "RevocationActorBirth") {
			t.Fatal("retained missing birth synthesized")
		}
		if current := personalKeyBehaviorLedger(t, db); len(current) != 113 || !reflect.DeepEqual(current, ledger[:113]) {
			t.Fatal("failed migration recorded V114 receipt")
		}
		dropTables()
		if database.MigrateThrough(ctx, db, 114) != nil {
			t.Fatal("restore only fixture-owned controller schema")
		}
		checkPrefix()
	})
}
