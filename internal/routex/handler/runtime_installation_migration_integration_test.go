package handler

import (
	"context"
	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func testRuntimeInstallationMigration(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	db = db.WithContext(ctx)
	original := personalKeyBehaviorLedger(t, db)
	if len(original) != 100 {
		t.Fatal("V100 current ledger required")
	}
	for i, row := range original {
		if row.Version != i+1 {
			t.Fatal("migration prefix changed")
		}
	}
	model := &entity.RuntimeInstallationObservation{}
	var count int64
	if db.Model(model).Count(&count).Error != nil || count != 0 {
		t.Fatal("migration backfilled an observation")
	}
	// A literal historical V87 fixture is routing-only data, not a claimed
	// actual installation. The upgrade must leave it unpromoted and byte-exact.
	legacyBirth := time.Now().UTC().Truncate(time.Microsecond)
	legacy := entity.RuntimeRoutingApplication{ID: "rap_01m36yee4gkbns18pfcqqc75a3", InstanceID: "ins_01m36yee4gkbns18pfcqqc75a3", InstanceStartedAt: legacyBirth, SnapshotID: "cfg_01m36yee4gkbns18pfcqqc75a3", RouteDigest: strings.Repeat("f", 64), PublishedAt: legacyBirth, AppliedAt: legacyBirth.Add(time.Microsecond)}
	if db.Create(&legacy).Error != nil {
		t.Fatal("seed retained routing-only fixture")
	}
	var old []entity.RuntimeRoutingApplication
	if db.Session(&gorm.Session{QueryFields: true}).Order("id").Find(&old).Error != nil {
		t.Fatal("capture routing-only history")
	}
	dropReceipt := func() {
		t.Helper()
		r := db.Table("schema_migrations").Where("version = ?", 100).Delete(&struct{}{})
		if r.Error != nil || r.RowsAffected != 1 {
			t.Fatal("remove only V100")
		}
	}
	prefix := original[:99]
	check := func() {
		t.Helper()
		got := personalKeyBehaviorLedger(t, db)
		if len(got) != 100 || got[99].Version != 100 || !reflect.DeepEqual(got[:99], prefix) {
			t.Fatal("V99 ledger prefix rewritten")
		}
		var retained []entity.RuntimeRoutingApplication
		if db.Session(&gorm.Session{QueryFields: true}).Order("id").Find(&retained).Error != nil || !reflect.DeepEqual(retained, old) {
			t.Fatal("routing-only history promoted or changed")
		}
	}
	// Only fixed schema categories may enter failure output. Driver errors may
	// contain SQL or values, so an unrecognized error remains unclassified.
	schemaError := func(err error) string {
		for _, category := range []string{
			"invalid runtime installation column set",
			"invalid runtime installation primary key",
			"invalid runtime installation nullability",
			"invalid runtime installation default",
			"invalid runtime installation string",
			"invalid runtime installation timestamp",
			"invalid runtime installation integer",
			"invalid runtime installation field",
			"invalid retained runtime installation projection",
			"invalid runtime installation index idx_runtime_installation_source",
			"invalid runtime installation index idx_runtime_installation_instance",
		} {
			if strings.HasSuffix(err.Error(), category) {
				return category
			}
		}
		return "unclassified_migration_error"
	}
	migrate := func(stage string) {
		t.Helper()
		if err := database.Migrate(ctx, db); err != nil {
			t.Fatalf("V100 migration failed: stage=%s schema_error=%s", stage, schemaError(err))
		}
		check()
	}
	if err := db.Migrator().DropTable(model); err != nil {
		t.Fatal(err)
	}
	dropReceipt()
	if err := database.MigrateThrough(ctx, db, 99); err != nil {
		t.Fatal("retained V99 boundary failed")
	}
	if db.Migrator().HasTable(model) {
		t.Fatal("bounded V99 created new table")
	}
	migrate("upgrade_from_v99")
	if db.Model(model).Count(&count).Error != nil || count != 0 {
		t.Fatal("empty upgrade fabricated evidence")
	}
	// Independent concurrent startups contend through the original migration lock.
	dropReceipt()
	var wg sync.WaitGroup
	failures := make(chan error, 2)
	for range 2 {
		wg.Go(func() { failures <- database.Migrate(ctx, db) })
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatalf("concurrent V100 migration failed: stage=concurrent_startup schema_error=%s", schemaError(err))
		}
	}
	migrate("repeat_after_concurrent")
	birth := time.Now().UTC().Truncate(time.Microsecond)
	row := entity.RuntimeInstallationObservation{ID: "rin_01m36yee4gkbns18pfcqqc75a3", InstanceID: "ins_01m36yee4gkbns18pfcqqc75a3", InstanceStartedAt: birth, SnapshotID: "cfg_01m36yee4gkbns18pfcqqc75a3", ProjectionVersion: 1, SourceDigest: strings.Repeat("a", 64), RoutesPublishedAt: birth, FirstObservedAt: birth.Add(time.Microsecond)}
	if db.Create(&row).Error != nil {
		t.Fatal("valid observation rejected")
	}
	var saved entity.RuntimeInstallationObservation
	if db.Session(&gorm.Session{QueryFields: true}).Take(&saved, "id = ?", row.ID).Error != nil {
		t.Fatal("read retained observation")
	}
	duplicate := row
	duplicate.ID = "rin_01m36yee4gkbns18pfcqqc75a4"
	if db.Create(&duplicate).Error == nil {
		t.Fatal("duplicate source admitted")
	}
	invalid := duplicate
	invalid.SourceDigest = strings.Repeat("b", 64)
	invalid.ProjectionVersion = 2
	if db.Create(&invalid).Error == nil {
		t.Fatal("future projection admitted")
	}
	for _, name := range []string{"idx_runtime_installation_source", "idx_runtime_installation_instance"} {
		if err := database.DropIndex(db, model, name); err != nil {
			t.Fatal(err)
		}
		dropReceipt()
		migrate("restore_missing_index:" + name)
	}
	if err := db.Migrator().DropConstraint(model, "ck_runtime_installation_projection"); err != nil {
		t.Fatal(err)
	}
	if db.Model(model).Where("id = ?", saved.ID).Update("projection_version", 2).Error != nil {
		t.Fatal("construct invalid retained projection")
	}
	dropReceipt()
	if database.Migrate(ctx, db) == nil {
		t.Fatal("invalid retained projection admitted")
	}
	var invalidReceipt int64
	if db.Table("schema_migrations").Where("version = ?", 100).Count(&invalidReceipt).Error != nil || invalidReceipt != 0 {
		t.Fatal("invalid retained projection recorded success")
	}
	if db.Model(model).Where("id = ?", saved.ID).Update("projection_version", 1).Error != nil {
		t.Fatal("restore owned retained projection")
	}
	migrate("restore_projection_check")
	invalid.ProjectionVersion = 0
	if db.Create(&invalid).Error == nil {
		t.Fatal("partial CHECK replay did not restore rejection")
	}
	var after entity.RuntimeInstallationObservation
	if db.Session(&gorm.Session{QueryFields: true}).Take(&after, "id = ?", saved.ID).Error != nil || !reflect.DeepEqual(saved, after) {
		t.Fatal("repeat rewrote first observation")
	}
	if err := db.Where("id = ?", saved.ID).Delete(model).Error; err != nil {
		t.Fatal(err)
	}
	// Empty partial physical tables may resume; incompatible shapes must never be
	// silently rewritten. Dynamic GORM models are fixture-owned, not migration definitions.
	typ := reflect.TypeFor[entity.RuntimeInstallationObservation]()
	for _, which := range []string{"width", "type", "precision", "nullable", "default", "primary", "extra", "index_order", "index_unique"} {
		if err := db.Migrator().DropTable(model); err != nil {
			t.Fatal(err)
		}
		dropReceipt()
		fields := make([]reflect.StructField, typ.NumField())
		for i := range fields {
			fields[i] = typ.Field(i)
		}
		switch which {
		case "primary":
			fields[0].Tag = `gorm:"size:30;not null"`
			fields[3].Tag = `gorm:"primaryKey;size:30;not null"`
		case "extra":
			fields = append(fields, reflect.StructField{Name: "Unexpected", Type: reflect.TypeFor[string](), Tag: `gorm:"size:30"`})
		case "width":
			fields[3].Tag = `gorm:"size:31;not null"`
		case "type":
			fields[3].Tag = `gorm:"type:text;not null"`
		case "precision":
			fields[2].Tag = `gorm:"precision:3;not null"`
		case "nullable":
			fields[3].Tag = `gorm:"size:30"`
		case "default":
			fields[3].Tag = `gorm:"size:30;not null;default:invented"`
		case "index_order":
			fields[1].Tag = `gorm:"size:30;not null;uniqueIndex:idx_runtime_installation_source,priority:2;index:idx_runtime_installation_instance,priority:1"`
			fields[5].Tag = `gorm:"size:64;not null;uniqueIndex:idx_runtime_installation_source,priority:1"`
		case "index_unique":
			fields[1].Tag = `gorm:"size:30;not null;index:idx_runtime_installation_source,priority:1;index:idx_runtime_installation_instance,priority:1"`
			fields[5].Tag = `gorm:"size:64;not null;index:idx_runtime_installation_source,priority:2"`
		}
		changed := reflect.New(reflect.StructOf(fields)).Interface()
		if db.Table(model.TableName()).Migrator().CreateTable(changed) != nil {
			t.Fatal("construct incompatible fixture", which)
		}
		if database.Migrate(ctx, db) == nil {
			t.Fatal("incompatible partial table admitted", which)
		}
		var ledgerCount int64
		if db.Table("schema_migrations").Where("version = ?", 100).Count(&ledgerCount).Error != nil || ledgerCount != 0 {
			t.Fatal("failed migration recorded success")
		}
		if db.Migrator().DropTable(model) != nil {
			t.Fatal("remove owned incompatible fixture")
		}
		migrate("restore_after_incompatible:" + which)
	}
	// Simulate missing-column partial DDL without legacy rows or invented defaults.
	if db.Migrator().DropColumn(model, "FirstObservedAt") != nil {
		t.Fatal("partial column fixture")
	}
	dropReceipt()
	migrate("resume_missing_first_observed_at")
	if db.Model(model).Count(&count).Error != nil || count != 0 {
		t.Fatal("partial resume fabricated observations")
	}
	// A crash after restoring the column can leave this same shortened index.
	// Construct it portably with frozen GORM tags on both supported drivers.
	if err := db.Migrator().DropTable(model); err != nil {
		t.Fatal("construct complete-column partial index fixture")
	}
	short := &runtimeInstallationShortInstanceIndexV100{}
	if err := db.Migrator().CreateTable(short); err != nil {
		t.Fatal("create frozen partial index fixture")
	}
	indexes, err := db.Migrator().GetIndexes(short)
	if err != nil {
		t.Fatal("inspect frozen partial index fixture")
	}
	shortened := false
	for _, index := range indexes {
		if index.Name() == "idx_runtime_installation_instance" {
			unique, known := index.Unique()
			shortened = known && !unique && reflect.DeepEqual(index.Columns(), []string{"instance_id"})
		}
	}
	if !shortened || !db.Migrator().HasColumn(short, "FirstObservedAt") {
		t.Fatal("complete-column shortened index prerequisite absent")
	}
	if db.Create(&row).Error != nil {
		t.Fatal("seed retained complete-column partial index observation")
	}
	var partialSaved entity.RuntimeInstallationObservation
	if db.Session(&gorm.Session{QueryFields: true}).Take(&partialSaved, "id = ?", row.ID).Error != nil {
		t.Fatal("capture complete-column partial index observation")
	}
	dropReceipt()
	migrate("resume_complete_columns_short_instance_index")
	migrate("repeat_repaired_instance_index")
	var partialAfter entity.RuntimeInstallationObservation
	if db.Session(&gorm.Session{QueryFields: true}).Take(&partialAfter, "id = ?", row.ID).Error != nil || !reflect.DeepEqual(partialSaved, partialAfter) {
		t.Fatal("partial index repair rewrote retained observation")
	}
	if db.Model(model).Count(&count).Error != nil || count != 1 {
		t.Fatal("partial index repair fabricated observations")
	}
}

// Independent V100 fixture: complete frozen columns, only the instance index's
// second field is absent. It is not a migration schema or a current-entity alias.
type runtimeInstallationShortInstanceIndexV100 struct {
	ID                string    `gorm:"primaryKey;size:30"`
	InstanceID        string    `gorm:"size:30;not null;uniqueIndex:idx_runtime_installation_source,priority:1;index:idx_runtime_installation_instance,priority:1"`
	InstanceStartedAt time.Time `gorm:"precision:6;not null"`
	SnapshotID        string    `gorm:"size:30;not null"`
	ProjectionVersion int       `gorm:"not null;check:ck_runtime_installation_projection,projection_version = 1"`
	SourceDigest      string    `gorm:"size:64;not null;uniqueIndex:idx_runtime_installation_source,priority:2"`
	RoutesPublishedAt time.Time `gorm:"precision:6;not null"`
	FirstObservedAt   time.Time `gorm:"precision:6;not null"`
}

func (runtimeInstallationShortInstanceIndexV100) TableName() string {
	return "runtime_installation_observations"
}
