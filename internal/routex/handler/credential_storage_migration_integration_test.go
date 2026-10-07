package handler

import (
	"context"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
)

type credentialStorageColumnFixture struct {
	StorageSource string `gorm:"size:16;not null;default:inline"`
}

func (credentialStorageColumnFixture) TableName() string { return "provider_credentials" }

type credentialStorageBadColumnFixture struct {
	StorageSource string `gorm:"size:16;not null;default:vault"`
}

func (credentialStorageBadColumnFixture) TableName() string { return "provider_credentials" }

func testProviderCredentialStorageMigration(t *testing.T, db *gorm.DB) {
	ctx := context.Background()
	before := personalKeyBehaviorLedger(t, db)
	if len(before) != 80 || before[74].Version != 75 || before[75].Version != 76 || before[76].Version != 77 || before[77].Version != 78 || before[78].Version != 79 || before[79].Version != 80 {
		t.Fatal("exact ordered V77 after V75/V76 required")
	}
	for i, row := range before {
		if row.Version != i+1 {
			t.Fatal("noncontiguous ledger")
		}
	}
	m := db.Migrator()
	checks := []struct {
		model any
		name  string
	}{
		{&entity.ProviderCredential{}, "ck_provider_credentials_storage_source"},
		{&entity.CredentialStoragePolicy{}, "ck_credential_storage_policy_mode"},
		{&entity.CredentialStoragePolicy{}, "ck_credential_store_singleton"},
		{&entity.CredentialStorageOperation{}, "ck_credential_storage_operation_source"},
		{&entity.CredentialStorageOperation{}, "ck_credential_storage_operation_kind"},
		{&entity.CredentialStorageOperation{}, "ck_credential_storage_operation_state"},
	}
	assertSchema := func() {
		t.Helper()
		for _, c := range checks {
			if !m.HasConstraint(c.model, c.name) {
				t.Fatal("missing exact constraint", c.name)
			}
		}
		for _, c := range []struct {
			model any
			name  string
		}{{&entity.CredentialStorageOperation{}, "idx_credential_storage_result"}, {&entity.CredentialStorageOperation{}, "idx_credential_storage_reference"}, {&entity.CredentialVaultReference{}, "idx_credential_vault_reference"}} {
			if !m.HasIndex(c.model, c.name) {
				t.Fatal("missing unique index", c.name)
			}
		}
		if !personalKeyBehaviorLedgerPreserved(before, personalKeyBehaviorLedger(t, db), 77) {
			t.Fatal("unrelated historical ledger changed")
		}
	}
	assertSchema()
	p := entity.Provider{ID: "prv_storage_upgrade", Name: "Retained provider"}
	if err := db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	c := entity.ProviderConnection{ID: "con_storage_upgrade", ProviderID: p.ID, Name: "Retained Connection", Protocol: entity.ProtocolOpenAIChat, BaseURL: "https://example.invalid/v1", EgressMode: "direct", ETag: "retained", Enabled: true}
	if err := db.Create(&c).Error; err != nil {
		t.Fatal(err)
	}
	old := entity.ProviderCredential{ID: "crd_storage_upgrade", ConnectionID: c.ID, Name: "Retained encrypted Credential", Ciphertext: "retained-existing-envelope", VerificationStatus: "pending", StorageSource: "inline"}
	if err := db.Create(&old).Error; err != nil {
		t.Fatal(err)
	}
	read := func() entity.ProviderCredential {
		t.Helper()
		var x entity.ProviderCredential
		if err := db.Session(&gorm.Session{QueryFields: true}).Take(&x, "id = ?", old.ID).Error; err != nil {
			t.Fatal(err)
		}
		return x
	}
	old = read()
	preserved := func() {
		t.Helper()
		x := read()
		if x.ID != old.ID || x.ConnectionID != old.ConnectionID || x.Ciphertext != old.Ciphertext || x.Name != old.Name || x.Priority != old.Priority || x.Enabled != old.Enabled || x.VerificationStatus != old.VerificationStatus || !x.CreatedAt.Equal(old.CreatedAt) || x.StorageSource != "inline" {
			t.Fatal("backfill rewrote retained encrypted Credential")
		}
	}
	remove := func() {
		t.Helper()
		q := db.Table("schema_migrations").Where("version = ?", 77).Delete(&struct{}{})
		if q.Error != nil || q.RowsAffected != 1 {
			t.Fatal("remove only V77", q.Error)
		}
	}
	replay := func() {
		t.Helper()
		if err := database.Migrate(ctx, db); err != nil {
			t.Fatal(err)
		}
		assertSchema()
		preserved()
	}
	if err := m.DropConstraint(&entity.ProviderCredential{}, "ck_provider_credentials_storage_source"); err != nil {
		t.Fatal(err)
	}
	if err := m.DropColumn(&credentialStorageColumnFixture{}, "StorageSource"); err != nil {
		t.Fatal(err)
	}
	for _, table := range []any{&entity.CredentialVaultReference{}, &entity.CredentialStorageOperation{}, &entity.CredentialStoragePolicy{}} {
		if err := m.DropTable(table); err != nil {
			t.Fatal(err)
		}
	}
	remove()
	replay()
	replay()
	// Committed MySQL DDL with a missing ledger/check must be resumed, preserving
	// inline envelopes and all other version timestamps.
	if err := m.DropConstraint(&entity.ProviderCredential{}, "ck_provider_credentials_storage_source"); err != nil {
		t.Fatal(err)
	}
	remove()
	replay()
	remove()
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		wg.Go(func() { errs <- database.Migrate(ctx, db) })
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	assertSchema()
	preserved()
	for _, value := range []any{"INLINE", "Inline", "inline ", "VAULT", "vault ", "unknown", "", nil} {
		if err := db.Model(&entity.ProviderCredential{}).Where("id = ?", old.ID).Update("storage_source", value).Error; err == nil {
			t.Fatal("invalid/case-folded source accepted", value)
		}
		preserved()
	}
	if err := db.Model(&entity.ProviderCredential{}).Where("id = ?", old.ID).Update("storage_source", "vault").Error; err != nil {
		t.Fatal("canonical vault source rejected", err)
	}
	if err := db.Model(&entity.ProviderCredential{}).Where("id = ?", old.ID).Update("storage_source", "inline").Error; err != nil {
		t.Fatal(err)
	}
	for _, value := range []any{"INLINE", "inline ", "VAULT", "vault ", "unknown", "", nil} {
		if err := db.Model(&entity.CredentialStoragePolicy{}).Where("id = ?", 1).Update("mode", value).Error; err == nil {
			t.Fatal("invalid policy mode accepted", value)
		}
	}

	// Both canonical policy variants persist exactly; unrelated metadata has no
	// authority to turn an invalid source spelling into a canonical value.
	birth := old.CreatedAt
	integrationID, revisionID := "vlt_storage_fixture", "vlr_storage_fixture"
	if err := db.Model(&entity.CredentialStoragePolicy{}).Where("id = ?", 1).Updates(map[string]any{"mode": "vault", "integration_id": integrationID, "integration_birth": birth, "revision_id": revisionID}).Error; err != nil {
		t.Fatal("canonical Vault policy rejected", err)
	}
	if err := db.Model(&entity.CredentialStoragePolicy{}).Where("id = ?", 1).Updates(map[string]any{"mode": "inline", "integration_id": nil, "integration_birth": nil, "revision_id": nil}).Error; err != nil {
		t.Fatal(err)
	}
	operation := entity.CredentialStorageOperation{StorageSource: "inline", RequestID: "77000000-1111-4111-8111-111111111199", ActorID: "usr_fixture", ActorBirth: birth, Kind: "credential", TargetID: c.ID, TargetBirth: birth, PolicyGeneration: "fixture", IntentJSON: "{}", CredentialID: "crd_fixture_plan", CredentialBirth: birth, ProviderID: p.ID, ProviderBirth: birth, ConnectionID: c.ID, ConnectionBirth: birth, ReferenceID: strings.Repeat("a", 32), ExpectedMarkerSHA256: strings.Repeat("b", 64), DescriptorSHA256: strings.Repeat("c", 64), State: "committed", WriteJSON: "{}", ReadJSON: "{}", ClaimedUntil: birth, CreatedAt: birth}
	if err := db.Create(&operation).Error; err != nil {
		t.Fatal(err)
	}
	for field, variants := range map[string][]any{"storage_source": {"INLINE", "inline ", "VAULT", "vault ", "", nil}, "kind": {"CREDENTIAL", "credential ", "unknown", "", nil}, "state": {"COMMITTED", "committed ", "unknown ", "", "invalid", nil}} {
		for _, value := range variants {
			if err := db.Model(&entity.CredentialStorageOperation{}).Where("request_id = ?", operation.RequestID).Update(field, value).Error; err == nil {
				t.Fatal("invalid operation enum accepted", field, value)
			}
		}
	}
	// Validate surviving column shape instead of silently accepting a partially
	// applied column with the wrong default. Restore the exact V77 test shape.
	if err := m.AlterColumn(&credentialStorageBadColumnFixture{}, "StorageSource"); err != nil {
		t.Fatal(err)
	}
	remove()
	defer func() {
		if err := m.AlterColumn(&credentialStorageColumnFixture{}, "StorageSource"); err != nil {
			t.Error(err)
			return
		}
		if err := database.Migrate(ctx, db); err != nil {
			t.Error(err)
		}
	}()
	if err := database.Migrate(ctx, db); err == nil {
		t.Fatal("unexpected existing source default accepted")
	}
	var count int64
	if err := db.Table("schema_migrations").Where("version = ?", 77).Count(&count).Error; err != nil || count != 0 {
		t.Fatal("failed validation recorded V77", err)
	}
	preserved()
}

func TestCredentialStorageExactRegistry148(t *testing.T) {
	raw, err := os.ReadFile("auth_integration_test.go")
	if err != nil {
		t.Fatal(err)
	}
	matches := regexp.MustCompile(`\{"([^"\n]+)", (test[A-Za-z0-9]+)\}`).FindAllStringSubmatch(string(raw), -1)
	names := make([]string, 0, len(matches))
	for _, m := range matches {
		names = append(names, m[1]+":"+m[2])
	}
	if len(names) == 154 {
		parent, ok := providerCleanupRegistryParent(names)
		if !ok {
			t.Fatal("unreviewed cleanup tail")
		}
		names = parent
	}
	if len(names) == 152 {
		parent, ok := azureDeploymentRegistryParent(names)
		if !ok {
			t.Fatal("unreviewed Azure tail")
		}
		names = parent
	}
	if len(names) == 150 {
		parent, ok := vaultAppRoleRegistryParent(names)
		if !ok {
			t.Fatal("unreviewed AppRole tail")
		}
		names = parent
	}
	if len(names) != 148 || !connectionEnablementRegistryPrefix(names) || !personalKeyBehaviorRegistryMatches(names) || !projectKeyMonthlyBehaviorRegistryMatches(names) || !teamMemberMonthlyRegistryTail(names) {
		t.Fatal("exact146 prefix plus two source cases required")
	}
	for _, mutate := range []func([]string) []string{
		func(x []string) []string { return x[:147] },
		func(x []string) []string { x[146], x[147] = x[147], x[146]; return x },
		func(x []string) []string { x[145] = "connection_status:unreviewed"; return x },
		func(x []string) []string { x[146] = "provider_credential_storage_migration:unreviewed"; return x },
		func(x []string) []string { return append(x, "unreviewed:extra") },
	} {
		if connectionEnablementRegistryPrefix(mutate(append([]string(nil), names...))) {
			t.Fatal("changed/missing/reordered/extra source registry accepted")
		}
	}
	if !strings.Contains(string(raw), "versions != 80") {
		t.Fatal("current V77 harness not bound")
	}
}
