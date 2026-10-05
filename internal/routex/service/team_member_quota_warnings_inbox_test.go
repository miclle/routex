package service

import (
	"encoding/json"
	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
	"gorm.io/gorm/callbacks"
	"strings"
	"testing"
	"time"
)

func memberWarningInboxFixture(t *testing.T) (teamMemberQuotaWarningInboxRow, quotaInboxAccess) {
	t.Helper()
	c, u := memberWarningFixture(t)
	v := monthlyTeamMemberQuotaWarnings(c, u)[1]
	v.ID = "mwo_original"
	return teamMemberQuotaWarningInboxRow{ID: "mwi_original", RecipientID: c.Actor.ID, RecipientCreatedAt: c.Actor.CreatedAt, ObservationID: v.ID, CreatedAt: v.AsOf, CurrentTeamID: c.Team.ID, CurrentTeamCreatedAt: c.Team.CreatedAt, CurrentTeamStatus: entity.ResourceActive, Observation: v}, quotaInboxAccess{ActorID: c.Actor.ID, ActorCreatedAt: c.Actor.CreatedAt, TeamIDs: []string{c.Team.ID}}
}
func TestTeamMemberQuotaWarningPrivateSelfHistoryAndBirths(t *testing.T) {
	for _, name := range []string{"self", "rejoin", "equal_time_zones", "historical_currency", "owner", "peer", "admin", "removed", "later_member", "recipient_alias", "recipient_recreated", "observation_birth", "team_recreated", "team_alias", "wrong_digest", "wrong_private_team", "wrong_private_user", "missing_birth", "inactive", "level", "threshold", "generation", "invalid_amount", "observation"} {
		t.Run(name, func(t *testing.T) {
			row, access := memberWarningInboxFixture(t)
			read := row.CreatedAt.Add(time.Minute)
			row.ReadAt = &read
			switch name {
			case "rejoin":
				access.TeamIDs = nil
				if validTeamMemberQuotaWarningInbox(row, access) {
					t.Fatal("removed membership leaked private history")
				}
				access.TeamIDs = []string{row.Observation.TeamID}
			case "equal_time_zones":
				access.ActorCreatedAt = access.ActorCreatedAt.In(time.FixedZone("test offset", 8*3600))
				row.CurrentTeamCreatedAt = row.CurrentTeamCreatedAt.In(time.FixedZone("second offset", -4*3600))
			case "historical_currency":
				row.Observation.Currency = "EUR"
			case "owner":
				access.ActorID = "usr_owner"
			case "peer":
				access.ActorID = "usr_peer"
			case "admin":
				access.Operational = true
				access.TeamIDs = nil
			case "removed":
				access.TeamIDs = nil
			case "later_member":
				access.ActorID = "usr_later"
			case "recipient_alias":
				row.RecipientID = strings.ToUpper(row.RecipientID)
			case "recipient_recreated":
				access.ActorCreatedAt = access.ActorCreatedAt.Add(time.Millisecond)
			case "observation_birth":
				row.Observation.UserCreatedAt = row.Observation.UserCreatedAt.Add(time.Millisecond)
			case "team_recreated":
				row.CurrentTeamCreatedAt = row.CurrentTeamCreatedAt.Add(time.Millisecond)
			case "team_alias":
				row.CurrentTeamID = strings.ToUpper(row.CurrentTeamID)
			case "wrong_digest":
				row.Observation.ScopeID = teamMemberLimitScopeID(row.Observation.TeamID, "usr_other")
			case "wrong_private_team":
				row.Observation.TeamID = "tem_other"
			case "wrong_private_user":
				row.Observation.MemberUserID = "usr_owner"
			case "missing_birth":
				row.RecipientCreatedAt = time.Time{}
			case "inactive":
				row.CurrentTeamStatus = entity.ResourceArchived
			case "level":
				row.Observation.Level = "near"
			case "threshold":
				row.Observation.Threshold = 80
			case "generation":
				row.Observation.ThresholdGeneration = teamQuotaWarningGeneration
			case "invalid_amount":
				row.Observation.Settled = "1e2"
			case "observation":
				row.ObservationID = strings.ToUpper(row.ObservationID)
			}
			want := name == "self" || name == "rejoin" || name == "equal_time_zones" || name == "historical_currency"
			if validTeamMemberQuotaWarningInbox(row, access) != want {
				t.Fatal("self/pair/history authority", name)
			}
			if want {
				record := teamMemberQuotaWarningRecord(row)
				if record.SubjectType != "team_member" || record.SubjectID != row.Observation.ScopeID || record.SubjectName != row.Observation.TeamName || !record.Read || !record.ReadAt.Equal(read) || record.QuotaWarning.ScopeKind != "team_member" || record.QuotaWarning.ThresholdGeneration != teamMemberQuotaWarningGeneration {
					t.Fatal("typed self history lost")
				}
				raw, err := json.Marshal(record)
				if err != nil {
					t.Fatal(err)
				}
				for _, private := range []string{"team_id", "member_user_id", "membership_id", "recipient_id", "user_created_at", "recipient_created_at", "resource_created_at", "coverage_start", "current_team"} {
					if strings.Contains(string(raw), private) {
						t.Fatal("public warning leaked private tuple/birth", private)
					}
				}
				var dto map[string]any
				if err = json.Unmarshal(raw, &dto); err != nil {
					t.Fatal(err)
				}
				if len(dto["quota_warning"].(map[string]any)) != 14 {
					t.Fatal("public warning shape grew")
				}
			}
		})
	}
}
func TestTeamMemberQuotaWarningPortableSelfReadMarkAndETagMapping(t *testing.T) {
	for _, dialect := range []gorm.Dialector{projectQuotaScopeDialector{}, personalLifecycleMySQLDialector{}} {
		t.Run(dialect.Name(), func(t *testing.T) {
			db, err := gorm.Open(dialect, &gorm.Config{DryRun: true, DisableAutomaticPing: true, SkipDefaultTransaction: true})
			if err != nil {
				t.Fatal(err)
			}
			callbacks.RegisterDefaultCallbacks(db, &callbacks.Config{})
			_, access := memberWarningInboxFixture(t)
			access.TeamIDs = append(access.TeamIDs, "tem_second")
			var rows []teamMemberQuotaWarningInboxRow
			q := teamMemberQuotaWarningInboxQuery(db, access).Select(teamMemberQuotaWarningInboxSelect).Limit(21).Find(&rows)
			if q.Error != nil {
				t.Fatal(q.Error)
			}
			sql := q.Statement.SQL.String()
			for _, required := range []string{"JOIN teams", "recipient_id", "recipient_created_at", "member_user_id", "user_created_at", "scope_id", "team.created_at = warning.resource_created_at", "threshold_generation"} {
				if !strings.Contains(sql, required) {
					t.Fatal("missing conjunctive private scope", required)
				}
			}
			if strings.Contains(sql, "team_memberships") || strings.Contains(sql, "users AS") {
				t.Fatal("history queried unrelated directory")
			}
			if dialect.Name() == "mysql" && strings.Count(sql, "AS BINARY") < 24 {
				t.Fatal("exact tuple collation lost")
			}
			q = teamMemberQuotaWarningMutationQuery(db, access).Where("read_at IS NULL").Update("read_at", nil)
			for _, required := range []string{"EXISTS", "recipient_created_at", "member_user_id", "user_created_at", "JOIN teams", "resource_created_at", "scope_id"} {
				if !strings.Contains(q.Statement.SQL.String(), required) {
					t.Fatal("mark-all escaped self birth", required)
				}
			}
			q = teamMemberQuotaWarningInboxQuery(db, quotaInboxAccess{ActorID: access.ActorID, ActorCreatedAt: access.ActorCreatedAt, Operational: true}).Find(&rows)
			if !strings.Contains(q.Statement.SQL.String(), "1 = 0") {
				t.Fatal("admin gained member history")
			}
			for _, model := range []any{&entity.ResourceLimit{}, &entity.QuotaSetting{}} {
				q = db.Model(model).Where("1 = 1").UpdateColumn("ETag", "reviewed")
				if q.Error != nil || !strings.Contains(q.Statement.SQL.String(), "e_tag") || strings.Contains(q.Statement.SQL.String(), "\"etag\"") || strings.Contains(q.Statement.SQL.String(), "`etag`") {
					t.Fatal("GORM ETag field mapped to guessed column", q.Error, q.Statement.SQL.String())
				}
			}
		})
	}
}
