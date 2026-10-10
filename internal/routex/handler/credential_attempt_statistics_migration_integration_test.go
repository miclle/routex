package handler

import (
	"context"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"gorm.io/gorm"
)

type wrongCredentialAttemptIndexV93 struct {
	CredentialID string `gorm:"size:30;index:idx_attempts_credential_time"`
}

func (wrongCredentialAttemptIndexV93) TableName() string { return "call_attempts" }

type wrongOrderCredentialAttemptIndexV93 struct {
	CredentialID string    `gorm:"size:30;index:idx_attempts_credential_time,priority:1"`
	ID           string    `gorm:"size:64;index:idx_attempts_credential_time,priority:2"`
	CompletedAt  time.Time `gorm:"index:idx_attempts_credential_time,priority:3"`
}

func (wrongOrderCredentialAttemptIndexV93) TableName() string { return "call_attempts" }

type uniqueCredentialAttemptIndexV93 struct {
	CredentialID string    `gorm:"size:30;uniqueIndex:idx_attempts_credential_time,priority:1"`
	CompletedAt  time.Time `gorm:"uniqueIndex:idx_attempts_credential_time,priority:2"`
	ID           string    `gorm:"size:64;uniqueIndex:idx_attempts_credential_time,priority:3"`
}

func (uniqueCredentialAttemptIndexV93) TableName() string { return "call_attempts" }

type extraPrefixCredentialAttemptIndexV93 struct {
	CredentialID string    `gorm:"size:30;index:idx_attempts_credential_time,priority:1"`
	CompletedAt  time.Time `gorm:"index:idx_attempts_credential_time,priority:2"`
	ID           string    `gorm:"size:64;index:idx_attempts_credential_time,priority:3"`
	ErrorCode    string    `gorm:"size:64;index:idx_attempts_credential_time,priority:4,length:4"`
}

func (extraPrefixCredentialAttemptIndexV93) TableName() string { return "call_attempts" }

// The shared owner runs this on both supported empty databases after Migrate.
func testCredentialAttemptStatisticsMigration(t *testing.T, db *gorm.DB) {
	ctx := context.Background()
	before := personalKeyBehaviorLedger(t, db)
	if len(before) != 97 || before[96].Version != 97 || before[95].Version != 96 || before[94].Version != 95 || before[93].Version != 94 || before[92].Version != 93 || before[91].Version != 92 {
		t.Fatal("exact V93 ledger")
	}
	const index = "idx_attempts_credential_time"
	if !db.Migrator().HasIndex(&entity.CallAttempt{}, index) {
		t.Fatal("empty startup chronology index")
	}
	svc, err := service.New(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	if err := svc.RecordCall(ctx, service.CallFact{RequestID: "req_stats_migration", UserID: "usr_historical", KeyID: "key_historical", ModelID: "mdl_historical", Protocol: entity.ProtocolOpenAIChat, Status: "error", StartedAt: now, CompletedAt: now,
		Attempts: []service.CallAttempt{{ID: "att_stats_migration", CredentialID: "crd_historical", SnapshotID: "cfg_historical", Status: "error", ErrorCode: "process_interrupted", StartedAt: now, CompletedAt: now}}}); err != nil {
		t.Fatal(err)
	}
	var original entity.CallAttempt
	if err := db.Session(&gorm.Session{QueryFields: true}).Take(&original, "id = ?", "att_stats_migration").Error; err != nil {
		t.Fatal(err)
	}
	removeLedger := func() {
		t.Helper()
		result := db.Table("schema_migrations").Where("version = ?", 93).Delete(&struct{}{})
		if result.Error != nil || result.RowsAffected != 1 {
			t.Fatal("remove only V93 ledger", result.Error)
		}
	}
	assertRetained := func() {
		t.Helper()
		var current entity.CallAttempt
		if err := db.Session(&gorm.Session{QueryFields: true}).Take(&current, "id = ?", original.ID).Error; err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(original, current) {
			t.Fatal("index migration rewrote retained attempt")
		}
		currentLedger := personalKeyBehaviorLedger(t, db)
		if len(currentLedger) != 97 || currentLedger[96].Version != 97 || currentLedger[95].Version != 96 || currentLedger[94].Version != 95 || currentLedger[93].Version != 94 || currentLedger[92].Version != 93 || !reflect.DeepEqual(before[:92], currentLedger[:92]) || !reflect.DeepEqual(before[93:], currentLedger[93:]) {
			t.Fatal("released migration prefix changed")
		}
	}
	if err := database.DropIndex(db, &entity.CallAttempt{}, index); err != nil {
		t.Fatal(err)
	}
	removeLedger()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	assertRetained()
	// MySQL may retain the completed DDL while the version ledger is absent.
	removeLedger()
	var group sync.WaitGroup
	failures := make(chan error, 4)
	for range 4 {
		group.Go(func() { failures <- database.Migrate(ctx, db) })
	}
	group.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	assertRetained()
	// Same-named incomplete, reordered, unique and extended indexes must fail.
	// The wrong-order case retains all three columns: unordered membership is
	// insufficient, and physical table column order is not index key order.
	for _, malformed := range []struct {
		name  string
		model any
	}{
		{"missing columns", &wrongCredentialAttemptIndexV93{}},
		{"wrong order", &wrongOrderCredentialAttemptIndexV93{}},
		{"unique", &uniqueCredentialAttemptIndexV93{}},
		{"extra prefix column", &extraPrefixCredentialAttemptIndexV93{}},
	} {
		if err := database.DropIndex(db, &entity.CallAttempt{}, index); err != nil {
			t.Fatal(err)
		}
		if err := db.Migrator().CreateIndex(malformed.model, index); err != nil {
			t.Fatal(err)
		}
		removeLedger()
		if err := database.Migrate(ctx, db); err == nil {
			t.Fatal("wrong-shape index accepted", malformed.name)
		}
		var versions int64
		if err := db.Table("schema_migrations").Where("version = ?", 93).Count(&versions).Error; err != nil || versions != 0 {
			t.Fatal("failed migration recorded as applied", err)
		}
		if err := database.DropIndex(db, &entity.CallAttempt{}, index); err != nil {
			t.Fatal(err)
		}
		if err := database.Migrate(ctx, db); err != nil {
			t.Fatal(err)
		}
		assertRetained()
	}
}
