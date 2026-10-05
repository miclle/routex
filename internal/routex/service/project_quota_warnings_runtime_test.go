package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/limits"
)

func projectWarningRuntimeFixture(t *testing.T) (*Service, *runtimeAuthorization, entity.ResourceLimit, entity.Project, limits.Policy, entity.QuotaSetting) {
	t.Helper()
	row, created, _ := projectWarningFixture()
	project := entity.Project{ID: row.ScopeID, Name: "Recorded Project", Status: entity.ResourceActive, CreatedAt: created}
	policy, err := policyFromRow(row)
	if err != nil {
		t.Fatal(err)
	}
	calendar := entity.QuotaSetting{ETag: "calendar_exact", TimeZone: "UTC", AccountingStarted: true}
	state := runtimeProjectCreationState{Project: project, Managers: map[string]string{"usr_exact": "pmg_exact"}, EnabledManagers: map[string]bool{"usr_exact": true}}
	auth := &runtimeAuthorization{ValidUntil: time.Now().Add(time.Minute), ProjectCreationStates: map[string]runtimeProjectCreationState{project.ID: state}, Quota: &runtimeQuotaData{Setting: calendar, Currency: "USD", Revisions: map[string]string{limitAccount("project", project.ID): row.ETag}}, LimitPolicies: map[string]limits.Policy{limitAccount("project", project.ID): policy}}
	s := &Service{runtime: &gatewayRuntime{}}
	s.runtime.auth.Store(auth)
	return s, auth, row, project, policy, calendar
}

func TestProjectQuotaWarningCurrentLeasedAggregateProof(t *testing.T) {
	for _, name := range []string{"valid", "stopped_leased", "nil_runtime", "scope", "target_alias", "inactive", "zero_birth", "birth", "missing_project", "project_alias", "published_id_alias", "published_inactive", "published_birth", "missing_calendar", "lease", "revision", "policy", "zone", "currency", "calendar", "zero_calendar", "accounting", "unstarted_accounting", "project_fence", "policy_fence", "calendar_fence", "cancel"} {
		t.Run(name, func(t *testing.T) {
			s, auth, row, project, policy, calendar := projectWarningRuntimeFixture(t)
			ctx := context.Background()
			state := auth.ProjectCreationStates[project.ID]
			switch name {
			case "stopped_leased":
				s.runtime.done = make(chan struct{})
				close(s.runtime.done)
			case "nil_runtime":
				s.runtime = nil
			case "scope":
				row.ScopeKind = "team"
			case "target_alias":
				row.ScopeID = strings.ToUpper(project.ID)
			case "inactive":
				project.Status = entity.ResourceArchived
			case "zero_birth":
				project.CreatedAt = time.Time{}
			case "birth":
				project.CreatedAt = project.CreatedAt.Add(time.Microsecond)
			case "missing_project":
				auth.ProjectCreationStates = nil
			case "project_alias":
				auth.ProjectCreationStates = map[string]runtimeProjectCreationState{strings.ToUpper(project.ID): state}
			case "published_id_alias":
				state.Project.ID = strings.ToUpper(project.ID)
				auth.ProjectCreationStates[project.ID] = state
			case "published_inactive":
				state.Project.Status = entity.ResourceArchived
				auth.ProjectCreationStates[project.ID] = state
			case "published_birth":
				state.Project.CreatedAt = state.Project.CreatedAt.Add(time.Microsecond)
				auth.ProjectCreationStates[project.ID] = state
			case "missing_calendar":
				auth.Quota = nil
			case "lease":
				auth.ValidUntil = time.Now().Add(-time.Second)
			case "revision":
				auth.Quota.Revisions[limitAccount("project", project.ID)] += "x"
			case "policy":
				auth.LimitPolicies[limitAccount("project", project.ID)] = limits.Policy{}
			case "zone":
				auth.Quota.Setting.TimeZone = "Etc/UTC"
			case "currency":
				auth.Quota.Currency = "EUR"
			case "calendar":
				calendar.ETag += "x"
			case "zero_calendar":
				calendar.ETag = ""
				auth.Quota.Setting.ETag = ""
			case "accounting":
				calendar.AccountingStarted = false
			case "unstarted_accounting":
				calendar.AccountingStarted = false
				auth.Quota.Setting.AccountingStarted = false
			case "project_fence":
				s.runtime.deniedProjects.Store(project.ID, true)
			case "policy_fence":
				s.runtime.deniedLimits.Store(limitAccount("project", project.ID), true)
			case "calendar_fence":
				s.runtime.deniedLimits.Store("quota_settings", true)
			case "cancel":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			want := name == "valid" || name == "stopped_leased"
			if got := s.projectQuotaWarningApplied(ctx, row, project, policy, calendar, "USD"); got != want {
				t.Fatalf("%s applied=%v want=%v", name, got, want)
			}
		})
	}
}

func TestProjectQuotaWarningRecipientAdmissionAndManagerPublication(t *testing.T) {
	for _, name := range []string{"approved", "pending", "rejected", "unknown", "user_birth", "user_alias", "application_alias", "disabled", "offboarded", "lease", "pointer", "manager", "manager_alias", "manager_empty", "manager_disabled", "missing_project", "project_alias", "inactive_project", "zero_project_birth", "creator_only", "admin_only", "user_fence", "session_fence", "project_fence"} {
		t.Run(name, func(t *testing.T) {
			s, auth, _, project, _, _ := projectWarningRuntimeFixture(t)
			u, apps := managedAdvisorySubject(entity.User{ID: "usr_exact", CreatedAt: project.CreatedAt})
			publishManagedAdvisory(auth, u, apps)
			manager := "pmg_exact"
			app := apps[*u.ApprovalApplicationID]
			state := auth.ProjectCreationStates[project.ID]
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
			case "manager":
				state.Managers[u.ID] = "pmg_new"
			case "manager_alias":
				manager = strings.ToUpper(manager)
			case "manager_empty":
				manager = ""
			case "manager_disabled":
				state.EnabledManagers[u.ID] = false
			case "missing_project":
				delete(auth.ProjectCreationStates, project.ID)
			case "project_alias":
				state.Project.ID = strings.ToUpper(project.ID)
			case "inactive_project":
				state.Project.Status = entity.ResourceArchived
			case "zero_project_birth":
				state.Project.CreatedAt = time.Time{}
			case "creator_only":
				state.Managers = nil
				state.Project.CreatorID = u.ID
			case "admin_only":
				state.Managers = nil
				u.Role = "admin"
				publishManagedAdvisory(auth, u, apps)
			case "user_fence":
				s.runtime.deniedUsers.Store(u.ID, true)
			case "session_fence":
				s.runtime.deniedSessionUsers.Store(u.ID, true)
			case "project_fence":
				s.runtime.deniedProjects.Store(project.ID, true)
			}
			if name != "missing_project" {
				auth.ProjectCreationStates[project.ID] = state
			}
			if s.projectQuotaWarningRecipientPublished(auth, u, apps, project.ID, manager) != (name == "approved") {
				t.Fatal("unreviewed recipient publication", name)
			}
		})
	}
}
