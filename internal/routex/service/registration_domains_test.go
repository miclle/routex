package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
)

func registrationDomainBoundary() []string {
	result := make([]string, 32)
	for i := range result {
		n := 50
		if i == 0 {
			n--
		}
		result[i] = fmt.Sprintf("d%02d", i) + strings.Repeat("a", n) + ".invalid"
	}
	return result
}
func TestRegistrationDomainsCanonicalAndExactMatching(t *testing.T) {
	original := []string{" EXAMPLE.INVALID ", "a.example.invalid"}
	before := slices.Clone(original)
	canonical, err := canonicalRegistrationDomains(original)
	if err != nil || !reflect.DeepEqual(canonical, []string{"a.example.invalid", "example.invalid"}) || !reflect.DeepEqual(before, original) {
		t.Fatal("normalization mutated input or wrong canonical review", err)
	}
	for _, c := range []struct {
		email   string
		allowed bool
	}{{"member@example.invalid", true}, {"member@a.example.invalid", true}, {"member@b.example.invalid", false}, {"member@notexample.invalid", false}, {"member@example.invalid.attacker.invalid", false}, {"member@example.invalid.", false}, {"member@EXAMPLE.INVALID", false}, {"member@éxample.invalid", false}, {`"x@y"@example.invalid`, true}, {"member@[127.0.0.1]", false}} {
		if got := registrationEmailAllowed(c.email, canonical); got != c.allowed {
			t.Fatal("nonexact email domain admitted", c.email)
		}
	}
	normalized, valid := normalizeEmail(" MEMBER@EXAMPLE.INVALID ")
	if !valid || !registrationEmailAllowed(normalized, canonical) {
		t.Fatal("existing normalized mailbox case changed")
	}
	if !registrationEmailAllowed("retained-valid@localhost", []string{}) {
		t.Fatal("unrestricted policy narrowed existing email grammar")
	}
}
func TestRegistrationDomainsInvalidAndBounds(t *testing.T) {
	for _, value := range []string{"", "localhost", "*.example.invalid", ".example.invalid", "example.invalid.", "a..invalid", "a_b.invalid", "-a.invalid", "a-.invalid", "a@invalid", "https://example.invalid", "example.invalid:443", "example.invalid/path", "éxample.invalid", "a\t.invalid", "\texample.invalid", "127.0.0.1", "127.0.0.01", "127.1", strings.Repeat("a", 64) + ".invalid", strings.Repeat("a", 63) + "." + strings.Repeat("b", 63) + "." + strings.Repeat("c", 63) + "." + strings.Repeat("d", 63)} {
		if _, err := canonicalRegistrationDomains([]string{value}); !errors.Is(err, apperrors.ErrBadRequest) {
			t.Fatal("unsafe domain accepted", value)
		}
	}
	if _, err := canonicalRegistrationDomains([]string{"A.invalid", "a.invalid"}); err == nil {
		t.Fatal("normalized duplicate silently removed")
	}
	// Only wholly numeric dotted names are excluded; no DNS/IP lookup or normalization.
	for _, domain := range []string{"corp.123", "1.corp"} {
		if result, err := canonicalRegistrationDomains([]string{domain}); err != nil || result[0] != domain {
			t.Fatal("genuine ASCII domain narrowed", err)
		}
	}
	if _, err := canonicalRegistrationDomains(nil); err == nil {
		t.Fatal("missing complete array accepted")
	}
	boundary := registrationDomainBoundary()
	raw, _ := json.Marshal(boundary)
	if len(raw) != 2048 {
		t.Fatal("fixture byte boundary wrong", len(raw))
	}
	if _, err := canonicalRegistrationDomains(boundary); err != nil {
		t.Fatal("supported exact boundary rejected", err)
	}
	boundary[0] = "a" + boundary[0]
	if _, err := canonicalRegistrationDomains(boundary); err == nil {
		t.Fatal("2049-byte policy accepted")
	}
	if _, err := canonicalRegistrationDomains(append(registrationDomainBoundary(), "z.invalid")); err == nil {
		t.Fatal("33 domains accepted")
	}
}
func TestRegistrationStoredDomainsAndCompletePolicyReview(t *testing.T) {
	for _, raw := range []string{"", `null`, `["EXAMPLE.invalid"]`, `["b.invalid","a.invalid"]`, `["a.invalid","a.invalid"]`, `[ "a.invalid" ]`, `["a.invalid"] `, `["\u0061.invalid"]`, `["*.invalid"]`} {
		if _, err := registrationStoredDomains(raw); !errors.Is(err, registrationApprovalUnavailable) {
			t.Fatal("invalid retained policy became unrestricted", raw)
		}
	}
	for _, raw := range []string{`[]`, `["a.invalid"]`} {
		if _, err := registrationStoredDomains(raw); err != nil {
			t.Fatal(err)
		}
	}
	v := entity.GovernanceSetting{RegistrationPolicyRevision: memberRoleBaseline, RegistrationAllowedEmailDomains: `[]`}
	a := registrationPolicyETag(v)
	v.RegistrationAllowedEmailDomains = `["a.invalid"]`
	if a == registrationPolicyETag(v) {
		t.Fatal("domain-only change omitted from strong review")
	}
	v.RegistrationAllowedEmailDomains = `[]`
	v.RegistrationPolicyRevision = strings.Repeat("a", 64)
	if a == registrationPolicyETag(v) {
		t.Fatal("domain ABA revision omitted")
	}
	if !errors.Is(requireRegistrationEmail(`["a.invalid"]`, "x@b.invalid"), apperrors.ErrForbidden) {
		t.Fatal("server gate did not reject")
	}
	if requireRegistrationEmail(`[]`, "x@b.invalid") != nil {
		t.Fatal("server open gate rejected")
	}
	if !errors.Is(requireRegistrationEmail(``, "x@a.invalid"), registrationApprovalUnavailable) {
		t.Fatal("unknown policy failed open")
	}
	var input RegistrationPolicyInput
	raw := `{"enabled":true,"approval_required":false,"allowed_email_domains":[" B.INVALID ","a.invalid"],"reason":"Reviewed domains"}`
	if err := json.Unmarshal([]byte(raw), &input); err != nil || !slices.Equal(input.AllowedEmailDomains, []string{"a.invalid", "b.invalid"}) {
		t.Fatal("complete canonical policy input wrong", err)
	}
}
