package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
	"os"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

func providerCleanupHistoricalLedger(t *testing.T, db *gorm.DB, maxVersion int) []personalKeyBehaviorMigrationEntry {
	t.Helper()
	var rows []personalKeyBehaviorMigrationEntry
	if e := db.Table("schema_migrations").Where("version <= ?", maxVersion).Order("version").Find(&rows).Error; e != nil {
		t.Fatal(e)
	}
	return rows
}
func testProviderCredentialCleanupMigration(t *testing.T, db *gorm.DB) {
	ctx := context.Background()
	before := personalKeyBehaviorLedger(t, db)
	if len(before) != 94 || before[93].Version != 94 || before[92].Version != 93 || before[91].Version != 92 || before[90].Version != 91 || before[89].Version != 90 || before[88].Version != 89 || before[87].Version != 88 || before[86].Version != 87 || before[85].Version != 86 || before[84].Version != 85 || before[83].Version != 84 || before[82].Version != 83 || before[80].Version != 81 || before[81].Version != 82 {
		t.Fatal("exact current87 ledger required")
	}
	for i, row := range before {
		if row.Version != i+1 {
			t.Fatal("ordered historical prefix")
		}
	}
	var ops []entity.CredentialStorageOperation
	if e := db.Order("request_id").Find(&ops).Error; e != nil {
		t.Fatal(e)
	}
	m := db.Migrator()
	if !m.HasTable(&entity.ProviderCredentialCreationUse{}) || !m.HasTable(&entity.ProviderCredentialCleanup{}) || !m.HasIndex(&entity.ProviderCredentialCleanup{}, "idx_provider_cleanup_command") || !m.HasConstraint(&entity.ProviderCredentialCleanup{}, "ck_provider_cleanup_state_v80") {
		t.Fatal("complete V80 schema")
	}
	if e := m.DropTable(&entity.ProviderCredentialCreationUse{}, &entity.ProviderCredentialCleanup{}); e != nil {
		t.Fatal(e)
	}
	if e := db.Exec("DELETE FROM schema_migrations WHERE version = ?", 80).Error; e != nil {
		t.Fatal(e)
	}
	historical := personalKeyBehaviorLedger(t, db)
	// Only V80 is reconstructed; preserve the exact later V81 ledger entry.
	wantHistorical := append(append([]personalKeyBehaviorMigrationEntry{}, before[:79]...), before[80:]...)
	if !reflect.DeepEqual(historical, wantHistorical) {
		t.Fatal("exact original79 ledger changed")
	}
	// Interrupted DDL may have created the table but not its indexes/constraint.
	if e := m.CreateTable(&entity.ProviderCredentialCleanup{}); e != nil {
		t.Fatal(e)
	}
	if db.Name() == "postgres" {
		// Pinned postgres GORM v1.6.2 DropIndex emits the invalid
		// CURRENT_SCHEMA().index qualifier. This fixed test-only statement
		// reconstructs partial DDL, following dropCredentialReplacementTestIndex.
		// Keep the created table/rows and all other indexes while removing only
		// this exact fault target; production migration repair remains GORM.
		if e := db.Exec(`DROP INDEX "idx_provider_cleanup_command"`).Error; e != nil {
			t.Fatal(e)
		}
	} else if e := m.DropIndex(&entity.ProviderCredentialCleanup{}, "idx_provider_cleanup_command"); e != nil {
		t.Fatal(e)
	}
	if m.HasIndex(&entity.ProviderCredentialCleanup{}, "idx_provider_cleanup_command") {
		t.Fatal("partial-DDL index removal failed")
	}
	if e := m.DropConstraint(&entity.ProviderCredentialCleanup{}, "ck_provider_cleanup_state_v80"); e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 3)
	for range 3 {
		wg.Go(func() { errs <- database.Migrate(ctx, db) })
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	if e := database.Migrate(ctx, db); e != nil {
		t.Fatal(e)
	}
	after := personalKeyBehaviorLedger(t, db)
	if len(after) != 94 || after[93].Version != 94 || after[92].Version != 93 || after[91].Version != 92 || after[90].Version != 91 || after[89].Version != 90 || after[88].Version != 89 || after[87].Version != 88 || after[86].Version != 87 || after[85].Version != 86 || after[84].Version != 85 || after[83].Version != 84 || after[82].Version != 83 || after[80].Version != 81 || after[81].Version != 82 || !reflect.DeepEqual(after[80:], before[80:]) || after[79].Version != 80 || !reflect.DeepEqual(after[:79], before[:79]) {
		t.Fatal("V80 repeat/current ledger or original79 prefix changed")
	}
	var retained []entity.CredentialStorageOperation
	if e := db.Order("request_id").Find(&retained).Error; e != nil || !reflect.DeepEqual(ops, retained) {
		t.Fatal("migration changed creation history", e)
	}
	if !m.HasTable(&entity.ProviderCredentialCreationUse{}) {
		t.Fatal("creation use table missing after replay")
	}
	var uses int64
	if e := db.Model(&entity.ProviderCredentialCreationUse{}).Count(&uses).Error; e != nil || uses != 0 {
		t.Fatal("legacy ownership was fabricated", e)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	base := entity.ProviderCredentialCleanup{ActorBirth: now, IntegrationBirth: now, StartedAt: now, Deadline: now.Add(time.Minute), CreationRequestID: "80000000-1111-4111-8111-111111111111", RequestID: "80000000-2222-4222-8222-222222222222", ActorID: "usr_fixture", IntegrationID: "vlt_fixture", RevisionID: "vlr_fixture", OperationProof: strings.Repeat("a", 64), ReviewedETag: strings.Repeat("b", 64) + "." + strings.Repeat("c", 64), Reason: "Migration constraints", State: "pending", OwnershipJSON: `{}`, CleanupJSON: `{}`}
	if e := db.Create(&base).Error; e != nil {
		t.Fatal(e)
	}
	for _, state := range []string{"PENDING", "Unknown", "ACKNOWLEDGED", "ready", "", "pending "} {
		if e := db.Model(&entity.ProviderCredentialCleanup{}).Where("creation_request_id = ?", base.CreationRequestID).Update("state", state).Error; e == nil {
			t.Fatal("case/unknown state accepted", state)
		}
	}
	duplicate := base
	duplicate.CreationRequestID = "80000000-3333-4333-8333-333333333333"
	if e := db.Create(&duplicate).Error; e == nil {
		t.Fatal("duplicate command UUID accepted")
	}
	if e := db.Delete(&base).Error; e != nil {
		t.Fatal(e)
	}
}
func providerCleanupRegistryParent(names []string) ([]string, bool) {
	if len(names) == 156 || len(names) == 158 || len(names) == 160 || len(names) == 162 || len(names) == 164 || len(names) == 166 || len(names) == 168 || len(names) == 170 || len(names) == 172 || len(names) == 174 || len(names) == 176 || len(names) == 177 || len(names) == 179 {
		parent, ok := personalRollingWarningRegistryParent(names)
		if !ok {
			return nil, false
		}
		names = parent
	}
	if len(names) != 154 || names[152] != "provider_orphan_cleanup_migration:testProviderCredentialCleanupMigration" || names[153] != "provider_orphan_cleanup:testProviderCredentialCleanupLifecycle" {
		return nil, false
	}
	sum := sha256.Sum256([]byte(strings.Join(names[:152], "\n")))
	if hex.EncodeToString(sum[:]) != "ee9240abd32fa3f839362437d9ec097363c87d660c1c155f474b61918973a312" {
		return nil, false
	}
	return names[:152], true
}
func TestProviderCleanupExact154RegistryAnd152Prefix(t *testing.T) {
	raw, e := os.ReadFile("auth_integration_test.go")
	if e != nil {
		t.Fatal(e)
	}
	var names []string
	for _, m := range regexp.MustCompile(`\{"([^"\n]+)", (test[A-Za-z0-9]+)\}`).FindAllStringSubmatch(string(raw), -1) {
		names = append(names, m[1]+":"+m[2])
	}
	if !oidcRegistry185Current(names) {
		t.Fatal("exact185 OIDC successor changed")
	}
	names = names[:181]
	if !credentialAttemptStatisticsRegistry181Current(names) {
		t.Fatal("exact V93/181 successor changed")
	}
	names = names[:179]
	if len(names) != 179 || !strings.Contains(string(raw), "versions != 94") {
		t.Fatal("current exact172 registry/V89 ledger changed")
	}
	if _, ok := providerCleanupRegistryParent(names); !ok {
		t.Fatal("exact152 prefix+cleanup2 required")
	}
	for _, mutate := range []func([]string) []string{func(x []string) []string { return x[:153] }, func(x []string) []string { x[152], x[153] = x[153], x[152]; return x }, func(x []string) []string { x[151] = x[150]; return x }, func(x []string) []string { return append(x, "extra:unreviewed") }} {
		if _, ok := providerCleanupRegistryParent(mutate(append([]string(nil), names...))); ok {
			t.Fatal("registry missing/duplicate/reordered/extra accepted")
		}
	}
}
