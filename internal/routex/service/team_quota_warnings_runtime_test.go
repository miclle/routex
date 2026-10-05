package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/limits"
)

func teamWarningRuntimeFixture(t *testing.T) (*Service, *runtimeAuthorization, entity.ResourceLimit, entity.Team, limits.Policy, entity.QuotaSetting) {
	t.Helper()
	row, created, _ := teamWarningFixture()
	team := entity.Team{ID: row.ScopeID, Name: "Recorded Team", Status: entity.ResourceActive, CreatedAt: created}
	policy, err := policyFromRow(row)
	if err != nil {
		t.Fatal(err)
	}
	calendar := entity.QuotaSetting{ETag: "calendar_exact", TimeZone: "UTC", AccountingStarted: true}
	auth := &runtimeAuthorization{ValidUntil: time.Now().Add(time.Minute), Teams: map[string]runtimeTeam{team.ID: {CreatedAt: created, Members: map[string]string{"usr_exact": "tmb_exact"}}}, Quota: &runtimeQuotaData{Setting: calendar, Currency: "USD", Revisions: map[string]string{limitAccount("team", team.ID): row.ETag}}, LimitPolicies: map[string]limits.Policy{limitAccount("team", team.ID): policy}}
	s := &Service{runtime: &gatewayRuntime{}}
	s.runtime.auth.Store(auth)
	return s, auth, row, team, policy, calendar
}
func TestTeamQuotaWarningCurrentLeasedAggregateProof(t *testing.T) {
	for _, name := range []string{"valid", "stopped_leased", "nil_runtime", "scope", "target_alias", "inactive", "zero_birth", "birth", "missing_team", "team_alias", "lease", "revision", "policy", "zone", "currency", "calendar", "accounting", "team_fence", "policy_fence", "calendar_fence", "cancel"} {
		t.Run(name, func(t *testing.T) {
			s, auth, row, team, policy, calendar := teamWarningRuntimeFixture(t)
			ctx := context.Background()
			switch name {
			case "stopped_leased":
				s.runtime.done = make(chan struct{})
				close(s.runtime.done)
			case "nil_runtime":
				s.runtime = nil
			case "scope":
				row.ScopeKind = "team_member"
			case "target_alias":
				row.ScopeID = strings.ToUpper(team.ID)
			case "inactive":
				team.Status = entity.ResourceArchived
			case "zero_birth":
				team.CreatedAt = time.Time{}
			case "birth":
				team.CreatedAt = team.CreatedAt.Add(time.Microsecond)
			case "missing_team":
				auth.Teams = nil
			case "team_alias":
				auth.Teams = map[string]runtimeTeam{strings.ToUpper(team.ID): {CreatedAt: team.CreatedAt}}
			case "lease":
				auth.ValidUntil = time.Now().Add(-time.Second)
			case "revision":
				auth.Quota.Revisions[limitAccount("team", team.ID)] += "x"
			case "policy":
				auth.LimitPolicies[limitAccount("team", team.ID)] = limits.Policy{}
			case "zone":
				auth.Quota.Setting.TimeZone = "Etc/UTC"
			case "currency":
				auth.Quota.Currency = "EUR"
			case "calendar":
				calendar.ETag += "x"
			case "accounting":
				calendar.AccountingStarted = false
			case "team_fence":
				s.runtime.deniedTeams.Store(team.ID, true)
			case "policy_fence":
				s.runtime.deniedLimits.Store(limitAccount("team", team.ID), true)
			case "calendar_fence":
				s.runtime.deniedLimits.Store("quota_settings", true)
			case "cancel":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			want := name == "valid" || name == "stopped_leased"
			if got := s.teamQuotaWarningApplied(ctx, row, team, policy, calendar, "USD"); got != want {
				t.Fatalf("%s applied=%v want=%v", name, got, want)
			}
		})
	}
}
func TestTeamQuotaWarningRecipientAdmissionAndMembershipPublication(t *testing.T) {
	for _, name := range []string{"approved", "pending", "rejected", "unknown", "user_birth", "user_alias", "application_alias", "disabled", "offboarded", "lease", "pointer", "membership", "membership_alias", "membership_empty", "user_fence", "session_fence", "membership_fence"} {
		t.Run(name, func(t *testing.T) {
			s, auth, _, team, _, _ := teamWarningRuntimeFixture(t)
			u, apps := managedAdvisorySubject(entity.User{ID: "usr_exact", CreatedAt: team.CreatedAt})
			publishManagedAdvisory(auth, u, apps)
			member := "tmb_exact"
			app := apps[*u.ApprovalApplicationID]
			switch name {
			case "pending":
				app.State = "pending"
				app.DecidedAt = nil
				app.DecisionActorID = nil
				app.DecisionReason = nil
				apps[app.ID] = app
			case "rejected":
				app.State = "rejected"
				apps[app.ID] = app
			case "unknown":
				apps = nil
			case "user_birth":
				u.CreatedAt = u.CreatedAt.Add(time.Microsecond)
			case "user_alias":
				u.ID = strings.ToUpper(u.ID)
			case "application_alias":
				app.UserID = strings.ToUpper(u.ID)
				apps[app.ID] = app
			case "disabled":
				u.Disabled = true
			case "offboarded":
				u.OffboardedAt = &u.CreatedAt
			case "lease":
				auth.ValidUntil = time.Now().Add(-time.Second)
			case "pointer":
				s.runtime.auth.Store(&runtimeAuthorization{})
			case "membership":
				rt := auth.Teams[team.ID]
				rt.Members[u.ID] = "tmb_new"
				auth.Teams[team.ID] = rt
			case "membership_alias":
				member = strings.ToUpper(member)
			case "membership_empty":
				member = ""
			case "user_fence":
				s.runtime.deniedUsers.Store(u.ID, true)
			case "session_fence":
				s.runtime.deniedSessionUsers.Store(u.ID, true)
			case "membership_fence":
				s.runtime.deniedTeamMembers.Store(teamMemberRuntimeKey(team.ID, u.ID), true)
			}
			if s.teamQuotaWarningRecipientPublished(auth, u, apps, team.ID, member) != (name == "approved") {
				t.Fatal("unreviewed recipient publication", name)
			}
		})
	}
}
