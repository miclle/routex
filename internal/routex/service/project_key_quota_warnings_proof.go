package service

import (
	"context"
	"reflect"
	"strings"
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/limits"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const projectKeyWarningChainLimit = 10000
const projectKeyQuotaWarningGeneration = "project-key-monthly-80-90-v1"
const projectKeyWarningSelect = "id,project_id,name,status,replaces_key_id,expires_at,created_at"

func projectWarningKeyID(value string) bool {
	return safeTeamSessionID(value) && strings.HasPrefix(value, "pky_")
}
func projectKeyWarningChainQuery(tx *gorm.DB, projectID string) *gorm.DB {
	// The released Project index bounds the equality candidate; ExactText is an independent identity guard.
	return tx.Model(&entity.ProjectKey{}).Select(projectKeyWarningSelect).Where("project_id = ?", projectID).
		Where(database.ExactText(tx, clause.Column{Name: "project_id"}, projectID)).Order("id").Limit(projectKeyWarningChainLimit + 1)
}
func projectKeyWarningRoots(keys []entity.ProjectKey, project entity.Project) (map[string]string, bool) {
	if !safeTeamSessionID(project.ID) || project.CreatedAt.IsZero() || len(keys) == 0 || len(keys) > projectKeyWarningChainLimit {
		return nil, false
	}
	seen := map[string]bool{}
	for _, key := range keys {
		folded := strings.ToLower(key.ID)
		if !projectWarningKeyID(key.ID) || key.ProjectID != project.ID || key.CreatedAt.IsZero() || key.CreatedAt.Before(project.CreatedAt) || seen[folded] {
			return nil, false
		}
		seen[folded] = true
		if key.Status != entity.KeyPending && key.Status != entity.KeyActive && key.Status != entity.KeyDisabled && key.Status != entity.KeyRevoked {
			return nil, false
		}
		if key.ReplacesKeyID != nil && !projectWarningKeyID(*key.ReplacesKeyID) {
			return nil, false
		}
	}
	roots, err := projectLimitRoots(keys)
	return roots, err == nil
}
func projectKeyWarningLive(s *Service, auth *runtimeAuthorization, keys []entity.ProjectKey, roots map[string]string, rootID string, now time.Time) bool {
	for _, key := range keys {
		if roots[key.ID] != rootID || key.Status != entity.KeyActive || key.ExpiresAt != nil && !now.Before(*key.ExpiresAt) || runtimeDenied(&s.runtime.deniedKeys, key.ID) {
			continue
		}
		current, present := auth.KeysByID[key.ID]
		if present && current.ProjectID == key.ProjectID && current.Key.UserID == "" && current.Key.ID == key.ID && current.Key.Status == key.Status && current.Key.CreatedAt.Equal(key.CreatedAt) && sameOptionalTime(current.Key.ExpiresAt, key.ExpiresAt) {
			return true
		}
	}
	return false
}
func (s *Service) projectKeyWarningApplied(ctx context.Context, auth *runtimeAuthorization, project entity.Project, keys []entity.ProjectKey, rootID string, row entity.ResourceLimit, policy limits.Policy, calendar entity.QuotaSetting, currency string) bool {
	rt := s.runtime
	if ctx.Err() != nil || rt == nil || rt.done == nil || auth == nil || auth.Quota == nil || project.Status != entity.ResourceActive || project.CreatedAt.IsZero() {
		return false
	}
	select {
	case <-rt.done:
		return false
	default:
	}
	state, exists := auth.ProjectCreationStates[project.ID]
	if !exists || state.Project.ID != project.ID || state.Project.Status != entity.ResourceActive || !state.Project.CreatedAt.Equal(project.CreatedAt) || !state.Eligible {
		return false
	}
	roots, complete := projectKeyWarningRoots(keys, project)
	if !complete || roots[rootID] != rootID {
		return false
	}
	account := limitAccount("key", rootID)
	if row.ScopeKind != "key" || row.ScopeID != rootID || row.ETag == "" || auth.Quota.Revisions[account] != row.ETag || calendar.ETag == "" || !calendar.AccountingStarted || auth.Quota.Setting.ETag != calendar.ETag || !auth.Quota.Setting.AccountingStarted || auth.Quota.Setting.TimeZone != calendar.TimeZone {
		return false
	}
	published, err := limits.Normalize(auth.LimitPolicies[account])
	if err != nil || !reflect.DeepEqual(published, policy) || policy.MoneyMonth != nil && auth.Quota.Currency != currency {
		return false
	}
	rootFound := false
	for _, key := range keys {
		// Retained parent links and births are immutable supported-writer facts. Inactive status is not a quota-account reset.
		if auth.LimitRoots[key.ID] != roots[key.ID] || !auth.Quota.Created[limitAccount("key", key.ID)].Equal(key.CreatedAt) {
			return false
		}
		if key.ID == rootID {
			rootFound = key.ReplacesKeyID == nil
		}
	}
	if !rootFound {
		return false
	}
	current := func() bool {
		if ctx.Err() != nil || s.runtime != rt || rt.auth.Load() != auth || !s.gatewayAttemptClock().Before(auth.ValidUntil) || runtimeDenied(&rt.deniedProjects, project.ID) || runtimeDenied(&rt.deniedLimits, account) || runtimeDenied(&rt.deniedLimits, "quota_settings") {
			return false
		}
		select {
		case <-rt.done:
			return false
		default:
		}
		return projectKeyWarningLive(s, auth, keys, roots, rootID, s.gatewayAttemptClock())
	}
	if !current() {
		return false
	}
	return current()
}
