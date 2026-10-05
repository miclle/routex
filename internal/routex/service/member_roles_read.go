package service

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func memberRolesPeople(tx *gorm.DB, actorID, userID string, read, lock bool) (entity.User, entity.User, error) {
	var actor, subject entity.User
	if lock {
		ids := []string{actorID, userID}
		slices.Sort(ids)
		ids = slices.Compact(ids)
		for _, id := range ids {
			var u entity.User
			err := memberRolesUserQuery(tx, id, true).First(&u).Error
			if errors.Is(err, gorm.ErrRecordNotFound) || err == nil && u.ID != id {
				if id == actorID {
					return actor, subject, apperrors.ErrUnauthorized
				}
				return actor, subject, apperrors.ErrNotFound
			}
			if err != nil {
				return actor, subject, err
			}
			if id == actorID {
				actor = u
			}
			if id == userID {
				subject = u
			}
		}
	} else {
		err := memberRolesUserQuery(tx, actorID, false).Where("disabled = ? AND offboarded_at IS NULL", false).First(&actor).Error
		if errors.Is(err, gorm.ErrRecordNotFound) || err == nil && actor.ID != actorID {
			return actor, subject, apperrors.ErrUnauthorized
		}
		if err != nil {
			return actor, subject, err
		}
	}
	if actor.Disabled || actor.OffboardedAt != nil || actor.Role != entity.RoleAdmin && actor.Role != entity.RoleMember {
		return actor, subject, apperrors.ErrUnauthorized
	}
	if err := requireRegistrationAdmission(memberRolesDB(tx), actor); err != nil {
		return actor, subject, err
	}
	if read {
		for _, permission := range []string{"members.read", "roles.read"} {
			allowed, err := exactGovernancePermissionForAdmittedActor(memberRolesDB(tx), actor, permission)
			if err != nil {
				return actor, subject, err
			}
			if !allowed {
				return actor, subject, apperrors.ErrForbidden
			}
		}
	} else if actor.Role != entity.RoleAdmin {
		return actor, subject, apperrors.ErrForbidden
	}
	if !lock {
		err := memberRolesUserQuery(tx, userID, false).First(&subject).Error
		if errors.Is(err, gorm.ErrRecordNotFound) || err == nil && subject.ID != userID {
			return actor, subject, apperrors.ErrNotFound
		}
		if err != nil {
			return actor, subject, err
		}
	}
	return actor, subject, nil
}
func memberRoleIDsQuery(tx *gorm.DB, column string, ids []string) *gorm.DB {
	expressions := make([]clause.Expression, 0, len(ids))
	values := make([]any, 0, len(ids))
	for _, id := range ids {
		values = append(values, id)
		expressions = append(expressions, database.ExactText(tx, clause.Column{Name: column}, id))
	}
	if len(expressions) == 0 {
		return tx.Where("1 = 0")
	}
	// The ordinary indexed predicate is only a superset prefilter. Retain
	// every exact comparison so collation aliases never become authority.
	return tx.Where(clause.IN{Column: clause.Column{Name: column}, Values: values}).Where(clause.Or(expressions...))
}
func loadMemberRoleRows(tx *gorm.DB, ids []string) ([]entity.Role, error) {
	rows := make([]entity.Role, 0, len(ids))
	for start := 0; start < len(ids); start += memberRolesBatchSize {
		end := min(start+memberRolesBatchSize, len(ids))
		var batch []entity.Role
		if err := memberRoleIDsQuery(memberRolesDB(tx).Model(&entity.Role{}), "id", ids[start:end]).Select("ID", "Name", "Builtin", "DefinitionRevision").Limit(end - start + 1).Find(&batch).Error; err != nil {
			return nil, err
		}
		wanted := map[string]bool{}
		for _, id := range ids[start:end] {
			wanted[id] = true
		}
		for _, row := range batch {
			if !wanted[row.ID] {
				return nil, memberRolesUnavailable
			}
			delete(wanted, row.ID)
		}
		if len(wanted) != 0 {
			return nil, memberRolesUnavailable
		}
		rows = append(rows, batch...)
	}
	return rows, nil
}
func loadMemberRolePermissions(tx *gorm.DB, rows []entity.Role) (map[string][]string, error) {
	result := map[string][]string{}
	ids := []string{}
	for _, role := range rows {
		if _, dup := result[role.ID]; dup {
			return nil, memberRolesUnavailable
		}
		result[role.ID] = []string{}
		ids = append(ids, role.ID)
	}
	slices.Sort(ids)
	total := 0
	for start := 0; start < len(ids); start += memberRolesBatchSize {
		end := min(start+memberRolesBatchSize, len(ids))
		var batch []entity.RolePermission
		if err := memberRoleIDsQuery(memberRolesDB(tx).Model(&entity.RolePermission{}), "role_id", ids[start:end]).Limit(memberRolesPermissionBudget - total + 1).Find(&batch).Error; err != nil {
			return nil, err
		}
		total += len(batch)
		if total > memberRolesPermissionBudget {
			return nil, memberRolesOverflow
		}
		wanted := map[string]bool{}
		for _, id := range ids[start:end] {
			wanted[id] = true
		}
		for _, row := range batch {
			if !wanted[row.RoleID] {
				return nil, memberRolesUnavailable
			}
			result[row.RoleID] = append(result[row.RoleID], row.Permission)
			if len(result[row.RoleID]) > 100 {
				return nil, memberRolesOverflow
			}
		}
	}
	return result, nil
}
func loadMemberRoles(tx *gorm.DB, actor, subject entity.User) (*memberRolesSnapshot, error) {
	var assignments []entity.UserRole
	if err := memberRolesExact(memberRolesDB(tx).Model(&entity.UserRole{}), "user_id", subject.ID).Limit(memberRolesReadBudget + 1).Find(&assignments).Error; err != nil {
		return nil, err
	}
	if len(assignments) > memberRolesReadBudget {
		return nil, memberRolesOverflow
	}
	assigned := []string{}
	selected := map[string]bool{}
	builtin := "rol_member"
	if subject.Role == entity.RoleAdmin {
		builtin = "rol_admin"
	}
	selected[builtin] = true
	for _, row := range assignments {
		if row.UserID != subject.ID || !memberRoleID(row.RoleID) || selected[row.RoleID] {
			return nil, memberRolesUnavailable
		}
		selected[row.RoleID] = true
		assigned = append(assigned, row.RoleID)
	}
	catalogue := []string{}
	catalogueOverflow := false
	catalogueRows := []entity.Role{}
	// Ineligible readers never query an unrelated Role catalogue.
	if actor.Role == entity.RoleAdmin && subject.OffboardedAt == nil && len(assigned) <= memberRolesWriteBeforeBudget {
		if err := memberRolesDB(tx).Model(&entity.Role{}).Select("ID", "Name", "Builtin", "DefinitionRevision").Where("builtin = ?", false).Limit(memberRolesCatalogueBudget + 1).Find(&catalogueRows).Error; err != nil {
			return nil, err
		}
		catalogueOverflow = len(catalogueRows) > memberRolesCatalogueBudget
		if !catalogueOverflow {
			for _, row := range catalogueRows {
				if row.Builtin || !memberRoleID(row.ID) {
					return nil, memberRolesUnavailable
				}
				catalogue = append(catalogue, row.ID)
				selected[row.ID] = true
			}
		}
	}
	ids := []string{}
	for id := range selected {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	// Hydrate only selected identities. Catalogue metadata is validated by the same exact map.
	rows, err := loadMemberRoleRows(tx, ids)
	if err != nil {
		return nil, err
	}
	permissions, err := loadMemberRolePermissions(tx, rows)
	if err != nil {
		return nil, err
	}
	definitions := map[string]memberRoleDefinition{}
	for _, row := range rows {
		definition, err := projectMemberRoleDefinition(row, permissions[row.ID])
		if err != nil {
			return nil, err
		}
		definitions[row.ID] = definition
	}
	return projectMemberRoles(actor, subject, assigned, catalogue, definitions, catalogueOverflow)
}
func (s *Service) GetMemberRoles(ctx context.Context, actorID, userID string) (*MemberRolesWorkspace, error) {
	snapshot, err := s.readMemberRoles(ctx, actorID, userID)
	if err != nil {
		return nil, err
	}
	return &snapshot.Page, nil
}
func (s *Service) readMemberRoles(ctx context.Context, actorID, userID string) (*memberRolesSnapshot, error) {
	if err := memberMetadataIDs(actorID, userID); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var result *memberRolesSnapshot
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		actor, subject, err := memberRolesPeople(tx, actorID, userID, true, false)
		if err != nil {
			return err
		}
		result, err = loadMemberRoles(tx, actor, subject)
		return err
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return result, catalogError(err)
}
func (s *Service) MemberRoleCandidates(ctx context.Context, actorID, userID, etag string, filter MemberRoleCandidateFilter) (*MemberRoleCandidatePage, error) {
	if filter.Limit == 0 {
		filter.Limit = 25
	}
	if _, err := candidatePattern(filter.Query); err != nil || filter.Limit < 1 || filter.Limit > 50 || len(filter.Cursor) > 200 || !validMemberRoleDigest(etag) {
		return nil, apperrors.ErrBadRequest
	}
	snapshot, err := s.readMemberRoles(ctx, actorID, userID)
	if err != nil {
		return nil, err
	}
	if snapshot.Actor.Role != entity.RoleAdmin {
		return nil, apperrors.ErrForbidden
	}
	if !snapshot.Page.CanEdit || etag != snapshot.Page.ETag {
		return nil, catalogConflict
	}
	after, err := decodeTeamRoleCursor(filter.Cursor, actorID, userID, filter.Query, etag)
	if err != nil {
		return nil, err
	}
	query := strings.ToLower(strings.TrimSpace(filter.Query))
	result := &MemberRoleCandidatePage{Items: []MemberRoleSummary{}, ETag: etag}
	for _, id := range snapshot.Catalogue {
		row := snapshot.Definitions[id].Summary
		if id <= after || !strings.Contains(strings.ToLower(row.Name), query) {
			continue
		}
		if len(result.Items) == filter.Limit {
			cursor := encodeTeamRoleCursor(result.Items[len(result.Items)-1].ID, actorID, userID, filter.Query, etag)
			result.NextCursor = &cursor
			break
		}
		result.Items = append(result.Items, row)
	}
	return result, nil
}
func (s *Service) GetMemberRoleDefinition(ctx context.Context, actorID, userID, roleID, etag string) (*MemberRoleDetail, error) {
	if !safeTeamSessionID(roleID) || !validMemberRoleDigest(etag) {
		return nil, apperrors.ErrBadRequest
	}
	snapshot, err := s.readMemberRoles(ctx, actorID, userID)
	if err != nil {
		return nil, err
	}
	if etag != snapshot.Page.ETag {
		return nil, catalogConflict
	}
	allowed := roleID == snapshot.Page.BuiltinRole.ID || slices.Contains(snapshot.Assigned, roleID) || snapshot.Page.CanEdit && slices.Contains(snapshot.Catalogue, roleID)
	d, ok := snapshot.Definitions[roleID]
	if !allowed || !ok {
		return nil, apperrors.ErrNotFound
	}
	return &MemberRoleDetail{userID, d.Summary, slices.Clone(d.Permissions), etag}, nil
}
