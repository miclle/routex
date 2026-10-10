package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"

	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
)

type connectionEnabledV76Fixture struct {
	Enabled bool `gorm:"not null;default:true"`
}

func (connectionEnabledV76Fixture) TableName() string { return "provider_connections" }

type connectionEnabledBadDefaultFixture struct {
	Enabled bool `gorm:"not null;default:false"`
}

func (connectionEnabledBadDefaultFixture) TableName() string { return "provider_connections" }

func testConnectionEnablementMigration(t *testing.T, db *gorm.DB) {
	ctx := context.Background()
	before := personalKeyBehaviorLedger(t, db)
	for i, row := range before {
		if row.Version != i+1 {
			t.Fatal("noncontiguous retained migration ledger")
		}
	}
	if (len(before) != 76 && len(before) != 77 && len(before) != 78 && len(before) != 79 && len(before) != 80 && len(before) != 81 && len(before) != 82 && len(before) != 83 && len(before) != 84 && len(before) != 85 && len(before) != 86 && len(before) != 87 && len(before) != 88 && len(before) != 89 && len(before) != 90 && len(before) != 91 && len(before) != 92 && len(before) != 93 && len(before) != 94) || before[75].Version != 76 || len(before) >= 77 && before[76].Version != 77 || len(before) >= 78 && before[77].Version != 78 || len(before) >= 79 && before[78].Version != 79 || len(before) >= 80 && before[79].Version != 80 || len(before) >= 81 && before[80].Version != 81 || len(before) >= 82 && before[81].Version != 82 || len(before) >= 83 && before[82].Version != 83 || len(before) >= 84 && before[83].Version != 84 || len(before) >= 85 && before[84].Version != 85 || len(before) >= 86 && before[85].Version != 86 || len(before) >= 87 && before[86].Version != 87 || len(before) >= 88 && before[87].Version != 88 || (len(before) == 89 || len(before) == 90 || len(before) == 91 || len(before) == 92 || len(before) == 93 || len(before) == 94) && before[88].Version != 89 || len(before) >= 90 && before[89].Version != 90 || len(before) >= 91 && before[90].Version != 91 || len(before) >= 92 && before[91].Version != 92 || len(before) >= 93 && before[92].Version != 93 || len(before) >= 94 && before[93].Version != 94 || !db.Migrator().HasColumn(&connectionEnabledV76Fixture{}, "Enabled") {
		t.Fatal("exact V76 ledger/column required")
	}
	if err := db.Create(&entity.Provider{ID: "prv_enabled_upgrade", Name: "Retained"}).Error; err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"con_enabled_old", "con_enabled_other"} {
		if err := db.Create(&entity.ProviderConnection{ID: id, ProviderID: "prv_enabled_upgrade", Name: id, BaseURL: "https://example.invalid/v1", Protocol: entity.ProtocolOpenAIChat, EgressMode: "direct", ETag: "rev_old", Enabled: true}).Error; err != nil {
			t.Fatal(err)
		}
	}
	read := func(id string) entity.ProviderConnection {
		t.Helper()
		var row entity.ProviderConnection
		if err := db.Session(&gorm.Session{QueryFields: true}).Take(&row, "id = ?", id).Error; err != nil {
			t.Fatal(err)
		}
		return row
	}
	old := read("con_enabled_old")
	other := read("con_enabled_other")
	remove := func() {
		t.Helper()
		q := db.Table("schema_migrations").Where("version = ?", 76).Delete(&struct{}{})
		if q.Error != nil || q.RowsAffected != 1 {
			t.Fatal("remove only owned V76", q.Error)
		}
	}
	migrate := func() {
		t.Helper()
		if err := database.Migrate(ctx, db); err != nil {
			t.Fatal(err)
		}
	}
	// Reconstruct only the newly introduced column. Retained V75 rows acquire
	// default true without rewriting any prior column, relationship or ledger.
	if err := db.Migrator().DropColumn(&connectionEnabledV76Fixture{}, "Enabled"); err != nil {
		t.Fatal(err)
	}
	remove()
	migrate()
	if !connectionEnablementSameRow(old, read(old.ID)) || !connectionEnablementSameRow(other, read(other.ID)) {
		t.Fatal("retained Connection changed during upgrade")
	}
	if err := db.Model(&entity.ProviderConnection{}).Where("id = ?", old.ID).Updates(map[string]any{"enabled": false}).Error; err != nil {
		t.Fatal(err)
	}
	stopped := read(old.ID)
	if stopped.Enabled {
		t.Fatal("explicit false omitted")
	}
	// Existing column with missing ledger simulates committed MySQL DDL followed
	// by interruption. Replay validates the schema and preserves explicit false.
	remove()
	migrate()
	migrate()
	if !connectionEnablementSameRow(stopped, read(old.ID)) {
		t.Fatal("partial-DDL replay reenabled or rewrote row")
	}
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
	if !personalKeyBehaviorLedgerPreserved(before, personalKeyBehaviorLedger(t, db), 76) {
		t.Fatal("migration changed unrelated ledger")
	}
	if err := db.Create(&entity.ProviderConnection{ID: "con_enabled_new", ProviderID: "prv_enabled_upgrade", Name: "New", BaseURL: "https://example.invalid/v1", Protocol: entity.ProtocolOpenAIChat, EgressMode: "direct", ETag: "rev_new"}).Error; err != nil {
		t.Fatal(err)
	}
	if !read("con_enabled_new").Enabled {
		t.Fatal("new Connection lacks enabled default")
	}
	// A mismatched surviving partial column is rejected; no completed V76 ledger
	// is fabricated. Restore only the private V76 shape before leaving the case.
	if err := db.Migrator().AlterColumn(&connectionEnabledBadDefaultFixture{}, "Enabled"); err != nil {
		t.Fatal(err)
	}
	remove()
	defer func() {
		if err := db.Migrator().AlterColumn(&connectionEnabledV76Fixture{}, "Enabled"); err != nil {
			t.Error(err)
			return
		}
		if err := database.Migrate(ctx, db); err != nil {
			t.Error(err)
		}
	}()
	if err := database.Migrate(ctx, db); err == nil {
		t.Fatal("wrong existing default accepted")
	}
	var n int64
	if err := db.Table("schema_migrations").Where("version = ?", 76).Count(&n).Error; err != nil || n != 0 {
		t.Fatal("failed schema validation recorded migration", err, n)
	}
	if !connectionEnablementSameRow(stopped, read(old.ID)) {
		t.Fatal("failed schema validation changed stored false")
	}
}

func connectionEnablementSameRow(a, b entity.ProviderConnection) bool {
	return a.ID == b.ID && a.ProviderID == b.ProviderID && a.Name == b.Name && a.Protocol == b.Protocol && a.BaseURL == b.BaseURL && a.EgressMode == b.EgressMode && ((a.EgressID == nil && b.EgressID == nil) || (a.EgressID != nil && b.EgressID != nil && *a.EgressID == *b.EgressID)) && a.ETag == b.ETag && a.Enabled == b.Enabled && a.CreatedAt.Equal(b.CreatedAt)
}

func connectionEnablementRegistryPrefix(names []string) bool {
	if len(names) == 156 || len(names) == 158 || len(names) == 160 || len(names) == 162 || len(names) == 164 || len(names) == 166 || len(names) == 168 || len(names) == 170 || len(names) == 172 || len(names) == 174 || len(names) == 176 || len(names) == 177 || len(names) == 179 {
		parent, ok := personalRollingWarningRegistryParent(names)
		if !ok {
			return false
		}
		names = parent
	}
	if len(names) == 154 {
		parent, ok := providerCleanupRegistryParent(names)
		if !ok {
			return false
		}
		names = parent
	}
	if len(names) == 152 {
		parent, ok := azureDeploymentRegistryParent(names)
		if !ok {
			return false
		}
		names = parent
	}
	if len(names) == 150 {
		parent, ok := vaultAppRoleRegistryParent(names)
		if !ok {
			return false
		}
		names = parent
	}
	if len(names) == 148 {
		if names[146] != "provider_credential_storage_migration:testProviderCredentialStorageMigration" || names[147] != "provider_credential_storage:testProviderCredentialStorageLifecycle" {
			return false
		}
		names = names[:146]
	}
	if len(names) != 146 || names[144] != "connection_enablement_migration:testConnectionEnablementMigration" || names[145] != "connection_status:testConnectionStatusLifecycle" {
		return false
	}
	digest := sha256.Sum256([]byte(strings.Join(names[:144], "\n")))
	return hex.EncodeToString(digest[:]) == "57d7bac2a0d572d512650f52e01b3905fa4a9a131ba0976420881b5c7408c319"
}
func TestConnectionEnablementExactRegistry146(t *testing.T) {
	raw, err := os.ReadFile("auth_integration_test.go")
	if err != nil {
		t.Fatal(err)
	}
	matches := regexp.MustCompile(`\{"([^"\n]+)", (test[A-Za-z0-9]+)\}`).FindAllStringSubmatch(string(raw), -1)
	var names []string
	for _, m := range matches {
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
	if !connectionEnablementRegistryPrefix(names) || !teamMemberMonthlyRegistryTail(names) || !projectKeyMonthlyBehaviorRegistryMatches(names) || !personalKeyBehaviorRegistryMatches(names) {
		t.Fatal("exact144 prefix/new146 tail drift")
	}
	for _, mutate := range []func([]string) []string{
		func(x []string) []string { return x[:145] },
		func(x []string) []string { x[144], x[145] = x[145], x[144]; return x },
		func(x []string) []string { x[143] = "unreviewed:unreviewed"; return x },
		func(x []string) []string { return append(x, "extra:unreviewed") },
	} {
		if connectionEnablementRegistryPrefix(mutate(append([]string(nil), names...))) {
			t.Fatal("missing/reordered/changed/extra registry accepted")
		}
	}
}
