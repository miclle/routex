package service

import "testing"

func TestCustomRolePermissionBoundary(t *testing.T) {
	for _, tc := range []struct {
		name        string
		permissions []string
		valid       bool
	}{
		{"reader", []string{"members.read", "providers.read"}, true},
		{"empty role", []string{}, true},
		{"reserved role mutation", []string{"roles.write"}, false},
		{"reserved registration", []string{"registration.write"}, false},
		{"unknown action", []string{"system.superuser"}, false},
		{"duplicate action", []string{"members.read", "members.read"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validateRoleInput("Test role", tc.permissions)
			if (err == nil) != tc.valid {
				t.Fatalf("validation error = %v, valid = %v", err, tc.valid)
			}
		})
	}
}
