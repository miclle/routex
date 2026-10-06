package service

import (
	"context"
	"fmt"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/limits"
	"strings"
	"testing"
	"time"
)

func projectKeyRuntimeFixture(t *testing.T) (*Service, *runtimeAuthorization, entity.Project, []entity.ProjectKey, entity.ResourceLimit, limits.Policy, entity.QuotaSetting) {
	t.Helper()
	row, root, project, _ := projectKeyWarningFixture()
	policy, err := policyFromRow(row)
	if err != nil {
		t.Fatal(err)
	}
	calendar := entity.QuotaSetting{ETag: "calendar_exact", AccountingStarted: true, TimeZone: "UTC"}
	a := &runtimeAuthorization{ValidUntil: time.Now().Add(time.Minute), ProjectCreationStates: map[string]runtimeProjectCreationState{project.ID: {Project: project, Eligible: true}}, LimitRoots: map[string]string{root.ID: root.ID}, KeysByID: map[string]runtimeKey{root.ID: {ProjectID: project.ID, Key: entity.APIKey{ID: root.ID, Status: root.Status, CreatedAt: root.CreatedAt}}}, Quota: &runtimeQuotaData{Setting: calendar, Currency: "USD", Created: map[string]time.Time{limitAccount("key", root.ID): root.CreatedAt}, Revisions: map[string]string{limitAccount("key", root.ID): row.ETag}}, LimitPolicies: map[string]limits.Policy{limitAccount("key", root.ID): policy}}
	s := &Service{runtime: &gatewayRuntime{done: make(chan struct{})}}
	s.runtime.auth.Store(a)
	return s, a, project, []entity.ProjectKey{root}, row, policy, calendar
}
func TestProjectKeyWarningCurrentCompleteRotationPublication(t *testing.T) {
	for _, name := range []string{"valid", "revoked_live_successor", "root_tombstone_live_successor", "revoked_only", "missing_live", "expired", "key_tombstone", "missing_root", "missing_parent", "cross_project", "cycle", "alias", "project_birth", "project_disabled", "project_archived", "published_project_birth", "published_project_alias", "no_eligible_manager", "published_birth", "published_root", "published_active_birth", "published_active_expiry", "published_active_status", "published_owner", "personal_key", "policy", "revision", "calendar", "zone", "currency", "accounting", "project_tombstone", "limit_tombstone", "calendar_tombstone", "lease", "pointer", "stopped", "nil_done", "cancel"} {
		t.Run(name, func(t *testing.T) {
			s, a, p, keys, row, policy, c := projectKeyRuntimeFixture(t)
			root := keys[0]
			ctx := context.Background()
			want := name == "valid" || name == "revoked_live_successor" || name == "root_tombstone_live_successor"
			if name == "revoked_live_successor" || name == "root_tombstone_live_successor" || name == "missing_parent" || name == "cross_project" || name == "cycle" {
				keys[0].Status = entity.KeyRevoked
				delete(a.KeysByID, root.ID)
				child := root
				child.ID = "pky_00000000000000000000000002"
				child.ReplacesKeyID = &root.ID
				child.CreatedAt = root.CreatedAt.Add(time.Millisecond)
				keys = append(keys, child)
				a.KeysByID[child.ID] = runtimeKey{ProjectID: p.ID, Key: entity.APIKey{ID: child.ID, Status: child.Status, CreatedAt: child.CreatedAt}}
				a.LimitRoots[child.ID] = root.ID
				a.Quota.Created[limitAccount("key", child.ID)] = child.CreatedAt
			}
			switch name {
			case "revoked_only":
				keys[0].Status = entity.KeyRevoked
			case "missing_live":
				delete(a.KeysByID, root.ID)
			case "expired":
				d := time.Now().Add(-time.Second)
				keys[0].ExpiresAt = &d
			case "key_tombstone", "root_tombstone_live_successor":
				s.runtime.deniedKeys.Store(root.ID, true)
			case "missing_root":
				keys = nil
			case "missing_parent":
				keys = keys[1:]
			case "cross_project":
				keys[1].ProjectID = "prj_other"
			case "cycle":
				keys[0].ReplacesKeyID = &keys[1].ID
			case "alias":
				keys[0].ID = strings.ToUpper(root.ID)
			case "project_birth":
				p.CreatedAt = p.CreatedAt.Add(time.Millisecond)
			case "project_disabled":
				p.Status = entity.ResourceDisabled
			case "project_archived":
				p.Status = entity.ResourceArchived
			case "published_project_birth", "published_project_alias", "no_eligible_manager":
				v := a.ProjectCreationStates[p.ID]
				if name == "published_project_birth" {
					v.Project.CreatedAt = v.Project.CreatedAt.Add(time.Millisecond)
				}
				if name == "published_project_alias" {
					v.Project.ID = strings.ToUpper(p.ID)
				}
				if name == "no_eligible_manager" {
					v.Eligible = false
				}
				a.ProjectCreationStates[p.ID] = v
			case "published_birth":
				a.Quota.Created[limitAccount("key", root.ID)] = root.CreatedAt.Add(time.Millisecond)
			case "published_root":
				a.LimitRoots[root.ID] = "pky_other"
			case "published_active_birth", "published_active_expiry", "published_active_status", "published_owner", "personal_key":
				v := a.KeysByID[root.ID]
				switch name {
				case "published_active_birth":
					v.Key.CreatedAt = v.Key.CreatedAt.Add(time.Millisecond)
				case "published_active_expiry":
					d := time.Now().Add(time.Hour)
					v.Key.ExpiresAt = &d
				case "published_active_status":
					v.Key.Status = entity.KeyPending
				case "published_owner":
					v.ProjectID = "prj_other"
				case "personal_key":
					v.Key.UserID = "usr_other"
				}
				a.KeysByID[root.ID] = v
			case "policy":
				a.LimitPolicies[limitAccount("key", root.ID)] = limits.Policy{}
			case "revision":
				a.Quota.Revisions[limitAccount("key", root.ID)] += "x"
			case "calendar":
				c.ETag += "x"
			case "zone":
				c.TimeZone = "Etc/UTC"
			case "currency":
				a.Quota.Currency = "EUR"
			case "accounting":
				c.AccountingStarted = false
			case "project_tombstone":
				s.runtime.deniedProjects.Store(p.ID, true)
			case "limit_tombstone":
				s.runtime.deniedLimits.Store(limitAccount("key", root.ID), true)
			case "calendar_tombstone":
				s.runtime.deniedLimits.Store("quota_settings", true)
			case "lease":
				a.ValidUntil = time.Now().Add(-time.Second)
			case "pointer":
				s.runtime.auth.Store(&runtimeAuthorization{})
			case "stopped":
				close(s.runtime.done)
			case "nil_done":
				s.runtime.done = nil
			case "cancel":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			if got := s.projectKeyWarningApplied(ctx, a, p, keys, root.ID, row, policy, c, "USD"); got != want {
				t.Fatalf("%s=%v want=%v", name, got, want)
			}
		})
	}
}
func TestProjectKeyWarningCompleteGraphBoundaries(t *testing.T) {
	for _, n := range []int{500, 501, 1000, 10000, 10001} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			_, root, project, _ := projectKeyWarningFixture()
			keys := make([]entity.ProjectKey, n)
			for i := range keys {
				keys[i] = root
				keys[i].ID = fmt.Sprintf("pky_%026d", i)
				if i > 0 {
					keys[i].ReplacesKeyID = &keys[i-1].ID
				}
			}
			start := time.Now()
			roots, ok := projectKeyWarningRoots(keys, project)
			if ok != (n <= 10000) || ok && (len(roots) != n || roots[keys[n-1].ID] != keys[0].ID) {
				t.Fatal("complete graph truncated", n)
			}
			t.Logf("bounded graph %d rows: %s", n, time.Since(start))
			if time.Since(start) > 3*time.Second {
				t.Fatal("source graph exceeded refresh deadline")
			}
		})
	}
}
