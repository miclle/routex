package service

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
	"gorm.io/gorm/callbacks"
)

func teamWarningInboxFixture() (teamQuotaWarningInboxRow, quotaInboxAccess) {
	row, birth, usage := teamWarningFixture()
	v := monthlyTeamQuotaWarnings(row, birth, usage, "USD")[1]
	v.ID = "two_original"
	v.TeamName = "Recorded Team"
	recipientBirth := birth.Add(-time.Hour)
	return teamQuotaWarningInboxRow{ID: "twi_original", RecipientID: "usr_exact", RecipientCreatedAt: recipientBirth, ObservationID: v.ID, CreatedAt: v.AsOf, CurrentTeamID: v.TeamID, CurrentTeamCreatedAt: birth, CurrentTeamStatus: entity.ResourceActive, Observation: v}, quotaInboxAccess{ActorID: "usr_exact", ActorCreatedAt: recipientBirth, TeamIDs: []string{v.TeamID}}
}
func TestTeamQuotaWarningRecordedRecipientHistoryAndPrivacy(t *testing.T) {
	for _, name := range []string{"original", "rejoin", "renamed", "admin_only", "later_member", "recipient_alias", "recipient_recreated", "recipient_birth_missing", "team_recreated", "team_alias", "inactive", "level", "threshold", "generation", "amount", "currency", "observation"} {
		t.Run(name, func(t *testing.T) {
			row, access := teamWarningInboxFixture()
			read := row.CreatedAt.Add(time.Minute)
			row.ReadAt = &read
			switch name {
			case "rejoin":
				access.TeamIDs = nil
				if validTeamQuotaWarningInbox(row, access) {
					t.Fatal("removed membership leaked history")
				}
				access.TeamIDs = []string{row.Observation.TeamID}
			case "renamed":
				row.Observation.TeamName = "Original historical name"
			case "admin_only":
				access.Operational = true
				access.TeamIDs = nil
			case "later_member":
				access.ActorID = "usr_later"
			case "recipient_alias":
				row.RecipientID = strings.ToUpper(row.RecipientID)
			case "recipient_recreated":
				access.ActorCreatedAt = access.ActorCreatedAt.Add(time.Microsecond)
			case "recipient_birth_missing":
				row.RecipientCreatedAt = time.Time{}
			case "team_recreated":
				row.CurrentTeamCreatedAt = row.CurrentTeamCreatedAt.Add(time.Microsecond)
			case "team_alias":
				row.CurrentTeamID = strings.ToUpper(row.CurrentTeamID)
			case "inactive":
				row.CurrentTeamStatus = entity.ResourceArchived
			case "level":
				row.Observation.Level = "near"
			case "threshold":
				row.Observation.Threshold = 80
			case "generation":
				row.Observation.ThresholdGeneration = quotaWarningGeneration
			case "amount":
				row.Observation.Settled = "1e2"
			case "currency":
				row.Observation.Currency = ""
			case "observation":
				row.ObservationID = strings.ToUpper(row.ObservationID)
			}
			want := name == "original" || name == "rejoin" || name == "renamed"
			if validTeamQuotaWarningInbox(row, access) != want {
				t.Fatal("history owner/source boundary", name)
			}
			if want {
				record := teamQuotaWarningRecord(row)
				if record.Kind != "monthly_quota_warning" || record.QuotaWarning.ScopeKind != "team" || record.SubjectType != "team" || record.SubjectID != row.Observation.TeamID || record.SubjectName != row.Observation.TeamName || !record.Read || !record.ReadAt.Equal(read) || record.Severity != "high" || record.QuotaWarning.ThresholdGeneration != teamQuotaWarningGeneration || record.QuotaWarning.Settled != "0.000000000000000009" {
					t.Fatal("typed historical read changed", record)
				}
				bytes, err := json.Marshal(record)
				if err != nil {
					t.Fatal(err)
				}
				for _, private := range []string{"recipient_id", "recipient_created_at", "resource_created_at", "coverage_start", "current_team"} {
					if strings.Contains(string(bytes), private) {
						t.Fatal("private birth/recipient projection", private)
					}
				}
			}
		})
	}
}
func TestTeamQuotaWarningPortableReadMarkAllAndScanScopes(t *testing.T) {
	for _, dialect := range []gorm.Dialector{projectQuotaScopeDialector{}, personalLifecycleMySQLDialector{}} {
		t.Run(dialect.Name(), func(t *testing.T) {
			db, err := gorm.Open(dialect, &gorm.Config{DryRun: true, DisableAutomaticPing: true, SkipDefaultTransaction: true})
			if err != nil {
				t.Fatal(err)
			}
			callbacks.RegisterDefaultCallbacks(db, &callbacks.Config{})
			_, access := teamWarningInboxFixture()
			access.TeamIDs = append(access.TeamIDs, "tem_other")
			var rows []teamQuotaWarningInboxRow
			q := teamQuotaWarningInboxQuery(db, access).Select(teamQuotaWarningInboxSelect).Where("team_quota_warning_inboxes.read_at IS NULL").Limit(21).Find(&rows)
			if q.Error != nil {
				t.Fatal(q.Error)
			}
			sql := q.Statement.SQL.String()
			for _, required := range []string{"JOIN teams", "recipient_id", "recipient_created_at", "team.created_at = warning.resource_created_at", "threshold_generation", "dimension", "read_at IS NULL"} {
				if !strings.Contains(sql, required) {
					t.Fatal("missing conjunctive scope", required, sql)
				}
			}
			if strings.Contains(sql, "project_managers") || strings.Contains(sql, "users AS") {
				t.Fatal("private read queried unrelated directory", sql)
			}
			if dialect.Name() == "mysql" && strings.Count(sql, "AS BINARY") < 18 {
				t.Fatal("Team exact collation lost", sql)
			}
			if !strings.Contains(sql, ") AND") {
				t.Fatal("Team OR escaped recipient scope", sql)
			}
			q = teamQuotaWarningMutationQuery(db, access).Where("read_at IS NULL").Update("read_at", nil)
			if q.Error != nil {
				t.Fatal(q.Error)
			}
			for _, required := range []string{"EXISTS", "recipient_created_at", "recipient_id", "JOIN teams", "resource_created_at"} {
				if !strings.Contains(q.Statement.SQL.String(), required) {
					t.Fatal("mark-all escaped birth/current scope", required)
				}
			}
			q = teamQuotaWarningInboxQuery(db, quotaInboxAccess{ActorID: access.ActorID, ActorCreatedAt: access.ActorCreatedAt, Operational: true}).Find(&rows)
			if !strings.Contains(q.Statement.SQL.String(), "1 = 0") {
				t.Fatal("admin expanded Team private history")
			}
			var policies []entity.ResourceLimit
			q = teamQuotaWarningScanQuery(db, quotaNotificationCursor{Kind: "team", ID: "tem_031"}).Find(&policies)
			if q.Error != nil || !strings.Contains(q.Statement.SQL.String(), "scope_id >") || !strings.Contains(q.Statement.SQL.String(), "ORDER BY") || q.Statement.Clauses["LIMIT"].Expression == nil {
				t.Fatal("bounded Team scan missing")
			}
		})
	}
}
