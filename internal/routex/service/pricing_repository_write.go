package service

import (
	"context"
	"database/sql"
	"slices"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"gorm.io/gorm"
)

func repositoryFollowSummary(tx *gorm.DB, model *entity.ModelPrice) error {
	var count int64
	if err := personalExact(tx.Model(&entity.PriceRate{}), "model_price_id", model.ID).Where("repository_model_key IS NOT NULL AND repository_rate_key IS NOT NULL").Count(&count).Error; err != nil {
		return err
	}
	model.FollowRepository = count > 0
	return personalExact(tx.Model(&entity.ModelPrice{}), "id", model.ID).Update("follow_repository", model.FollowRepository).Error
}
func repositorySetRateSource(tx *gorm.DB, modelPriceID string, change RepositoryPriceChange) error {
	query := func() *gorm.DB {
		return personalExact(personalExact(personalExact(tx.Model(&entity.PriceRate{}), "model_price_id", modelPriceID), "metric", change.After.Metric), "tier", change.After.Tier)
	}
	result := query().Updates(map[string]any{"repository_model_key": change.SourceModelKey, "repository_rate_key": change.SourceRateKey})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 1 {
		return nil
	}
	if result.RowsAffected != 0 {
		return catalogConflict
	}
	// MySQL may report zero changed rows when this exact ownership already
	// matches. Verify stored identity and provenance rather than treating a
	// missing row as a successful update.
	var rate entity.PriceRate
	if err := query().Take(&rate).Error; err != nil {
		return err
	}
	if rate.ModelPriceID != modelPriceID || rate.Metric != change.After.Metric || rate.Tier != change.After.Tier || rate.RepositoryModelKey == nil || *rate.RepositoryModelKey != change.SourceModelKey || rate.RepositoryRateKey == nil || *rate.RepositoryRateKey != change.SourceRateKey {
		return catalogConflict
	}
	return nil
}
func (s *Service) ApplyRepositoryPrices(ctx context.Context, actorID, etag string, input RepositoryPriceApplyInput) (*RepositoryPriceResult, error) {
	if !personalModelETag(etag) || !personalModelETag(input.PreviewDigest) || !credentialReplacementRequestID.MatchString(input.RequestID) || !repositoryReason(input.Reason) {
		return nil, apperrors.ErrBadRequest
	}
	selection, err := canonicalRepositorySelection(input.Selection)
	if err != nil {
		return nil, err
	}
	input.Selection = selection
	source, err := s.repositoryPriceSnapshot()
	if err != nil {
		return nil, apperrors.ErrInternal
	}
	hash := repositoryHash(struct {
		Actor, Review string
		Input         RepositoryPriceApplyInput
	}{actorID, etag, input})
	var receipt entity.RepositoryPriceReceipt
	created := false
	err = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		if err := exactCatalogPermission(tx, actorID, "prices.write"); err != nil {
			return err
		}
		if replay, err := repositoryKnownReceipt(tx, actorID, input.RequestID, hash, &receipt); err != nil || replay {
			return err
		}
		setting, err := lockPricing(tx)
		if err != nil {
			return err
		}
		config, mappings, err := repositoryConfig(tx)
		if err != nil {
			return err
		}
		if repositoryReview(actorID, source, setting, config, mappings) != etag {
			return pricingStale
		}
		preview, plans, err := repositoryPreview(tx, actorID, source, setting, config, mappings, selection)
		if err != nil {
			return err
		}
		if !preview.Valid {
			return &apperrors.Error{Code: 422, Message: "repository price selection is invalid; review the located preview errors"}
		}
		if preview.PreviewDigest != input.PreviewDigest {
			return pricingStale
		}
		before, after := []PriceRecord{}, []PriceRecord{}
		for _, plan := range plans {
			thresholdChange := plan.Input.ContextThreshold != nil && *plan.Input.ContextThreshold != plan.Before.ContextThreshold
			if len(plan.Input.Rates) == 0 && !thresholdChange {
				continue
			}
			_, updated, err := writePrice(tx, plan.Input, "repository")
			if err != nil {
				return err
			}
			for _, change := range plan.Changes {
				if change.Action == "protected_custom" || change.Action == "unchanged" {
					continue
				}
				if change.After == nil {
					return apperrors.ErrInternal
				}
				if err := repositorySetRateSource(tx, updated.ID, change); err != nil {
					return err
				}
			}
			if err := personalExact(tx.Model(&entity.ModelPrice{}), "id", updated.ID).Update("repository_threshold_key", plan.ThresholdKey).Error; err != nil {
				return err
			}
			var model entity.ModelPrice
			if err := personalExact(tx, "id", updated.ID).Take(&model).Error; err != nil {
				return err
			}
			if err := repositoryFollowSummary(tx, &model); err != nil {
				return err
			}
			final, err := loadPrice(tx, model)
			if err != nil {
				return err
			}
			before = append(before, plan.Before)
			after = append(after, final)
		}
		fx, err := pricingFX(tx, setting)
		if err != nil {
			return err
		}
		if err := validateEnabledPriceFX(tx, fx); err != nil {
			return err
		}
		outcome := "no_changes"
		if len(after) > 0 {
			outcome = "committed"
			if err := updatePricingETag(tx, &setting); err != nil {
				return err
			}
		}
		now := time.Now().UTC().Truncate(time.Microsecond)
		config.LastAttemptAt = &now
		config.LastSuccessAt = &now
		config.LastResult = &outcome
		if err := tx.Save(&config).Error; err != nil {
			return err
		}
		published, err := loadRuntimePricing(tx)
		if err != nil {
			return err
		}
		catalogueDigest := repositoryCatalogueDigest(published)
		receipt = entity.RepositoryPriceReceipt{RequestID: input.RequestID, ActorID: actorID, RequestHash: hash, SourceDigest: source.Digest(), ReviewETag: etag, Mode: selection.Mode, ConfigDigest: repositoryConfigurationDigest(config, mappings), CatalogueDigest: &catalogueDigest, ConfigETag: config.ETag, CatalogueETag: setting.ETag, CreatedAt: now}
		if err := tx.Create(&receipt).Error; err != nil {
			return err
		}
		if err := appendRepositoryAudit(tx, actorID, "prices.repository.apply", "pricing_catalogue", source.Digest(), input.Reason, RepositoryPriceAudit{RequestID: input.RequestID, Mode: selection.Mode, Before: before, After: after}); err != nil {
			return err
		}
		created = true
		return personalExact(tx, "request_id", input.RequestID).Take(&receipt).Error
	})
	if err != nil {
		return nil, pricingError(err)
	}
	// Publication is reconciliation only; an original known retry never writes prices.
	_ = s.RefreshRuntime(ctx)
	return s.repositoryResult(ctx, actorID, receipt, created), nil
}
func (s *Service) repositoryReceipt(ctx context.Context, actorID, requestID string) (*RepositoryPriceResult, error) {
	var receipt entity.RepositoryPriceReceipt
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := exactCatalogPermission(tx, actorID, "prices.write"); err != nil {
			return err
		}
		if err := personalExact(tx, "request_id", requestID).Take(&receipt).Error; err != nil {
			return err
		}
		if receipt.ActorID != actorID || receipt.RequestID != requestID {
			return apperrors.ErrNotFound
		}
		return nil
	})
	if err != nil {
		return nil, pricingError(err)
	}
	return s.repositoryResult(ctx, actorID, receipt, false), nil
}
func (s *Service) repositoryResult(ctx context.Context, actorID string, receipt entity.RepositoryPriceReceipt, created bool) *RepositoryPriceResult {
	result := &RepositoryPriceResult{Receipt: RepositoryPriceReceiptView{receipt.RequestID, receipt.SourceDigest, receipt.Mode, receipt.CreatedAt.UTC()}, Committed: true, Created: created, ApplicationStatus: "unavailable"}
	source, err := s.repositoryPriceSnapshot()
	if err != nil {
		return result
	}
	var setting entity.PricingSetting
	var config entity.RepositoryPriceSetting
	var mappings []RepositoryPriceMappingInput
	var catalogueDigest string
	err = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := exactCatalogPermission(tx, actorID, "prices.write"); err != nil {
			return err
		}
		var err error
		setting, err = lockPricing(tx)
		if err != nil {
			return err
		}
		config, mappings, err = repositoryConfig(tx)
		if err != nil {
			return err
		}
		if receipt.Mode != "configure" {
			data, err := loadRuntimePricing(tx)
			if err != nil {
				return err
			}
			catalogueDigest = repositoryCatalogueDigest(data)
		}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return result
	}
	if receipt.Mode == "configure" {
		result.Configuration = repositoryConfigView(actorID, source, setting, config, mappings, true)
		if config.ETag != receipt.ConfigETag || source.Digest() != receipt.SourceDigest || repositoryConfigurationDigest(config, mappings) != receipt.ConfigDigest {
			result.ApplicationStatus = "superseded"
			return result
		}
		result.ConfigurationApplied = true
		result.ApplicationStatus = "applied"
		return result
	}
	if setting.ETag != receipt.CatalogueETag || receipt.CatalogueDigest == nil || catalogueDigest != *receipt.CatalogueDigest {
		result.ApplicationStatus = "superseded"
		return result
	}
	result.ApplicationStatus = "pending"
	if s.repositoryPublicationApplied(ctx, receipt.CatalogueETag, *receipt.CatalogueDigest) {
		result.RuntimeApplied = true
		result.ApplicationStatus = "applied"
	}
	return result
}
func (s *Service) repositoryPublicationApplied(ctx context.Context, catalogueETag, catalogueDigest string) bool {
	if s.runtime == nil {
		return false
	}
	auth, routes := s.runtime.auth.Load(), s.runtime.routes.Load()
	if auth == nil || routes == nil || !time.Now().Before(auth.ValidUntil) || auth.SourceDigest == "" || auth.SourceDigest != routes.Digest {
		return false
	}
	data, err := s.loadRuntimeData(ctx)
	if err != nil || data.Pricing == nil || data.Pricing.Setting.ETag != catalogueETag || repositoryCatalogueDigest(data.Pricing) != catalogueDigest {
		return false
	}
	digest, err := runtimeDigest(data)
	return err == nil && digest == routes.Digest
}

func repositoryConfigurationDigest(config entity.RepositoryPriceSetting, mappings []RepositoryPriceMappingInput) string {
	return repositoryHash(struct {
		Enabled  bool
		Mappings []RepositoryPriceMappingInput
	}{config.Enabled, mappings})
}
func repositoryCatalogueDigest(data *runtimePricingData) string {
	copy := *data
	copy.Prices = slices.Clone(data.Prices)
	// Business identities, schedules, provenance and FX are proof; incidental
	// persistence timestamps do not change the current catalogue configuration.
	for index := range copy.Prices {
		copy.Prices[index].CreatedAt = time.Time{}
		copy.Prices[index].UpdatedAt = time.Time{}
	}
	return repositoryHash(copy)
}
