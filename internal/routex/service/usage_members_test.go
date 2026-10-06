package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
)

func TestUsageModelsRequireRecordedActorCoverage(t *testing.T) {
	from := time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(time.Hour)
	plan, err := planUsage(UsageFilter{From: &from, To: &to}, to)
	if err != nil {
		t.Fatal(err)
	}
	rows := []entity.CallRecord{
		{RequestID: "req_members_success", UserID: "usr_A", ModelID: "mdl_members", Status: "success", StartedAt: from},
		{RequestID: "req_members_error", UserID: "usr_A", ModelID: "mdl_members", Status: "error", StartedAt: from},
		{RequestID: "req_members_canceled", UserID: "usr_a", ModelID: "mdl_members", Status: "canceled", StartedAt: from},
	}
	period, err := aggregateUsage(rows, plan.current, plan, false)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(period)
	if err != nil {
		t.Fatal(err)
	}
	var wire struct {
		Models []struct {
			Members *struct {
				Value        *int64 `json:"value"`
				Known        int64  `json:"known"`
				UnknownCalls int64  `json:"unknown_calls"`
			} `json:"members"`
		} `json:"models"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatal(err)
	}
	if len(wire.Models) != 1 || wire.Models[0].Members == nil {
		t.Fatal("Model groups lack recorded-actor coverage")
	}
	count := wire.Models[0].Members
	if count.Value == nil || *count.Value != 2 || count.Known != 2 || count.UnknownCalls != 0 || period.Summary.Requests != 3 || period.Summary.Errors != 1 || period.Summary.Canceled != 1 {
		t.Fatal("recorded requests were folded by collation, membership or terminal success", count, period.Summary)
	}
}

func TestUsageRecordedActorIdentityCoverage(t *testing.T) {
	for _, value := range []string{"usr_a", "usr_A", "usr_0-_-", "usr_" + strings.Repeat("a", 26)} {
		if !usageRecordedActorID(value) {
			t.Fatal("safe historical identity rejected", value)
		}
	}
	for _, value := range []string{"", "usr_", "USR_a", "usr_a ", " usr_a", "usr_é", "usr_a/b", "usr_a\x00", "usr_a\n", "usr_" + strings.Repeat("a", 27)} {
		if usageRecordedActorID(value) {
			t.Fatal("invalid historical identity accepted", value)
		}
	}
	var acc usageMemberAccumulator
	for _, value := range []string{"usr_a", "usr_a", "usr_A", "", "USR_a"} {
		acc.add(value)
	}
	result := acc.result()
	if result.Value != nil || result.Known != 2 || result.UnknownCalls != 2 {
		t.Fatal("unknown coverage was converted into a total", result)
	}
	var empty usageMemberAccumulator
	if zero := empty.result(); zero.Value == nil || *zero.Value != 0 || zero.Known != 0 || zero.UnknownCalls != 0 {
		t.Fatal("known empty coverage lost zero", zero)
	}
	var unknown usageMemberAccumulator
	unknown.add("")
	if result := unknown.result(); result.Value != nil || result.Known != 0 || result.UnknownCalls != 1 {
		t.Fatal("missing attribution inferred actor", result)
	}
}

func TestUsageMembersExactPeriodsModelsAndImmutableFacts(t *testing.T) {
	now := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	plan, err := planUsage(UsageFilter{Period: "month", Timezone: "UTC", Compare: true}, now)
	if err != nil {
		t.Fatal(err)
	}
	if plan.current.from != time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC) || !plan.current.to.Equal(now) {
		t.Fatal("UTC month-to-query changed", plan)
	}
	input, output := int64(1), int64(2)
	amount, currency := "1.000000000000000001", "USD"
	base := entity.CallRecord{UserID: "usr_recorded", ModelID: "mdl_A", ModelName: "Recorded", TeamMembershipID: "tmm_old", Status: "success", StartedAt: plan.current.from, InputTokens: &input, OutputTokens: &output, PricingStatus: "priced", ChargeAmount: &amount, ChargeCurrency: &currency}
	rows := []entity.CallRecord{base}
	appendRow := func(request, user, model, membership, status string, at time.Time) {
		row := base
		row.RequestID, row.UserID, row.ModelID, row.TeamMembershipID, row.Status, row.StartedAt = request, user, model, membership, status, at
		rows = append(rows, row)
	}
	appendRow("req_rejoin", "usr_recorded", "mdl_A", "tmm_rejoin", "error", plan.current.from.Add(time.Second))
	appendRow("req_invalid", "USR_recorded", "mdl_A", "", "canceled", plan.current.from.Add(time.Second))
	appendRow("req_alias", "usr_recorded", "mdl_a", "", "success", plan.current.from)
	appendRow("req_unknown_model", "usr_recorded", "", "", "success", plan.current.from)
	appendRow("req_previous", "usr_previous", "mdl_A", "", "success", plan.current.from.Add(-time.Nanosecond))
	appendRow("req_before", "usr_outside", "mdl_A", "", "success", plan.previous.from.Add(-time.Nanosecond))
	appendRow("req_end", "usr_outside", "mdl_A", "", "success", now)
	current, err := aggregateUsage(rows, plan.current, plan, true)
	if err != nil {
		t.Fatal(err)
	}
	previous, err := aggregateUsage(rows, *plan.previous, plan, true)
	if err != nil {
		t.Fatal(err)
	}
	if current.Summary.Requests != 5 || previous.Summary.Requests != 1 {
		t.Fatal("half-open period isolation lost", current.Summary, previous.Summary)
	}
	if len(current.Models) != 3 {
		t.Fatal("exact Model identity or unknown group was merged", current.Models)
	}
	for _, group := range current.Models {
		if group.Members == nil {
			t.Fatal("missing Model coverage")
		}
		if group.ID == "mdl_A" {
			if group.Members.Known != 1 || group.Members.UnknownCalls != 1 || group.Members.Value != nil || group.Stats.Requests != 3 {
				t.Fatal("membership reincarnation or invalid identity inferred", group)
			}
		} else if group.Members.Value == nil || *group.Members.Value != 1 {
			t.Fatal("independent Model coverage lost", group)
		}
		if group.Members.Known+group.Members.UnknownCalls > group.Stats.Requests {
			t.Fatal("coverage exceeds requests", group)
		}
	}
	if previous.Models[0].Members.Value == nil || *previous.Models[0].Members.Value != 1 {
		t.Fatal("previous actors leaked current window")
	}
	if len(current.Summary.Amounts) != 1 || current.Summary.Amounts[0].Amount != "5.000000000000000005" || current.Summary.Tokens.Total.Known != "15" {
		t.Fatal("member projection changed exact money or tokens", current.Summary)
	}
}

func TestUsageMembersBoundedCoverageAndGroupOverflow(t *testing.T) {
	from := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(time.Hour)
	plan, err := planUsage(UsageFilter{From: &from, To: &to}, to)
	if err != nil {
		t.Fatal(err)
	}
	rows := make([]entity.CallRecord, usageRowLimit)
	for i := range rows {
		rows[i] = entity.CallRecord{RequestID: fmt.Sprintf("req_%05d", i), UserID: fmt.Sprintf("usr_%05d", i), ModelID: "mdl_exact", Status: "success", StartedAt: from}
	}
	period, err := aggregateUsage(rows, plan.current, plan, false)
	if err != nil {
		t.Fatal(err)
	}
	count := period.Models[0].Members
	if count == nil || count.Value == nil || *count.Value != usageRowLimit || count.Known != usageRowLimit || count.UnknownCalls != 0 {
		t.Fatal("complete row ceiling silently truncated actors", count)
	}
	for i := 0; i <= usageGroupLimit; i++ {
		rows[i].ModelID = fmt.Sprintf("mdl_%03d", i)
	}
	if _, err := aggregateUsage(rows[:usageGroupLimit+1], plan.current, plan, false); !errors.Is(err, usageTooLarge) {
		t.Fatal("group overflow returned partial coverage", err)
	}
}

func TestUsageMembersPublicProjectionAndCSVUnchanged(t *testing.T) {
	from := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(time.Hour)
	plan, err := planUsage(UsageFilter{From: &from, To: &to}, to)
	if err != nil {
		t.Fatal(err)
	}
	rows := []entity.CallRecord{{RequestID: "req_whitelist", UserID: "usr_private_actor", ModelID: "mdl_public", KeyID: "key_public", ProviderID: "prv_public", ProviderModelID: "pmd_public", ConnectionID: "con_public", Status: "success", StartedAt: from}}
	period, err := aggregateUsage(rows, plan.current, plan, true)
	if err != nil {
		t.Fatal(err)
	}
	report := UsageReport{MemberCountBasis: usageMemberCountBasis, Current: period, Previous: &period}
	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(rows[0].UserID)) || bytes.Contains(raw, []byte("user_id")) {
		t.Fatal("raw recorded actors exposed", string(raw))
	}
	var wire map[string]json.RawMessage
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatal(err)
	}
	if string(wire["member_count_basis"]) != `"distinct_recorded_actors"` {
		t.Fatal("basis absent", wire)
	}
	for _, name := range []string{"current", "previous"} {
		var p map[string]json.RawMessage
		if err := json.Unmarshal(wire[name], &p); err != nil {
			t.Fatal(err)
		}
		var groups []map[string]json.RawMessage
		if err := json.Unmarshal(p["models"], &groups); err != nil {
			t.Fatal(err)
		}
		var members map[string]json.RawMessage
		if err := json.Unmarshal(groups[0]["members"], &members); err != nil {
			t.Fatal(err)
		}
		if len(members) != 3 || string(members["value"]) != "1" || string(members["known"]) != "1" || string(members["unknown_calls"]) != "0" {
			t.Fatal("member wire changed", members)
		}
		for _, dimension := range []string{"keys", "providers", "provider_models", "connections", "summary", "trend"} {
			if bytes.Contains(p[dimension], []byte(`"members"`)) {
				t.Fatal("coverage escaped Model groups", dimension)
			}
		}
	}
	fixture := usageExportFixture()
	original, err := encodeUsageCSV(context.Background(), "personal", "usr_self", UsageFilter{Compare: true}, fixture)
	if err != nil {
		t.Fatal(err)
	}
	fixture.MemberCountBasis = usageMemberCountBasis
	value := int64(1)
	fixture.Current.Models[0].Members = &UsageMembers{Value: &value, Known: 1}
	updated, err := encodeUsageCSV(context.Background(), "personal", "usr_self", UsageFilter{Compare: true}, fixture)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(original, updated) {
		t.Fatal("new JSON coverage changed frozen CSV bytes")
	}
	usageExportRows(t, updated)
}

func TestUsageMembersSelectOnlyOnePrivateActorColumn(t *testing.T) {
	for _, scope := range []string{"personal", "project", "team", "admin"} {
		columns := usageFactColumns(scope)
		count := 0
		for _, column := range columns {
			if column == "user_id" {
				count++
			}
		}
		if count != 1 {
			t.Fatal("private actor not selected exactly once", scope, columns)
		}
		want := []string{"request_id", "user_id", "team_id", "model_id", "model_name", "status", "started_at", "completed_at", "duration_ms", "input_tokens", "output_tokens", "pricing_status", "charge_amount", "charge_currency"}
		if scope != "team" {
			want = append(want, "key_id", "provider_id", "provider_name", "provider_model_id", "upstream_model_name", "connection_id", "connection_name")
		}
		if !reflect.DeepEqual(columns, want) {
			t.Fatal("unrelated fact projection changed", scope, columns)
		}
	}
}
