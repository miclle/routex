package service

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/limits"
)

func TestPersonalMonthlyBehaviorStrictInputAndCanonicalWire(t *testing.T) {
	for _, body := range []string{`{"reason":"review"}`, `{"reason":"review","tokens_month_behavior":"stop","money_month_behavior":"alert_only"}`} {
		var input LimitInput
		if err := json.Unmarshal([]byte(body), &input); err != nil {
			t.Fatal(err)
		}
		normalized, err := limits.Normalize(input.Policy)
		if err != nil {
			t.Fatal(err)
		}
		wire := userMonthlyBehaviorWire(normalized)
		if wire.TokensMonthBehavior != "stop" || wire.MoneyMonthBehavior != limits.StoredMonthlyBehavior(normalized.MoneyMonthBehavior) {
			t.Fatal("canonical User response", wire)
		}
		raw, _ := json.Marshal(wire)
		var keys map[string]any
		_ = json.Unmarshal(raw, &keys)
		if len(keys) != 12 || keys["tokens_month_behavior"] != "stop" {
			t.Fatal("exact User policy keys", string(raw))
		}
	}
	for _, body := range []string{
		`null`, `[]`, `{"reason":"r","tokens_month_behavior":null}`, `{"tokens_month_behavior":""}`, `{"tokens_month_behavior":[]}`, `{"money_month_behavior":1}`, `{"money_month_behavior":"ALERT_ONLY"}`, `{"money_month_behavior":"alert_only "}`, `{"tokens_month_behavior":"stop","tokens_month_behavior":"alert_only"}`, `{"reason":"r","reason":"r"}`, `{"extra":1}`, `{} {}`,
	} {
		var input LimitInput
		if json.Unmarshal([]byte(body), &input) == nil {
			t.Fatal("malformed intent accepted", body)
		}
	}
	base, _ := limits.Normalize(limits.Policy{})
	raw, _ := json.Marshal(base)
	var keys map[string]any
	_ = json.Unmarshal(raw, &keys)
	if _, exists := keys["tokens_month_behavior"]; exists {
		t.Fatal("legacy/default/nonuser shape expanded")
	}
}

func TestPersonalMonthlyBehaviorStoredScopeAndReset(t *testing.T) {
	for _, kind := range []string{"user", "project", "key", "team", "team_member", "User"} {
		row := entity.ResourceLimit{ScopeKind: kind, TokensMonthBehavior: "stop", MoneyMonthBehavior: "stop"}
		hard, err := policyFromRow(row)
		if err != nil || hard.TokensMonthBehavior != "" || hard.MoneyMonthBehavior != "" {
			t.Fatal("legacy physical stop", kind, err)
		}
		row.TokensMonthBehavior = "alert_only"
		row.TokensMonth = limitNumber(0)
		p, err := policyFromRow(row)
		if kind == "user" {
			if err != nil || p.TokensMonthBehavior != "alert_only" || *p.TokensMonth != 0 {
				t.Fatal("User soft zero", err)
			}
		} else if err == nil {
			t.Fatal("other scope soft admitted", kind)
		}
	}
	for _, bad := range []string{"STOP", "ALERT_ONLY", "stop ", "alert_only ", "unknown"} {
		if _, err := policyFromRow(entity.ResourceLimit{ScopeKind: "user", TokensMonthBehavior: bad}); err == nil {
			t.Fatal("invalid stored mode", bad)
		}
	}
	before, _ := limits.Normalize(limits.Policy{TokensMonthBehavior: "alert_only", MoneyMonthBehavior: "alert_only", IPMode: "allowlist", IPRanges: []string{"192.0.2.0/24"}})
	defaults, _ := limits.Normalize(limits.Policy{TokensMonth: limitNumber(100)})
	p, err := defaultLimitResetPolicy(&defaultLimitResetState{Target: LimitTarget{Kind: "user"}, Stored: before, Policy: defaults})
	if err != nil || p.TokensMonthBehavior != "" || p.MoneyMonthBehavior != "" || !reflect.DeepEqual(p.IPRanges, before.IPRanges) {
		t.Fatal("hard defaults/IP copy changed", err, p)
	}
	defaultRaw := strings.Replace(fullDefaultLimitBody, `"tokens_month":0`, `"tokens_month":0,"tokens_month_behavior":"alert_only"`, 1)
	var input DefaultLimitInput
	if json.Unmarshal([]byte(defaultRaw), &input) == nil {
		t.Fatal("default editor accepted soft mode")
	}
}

func TestPersonalMonthlyBehaviorRuntimeAndAuditIdentity(t *testing.T) {
	svc, data, _ := runtimeFixture(t, "http://127.0.0.1/v1")
	row := entity.ResourceLimit{ScopeKind: "user", ScopeID: "usr_one", ETag: "mode_one", TokensMonth: limitNumber(100), TokensMonthBehavior: "alert_only", MoneyMonthBehavior: "stop", IPMode: "none", IPRangesJSON: "[]"}
	data.Limits = []entity.ResourceLimit{row}
	if err := compileRuntimeLimits(data); err != nil {
		t.Fatal(err)
	}
	data.Quota = &runtimeQuotaData{Setting: entity.QuotaSetting{TimeZone: "UTC"}, Revisions: map[string]string{limitAccount("user", "usr_one"): row.ETag}, Currency: "USD"}
	svc.runtime.auth.Store(buildRuntimeAuthorization(data, time.Now().Add(time.Minute)))
	policy, err := policyFromRow(row)
	if err != nil {
		t.Fatal(err)
	}
	if !svc.quotaNotificationPolicyApplied(row, policy, "UTC", "USD") {
		t.Fatal("exact published mode not applied")
	}
	policy.TokensMonthBehavior = ""
	if svc.quotaNotificationPolicyApplied(row, policy, "UTC", "USD") {
		t.Fatal("different mode reused runtime proof")
	}
	detail, _ := json.Marshal(struct {
		Before, After limits.Policy
		Reason, ETag  string
	}{Before: policy, After: data.LimitPolicies[limitAccount("user", "usr_one")], Reason: "review", ETag: row.ETag})
	raw := string(detail)
	audit := auditRecord(entity.AuditEvent{Action: "limits.update", ResourceType: "user", ResourceID: "usr_one", DetailsJSON: &raw})
	encoded, _ := json.Marshal(audit)
	if !strings.Contains(string(encoded), `"tokens_month_behavior":"alert_only"`) {
		t.Fatal("typed audit lost mode", string(encoded))
	}
	changed := row
	changed.ScopeKind = "project"
	data.Limits = []entity.ResourceLimit{changed}
	if compileRuntimeLimits(data) == nil {
		t.Fatal("foreign soft runtime published")
	}
}

func TestPersonalMonthlyBehaviorAuditFailClosed(t *testing.T) {
	for _, test := range []struct {
		kind, mode string
		valid      bool
	}{
		{"user", "", true}, {"user", "stop", true}, {"user", "alert_only", true}, {"user", "ALERT_ONLY", false}, {"user", "alert_only ", false}, {"project", "alert_only", false}, {"team", "alert_only", false}, {"key", "alert_only", false}, {"User", "alert_only", false},
	} {
		raw, _ := json.Marshal(map[string]any{"before": limits.Policy{}, "after": limits.Policy{TokensMonthBehavior: test.mode}})
		detail := string(raw)
		record := auditRecord(entity.AuditEvent{Action: "limits.update", ResourceType: test.kind, DetailsJSON: &detail})
		if (len(record.Changes) != 0) != test.valid {
			t.Fatal("audit mode projection", test, string(record.Changes))
		}
	}
}

func TestPersonalMonthlyBehaviorDoesNotChangeNotificationThresholds(t *testing.T) {
	row, created, usage := warningFixture()
	for _, used := range []int64{79, 80, 90, 99, 100, 150} {
		usage.Month.TokensUsed = used
		hardWarnings := monthlyQuotaWarnings(row, created, usage, "USD")
		hardExhaustion := monthlyQuotaObservations(row, created, usage, "USD")
		row.TokensMonthBehavior = "alert_only"
		row.MoneyMonthBehavior = "alert_only"
		if !reflect.DeepEqual(monthlyQuotaWarnings(row, created, usage, "USD"), hardWarnings) || !reflect.DeepEqual(monthlyQuotaObservations(row, created, usage, "USD"), hardExhaustion) {
			t.Fatal("mode changed current threshold/recipient observation contract", used)
		}
		row.TokensMonthBehavior = ""
		row.MoneyMonthBehavior = ""
	}
}
