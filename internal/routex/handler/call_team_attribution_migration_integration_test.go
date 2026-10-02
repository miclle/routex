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

func testCallTeamAttributionMigration(t *testing.T, db *gorm.DB) {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Microsecond)
	legacy := entity.CallRecord{RequestID: "req_team_upgrade", UserID: "usr_old", KeyID: "key_old", Protocol: entity.ProtocolOpenAIChat, Status: "success", StartedAt: now, CompletedAt: now}
	if err := db.Create(&legacy).Error; err != nil {
		t.Fatal(err)
	}
	model := &entity.CallRecord{}
	reset := func(dropFields ...string) {
		t.Helper()
		if db.Migrator().HasConstraint(model, "ck_calls_team_subject") {
			if err := db.Migrator().DropConstraint(model, "ck_calls_team_subject"); err != nil {
				t.Fatal(err)
			}
		}
		if db.Migrator().HasIndex(model, "idx_calls_team_actor_time") {
			if db.Name() == "postgres" {
				// Pinned GORM DropIndex emits invalid CURRENT_SCHEMA().index SQL.
				// This fixed fixture-only identifier reconstructs interrupted DDL;
				// production migration creation and repair remain GORM-only.
				if err := db.Exec("DROP INDEX idx_calls_team_actor_time").Error; err != nil {
					t.Fatal(err)
				}
			} else if err := db.Migrator().DropIndex(model, "idx_calls_team_actor_time"); err != nil {
				t.Fatal(err)
			}
		}
		for _, field := range dropFields {
			if err := db.Migrator().DropColumn(model, field); err != nil {
				t.Fatal(err)
			}
		}
		if err := db.Table("schema_migrations").Where("version = ?", 38).Delete(&struct{}{}).Error; err != nil {
			t.Fatal(err)
		}
	}
	assert := func() {
		t.Helper()
		for _, field := range []string{"TeamID", "TeamMembershipID"} {
			if !db.Migrator().HasColumn(model, field) {
				t.Fatal("missing historical Team attribution", field)
			}
		}
		if !db.Migrator().HasConstraint(model, "ck_calls_team_subject") || !db.Migrator().HasIndex(model, "idx_calls_team_actor_time") {
			t.Fatal("missing Team subject guard or actor cursor index")
		}
		var record entity.CallRecord
		if err := db.Select("request_id", "team_id", "team_membership_id", "user_id", "key_id", "started_at").First(&record, "request_id = ?", legacy.RequestID).Error; err != nil {
			t.Fatal(err)
		}
		if record.TeamID != "" || record.TeamMembershipID != "" || record.UserID != legacy.UserID || record.KeyID != legacy.KeyID || !record.StartedAt.Equal(now) {
			t.Fatal("upgrade invented Team ownership or changed historical facts", record)
		}
	}
	reset("TeamID", "TeamMembershipID")
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
	assert()
	for _, field := range []string{"TeamID", "TeamMembershipID"} {
		reset(field)
		if err := database.Migrate(context.Background(), db); err != nil {
			t.Fatal("partially applied Team migration did not resume", err)
		}
		assert()
	}
	reset()
	if err := database.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	assert()
	if err := database.Migrate(context.Background(), db); err != nil {
		t.Fatal("repeat Team migration failed", err)
	}
	team := legacy
	team.RequestID, team.TeamID, team.TeamMembershipID, team.KeyID = "req_deleted_team", "tem_deleted", "tmm_deleted", ""
	if err := db.Create(&team).Error; err != nil {
		t.Fatal("immutable Team history must not acquire a live FK", err)
	}
	for _, invalid := range []struct{ team, member, user, project, key string }{
		{"tem_x", "", "usr_x", "", ""}, {"", "tmm_x", "usr_x", "", ""},
		{"tem_x", "tmm_x", "", "", ""}, {"tem_x", "tmm_x", "usr_x", "prj_x", ""},
		{"tem_x", "tmm_x", "usr_x", "", "key_x"},
	} {
		bad := team
		bad.RequestID = "req_invalid_team_subject"
		bad.TeamID, bad.TeamMembershipID, bad.UserID, bad.ProjectID, bad.KeyID = invalid.team, invalid.member, invalid.user, invalid.project, invalid.key
		if err := db.Create(&bad).Error; err == nil {
			t.Fatal("database accepted contradictory Team/Personal/Project subject", invalid)
		}
	}
	for _, requestID := range []string{legacy.RequestID, team.RequestID} {
		if err := db.Where("request_id = ?", requestID).Delete(model).Error; err != nil {
			t.Fatal(err)
		}
	}
}
