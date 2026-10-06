package database

import (
	"errors"
	"slices"
	"time"

	"github.com/miclle/routex/pkg/secret"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Frozen V68 is data-only. V6/V56/V67 own the existing schema; this step
// never derives grants from the current permission catalogue or rewrites roles.
type dutyRoleV68 struct {
	ID                 string    `gorm:"primaryKey;size:30"`
	Name               string    `gorm:"size:100;not null"`
	NameKey            string    `gorm:"size:64;not null"`
	Builtin            bool      `gorm:"not null"`
	CreatedAt          time.Time `gorm:"not null"`
	DefinitionRevision string    `gorm:"size:64;not null"`
	Description        string    `gorm:"size:2000;not null"`
}

func (dutyRoleV68) TableName() string { return "roles" }

type dutyPermissionV68 struct {
	RoleID     string `gorm:"primaryKey;size:30"`
	Permission string `gorm:"primaryKey;size:80"`
}

func (dutyPermissionV68) TableName() string { return "role_permissions" }

type dutySeedV68 struct {
	Role        dutyRoleV68
	Permissions []string
}

const dutyNameNamespaceV68 = "\x00routex:builtin-duty:v1\x00"

var errDutySeedV68 = errors.New("incompatible retained duty role seed")

func dutySeedsV68() []dutySeedV68 {
	seeds := []dutySeedV68{
		{Role: dutyRoleV68{ID: "rol_procurement", Name: "Procurement", Description: "Read model, price, and Provider configuration."}, Permissions: []string{"models.read_all", "prices.read", "providers.read"}},
		{Role: dutyRoleV68{ID: "rol_finance", Name: "Finance", Description: "Manage Team monetary caps and read price and Provider configuration."}, Permissions: []string{"prices.read", "providers.read", "teams.money.write"}},
		{Role: dutyRoleV68{ID: "rol_operations", Name: "Operations", Description: "Manage Provider, egress, system, and Team Token operations; read platform calls."}, Permissions: []string{"calls.read_all", "egress.read", "egress.test", "egress.write", "models.read_all", "prices.read", "providers.read", "providers.write", "system.read", "system.write", "teams.tokens.write"}},
	}
	for i := range seeds {
		role := &seeds[i].Role
		role.Builtin = true
		role.NameKey = secret.SHA256Hex(dutyNameNamespaceV68 + role.ID)
		contents := dutyNameNamespaceV68 + role.ID + "\x00" + role.Name + "\x00" + role.Description
		for _, permission := range seeds[i].Permissions {
			contents += "\x00" + permission
		}
		role.DefinitionRevision = secret.SHA256Hex(contents)
	}
	return seeds
}

// Partial seeds may contain only a subset of the exact frozen grants. Extra,
// aliased or incompatible rows are never adopted, deleted or silently repaired.
func validateDutyRowsV68(seeds []dutySeedV68, roles []dutyRoleV68, permissions []dutyPermissionV68, complete bool) error {
	byID := make(map[string]dutySeedV68, len(seeds))
	for _, seed := range seeds {
		byID[seed.Role.ID] = seed
	}
	found := make(map[string]bool, len(roles))
	for _, row := range roles {
		seed, ok := byID[row.ID]
		want := seed.Role
		if !ok || found[row.ID] || !row.Builtin || row.Name != want.Name || row.NameKey != want.NameKey || row.Description != want.Description || row.DefinitionRevision != want.DefinitionRevision || row.CreatedAt.IsZero() {
			return errDutySeedV68
		}
		found[row.ID] = true
	}
	seen := make(map[dutyPermissionV68]bool, len(permissions))
	for _, row := range permissions {
		seed, ok := byID[row.RoleID]
		if !ok || !found[row.RoleID] || seen[row] || !slices.Contains(seed.Permissions, row.Permission) {
			return errDutySeedV68
		}
		seen[row] = true
	}
	if complete {
		for _, seed := range seeds {
			if !found[seed.Role.ID] {
				return errDutySeedV68
			}
			for _, permission := range seed.Permissions {
				if !seen[dutyPermissionV68{RoleID: seed.Role.ID, Permission: permission}] {
					return errDutySeedV68
				}
			}
		}
	}
	return nil
}

func dutyRoleMigration(db *gorm.DB) error {
	seeds := dutySeedsV68()
	ids := make([]string, len(seeds))
	for i, seed := range seeds {
		ids[i] = seed.Role.ID
	}
	return db.Transaction(func(tx *gorm.DB) error {
		read := func() ([]dutyRoleV68, []dutyPermissionV68, error) {
			var roles []dutyRoleV68
			// This bounded migration-only candidate expression deliberately detects
			// case/space aliases on both collations; exact rows are checked above.
			if err := tx.Where("LOWER(TRIM(id)) IN ?", ids).Order("id").Limit(4).Clauses(clause.Locking{Strength: "UPDATE"}).Find(&roles).Error; err != nil {
				return nil, nil, err
			}
			var permissions []dutyPermissionV68
			if err := tx.Where("role_id IN ?", ids).Order("role_id, permission").Limit(18).Find(&permissions).Error; err != nil {
				return nil, nil, err
			}
			return roles, permissions, nil
		}
		roles, permissions, err := read()
		if err != nil {
			return err
		}
		if err := validateDutyRowsV68(seeds, roles, permissions, false); err != nil {
			return err
		}
		now := time.Now().UTC().Truncate(time.Millisecond)
		for _, seed := range seeds {
			if !slices.ContainsFunc(roles, func(row dutyRoleV68) bool { return row.ID == seed.Role.ID }) {
				row := seed.Role
				row.CreatedAt = now
				if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; err != nil {
					return err
				}
			}
			for _, permission := range seed.Permissions {
				row := dutyPermissionV68{RoleID: seed.Role.ID, Permission: permission}
				if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; err != nil {
					return err
				}
			}
		}
		roles, permissions, err = read()
		if err != nil {
			return err
		}
		return validateDutyRowsV68(seeds, roles, permissions, true)
	})
}
