package handler

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
)

func testCredentialRetirementMigration(t *testing.T, db *gorm.DB) {
	t.Helper()
	model := &entity.CredentialRetirementReceipt{}
	// Reconstruct the released V33 boundary without changing any earlier step.
	if err := db.Migrator().DropTable(model); err != nil {
		t.Fatal(err)
	}
	removeLedger := func() {
		result := db.Table("schema_migrations").Where("version = ?", 34).Delete(&struct{}{})
		if result.Error != nil || result.RowsAffected != 1 {
			t.Fatal("cannot reconstruct V34 ledger")
		}
	}
	removeLedger()
	var wg sync.WaitGroup
	failures := make(chan error, 2)
	for range 2 {
		wg.Go(func() { failures <- database.Migrate(context.Background(), db) })
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	assertSchema := func() {
		if !db.Migrator().HasTable(model) || !db.Migrator().HasConstraint(model, "ck_credential_retirement_distinct") {
			t.Fatal("historical retirement schema missing")
		}
		columns, err := db.Migrator().ColumnTypes(model)
		if err != nil {
			t.Fatal(err)
		}
		if len(columns) != 12 {
			t.Fatalf("retirement receipt columns: %d", len(columns))
		}
		for _, column := range columns {
			if nullable, ok := column.Nullable(); ok && nullable {
				t.Fatalf("receipt column %s unexpectedly nullable", column.Name())
			}
		}
	}
	assertSchema()
	now := time.Now().UTC().Truncate(time.Microsecond)
	receipt := entity.CredentialRetirementReceipt{RequestID: "81b043bb-cb96-419c-b9a2-cab04814b65e", ActorID: "usr_historical_retirement", SourceCredentialID: "crd_historical_source", ReplacementCredentialID: "crd_historical_successor", ConnectionID: "con_historical_retirement", RequestHash: strings.Repeat("a", 64), ReadinessETag: strings.Repeat("b", 64), SourceETag: strings.Repeat("c", 64), ReplacementETag: strings.Repeat("d", 64), PreDisableSnapshotID: "cfg_historical", EvidenceAttemptID: "attempt_historical", CommittedAt: now}
	if err := db.Create(&receipt).Error; err != nil {
		t.Fatal("receipt must retain historical IDs without live foreign keys", err)
	}
	duplicate := receipt
	if err := db.Create(&duplicate).Error; err == nil {
		t.Fatal("request UUID uniqueness absent")
	}
	distinctRequest := receipt
	distinctRequest.RequestID = "91b043bb-cb96-419c-b9a2-cab04814b65e"
	if err := db.Create(&distinctRequest).Error; err != nil {
		t.Fatal("fresh intents for re-enabled sources must remain possible", err)
	}
	invalid := receipt
	invalid.RequestID = "a1b043bb-cb96-419c-b9a2-cab04814b65e"
	invalid.ReplacementCredentialID = invalid.SourceCredentialID
	if err := db.Create(&invalid).Error; err == nil {
		t.Fatal("source and successor must be distinct")
	}
	missing := db.Model(model).Where("request_id = ?", receipt.RequestID).Update("actor_id", nil)
	if missing.Error == nil {
		t.Fatal("required historical identity allowed NULL")
	}
	// A table created before a nontransactional CHECK DDL or ledger write is a
	// valid interrupted migration. Re-entry repairs its constraint without loss.
	if err := db.Migrator().DropConstraint(model, "ck_credential_retirement_distinct"); err != nil {
		t.Fatal(err)
	}
	removeLedger()
	if err := database.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	assertSchema()
	var retained entity.CredentialRetirementReceipt
	if err := db.First(&retained, "request_id = ?", receipt.RequestID).Error; err != nil || retained.RequestHash != receipt.RequestHash || retained.SourceCredentialID != receipt.SourceCredentialID || !retained.CommittedAt.Equal(now) {
		t.Fatal("migration repair altered committed historical intent", err)
	}
	if err := database.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
}
