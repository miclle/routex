package handler

import (
	"context"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
)

type callDiagnosticsRecordV25Integration struct {
	RequestID       string `gorm:"primaryKey;size:64"`
	RouteStopReason string `gorm:"size:40;not null;default:''"`
}

func (callDiagnosticsRecordV25Integration) TableName() string { return "call_records" }

type callDiagnosticsAttemptV25Integration struct {
	ID              string `gorm:"primaryKey;size:64"`
	AttemptNumber   int    `gorm:"not null;default:0"`
	FailureClass    string `gorm:"size:40;not null;default:permanent_failure"`
	WorkEvidence    string `gorm:"size:40;not null;default:unknown"`
	OutputStarted   bool   `gorm:"not null;default:false"`
	FinalUsageKnown bool   `gorm:"not null;default:false"`
	EvidenceCode    string `gorm:"size:40;not null;default:''"`
}

func (callDiagnosticsAttemptV25Integration) TableName() string { return "call_attempts" }

func testCallAttemptDiagnosticsMigration(t *testing.T, db *gorm.DB) {
	t.Helper()
	now := time.Now().UTC()
	record := entity.CallRecord{SnapshotID: "cfg_attempt_migration", RequestID: "req_attempt_migration", UserID: "usr_attempt_migration", KeyID: "key_attempt_migration", ModelID: "mdl_attempt_migration", ModelName: "attempt-migration", ProviderModelID: "pmd_attempt_migration", ConnectionID: "con_attempt_migration", Protocol: entity.ProtocolOpenAIChat, Status: "error", StartedAt: now, CompletedAt: now}
	if err := db.Create(&record).Error; err != nil {
		t.Fatal(err)
	}
	attempt := entity.CallAttempt{ID: "att_attempt_migration", RequestID: record.RequestID, ProviderModelID: record.ProviderModelID, ConnectionID: record.ConnectionID, Status: "error", StartedAt: now, CompletedAt: now}
	if err := db.Create(&attempt).Error; err != nil {
		t.Fatal(err)
	}

	recordModel := &callDiagnosticsRecordV25Integration{}
	attemptModel := &callDiagnosticsAttemptV25Integration{}
	for _, field := range []string{"RouteStopReason"} {
		if err := db.Migrator().DropColumn(recordModel, field); err != nil {
			t.Fatal(err)
		}
	}
	for _, field := range []string{"AttemptNumber", "FailureClass", "WorkEvidence", "OutputStarted", "FinalUsageKnown", "EvidenceCode"} {
		if err := db.Migrator().DropColumn(attemptModel, field); err != nil {
			t.Fatal(err)
		}
	}
	removeCallAttemptDiagnosticsMigrationLedger(t, db)
	if err := database.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	assertCallAttemptDiagnosticsSchema(t, db)
	var upgradedRecord callDiagnosticsRecordV25Integration
	var upgradedAttempt callDiagnosticsAttemptV25Integration
	if err := db.Select("request_id", "route_stop_reason").First(&upgradedRecord, "request_id = ?", record.RequestID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Select("id", "attempt_number", "failure_class", "work_evidence", "output_started", "final_usage_known", "evidence_code").First(&upgradedAttempt, "id = ?", attempt.ID).Error; err != nil {
		t.Fatal(err)
	}
	if upgradedRecord.RouteStopReason != "" || upgradedAttempt.AttemptNumber != 0 || upgradedAttempt.FailureClass != "permanent_failure" || upgradedAttempt.WorkEvidence != "unknown" || upgradedAttempt.OutputStarted || upgradedAttempt.FinalUsageKnown || upgradedAttempt.EvidenceCode != "" {
		t.Fatalf("legacy attempt gained unsafe evidence: record=%+v attempt=%+v", upgradedRecord, upgradedAttempt)
	}

	if err := db.Model(recordModel).Where("request_id = ?", record.RequestID).Update("route_stop_reason", "unsafe_to_replay").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(attemptModel).Where("id = ?", attempt.ID).Updates(map[string]any{"attempt_number": 2, "failure_class": "connection_failure", "evidence_code": "transport_ambiguous"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Migrator().DropColumn(attemptModel, "WorkEvidence"); err != nil {
		t.Fatal(err)
	}
	removeCallAttemptDiagnosticsMigrationLedger(t, db)
	if err := database.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	assertCallAttemptDiagnosticsSchema(t, db)
	upgradedRecord, upgradedAttempt = callDiagnosticsRecordV25Integration{}, callDiagnosticsAttemptV25Integration{}
	if err := db.Select("request_id", "route_stop_reason").First(&upgradedRecord, "request_id = ?", record.RequestID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Select("id", "attempt_number", "failure_class", "work_evidence", "output_started", "final_usage_known", "evidence_code").First(&upgradedAttempt, "id = ?", attempt.ID).Error; err != nil {
		t.Fatal(err)
	}
	if upgradedRecord.RouteStopReason != "unsafe_to_replay" || upgradedAttempt.AttemptNumber != 2 || upgradedAttempt.FailureClass != "connection_failure" || upgradedAttempt.WorkEvidence != "unknown" || upgradedAttempt.EvidenceCode != "transport_ambiguous" {
		t.Fatalf("partial attempt migration changed preserved evidence: record=%+v attempt=%+v", upgradedRecord, upgradedAttempt)
	}
}

func removeCallAttemptDiagnosticsMigrationLedger(t *testing.T, db *gorm.DB) {
	t.Helper()
	result := db.Table("schema_migrations").Where("version = ?", 25).Delete(&struct{}{})
	if result.Error != nil || result.RowsAffected != 1 {
		t.Fatalf("remove migration 25 ledger: affected=%d err=%v", result.RowsAffected, result.Error)
	}
}

func assertCallAttemptDiagnosticsSchema(t *testing.T, db *gorm.DB) {
	t.Helper()
	if !db.Migrator().HasColumn(&callDiagnosticsRecordV25Integration{}, "RouteStopReason") {
		t.Fatal("call attempt diagnostics migration did not restore RouteStopReason")
	}
	for _, field := range []string{"AttemptNumber", "FailureClass", "WorkEvidence", "OutputStarted", "FinalUsageKnown", "EvidenceCode"} {
		if !db.Migrator().HasColumn(&callDiagnosticsAttemptV25Integration{}, field) {
			t.Fatalf("call attempt diagnostics migration did not restore %s", field)
		}
	}
}
