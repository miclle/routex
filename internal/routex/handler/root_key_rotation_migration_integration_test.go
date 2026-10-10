package handler

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/secretstore"
)

// Private partial tables reconstruct committed MySQL DDL without dropping an
// index through PostgreSQL's known GORM DropIndex adapter limitation.
type rootPolicyPartialV48Fixture struct {
	ID          int       `gorm:"primaryKey;autoIncrement:false"`
	Initialized bool      `gorm:"not null"`
	WriteKeyID  *string   `gorm:"size:64"`
	Epoch       uint64    `gorm:"not null"`
	ETag        string    `gorm:"column:etag;size:64;not null"`
	ActiveJobID *string   `gorm:"size:30"`
	UpdatedAt   time.Time `gorm:"precision:6;not null"`
}

func (rootPolicyPartialV48Fixture) TableName() string { return "secret_write_policies" }

type rootReceiptPartialV48Fixture struct {
	RequestID   string    `gorm:"primaryKey;size:36"`
	ActorID     string    `gorm:"size:30;not null"`
	JobID       string    `gorm:"size:30;not null"`
	Action      string    `gorm:"size:16;not null"`
	RequestHash string    `gorm:"size:64;not null"`
	ReviewETag  string    `gorm:"column:review_etag;size:64;not null"`
	ResultEpoch uint64    `gorm:"not null"`
	ResultKeyID string    `gorm:"size:64;not null"`
	CreatedAt   time.Time `gorm:"precision:6;not null"`
}

func (rootReceiptPartialV48Fixture) TableName() string { return "secret_rotation_receipts" }

type rootSystemJobCodeV47Fixture struct {
	Code string `gorm:"size:40;not null;check:ck_system_jobs_code,code IN ('runtime_publication','call_record_delivery','storage_cleanup')"`
}

func (rootSystemJobCodeV47Fixture) TableName() string { return "system_jobs" }

func testRootKeyRotationMigration(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx := context.Background()
	legacy, err := secretstore.New(bytes.Repeat([]byte{71}, 32))
	if err != nil {
		t.Fatal(err)
	}
	retained := rootRotationSeedLegacy(t, db, legacy)
	baseline := retained.read(t, db)
	stamp := time.Now().UTC().Truncate(time.Microsecond)
	oldOperational := entity.SystemJob{ID: "job_root_retained", Code: "runtime_publication", Status: "completed", ItemsTotal: 2, ItemsCompleted: 2, StartedAt: stamp, UpdatedAt: stamp, CompletedAt: &stamp}
	if err := db.Create(&oldOperational).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Take(&oldOperational, "id = ?", oldOperational.ID).Error; err != nil {
		t.Fatal(err)
	}
	history := entity.SecretRotationReceipt{RequestID: "48000000-1111-4111-8111-111111111111", ActorID: "usr_v48_missing", JobID: "srt_01arz3ndektsv4rrffq69g5fav", Action: "start", RequestHash: strings.Repeat("a", 64), ReviewETag: strings.Repeat("b", 64), ResultEpoch: 2, ResultKeyID: "next", CreatedAt: stamp}
	models := []any{&entity.SecretProcessVerification{}, &entity.SecretRotationReceipt{}, &entity.SecretRotationItem{}, &entity.SecretRotationJob{}, &entity.SecretRootKey{}, &entity.SecretWritePolicy{}}
	checks := []struct {
		model any
		names []string
	}{
		{&entity.SecretWritePolicy{}, []string{"ck_secret_policy_singleton", "ck_secret_policy_state", "ck_secret_policy_etag"}},
		{&entity.SecretRootKey{}, []string{"ck_secret_root_identity", "ck_secret_root_state", "ck_secret_root_retired"}},
		{&entity.SecretRotationJob{}, []string{"ck_secret_rotation_keys", "ck_secret_rotation_epoch", "ck_secret_rotation_etag", "ck_secret_rotation_status", "ck_secret_rotation_phase", "ck_secret_rotation_domain", "ck_secret_rotation_terminal"}},
		{&entity.SecretRotationItem{}, []string{"ck_secret_item_domain", "ck_secret_item_outcome", "ck_secret_item_attempts"}},
		{&entity.SecretRotationReceipt{}, []string{"ck_secret_receipt_hash", "ck_secret_receipt_action", "ck_secret_receipt_epoch"}},
		{&entity.SecretProcessVerification{}, []string{"ck_secret_process_epoch"}},
	}
	for prefix := range 3 {
		t.Logf("V48 partial-DDL prefix %d", prefix)
		for _, model := range models {
			if err := db.Migrator().DropTable(model); err != nil {
				t.Fatal(err)
			}
		}
		if result := db.Table("schema_migrations").Where("version IN ?", []int{48, 72, 94, 95}).Delete(&struct{}{}); result.Error != nil || result.RowsAffected != 4 {
			t.Fatal("reconstruct V48 and additive V72/V94/V95 ledgers independently", result.Error, result.RowsAffected)
		}
		// Reconstruct the released operational code guard without touching its rows.
		if err := db.Migrator().DropConstraint(&rootSystemJobCodeV47Fixture{}, "ck_system_jobs_code"); err != nil {
			t.Fatal(err)
		}
		if err := db.Migrator().CreateConstraint(&rootSystemJobCodeV47Fixture{}, "ck_system_jobs_code"); err != nil {
			t.Fatal(err)
		}
		var persisted *entity.SecretRotationReceipt
		if prefix != 0 {
			for _, partial := range []any{&rootPolicyPartialV48Fixture{}, &rootReceiptPartialV48Fixture{}} {
				if err := db.Migrator().CreateTable(partial); err != nil {
					t.Fatal("create committed partial table", err)
				}
			}
			policy := entity.SecretWritePolicy{ID: 1, ETag: strings.Repeat("c", 64), UpdatedAt: stamp}
			if err := db.Create(&policy).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Create(&history).Error; err != nil {
				t.Fatal("history unexpectedly requires a live actor/job", err)
			}
			var before entity.SecretRotationReceipt
			if err := db.Take(&before, "request_id = ?", history.RequestID).Error; err != nil {
				t.Fatal(err)
			}
			persisted = &before // Persisted precision/location is the comparison baseline.
			if prefix == 2 {
				if err := db.Migrator().CreateIndex(&entity.SecretRotationReceipt{}, "idx_secret_receipt_job"); err != nil {
					t.Fatal(err)
				}
			}
		}
		var workers sync.WaitGroup
		results := make(chan error, 2)
		for range 2 {
			workers.Go(func() { results <- database.Migrate(ctx, db) })
		}
		workers.Wait()
		close(results)
		for err := range results {
			if err != nil {
				t.Fatal("concurrent V48 startup repair", prefix, err)
			}
		}
		if err := database.Migrate(ctx, db); err != nil {
			t.Fatal("repeat V48", err)
		}
		for _, guard := range checks {
			if !db.Migrator().HasTable(guard.model) {
				t.Fatal("V48 table missing", guard.model)
			}
			for _, name := range guard.names {
				if !db.Migrator().HasConstraint(guard.model, name) {
					t.Fatal("V48 partial repair omitted guard", prefix, name)
				}
			}
			statement := &gorm.Statement{DB: db}
			if err := statement.Parse(guard.model); err != nil || len(statement.Schema.Relationships.Relations) != 0 {
				t.Fatal("V48 history acquired live relations", err)
			}
		}
		if !db.Migrator().HasIndex(&entity.SecretRotationReceipt{}, "idx_secret_receipt_job") {
			t.Fatal("V48 receipt index not repaired")
		}
		if after := retained.read(t, db); !reflect.DeepEqual(baseline, after) {
			t.Fatal("V48 altered retained five-domain ciphertext or lifecycle/MFA state", prefix)
		}
		var operationalAfter entity.SystemJob
		if err := db.Take(&operationalAfter, "id = ?", oldOperational.ID).Error; err != nil || !reflect.DeepEqual(oldOperational, operationalAfter) {
			t.Fatal("V48 changed retained operational history", err)
		}
		var policy entity.SecretWritePolicy
		if err := db.Take(&policy, 1).Error; err != nil || policy.Initialized || policy.WriteKeyID != nil || policy.ActiveJobID != nil || policy.Epoch != 0 {
			t.Fatal("DDL selected secret material or initialized policy", err)
		}
		if persisted != nil {
			var after entity.SecretRotationReceipt
			if err := db.Take(&after, "request_id = ?", history.RequestID).Error; err != nil || !reflect.DeepEqual(*persisted, after) || policy.ETag != strings.Repeat("c", 64) {
				t.Fatal("repeat migration overwrote committed policy/history", prefix, err)
			}
		}
	}
	var adminPermissions, memberPermissions int64
	if err := db.Table("role_permissions").Where("role_id = ? AND permission IN ?", "rol_admin", []string{"secrets.read", "secrets.rotate"}).Count(&adminPermissions).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Table("role_permissions").Where("role_id = ? AND permission IN ?", "rol_member", []string{"secrets.read", "secrets.rotate"}).Count(&memberPermissions).Error; err != nil || adminPermissions != 2 || memberPermissions != 0 {
		t.Fatal("V48 widened builtin member authority or duplicated permission seed", err)
	}
	duplicate := history
	duplicate.ActorID = "usr_v48_other"
	if err := db.Create(&duplicate).Error; !errors.Is(err, gorm.ErrDuplicatedKey) {
		t.Fatal("global receipt UUID did not reject different actor intent portably", err)
	}
	for index, mutate := range []func(*entity.SecretRotationReceipt){
		func(row *entity.SecretRotationReceipt) { row.RequestHash = "short" },
		func(row *entity.SecretRotationReceipt) { row.ReviewETag = "short" },
		func(row *entity.SecretRotationReceipt) { row.Action = "START" },
		func(row *entity.SecretRotationReceipt) { row.ResultEpoch = 0 },
	} {
		invalid := history
		invalid.RequestID = []string{"48000000-2222-4222-8222-222222222222", "48000000-3333-4333-8333-333333333333", "48000000-4444-4444-8444-444444444444", "48000000-5555-4555-8555-555555555555"}[index]
		mutate(&invalid)
		if err := db.Create(&invalid).Error; err == nil {
			t.Fatal("V48 invalid receipt constraint accepted", index)
		}
	}
	for _, change := range []map[string]any{{"id": 2}, {"Initialized": true}, {"Epoch": 1}, {"ETag": "short"}} {
		if err := db.Model(&entity.SecretWritePolicy{}).Where("id = ?", 1).Updates(change).Error; err == nil {
			t.Fatal("V48 invalid singleton policy accepted", change)
		}
	}
	key := entity.SecretRootKey{KeyID: "orphan-key", State: "decrypt_only", ProofCiphertext: "inert-history", CreatedAt: stamp}
	if err := db.Create(&key).Error; err != nil {
		t.Fatal(err)
	}
	caseKey := key
	caseKey.KeyID = "ORPHAN-key"
	if err := db.Create(&caseKey).Error; err != nil {
		t.Fatal("V48 canonical case-distinct root IDs collapsed", err)
	}
	if err := db.Create(&caseKey).Error; !errors.Is(err, gorm.ErrDuplicatedKey) {
		t.Fatal("V48 exact root-key identity was not unique", err)
	}
	for _, change := range []map[string]any{{"State": "unknown"}, {"State": "WRITE"}, {"State": "retired"}, {"ProofCiphertext": ""}} {
		if err := db.Model(&entity.SecretRootKey{}).Where("key_id = ?", key.KeyID).Updates(change).Error; err == nil {
			t.Fatal("V48 invalid key-state proof guard accepted", change)
		}
	}
	// Historical progress remains valid without a live policy job or subject.
	job := entity.SecretRotationJob{ID: "srt_v48_missing", SourceKeyID: "source", TargetKeyID: "target", CutoverEpoch: 1, ScanGeneration: 1, ETag: strings.Repeat("f", 64), Status: "migrating", Phase: "migration", CountsJSON: "{}", CreatedAt: stamp, UpdatedAt: stamp}
	if err := db.Create(&job).Error; err != nil {
		t.Fatal("valid orphan historical job", err)
	}
	caseJob := job
	caseJob.ID = "srt_v48_case_keys"
	caseJob.SourceKeyID, caseJob.TargetKeyID = "RootA", "roota"
	if err := db.Create(&caseJob).Error; err != nil {
		t.Fatal("V48 case-distinct canonical source/target keys collapsed", err)
	}
	for _, change := range []map[string]any{
		{"TargetKeyID": "source"}, {"CutoverEpoch": 0}, {"ScanGeneration": 0}, {"ETag": "short"},
		{"Status": "unknown"}, {"Status": "MIGRATING"}, {"Phase": "unknown"}, {"Phase": "MIGRATION"}, {"Domain": 6}, {"Status": "completed"},
	} {
		if err := db.Model(&entity.SecretRotationJob{}).Where("id = ?", job.ID).Updates(change).Error; err == nil {
			t.Fatal("V48 invalid job guard accepted", change)
		}
	}
	item := entity.SecretRotationItem{JobID: "srt_v48_deleted", Domain: "storage_revisions", SubjectID: "str_v48_deleted", Reference: "storage:str_v48_deleted:sec_deleted", OriginalDigest: strings.Repeat("a", 64), ResultDigest: strings.Repeat("b", 64), Outcome: "deleted", Attempts: 1, UpdatedAt: stamp}
	if err := db.Create(&item).Error; err != nil {
		t.Fatal("historical item unexpectedly required live job/storage", err)
	}
	for _, change := range []map[string]any{{"Domain": "unknown"}, {"Outcome": "unknown"}, {"Attempts": 0}} {
		if err := db.Model(&entity.SecretRotationItem{}).Where("job_id = ? AND domain = ? AND subject_id = ?", item.JobID, item.Domain, item.SubjectID).Updates(change).Error; err == nil {
			t.Fatal("V48 invalid progress guard accepted", change)
		}
	}
	if !db.Migrator().HasConstraint(&entity.SystemJob{}, "ck_system_jobs_code") {
		t.Fatal("V48 actual job code guard missing")
	}
	operational := entity.SystemJob{ID: "job_v48_rotation", Code: "secret_root_rotation", Status: "completed", StartedAt: stamp, UpdatedAt: stamp, CompletedAt: &stamp}
	if err := db.Create(&operational).Error; err != nil {
		t.Fatal("V48 rejects actual rotation operational run", err)
	}
	if err := db.Model(&entity.SystemJob{}).Where("id = ?", operational.ID).Update("Code", "invented").Error; err == nil {
		t.Fatal("V48 operational code check broadened beyond actual jobs")
	}
	proof := entity.SecretProcessVerification{ProcessID: "ins_v48_missing", PolicyEpoch: 1, CryptoVersion: 2, KeyManifestDigest: strings.Repeat("d", 64), LeaseToken: "lck_v48_missing", RuntimeSnapshotID: "cfg_v48_missing", RuntimeSourceDigest: strings.Repeat("e", 64), VerifiedAt: stamp}
	if err := db.Create(&proof).Error; err != nil {
		t.Fatal("history unexpectedly required live SystemInstance", err)
	}
	if err := db.Model(&entity.SecretProcessVerification{}).Where("process_id = ?", proof.ProcessID).Update("CryptoVersion", 1).Error; err == nil {
		t.Fatal("V48 accepted unsupported crypto process proof")
	}
}
