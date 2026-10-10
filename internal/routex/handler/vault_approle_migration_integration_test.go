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
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
)

type vaultAuthMethodColumnFixture struct {
	Method string `gorm:"size:16;not null;default:token"`
}

func (vaultAuthMethodColumnFixture) TableName() string { return "vault_writer_auth" }

type vaultAuthBadMethodColumnFixture struct {
	Method string `gorm:"size:10;not null;default:token"`
}

func (vaultAuthBadMethodColumnFixture) TableName() string { return "vault_writer_auth" }

// The common real-driver harness then proves empty creation before this upgrade.
func testVaultSavedAppRoleMigration(t *testing.T, db *gorm.DB) {
	ctx := context.Background()
	before := personalKeyBehaviorLedger(t, db)
	if len(before) < 78 || before[77].Version != 78 {
		t.Fatal("V78 after exact predecessors required")
	}
	for i, row := range before {
		if row.Version != i+1 {
			t.Fatal("noncontiguous ledger")
		}
	}
	birth := time.Now().UTC().Truncate(time.Microsecond)
	integration := entity.VaultIntegration{ID: "vlt_approle_upgrade", Name: "Retained Vault", RevisionID: "vlr_approle_upgrade", CreatedAt: birth, UpdatedAt: birth}
	revision := entity.VaultRevision{ID: integration.RevisionID, IntegrationID: integration.ID, IntegrationBirth: birth, Name: integration.Name, Endpoint: "https://vault.example", Mount: "kv", Prefix: "system", DataField: "value", CreatedAt: birth}
	writer := entity.VaultWriterAuth{ID: revision.ID, SecretGeneration: "vag_retained_writer", AuthCiphertext: "retained-original-writer-envelope", Method: "token"}
	reader := entity.VaultReaderAuth{ID: revision.ID, SecretGeneration: "vag_retained_reader", AuthCiphertext: "retained-original-reader-envelope", Method: "token"}
	for _, row := range []any{&integration, &revision, &writer, &reader} {
		if e := db.Create(row).Error; e != nil {
			t.Fatal(e)
		}
	}
	read := func() (entity.VaultWriterAuth, entity.VaultReaderAuth) {
		t.Helper()
		var w entity.VaultWriterAuth
		var r entity.VaultReaderAuth
		if e := db.Session(&gorm.Session{QueryFields: true}).Take(&w, "id = ?", revision.ID).Error; e != nil {
			t.Fatal(e)
		}
		if e := db.Session(&gorm.Session{QueryFields: true}).Take(&r, "id = ?", revision.ID).Error; e != nil {
			t.Fatal(e)
		}
		return w, r
	}
	preserved := func() {
		t.Helper()
		w, r := read()
		if !reflect.DeepEqual(w, writer) || !reflect.DeepEqual(r, reader) {
			t.Fatal("method backfill rewrote retained auth envelope/generation")
		}
	}
	checks := []struct {
		model any
		name  string
	}{{&entity.VaultWriterAuth{}, "ck_vault_writer_method"}, {&entity.VaultReaderAuth{}, "ck_vault_reader_method"}}
	assert := func() {
		t.Helper()
		for _, c := range checks {
			if !db.Migrator().HasConstraint(c.model, c.name) {
				t.Fatal("missing exact method constraint", c.name)
			}
		}
		if !personalKeyBehaviorLedgerPreserved(before, personalKeyBehaviorLedger(t, db), 78) {
			t.Fatal("historical/later ledger changed")
		}
		preserved()
	}
	remove := func() {
		t.Helper()
		q := db.Table("schema_migrations").Where("version = ?", 78).Delete(&struct{}{})
		if q.Error != nil || q.RowsAffected != 1 {
			t.Fatal("remove only V78", q.Error)
		}
	}
	replay := func() {
		t.Helper()
		if e := database.Migrate(ctx, db); e != nil {
			t.Fatal(e)
		}
		assert()
	}
	assert()
	// Legacy upgrade: no auth material is decrypted/replaced by the migration.
	for _, c := range checks {
		if e := db.Migrator().DropConstraint(c.model, c.name); e != nil {
			t.Fatal(e)
		}
		if e := db.Migrator().DropColumn(c.model, "Method"); e != nil {
			t.Fatal(e)
		}
	}
	remove()
	replay()
	replay()
	// Partially committed MySQL DDL resumes only after exact column validation.
	if e := db.Migrator().DropConstraint(&entity.VaultWriterAuth{}, "ck_vault_writer_method"); e != nil {
		t.Fatal(e)
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
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	assert()
	for _, c := range checks {
		for _, value := range []any{"TOKEN", "Token", "token ", "APPROLE", "AppRole", "approle ", "unknown", "", nil, "approle" + strings.Repeat(" ", 32)} {
			if e := db.Model(c.model).Where("id = ?", revision.ID).Update("method", value).Error; e == nil {
				t.Fatal("invalid/case/padded auth method admitted", value)
			}
			preserved()
		}
		if e := db.Model(c.model).Where("id = ?", revision.ID).Update("method", "approle").Error; e != nil {
			t.Fatal("canonical AppRole rejected", e)
		}
		if e := db.Model(c.model).Where("id = ?", revision.ID).Update("method", "token").Error; e != nil {
			t.Fatal(e)
		}
		preserved()
	}
	if e := db.Migrator().DropConstraint(&entity.VaultWriterAuth{}, "ck_vault_writer_method"); e != nil {
		t.Fatal(e)
	}
	if e := db.Migrator().DropColumn(&vaultAuthMethodColumnFixture{}, "Method"); e != nil {
		t.Fatal(e)
	}
	if e := db.Migrator().AddColumn(&vaultAuthBadMethodColumnFixture{}, "Method"); e != nil {
		t.Fatal(e)
	}
	remove()
	if e := database.Migrate(ctx, db); e == nil {
		t.Fatal("incompatible partial auth method schema accepted")
	}
	if e := db.Migrator().DropColumn(&vaultAuthBadMethodColumnFixture{}, "Method"); e != nil {
		t.Fatal(e)
	}
	if e := db.Migrator().AddColumn(&vaultAuthMethodColumnFixture{}, "Method"); e != nil {
		t.Fatal(e)
	}
	replay()
}

// Only the reviewed AppRole pair may extend the exact retained 148-case prefix.
func vaultAppRoleRegistryParent(names []string) ([]string, bool) {
	if len(names) == 156 || len(names) == 158 || len(names) == 160 || len(names) == 162 || len(names) == 164 || len(names) == 166 || len(names) == 168 || len(names) == 170 || len(names) == 172 || len(names) == 174 || len(names) == 176 || len(names) == 177 || len(names) == 179 {
		parent, ok := personalRollingWarningRegistryParent(names)
		if !ok {
			return nil, false
		}
		names = parent
	}
	if len(names) == 154 {
		parent, ok := providerCleanupRegistryParent(names)
		if !ok {
			return nil, false
		}
		names = parent
	}
	if len(names) == 152 {
		parent, ok := azureDeploymentRegistryParent(names)
		if !ok {
			return nil, false
		}
		names = parent
	}
	if len(names) != 150 || names[148] != "vault_saved_approle_migration:testVaultSavedAppRoleMigration" || names[149] != "vault_saved_approle:testVaultSavedAppRoleLifecycle" {
		return nil, false
	}
	return names[:148], true
}
func TestVaultSavedAppRoleExactRegistry150(t *testing.T) {
	raw, e := os.ReadFile("auth_integration_test.go")
	if e != nil {
		t.Fatal(e)
	}
	var names []string
	for _, p := range regexp.MustCompile(`\{"([^"\n]+)", (test[A-Za-z0-9]+)\}`).FindAllStringSubmatch(string(raw), -1) {
		names = append(names, p[1]+":"+p[2])
	}
	if !googleRegistry200Current(names) {
		t.Fatal("exact194 SAML successor changed")
	}
	names = names[:181]
	if !credentialAttemptStatisticsRegistry181Current(names) {
		t.Fatal("exact V93/181 successor changed")
	}
	names = names[:179]
	if len(names) != 179 || !strings.Contains(string(raw), "versions != 99") {
		t.Fatal("current exact172 registry/V89 ledger changed")
	}
	valid := func(values []string) bool {
		parent, ok := vaultAppRoleRegistryParent(values)
		if !ok {
			return false
		}
		digest := sha256.Sum256([]byte(strings.Join(parent, "\n")))
		return hex.EncodeToString(digest[:]) == "03e8462bea17d934b4ed90c12d82c9ab3a5d11116d8ca0fd4f45888d3afea268" && connectionEnablementRegistryPrefix(values) && personalKeyBehaviorRegistryMatches(values) && projectKeyMonthlyBehaviorRegistryMatches(values) && teamMemberMonthlyRegistryTail(values)
	}
	if !valid(names) {
		t.Fatal("exact148 prefix plus reviewed AppRole pair required")
	}
	for _, mutate := range []func([]string) []string{
		func(x []string) []string { return x[:149] },
		func(x []string) []string { x[148], x[149] = x[149], x[148]; return x },
		func(x []string) []string { x[148] = "vault_saved_approle_migration:unreviewed"; return x },
		func(x []string) []string { x[147] = "provider_credential_storage:unreviewed"; return x },
		func(x []string) []string { x[0] = x[1]; return x },
		func(x []string) []string { return append(x, "unreviewed:extra") },
	} {
		if valid(mutate(append([]string(nil), names...))) {
			t.Fatal("changed/missing/reordered/extra registry accepted")
		}
	}
	if !strings.Contains(string(raw), "versions != 99") {
		t.Fatal("current V78 harness not bound")
	}
}
