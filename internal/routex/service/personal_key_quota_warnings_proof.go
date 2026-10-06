package service

import (
	"context"
	"reflect"
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/limits"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const personalKeyWarningChainLimit = 10000
const personalKeyQuotaWarningGeneration = "personal-key-monthly-80-90-v1"

// The complete retained owner graph excludes secret, digest and display prefix.
const personalKeyWarningSelect = "id,user_id,name,status,lifecycle_revision,replaces_key_id,expires_at,created_at"

func personalKeyWarningChainQuery(tx *gorm.DB, ownerID string) *gorm.DB {
	return tx.Model(&entity.APIKey{}).Select(personalKeyWarningSelect).Where("user_id = ?", ownerID).
		Where(database.ExactText(tx, clause.Column{Name: "user_id"}, ownerID)).Order("id").Limit(personalKeyWarningChainLimit + 1)
}
func personalKeyWarningRoots(keys []entity.APIKey, ownerID string) (map[string]string, bool) {
	if !memberKeyUserID.MatchString(ownerID) || len(keys) == 0 || len(keys) > personalKeyWarningChainLimit {
		return nil, false
	}
	seen := map[string]bool{}
	for _, k := range keys {
		if !memberKeyID.MatchString(k.ID) || !memberKeyRevision.MatchString(k.LifecycleRevision) || k.UserID != ownerID || k.CreatedAt.IsZero() || seen[k.ID] {
			return nil, false
		}
		seen[k.ID] = true
		if k.Status != entity.KeyPending && k.Status != entity.KeyActive && k.Status != entity.KeyDisabled && k.Status != entity.KeyRevoked {
			return nil, false
		}
	}
	roots, err := personalLimitRoots(keys)
	return roots, err == nil
}
func (s *Service) personalKeyWarningApplied(ctx context.Context, auth *runtimeAuthorization, owner entity.User, apps map[string]entity.RegistrationApprovalApplication, keys []entity.APIKey, rootID string, row entity.ResourceLimit, policy limits.Policy, calendar entity.QuotaSetting, currency string) bool {
	rt := s.runtime
	if ctx.Err() != nil || rt == nil || rt.done == nil || auth == nil || auth.Quota == nil || runtimeDenied(&rt.deniedPersonalGrants, owner.ID) || !s.registrationAdvisoryPublished(auth, owner, apps) {
		return false
	}
	select {
	case <-rt.done:
		return false
	default:
	}
	roots, ok := personalKeyWarningRoots(keys, owner.ID)
	if !ok {
		return false
	}
	account := limitAccount("key", rootID)
	if row.ScopeKind != "key" || row.ScopeID != rootID || row.ETag == "" || auth.Quota.Revisions[account] != row.ETag || calendar.ETag == "" || !calendar.AccountingStarted || auth.Quota.Setting.ETag != calendar.ETag || !auth.Quota.Setting.AccountingStarted || auth.Quota.Setting.TimeZone != calendar.TimeZone || runtimeDenied(&rt.deniedLimits, account) || runtimeDenied(&rt.deniedLimits, "quota_settings") {
		return false
	}
	published, err := limits.Normalize(auth.LimitPolicies[account])
	if err != nil || !reflect.DeepEqual(published, policy) || (policy.MoneyMonth != nil && auth.Quota.Currency != currency) {
		return false
	}
	rootFound := false
	now := s.gatewayAttemptClock()
	for _, key := range keys {
		state, present := auth.PersonalKeyStates[key.ID]
		if key.CreatedAt.Before(owner.CreatedAt) || !present || state.UserID != owner.ID || state.Revision != key.LifecycleRevision || state.Status != key.Status || auth.LimitRoots[key.ID] != roots[key.ID] || !auth.Quota.Created[limitAccount("key", key.ID)].Equal(key.CreatedAt) {
			return false
		}
		if key.ID == rootID {
			rootFound = roots[key.ID] == rootID && key.ReplacesKeyID == nil
		}
	}
	if !rootFound || !personalKeyWarningLive(s, auth, keys, roots, rootID, now) {
		return false
	}
	if ctx.Err() != nil || s.runtime != rt || rt.auth.Load() != auth || !s.registrationAdvisoryPublished(auth, owner, apps) || !s.gatewayAttemptClock().Before(auth.ValidUntil) || runtimeDenied(&rt.deniedLimits, account) || runtimeDenied(&rt.deniedLimits, "quota_settings") {
		return false
	}
	select {
	case <-rt.done:
		return false
	default:
		return personalKeyWarningLive(s, auth, keys, roots, rootID, s.gatewayAttemptClock()) && ctx.Err() == nil && s.runtime == rt && rt.auth.Load() == auth && s.gatewayAttemptClock().Before(auth.ValidUntil) && !runtimeDenied(&rt.deniedLimits, account) && !runtimeDenied(&rt.deniedLimits, "quota_settings") && !runtimeDenied(&rt.deniedPersonalGrants, owner.ID) && s.registrationAdvisoryPublished(auth, owner, apps)
	}
}
func personalKeyWarningLive(s *Service, auth *runtimeAuthorization, keys []entity.APIKey, roots map[string]string, rootID string, now time.Time) bool {
	for _, key := range keys {
		if roots[key.ID] != rootID || key.Status != entity.KeyActive || key.ExpiresAt != nil && !now.Before(*key.ExpiresAt) || runtimeDenied(&s.runtime.deniedKeys, key.ID) {
			continue
		}
		current, present := auth.KeysByID[key.ID]
		if present && current.ProjectID == "" && current.Key.ID == key.ID && current.Key.UserID == key.UserID && current.Key.Status == key.Status && current.Key.LifecycleRevision == key.LifecycleRevision && current.Key.CreatedAt.Equal(key.CreatedAt) && sameOptionalTime(current.Key.ExpiresAt, key.ExpiresAt) {
			return true
		}
	}
	return false
}

func sameOptionalTime(a, b *time.Time) bool {
	return a == nil && b == nil || a != nil && b != nil && a.Equal(*b)
}
