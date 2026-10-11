package handler

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
)

type certificateWriterHistoricalV103 struct {
	Method string `gorm:"size:16;not null;default:token;check:ck_vault_writer_method,(OCTET_LENGTH(method) = 5 AND ASCII(SUBSTRING(method,1,1)) = 116 AND ASCII(SUBSTRING(method,2,1)) = 111 AND ASCII(SUBSTRING(method,3,1)) = 107 AND ASCII(SUBSTRING(method,4,1)) = 101 AND ASCII(SUBSTRING(method,5,1)) = 110) OR (OCTET_LENGTH(method) = 7 AND ASCII(SUBSTRING(method,1,1)) = 97 AND ASCII(SUBSTRING(method,2,1)) = 112 AND ASCII(SUBSTRING(method,3,1)) = 112 AND ASCII(SUBSTRING(method,4,1)) = 114 AND ASCII(SUBSTRING(method,5,1)) = 111 AND ASCII(SUBSTRING(method,6,1)) = 108 AND ASCII(SUBSTRING(method,7,1)) = 101)" json:"-"`
}

func (certificateWriterHistoricalV103) TableName() string { return "vault_writer_auth" }

type certificateReaderHistoricalV103 struct {
	Method string `gorm:"size:16;not null;default:token;check:ck_vault_reader_method,(OCTET_LENGTH(method) = 5 AND ASCII(SUBSTRING(method,1,1)) = 116 AND ASCII(SUBSTRING(method,2,1)) = 111 AND ASCII(SUBSTRING(method,3,1)) = 107 AND ASCII(SUBSTRING(method,4,1)) = 101 AND ASCII(SUBSTRING(method,5,1)) = 110) OR (OCTET_LENGTH(method) = 7 AND ASCII(SUBSTRING(method,1,1)) = 97 AND ASCII(SUBSTRING(method,2,1)) = 112 AND ASCII(SUBSTRING(method,3,1)) = 112 AND ASCII(SUBSTRING(method,4,1)) = 114 AND ASCII(SUBSTRING(method,5,1)) = 111 AND ASCII(SUBSTRING(method,6,1)) = 108 AND ASCII(SUBSTRING(method,7,1)) = 101)" json:"-"`
}

func (certificateReaderHistoricalV103) TableName() string { return "vault_reader_auth" }

// Integration-only peel. It has no application downgrade or production API.
// Call before the existing V103→V102→V101 pristine historical peels; never
// continue a current application lifecycle on the bounded historical schema.
func legacySavedCertificateBeforeV103(t *testing.T, db *gorm.DB) {
	t.Helper()
	before := personalKeyBehaviorLedger(t, db)
	if len(before) != 103 && len(before) != 104 {
		t.Fatal("unsupported certificate historical boundary")
	}
	for i, row := range before {
		if row.Version != i+1 {
			t.Fatal("noncontiguous certificate historical boundary")
		}
	}
	for _, c := range []struct {
		model any
		name  string
	}{{&certificateWriterHistoricalV103{}, "ck_vault_writer_method"}, {&certificateReaderHistoricalV103{}, "ck_vault_reader_method"}} {
		st := &gorm.Statement{DB: db}
		if st.Parse(c.model) != nil {
			t.Fatal("historical literal parse")
		}
		check, ok := st.Schema.ParseCheckConstraints()[c.name]
		if !ok {
			t.Fatal("historical literal CHECK missing")
		}
		var invalid int64
		if db.Table(st.Schema.Table).Where("method IS NULL OR NOT ("+check.Constraint+")").Count(&invalid).Error != nil || invalid != 0 {
			t.Fatal("certificate or unknown authority prevents historical peel")
		}
	}
	for _, model := range []any{&entity.VaultProbe{}, &entity.VaultProbeCommand{}} {
		var count int64
		if db.Model(model).Count(&count).Error != nil || count != 0 {
			t.Fatal("retained command/probe prevents historical peel")
		}
	}
	var writers []entity.VaultWriterAuth
	var readers []entity.VaultReaderAuth
	if db.Session(&gorm.Session{QueryFields: true}).Order("id").Find(&writers).Error != nil || db.Session(&gorm.Session{QueryFields: true}).Order("id").Find(&readers).Error != nil {
		t.Fatal("historical auth capture")
	}
	for _, c := range []struct {
		model any
		name  string
	}{{&certificateWriterHistoricalV103{}, "ck_vault_writer_method"}, {&certificateReaderHistoricalV103{}, "ck_vault_reader_method"}} {
		if db.Migrator().HasConstraint(c.model, c.name) {
			if db.Migrator().DropConstraint(c.model, c.name) != nil {
				t.Fatal("historical method CHECK removal")
			}
		}
		if db.Migrator().CreateConstraint(c.model, c.name) != nil {
			t.Fatal("historical method CHECK restoration")
		}
	}
	if len(before) == 104 {
		q := db.Table("schema_migrations").Where("version = ?", 104).Delete(&struct{}{})
		if q.Error != nil || q.RowsAffected != 1 {
			t.Fatal("remove only certificate receipt")
		}
	}
	var afterW []entity.VaultWriterAuth
	var afterR []entity.VaultReaderAuth
	if db.Session(&gorm.Session{QueryFields: true}).Order("id").Find(&afterW).Error != nil || db.Session(&gorm.Session{QueryFields: true}).Order("id").Find(&afterR).Error != nil || !reflect.DeepEqual(writers, afterW) || !reflect.DeepEqual(readers, afterR) || !reflect.DeepEqual(before[:103], personalKeyBehaviorLedger(t, db)) {
		t.Fatal("historical peel changed retained facts")
	}
}

// The full harness proves current startup before this bounded V104 upgrade
// scenario and receives the restored current head on completion.
func testVaultSavedCertificateMigration(t *testing.T, db *gorm.DB) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	currentEntry := personalKeyBehaviorLedger(t, db)
	if len(currentEntry) != 106 || currentEntry[105].Version != 106 || currentEntry[104].Version != 105 || currentEntry[103].Version != 104 {
		t.Fatal("exact V105 startup required")
	}
	legacyRuntimePublicationBeforeV104(t, db)
	before := personalKeyBehaviorLedger(t, db)
	if !reflect.DeepEqual(currentEntry[:104], before) {
		t.Fatal("V105 peel changed retained V104 prefix")
	}
	if len(before) != 104 {
		t.Fatal("exact V104 startup required")
	}
	for i, row := range before {
		if row.Version != i+1 {
			t.Fatal("noncontiguous current ledger")
		}
	}
	prefix := append([]personalKeyBehaviorMigrationEntry(nil), before[:103]...)
	for _, model := range []any{&entity.VaultWriterAuth{}, &entity.VaultReaderAuth{}} {
		var count int64
		if db.Model(model).Count(&count).Error != nil || count != 0 {
			t.Fatal("auth corpus must be pristine before historical upgrade")
		}
	}
	// V72 retains revision/integration foreign keys; auth rows cannot be orphans.
	birth := time.Now().UTC().Truncate(time.Microsecond)
	integration := entity.VaultIntegration{ID: "vlt_certificate_upgrade", Name: "Retained Vault", RevisionID: "vlr_certificate_upgrade", CreatedAt: birth, UpdatedAt: birth}
	revision := entity.VaultRevision{ID: integration.RevisionID, IntegrationID: integration.ID, IntegrationBirth: birth, Name: integration.Name, Endpoint: "https://vault.example", Mount: "kv", Prefix: "system", DataField: "value", CreatedAt: birth}
	if db.Create(&integration).Error != nil || db.Create(&revision).Error != nil {
		t.Fatal("retained upgrade parent rows")
	}
	writer := entity.VaultWriterAuth{ID: "vlr_certificate_upgrade", Method: "token", SecretGeneration: "vag_retained_writer", AuthCiphertext: "retained-encrypted-writer-envelope"}
	reader := entity.VaultReaderAuth{ID: writer.ID, Method: "approle", SecretGeneration: "vag_retained_reader", AuthCiphertext: "retained-encrypted-reader-envelope"}
	if db.Create(&writer).Error != nil || db.Create(&reader).Error != nil {
		t.Fatal("retained upgrade rows")
	}
	read := func() (entity.VaultWriterAuth, entity.VaultReaderAuth) {
		t.Helper()
		var w entity.VaultWriterAuth
		var r entity.VaultReaderAuth
		if db.Session(&gorm.Session{QueryFields: true}).Take(&w, "id = ?", writer.ID).Error != nil || db.Session(&gorm.Session{QueryFields: true}).Take(&r, "id = ?", reader.ID).Error != nil {
			t.Fatal("retained auth read")
		}
		return w, r
	}
	preserved := func() {
		t.Helper()
		w, r := read()
		if !reflect.DeepEqual(w, writer) || !reflect.DeepEqual(r, reader) {
			t.Fatal("migration rewrote retained method/envelope/generation/identity")
		}
	}
	checks := []struct {
		current, historical    any
		name                   string
		generation, ciphertext string
	}{
		{&entity.VaultWriterAuth{}, &certificateWriterHistoricalV103{}, "ck_vault_writer_method", writer.SecretGeneration, writer.AuthCiphertext},
		{&entity.VaultReaderAuth{}, &certificateReaderHistoricalV103{}, "ck_vault_reader_method", reader.SecretGeneration, reader.AuthCiphertext},
	}
	remove := func() {
		t.Helper()
		q := db.Table("schema_migrations").Where("version = ?", 104).Delete(&struct{}{})
		if q.Error != nil || q.RowsAffected != 1 || !reflect.DeepEqual(prefix, personalKeyBehaviorLedger(t, db)) {
			t.Fatal("remove only V104 receipt")
		}
	}
	assert := func() {
		t.Helper()
		after := personalKeyBehaviorLedger(t, db)
		if len(after) != 104 || after[103].Version != 104 || !reflect.DeepEqual(prefix, after[:103]) {
			t.Fatal("V103 receipt prefix changed")
		}
		for _, c := range checks {
			if !db.Migrator().HasConstraint(c.current, c.name) {
				t.Fatal("current certificate CHECK absent")
			}
		}
		preserved()
	}
	migrate := func(stage string) {
		t.Helper()
		if database.MigrateThrough(ctx, db, 104) != nil {
			t.Fatalf("certificate migration failed: stage=%s", stage)
		}
		assert()
	}
	// Independent literal pre-certificate constraints; no current model rewrites history.
	legacySavedCertificateBeforeV103(t, db)
	if !reflect.DeepEqual(prefix, personalKeyBehaviorLedger(t, db)) {
		t.Fatal("historical peel changed prefix")
	}
	for _, c := range checks {
		if db.Model(c.current).Where("id = ?", writer.ID).Update("method", "certificate").Error == nil {
			t.Fatal("historical constraint already admitted certificate")
		}
		preserved()
	}
	migrate("upgrade_from103")
	receipt := personalKeyBehaviorLedger(t, db)
	migrate("repeat")
	if !reflect.DeepEqual(receipt, personalKeyBehaviorLedger(t, db)) {
		t.Fatal("repeat rewrote receipt")
	}
	remove()
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Go(func() { results <- database.MigrateThrough(ctx, db, 104) })
	}
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal("concurrent certificate startup failed")
		}
	}
	assert()
	// Model a committed writer CHECK followed by absent reader CHECK; the same
	// path runs on both real drivers and does not assume transactional MySQL DDL.
	if db.Migrator().DropConstraint(&entity.VaultReaderAuth{}, "ck_vault_reader_method") != nil {
		t.Fatal("construct partial CHECK DDL")
	}
	remove()
	migrate("partial_reader_check")
	rollback := errors.New("owned positive control rollback")
	for _, c := range checks {
		for _, value := range []string{"token", "approle", "certificate"} {
			accepted := false
			err := db.Transaction(func(tx *gorm.DB) error {
				q := tx.Model(c.current).Where("id = ?", writer.ID).Update("method", value)
				if q.Error != nil {
					return q.Error
				}
				// An unchanged MySQL UPDATE may report zero; prove the exact stored row.
				if q.RowsAffected < 0 || q.RowsAffected > 1 {
					return errors.New("positive did not address exact row")
				}
				var stored struct {
					ID               string
					Method           string
					SecretGeneration string
					AuthCiphertext   string
				}
				readback := tx.Model(c.current).Where("id = ?", writer.ID).Take(&stored)
				if readback.Error != nil || readback.RowsAffected != 1 || stored.ID != writer.ID || stored.Method != value || stored.SecretGeneration != c.generation || stored.AuthCiphertext != c.ciphertext {
					return errors.New("positive did not preserve exact retained row")
				}
				accepted = true
				return rollback
			})
			if !accepted || !errors.Is(err, rollback) {
				t.Fatal("canonical method rejected")
			}
			preserved()
		}
		for _, value := range []any{"Certificate", "CERTIFICATE", "certificate ", " certificate", "", nil, "unknown"} {
			if db.Model(c.current).Where("id = ?", writer.ID).Update("method", value).Error == nil {
				t.Fatal("invalid method admitted")
			}
			preserved()
		}
	}
	// Unknown retained value must reject before either CHECK is replaced.
	if db.Migrator().DropConstraint(&entity.VaultWriterAuth{}, "ck_vault_writer_method") != nil {
		t.Fatal("prepare retained invalid method")
	}
	if q := db.Model(&entity.VaultWriterAuth{}).Where("id = ?", writer.ID).Update("method", "unknown"); q.Error != nil || q.RowsAffected != 1 {
		t.Fatal("invalid retained control was not stored")
	}
	remove()
	if database.MigrateThrough(ctx, db, 104) == nil || !reflect.DeepEqual(prefix, personalKeyBehaviorLedger(t, db)) || db.Migrator().HasConstraint(&entity.VaultWriterAuth{}, "ck_vault_writer_method") == true {
		t.Fatal("invalid retained row gained DDL or receipt")
	}
	if db.Model(&entity.VaultWriterAuth{}).Where("id = ?", writer.ID).Update("method", writer.Method).Error != nil {
		t.Fatal("restore exact retained method")
	}
	migrate("restored_retained_method")
	// A nonempty partial table is never silently backfilled by this CHECK-only step.
	if db.Migrator().DropConstraint(&entity.VaultWriterAuth{}, "ck_vault_writer_method") != nil || db.Migrator().DropColumn(&entity.VaultWriterAuth{}, "Method") != nil {
		t.Fatal("construct missing retained column")
	}
	remove()
	if database.MigrateThrough(ctx, db, 104) == nil || db.Migrator().HasColumn(&entity.VaultWriterAuth{}, "Method") || !reflect.DeepEqual(prefix, personalKeyBehaviorLedger(t, db)) {
		t.Fatal("nonempty missing method synthesized")
	}
	if db.Migrator().AddColumn(&certificateWriterHistoricalV103{}, "Method") != nil {
		t.Fatal("restore owned default-token column")
	}
	migrate("restored_method_column")
	// PK uniqueness remains intact; certificate migration adds no index or table.
	duplicate := writer
	if db.Create(&duplicate).Error == nil {
		t.Fatal("retained auth identity uniqueness lost")
	}
	preserved()
	migrate("final_repeat")
	beforeCurrent := personalKeyBehaviorLedger(t, db)
	if database.Migrate(ctx, db) != nil {
		t.Fatal("restore current105 after bounded certificate scenario")
	}
	current := personalKeyBehaviorLedger(t, db)
	if len(current) != 106 || current[105].Version != 106 || current[104].Version != 105 || !reflect.DeepEqual(beforeCurrent, current[:104]) {
		t.Fatal("current105 restoration changed bounded V104 receipts")
	}
	preserved()
}
