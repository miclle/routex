package service

import (
	"context"
	"slices"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/id"
	"github.com/miclle/routex/pkg/secret"
)

// AvailablePermissions defines the implemented platform resource/action surface.
// Personal resource access and model call grants remain independently scoped.
var AvailablePermissions = []string{"members.read", "members.write", "roles.read", "roles.write", "registration.write", "providers.read", "providers.write", "models.read_all", "models.write", "calls.read_all", "audit.read", "system.read", "teams.read_all", "teams.write", "teams.models.write", "projects.read_all", "projects.write", "projects.models.write", "prices.read", "prices.write", "limits.users.write", "limits.settings.write", "projects.limits.write", "site.write", "announcements.write", "egress.read", "egress.write", "egress.test", "smtp.read", "smtp.write", "smtp.test", "storage.read", "storage.write", "storage.test"}

type RoleRecord struct {
	Role        entity.Role
	Permissions []string
}
type MemberRecord struct {
	User    entity.User
	RoleIDs []string
}
type MemberFilter struct {
	Query  string
	Status string
	Role   string
	Cursor string
	Limit  int
}
type MemberPage struct {
	Members    []MemberRecord
	NextCursor string
}

func lockGovernance(tx *gorm.DB) error {
	var setting entity.GovernanceSetting
	return tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&setting, 1).Error
}

func activePlatformAdmin(db *gorm.DB, actorID string) error {
	var actor entity.User
	if err := db.Where("id = ? AND disabled = ? AND role = ?", actorID, false, entity.RoleAdmin).First(&actor).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return apperrors.ErrForbidden
		}
		return err
	}
	return nil
}

func permissionsFor(db *gorm.DB, userID string) ([]string, error) {
	var user entity.User
	if err := db.Where("id = ? AND disabled = ?", userID, false).First(&user).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, apperrors.ErrUnauthorized
		}
		return nil, err
	}
	permissions := []string{}
	query := db.Table("role_permissions p").Select("DISTINCT p.permission").Joins("LEFT JOIN user_roles u ON u.role_id = p.role_id AND u.user_id = ?", userID)
	builtin := "rol_member"
	if user.Role == entity.RoleAdmin {
		builtin = "rol_admin"
	}
	if err := query.Where("p.role_id = ? OR u.user_id = ?", builtin, userID).Order("p.permission").Scan(&permissions).Error; err != nil {
		return nil, err
	}
	return permissions, nil
}

func (s *Service) Permissions(ctx context.Context, userID string) ([]string, error) {
	result, err := permissionsFor(s.authDB(ctx), userID)
	return result, catalogError(err)
}
func (s *Service) Authorize(ctx context.Context, userID, permission string) error {
	if !slices.Contains(AvailablePermissions, permission) {
		return apperrors.ErrForbidden
	}
	permissions, err := s.Permissions(ctx, userID)
	if err != nil {
		return err
	}
	if !slices.Contains(permissions, permission) {
		return apperrors.ErrForbidden
	}
	return nil
}

func authorizeGovernance(db *gorm.DB, userID, permission string) error {
	permissions, err := permissionsFor(db, userID)
	if err != nil {
		return err
	}
	if !slices.Contains(permissions, permission) {
		return apperrors.ErrForbidden
	}
	return nil
}

func rolePermissions(db *gorm.DB, roleID string) ([]string, error) {
	result := []string{}
	err := db.Model(&entity.RolePermission{}).Where("role_id = ?", roleID).Order("permission").Pluck("permission", &result).Error
	return result, err
}
func memberRecord(db *gorm.DB, userID string) (*MemberRecord, error) {
	result := &MemberRecord{RoleIDs: []string{}}
	if err := db.Omit("password_hash").First(&result.User, "id = ?", userID).Error; err != nil {
		return nil, err
	}
	if err := db.Model(&entity.UserRole{}).Where("user_id = ?", userID).Order("role_id").Pluck("role_id", &result.RoleIDs).Error; err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Service) ListRoles(ctx context.Context, actorID string) ([]RoleRecord, error) {
	db := s.authDB(ctx)
	if err := authorizeGovernance(db, actorID, "roles.read"); err != nil {
		return nil, catalogError(err)
	}
	roles := []entity.Role{}
	if err := db.Order("builtin DESC, name").Find(&roles).Error; err != nil {
		return nil, catalogError(err)
	}
	result := make([]RoleRecord, 0, len(roles))
	for _, role := range roles {
		permissions, err := rolePermissions(db, role.ID)
		if err != nil {
			return nil, catalogError(err)
		}
		result = append(result, RoleRecord{Role: role, Permissions: permissions})
	}
	return result, nil
}

// AssignablePermissions excludes identity-policy powers that require the
// protected administrator identity, even when a custom role is otherwise broad.
func AssignablePermissions() []string {
	result := []string{}
	for _, permission := range AvailablePermissions {
		if permission != "roles.write" && permission != "registration.write" {
			result = append(result, permission)
		}
	}
	return result
}

func validateRoleInput(name string, permissions []string) error {
	if !validCatalogLabel(name) || len(permissions) > len(AvailablePermissions) {
		return apperrors.ErrBadRequest
	}
	seen := map[string]bool{}
	for _, permission := range permissions {
		if !slices.Contains(AssignablePermissions(), permission) || seen[permission] {
			return apperrors.ErrBadRequest
		}
		seen[permission] = true
	}
	return nil
}

func (s *Service) SaveRole(ctx context.Context, actorID, roleID, name string, permissions []string) (*RoleRecord, error) {
	name = strings.TrimSpace(name)
	if err := validateRoleInput(name, permissions); err != nil {
		return nil, err
	}
	creating := roleID == ""
	var err error
	if creating {
		roleID, err = id.NewPrefixed("rol")
		if err != nil {
			return nil, apperrors.ErrInternal
		}
	}
	role := entity.Role{ID: roleID, Name: name, NameKey: secret.SHA256Hex(name)}
	db := s.authDB(ctx)
	err = db.Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		if err := activePlatformAdmin(tx, actorID); err != nil {
			return err
		}
		if !creating {
			if err := tx.First(&role, "id = ?", roleID).Error; err != nil {
				return err
			}
			if role.Builtin {
				return apperrors.ErrForbidden
			}
			role.Name = name
			role.NameKey = secret.SHA256Hex(name)
			if err := tx.Model(&role).Updates(map[string]any{"name": name, "name_key": role.NameKey}).Error; err != nil {
				return err
			}
		} else if err := tx.Create(&role).Error; err != nil {
			return err
		}
		if err := tx.Where("role_id = ?", roleID).Delete(&entity.RolePermission{}).Error; err != nil {
			return err
		}
		for _, permission := range permissions {
			if err := tx.Create(&entity.RolePermission{RoleID: roleID, Permission: permission}).Error; err != nil {
				return err
			}
		}
		return appendAudit(tx, actorID, "role.save", "role", roleID)
	})
	if err != nil {
		return nil, catalogError(err)
	}
	permissions = slices.Clone(permissions)
	slices.Sort(permissions)
	return &RoleRecord{Role: role, Permissions: permissions}, nil
}

func (s *Service) DeleteRole(ctx context.Context, actorID, roleID string) error {
	return catalogError(s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		if err := activePlatformAdmin(tx, actorID); err != nil {
			return err
		}
		var role entity.Role
		if err := tx.First(&role, "id = ?", roleID).Error; err != nil {
			return err
		}
		if role.Builtin {
			return apperrors.ErrForbidden
		}
		var assigned int64
		if err := tx.Model(&entity.UserRole{}).Where("role_id = ?", roleID).Count(&assigned).Error; err != nil {
			return err
		}
		if assigned > 0 {
			return catalogConflict
		}
		if err := tx.Where("role_id = ?", roleID).Delete(&entity.RolePermission{}).Error; err != nil {
			return err
		}
		if err := tx.Delete(&role).Error; err != nil {
			return err
		}
		return appendAudit(tx, actorID, "role.delete", "role", roleID)
	}))
}

func (s *Service) SetMemberRoles(ctx context.Context, actorID, userID string, roleIDs []string) (*MemberRecord, error) {
	if len(roleIDs) > 100 {
		return nil, apperrors.ErrBadRequest
	}
	seen := map[string]bool{}
	for _, roleID := range roleIDs {
		if roleID == "" || seen[roleID] {
			return nil, apperrors.ErrBadRequest
		}
		seen[roleID] = true
	}
	db := s.authDB(ctx)
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		if err := activePlatformAdmin(tx, actorID); err != nil {
			return err
		}
		var user entity.User
		if err := tx.First(&user, "id = ?", userID).Error; err != nil {
			return err
		}
		if len(roleIDs) > 0 {
			var count int64
			if err := tx.Model(&entity.Role{}).Where("id IN ? AND builtin = ?", roleIDs, false).Count(&count).Error; err != nil {
				return err
			}
			if count != int64(len(roleIDs)) {
				return apperrors.ErrBadRequest
			}
		}
		if err := tx.Where("user_id = ?", userID).Delete(&entity.UserRole{}).Error; err != nil {
			return err
		}
		for _, roleID := range roleIDs {
			if err := tx.Create(&entity.UserRole{UserID: userID, RoleID: roleID}).Error; err != nil {
				return err
			}
		}
		return appendAudit(tx, actorID, "member.roles.update", "user", userID)
	})
	if err != nil {
		return nil, catalogError(err)
	}
	result, err := memberRecord(db, userID)
	return result, catalogError(err)
}
