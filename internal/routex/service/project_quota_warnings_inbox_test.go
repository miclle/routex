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

func projectWarningInboxFixture() (projectQuotaWarningInboxRow, quotaInboxAccess) {
	row, birth, usage := projectWarningFixture()
	v := monthlyProjectQuotaWarnings(row, birth, usage, "USD")[1]
	v.ID = "pwo_original"
	v.ProjectName = "Recorded Project"
	recipientBirth := birth.Add(-time.Hour)
	return projectQuotaWarningInboxRow{ID: "pwi_original", RecipientID: "usr_exact", RecipientCreatedAt: recipientBirth, ObservationID: v.ID, CreatedAt: v.AsOf, CurrentProjectID: v.ProjectID, CurrentProjectCreatedAt: birth, CurrentProjectStatus: entity.ResourceActive, Observation: v}, quotaInboxAccess{ActorID: "usr_exact", ActorCreatedAt: recipientBirth, ProjectIDs: []string{v.ProjectID}}
}
func TestProjectQuotaWarningRecordedRecipientHistoryAndPrivacy(t *testing.T) {
	for _, name := range []string{"original", "rejoin", "renamed", "admin_only", "later_member", "recipient_alias", "recipient_recreated", "recipient_birth_missing", "project_recreated", "project_alias", "inactive", "level", "threshold", "generation", "amount", "currency", "observation"} {
		t.Run(name, func(t *testing.T) {
			row, access := projectWarningInboxFixture()
			read := row.CreatedAt.Add(time.Minute)
			row.ReadAt = &read
			switch name {
			case "rejoin":
				access.ProjectIDs = nil
				if validProjectQuotaWarningInbox(row, access) {
					t.Fatal("removed management leaked history")
				}
				access.ProjectIDs = []string{row.Observation.ProjectID}
			case "renamed":
				row.Observation.ProjectName = "Original historical name"
			case "admin_only":
				access.Operational = true
				access.ProjectIDs = nil
			case "later_member":
				access.ActorID = "usr_later"
			case "recipient_alias":
				row.RecipientID = strings.ToUpper(row.RecipientID)
			case "recipient_recreated":
				access.ActorCreatedAt = access.ActorCreatedAt.Add(time.Microsecond)
			case "recipient_birth_missing":
				row.RecipientCreatedAt = time.Time{}
			case "project_recreated":
				row.CurrentProjectCreatedAt = row.CurrentProjectCreatedAt.Add(time.Microsecond)
			case "project_alias":
				row.CurrentProjectID = strings.ToUpper(row.CurrentProjectID)
			case "inactive":
				row.CurrentProjectStatus = entity.ResourceArchived
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
			if validProjectQuotaWarningInbox(row, access) != want {
				t.Fatal("history owner/source boundary", name)
			}
			if want {
				record := projectQuotaWarningRecord(row)
				if record.Kind != "monthly_quota_warning" || record.QuotaWarning.ScopeKind != "project" || record.SubjectType != "project" || record.SubjectID != row.Observation.ProjectID || record.SubjectName != row.Observation.ProjectName || !record.Read || !record.ReadAt.Equal(read) || record.Severity != "high" || record.QuotaWarning.ThresholdGeneration != projectQuotaWarningGeneration || record.QuotaWarning.Settled != "0.000000000000000009" {
					t.Fatal("typed historical read changed", record)
				}
				bytes, err := json.Marshal(record)
				if err != nil {
					t.Fatal(err)
				}
				for _, private := range []string{"recipient_id", "recipient_created_at", "resource_created_at", "coverage_start", "current_project"} {
					if strings.Contains(string(bytes), private) {
						t.Fatal("private birth/recipient projection", private)
					}
				}
			}
		})
	}
}
func TestProjectQuotaWarningPortableReadMarkAllAndScanScopes(t *testing.T) {
	for _, dialect := range []gorm.Dialector{projectQuotaScopeDialector{}, personalLifecycleMySQLDialector{}} {
		t.Run(dialect.Name(), func(t *testing.T) {
			db, err := gorm.Open(dialect, &gorm.Config{DryRun: true, DisableAutomaticPing: true, SkipDefaultTransaction: true})
			if err != nil {
				t.Fatal(err)
			}
			callbacks.RegisterDefaultCallbacks(db, &callbacks.Config{})
			_, access := projectWarningInboxFixture()
			access.ProjectIDs = append(access.ProjectIDs, "prj_other")
			var rows []projectQuotaWarningInboxRow
			q := projectQuotaWarningInboxQuery(db, access).Select(projectQuotaWarningInboxSelect).Where("project_quota_warning_inboxes.read_at IS NULL").Limit(21).Find(&rows)
			if q.Error != nil {
				t.Fatal(q.Error)
			}
			sql := q.Statement.SQL.String()
			for _, required := range []string{"JOIN projects", "recipient_id", "recipient_created_at", "project.created_at = warning.resource_created_at", "threshold_generation", "dimension", "read_at IS NULL"} {
				if !strings.Contains(sql, required) {
					t.Fatal("missing conjunctive scope", required, sql)
				}
			}
			if strings.Contains(sql, "project_managers") || strings.Contains(sql, "users AS") {
				t.Fatal("private read queried unrelated directory", sql)
			}
			if dialect.Name() == "mysql" && strings.Count(sql, "AS BINARY") < 18 {
				t.Fatal("Project exact collation lost", sql)
			}
			if !strings.Contains(sql, ") AND") {
				t.Fatal("Project OR escaped recipient scope", sql)
			}
			q = projectQuotaWarningMutationQuery(db, access).Where("read_at IS NULL").Update("read_at", nil)
			if q.Error != nil {
				t.Fatal(q.Error)
			}
			for _, required := range []string{"EXISTS", "recipient_created_at", "recipient_id", "JOIN projects", "resource_created_at"} {
				if !strings.Contains(q.Statement.SQL.String(), required) {
					t.Fatal("mark-all escaped birth/current scope", required)
				}
			}
			q = projectQuotaWarningInboxQuery(db, quotaInboxAccess{ActorID: access.ActorID, ActorCreatedAt: access.ActorCreatedAt, Operational: true}).Find(&rows)
			if !strings.Contains(q.Statement.SQL.String(), "1 = 0") {
				t.Fatal("admin expanded Project private history")
			}
			var policies []entity.ResourceLimit
			q = projectQuotaWarningScanQuery(db, quotaNotificationCursor{Kind: "project", ID: "prj_031"}).Find(&policies)
			if q.Error != nil || !strings.Contains(q.Statement.SQL.String(), "scope_id >") || !strings.Contains(q.Statement.SQL.String(), "ORDER BY") || q.Statement.Clauses["LIMIT"].Expression == nil {
				t.Fatal("bounded Project scan missing")
			}
		})
	}
}
