package handler

import (
	"context"
	"sync"
	"testing"

	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
)

func testCredentialReplacementMigration(t *testing.T, db *gorm.DB) {
	t.Helper()
	provider := entity.Provider{ID: "prv_replacement_upgrade", Name: "Upgrade provider"}
	connection := entity.ProviderConnection{ID: "con_replacement_upgrade", ProviderID: provider.ID, Name: "Upgrade connection", BaseURL: "https://example.invalid/v1", Protocol: entity.ProtocolOpenAIChat}
	legacy := entity.ProviderCredential{ID: "crd_replacement_upgrade", ConnectionID: connection.ID, Name: "Legacy credential", Ciphertext: "test-only-legacy-encrypted-envelope", Priority: 9, VerificationStatus: "pending"}
	for _, row := range []any{&provider, &connection, &legacy} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	// Reconstruct V30, retaining its records. This tests an actual additive
	// upgrade instead of mutating any released migration definition.
	if err := db.Migrator().DropTable(&entity.CredentialReplacementReceipt{}); err != nil {
		t.Fatal(err)
	}
	dropCredentialReplacementTestIndex(t, db, &entity.ProviderCredential{}, "idx_credentials_replaces")
	if err := db.Migrator().DropColumn(&entity.ProviderCredential{}, "ReplacesCredentialID"); err != nil {
		t.Fatal(err)
	}
	removeCredentialReplacementLedger := func() {
		result := db.Table("schema_migrations").Where("version = ?", 31).Delete(&struct{}{})
		if result.Error != nil || result.RowsAffected != 1 {
			t.Fatal("cannot reconstruct replacement migration ledger")
		}
	}
	removeCredentialReplacementLedger()
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
		if !db.Migrator().HasColumn(&entity.ProviderCredential{}, "ReplacesCredentialID") || !db.Migrator().HasTable(&entity.CredentialReplacementReceipt{}) {
			t.Fatal("replacement schema missing")
		}
		for _, index := range []string{"idx_credential_replacement_source", "idx_credential_replacement_result"} {
			if !db.Migrator().HasIndex(&entity.CredentialReplacementReceipt{}, index) {
				t.Fatal("replacement receipt index missing")
			}
		}
		if !db.Migrator().HasIndex(&entity.ProviderCredential{}, "idx_credentials_replaces") {
			t.Fatal("replacement lineage index missing")
		}
	}
	assertSchema()
	var upgraded entity.ProviderCredential
	if err := db.Select("id", "connection_id", "name", "ciphertext", "priority", "enabled", "verification_status", "verified_at", "created_at", "replaces_credential_id").First(&upgraded, "id = ?", legacy.ID).Error; err != nil || upgraded.ReplacesCredentialID != nil || upgraded.Ciphertext != legacy.Ciphertext || upgraded.Priority != legacy.Priority {
		t.Fatal("upgrade changed legacy credentials or invented lineage")
	}
	receipt := entity.CredentialReplacementReceipt{RequestID: "87b043bb-cb96-419c-b9a2-cab04814b65e", ActorID: "usr_historical", SourceCredentialID: legacy.ID, ConnectionID: connection.ID, ResultCredentialID: "crd_historical_result", RequestHash: "test-only-non-sensitive-request-hash"}
	if err := db.Create(&receipt).Error; err != nil {
		t.Fatal("historical creation receipt acquired a live FK")
	}
	duplicate := receipt
	duplicate.RequestID = "07b043bb-cb96-419c-b9a2-cab04814b65e"
	if err := db.Create(&duplicate).Error; err == nil {
		t.Fatal("receipt result uniqueness not enforced")
	}
	// Model MySQL's independently committed column/table DDL with missing indexes
	// before its ledger advances, then repeat with the receipt table missing.
	for _, missingIndex := range []string{"idx_credentials_replaces", "idx_credential_replacement_result"} {
		model := any(&entity.CredentialReplacementReceipt{})
		if missingIndex == "idx_credentials_replaces" {
			model = &entity.ProviderCredential{}
		}
		dropCredentialReplacementTestIndex(t, db, model, missingIndex)
		removeCredentialReplacementLedger()
		if err := database.Migrate(context.Background(), db); err != nil {
			t.Fatal(err)
		}
		assertSchema()
		var retained entity.CredentialReplacementReceipt
		if err := db.First(&retained, "request_id = ?", receipt.RequestID).Error; err != nil || retained.ResultCredentialID != receipt.ResultCredentialID || retained.RequestHash != receipt.RequestHash {
			t.Fatal("partial migration lost receipt")
		}
	}
	if err := db.Migrator().DropTable(&entity.CredentialReplacementReceipt{}); err != nil {
		t.Fatal(err)
	}
	removeCredentialReplacementLedger()
	if err := database.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	assertSchema()
	if err := database.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
}

func dropCredentialReplacementTestIndex(t *testing.T, db *gorm.DB, model any, name string) {
	t.Helper()
	if !db.Migrator().HasIndex(model, name) {
		t.Fatal("replacement fault-injection index missing before removal")
	}
	if db.Name() == "postgres" {
		// The pinned PostgreSQL GORM Migrator.DropIndex renders an invalid
		// CURRENT_SCHEMA().index qualifier. Use only these fixed statements for
		// test schema reconstruction; no input is interpolated into SQL, and
		// production migration creation/reconciliation remains entirely GORM.
		statements := map[string]string{
			"idx_credentials_replaces":          "DROP INDEX idx_credentials_replaces",
			"idx_credential_replacement_result": "DROP INDEX idx_credential_replacement_result",
		}
		statement := statements[name]
		if statement == "" {
			t.Fatal("unsupported replacement fault-injection index")
		}
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	} else if err := db.Migrator().DropIndex(model, name); err != nil {
		t.Fatal(err)
	}
	if db.Migrator().HasIndex(model, name) {
		t.Fatal("replacement fault injection did not remove index")
	}
}
