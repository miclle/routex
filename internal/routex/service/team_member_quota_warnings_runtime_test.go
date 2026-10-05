package service

import (
	"context"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/limits"
	"strings"
	"testing"
	"time"
)

func memberWarningRuntimeFixture(t *testing.T) (*Service, *runtimeAuthorization, *teamLimitContext, entity.QuotaSetting, map[string]entity.RegistrationApprovalApplication) {
	t.Helper()
	c, _ := memberWarningFixture(t)
	user, apps := managedAdvisorySubject(c.Actor)
	c.Actor = user
	calendar := entity.QuotaSetting{ETag: "calendar_current", TimeZone: "UTC", AccountingStarted: true}
	child, parent := limitAccount("team_member", c.Row.ScopeID), limitAccount("team", c.Team.ID)
	a := &runtimeAuthorization{ValidUntil: time.Now().Add(time.Minute), Teams: map[string]runtimeTeam{c.Team.ID: {CreatedAt: c.Team.CreatedAt, Members: map[string]string{c.Actor.ID: c.Member.ID}}}, Quota: &runtimeQuotaData{Setting: calendar, Currency: "USD", Revisions: map[string]string{child: c.Row.ETag, parent: c.ParentRow.ETag}}, LimitPolicies: map[string]limits.Policy{child: c.Stored, parent: c.Parent}}
	publishManagedAdvisory(a, c.Actor, apps)
	s := &Service{runtime: &gatewayRuntime{}, recorder: &callRecorder{}}
	s.runtime.auth.Store(a)
	return s, a, c, calendar, apps
}
func TestTeamMemberQuotaWarningCompleteCurrentPairApplication(t *testing.T) {
	for _, name := range []string{"current", "owner_self", "stopped_leased", "rejoined_current", "missing_admission", "pending", "rejected", "unknown_application", "user_birth", "user_alias", "team_birth", "member_alias", "member_removed", "parent_revision", "parent_policy", "child_revision", "child_policy", "currency", "calendar", "calendar_generation", "unstarted", "accounting_mismatch", "expired", "pointer", "disabled", "offboarded", "owner_peer", "user_fence", "session_fence", "member_fence", "team_fence", "child_fence", "parent_fence", "calendar_fence", "cancel", "no_runtime", "no_recorder"} {
		t.Run(name, func(t *testing.T) {
			s, a, c, setting, apps := memberWarningRuntimeFixture(t)
			ctx := context.Background()
			id := *c.Actor.ApprovalApplicationID
			app := apps[id]
			child, parent := limitAccount("team_member", c.Row.ScopeID), limitAccount("team", c.Team.ID)
			switch name {
			case "owner_self":
				c.Member.Role = entity.TeamOwner
			case "stopped_leased":
				s.runtime.done = make(chan struct{})
				close(s.runtime.done)
			case "rejoined_current":
				c.Member.ID = "tmm_rejoined"
				a.Teams[c.Team.ID].Members[c.Actor.ID] = c.Member.ID
			case "missing_admission":
				a.UserAdmissions = nil
			case "pending":
				app.State = "pending"
				app.DecidedAt = nil
				app.DecisionActorID = nil
				app.DecisionReason = nil
				apps[id] = app
			case "rejected":
				app.State = "rejected"
				apps[id] = app
			case "unknown_application":
				apps = nil
			case "user_birth":
				c.Actor.CreatedAt = c.Actor.CreatedAt.Add(time.Millisecond)
			case "user_alias":
				c.Actor.ID = strings.ToUpper(c.Actor.ID)
			case "team_birth":
				c.Team.CreatedAt = c.Team.CreatedAt.Add(time.Millisecond)
			case "member_alias":
				c.Member.ID = strings.ToUpper(c.Member.ID)
			case "member_removed":
				team := a.Teams[c.Team.ID]
				team.Members = nil
				a.Teams[c.Team.ID] = team
			case "parent_revision":
				a.Quota.Revisions[parent] += "x"
			case "parent_policy":
				a.LimitPolicies[parent] = limits.Policy{}
			case "child_revision":
				a.Quota.Revisions[child] += "x"
			case "child_policy":
				a.LimitPolicies[child] = limits.Policy{}
			case "currency":
				a.Quota.Currency = "EUR"
			case "calendar":
				setting.TimeZone = "Etc/UTC"
			case "calendar_generation":
				setting.ETag += "x"
			case "unstarted":
				setting.AccountingStarted = false
				a.Quota.Setting.AccountingStarted = false
			case "accounting_mismatch":
				a.Quota.Setting.AccountingStarted = false
			case "expired":
				a.ValidUntil = time.Now().Add(-time.Second)
			case "pointer":
				other := *a
				s.runtime.auth.Store(&other)
			case "disabled":
				c.Actor.Disabled = true
			case "offboarded":
				c.Actor.OffboardedAt = &c.Actor.CreatedAt
			case "owner_peer":
				c.Actor.ID = "usr_owner"
			case "user_fence":
				s.runtime.deniedUsers.Store(c.Actor.ID, true)
			case "session_fence":
				s.runtime.deniedSessionUsers.Store(c.Actor.ID, true)
			case "member_fence":
				s.runtime.deniedTeamMembers.Store(teamMemberRuntimeKey(c.Team.ID, c.Actor.ID), true)
			case "team_fence":
				s.runtime.deniedTeams.Store(c.Team.ID, true)
			case "child_fence":
				s.runtime.deniedLimits.Store(child, true)
			case "parent_fence":
				s.runtime.deniedLimits.Store(parent, true)
			case "calendar_fence":
				s.runtime.deniedLimits.Store("quota_settings", true)
			case "cancel":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "no_runtime":
				s.runtime = nil
			case "no_recorder":
				s.recorder = nil
			}
			want := name == "current" || name == "owner_self" || name == "stopped_leased" || name == "rejoined_current"
			if got := s.teamMemberQuotaWarningApplied(ctx, a, c, setting, apps); got != want {
				t.Fatal("complete current pair proof", name, got, want)
			}
		})
	}
}
