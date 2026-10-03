package service

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/entity"
)

func TestTeamUsageRejectsIdentityAndDiagnosticFilters(t *testing.T) {
	for _, filter := range []UsageFilter{{KeyID: "key_other"}, {UserID: "usr_other"}, {ProjectID: "prj_other"}, {TeamID: "tea_other"}, {ProviderID: "prv_other"}, {ProviderModelID: "pmd_other"}, {ConnectionID: "con_other"}} {
		if validateTeamUsageFilter(filter) == nil {
			t.Fatalf("Team report accepted private or cross-scope filter: %+v", filter)
		}
	}
	if err := validateTeamUsageFilter(UsageFilter{ModelID: "mdl_exact", Status: "success", Protocol: entity.ProtocolOpenAIChat}); err != nil {
		t.Fatal(err)
	}
	for _, filter := range []UsageFilter{{TeamID: "tea_exact", UserID: "usr_actor"}, {TeamID: "tea_exact", ProjectID: "prj_other"}, {TeamID: "tea_exact", KeyID: "key_other"}} {
		if validateUsageFilter(filter, true) == nil {
			t.Fatal("ambiguous admin attribution accepted", filter)
		}
	}
	if err := validateUsageFilter(UsageFilter{TeamID: "tea_historical", ProviderID: "prv_diagnostic"}, true); err != nil {
		t.Fatal("independent administrator diagnostics rejected", err)
	}
}

func TestTeamUsageFactScopeAndProjectionExcludePrivateIdentities(t *testing.T) {
	db, err := gorm.Open(projectQuotaScopeDialector{}, &gorm.Config{DryRun: true, DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	query := usageQueryFilters(db.Model(&entity.CallRecord{}), UsageFilter{TeamID: "tea_exact", Status: "success"}).Where("started_at >= ?", "start")
	query.Statement.Clauses["WHERE"].Build(query.Statement)
	if query.Statement.SQL.String() != `WHERE "team_id" = ? AND "project_id" = ? AND "key_id" = ? AND status = ? AND started_at >= ?` || !reflect.DeepEqual(query.Statement.Vars, []any{"tea_exact", "", "", "success", "start"}) {
		t.Fatal("Team attribution lost exact conjunctive scoping", query.Statement.SQL.String(), query.Statement.Vars)
	}
	columns := usageFactColumns("team")
	for _, forbidden := range []string{"key_id", "user_id", "team_membership_id", "provider_id", "provider_name", "provider_model_id", "connection_id", "connection_name", "upstream_model_name"} {
		if slices.Contains(columns, forbidden) {
			t.Fatal("member Team query borrows private diagnostics", forbidden)
		}
	}
	if !slices.Contains(usageFactColumns("admin"), "provider_id") || !slices.Contains(usageFactColumns("personal"), "key_id") {
		t.Fatal("existing authorized projections lost fields")
	}
}

func TestTeamUsageRejectsMismatchedSelectedFact(t *testing.T) {
	if err := validateTeamUsageFacts([]entity.CallRecord{{TeamID: "tea_exact"}, {TeamID: "tea_exact"}}, "tea_exact"); err != nil {
		t.Fatal(err)
	}
	for _, teamID := range []string{"", "tea_other", "TEA_EXACT", "tea_exact "} {
		if validateTeamUsageFacts([]entity.CallRecord{{TeamID: "tea_exact"}, {TeamID: teamID}}, "tea_exact") == nil {
			t.Fatal("mismatched selected attribution was accepted or partially filtered", teamID)
		}
	}
}

func TestTeamUsageDimensionsPreserveIndependentAdminDiagnostics(t *testing.T) {
	for _, test := range []struct {
		scope  string
		filter UsageFilter
		want   []string
	}{
		{"team", UsageFilter{}, []string{"model"}},
		{"admin", UsageFilter{TeamID: "tea_historical"}, []string{"model", "provider", "provider_model", "connection"}},
		{"admin", UsageFilter{}, []string{"model", "key", "provider", "provider_model", "connection"}},
		{"admin", UsageFilter{ProjectID: "prj_historical"}, []string{"model", "key", "provider", "provider_model", "connection"}},
		{"personal", UsageFilter{}, []string{"model", "key"}},
		{"project", UsageFilter{}, []string{"model", "key"}},
	} {
		if got := usageDimensions(test.scope, test.filter); !reflect.DeepEqual(got, test.want) {
			t.Fatal("scope advertises unsupported or missing dimensions", test.scope, test.filter, got)
		}
	}
}

func TestTeamUsageAggregatePreservesUnknownAndHistoricalCurrency(t *testing.T) {
	start := time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	plan, err := planUsage(UsageFilter{From: &start, To: &end}, end)
	if err != nil {
		t.Fatal(err)
	}
	first := entity.CallRecord{RequestID: "req_private_one", TeamID: "tea_exact", UserID: "usr_private_one", KeyID: "key_private", ProviderID: "prv_private", ProviderName: "Private supplier", ModelID: "mdl_one", ModelName: "Historical model", Status: "success", StartedAt: start, DurationMS: 20, InputTokens: usageTestPointer(int64(4)), OutputTokens: usageTestPointer(int64(1)), CallPricingFields: entity.CallPricingFields{PricingStatus: "priced", ChargeAmount: usageTestPointer("0.100000000000000001"), ChargeCurrency: usageTestPointer("USD")}}
	second := first
	second.RequestID, second.UserID, second.PricingStatus = "req_private_two", "usr_private_two", "unknown_usage"
	second.InputTokens, second.ChargeAmount, second.ChargeCurrency = nil, nil, nil
	third := first
	third.RequestID, third.ChargeCurrency = "req_private_three", usageTestPointer("EUR")
	period, err := aggregateUsage([]entity.CallRecord{first, second, third}, plan.current, plan, false)
	if err != nil {
		t.Fatal(err)
	}
	if period.Summary.Requests != 3 || period.Summary.Tokens.Total.Value != nil || period.Summary.Tokens.Total.Known != "11" || period.Summary.Tokens.Total.UnknownCalls != 1 || len(period.Summary.Amounts) != 2 || period.Summary.Amounts[0].Amount != "0.100000000000000001" || period.Summary.UnknownAmountCalls != 1 || len(period.Keys) != 0 || len(period.Models) != 1 {
		t.Fatal("Team aggregate changed exact arithmetic or privacy", period)
	}
	raw, err := json.Marshal(UsageReport{TeamID: "tea_exact", Current: period, AvailableDimensions: []string{"model"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"req_private", "usr_private", "key_private", "prv_private", "Private supplier", `"providers"`, `"team_membership_id"`} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatal("Team wire leaked private attribution", forbidden)
		}
	}
}
