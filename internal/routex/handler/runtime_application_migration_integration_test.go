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

func testRuntimeApplicationMigration(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx := context.Background()
	var baseline []int
	if err := db.Table("schema_migrations").Order("version").Pluck("version", &baseline).Error; err != nil || len(baseline) != 97 || baseline[96] != 97 || baseline[95] != 96 || baseline[94] != 95 || baseline[93] != 94 || baseline[92] != 93 || baseline[91] != 92 || baseline[90] != 91 || baseline[89] != 90 || baseline[88] != 89 || baseline[87] != 88 || baseline[86] != 87 {
		t.Fatal("V87 ledger prefix", err)
	}
	for index, version := range baseline {
		if version != index+1 {
			t.Fatal("noncontiguous retained migration ledger")
		}
	}
	retainedLedger := personalKeyBehaviorLedger(t, db)
	birth := time.Now().UTC().Truncate(time.Microsecond)
	instance := entity.SystemInstance{ID: "ins_01m36yee4gkbns18pfcqqc75a3", LeaseToken: "lck_preserved", HeartbeatRevision: 1, Name: "Evidence history", Hostname: "history.invalid", Role: "combined", Version: "test", GoVersion: "test", OS: "test", Arch: "test", StartedAt: birth, LastHeartbeatAt: birth, LeaseExpiresAt: birth.Add(time.Hour)}
	publication := entity.RuntimePublication{ID: "pub_01m36yee4gkbns18pfcqqc75a3", SnapshotID: "cfg_01m36yee4gkbns18pfcqqc75a3", Status: "ready", CreatedAt: birth}
	if err := db.Create(&instance).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&publication).Error; err != nil {
		t.Fatal(err)
	}
	readHistory := func() (entity.SystemInstance, entity.RuntimePublication) {
		t.Helper()
		var i entity.SystemInstance
		var p entity.RuntimePublication
		if err := db.Take(&i, "id = ?", instance.ID).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Take(&p, "id = ?", publication.ID).Error; err != nil {
			t.Fatal(err)
		}
		return i, p
	}
	beforeInstance, beforePublication := readHistory()
	removeLedger := func() {
		t.Helper()
		result := db.Table("schema_migrations").Where("version = ?", 87).Delete(&struct{}{})
		if result.Error != nil || result.RowsAffected != 1 {
			t.Fatal("remove only V87", result.Error)
		}
	}
	repeat := func() {
		t.Helper()
		var wg sync.WaitGroup
		failures := make(chan error, 2)
		for range 2 {
			wg.Go(func() { failures <- database.Migrate(ctx, db) })
		}
		wg.Wait()
		close(failures)
		for err := range failures {
			if err != nil {
				t.Fatal(err)
			}
		}
		if err := database.Migrate(ctx, db); err != nil {
			t.Fatal(err)
		}
		var got []int
		if err := db.Table("schema_migrations").Order("version").Pluck("version", &got).Error; err != nil || !reflect.DeepEqual(got, baseline) {
			t.Fatal("unrelated ledger changed", err)
		}
		if !personalKeyBehaviorLedgerPreserved(retainedLedger, personalKeyBehaviorLedger(t, db), 87) {
			t.Fatal("unrelated migration ledger/version/time changed")
		}
		i, p := readHistory()
		if !reflect.DeepEqual(i, beforeInstance) || !reflect.DeepEqual(p, beforePublication) {
			t.Fatal("historical process/publication changed")
		}
	}
	if err := db.Migrator().DropTable(&entity.RuntimeRoutingApplication{}); err != nil {
		t.Fatal(err)
	}
	removeLedger()
	repeat()
	var count int64
	if err := db.Model(&entity.RuntimeRoutingApplication{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatal("migration fabricated legacy application", err)
	}
	row := entity.RuntimeRoutingApplication{ID: "rap_01m36yee4gkbns18pfcqqc75a3", InstanceID: instance.ID, InstanceStartedAt: birth, SnapshotID: publication.SnapshotID, RouteDigest: strings.Repeat("a", 64), PublishedAt: birth, AppliedAt: birth.Add(time.Microsecond)}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	var saved entity.RuntimeRoutingApplication
	if err := db.Take(&saved, "id = ?", row.ID).Error; err != nil {
		t.Fatal(err)
	}
	duplicate := row
	duplicate.ID = "rap_01m36yee4gkbns18pfcqqc75a4"
	if err := db.Create(&duplicate).Error; err == nil {
		t.Fatal("duplicate process/snapshot accepted")
	}
	invalid := row
	invalid.ID = "rap_01m36yee4gkbns18pfcqqc75a5"
	invalid.SnapshotID = "cfg_01m36yee4gkbns18pfcqqc75a5"
	invalid.RouteDigest = "short"
	if err := db.Create(&invalid).Error; err == nil {
		t.Fatal("digest constraint absent")
	}
	if err := database.DropIndex(db, &entity.RuntimeRoutingApplication{}, "idx_runtime_routing_application_instance"); err != nil {
		t.Fatal(err)
	}
	removeLedger()
	repeat()
	if !db.Migrator().HasIndex(&entity.RuntimeRoutingApplication{}, "idx_runtime_routing_application_instance") {
		t.Fatal("partial DDL did not repair index")
	}
	var after entity.RuntimeRoutingApplication
	if err := db.Take(&after, "id = ?", row.ID).Error; err != nil || !reflect.DeepEqual(saved, after) {
		t.Fatal("partial migration rewrote first application", err)
	}
}
