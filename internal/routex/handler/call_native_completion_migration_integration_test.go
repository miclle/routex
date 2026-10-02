package handler

import (
	"context"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
)

func testCallNativeCompletionMigration(t *testing.T, db *gorm.DB) {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Microsecond)
	call := entity.CallRecord{RequestID: "req_native_upgrade", SnapshotID: "cfg_native_parent", UserID: "usr_historical", Protocol: entity.ProtocolOpenAIChat, Status: "success", StartedAt: now, CompletedAt: now}
	attempt := entity.CallAttempt{ID: "att_native_upgrade", RequestID: call.RequestID, CredentialID: "crd_native_historical", SnapshotID: "cfg_native_attempt", Status: "success", WorkEvidence: "completed", FinalUsageKnown: true, StartedAt: now, CompletedAt: now}
	for _, row := range []any{&call, &attempt} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	model := &entity.CallAttempt{}
	if err := db.Migrator().DropConstraint(model, "ck_attempts_native_completion"); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrator().DropColumn(model, "NativeCompletionEvidence"); err != nil {
		t.Fatal(err)
	}
	removeCallNativeCompletionLedger(t, db)
	var wg sync.WaitGroup
	failures := make(chan error, 2)
	for range 2 {
		wg.Go(func() { failures <- database.Migrate(context.Background(), db) })
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	assertCallNativeCompletionSchema(t, db)
	var upgraded entity.CallAttempt
	load := func() {
		t.Helper()
		upgraded = entity.CallAttempt{}
		if err := db.Select("id", "credential_id", "snapshot_id", "native_completion_evidence", "status", "work_evidence", "final_usage_known", "completed_at").First(&upgraded, "id = ?", attempt.ID).Error; err != nil {
			t.Fatal(err)
		}
		if upgraded.CredentialID != attempt.CredentialID || upgraded.SnapshotID != attempt.SnapshotID || upgraded.Status != "success" || upgraded.WorkEvidence != "completed" || !upgraded.FinalUsageKnown || !upgraded.CompletedAt.Equal(now) {
			t.Fatal("native evidence migration changed existing immutable attempt facts")
		}
	}
	load()
	if upgraded.NativeCompletionEvidence != "unknown" {
		t.Fatal("V32 upgrade inferred native completion from success or usage")
	}
	// Partial MySQL DDL: column committed and explicit evidence already stored,
	// but constraint and ledger absent. Repair must preserve that exact marker.
	if err := db.Model(model).Where("id = ?", attempt.ID).Update("native_completion_evidence", "handoff").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Migrator().DropConstraint(model, "ck_attempts_native_completion"); err != nil {
		t.Fatal(err)
	}
	removeCallNativeCompletionLedger(t, db)
	if err := database.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	assertCallNativeCompletionSchema(t, db)
	load()
	if upgraded.NativeCompletionEvidence != "handoff" {
		t.Fatal("constraint-only repair changed explicit native evidence")
	}
	removeCallNativeCompletionLedger(t, db)
	if err := database.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	load()
	if upgraded.NativeCompletionEvidence != "handoff" {
		t.Fatal("reentry/repeat changed native evidence")
	}
	for _, allowed := range []string{"unknown", "completed", "handoff", "blocked", "incomplete"} {
		if err := db.Model(model).Where("id = ?", attempt.ID).Update("native_completion_evidence", allowed).Error; err != nil {
			t.Fatalf("DB rejected allowed native domain %s: %v", allowed, err)
		}
	}
	for _, forbidden := range []any{nil, "future_native_value", ""} {
		if err := db.Model(model).Where("id = ?", attempt.ID).Update("native_completion_evidence", forbidden).Error; err == nil {
			t.Fatalf("DB allowed null/unsupported native domain %v", forbidden)
		}
	}
}

func removeCallNativeCompletionLedger(t *testing.T, db *gorm.DB) {
	t.Helper()
	result := db.Table("schema_migrations").Where("version = ?", 33).Delete(&struct{}{})
	if result.Error != nil || result.RowsAffected != 1 {
		t.Fatal("cannot reconstruct V33 migration ledger")
	}
}

func assertCallNativeCompletionSchema(t *testing.T, db *gorm.DB) {
	t.Helper()
	columns, err := db.Migrator().ColumnTypes(&entity.CallAttempt{})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, column := range columns {
		if column.Name() != "native_completion_evidence" {
			continue
		}
		found = true
		if size, known := column.Length(); !known || size != 20 {
			t.Fatal("native evidence must remain VARCHAR20")
		}
		if nullable, known := column.Nullable(); !known || nullable {
			t.Fatal("native evidence must be nonnull")
		}
		if value, known := column.DefaultValue(); !known || value != "unknown" {
			t.Fatal("native evidence default must remain unknown")
		}
	}
	if !found || !db.Migrator().HasConstraint(&entity.CallAttempt{}, "ck_attempts_native_completion") {
		t.Fatal("native evidence migration missing column/check")
	}
	// Native evidence adds no live relation or new index. Preserve the exact
	// attribution lookup and all existing quality indexes from earlier steps.
	assertCallCredentialAttributionSchema(t, db)
	for _, index := range []string{"idx_attempts_provider_time", "idx_attempts_connection_time", "idx_attempts_provider_model_time"} {
		if !db.Migrator().HasIndex(&entity.CallAttempt{}, index) {
			t.Fatalf("native evidence migration lost prior index %s", index)
		}
	}
}
