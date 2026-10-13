package service

import (
	"encoding/json"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/secret"
	"strings"
	"testing"
	"time"
)

func TestInsightsReportDefinitionGrammarAndExactInstants(t *testing.T) {
	input := CreateInsightsReportInput{RequestID: "d52b7d9c-6e5d-44b2-b697-c55bd6a77974", Kind: "tokens_by_model", From: "2026-10-01T08:00:00.123456789+08:00", To: "2026-10-02T08:00:00.123456790+08:00", Timezone: "Asia/Shanghai"}
	normalized, err := normalizeInsightsReport(input)
	if err != nil || normalized.From != "2026-10-01T00:00:00.123456789Z" || normalized.To != "2026-10-02T00:00:00.12345679Z" {
		t.Fatal("exact UTC instants", normalized, err)
	}
	equivalent := input
	equivalent.From = normalized.From
	equivalent.To = normalized.To
	other, err := normalizeInsightsReport(equivalent)
	if err != nil || insightsReportIntent(other) != insightsReportIntent(normalized) {
		t.Fatal("normalized replay changed", err)
	}
	changed := normalized
	changed.To = "2026-10-02T00:00:00.123456791Z"
	if insightsReportIntent(changed) == insightsReportIntent(normalized) {
		t.Fatal("nanosecond change lost")
	}
	for _, mutate := range []func(*CreateInsightsReportInput){
		func(v *CreateInsightsReportInput) { v.Kind = "provider_quality" },
	} {
		v := input
		mutate(&v)
		if _, err := normalizeInsightsReport(v); err != nil {
			t.Fatal(err)
		}
	}
	for _, mutate := range []func(*CreateInsightsReportInput){
		func(v *CreateInsightsReportInput) { v.Kind = "TOKENS_BY_MODEL" },
		func(v *CreateInsightsReportInput) { v.Kind = "sql" },
		func(v *CreateInsightsReportInput) { v.RequestID = strings.ToUpper(v.RequestID) },
		func(v *CreateInsightsReportInput) { v.RequestID = "d52b7d9c-6e5d-14b2-b697-c55bd6a77974" },
		func(v *CreateInsightsReportInput) { v.From = "2026-10-01T00:00:00" },
		func(v *CreateInsightsReportInput) { v.To = v.From },
		func(v *CreateInsightsReportInput) { v.To = "2028-10-01T00:00:00Z" },
		func(v *CreateInsightsReportInput) { v.Timezone = "Local" },
		func(v *CreateInsightsReportInput) { v.Timezone = "" },
		func(v *CreateInsightsReportInput) { v.Timezone = "UTC\x00" },
	} {
		v := input
		mutate(&v)
		if _, err := normalizeInsightsReport(v); err == nil {
			t.Fatal("invalid definition accepted", v)
		}
	}
}
func TestInsightsReportOwnerCursorAndReceiptScope(t *testing.T) {
	actor := entity.User{ID: "usr_owner", CreatedAt: time.Date(2026, 10, 1, 0, 0, 0, 123456000, time.UTC)}
	owner := insightsReportOwner(actor)
	alias := actor
	alias.ID = "usr_OWNER"
	reborn := actor
	reborn.CreatedAt = actor.CreatedAt.Add(time.Microsecond)
	if owner == insightsReportOwner(alias) || owner == insightsReportOwner(reborn) {
		t.Fatal("owner alias or birth reused")
	}
	const last = "isr_01k7g000000000000000000000"
	cursor := insightsReportCursor(owner, last)
	if got, err := parseInsightsReportCursor(cursor, owner); err != nil || got != last {
		t.Fatal(got, err)
	}
	for _, value := range []string{cursor + "=", strings.Repeat("x", 161), insightsReportCursor(insightsReportOwner(alias), last), insightsReportCursor(owner, strings.ToUpper(last)), insightsReportCursor(owner, "isr_81k7g000000000000000000000")} {
		if _, err := parseInsightsReportCursor(value, owner); err == nil {
			t.Fatal("invalid owner cursor", value)
		}
	}
}

func TestInsightsReportQuotaParametersAndLegacyIntent(t *testing.T) {
	const request = "d52b7d9c-6e5d-44b2-b697-c55bd6a77974"
	for _, input := range []CreateInsightsReportInput{
		{RequestID: request, Kind: "team_quota", Dimension: "tokens"},
		{RequestID: request, Kind: "team_quota", Dimension: "money"},
		{RequestID: request, Kind: "key_quota", Dimension: "tokens", ScopeKind: "member", ScopeID: "usr_01k7g000000000000000000000"},
		{RequestID: request, Kind: "key_quota", Dimension: "money", ScopeKind: "project", ScopeID: "prj_fixture"},
	} {
		normalized, err := normalizeInsightsReport(input)
		if err != nil || normalized != input {
			t.Fatal("valid quota parameters changed", input, err)
		}
		changed := input
		changed.Dimension = "money"
		if input.Dimension == "money" {
			changed.Dimension = "tokens"
		}
		if insightsReportIntent(input) == insightsReportIntent(changed) {
			t.Fatal("dimension omitted from intent")
		}
	}
	valid := CreateInsightsReportInput{RequestID: request, Kind: "key_quota", Dimension: "tokens", ScopeKind: "project", ScopeID: "prj_fixture"}
	for _, mutate := range []func(*CreateInsightsReportInput){
		func(v *CreateInsightsReportInput) { v.Dimension = "" },
		func(v *CreateInsightsReportInput) { v.Dimension = "TOKENS" },
		func(v *CreateInsightsReportInput) { v.ScopeKind = "Project" },
		func(v *CreateInsightsReportInput) { v.ScopeID = "" },
		func(v *CreateInsightsReportInput) { v.ScopeID = strings.Repeat("x", 31) },
		func(v *CreateInsightsReportInput) { v.ScopeID = "prj_private\x00" },
		func(v *CreateInsightsReportInput) { v.From = "2026-10-01T00:00:00Z" },
		func(v *CreateInsightsReportInput) { v.Timezone = "UTC" },
		func(v *CreateInsightsReportInput) { v.Kind = "team_quota" },
	} {
		input := valid
		mutate(&input)
		if _, err := normalizeInsightsReport(input); err == nil {
			t.Fatal("mixed/invalid quota definition accepted", input)
		}
	}
	legacy := CreateInsightsReportInput{RequestID: request, Kind: "tokens_by_model", From: "2026-10-01T00:00:00Z", To: "2026-10-02T00:00:00Z", Timezone: "UTC"}
	// Released V110 intent bytes must survive new fields and upgraded schemas.
	raw := `{"request_id":"d52b7d9c-6e5d-44b2-b697-c55bd6a77974","kind":"tokens_by_model","from":"2026-10-01T00:00:00Z","to":"2026-10-02T00:00:00Z","timezone":"UTC"}`
	if insightsReportIntent(legacy) != secret.SHA256Hex(raw) {
		t.Fatal("released intent digest changed")
	}
	legacy.Dimension = "tokens"
	if _, err := normalizeInsightsReport(legacy); err == nil {
		t.Fatal("legacy definition admitted quota fields")
	}
}

func TestInsightsReportQuotaDTOOmitsHistoricalAndInapplicableFields(t *testing.T) {
	for _, row := range []entity.InsightsReport{
		{Kind: "team_quota", Dimension: "money"},
		{Kind: "key_quota", Dimension: "tokens", ScopeKind: "project", ScopeID: "prj_fixture"},
		{Kind: "tokens_by_model", From: "from", To: "to", Timezone: "UTC"},
	} {
		raw, err := json.Marshal(insightsReportDefinition(row))
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]json.RawMessage
		if err = json.Unmarshal(raw, &fields); err != nil {
			t.Fatal(err)
		}
		quota := row.Kind == "team_quota" || row.Kind == "key_quota"
		for _, name := range []string{"from", "to", "timezone"} {
			if _, has := fields[name]; has == quota {
				t.Fatal("inapplicable range field", name, string(raw))
			}
		}
		if _, has := fields["dimension"]; has != quota {
			t.Fatal("dimension discriminator", string(raw))
		}
		for _, name := range []string{"scope_kind", "scope_id"} {
			if _, has := fields[name]; has != (row.Kind == "key_quota") {
				t.Fatal("inapplicable scope field", name, string(raw))
			}
		}
	}
}
