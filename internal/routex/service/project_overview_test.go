package service

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
)

func TestProjectOverviewMonthlyUsageKeepsAuthoritativeUnknowns(t *testing.T) {
	stamp := time.Date(2026, time.March, 15, 12, 0, 0, 0, time.UTC)
	for _, input := range []*QuotaUsageRecord{
		nil,
		{Activated: false, AsOf: &stamp, Month: &QuotaWindowRecord{}},
		{Activated: true, Month: &QuotaWindowRecord{}},
		{Activated: true, AsOf: &stamp},
	} {
		result, err := projectOverviewMonthlyUsage(input)
		if err != nil || result != nil {
			t.Fatalf("missing journal facts became usage: %+v, %v", result, err)
		}
	}
	input := &QuotaUsageRecord{
		Activated: true, AsOf: &stamp, TimeZone: "America/New_York",
		Month: &QuotaWindowRecord{
			Covered: false, TokensUsed: 9007199254740993, TokensHeld: 7, TokensUnknown: 2,
			MoneyUsed: map[string]string{"USD": "123456789012345678.123456789012345678", "CNY": "0"},
			MoneyHeld: map[string]string{"USD": "0.000000000000000001"}, MoneyUnknown: 3,
		},
	}
	result, err := projectOverviewMonthlyUsage(input)
	if err != nil {
		t.Fatal(err)
	}
	if result.Covered || result.TokensUsed != "9007199254740993" || result.TokensHeld != "7" || result.TokensUnknown != 2 || result.MoneyUnknown != 3 {
		t.Fatalf("unknown coverage, holds or exact integer changed: %+v", result)
	}
	if result.MoneyUsed["USD"] != input.Month.MoneyUsed["USD"] || result.MoneyUsed["CNY"] != "0" || result.MoneyHeld["USD"] != input.Month.MoneyHeld["USD"] {
		t.Fatalf("money was converted or rounded: %+v", result)
	}
	start := time.Date(2026, time.March, 1, 5, 0, 0, 0, time.UTC)
	end := time.Date(2026, time.April, 1, 4, 0, 0, 0, time.UTC)
	if !result.AsOf.Equal(stamp) || !result.MonthStart.Equal(start) || !result.MonthEnd.Equal(end) {
		t.Fatalf("server calendar did not preserve daylight-saving month: %+v", result)
	}
	input.TimeZone = "invalid/zone"
	if result, err := projectOverviewMonthlyUsage(input); result != nil || !errors.Is(err, runtimeUnavailable) {
		t.Fatalf("invalid authoritative calendar became a browser/default calendar: %+v, %v", result, err)
	}
}

func TestProjectOverviewActivitiesExposeOnlyTypedRecordedFacts(t *testing.T) {
	private := `{"secret":"hidden-secret","email":"hidden@example.invalid","reason":"private-reason"}`
	stamp := time.Date(2026, time.October, 3, 11, 1, 0, 0, time.FixedZone("offset", 3600))
	for action, kind := range map[string]string{
		"resource.create": "project_created", "resource.update": "project_updated",
		"project.managers.replace": "managers_changed", "resource.models.replace": "models_changed",
		"limits.update": "limits_changed",
	} {
		resourceType := "projects"
		if action == "limits.update" {
			resourceType = "project"
		}
		row := entity.AuditEvent{ID: "aud_overview", ActorID: "usr_private", Action: action, ResourceType: resourceType, ResourceID: "prj_overview", DetailsJSON: &private, CreatedAt: stamp}
		result, valid := projectOverviewActivity(row, "prj_overview")
		if !valid || result.Kind != kind || result.Status != "committed" || result.ActorName != nil || result.CreatedAt.Location() != time.UTC || !result.CreatedAt.Equal(stamp) {
			t.Fatalf("recorded activity did not retain safe facts: %+v, %t", result, valid)
		}
		encoded, err := json.Marshal(result)
		if err != nil {
			t.Fatal(err)
		}
		for _, forbidden := range []string{"hidden-secret", "hidden@example.invalid", "private-reason", "usr_private", "details", "actor_id"} {
			if strings.Contains(string(encoded), forbidden) {
				t.Fatalf("activity leaked %q: %s", forbidden, encoded)
			}
		}
	}
	for _, row := range []entity.AuditEvent{
		{Action: "project_key.create", ResourceType: "projects", ResourceID: "prj_overview"},
		{Action: "resource.create", ResourceType: "projects", ResourceID: "PRJ_OVERVIEW"},
		{Action: "RESOURCE.CREATE", ResourceType: "projects", ResourceID: "prj_overview"},
		{Action: "resource.create", ResourceType: "project", ResourceID: "prj_overview"},
		{Action: "limits.update", ResourceType: "projects", ResourceID: "prj_overview"},
		{Action: "resource.update", ResourceType: "teams", ResourceID: "prj_overview"},
	} {
		if _, valid := projectOverviewActivity(row, "prj_overview"); valid {
			t.Fatalf("unrelated or noncanonical activity was exposed: %+v", row)
		}
	}
}
