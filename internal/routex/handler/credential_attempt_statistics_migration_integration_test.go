package handler

import (
	"context"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
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
	if len(before) != 111 || before[110].Version != 111 || before[109].Version != 110 || before[108].Version != 109 || before[107].Version != 108 || before[106].Version != 107 || before[105].Version != 106 || before[104].Version != 105 || before[103].Version != 104 || before[102].Version != 103 || before[100].Version != 101 || before[101].Version != 102 || before[99].Version != 100 || before[98].Version != 99 || before[97].Version != 98 || before[96].Version != 97 || before[95].Version != 96 || before[94].Version != 95 || before[93].Version != 94 || before[92].Version != 93 || before[91].Version != 92 {
		t.Fatal("exact V93 ledger")
	}
	const index = "idx_attempts_credential_time"
	if !db.Migrator().HasIndex(&entity.CallAttempt{}, index) {
		t.Fatal("empty startup chronology index")
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	// Fixed historical rows isolate index repair from current RecordCall side
	// effects. The fixture starts at the real current head and adds no future tables.
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Table("call_records").Create(map[string]any{
			"request_id": "req_stats_migration", "snapshot_id": "", "user_id": "usr_historical", "key_id": "key_historical",
			"model_id": "mdl_historical", "model_name": "", "provider_model_id": "", "connection_id": "", "protocol": "openai_chat",
			"status": "error", "stream": false, "started_at": now, "completed_at": now, "duration_ms": 0, "error_code": "",
		}).Error; err != nil {
			return err
		}
		return tx.Table("call_attempts").Create(map[string]any{
			"id": "att_stats_migration", "request_id": "req_stats_migration", "credential_id": "crd_historical", "snapshot_id": "cfg_historical",
			"provider_model_id": "", "connection_id": "", "attempt_number": 1, "status": "error", "http_status": 0,
			"error_code": "process_interrupted", "started_at": now, "completed_at": now,
		}).Error
	}); err != nil {
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
		if len(currentLedger) != 111 || currentLedger[110].Version != 111 || currentLedger[109].Version != 110 || currentLedger[108].Version != 109 || currentLedger[107].Version != 108 || currentLedger[106].Version != 107 || currentLedger[105].Version != 106 || currentLedger[104].Version != 105 || currentLedger[103].Version != 104 || currentLedger[102].Version != 103 || currentLedger[100].Version != 101 || currentLedger[101].Version != 102 || currentLedger[99].Version != 100 || currentLedger[98].Version != 99 || currentLedger[97].Version != 98 || currentLedger[96].Version != 97 || currentLedger[95].Version != 96 || currentLedger[94].Version != 95 || currentLedger[93].Version != 94 || currentLedger[92].Version != 93 || !reflect.DeepEqual(before[:92], currentLedger[:92]) || !reflect.DeepEqual(before[93:], currentLedger[93:]) {
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
