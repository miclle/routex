package service

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type roleDefinitionSnapshot struct {
	Actor  entity.User
	Role   entity.Role
	Record RoleDefinitionRecord
}

func roleDefinitionActor(tx *gorm.DB, actorID string, read, lock bool) (entity.User, runtimeAdmissionProof, error) {
	actor, err := registrationAdmittedUser(memberRolesDB(tx), actorID, lock)
	if err != nil {
		return actor, runtimeAdmissionProof{}, err
	}
	if actor.Role != entity.RoleAdmin && actor.Role != entity.RoleMember {
		return actor, runtimeAdmissionProof{}, roleDefinitionUnavailable
	}
	if !read && actor.Role != entity.RoleAdmin {
		return actor, runtimeAdmissionProof{}, apperrors.ErrForbidden
	}
	if read {
		allowed, err := exactGovernancePermission(memberRolesDB(tx), actor, "roles.read")
		if err != nil {
			return actor, runtimeAdmissionProof{}, err
		}
		if !allowed {
			return actor, runtimeAdmissionProof{}, apperrors.ErrForbidden
		}
	}
	applications, err := loadRegistrationApplications(memberRolesDB(tx), []entity.User{actor})
	if err != nil {
		return actor, runtimeAdmissionProof{}, err
	}
	admission, proof := registrationAdmission(actor, applications)
	if !admission.AdmissionEligible {
		return actor, proof, apperrors.ErrUnauthorized
	}
	return actor, proof, nil
}

func loadRoleDefinition(tx *gorm.DB, roleID string, lock bool) (entity.Role, []string, error) {
	var role entity.Role
	q := memberRolesExact(memberRolesDB(tx).Model(&entity.Role{}), "id", roleID).
		Select("ID", "Name", "NameKey", "Builtin", "CreatedAt", "DefinitionRevision")
	if lock {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	if err := q.First(&role).Error; err != nil {
		return role, nil, err
	}
	if role.ID != roleID {
		return role, nil, apperrors.ErrNotFound
	}
	var rows []entity.RolePermission
	if err := memberRolesExact(memberRolesDB(tx).Model(&entity.RolePermission{}), "role_id", roleID).
		Order("permission").Limit(roleDefinitionPermissionBudget + 1).Find(&rows).Error; err != nil {
		return role, nil, err
	}
	if len(rows) > roleDefinitionPermissionBudget {
		return role, nil, roleDefinitionOverflow
	}
	permissions := make([]string, 0, len(rows))
	for _, row := range rows {
		if row.RoleID != roleID {
			return role, nil, roleDefinitionUnavailable
		}
		permissions = append(permissions, row.Permission)
	}
	return role, permissions, nil
}

func roleDefinitionCatalogue() ([]string, error) {
	permissions := AssignablePermissions()
	if len(permissions) > roleDefinitionPermissionBudget {
		return nil, roleDefinitionOverflow
	}
	slices.Sort(permissions)
	for i, code := range permissions {
		if !memberRolePermissionCode(code) || i > 0 && permissions[i-1] == code {
			return nil, roleDefinitionUnavailable
		}
	}
	return permissions, nil
}

func roleDefinitionIdentity(role entity.Role) (*string, error) {
	birth := role.CreatedAt.UTC()
	if birth.IsZero() || birth.Year() < 1 || birth.Year() > 9999 {
		return nil, nil
	}
	proof, err := teamQuotaHash(struct {
		Version   string
		ID        string
		CreatedAt time.Time
	}{"role.definition.identity.v1", role.ID, birth})
	if err != nil {
		return nil, err
	}
	return &proof, nil
}

func projectRoleDefinition(actor entity.User, admission runtimeAdmissionProof, role entity.Role, permissions []string) (*roleDefinitionSnapshot, error) {
	if !validMemberRoleDigest(actor.MemberRoleRevision) {
		return nil, roleDefinitionUnavailable
	}
	if len(permissions) > roleDefinitionPermissionBudget {
		return nil, roleDefinitionOverflow
	}
	definition, err := projectMemberRoleDefinition(role, permissions)
	if err != nil {
		return nil, roleDefinitionUnavailable
	}
	catalogue, err := roleDefinitionCatalogue()
	if err != nil {
		return nil, err
	}
	identity, err := roleDefinitionIdentity(role)
	if err != nil {
		return nil, roleDefinitionUnavailable
	}
	admission.CreatedAt = admission.CreatedAt.UTC()
	admission.ApplicationCreatedAt = admission.ApplicationCreatedAt.UTC()
	canEdit := actor.Role == entity.RoleAdmin && admission.Eligible && !role.Builtin && identity != nil
	review, err := teamQuotaHash(struct {
		Version                string
		ActorID, ActorRole     string
		ActorCreatedAt         time.Time
		ActorRevision          string
		Admission              runtimeAdmissionProof
		RoleID, DefinitionETag string
		IdentityETag           *string
		AvailablePermissions   []string
		CanEdit                bool
	}{"role.definition.review.v1", actor.ID, actor.Role, actor.CreatedAt.UTC(), actor.MemberRoleRevision, admission, role.ID, definition.Summary.DefinitionETag, identity, catalogue, canEdit})
	if err != nil {
		return nil, roleDefinitionUnavailable
	}
	return &roleDefinitionSnapshot{Actor: actor, Role: role, Record: RoleDefinitionRecord{
		ID: role.ID, Name: role.Name, Builtin: role.Builtin,
		Permissions: definition.Permissions, AvailablePermissions: catalogue,
		DefinitionETag: definition.Summary.DefinitionETag, IdentityETag: identity,
		ReviewETag: review, CanEdit: canEdit,
	}}, nil
}

func readRoleDefinition(tx *gorm.DB, actorID, roleID string, read, lock bool) (*roleDefinitionSnapshot, error) {
	actor, admission, err := roleDefinitionActor(tx, actorID, read, lock)
	if err != nil {
		return nil, err
	}
	role, permissions, err := loadRoleDefinition(tx, roleID, lock)
	if err != nil {
		return nil, err
	}
	return projectRoleDefinition(actor, admission, role, permissions)
}

func roleDefinitionError(err error) error {
	if err == nil {
		return nil
	}
	var app *apperrors.Error
	if errors.As(err, &app) {
		return app
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return apperrors.ErrNotFound
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return catalogConflict
	}
	return roleDefinitionUnavailable
}

// GetRoleDefinition requires independent current global Role read authority.
func (s *Service) GetRoleDefinition(ctx context.Context, actorID, roleID string) (*RoleDefinitionRecord, error) {
	if err := roleDefinitionIDs(actorID, roleID); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var snapshot *roleDefinitionSnapshot
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		snapshot, err = readRoleDefinition(tx, actorID, roleID, true, false)
		return err
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, roleDefinitionError(err)
	}
	return &snapshot.Record, nil
}
