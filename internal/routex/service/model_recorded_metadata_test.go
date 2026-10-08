package service

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestModelRecordedMetadataHistoricalProofCompatibility(t *testing.T) {
	birth := time.Date(2026, 10, 1, 0, 0, 0, 123456789, time.UTC)
	model := newRecordedModel("mdl_recorded", "active", birth)
	if model.ConfigUpdatedAt == nil || !model.CreatedAt.Equal(*model.ConfigUpdatedAt) || model.CreatedAt.Nanosecond() != 123000000 {
		t.Fatal("creation must record a portable exact birth and configuration timestamp")
	}
	// This frozen shape is the entity serialized into released review/receipt
	// state hashes. Neither a legacy null nor a new timestamp may change it.
	legacy := struct {
		ID        string
		Status    string
		CreatedAt time.Time
	}{model.ID, model.Status, model.CreatedAt}
	for _, recorded := range []*time.Time{nil, &birth} {
		model.ConfigUpdatedAt = recorded
		oldBytes, _ := json.Marshal(legacy)
		newBytes, _ := json.Marshal(model)
		if string(oldBytes) != string(newBytes) || personalHash(legacy) != personalHash(model) || strings.Contains(string(newBytes), "Updated") {
			t.Fatal("recorded metadata changed historical Model proof serialization")
		}
	}
}

func TestModelMonthlyCountsExactLogicalIdentityAndBounds(t *testing.T) {
	from := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	until := from.Add(time.Hour)
	ids := []string{"mdl_one", "mdl_empty"}
	facts := []modelMonthlyFact{{"req_one", "mdl_one", from}, {"req_two", "mdl_one", until.Add(-time.Nanosecond)}, {"req_foreign", "MDL_ONE", from}}
	items, err := modelMonthlyCounts(ids, facts, from, until)
	if err != nil || len(items) != 2 || items[0].Requests != "2" || items[1].Requests != "0" {
		t.Fatal("logical request count borrowed aliases or lost known zero", err, items)
	}
	for _, invalid := range [][]modelMonthlyFact{{facts[0], facts[0]}, {{"", "mdl_one", from}}, {{"req_late", "mdl_one", until}}, {{"req_old", "mdl_one", from.Add(-time.Nanosecond)}}, make([]modelMonthlyFact, usageRowLimit+1)} {
		if result, err := modelMonthlyCounts(ids, invalid, from, until); err == nil || result != nil {
			t.Fatal("invalid or incomplete query produced partial/zero results")
		}
	}
	for _, invalid := range [][]string{nil, {"mdl_one", "mdl_one"}, {"alias"}, {"mdl_one "}, make([]string, modelMonthlyBatchLimit+1)} {
		if _, err := modelMonthlyTargets(invalid); err == nil {
			t.Fatal("invalid target batch accepted")
		}
	}
}
