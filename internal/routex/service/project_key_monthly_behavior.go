package service

import (
	"errors"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/limits"
)

type projectKeyMonthlyIdentity struct {
	Root    entity.ProjectKey
	Project entity.Project
}

// A key row alone cannot establish its immutable Project owner or birth.
func projectKeyPolicyFromRow(row entity.ResourceLimit, root entity.ProjectKey, project entity.Project) (limits.Policy, error) {
	if row.ScopeKind != "key" || row.ScopeID != root.ID || !projectWarningKeyID(root.ID) || !projectMonthlyID(project.ID) || root.ProjectID != project.ID || root.ReplacesKeyID != nil || project.CreatedAt.IsZero() || root.CreatedAt.IsZero() || root.CreatedAt.Before(project.CreatedAt) {
		return limits.Policy{}, limits.ErrInvalid
	}
	return decodeLimitPolicy(row, true)
}

func projectKeyLimitReader(project string) func(*gorm.DB, string, string) (entity.ResourceLimit, limits.Policy, error) {
	return func(db *gorm.DB, kind, root string) (entity.ResourceLimit, limits.Policy, error) {
		if kind != "key" {
			return entity.ResourceLimit{}, limits.Policy{}, limits.ErrInvalid
		}
		return readProjectKeyLimitPolicy(db, root, project, "")
	}
}

// current is supplied only by the no-publisher gateway path. Control-plane
// target resolution has already authorized the current Key before this read.
func readProjectKeyLimitPolicy(db *gorm.DB, rootID, projectID, current string) (entity.ResourceLimit, limits.Policy, error) {
	var project entity.Project
	if err := db.Where(database.ExactText(db, clause.Column{Name: "id"}, projectID)).Take(&project).Error; err != nil {
		return entity.ResourceLimit{}, limits.Policy{}, err
	}
	var keys []entity.ProjectKey
	if err := projectKeyWarningChainQuery(db, projectID).Find(&keys).Error; err != nil {
		return entity.ResourceLimit{}, limits.Policy{}, err
	}
	roots, complete := projectKeyWarningRoots(keys, project)
	if !complete || roots[rootID] != rootID || current != "" && roots[current] != rootID {
		return entity.ResourceLimit{}, limits.Policy{}, limits.ErrInvalid
	}
	var root entity.ProjectKey
	ids := make([]string, 0, len(keys))
	for _, key := range keys {
		ids = append(ids, key.ID)
		if key.ID == rootID {
			root = key
		}
	}
	// A bounded candidate query catches exact identities and collation aliases.
	var collisions []entity.APIKey
	if err := db.Select("id").Where("id IN ?", ids).Limit(projectKeyWarningChainLimit + 1).Find(&collisions).Error; err != nil {
		return entity.ResourceLimit{}, limits.Policy{}, err
	}
	if len(collisions) != 0 {
		return entity.ResourceLimit{}, limits.Policy{}, limits.ErrInvalid
	}
	row := entity.ResourceLimit{ScopeKind: "key", ScopeID: rootID, ETag: "0"}
	err := db.Where(database.ExactText(db, clause.Column{Name: "scope_kind"}, "key")).Where(database.ExactText(db, clause.Column{Name: "scope_id"}, rootID)).Take(&row).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return row, limits.Policy{}, err
	}
	policy, err := projectKeyPolicyFromRow(row, root, project)
	return row, policy, err
}

func runtimeProjectKeyRoots(data *runtimeData) map[string]projectKeyMonthlyIdentity {
	result := map[string]projectKeyMonthlyIdentity{}
	if data.ProjectData == nil {
		return result
	}
	collisions := map[string]int{}
	for _, key := range data.Keys {
		collisions[strings.ToLower(key.ID)]++
	}
	for _, key := range data.ProjectData.Keys {
		collisions[strings.ToLower(key.ID)]++
	}
	projects := map[string]int{}
	for _, project := range data.ProjectData.Projects {
		projects[strings.ToLower(project.ID)]++
	}
	for _, project := range data.ProjectData.Projects {
		if projects[strings.ToLower(project.ID)] != 1 || !projectMonthlyID(project.ID) {
			continue
		}
		keys := []entity.ProjectKey{}
		disjoint := true
		for _, key := range data.ProjectData.Keys {
			if key.ProjectID == project.ID {
				keys = append(keys, key)
				disjoint = disjoint && collisions[strings.ToLower(key.ID)] == 1
			}
		}
		roots, complete := projectKeyWarningRoots(keys, project)
		if !complete || !disjoint {
			continue
		}
		for _, key := range keys {
			if roots[key.ID] == key.ID {
				result[key.ID] = projectKeyMonthlyIdentity{key, project}
			}
		}
	}
	return result
}

func projectKeyQuotaProof(auth *runtimeAuthorization, result *GatewayResult, account string) bool {
	if auth == nil || auth.Quota == nil || result.ProjectID == "" || result.UserID != "" || result.TeamID != "" || result.KeyID == "" {
		return false
	}
	root := auth.LimitRoots[result.KeyID]
	identity, ok := auth.ProjectLimitOwners[root]
	current, present := auth.KeysByID[result.KeyID]
	state, known := auth.ProjectCreationStates[result.ProjectID]
	return ok && present && known && state.Project.ID == result.ProjectID && state.Project.Status == entity.ResourceActive && state.Project.CreatedAt.Equal(identity.Project.CreatedAt) && !identity.Project.CreatedAt.IsZero() && identity.Project.ID == result.ProjectID && identity.Root.ProjectID == result.ProjectID && identity.Root.ID == root && identity.Root.ReplacesKeyID == nil && !identity.Root.CreatedAt.IsZero() && auth.Quota.Created[limitAccount("key", root)].Equal(identity.Root.CreatedAt) && current.ProjectID == result.ProjectID && current.Key.UserID == "" && current.Key.ID == result.KeyID && current.Key.Status == entity.KeyActive && !current.Key.CreatedAt.IsZero() && auth.Quota.Created[limitAccount("key", result.KeyID)].Equal(current.Key.CreatedAt) && account == limitAccount("key", root)
}
