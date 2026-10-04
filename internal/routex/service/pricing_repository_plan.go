package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/pricing"
	"github.com/miclle/routex/prices"
	"gorm.io/gorm"
)

type repositoryPlan struct {
	Before           PriceRecord
	Input            PriceInput
	ThresholdKey     *string
	Changes          []RepositoryPriceChange
	Errors, Warnings []RepositoryPriceIssue
}

func canonicalRepositorySelection(selection RepositoryPriceSelection) (RepositoryPriceSelection, error) {
	if (selection.Mode != "sync" && selection.Mode != "restore") || len(selection.ProviderModelIDs) < 1 || len(selection.ProviderModelIDs) > 20 || len(selection.RateIDs) > 200 || (selection.Mode == "sync" && len(selection.RateIDs) != 0) || (selection.Mode == "restore" && len(selection.RateIDs) == 0) {
		return selection, apperrors.ErrBadRequest
	}
	selection.ProviderModelIDs = slices.Clone(selection.ProviderModelIDs)
	selection.RateIDs = slices.Clone(selection.RateIDs)
	if selection.RateIDs == nil {
		selection.RateIDs = []string{}
	}
	for _, items := range [][]string{selection.ProviderModelIDs, selection.RateIDs} {
		slices.Sort(items)
		for index, item := range items {
			if !safeTeamSessionID(item) || (index > 0 && items[index-1] == item) {
				return selection, apperrors.ErrBadRequest
			}
		}
	}
	return selection, nil
}
func repositoryTokenRate(metric string) bool { return strings.HasSuffix(metric, "_TOKEN") }
func repositoryModelPlan(before PriceRecord, source prices.Model, selection RepositoryPriceSelection, fx pricing.FX) repositoryPlan {
	plan := repositoryPlan{Before: before, Input: PriceInput{ProviderModelID: before.ProviderModelID, Rates: []pricing.Rate{}}, ThresholdKey: before.ContextThresholdSource.SourceModelKey, Changes: []RepositoryPriceChange{}, Errors: []RepositoryPriceIssue{}, Warnings: []RepositoryPriceIssue{}}
	issue := func(rateID *string, code, message string) {
		plan.Errors = append(plan.Errors, RepositoryPriceIssue{before.ProviderModelID, rateID, code, message})
	}
	selected := map[string]bool{}
	for _, id := range selection.RateIDs {
		selected[id] = true
	}
	existing := map[string]pricing.Rate{}
	for _, rate := range before.Rates {
		existing[rate.Metric+"/"+rate.Tier] = rate
	}
	if selection.Mode == "sync" {
		for _, rate := range before.Rates {
			owner := before.RateSources[rate.ID]
			if owner.Kind == "repository" && (owner.SourceModelKey == nil || *owner.SourceModelKey != source.Key) {
				id := rate.ID
				issue(&id, "source_identity_changed", "Restore this exact rate explicitly before changing its mapped source.")
			}
		}
	}
	matched := map[string]bool{}
	threshold := before.ContextThreshold
	requiresThreshold := false
	sourceRates := slices.Clone(source.Rates)
	slices.SortFunc(sourceRates, func(a, b prices.Rate) int {
		return compareStrings(a.Value.Metric+"/"+a.Value.Tier, b.Value.Metric+"/"+b.Value.Tier)
	})
	for _, sourceRate := range sourceRates {
		after := sourceRate.Value
		if selection.Mode == "sync" {
			moved := false
			for _, rate := range before.Rates {
				owner := before.RateSources[rate.ID]
				if owner.Kind == "repository" && owner.SourceModelKey != nil && owner.SourceRateKey != nil && *owner.SourceModelKey == source.Key && *owner.SourceRateKey == sourceRate.Key && (rate.Metric != after.Metric || rate.Tier != after.Tier) {
					id := rate.ID
					issue(&id, "business_key_changed", "The source rate identity changed metric or tier; maintain this exact rate explicitly.")
					moved = true
				}
			}
			if moved {
				continue
			}
		}
		old, found := existing[after.Metric+"/"+after.Tier]
		var prior *pricing.Rate
		var rateID *string
		owner := PriceRateSource{Kind: "custom"}
		if found {
			copy := old
			prior = &copy
			id := old.ID
			rateID = &id
			after.ID = old.ID
			if value, ok := before.RateSources[old.ID]; ok {
				owner = value
			}
		}
		if selection.Mode == "restore" && (!found || !selected[old.ID]) {
			continue
		}
		if found {
			matched[old.ID] = true
		}
		change := RepositoryPriceChange{ProviderModelID: before.ProviderModelID, RateID: rateID, SourceModelKey: source.Key, SourceRateKey: sourceRate.Key, Before: prior, After: &after, BeforeSource: owner, AfterSource: PriceRateSource{Kind: "repository", SourceModelKey: &source.Key, SourceRateKey: &sourceRate.Key}, ThresholdBefore: before.ContextThreshold, ThresholdAfter: before.ContextThreshold}
		if selection.Mode == "sync" && found && owner.Kind != "repository" {
			change.Action = "protected_custom"
			copy := old
			change.After = &copy
			change.AfterSource = owner
			plan.Changes = append(plan.Changes, change)
			continue
		}
		if selection.Mode == "sync" && found && (owner.SourceModelKey == nil || owner.SourceRateKey == nil || *owner.SourceModelKey != source.Key || *owner.SourceRateKey != sourceRate.Key) {
			issue(rateID, "source_identity_changed", "Restore this exact rate explicitly before changing its repository identity.")
			continue
		}
		if selection.Mode == "sync" && found && (old.Unit != after.Unit || old.Currency != after.Currency) {
			issue(rateID, "business_key_changed", "Restore this exact rate explicitly before changing its unit or currency.")
			continue
		}
		if repositoryTokenRate(after.Metric) {
			requiresThreshold = true
		}
		change.Action = "added"
		if found {
			change.Action = "updated"
			if old == after && owner.Kind == "repository" && owner.SourceModelKey != nil && owner.SourceRateKey != nil && *owner.SourceModelKey == source.Key && *owner.SourceRateKey == sourceRate.Key {
				change.Action = "unchanged"
			}
		}
		if after.Enabled && after.Currency != fx.PlatformCurrency {
			if _, ok := fx.Rates[after.Currency]; !ok {
				issue(rateID, "missing_exchange_rate", "Configure conversion before enabling this repository price.")
			}
		}
		plan.Changes = append(plan.Changes, change)
		if change.Action != "unchanged" {
			input := after
			input.ID = ""
			plan.Input.Rates = append(plan.Input.Rates, input)
		}
	}
	for _, rate := range before.Rates {
		if selection.Mode == "restore" && selected[rate.ID] && !matched[rate.ID] {
			id := rate.ID
			issue(&id, "missing_source_rate", "The selected rate has no exact metric/tier in this mapped source.")
		}
		if selection.Mode == "sync" && !matched[rate.ID] {
			id := rate.ID
			plan.Warnings = append(plan.Warnings, RepositoryPriceIssue{before.ProviderModelID, &id, "missing_source_rate", "The local rate is retained because this source has no matching metric/tier."})
		}
	}
	if requiresThreshold {
		threshold = source.ContextThreshold
		if threshold != before.ContextThreshold && before.ID != "" {
			if selection.Mode == "sync" && (before.ContextThresholdSource.Kind != "repository" || before.ContextThresholdSource.SourceModelKey == nil || *before.ContextThresholdSource.SourceModelKey != source.Key) {
				issue(nil, "custom_threshold", "Restore selected token rates explicitly before changing a custom threshold.")
			}
			for _, rate := range before.Rates {
				if !repositoryTokenRate(rate.Metric) {
					continue
				}
				owner := before.RateSources[rate.ID]
				safe := owner.Kind == "repository" && owner.SourceModelKey != nil && *owner.SourceModelKey == source.Key
				if !safe && (selection.Mode != "restore" || !selected[rate.ID]) {
					id := rate.ID
					issue(&id, "protected_threshold", "Changing the shared threshold would reinterpret an unselected custom token rate.")
				}
			}
		}
		if before.ID == "" || selection.Mode == "restore" || before.ContextThresholdSource.Kind == "repository" {
			key := source.Key
			plan.ThresholdKey = &key
		}
	}
	plan.Input.ContextThreshold = &threshold
	merged := priceSchedule(before)
	merged.ContextThreshold = threshold
	merged.Rates = slices.Clone(merged.Rates)
	for _, rate := range plan.Input.Rates {
		found := false
		for index, old := range merged.Rates {
			if old.Metric == rate.Metric && old.Tier == rate.Tier {
				rate.ID = old.ID
				merged.Rates[index] = rate
				found = true
				break
			}
		}
		if !found {
			merged.Rates = append(merged.Rates, rate)
		}
	}
	if pricing.ValidateSchedule(merged) != nil {
		issue(nil, "invalid_schedule", "The resulting native schedule is invalid.")
	}
	for index := range plan.Changes {
		plan.Changes[index].ThresholdAfter = threshold
		if threshold != before.ContextThreshold && plan.Changes[index].Action == "unchanged" {
			plan.Changes[index].Action = "updated"
		}
	}
	return plan
}
func repositoryLoadPrice(tx *gorm.DB, providerModelID string) (PriceRecord, error) {
	model, connection, err := loadExactPriceSubject(tx, providerModelID)
	if err != nil {
		return PriceRecord{}, err
	}
	var aggregate entity.ModelPrice
	err = personalExact(tx, "provider_model_id", providerModelID).Take(&aggregate).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return PriceRecord{ProviderID: connection.ProviderID, ProviderModelID: model.ID, UpstreamName: model.UpstreamName, Protocol: connection.Protocol, Rates: []pricing.Rate{}, RateSources: map[string]PriceRateSource{}, ContextThresholdSource: PriceThresholdSource{Kind: "custom"}}, nil
	}
	if err != nil {
		return PriceRecord{}, err
	}
	return loadPrice(tx, aggregate)
}
func repositoryPreview(tx *gorm.DB, actorID string, source *prices.Snapshot, setting entity.PricingSetting, config entity.RepositoryPriceSetting, mappings []RepositoryPriceMappingInput, selection RepositoryPriceSelection) (*RepositoryPricePreview, []repositoryPlan, error) {
	preview := &RepositoryPricePreview{ReviewETag: repositoryReview(actorID, source, setting, config, mappings), SourceDigest: source.Digest(), Mode: selection.Mode, Changes: []RepositoryPriceChange{}, Errors: []RepositoryPriceIssue{}, Warnings: []RepositoryPriceIssue{}}
	plans := []repositoryPlan{}
	if selection.Mode == "sync" && !config.Enabled {
		preview.Errors = append(preview.Errors, RepositoryPriceIssue{Code: "sync_disabled", Message: "Enable repository synchronization before reviewing an apply."})
	}
	fx, err := pricingFX(tx, setting)
	if err != nil {
		return nil, nil, err
	}
	if pricing.ValidateFX(fx) != nil {
		preview.Errors = append(preview.Errors, RepositoryPriceIssue{Code: "invalid_exchange_rates", Message: "Review the current exact exchange rates before applying repository prices."})
		return preview, nil, nil
	}
	mapping := map[string]string{}
	for _, item := range mappings {
		mapping[item.ProviderModelID] = item.SourceModelKey
	}
	foundRates := map[string]bool{}
	for _, modelID := range selection.ProviderModelIDs {
		before, err := repositoryLoadPrice(tx, modelID)
		if err != nil {
			return nil, nil, err
		}
		for _, rate := range before.Rates {
			foundRates[rate.ID] = true
		}
		mapped, ok := source.Lookup(mapping[modelID])
		if !ok {
			preview.Errors = append(preview.Errors, RepositoryPriceIssue{ProviderModelID: modelID, Code: "unmatched_model", Message: "Select an exact current source mapping for this model."})
			continue
		}
		if before.Protocol != mapped.Protocol {
			preview.Errors = append(preview.Errors, RepositoryPriceIssue{ProviderModelID: modelID, Code: "protocol_mismatch", Message: "The mapped native protocol no longer matches this model."})
			continue
		}
		plan := repositoryModelPlan(before, mapped, selection, fx)
		plans = append(plans, plan)
		preview.Changes = append(preview.Changes, plan.Changes...)
		preview.Errors = append(preview.Errors, plan.Errors...)
		preview.Warnings = append(preview.Warnings, plan.Warnings...)
	}
	for _, rateID := range selection.RateIDs {
		if !foundRates[rateID] {
			id := rateID
			preview.Errors = append(preview.Errors, RepositoryPriceIssue{RateID: &id, Code: "unselected_rate", Message: "The selected rate does not belong to a selected model."})
		}
	}
	if !repositoryAuditFits(plans, source.Digest(), selection.Mode) {
		preview.Errors = append(preview.Errors, RepositoryPriceIssue{Code: "audit_too_large", Message: "The normalized changes exceed the audit limit; select fewer models."})
	}
	preview.Valid = len(preview.Errors) == 0
	if preview.Valid {
		preview.PreviewDigest = repositoryHash(struct {
			Review    string
			Selection RepositoryPriceSelection
			Changes   []RepositoryPriceChange
			Warnings  []RepositoryPriceIssue
		}{preview.ReviewETag, selection, preview.Changes, preview.Warnings})
	}
	return preview, plans, nil
}
func (s *Service) PreviewRepositoryPrices(ctx context.Context, actorID string, selection RepositoryPriceSelection) (*RepositoryPricePreview, error) {
	selection, err := canonicalRepositorySelection(selection)
	if err != nil {
		return nil, err
	}
	source, err := s.repositoryPriceSnapshot()
	if err != nil {
		return nil, apperrors.ErrInternal
	}
	var result *RepositoryPricePreview
	err = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := exactCatalogPermission(tx, actorID, "prices.read"); err != nil {
			return err
		}
		var setting entity.PricingSetting
		if err := tx.Take(&setting, 1).Error; err != nil {
			return err
		}
		config, mappings, err := repositoryConfig(tx)
		if err != nil {
			return err
		}
		result, _, err = repositoryPreview(tx, actorID, source, setting, config, mappings, selection)
		return err
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return result, pricingError(err)
}

func repositoryAuditFits(plans []repositoryPlan, digest, mode string) bool {
	record := RepositoryPriceAudit{RequestID: "11111111-1111-4111-8111-111111111111", SourceDigest: digest, Reason: strings.Repeat("\U0001F600", 1000), Mode: mode}
	for _, plan := range plans {
		if len(plan.Input.Rates) == 0 && (plan.Input.ContextThreshold == nil || *plan.Input.ContextThreshold == plan.Before.ContextThreshold) {
			continue
		}
		before := plan.Before
		after := before
		after.Rates = slices.Clone(before.Rates)
		after.RateSources = maps.Clone(before.RateSources)
		if after.RateSources == nil {
			after.RateSources = map[string]PriceRateSource{}
		}
		if after.ID == "" {
			after.ID = "prc_" + strings.Repeat("0", 26)
		}
		after.UpdateSource = "repository"
		after.FollowRepository = true
		if plan.Input.ContextThreshold != nil {
			after.ContextThreshold = *plan.Input.ContextThreshold
		}
		after.ContextThresholdSource = PriceThresholdSource{Kind: "custom"}
		if plan.ThresholdKey != nil {
			after.ContextThresholdSource = PriceThresholdSource{Kind: "repository", SourceModelKey: plan.ThresholdKey}
		}
		placeholderIndex := 0
		for _, change := range plan.Changes {
			if change.Action == "protected_custom" || change.Action == "unchanged" || change.After == nil {
				continue
			}
			rate := *change.After
			if rate.ID == "" {
				for {
					rate.ID = fmt.Sprintf("rat_%026d", placeholderIndex)
					placeholderIndex++
					if _, occupied := after.RateSources[rate.ID]; !occupied {
						break
					}
				}
			}
			found := false
			for index, old := range after.Rates {
				if old.Metric == rate.Metric && old.Tier == rate.Tier {
					after.Rates[index] = rate
					found = true
					break
				}
			}
			if !found {
				after.Rates = append(after.Rates, rate)
			}
			after.RateSources[rate.ID] = change.AfterSource
		}
		record.Before = append(record.Before, before)
		record.After = append(record.After, after)
	}
	raw, err := json.Marshal(struct {
		Source string               `json:"source"`
		Before any                  `json:"before"`
		After  RepositoryPriceAudit `json:"after"`
	}{prices.SourceID, nil, record})
	return err == nil && len(raw) <= 60*1024
}
