package database

import (
	"errors"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/miclle/routex/pkg/secret"
	"gorm.io/gorm/schema"
)

func TestDutyRoleV68FrozenSchema(t *testing.T) {
	for _, test := range []struct {
		model   any
		table   string
		columns []string
	}{
		{&dutyRoleV68{}, "roles", []string{"id", "name", "name_key", "builtin", "created_at", "definition_revision", "description"}},
		{&dutyPermissionV68{}, "role_permissions", []string{"role_id", "permission"}},
	} {
		parsed, err := schema.Parse(test.model, &sync.Map{}, schema.NamingStrategy{})
		if err != nil {
			t.Fatal(err)
		}
		if parsed.Table != test.table || !reflect.DeepEqual(parsed.DBNames, test.columns) || len(parsed.Relationships.Relations) != 0 || len(parsed.ParseIndexes()) != 0 {
			t.Fatal("unfrozen schema", parsed.Table, parsed.DBNames)
		}
	}
}

func TestDutyRoleV68CollisionAndPartialValidation(t *testing.T) {
	seeds := dutySeedsV68()
	var roles []dutyRoleV68
	var permissions []dutyPermissionV68
	for _, seed := range seeds {
		row := seed.Role
		row.CreatedAt = time.Date(2026, 10, 6, 1, 2, 3, 456000000, time.UTC)
		roles = append(roles, row)
		if row.NameKey == secret.SHA256Hex(row.Name) || len(row.DefinitionRevision) != 64 || row.DefinitionRevision == strings.Repeat("0", 64) || !slices.IsSorted(seed.Permissions) {
			t.Fatal("unprotected namespace/revision/grants")
		}
		for _, permission := range seed.Permissions {
			permissions = append(permissions, dutyPermissionV68{RoleID: row.ID, Permission: permission})
		}
	}
	if len(roles) != 3 || len(permissions) != 17 {
		t.Fatal("frozen seed cardinality")
	}
	if err := validateDutyRowsV68(seeds, roles, permissions, true); err != nil {
		t.Fatal(err)
	}
	if err := validateDutyRowsV68(seeds, roles[:1], permissions[:1], false); err != nil {
		t.Fatal("compatible partial rejected", err)
	}
	if err := validateDutyRowsV68(seeds, roles[:1], permissions[:1], true); !errors.Is(err, errDutySeedV68) {
		t.Fatal("incomplete seed accepted")
	}
	for _, test := range []struct {
		name   string
		change func(*dutyRoleV68)
	}{
		{"alias", func(r *dutyRoleV68) { r.ID = strings.ToUpper(r.ID) }},
		{"space", func(r *dutyRoleV68) { r.ID += " " }},
		{"custom reserved", func(r *dutyRoleV68) { r.Builtin = false }},
		{"name", func(r *dutyRoleV68) { r.Name += " changed" }},
		{"normal name key", func(r *dutyRoleV68) { r.NameKey = secret.SHA256Hex(r.Name) }},
		{"description", func(r *dutyRoleV68) { r.Description += " changed" }},
		{"revision", func(r *dutyRoleV68) { r.DefinitionRevision = strings.Repeat("a", 64) }},
		{"unknown birth", func(r *dutyRoleV68) { r.CreatedAt = time.Time{} }},
	} {
		t.Run(test.name, func(t *testing.T) {
			changed := slices.Clone(roles)
			test.change(&changed[0])
			if !errors.Is(validateDutyRowsV68(seeds, changed, permissions, false), errDutySeedV68) {
				t.Fatal("incompatible retained row adopted")
			}
		})
	}
	for _, test := range []struct {
		name string
		rows []dutyPermissionV68
	}{
		{"extra protected grant", append(slices.Clone(permissions), dutyPermissionV68{RoleID: "rol_finance", Permission: "roles.write"})},
		{"case alias", []dutyPermissionV68{{RoleID: "rol_finance", Permission: "PRICES.READ"}}},
		{"role alias", []dutyPermissionV68{{RoleID: "ROL_FINANCE", Permission: "prices.read"}}},
		{"orphan", []dutyPermissionV68{{RoleID: "rol_missing", Permission: "prices.read"}}},
		{"duplicate", append(slices.Clone(permissions), permissions[0])},
	} {
		t.Run(test.name, func(t *testing.T) {
			if !errors.Is(validateDutyRowsV68(seeds, roles, test.rows, false), errDutySeedV68) {
				t.Fatal("unexpected grant adopted")
			}
		})
	}
	// SQL timestamp location representation is immaterial, but the nonzero birth
	// remains a retained immutable value; no normalization or rewrite is needed.
	changed := slices.Clone(roles)
	changed[0].CreatedAt = changed[0].CreatedAt.In(time.FixedZone("retained", 3600))
	if err := validateDutyRowsV68(seeds, changed, permissions, true); err != nil {
		t.Fatal(err)
	}
}
