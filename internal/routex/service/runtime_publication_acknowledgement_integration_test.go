package service

import (
	"errors"
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

// Only exclusively owned migration fixtures may peel this complete empty suffix.
// No retained acknowledgement or historical command can be discarded.
func peelRuntimePublicationAcknowledgementFixture(t *testing.T, db *gorm.DB) {
	t.Helper()
	var before []runtimeArchiveCleanupLedger
	if err := db.Order("version").Find(&before).Error; err != nil {
		t.Fatal(err)
	}
	if err := admissionOwnerFixtureV118(db, len(before)); err != nil {
		t.Fatal(err)
	}
	if len(before) == 116 {
		if db.Migrator().HasTable(&entity.RuntimePublicationAcknowledgement{}) {
			t.Fatal("V116 retained an acknowledgement overlay")
		}
		return
	}
	if len(before) != 117 && len(before) != 118 {
		t.Fatal("exact V117 or V118 ledger required")
	}
	for i, row := range before {
		if row.Version != i+1 || row.AppliedAt == "" {
			t.Fatal("incomplete V117 ledger")
		}
	}
	model := &entity.RuntimePublicationAcknowledgement{}
	st := &gorm.Statement{DB: db}
	if err := st.Parse(model); err != nil {
		t.Fatal(err)
	}
	if !db.Migrator().HasTable(model) {
		t.Fatal("missing V117 overlay")
	}
	for _, name := range st.Schema.DBNames {
		if !db.Migrator().HasColumn(model, name) {
			t.Fatal("partial V117 overlay")
		}
	}
	for name := range st.Schema.ParseCheckConstraints() {
		if !db.Migrator().HasConstraint(model, name) {
			t.Fatal("missing V117 check")
		}
	}
	for _, name := range []string{"fk_runtime_publication_ack_command", "fk_runtime_publication_ack_installation"} {
		if !db.Migrator().HasConstraint(model, name) {
			t.Fatal("missing V117 reference")
		}
	}
	for _, index := range st.Schema.ParseIndexes() {
		if !db.Migrator().HasIndex(model, index.Name) {
			t.Fatal("missing V117 index")
		}
	}
	var count int64
	if db.Model(model).Count(&count).Error != nil || count != 0 {
		t.Fatal("retained acknowledgement prevents historical reconstruction")
	}
	if len(before) == 118 {
		if err := peelAdmissionOwnerFixtureV118(db, before); err != nil {
			t.Fatal(err)
		}
		before = before[:117]
	}
	if err := db.Migrator().DropTable(model); err != nil {
		t.Fatal(err)
	}
	removed := db.Where("version = ? AND applied_at = ?", 117, before[116].AppliedAt).Delete(&runtimeArchiveTestLedger{})
	var after []runtimeArchiveCleanupLedger
	if removed.Error != nil || removed.RowsAffected != 1 || db.Order("version").Find(&after).Error != nil || !reflect.DeepEqual(after, before[:116]) {
		t.Fatal("V117 peel changed retained prefix")
	}
}

// Runs inside the existing archive upgrade child on both disposable drivers;
// no new registry entry, process birth or test-name census is introduced.
func testRuntimePublicationAcknowledgementMigration(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx := t.Context()
	var before []runtimeArchiveCleanupLedger
	if err := db.Order("version").Find(&before).Error; err != nil || len(before) != 118 {
		t.Fatal("V118 startup required", err)
	}
	for i, row := range before {
		if row.Version != i+1 || row.AppliedAt == "" {
			t.Fatal("noncontiguous V118 startup")
		}
	}
	testAdmissionOwnerFixtureV118(t, db, before)
	peelRuntimePublicationAcknowledgementFixture(t, db)
	if err := database.MigrateThrough(ctx, db, 116); err != nil || db.Migrator().HasTable(&entity.RuntimePublicationAcknowledgement{}) {
		t.Fatal("bounded V116 fabricated acknowledgement", err)
	}
	var wg sync.WaitGroup
	failures := make(chan error, 2)
	for range 2 {
		wg.Go(func() { failures <- database.Migrate(ctx, db) })
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal("concurrent V118 startup", err)
		}
	}
	ledger := func() []runtimeArchiveCleanupLedger {
		t.Helper()
		var rows []runtimeArchiveCleanupLedger
		if err := db.Order("version").Find(&rows).Error; err != nil {
			t.Fatal(err)
		}
		return rows
	}
	if got := ledger(); len(got) != 118 || !reflect.DeepEqual(got[:116], before[:116]) {
		t.Fatal("V117 rewrote released ledger")
	}
	stable := ledger()
	if err := database.Migrate(ctx, db); err != nil || !reflect.DeepEqual(stable, ledger()) {
		t.Fatal("repeat V117 changed history", err)
	}
	model := &entity.RuntimePublicationAcknowledgement{}
	var count int64
	if db.Model(model).Count(&count).Error != nil || count != 0 {
		t.Fatal("migration backfilled legacy proof")
	}
	birth := time.Now().UTC().Truncate(time.Microsecond)
	command := entity.RuntimePublicationCommand{RequestID: "af739b12-12ce-4a20-8da7-04bc8885dfb8", ActorID: "usr_01m36yee4gkbns18pfcqqc75a3", ActorCreatedAt: birth, Reason: "Retain exact local installation", ClaimInstanceID: "ins_01m36yee4gkbns18pfcqqc75a3", ClaimInstanceStartedAt: birth, ClaimedAt: birth, Outcome: "claimed"}
	installation := entity.RuntimeInstallationObservation{ID: "rin_01m36yee4gkbns18pfcqqc75a3", InstanceID: command.ClaimInstanceID, InstanceStartedAt: birth, SnapshotID: "cfg_01m36yee4gkbns18pfcqqc75a3", ProjectionVersion: 2, SourceDigest: personalHash([]string{"fixture installation"}), RoutesPublishedAt: birth, FirstObservedAt: birth}
	if err := db.Create(&command).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&installation).Error; err != nil {
		t.Fatal(err)
	}
	row := entity.RuntimePublicationAcknowledgement{RequestID: command.RequestID, InstallationID: installation.ID, AcknowledgedAt: birth.Add(time.Microsecond)}
	if err := db.Omit("Command", "Installation").Create(&row).Error; err != nil {
		t.Fatal("valid acknowledgement rejected", err)
	}
	var retained entity.RuntimePublicationAcknowledgement
	if db.Take(&retained, "request_id = ?", row.RequestID).Error != nil {
		t.Fatal("read retained acknowledgement")
	}
	if db.Create(&row).Error == nil {
		t.Fatal("duplicate command acknowledgement admitted")
	}
	if db.Delete(&command).Error == nil || db.Delete(&installation).Error == nil {
		t.Fatal("immutable parents lost restrictive protection")
	}
	orphan := row
	orphan.RequestID = "bf739b12-12ce-4a20-8da7-04bc8885dfb8"
	if db.Create(&orphan).Error == nil {
		t.Fatal("orphan command admitted")
	}
	// A valid separate parent isolates the installation FK negative.
	other := command
	other.RequestID = orphan.RequestID
	if db.Create(&other).Error != nil {
		t.Fatal("second original command fixture")
	}
	orphan.InstallationID = "rin_01m36yee4gkbns18pfcqqc75a4"
	if db.Create(&orphan).Error == nil {
		t.Fatal("orphan installation admitted")
	}
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal("retained V117 repeat", err)
	}
	var after entity.RuntimePublicationAcknowledgement
	if db.Take(&after, "request_id = ?", row.RequestID).Error != nil || retained.RequestID != after.RequestID || retained.InstallationID != after.InstallationID || !retained.AcknowledgedAt.Equal(after.AcknowledgedAt) {
		t.Fatal("repeat rewrote exact acknowledgement")
	}
	removeReceipt := func() {
		t.Helper()
		rows := ledger()
		if len(rows) != 117 {
			t.Fatal("missing V117 receipt")
		}
		r := db.Where("version = ? AND applied_at = ?", 117, rows[116].AppliedAt).Delete(&runtimeArchiveTestLedger{})
		if r.Error != nil || r.RowsAffected != 1 || !reflect.DeepEqual(ledger(), rows[:116]) {
			t.Fatal("remove exact V117 receipt")
		}
	}
	if err := peelAdmissionOwnerFixtureV118(db, ledger()); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrator().DropColumn(model, "AcknowledgedAt"); err != nil {
		t.Fatal(err)
	}
	removeReceipt()
	if err := database.Migrate(ctx, db); err == nil || db.Migrator().HasColumn(model, "AcknowledgedAt") || len(ledger()) != 116 {
		t.Fatal("nonempty partial proof synthesized")
	}
	if err := db.Migrator().DropTable(model); err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal("restore empty V117 table", err)
	}
	if db.Model(model).Count(&count).Error != nil || count != 0 {
		t.Fatal("repair invented acknowledgement")
	}
	// Empty interrupted DDL may resume; retained command/installation stay exact.
	if err := peelAdmissionOwnerFixtureV118(db, ledger()); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrator().DropColumn(model, "AcknowledgedAt"); err != nil {
		t.Fatal(err)
	}
	removeReceipt()
	if err := database.Migrate(ctx, db); err != nil || !db.Migrator().HasColumn(model, "AcknowledgedAt") {
		t.Fatal("empty V117 partial resume", err)
	}
	if db.Delete(&command).Error != nil || db.Delete(&other).Error != nil || db.Delete(&installation).Error != nil {
		t.Fatal("clean only owned migration fixture parents")
	}
	// Keep current118 and its exact116 predecessor after all destructive controls.
	if got := ledger(); len(got) != 118 || !reflect.DeepEqual(got[:116], before[:116]) {
		t.Fatal("V117 controls changed released prefix")
	}
}

// V118 is removable only in an exclusively owned historical test fixture.
// Schema and data checks are read-only; callers run all older refusal guards
// before invoking the separate owner-first DDL phase.
func admissionOwnerFixtureV118(db *gorm.DB, head int) error {
	models := []any{&entity.AdmissionMode{}, &entity.AdmissionSpoolOwner{}}
	if head < 118 {
		for _, model := range models {
			if db.Migrator().HasTable(model) {
				return errors.New("historical ledger retained admission owner overlay")
			}
		}
		return nil
	}
	if head != 118 {
		return errors.New("unsupported admission owner fixture head")
	}
	for _, model := range models {
		st := &gorm.Statement{DB: db}
		if err := st.Parse(model); err != nil {
			return err
		}
		if !db.Migrator().HasTable(model) {
			return errors.New("missing admission owner fixture table")
		}
		columns, err := db.Migrator().ColumnTypes(model)
		if err != nil || len(columns) != len(st.Schema.DBNames) {
			return errors.New("incomplete admission owner fixture columns")
		}
		seen := map[string]bool{}
		for _, column := range columns {
			field := st.Schema.FieldsByDBName[column.Name()]
			if field == nil || seen[column.Name()] {
				return errors.New("unexpected admission owner fixture column")
			}
			seen[column.Name()] = true
			if increment, known := column.AutoIncrement(); known && increment != field.AutoIncrement {
				return errors.New("invalid admission owner fixture auto increment")
			}
			pk, known := column.PrimaryKey()
			if !known || pk != field.PrimaryKey {
				return errors.New("invalid admission owner fixture primary key")
			}
			nullable, known := column.Nullable()
			if !known || nullable != (!field.NotNull && !field.PrimaryKey) {
				return errors.New("invalid admission owner fixture nullability")
			}
			def, present := column.DefaultValue()
			if present != field.HasDefaultValue || (present && strings.Trim(strings.SplitN(def, "::", 2)[0], "'() ") != field.DefaultValue) {
				return errors.New("invalid admission owner fixture default")
			}
			kind := strings.ToLower(column.DatabaseTypeName())
			switch field.IndirectFieldType.Kind() {
			case reflect.String:
				width, known := column.Length()
				if !known || width != int64(field.Size) || (kind != "varchar" && kind != "character varying") {
					return errors.New("invalid admission owner fixture string")
				}
			case reflect.Int64:
				if kind != "bigint" && kind != "int8" {
					return errors.New("invalid admission owner fixture integer")
				}
			case reflect.Int:
				if kind != "bigint" && kind != "int8" && kind != "int" && kind != "int4" && kind != "integer" {
					return errors.New("invalid admission owner fixture singleton")
				}
			case reflect.Struct:
				precision, _, known := column.DecimalSize()
				if field.IndirectFieldType != reflect.TypeFor[time.Time]() || (kind != "timestamp" && kind != "timestamptz" && kind != "datetime") || !known || precision != 6 {
					return errors.New("invalid admission owner fixture timestamp")
				}
			default:
				return errors.New("unsupported admission owner fixture column")
			}
		}
		for name := range st.Schema.ParseCheckConstraints() {
			if !db.Migrator().HasConstraint(model, name) {
				return errors.New("missing admission owner fixture check")
			}
		}
		for _, relation := range st.Schema.Relationships.Relations {
			if constraint := relation.ParseConstraint(); constraint != nil && !db.Migrator().HasConstraint(model, constraint.Name) {
				return errors.New("missing admission owner fixture reference")
			}
		}
		indexes, err := db.Migrator().GetIndexes(model)
		if err != nil {
			return err
		}
		for _, expected := range st.Schema.ParseIndexes() {
			matched := false
			for _, actual := range indexes {
				if actual.Name() != expected.Name {
					continue
				}
				unique, known := actual.Unique()
				columns := actual.Columns()
				if !known || unique != (expected.Class == "UNIQUE") || len(columns) != len(expected.Fields) {
					return errors.New("invalid admission owner fixture index")
				}
				for i, field := range expected.Fields {
					if columns[i] != field.DBName {
						return errors.New("invalid admission owner fixture index order")
					}
				}
				matched = true
			}
			if !matched {
				return errors.New("missing admission owner fixture index")
			}
		}
	}
	var count int64
	if err := db.Model(&entity.AdmissionSpoolOwner{}).Count(&count).Error; err != nil || count != 0 {
		return errors.New("retained admission owner history prevents fixture peel")
	}
	var modes []entity.AdmissionMode
	if err := db.Find(&modes).Error; err != nil || len(modes) > 1 {
		return errors.New("invalid admission mode fixture")
	}
	if len(modes) == 0 {
		return nil
	}
	var binding entity.LimitInstallation
	if err := db.Take(&binding, 1).Error; err != nil || binding.ID != 1 || !binding.Initialized || !regexp.MustCompile(`^led_[0-7][0-9a-hjkmnp-tv-z]{25}$`).MatchString(binding.JournalID) {
		return errors.New("uninitialized admission mode fixture")
	}
	mode := modes[0]
	if mode.ID != 1 || mode.Mode != "local" || mode.Revision != 1 || mode.InstallationID != binding.JournalID {
		return errors.New("retained or foreign admission mode prevents fixture peel")
	}
	return nil
}

func peelAdmissionOwnerFixtureV118(db *gorm.DB, expected []runtimeArchiveCleanupLedger) error {
	if len(expected) != 118 {
		return errors.New("exact V118 fixture receipts required")
	}
	for i, row := range expected {
		if row.Version != i+1 || row.AppliedAt == "" {
			return errors.New("invalid V118 fixture receipts")
		}
	}
	var actual []runtimeArchiveCleanupLedger
	if err := db.Table("schema_migrations").Order("version").Find(&actual).Error; err != nil || !reflect.DeepEqual(actual, expected) {
		return errors.New("V118 fixture receipt mismatch")
	}
	if err := admissionOwnerFixtureV118(db, len(expected)); err != nil {
		return err
	}
	if err := db.Migrator().DropTable(&entity.AdmissionSpoolOwner{}, &entity.AdmissionMode{}); err != nil {
		return err
	}
	removed := db.Table("schema_migrations").Where("version = ? AND applied_at = ?", 118, expected[117].AppliedAt).Delete(&struct{}{})
	actual = nil
	if removed.Error != nil || removed.RowsAffected != 1 || db.Table("schema_migrations").Order("version").Find(&actual).Error != nil || !reflect.DeepEqual(actual, expected[:117]) {
		return errors.New("V118 peel changed retained prefix")
	}
	return nil
}

// Exercise retained owner/mode refusals without adding named registry children.
func testAdmissionOwnerFixtureV118(t *testing.T, db *gorm.DB, expected []runtimeArchiveCleanupLedger) {
	t.Helper()
	if err := admissionOwnerFixtureV118(db, len(expected)); err != nil {
		t.Fatal("pristine V118 fixture required", err)
	}
	rollback := errors.New("rollback admission owner fixture refusal")
	for _, kind := range []string{"shared", "revised", "foreign", "owner"} {
		err := db.Transaction(func(tx *gorm.DB) error {
			journal := "led_01arz3ndektsv4rrffq69g5fav"
			var binding entity.LimitInstallation
			if err := tx.Take(&binding, 1).Error; err != nil {
				return err
			}
			if binding.Initialized {
				journal = binding.JournalID
			} else if err := tx.Model(&entity.LimitInstallation{}).Where("id = ?", 1).Updates(map[string]any{"initialized": true, "journal_id": journal}).Error; err != nil {
				return err
			}
			if err := tx.Where("id = ?", 1).Delete(&entity.AdmissionMode{}).Error; err != nil {
				return err
			}
			mode := entity.AdmissionMode{ID: 1, InstallationID: journal, Mode: "local", Revision: 1}
			switch kind {
			case "shared":
				mode.Mode = "shared"
			case "revised":
				mode.Revision = 2
			case "foreign":
				if journal[len(journal)-1] == '0' {
					mode.InstallationID = journal[:len(journal)-1] + "1"
				} else {
					mode.InstallationID = journal[:len(journal)-1] + "0"
				}
			}
			if err := tx.Omit("Installation").Create(&mode).Error; err != nil {
				return err
			}
			if kind == "owner" {
				birth := time.Now().UTC().Truncate(time.Microsecond)
				owner := entity.AdmissionSpoolOwner{InstallationID: journal, SpoolID: "retained-fixture", ModeID: 1, ModeRevision: 1, RegistrationID: "7b258039-18f7-4bf5-975d-27efa7fa6a8f", IncarnationID: "ins_01arz3ndektsv4rrffq69g5fav", InstanceStartedAt: birth, RegisteredAt: birth, RegisteredFence: 1, CurrentFence: 1}
				if err := tx.Omit("Mode").Create(&owner).Error; err != nil {
					return err
				}
			}
			if admissionOwnerFixtureV118(tx, 118) == nil || peelAdmissionOwnerFixtureV118(tx, expected) == nil {
				return errors.New("retained admission owner fixture admitted teardown")
			}
			var actual []runtimeArchiveCleanupLedger
			var after entity.AdmissionMode
			var ownerCount int64
			wantOwners := int64(0)
			if kind == "owner" {
				wantOwners = 1
			}
			if tx.Table("schema_migrations").Order("version").Find(&actual).Error != nil || !reflect.DeepEqual(actual, expected) || tx.Take(&after, 1).Error != nil || after.ID != mode.ID || after.InstallationID != mode.InstallationID || after.Mode != mode.Mode || after.Revision != mode.Revision || tx.Model(&entity.AdmissionSpoolOwner{}).Count(&ownerCount).Error != nil || ownerCount != wantOwners || !tx.Migrator().HasTable(&entity.AdmissionSpoolOwner{}) {
				return errors.New("admission owner refusal changed retained facts")
			}
			return rollback
		})
		if !errors.Is(err, rollback) {
			t.Fatal("admission owner historical refusal", kind, err)
		}
	}
	for _, mutate := range []func([]runtimeArchiveCleanupLedger) []runtimeArchiveCleanupLedger{
		func(rows []runtimeArchiveCleanupLedger) []runtimeArchiveCleanupLedger {
			rows[117].AppliedAt = "changed"
			return rows
		},
		func(rows []runtimeArchiveCleanupLedger) []runtimeArchiveCleanupLedger {
			rows[117].Version = 119
			return rows
		},
		func(rows []runtimeArchiveCleanupLedger) []runtimeArchiveCleanupLedger {
			return append(rows, runtimeArchiveCleanupLedger{Version: 119, AppliedAt: "unrelated"})
		},
	} {
		bad := mutate(append([]runtimeArchiveCleanupLedger(nil), expected...))
		var after []runtimeArchiveCleanupLedger
		if peelAdmissionOwnerFixtureV118(db, bad) == nil || db.Table("schema_migrations").Order("version").Find(&after).Error != nil || !reflect.DeepEqual(after, expected) || !db.Migrator().HasTable(&entity.AdmissionSpoolOwner{}) || !db.Migrator().HasTable(&entity.AdmissionMode{}) {
			t.Fatal("malformed V118 tail changed fixture schema or receipts")
		}
	}
}
