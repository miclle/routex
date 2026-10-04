package service

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
)

func TestTeamCallExportScopeIsExactOwnActorAndTeam(t *testing.T) {
	for _, dialect := range []gorm.Dialector{projectQuotaScopeDialector{}, personalLifecycleMySQLDialector{}} {
		for _, actor := range []string{"usr_exact", "USR_EXACT", "usr_exact "} {
			t.Run(dialect.Name()+"/"+actor, func(t *testing.T) {
				db, err := gorm.Open(dialect, &gorm.Config{DryRun: true, DisableAutomaticPing: true})
				if err != nil {
					t.Fatal(err)
				}
				query := teamCallExportScope(db.Model(&entity.CallRecord{}), actor, "tea_exact")
				query.Statement.Clauses["WHERE"].Build(query.Statement)
				expected := `WHERE "user_id" = ? AND "team_id" = ?`
				if dialect.Name() == "mysql" {
					expected = `WHERE CAST("user_id" AS BINARY) = CAST(? AS BINARY) AND CAST("team_id" AS BINARY) = CAST(? AS BINARY)`
				}
				if query.Statement.SQL.String() != expected || !slices.Equal(query.Statement.Vars, []any{actor, "tea_exact"}) {
					t.Fatal("Team export must bind exact actor and selected Team instead of Personal scope", query.Statement.SQL.String(), query.Statement.Vars)
				}
			})
		}
	}
}

func TestTeamCallExportRejectsScopeAndPageExpansionBeforeDatabase(t *testing.T) {
	svc := &Service{}
	from, to := time.Now(), time.Now().Add(-time.Hour)
	for _, filter := range []CallFilter{{Cursor: "next"}, {Limit: 1}, {KeyID: "key_other"}, {UserID: "usr_other"}, {ProjectID: "prj_other"}, {TeamID: "tea_other"}, {Status: "unknown"}, {ModelID: "mdl_one "}, {From: &from, To: &to}} {
		t.Run("forbidden", func(t *testing.T) {
			if _, err := svc.ExportTeamCalls(context.Background(), "usr_one", "tea_one", filter); err == nil {
				t.Fatal("foreign or page selector accepted")
			}
		})
	}
}

func TestTeamCallExportCancellationNeverReturnsAFile(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := (&Service{}).ExportTeamCalls(ctx, "usr_one", "tea_one", CallFilter{})
	if result != nil || !errors.Is(err, callExportUnavailable) {
		t.Fatal("canceled export returned file or lost bounded error", result, err)
	}
}
