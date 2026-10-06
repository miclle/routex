package service

import (
	"context"
	"database/sql"
	"slices"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/secret"
	"gorm.io/gorm"
)

func roleDefinitionAssignable(permissions, catalogue []string) error {
	for _, code := range permissions {
		if !slices.Contains(catalogue, code) {
			return apperrors.ErrBadRequest
		}
	}
	return nil
}

func reviewRoleDefinition(snapshot *roleDefinitionSnapshot, etag string, input RoleDefinitionInput) error {
	if snapshot.Actor.Role != entity.RoleAdmin || snapshot.Role.Builtin {
		return apperrors.ErrForbidden
	}
	if snapshot.Record.IdentityETag == nil || input.IdentityETag != *snapshot.Record.IdentityETag {
		return catalogConflict
	}
	if err := roleDefinitionAssignable(input.Permissions, snapshot.Record.AvailablePermissions); err != nil {
		return err
	}
	// An equal definition confirms current contents, never the historical request.
	// Incarnation, protected authority and current assignability still precede equality.
	if snapshot.Role.Name == input.Name && snapshot.Role.Description == input.Description && slices.Equal(snapshot.Record.Permissions, input.Permissions) {
		return nil
	}
	if etag != snapshot.Record.ReviewETag {
		return catalogConflict
	}
	return nil
}

// SetReviewedRoleDefinition changes an existing Role under intrinsic administrator authority.
func (s *Service) SetReviewedRoleDefinition(ctx context.Context, actorID, roleID, etag string, input RoleDefinitionInput) (*RoleDefinitionResult, error) {
	if err := roleDefinitionIDs(actorID, roleID); err != nil {
		return nil, err
	}
	if !validMemberRoleDigest(etag) {
		return nil, apperrors.ErrBadRequest
	}
	if err := validateRoleDefinitionInput(input); err != nil {
		return nil, err
	}
	input.Permissions = slices.Clone(input.Permissions)
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if _, err := s.mutateRoleDefinition(ctx, actorID, roleID, etag, input, true, false); err != nil {
		return nil, roleDefinitionError(err)
	}
	return s.confirmRoleDefinition(ctx, actorID, roleID, input)
}

// Both the strict public editor and trusted SaveRole adapter share this durable
// fence and atomic engine. Only the trusted adapter may create, and it has no
// captured public incarnation review; it preserves existing zero-birth fixtures.
func (s *Service) mutateRoleDefinition(ctx context.Context, actorID, roleID, etag string, input RoleDefinitionInput, reviewed, creating bool) (*RoleRecord, error) {
	var result RoleRecord
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(memberRolesDB(tx)); err != nil {
			return err
		}
		actor, admission, err := roleDefinitionActor(tx, actorID, false, true)
		if err != nil {
			if !reviewed && err == apperrors.ErrUnauthorized {
				return apperrors.ErrForbidden
			}
			return err
		}
		catalogue, err := roleDefinitionCatalogue()
		if err != nil {
			return err
		}
		if err := roleDefinitionAssignable(input.Permissions, catalogue); err != nil {
			return err
		}
		role := entity.Role{ID: roleID, Name: input.Name, Description: input.Description, NameKey: secret.SHA256Hex(input.Name)}
		var before []string
		audit := ""
		if !creating {
			role, before, err = loadRoleDefinition(tx, roleID, true)
			if err != nil {
				return err
			}
			if !validRoleDescription(role.Description, true) {
				return roleDefinitionUnavailable
			}
			if role.Builtin {
				return apperrors.ErrForbidden
			}
			if !reviewed {
				input.Description = role.Description
			}
			slices.Sort(before)
			if reviewed {
				snapshot, err := projectRoleDefinition(actor, admission, role, before)
				if err != nil {
					return err
				}
				before = snapshot.Record.Permissions
				if err := reviewRoleDefinition(snapshot, etag, input); err != nil {
					return err
				}
			}
			if role.Name == input.Name && role.Description == input.Description && slices.Equal(before, input.Permissions) {
				result = RoleRecord{Role: role, Permissions: slices.Clone(before)}
				return nil
			}
			if reviewed {
				audit, err = encodeRoleDefinitionAudit(role.ID, input.Reason,
					roleDefinitionAuditValues{Name: role.Name, Permissions: before, Description: &role.Description},
					roleDefinitionAuditValues{Name: input.Name, Permissions: input.Permissions, Description: &input.Description})
				if err != nil {
					return err
				}
			}
		}
		role.DefinitionRevision, err = newMemberRoleRevision()
		if err != nil {
			return err
		}
		role.Name = input.Name
		role.Description = input.Description
		role.NameKey = secret.SHA256Hex(input.Name)
		if creating {
			if reviewed {
				return apperrors.ErrBadRequest
			}
			if err := memberRolesDB(tx).Create(&role).Error; err != nil {
				return err
			}
		} else {
			updated := memberRolesExact(memberRolesDB(tx).Model(&entity.Role{}), "id", role.ID).
				Updates(map[string]any{"name": role.Name, "description": role.Description, "name_key": role.NameKey, "definition_revision": role.DefinitionRevision})
			if updated.Error != nil {
				return updated.Error
			}
			if updated.RowsAffected != 1 {
				return catalogConflict
			}
		}
		if creating || !slices.Equal(before, input.Permissions) {
			if err := memberRolesExact(memberRolesDB(tx), "role_id", role.ID).Delete(&entity.RolePermission{}).Error; err != nil {
				return err
			}
			for _, permission := range input.Permissions {
				if err := memberRolesDB(tx).Create(&entity.RolePermission{RoleID: role.ID, Permission: permission}).Error; err != nil {
					return err
				}
			}
		}
		if reviewed {
			err = appendRoleDefinitionAudit(tx, actor.ID, role.ID, audit)
		} else {
			err = appendAudit(memberRolesDB(tx), actor.ID, "role.save", "role", role.ID)
		}
		if err != nil {
			return err
		}
		result = RoleRecord{Role: role, Permissions: slices.Clone(input.Permissions)}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func (s *Service) confirmRoleDefinition(ctx context.Context, actorID, roleID string, input RoleDefinitionInput) (*RoleDefinitionResult, error) {
	var result *RoleDefinitionResult
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		// Confirmation uses independent writer authority, never public roles.read.
		current, err := readRoleDefinition(tx, actorID, roleID, false, false)
		if err != nil {
			return err
		}
		if err := reviewRoleDefinition(current, current.Record.ReviewETag, input); err != nil {
			return err
		}
		if current.Role.Name != input.Name || current.Role.Description != input.Description || !slices.Equal(current.Record.Permissions, input.Permissions) {
			return catalogConflict
		}
		result = &RoleDefinitionResult{ID: roleID, Name: current.Role.Name, Description: current.Role.Description,
			Permissions: slices.Clone(current.Record.Permissions), IdentityETag: *current.Record.IdentityETag,
			ETag: current.Record.ReviewETag, Confirmation: "current_role_definition", Effect: "current_database"}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, roleDefinitionError(err)
	}
	return result, nil
}
