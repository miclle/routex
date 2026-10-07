package service

import (
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/limits"
)

// A shared key policy becomes soft only with an exact immutable Personal root.
// Generic/project readers retain policyFromRow's hard-only key scope fence.
func personalKeyPolicyFromRow(row entity.ResourceLimit, root entity.APIKey, owner string) (limits.Policy, error) {
	if row.ScopeKind != "key" || row.ScopeID != root.ID || root.ID == "" || owner == "" || root.UserID != owner || root.ReplacesKeyID != nil {
		return limits.Policy{}, limits.ErrInvalid
	}
	return decodeLimitPolicy(row, true)
}

func personalKeyLimitReader(owner string) func(*gorm.DB, string, string) (entity.ResourceLimit, limits.Policy, error) {
	return func(db *gorm.DB, kind, root string) (entity.ResourceLimit, limits.Policy, error) {
		if kind != "key" {
			return entity.ResourceLimit{}, limits.Policy{}, limits.ErrInvalid
		}
		return readPersonalKeyLimitPolicy(db, root, owner)
	}
}

func readPersonalKeyLimitPolicy(db *gorm.DB, rootID, owner string) (entity.ResourceLimit, limits.Policy, error) {
	var root entity.APIKey
	err := db.Select("id,user_id,replaces_key_id").Where(database.ExactText(db, clause.Column{Name: "id"}, rootID)).Where(database.ExactText(db, clause.Column{Name: "user_id"}, owner)).Take(&root).Error
	if err != nil {
		return entity.ResourceLimit{}, limits.Policy{}, err
	}
	// Independent tables cannot establish a disjoint identity through a prefix.
	var collision entity.ProjectKey
	err = db.Select("id").Where(database.ExactText(db, clause.Column{Name: "id"}, rootID)).Take(&collision).Error
	if err == nil {
		return entity.ResourceLimit{}, limits.Policy{}, limits.ErrInvalid
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return entity.ResourceLimit{}, limits.Policy{}, err
	}
	row := entity.ResourceLimit{ScopeKind: "key", ScopeID: rootID, ETag: "0"}
	err = db.Where(database.ExactText(db, clause.Column{Name: "scope_kind"}, "key")).Where(database.ExactText(db, clause.Column{Name: "scope_id"}, rootID)).Take(&row).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return row, limits.Policy{}, err
	}
	policy, err := personalKeyPolicyFromRow(row, root, owner)
	return row, policy, err
}

func readGatewayLimitPolicy(db *gorm.DB, kind, scopeID string, result *GatewayResult) (entity.ResourceLimit, limits.Policy, error) {
	if kind == "key" && result.ProjectID == "" && result.TeamID == "" && result.UserID != "" {
		// The no-publisher path must prove the selected current Key belongs to
		// this exact root, not accept an arbitrary account supplied by a caller.
		var keys []entity.APIKey
		if err := db.Select("id,user_id,replaces_key_id").Where(database.ExactText(db, clause.Column{Name: "user_id"}, result.UserID)).Find(&keys).Error; err != nil {
			return entity.ResourceLimit{}, limits.Policy{}, err
		}
		if !personalKeyGatewayRoot(keys, result, scopeID) {
			return entity.ResourceLimit{}, limits.Policy{}, limits.ErrInvalid
		}
		var collision entity.ProjectKey
		err := db.Select("id").Where(database.ExactText(db, clause.Column{Name: "id"}, result.KeyID)).Take(&collision).Error
		if err == nil {
			return entity.ResourceLimit{}, limits.Policy{}, limits.ErrInvalid
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return entity.ResourceLimit{}, limits.Policy{}, err
		}
		return readPersonalKeyLimitPolicy(db, scopeID, result.UserID)
	}
	if kind == "key" && result.ProjectID != "" && result.UserID == "" && result.TeamID == "" {
		return readProjectKeyLimitPolicy(db, scopeID, result.ProjectID, result.KeyID)
	}
	return readLimitPolicy(db, kind, scopeID)
}

// personalKeyGatewayRoot checks the no-publisher current Key and every retained
// rotation node using exact identities before a root can carry soft behavior.
func personalKeyGatewayRoot(keys []entity.APIKey, result *GatewayResult, root string) bool {
	if result.ProjectID != "" || result.TeamID != "" || result.KeyID == "" || result.UserID == "" || root == "" {
		return false
	}
	seen := map[string]bool{}
	for _, key := range keys {
		if key.ID == "" || key.UserID != result.UserID || seen[key.ID] {
			return false
		}
		seen[key.ID] = true
	}
	roots, err := personalLimitRoots(keys)
	return err == nil && roots[result.KeyID] == root
}

func runtimePersonalKeyRoots(data *runtimeData) map[string]entity.APIKey {
	users := map[string]int{}
	for _, user := range data.Users {
		users[user.ID]++
	}
	collisions := map[string]int{}
	if data.ProjectData != nil {
		for _, key := range data.ProjectData.Keys {
			collisions[key.ID]++
		}
	}
	roots := map[string]entity.APIKey{}
	for _, key := range data.Keys {
		collisions[key.ID]++
		if key.ReplacesKeyID == nil && key.ID != "" && key.UserID != "" && users[key.UserID] == 1 {
			roots[key.ID] = key
		}
	}
	for key := range roots {
		if collisions[key] != 1 {
			delete(roots, key)
		}
	}
	return roots
}

func personalKeyAuditParent(target resolvedLimitTarget) string {
	if target.kind == "key" && (target.parentKind == "user" || target.parentKind == "project") {
		return target.parentKind
	}
	return ""
}

func validLimitAuditBehavior(row entity.AuditEvent, parentKind string, policy limits.Policy) bool {
	if parentKind != "" {
		if row.ResourceType != "key" || (parentKind != "user" && parentKind != "project") || !safeTeamSessionID(row.ResourceID) || parentKind == "project" && !projectWarningKeyID(row.ResourceID) {
			return false
		}
		_, err := limits.Normalize(policy)
		return err == nil
	}
	if row.ResourceType == "project" && (policy.TokensMonthBehavior == "alert_only" || policy.MoneyMonthBehavior == "alert_only") && !projectMonthlyID(row.ResourceID) {
		return false
	}
	return validMonthlyBehaviorAudit(row.ResourceType, policy)
}

func runtimePersonalLimitOwners(data *runtimeData) map[string]string {
	owners := map[string]string{}
	for root, key := range runtimePersonalKeyRoots(data) {
		owners[root] = key.UserID
	}
	return owners
}

func personalKeyQuotaProof(auth *runtimeAuthorization, result *GatewayResult, account string) bool {
	if auth == nil || result.ProjectID != "" || result.TeamID != "" || result.UserID == "" || result.KeyID == "" {
		return false
	}
	current, ok := auth.KeysByID[result.KeyID]
	root := auth.LimitRoots[result.KeyID]
	return ok && current.ProjectID == "" && current.Key.ID == result.KeyID && current.Key.UserID == result.UserID && root != "" && auth.PersonalLimitOwners[root] == result.UserID && auth.UserProofs[result.UserID].Enabled && account == limitAccount("key", root)
}
