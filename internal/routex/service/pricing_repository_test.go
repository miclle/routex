package service

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/pricing"
	"github.com/miclle/routex/prices"
)

func repositoryTestModel() prices.Model {
	return prices.Model{Key: "source/model", Name: "An unrelated display name", Protocol: entity.ProtocolOpenAIChat, ContextThreshold: 0, Rates: []prices.Rate{
		{Key: "source/input", Value: pricing.Rate{Metric: pricing.Input, Tier: pricing.Base, Unit: pricing.Unit, Currency: "USD", Amount: "9007199254740993.000000000000000001", Enabled: true}},
		{Key: "source/output", Value: pricing.Rate{Metric: pricing.Output, Tier: pricing.Base, Unit: pricing.Unit, Currency: "USD", Amount: "2", Enabled: true}},
	}}
}
func repositoryTestRecord(source prices.Model, custom bool) PriceRecord {
	record := PriceRecord{ID: "prc_local", ProviderModelID: "pmo_local", Protocol: source.Protocol, UpstreamName: "An unrelated local name", ContextThreshold: source.ContextThreshold, Rates: []pricing.Rate{}, RateSources: map[string]PriceRateSource{}, ContextThresholdSource: PriceThresholdSource{Kind: "repository", SourceModelKey: &source.Key}}
	for index, r := range source.Rates {
		rate := r.Value
		rate.ID = "rat_input"
		if index > 0 {
			rate.ID = "rat_output"
		}
		record.Rates = append(record.Rates, rate)
		key := r.Key
		record.RateSources[rate.ID] = PriceRateSource{Kind: "repository", SourceModelKey: &source.Key, SourceRateKey: &key}
		if custom {
			record.RateSources[rate.ID] = PriceRateSource{Kind: "custom"}
		}
	}
	return record
}
func repositoryTestFX() pricing.FX {
	return pricing.FX{PlatformCurrency: "USD", Rates: map[string]string{"USD": "1"}}
}
func TestRepositorySyncProtectsCustomZeroDisabledAndSameAmount(t *testing.T) {
	source := repositoryTestModel()
	for _, mode := range []string{"zero", "disabled", "same"} {
		t.Run(mode, func(t *testing.T) {
			before := repositoryTestRecord(source, true)
			switch mode {
			case "zero":
				before.Rates[0].Amount = "0"
			case "disabled":
				before.Rates[0].Enabled = false
			}
			frozen, _ := json.Marshal(before)
			plan := repositoryModelPlan(before, source, RepositoryPriceSelection{Mode: "sync"}, repositoryTestFX())
			if len(plan.Errors) != 0 || len(plan.Input.Rates) != 0 || len(plan.Changes) != 2 {
				t.Fatalf("custom sync changed values: %+v", plan)
			}
			for _, change := range plan.Changes {
				if change.Action != "protected_custom" || change.AfterSource.Kind != "custom" || *change.Before != *change.After {
					t.Fatal("custom rate overwritten")
				}
			}
			after, _ := json.Marshal(before)
			if string(after) != string(frozen) {
				t.Fatal("planner mutated current state")
			}
		})
	}
}
func TestRepositoryAddsMissingAndUpdatesOnlyFollowedRates(t *testing.T) {
	source := repositoryTestModel()
	before := repositoryTestRecord(source, false)
	before.Rates = before.Rates[:1]
	before.Rates[0].Amount = "1"
	plan := repositoryModelPlan(before, source, RepositoryPriceSelection{Mode: "sync"}, repositoryTestFX())
	if len(plan.Errors) != 0 || len(plan.Input.Rates) != 2 || plan.Input.Rates[0].Amount != "9007199254740993.000000000000000001" || plan.Changes[1].Action != "added" || plan.Changes[1].RateID != nil {
		t.Fatalf("exact source transition lost: %+v", plan)
	}
	source.Rates = source.Rates[:1]
	before = repositoryTestRecord(repositoryTestModel(), false)
	plan = repositoryModelPlan(before, source, RepositoryPriceSelection{Mode: "sync"}, repositoryTestFX())
	if len(plan.Input.Rates) != 0 || len(plan.Warnings) != 1 || plan.Warnings[0].Code != "missing_source_rate" {
		t.Fatal("missing source implied deletion", plan)
	}
}
func TestRepositoryRestoreIsExactSelectedRateOnly(t *testing.T) {
	source := repositoryTestModel()
	before := repositoryTestRecord(source, true)
	before.Rates[0].Amount = "0"
	before.Rates[1].Enabled = false
	plan := repositoryModelPlan(before, source, RepositoryPriceSelection{Mode: "restore", RateIDs: []string{"rat_input"}}, repositoryTestFX())
	if len(plan.Errors) != 0 || len(plan.Input.Rates) != 1 || len(plan.Changes) != 1 || plan.Changes[0].RateID == nil || *plan.Changes[0].RateID != "rat_input" || plan.Changes[0].AfterSource.Kind != "repository" {
		t.Fatal("restore escaped selection", plan)
	}
	if before.RateSources["rat_output"].Kind != "custom" || before.Rates[1].Enabled {
		t.Fatal("unsubmitted custom owner changed")
	}
	source.Rates = source.Rates[:1]
	plan = repositoryModelPlan(before, source, RepositoryPriceSelection{Mode: "restore", RateIDs: []string{"rat_output"}}, repositoryTestFX())
	if len(plan.Errors) != 1 || plan.Errors[0].Code != "missing_source_rate" {
		t.Fatal("missing restore silently succeeded")
	}
}
func TestRepositoryThresholdCannotReinterpretProtectedCustomTokenRates(t *testing.T) {
	source := repositoryTestModel()
	source.ContextThreshold = 128000
	before := repositoryTestRecord(repositoryTestModel(), true)
	for _, mode := range []string{"sync", "restore"} {
		current := before
		current.RateSources = map[string]PriceRateSource{}
		for id, owner := range before.RateSources {
			current.RateSources[id] = owner
		}
		if mode == "sync" {
			key := source.Rates[0].Key
			current.RateSources["rat_input"] = PriceRateSource{Kind: "repository", SourceModelKey: &source.Key, SourceRateKey: &key}
		}
		plan := repositoryModelPlan(current, source, RepositoryPriceSelection{Mode: mode, RateIDs: []string{"rat_input"}}, repositoryTestFX())
		found := false
		for _, issue := range plan.Errors {
			found = found || issue.Code == "protected_threshold"
		}
		if !found {
			t.Fatal("unselected custom threshold reinterpreted", mode, plan)
		}
	}
	plan := repositoryModelPlan(before, source, RepositoryPriceSelection{Mode: "restore", RateIDs: []string{"rat_input", "rat_output"}}, repositoryTestFX())
	if len(plan.Errors) != 0 || *plan.Input.ContextThreshold != 128000 || plan.ThresholdKey == nil {
		t.Fatal("reviewed complete restore rejected", plan)
	}
	// Equal values do not authorize following a manually maintained threshold.
	before = repositoryTestRecord(repositoryTestModel(), false)
	before.ContextThresholdSource = PriceThresholdSource{Kind: "custom"}
	plan = repositoryModelPlan(before, source, RepositoryPriceSelection{Mode: "sync"}, repositoryTestFX())
	found := false
	for _, issue := range plan.Errors {
		found = found || issue.Code == "custom_threshold"
	}
	if !found {
		t.Fatal("custom shared threshold silently followed")
	}
}
func TestRepositoryRejectsImplicitSourceAndBusinessKeyTransitions(t *testing.T) {
	for _, mode := range []string{"key", "currency", "unit"} {
		t.Run(mode, func(t *testing.T) {
			source := repositoryTestModel()
			before := repositoryTestRecord(source, false)
			switch mode {
			case "key":
				source.Rates[0].Key = "source/renamed"
			case "currency":
				source.Rates[0].Value.Currency = "EUR"
			case "unit":
				source.Rates[0].Value.Unit = "1_TOKEN"
			}
			plan := repositoryModelPlan(before, source, RepositoryPriceSelection{Mode: "sync"}, repositoryTestFX())
			if len(plan.Errors) == 0 {
				t.Fatal("ambiguous transition silently accepted")
			}
		})
	}
	source := repositoryTestModel()
	source.Rates[0].Value.Currency = "EUR"
	plan := repositoryModelPlan(repositoryTestRecord(source, true), source, RepositoryPriceSelection{Mode: "restore", RateIDs: []string{"rat_input"}}, repositoryTestFX())
	if len(plan.Errors) != 1 || plan.Errors[0].Code != "missing_exchange_rate" {
		t.Fatal("missing FX accepted", plan)
	}
}
func TestRepositorySelectionCanonicalBoundsAndExactIdentity(t *testing.T) {
	selection, err := canonicalRepositorySelection(RepositoryPriceSelection{Mode: "sync", ProviderModelIDs: []string{"pmo_b", "pmo_a"}})
	if err != nil || selection.ProviderModelIDs[0] != "pmo_a" || selection.RateIDs == nil {
		t.Fatal("canonical selection rejected")
	}
	for _, input := range []RepositoryPriceSelection{{Mode: "sync"}, {Mode: "sync", ProviderModelIDs: []string{"pmo_a", "pmo_a"}}, {Mode: "sync", ProviderModelIDs: []string{"pmo_a"}, RateIDs: []string{"rat_a"}}, {Mode: "restore", ProviderModelIDs: []string{"pmo_a"}}, {Mode: "restore", ProviderModelIDs: []string{"pmo_a"}, RateIDs: []string{"rat_a "}}, {Mode: "other", ProviderModelIDs: []string{"pmo_a"}}} {
		if _, err := canonicalRepositorySelection(input); err == nil {
			t.Fatal("invalid selector accepted", input)
		}
	}
	for _, reason := range []string{"", " trailing", "trailing ", "line\nbreak", string([]byte{255}), strings.Repeat("界", 1001)} {
		if repositoryReason(reason) {
			t.Fatal("invalid reason accepted")
		}
	}
}
func TestRepositoryReviewBindsBytesActorCatalogueConfigurationAndMappings(t *testing.T) {
	a, err := prices.Parse([]byte(`{"schema_version":1,"models":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	b, err := prices.Parse([]byte(`{"schema_version":1,"models":[] }`))
	if err != nil {
		t.Fatal(err)
	}
	setting := entity.PricingSetting{ETag: "random_existing_32_urlsafe_value"}
	config := entity.RepositoryPriceSetting{ETag: strings.Repeat("0", 64)}
	etag := repositoryReview("usr_a", a, setting, config, []RepositoryPriceMappingInput{})
	if !personalModelETag(etag) || etag == setting.ETag {
		t.Fatal("composite review is not SHA64")
	}
	for _, changed := range []string{repositoryReview("usr_A", a, setting, config, nil), repositoryReview("usr_a", b, setting, config, nil), repositoryReview("usr_a", a, entity.PricingSetting{ETag: "changed"}, config, nil), repositoryReview("usr_a", a, setting, entity.RepositoryPriceSetting{Enabled: true, ETag: config.ETag}, nil), repositoryReview("usr_a", a, setting, config, []RepositoryPriceMappingInput{{"pmo_a", "exact/source"}})} {
		if changed == etag {
			t.Fatal("review omitted authority/source/current input")
		}
	}
}
func TestRepositoryTypedAuditUsesRecordedSafeProjection(t *testing.T) {
	enabled := false
	record := RepositoryPriceAudit{RequestID: "11111111-1111-4111-8111-111111111111", SourceDigest: strings.Repeat("a", 64), Reason: "Reviewed setting", Mode: "configure", Enabled: &enabled}
	raw, _ := json.Marshal(struct {
		Source string               `json:"source"`
		After  RepositoryPriceAudit `json:"after"`
	}{prices.SourceID, record})
	value := string(raw[:len(raw)-1]) + `,"secret":"must_not_render"}`
	row := entity.AuditEvent{Action: "prices.repository.config", ResourceType: "pricing", DetailsJSON: &value}
	result := auditRecord(row)
	if result.Changes == nil || result.Source == nil || *result.Source != prices.SourceID || strings.Contains(string(result.Changes), "must_not_render") {
		t.Fatal("typed audit dispatch absent/private", string(result.Changes))
	}
	row.Action = "prices.repository.apply"
	if _, ok := repositoryPriceAuditProjection(row); ok {
		t.Fatal("configure receipt borrowed apply shape")
	}
}
func TestRepositoryRateSourcesDoNotChangeImmutablePricingRateShape(t *testing.T) {
	custom := repositoryRateSource(entity.PriceRate{ID: "rat_old"})
	if custom.Kind != "custom" || custom.SourceRateKey != nil {
		t.Fatal("historical repository inferred")
	}
	typeOf := reflect.TypeFor[pricing.Rate]()
	for _, name := range []string{"RepositoryModelKey", "RepositoryRateKey", "Source"} {
		if _, found := typeOf.FieldByName(name); found {
			t.Fatal("ownership polluted immutable billing type")
		}
	}
}

func TestRepositorySourceRateIdentityCannotMoveAcrossBusinessSlots(t *testing.T) {
	source := repositoryTestModel()
	before := repositoryTestRecord(source, false)
	source.Rates = source.Rates[:1]
	source.Rates[0].Value.Metric = pricing.CacheRead
	plan := repositoryModelPlan(before, source, RepositoryPriceSelection{Mode: "sync"}, repositoryTestFX())
	if len(plan.Errors) != 1 || plan.Errors[0].Code != "business_key_changed" || len(plan.Input.Rates) != 0 {
		t.Fatal("source identity silently created a different metric", plan)
	}
	source = repositoryTestModel()
	source.Key = "other/model"
	plan = repositoryModelPlan(before, source, RepositoryPriceSelection{Mode: "sync"}, repositoryTestFX())
	if len(plan.Errors) == 0 {
		t.Fatal("mapping change borrowed old ownership")
	}
}

func TestRepositoryReceiptDigestsBindActualStateNotOnlyETags(t *testing.T) {
	modelKey, rateKey := "source/model", "source/input"
	data := &runtimePricingData{Setting: entity.PricingSetting{ID: 1, ETag: "original", PlatformCurrency: "USD"}, FX: []entity.PricingExchangeRate{{Currency: "USD", Rate: "1"}}, Prices: []entity.ModelPrice{{ID: "prc_a", ProviderModelID: "pmo_a"}}, Rates: []entity.PriceRate{{ID: "rat_a", ModelPriceID: "prc_a", Amount: "0", RepositoryModelKey: &modelKey, RepositoryRateKey: &rateKey}}}
	initial := repositoryCatalogueDigest(data)
	data.Prices[0].CreatedAt = time.Now()
	data.Prices[0].UpdatedAt = time.Now()
	if repositoryCatalogueDigest(data) != initial {
		t.Fatal("incidental timestamps changed receipt application")
	}
	for _, mode := range []string{"amount", "ownership", "fx", "row_id"} {
		t.Run(mode, func(t *testing.T) {
			copy := *data
			copy.Rates = slices.Clone(data.Rates)
			copy.FX = slices.Clone(data.FX)
			switch mode {
			case "amount":
				copy.Rates[0].Amount = "0.000000000000000001"
			case "ownership":
				copy.Rates[0].RepositoryModelKey = nil
				copy.Rates[0].RepositoryRateKey = nil
			case "fx":
				copy.FX[0].Rate = "2"
			case "row_id":
				copy.Rates[0].ID = "rat_readded"
			}
			if repositoryCatalogueDigest(&copy) == initial {
				t.Fatal("unchanged ETag borrowed original receipt proof")
			}
		})
	}
	config := entity.RepositoryPriceSetting{ETag: strings.Repeat("0", 64)}
	mappings := []RepositoryPriceMappingInput{{ProviderModelID: "pmo_a", SourceModelKey: "source/model"}}
	before := repositoryConfigurationDigest(config, mappings)
	config.Enabled = true
	if repositoryConfigurationDigest(config, mappings) == before {
		t.Fatal("raw enablement change borrowed proof")
	}
	config.Enabled = false
	mappings[0].SourceModelKey = "source/other"
	if repositoryConfigurationDigest(config, mappings) == before {
		t.Fatal("raw mapping change borrowed proof")
	}
}

func TestRepositoryAuditBoundIncludesEveryAddedRateOwnership(t *testing.T) {
	modelKey, rateKey := strings.Repeat("m", 128), strings.Repeat("r", 128)
	var plans []repositoryPlan
	for range 20 {
		plan := repositoryPlan{Before: PriceRecord{Rates: []pricing.Rate{}, RateSources: map[string]PriceRateSource{}}, Input: PriceInput{Rates: []pricing.Rate{{Metric: pricing.Input}}}}
		for _, metric := range []string{pricing.Input, pricing.Output, pricing.CacheRead, pricing.CacheWrite} {
			for _, tier := range []string{pricing.Base, pricing.Long} {
				rate := pricing.Rate{Metric: metric, Tier: tier, Unit: pricing.Unit, Currency: "USD", Amount: "9007199254740993.000000000000000001", Enabled: true}
				plan.Changes = append(plan.Changes, RepositoryPriceChange{Action: "added", After: &rate, AfterSource: PriceRateSource{Kind: "repository", SourceModelKey: &modelKey, SourceRateKey: &rateKey}})
			}
		}
		for _, metric := range []string{pricing.ImageInput, pricing.PDFInput} {
			rate := pricing.Rate{Metric: metric, Tier: pricing.Base, Unit: "1_IMAGE", Currency: "USD", Amount: "1", Enabled: true}
			if metric == pricing.PDFInput {
				rate.Unit = "1_PDF"
			}
			plan.Changes = append(plan.Changes, RepositoryPriceChange{Action: "added", After: &rate, AfterSource: PriceRateSource{Kind: "repository", SourceModelKey: &modelKey, SourceRateKey: &rateKey}})
		}
		plans = append(plans, plan)
	}
	if !repositoryAuditFits(plans[:1], strings.Repeat("a", 64), "sync") {
		t.Fatal("bounded single-model audit refused")
	}
	if repositoryAuditFits(plans, strings.Repeat("a", 64), "sync") {
		t.Fatal("audit bound omitted distinct added-rate provenance entries")
	}
}
