package service

import (
	"strings"
	"testing"
)

func TestLocalIdentityValidation(t *testing.T) {
	for _, tc := range []struct {
		email string
		valid bool
	}{
		{"ADMIN@Example.com", true}, {" admin@example.com ", true}, {"Admin <admin@example.com>", false}, {"bad", false}, {"admin@example.com\r\nX: evil", false},
	} {
		t.Run(tc.email, func(t *testing.T) {
			if _, valid := normalizeEmail(tc.email); valid != tc.valid {
				t.Errorf("valid = %v, want %v", valid, tc.valid)
			}
		})
	}
	for _, tc := range []struct {
		name, password string
		valid          bool
	}{
		{"short", "short", false}, {"minimum", strings.Repeat("a", 12), true}, {"maximum", strings.Repeat("a", 72), true}, {"too long", strings.Repeat("a", 73), false}, {"UTF8 bytes", strings.Repeat("密", 24), true}, {"UTF8 over maximum", strings.Repeat("密", 25), false}, {"invalid UTF8", strings.Repeat("\xff", 12), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if valid := validPassword(tc.password); valid != tc.valid {
				t.Errorf("valid = %v, want %v", valid, tc.valid)
			}
		})
	}
}

func TestCSRFIsSessionBound(t *testing.T) {
	a, b := &Authentication{Token: "first"}, &Authentication{Token: "second"}
	if !a.CheckCSRF(a.CSRFToken()) || a.CheckCSRF(b.CSRFToken()) || a.CheckCSRF("") || a.CheckCSRF(a.Token) {
		t.Fatal("CSRF token must belong to the authenticated session")
	}
}
