package service

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/miclle/routex/internal/routex/entity"
)

func domainAuditRow(raw string) entity.AuditEvent {
	return entity.AuditEvent{Action: "registration.policy.update", ResourceType: "registration", ResourceID: "1", DetailsJSON: &raw}
}
func TestRegistrationDomainsTypedAuditHistoryAndPrivacy(t *testing.T) {
	old := `{"before":{"enabled":false,"approval_required":false},"after":{"enabled":true,"approval_required":true},"reason":"Recorded policy"}`
	projected, ok := registrationApprovalAuditProjection(domainAuditRow(old))
	legacy, typed := projected.(registrationPolicyAudit)
	if !ok || !typed || !legacy.After.Enabled || !legacy.After.ApprovalRequired {
		t.Fatal("historical two-field projection changed")
	}
	historicalJSON, err := json.Marshal(projected)
	if err != nil || strings.Contains(string(historicalJSON), "allowed_email_domains") || strings.Contains(string(historicalJSON), "version") {
		t.Fatal("historical missing domains invented or rewritten")
	}
	value := registrationDomainPolicyAudit{Version: 2, Before: registrationDomainPolicyAuditValue{true, false, []string{}}, After: registrationDomainPolicyAuditValue{true, false, []string{"a.invalid"}}, Reason: "Restrict registration"}
	raw, _ := json.Marshal(value)
	projected, ok = registrationApprovalAuditProjection(domainAuditRow(string(raw)))
	current, typed := projected.(registrationDomainPolicyAudit)
	if !ok || !typed || !slices.Equal(current.After.AllowedEmailDomains, value.After.AllowedEmailDomains) {
		t.Fatal("complete typed domain-only change missing")
	}
	for _, invalid := range []string{strings.Replace(string(raw), `"version":2`, `"version":3`, 1), strings.Replace(string(raw), `["a.invalid"]`, `null`, 1), strings.Replace(string(raw), `["a.invalid"]`, `["A.invalid"]`, 1), strings.Replace(string(raw), `"reason":`, `"password":"not-public","reason":`, 1)} {
		if _, ok := registrationApprovalAuditProjection(domainAuditRow(invalid)); ok {
			t.Fatal("unsafe arbitrary domain audit exposed")
		}
	}
	value.After = value.Before
	raw, _ = json.Marshal(value)
	if _, ok := registrationApprovalAuditProjection(domainAuditRow(string(raw))); ok {
		t.Fatal("unchanged audit accepted")
	}
}
func TestRegistrationDomainsTypedAuditEightKiBBound(t *testing.T) {
	// Two maximum canonical lists plus a maximum escaped reason remain below8KiB.
	before := registrationDomainBoundary()
	after := slices.Clone(before)
	after[0] = "c" + after[0][1:]
	slices.Sort(after)
	for _, reason := range []string{strings.Repeat(`"`, 1024), strings.Repeat(`\`, 1024), strings.Repeat("<", 1024), strings.Repeat("&", 1024), "a" + strings.Repeat("\u2028", 340) + "a"} {
		value := registrationDomainPolicyAudit{Version: 2, Before: registrationDomainPolicyAuditValue{true, false, before}, After: registrationDomainPolicyAuditValue{true, true, after}, Reason: reason}
		raw, err := registrationDomainAuditJSON(value)
		if err != nil || len(raw) <= 4096 || len(raw) > 8192 || !registrationDomainPolicyAuditValid(value) {
			t.Fatal("reviewed audit bound wrong", len(raw), err)
		}
		projected, ok := registrationApprovalAuditProjection(domainAuditRow(string(raw)))
		if !ok || projected.(registrationDomainPolicyAudit).Reason != reason {
			t.Fatal("supported maximum complete audit unavailable or changed text")
		}
		if _, ok := registrationApprovalAuditProjection(domainAuditRow(string(raw) + strings.Repeat(" ", 8193-len(raw)))); ok {
			t.Fatal("oversized raw audit accepted")
		}
	}
	decision := fmt.Sprintf(`{"user_id":"usr_target","application_id":"raa_01j00000000000000000000000","before":"pending","after":"approved","reason":%q}`, strings.Repeat("r", 1024))
	decision += strings.Repeat(" ", 4097-len(decision))
	row := entity.AuditEvent{Action: "member.approval.decide", ResourceType: "user", ResourceID: "usr_target", DetailsJSON: &decision}
	if _, ok := registrationApprovalAuditProjection(row); ok {
		t.Fatal("decision legacy cap widened")
	}
}
