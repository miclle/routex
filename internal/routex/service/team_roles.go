package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/id"
)

var teamRoleActions = []string{"teams.models.write", "teams.write"}
var errTeamRoleOverflow = &apperrors.Error{Code: http.StatusUnprocessableEntity, Message: "Team roles exceed supported bounds"}

type TeamRoleSummary struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Builtin     bool     `json:"builtin"`
	TeamActions []string `json:"team_actions"`
}
type TeamRolesRecord struct {
	TeamID               string            `json:"team_id"`
	RoleIDs              []string          `json:"role_ids"`
	Roles                []TeamRoleSummary `json:"roles"`
	EffectiveTeamActions []string          `json:"effective_team_actions"`
	ActorTeamActions     []string          `json:"actor_team_actions"`
	ETag                 string            `json:"etag"`
	CanAssignRoles       bool              `json:"can_assign_roles"`
}
type TeamRoleCandidateFilter struct {
	Query, Cursor string
	Limit         int
}
type TeamRoleCandidatePage struct {
	Items      []TeamRoleSummary `json:"items"`
	NextCursor *string           `json:"next_cursor"`
	ETag       string            `json:"etag"`
}
type teamRoleTarget struct {
	Actor         entity.User
	Team          entity.Team
	ActiveMember  bool
	ReadAll       bool
	DirectActions []string
}

func teamRoleTargetAccess(tx *gorm.DB, actorID, teamID string, lock bool) (*teamRoleTarget, error) {
	actor, err := exactEnabledActor(tx, actorID)
	if err != nil {
		return nil, err
	}
	target := &teamRoleTarget{Actor: actor, DirectActions: []string{}}
	target.ReadAll, err = exactGovernancePermission(tx, actor, "teams.read_all")
	if err != nil {
		return nil, err
	}
	for _, action := range teamRoleActions {
		allowed, err := exactGovernancePermission(tx, actor, action)
		if err != nil {
			return nil, err
		}
		if allowed {
			target.DirectActions = append(target.DirectActions, action)
		}
	}
	var memberships []entity.TeamMembership
	if err := quotaExact(quotaExact(tx, "team_id", teamID), "user_id", actorID).Where(database.ExactText(tx, clause.Column{Name: "status"}, entity.ResourceActive)).Limit(1).Find(&memberships).Error; err != nil {
		return nil, err
	}
	target.ActiveMember = len(memberships) == 1 && memberships[0].TeamID == teamID && memberships[0].UserID == actorID && memberships[0].Status == entity.ResourceActive && (memberships[0].Role == entity.TeamMember || memberships[0].Role == entity.TeamOwner)
	// Scope is checked before looking up an unrelated Team identity.
	if !target.ReadAll && len(target.DirectActions) == 0 && !target.ActiveMember {
		return nil, apperrors.ErrNotFound
	}
	query := quotaExact(tx, "id", teamID)
	if lock {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	if err := query.First(&target.Team).Error; err != nil {
		return nil, err
	}
	if target.Team.ID != teamID {
		return nil, apperrors.ErrNotFound
	}
	target.ActiveMember = target.ActiveMember && target.Team.Status == entity.ResourceActive
	if !target.ReadAll && len(target.DirectActions) == 0 && !target.ActiveMember {
		return nil, apperrors.ErrNotFound
	}
	return target, nil
}
func teamRoleActionUnion(roles []TeamRoleSummary) []string {
	result := []string{}
	for _, role := range roles {
		for _, action := range role.TeamActions {
			if slices.Contains(teamRoleActions, action) && !slices.Contains(result, action) {
				result = append(result, action)
			}
		}
	}
	slices.Sort(result)
	return result
}
func teamRoleDefinitionQuery(tx *gorm.DB, roleIDs []string) *gorm.DB {
	expressions := []clause.Expression{}
	for _, roleID := range roleIDs {
		expressions = append(expressions, database.ExactText(tx, clause.Column{Name: "id"}, roleID))
	}
	if len(expressions) == 0 {
		return tx.Where("1 = 0")
	}
	return tx.Where(clause.Or(expressions...))
}

type teamRoleDefinitionGeneration struct {
	ID       string
	Revision string
}

func loadTeamRoleDefinitions(tx *gorm.DB, roleIDs []string, all bool) ([]TeamRoleSummary, error) {
	roles, _, err := loadReviewedTeamRoleDefinitions(tx, roleIDs, all)
	return roles, err
}
func loadReviewedTeamRoleDefinitions(tx *gorm.DB, roleIDs []string, all bool) ([]TeamRoleSummary, []teamRoleDefinitionGeneration, error) {
	query := tx.Model(&entity.Role{})
	limit := 101
	if all {
		limit = 1001
	} else {
		query = teamRoleDefinitionQuery(query, roleIDs)
	}
	var rows []entity.Role
	if err := query.Order("id").Limit(limit).Find(&rows).Error; err != nil {
		return nil, nil, err
	}
	if len(rows) == limit {
		return nil, nil, errTeamRoleOverflow
	}
	result := []TeamRoleSummary{}
	generations := []teamRoleDefinitionGeneration{}
	selected := map[string]int{}
	for _, row := range rows {
		if !safeTeamSessionID(row.ID) || !strings.HasPrefix(row.ID, "rol_") || !validCatalogLabel(row.Name) || !validMemberRoleDigest(row.DefinitionRevision) {
			return nil, nil, apperrors.ErrInternal
		}
		generations = append(generations, teamRoleDefinitionGeneration{ID: row.ID, Revision: row.DefinitionRevision})
		selected[row.ID] = len(result)
		result = append(result, TeamRoleSummary{ID: row.ID, Name: row.Name, Builtin: row.Builtin, TeamActions: []string{}})
	}
	if !all && len(rows) != len(roleIDs) {
		return nil, nil, apperrors.ErrBadRequest
	}
	if len(rows) == 0 {
		return result, generations, nil
	}
	roleScope := []clause.Expression{}
	for _, row := range rows {
		roleScope = append(roleScope, database.ExactText(tx, clause.Column{Name: "role_id"}, row.ID))
	}
	actionScope := []clause.Expression{}
	for _, action := range teamRoleActions {
		actionScope = append(actionScope, database.ExactText(tx, clause.Column{Name: "permission"}, action))
	}
	var permissions []entity.RolePermission
	if err := tx.Where(clause.Or(roleScope...)).Where(clause.Or(actionScope...)).Order("role_id,permission").Limit(2001).Find(&permissions).Error; err != nil {
		return nil, nil, err
	}
	if len(permissions) > 2000 {
		return nil, nil, errTeamRoleOverflow
	}
	for _, permission := range permissions {
		index, exists := selected[permission.RoleID]
		if !exists || !slices.Contains(teamRoleActions, permission.Permission) {
			return nil, nil, apperrors.ErrInternal
		}
		if !slices.Contains(result[index].TeamActions, permission.Permission) {
			result[index].TeamActions = append(result[index].TeamActions, permission.Permission)
		}
	}
	for index := range result {
		slices.Sort(result[index].TeamActions)
	}
	return result, generations, nil
}
func loadTeamRoles(tx *gorm.DB, teamID string) ([]TeamRoleSummary, error) {
	roles, _, err := loadReviewedTeamRoles(tx, teamID)
	return roles, err
}
func loadReviewedTeamRoles(tx *gorm.DB, teamID string) ([]TeamRoleSummary, []teamRoleDefinitionGeneration, error) {
	var rows []entity.TeamRole
	if err := quotaExact(tx, "team_id", teamID).Order("role_id").Limit(101).Find(&rows).Error; err != nil {
		return nil, nil, err
	}
	if len(rows) > 100 {
		return nil, nil, errTeamRoleOverflow
	}
	ids := []string{}
	for _, row := range rows {
		if row.TeamID != teamID {
			return nil, nil, apperrors.ErrInternal
		}
		ids = append(ids, row.RoleID)
	}
	return loadReviewedTeamRoleDefinitions(tx, ids, false)
}
func teamRolesRecord(tx *gorm.DB, target *teamRoleTarget) (*TeamRolesRecord, error) {
	roles, roleGenerations, err := loadReviewedTeamRoles(tx, target.Team.ID)
	if err != nil {
		return nil, err
	}
	result := &TeamRolesRecord{TeamID: target.Team.ID, RoleIDs: []string{}, Roles: roles, EffectiveTeamActions: []string{}, ActorTeamActions: slices.Clone(target.DirectActions), CanAssignRoles: target.Actor.Role == entity.RoleAdmin && target.Team.Status != entity.ResourceArchived}
	for _, role := range roles {
		result.RoleIDs = append(result.RoleIDs, role.ID)
	}
	if target.Team.Status == entity.ResourceActive {
		result.EffectiveTeamActions = teamRoleActionUnion(roles)
		if target.ActiveMember {
			for _, action := range result.EffectiveTeamActions {
				if !slices.Contains(result.ActorTeamActions, action) {
					result.ActorTeamActions = append(result.ActorTeamActions, action)
				}
			}
		}
	}
	if target.Team.Status == entity.ResourceArchived {
		result.ActorTeamActions = []string{}
	}
	slices.Sort(result.ActorTeamActions)
	// Protected selection reviews the complete safe catalogue so newly selected
	// roles cannot change their definitions between candidate read and save,
	// including A-to-B-to-A edits with identical restored public Team actions.
	catalogue := []TeamRoleSummary{}
	catalogueGenerations := []teamRoleDefinitionGeneration{}
	if result.CanAssignRoles {
		catalogue, catalogueGenerations, err = loadReviewedTeamRoleDefinitions(tx, nil, true)
		if err != nil {
			return nil, err
		}
	}
	result.ETag, err = teamQuotaHash(struct {
		ActorID                               string
		Team                                  entity.Team
		Roles, Catalogue                      []TeamRoleSummary
		ActorActions                          []string
		CanAssign                             bool
		RoleGenerations, CatalogueGenerations []teamRoleDefinitionGeneration
	}{target.Actor.ID, target.Team, roles, catalogue, result.ActorTeamActions, result.CanAssignRoles, roleGenerations, catalogueGenerations})
	return result, err
}
func teamTargetActionAllowed(tx *gorm.DB, actorID, teamID, action string) (bool, error) {
	if !slices.Contains(teamRoleActions, action) {
		return false, apperrors.ErrBadRequest
	}
	target, err := teamRoleTargetAccess(tx, actorID, teamID, false)
	if err != nil {
		return false, err
	}
	if slices.Contains(target.DirectActions, action) {
		return true, nil
	}
	if !target.ActiveMember {
		return false, nil
	}
	roles, err := loadTeamRoles(tx, teamID)
	if err != nil {
		return false, err
	}
	return slices.Contains(teamRoleActionUnion(roles), action), nil
}
func (s *Service) GetTeamRoles(ctx context.Context, actorID, teamID string) (*TeamRolesRecord, error) {
	if !safeTeamSessionID(teamID) {
		return nil, apperrors.ErrBadRequest
	}
	var result *TeamRolesRecord
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		target, err := teamRoleTargetAccess(tx, actorID, teamID, false)
		if err != nil {
			return err
		}
		result, err = teamRolesRecord(tx, target)
		return err
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return result, catalogError(err)
}
func (s *Service) TeamRoleCandidates(ctx context.Context, actorID, teamID string, filter TeamRoleCandidateFilter) (*TeamRoleCandidatePage, error) {
	if filter.Limit == 0 {
		filter.Limit = 25
	}
	if _, err := candidatePattern(filter.Query); err != nil || filter.Limit < 1 || filter.Limit > 50 || len(filter.Cursor) > 200 {
		return nil, apperrors.ErrBadRequest
	}
	var result *TeamRoleCandidatePage
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		actor, err := exactEnabledActor(tx, actorID)
		if err != nil {
			return err
		}
		if actor.Role != entity.RoleAdmin {
			return apperrors.ErrForbidden
		}
		target, err := teamRoleTargetAccess(tx, actorID, teamID, false)
		if err != nil {
			return err
		}
		record, err := teamRolesRecord(tx, target)
		if err != nil {
			return err
		}
		roles, err := loadTeamRoleDefinitions(tx, nil, true)
		if err != nil {
			return err
		}
		result = &TeamRoleCandidatePage{Items: []TeamRoleSummary{}, ETag: record.ETag}
		after, err := decodeTeamRoleCursor(filter.Cursor, actorID, teamID, filter.Query, record.ETag)
		if err != nil {
			return err
		}
		query := strings.ToLower(strings.TrimSpace(filter.Query))
		for _, role := range roles {
			if role.ID <= after || !strings.Contains(strings.ToLower(role.Name), query) {
				continue
			}
			if len(result.Items) == filter.Limit {
				cursor := encodeTeamRoleCursor(result.Items[len(result.Items)-1].ID, actorID, teamID, filter.Query, record.ETag)
				result.NextCursor = &cursor
				break
			}
			result.Items = append(result.Items, role)
		}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return result, catalogError(err)
}
func (s *Service) SetTeamRoles(ctx context.Context, actorID, teamID, etag string, input TeamRoleInput) (*TeamRolesRecord, error) {
	input.Reason = strings.TrimSpace(input.Reason)
	if !safeTeamSessionID(teamID) || !teamSessionDigest.MatchString(etag) || !validCredentialMetadataReason(input.Reason) || len(input.RoleIDs) > 100 {
		return nil, apperrors.ErrBadRequest
	}
	desired := slices.Clone(input.RoleIDs)
	slices.Sort(desired)
	for index, roleID := range desired {
		if !safeTeamSessionID(roleID) || !strings.HasPrefix(roleID, "rol_") || index > 0 && roleID == desired[index-1] {
			return nil, apperrors.ErrBadRequest
		}
	}
	var result *TeamRolesRecord
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		actor, err := exactEnabledActor(tx, actorID)
		if err != nil {
			return err
		}
		if actor.Role != entity.RoleAdmin {
			return apperrors.ErrForbidden
		}
		target, err := teamRoleTargetAccess(tx, actorID, teamID, true)
		if err != nil {
			return err
		}
		if target.Team.Status == entity.ResourceArchived {
			return catalogConflict
		}
		var locked []entity.Role
		if len(desired) > 0 {
			if err := teamRoleDefinitionQuery(tx, desired).Order("id").Clauses(clause.Locking{Strength: "UPDATE"}).Find(&locked).Error; err != nil {
				return err
			}
		}
		before, err := teamRolesRecord(tx, target)
		if err != nil {
			return err
		}
		// A matching ID set alone cannot reconcile obsolete reviewed definitions.
		if before.ETag != etag {
			return catalogConflict
		}
		if len(locked) != len(desired) {
			return apperrors.ErrBadRequest
		}
		if slices.Equal(before.RoleIDs, desired) {
			result = before
			return nil
		}
		if err := quotaExact(tx, "team_id", teamID).Delete(&entity.TeamRole{}).Error; err != nil {
			return err
		}
		for _, roleID := range desired {
			if err := tx.Create(&entity.TeamRole{TeamID: teamID, RoleID: roleID}).Error; err != nil {
				return err
			}
		}
		now := teamRoleAssignmentGeneration(target.Team.UpdatedAt, time.Now())
		if err := tx.Model(&target.Team).Update("UpdatedAt", now).Error; err != nil {
			return err
		}
		// Review the actual saved representation while retaining the Team lock.
		// Database timestamp precision/normalization must not change the validator
		// between the successful write and the next authorized read.
		if err := quotaExact(tx, "id", teamID).First(&target.Team).Error; err != nil {
			return err
		}
		result, err = teamRolesRecord(tx, target)
		if err != nil {
			return err
		}
		return appendTeamRolesAudit(tx, actorID, *before, *result, input.Reason)
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	return result, catalogError(err)
}
func appendTeamRolesAudit(tx *gorm.DB, actorID string, before, after TeamRolesRecord, reason string) error {
	type assignment struct {
		RoleIDs     []string `json:"role_ids"`
		TeamActions []string `json:"team_actions"`
	}
	raw, err := json.Marshal(struct {
		TeamID string     `json:"team_id"`
		Before assignment `json:"before"`
		After  assignment `json:"after"`
		Reason string     `json:"reason"`
	}{after.TeamID, assignment{before.RoleIDs, before.EffectiveTeamActions}, assignment{after.RoleIDs, after.EffectiveTeamActions}, reason})
	if err != nil {
		return err
	}
	auditID, err := id.NewPrefixed("aud")
	if err != nil {
		return err
	}
	details := string(raw)
	return tx.Create(&entity.AuditEvent{ID: auditID, ActorID: actorID, Action: "team.roles.replace", ResourceType: "teams", ResourceID: after.TeamID, DetailsJSON: &details, CreatedAt: time.Now().UTC()}).Error
}

// Millisecond generations are representable by every supported historical Team
// schema. Always advance the saved generation, even when the clock stalls.
func teamRoleAssignmentGeneration(previous, now time.Time) time.Time {
	next := now.UTC().Truncate(time.Millisecond)
	if !next.After(previous) {
		next = previous.UTC().Truncate(time.Millisecond).Add(time.Millisecond)
	}
	return next
}
