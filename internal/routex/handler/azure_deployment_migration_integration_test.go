package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
)

func testAzureDeploymentMigration(t *testing.T, db *gorm.DB) {
	ctx := context.Background()
	before := providerCleanupHistoricalLedger(t, db, 79)
	if len(before) != 79 {
		t.Fatal("exact current V79 ledger required")
	}
	for i, r := range before {
		if r.Version != i+1 {
			t.Fatal("historical prefix changed")
		}
	}
	provider := entity.Provider{ID: "prv_azure_upgrade", Name: "Upgrade provider"}
	connection := entity.ProviderConnection{ID: "con_azure_upgrade", ProviderID: provider.ID, Name: "Retained native", BaseURL: "https://example.invalid/v1", Protocol: entity.ProtocolOpenAIChat, EgressMode: "direct", Enabled: true, ETag: "rev_azure_upgrade"}
	credential := entity.ProviderCredential{ID: "crd_azure_upgrade", ConnectionID: connection.ID, Name: "Retained secret", Ciphertext: "opaque-original-envelope", StorageSource: "inline", VerificationStatus: "pending", Priority: 3}
	for _, row := range []any{&provider, &connection, &credential} {
		if e := db.Create(row).Error; e != nil {
			t.Fatal(e)
		}
	}
	var originalConnection entity.ProviderConnection
	var originalCredential entity.ProviderCredential
	if e := db.Take(&originalConnection, "id = ?", connection.ID).Error; e != nil {
		t.Fatal(e)
	}
	if e := db.Take(&originalCredential, "id = ?", credential.ID).Error; e != nil {
		t.Fatal(e)
	}
	m := db.Migrator()
	if e := m.DropTable(&entity.CredentialDeploymentAttestation{}); e != nil {
		t.Fatal(e)
	}
	if e := m.DropConstraint(&entity.ProviderConnection{}, "ck_connection_adapter_v79"); e != nil {
		t.Fatal(e)
	}
	if e := m.DropConstraint(&entity.ProviderCredential{}, "ck_credential_coverage_revision_v79"); e != nil {
		t.Fatal(e)
	}
	for _, pair := range []struct {
		model any
		field string
	}{{&entity.ProviderConnection{}, "Adapter"}, {&entity.ProviderConnection{}, "APIVersion"}, {&entity.ProviderCredential{}, "CoverageRevision"}, {&entity.ProviderCredential{}, "CoverageReviewETag"}, {&entity.ProviderCredential{}, "CoverageIntentSHA256"}} {
		if e := m.DropColumn(pair.model, pair.field); e != nil {
			t.Fatal(e)
		}
	}
	if e := db.Exec("DELETE FROM schema_migrations WHERE version = ?", 79).Error; e != nil {
		t.Fatal(e)
	}
	historical := providerCleanupHistoricalLedger(t, db, 79)
	if !reflect.DeepEqual(historical, before[:78]) {
		t.Fatal("V78 exact prefix changed")
	}
	// Simulate a partial MySQL DDL boundary: nullable version is already present.
	if e := m.AddColumn(&entity.ProviderConnection{}, "APIVersion"); e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	failures := make(chan error, 3)
	for range 3 {
		wg.Go(func() { failures <- database.Migrate(ctx, db) })
	}
	wg.Wait()
	close(failures)
	for e := range failures {
		if e != nil {
			t.Fatal(e)
		}
	}
	if e := database.Migrate(ctx, db); e != nil {
		t.Fatal(e)
	}
	after := providerCleanupHistoricalLedger(t, db, 79)
	if len(after) != 79 || !reflect.DeepEqual(after[:78], before[:78]) || after[78].Version != 79 {
		t.Fatal("current/repeated ordered ledger")
	}
	var currentConnection entity.ProviderConnection
	var currentCredential entity.ProviderCredential
	if e := db.Take(&currentConnection, "id = ?", connection.ID).Error; e != nil {
		t.Fatal(e)
	}
	if e := db.Take(&currentCredential, "id = ?", credential.ID).Error; e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(currentConnection, originalConnection) || !reflect.DeepEqual(currentCredential, originalCredential) {
		t.Fatal("V79 changed native transport or credential envelope/state")
	}
	if !m.HasTable(&entity.CredentialDeploymentAttestation{}) || !m.HasConstraint(&entity.ProviderConnection{}, "ck_connection_adapter_v79") || !m.HasConstraint(&entity.ProviderCredential{}, "ck_credential_coverage_revision_v79") {
		t.Fatal("additive schema not complete")
	}
	for _, updates := range []map[string]any{{"adapter": "AZURE_OPENAI_CLASSIC", "api_version": "2024-10-21"}, {"adapter": "native", "api_version": "2024-10-21"}, {"adapter": "azure_openai_classic", "api_version": nil}, {"adapter": "azure_openai_classic", "api_version": "2024-10-21", "protocol": "openai_responses"}} {
		err := db.Transaction(func(tx *gorm.DB) error {
			return tx.Model(&entity.ProviderConnection{}).Where("id = ?", connection.ID).Updates(updates).Error
		})
		if err == nil {
			t.Fatal("transport constraint accepted invalid cross-field state")
		}
	}
	if e := db.Model(&entity.ProviderCredential{}).Where("id = ?", credential.ID).Update("coverage_revision", int64(-1)).Error; e == nil {
		t.Fatal("negative coverage revision accepted")
	}
}

// The entire original 150-case registry remains byte-exact in name/order.
func azureDeploymentRegistryParent(names []string) ([]string, bool) {
	if len(names) == 156 || len(names) == 158 || len(names) == 160 || len(names) == 162 || len(names) == 164 || len(names) == 166 || len(names) == 168 || len(names) == 170 || len(names) == 172 {
		parent, ok := personalRollingWarningRegistryParent(names)
		if !ok {
			return nil, false
		}
		names = parent
	}
	if len(names) == 154 {
		var ok bool
		names, ok = providerCleanupRegistryParent(names)
		if !ok {
			return nil, false
		}
	}
	if len(names) != 152 || names[150] != "azure_deployment_migration:testAzureDeploymentMigration" || names[151] != "azure_deployment_coverage:testAzureDeploymentCoverage" {
		return nil, false
	}
	digest := sha256.Sum256([]byte(strings.Join(names[:150], "\n")))
	if hex.EncodeToString(digest[:]) != "3237f2f4b9aedd15123d4bf2cab1460dc8bc3843e403f7f1289ea8940a37dafe" {
		return nil, false
	}
	return names[:150], true
}
func TestAzureExact152RegistryAndRetained150Prefix(t *testing.T) {
	raw, e := os.ReadFile("auth_integration_test.go")
	if e != nil {
		t.Fatal(e)
	}
	var names []string
	for _, m := range regexp.MustCompile(`\{"([^"\n]+)", (test[A-Za-z0-9]+)\}`).FindAllStringSubmatch(string(raw), -1) {
		names = append(names, m[1]+":"+m[2])
	}
	if len(names) != 172 || !strings.Contains(string(raw), "versions != 89") {
		t.Fatal("current exact172 registry/V89 ledger changed")
	}
	if _, ok := azureDeploymentRegistryParent(names); !ok {
		t.Fatal("exact150 prefix plus reviewed Azure pair required")
	}
	for _, mutate := range []func([]string) []string{
		func(x []string) []string { return x[:151] },
		func(x []string) []string { x[150], x[151] = x[151], x[150]; return x },
		func(x []string) []string { x[149] = "unreviewed:replacement"; return x },
		func(x []string) []string { x[0] = x[1]; return x },
		func(x []string) []string { return append(x, "unreviewed:extra") },
	} {
		if _, ok := azureDeploymentRegistryParent(mutate(append([]string(nil), names...))); ok {
			t.Fatal("changed/missing/reordered/extra registry accepted")
		}
	}
}
