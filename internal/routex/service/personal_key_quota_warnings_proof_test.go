package service

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/limits"
)

func personalKeyRuntimeFixture(t *testing.T) (*Service, *runtimeAuthorization, entity.User, map[string]entity.RegistrationApprovalApplication, []entity.APIKey, entity.ResourceLimit, limits.Policy, entity.QuotaSetting) {
	t.Helper()
	row, root, owner, _ := personalKeyWarningFixture()
	owner, apps := managedAdvisorySubject(owner)
	policy, err := policyFromRow(row)
	if err != nil {
		t.Fatal(err)
	}
	calendar := entity.QuotaSetting{ETag: "calendar_exact", AccountingStarted: true, TimeZone: "UTC"}
	auth := &runtimeAuthorization{ValidUntil: time.Now().Add(time.Minute), PersonalKeyStates: runtimeMemberKeyStates([]entity.APIKey{root}), LimitRoots: map[string]string{root.ID: root.ID}, KeysByID: map[string]runtimeKey{root.ID: {Key: root}}, Quota: &runtimeQuotaData{Setting: calendar, Currency: "USD", Created: map[string]time.Time{limitAccount("key", root.ID): root.CreatedAt}, Revisions: map[string]string{limitAccount("key", root.ID): row.ETag}}, LimitPolicies: map[string]limits.Policy{limitAccount("key", root.ID): policy}}
	publishManagedAdvisory(auth, owner, apps)
	s := &Service{runtime: &gatewayRuntime{done: make(chan struct{})}}
	s.runtime.auth.Store(auth)
	return s, auth, owner, apps, []entity.APIKey{root}, row, policy, calendar
}
func TestPersonalKeyWarningCurrentCompleteRotationPublication(t *testing.T) {
	for _, name := range []string{"valid", "revoked_live_successor", "root_revoked_only", "no_live", "expired", "key_tombstone", "root_tombstone_live_successor", "missing_root", "missing_parent", "cross_owner", "cycle", "alias", "published_birth", "published_root", "published_revision", "published_status", "published_owner", "published_active_birth", "published_active_expiry", "project_key", "pending_owner", "rejected_owner", "missing_application", "owner_birth", "disabled_owner", "offboarded_owner", "policy", "revision", "calendar", "zone", "currency", "accounting", "limit_tombstone", "calendar_tombstone", "owner_tombstone", "grant_tombstone", "lease", "pointer", "stopped", "nil_done", "cancel"} {
		t.Run(name, func(t *testing.T) {
			s, a, u, apps, keys, row, policy, c := personalKeyRuntimeFixture(t)
			root := keys[0]
			ctx := context.Background()
			want := name == "valid" || name == "revoked_live_successor" || name == "root_tombstone_live_successor"
			if name == "revoked_live_successor" || name == "root_tombstone_live_successor" || name == "missing_parent" || name == "cross_owner" || name == "cycle" {
				keys[0].Status = entity.KeyRevoked
				delete(a.KeysByID, root.ID)
				child := root
				child.ID = "key_00000000000000000000000002"
				child.ReplacesKeyID = &root.ID
				child.CreatedAt = root.CreatedAt.Add(time.Millisecond)
				keys = append(keys, child)
				a.KeysByID[child.ID] = runtimeKey{Key: child}
				a.LimitRoots[child.ID] = root.ID
				a.Quota.Created[limitAccount("key", child.ID)] = child.CreatedAt
				a.PersonalKeyStates = runtimeMemberKeyStates(keys)
			}
			switch name {
			case "root_revoked_only":
				keys[0].Status = entity.KeyRevoked
				a.PersonalKeyStates = runtimeMemberKeyStates(keys)
			case "no_live":
				delete(a.KeysByID, root.ID)
			case "expired":
				expired := time.Now().Add(-time.Second)
				keys[0].ExpiresAt = &expired
			case "key_tombstone":
				s.runtime.deniedKeys.Store(root.ID, true)
			case "root_tombstone_live_successor":
				s.runtime.deniedKeys.Store(root.ID, true)
			case "missing_root":
				keys = nil
			case "missing_parent":
				keys = keys[1:]
			case "cross_owner":
				keys[1].UserID = "usr_other"
			case "cycle":
				keys[0].ReplacesKeyID = &keys[1].ID
			case "alias":
				keys[0].ID = strings.ToUpper(root.ID)
			case "published_birth":
				a.Quota.Created[limitAccount("key", root.ID)] = root.CreatedAt.Add(time.Millisecond)
			case "published_root":
				a.LimitRoots[root.ID] = "key_other"
			case "published_revision":
				p := a.PersonalKeyStates[root.ID]
				p.Revision += "x"
				a.PersonalKeyStates[root.ID] = p
			case "published_status":
				p := a.PersonalKeyStates[root.ID]
				p.Status = entity.KeyPending
				a.PersonalKeyStates[root.ID] = p
			case "published_owner":
				p := a.PersonalKeyStates[root.ID]
				p.UserID = "usr_other"
				a.PersonalKeyStates[root.ID] = p
			case "published_active_birth":
				p := a.KeysByID[root.ID]
				p.Key.CreatedAt = p.Key.CreatedAt.Add(time.Millisecond)
				a.KeysByID[root.ID] = p
			case "published_active_expiry":
				p := a.KeysByID[root.ID]
				date := time.Now().Add(time.Hour)
				p.Key.ExpiresAt = &date
				a.KeysByID[root.ID] = p
			case "project_key":
				p := a.KeysByID[root.ID]
				p.ProjectID = "prj_other"
				a.KeysByID[root.ID] = p
			case "pending_owner", "rejected_owner":
				p := apps[*u.ApprovalApplicationID]
				p.State = "pending"
				if name == "rejected_owner" {
					p.State = "rejected"
				}
				apps[p.ID] = p
			case "missing_application":
				apps = nil
			case "owner_birth":
				u.CreatedAt = u.CreatedAt.Add(time.Millisecond)
			case "disabled_owner":
				u.Disabled = true
			case "offboarded_owner":
				u.OffboardedAt = &u.CreatedAt
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
			case "limit_tombstone":
				s.runtime.deniedLimits.Store(limitAccount("key", root.ID), true)
			case "calendar_tombstone":
				s.runtime.deniedLimits.Store("quota_settings", true)
			case "owner_tombstone":
				s.runtime.deniedUsers.Store(u.ID, true)
			case "grant_tombstone":
				s.runtime.deniedPersonalGrants.Store(u.ID, true)
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
			if got := s.personalKeyWarningApplied(ctx, a, u, apps, keys, root.ID, row, policy, c, "USD"); got != want {
				t.Fatalf("%s=%v want=%v", name, got, want)
			}
		})
	}
}
func TestPersonalKeyWarningCompleteGraphBoundaries(t *testing.T) {
	for _, n := range []int{500, 501, 1000, 10000, 10001} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			_, root, owner, _ := personalKeyWarningFixture()
			keys := make([]entity.APIKey, n)
			for i := range keys {
				keys[i] = root
				keys[i].ID = fmt.Sprintf("key_%026d", i)
				if i > 0 {
					keys[i].ReplacesKeyID = &keys[i-1].ID
				}
			}
			start := time.Now()
			roots, ok := personalKeyWarningRoots(keys, owner.ID)
			elapsed := time.Since(start)
			if ok != (n <= 10000) || ok && (len(roots) != n || roots[keys[n-1].ID] != keys[0].ID) {
				t.Fatal("complete graph was truncated or rejected", n)
			}
			t.Logf("bounded graph %d rows: %s", n, elapsed)
			if elapsed > 3*time.Second {
				t.Fatal("bounded source graph exceeded existing runtime refresh deadline")
			}
		})
	}
}
