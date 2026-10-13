package service

import (
	"github.com/miclle/routex/internal/routex/entity"
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
