package handler

import (
	"context"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
)

func testSystemStatusMigration(t *testing.T, db *gorm.DB) {
	t.Helper()
	now := time.Now().UTC()
	instance := entity.SystemInstance{
		ID: "ins_01m36yee4gkbns18pfcqqc75a3", LeaseToken: "lck_migration",
		HeartbeatRevision: 3, Name: "migration-process", Hostname: "migration.example.invalid",
		Role: "combined", Version: "test", Commit: "abc123", BuildTime: "2026-09-29T00:00:00Z",
		GoVersion: "go-test", OS: "test", Arch: "test", StartedAt: now.Add(-time.Minute),
		LastHeartbeatAt: now, LeaseExpiresAt: now.Add(time.Minute),
	}
	if err := db.Create(&instance).Error; err != nil {
		t.Fatal(err)
	}
	progress := 50
	job := entity.SystemJob{
		ID: "job_01m36yee4gkbns18pfcqqc75a3", Code: "storage_cleanup", Status: "running",
		ExecutorID: instance.ID, Progress: &progress, ItemsTotal: 2, ItemsCompleted: 1,
		StartedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&job).Error; err != nil {
		t.Fatal(err)
	}

	for _, index := range []struct {
		model any
		name  string
	}{
		{&entity.SystemInstance{}, "idx_system_instances_cleanup"},
		{&entity.SystemInstance{}, "idx_system_instances_lease"},
		{&entity.SystemJob{}, "idx_system_jobs_code_started"},
		{&entity.SystemJob{}, "idx_system_jobs_status_updated"},
	} {
		var err error
		if db.Name() == "postgres" {
			// postgres.Migrator.DropIndex in v1.6.2 emits the invalid
			// `DROP INDEX CURRENT_SCHEMA().name`; static test DDL avoids
			// obscuring the production GORM-first migration behavior.
			err = db.Exec(`DROP INDEX IF EXISTS "` + index.name + `"`).Error
		} else {
			err = db.Migrator().DropIndex(index.model, index.name)
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Where("role_id = ? AND permission = ?", "rol_admin", "system.write").Delete(&entity.RolePermission{}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Table("schema_migrations").Where("version IN ?", []int{27, 28}).Delete(&struct{}{}).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	assertSystemStatusMigration(t, db, instance.ID, job.ID)

	// Reenter both immutable steps once more to prove their seed and index work
	// is idempotent after MySQL's nontransactional DDL.
	if err := db.Table("schema_migrations").Where("version IN ?", []int{27, 28}).Delete(&struct{}{}).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	assertSystemStatusMigration(t, db, instance.ID, job.ID)

	invalidProgress := 101
	invalid := entity.SystemJob{
		ID: "job_01m36yee4gkbns18pfcqqc75a4", Code: "storage_cleanup", Status: "running",
		Progress: &invalidProgress, StartedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&invalid).Error; err == nil {
		t.Fatal("system job progress check accepted an impossible percentage")
	}
	for name, mutate := range map[string]func(*entity.SystemJob){
		"code":      func(row *entity.SystemJob) { row.Code = "demonstration" },
		"status":    func(row *entity.SystemJob) { row.Status = "queued" },
		"total":     func(row *entity.SystemJob) { row.ItemsTotal = -1 },
		"completed": func(row *entity.SystemJob) { row.ItemsCompleted = row.ItemsTotal + 1 },
	} {
		t.Run("system_job_constraint_"+name, func(t *testing.T) {
			invalid := job
			invalid.ID, invalid.Progress = "job_01m36yee4gkbns18pfcqqc75"+name[:1], nil
			mutate(&invalid)
			if err := db.Create(&invalid).Error; err == nil {
				t.Fatalf("system job %s constraint accepted invalid data", name)
			}
		})
	}
	invalidRole := instance
	invalidRole.ID = "ins_01m36yee4gkbns18pfcqqc75a4"
	invalidRole.Role = "primary"
	if err := db.Create(&invalidRole).Error; err == nil {
		t.Fatal("system instance role check accepted a fabricated primary role")
	}
	invalidRevision := instance
	invalidRevision.ID = "ins_01m36yee4gkbns18pfcqqc75a5"
	invalidRevision.HeartbeatRevision = 0
	if err := db.Create(&invalidRevision).Error; err == nil {
		t.Fatal("system instance revision check accepted zero")
	}
}

func assertSystemStatusMigration(t *testing.T, db *gorm.DB, instanceID, jobID string) {
	t.Helper()
	for _, index := range []struct {
		model any
		name  string
	}{
		{&entity.SystemInstance{}, "idx_system_instances_cleanup"},
		{&entity.SystemInstance{}, "idx_system_instances_lease"},
		{&entity.SystemJob{}, "idx_system_jobs_code_started"},
		{&entity.SystemJob{}, "idx_system_jobs_status_updated"},
	} {
		if !db.Migrator().HasIndex(index.model, index.name) {
			t.Fatalf("migration did not restore %s", index.name)
		}
	}
	var instances, jobs, permissions int64
	if err := db.Model(&entity.SystemInstance{}).Where("id = ?", instanceID).Count(&instances).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&entity.SystemJob{}).Where("id = ?", jobID).Count(&jobs).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&entity.RolePermission{}).Where("role_id = ? AND permission = ?", "rol_admin", "system.write").Count(&permissions).Error; err != nil {
		t.Fatal(err)
	}
	if instances != 1 || jobs != 1 || permissions != 1 {
		t.Fatalf("migration reentry changed data: instances=%d jobs=%d permissions=%d", instances, jobs, permissions)
	}
}
