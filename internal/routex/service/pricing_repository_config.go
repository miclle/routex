package service

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/prices"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func repositoryConfig(tx *gorm.DB) (entity.RepositoryPriceSetting, []RepositoryPriceMappingInput, error) {
	var config entity.RepositoryPriceSetting
	if err := tx.Take(&config, 1).Error; err != nil {
		return config, nil, err
	}
	var rows []entity.RepositoryPriceMapping
	if err := tx.Order("provider_model_id").Limit(repositoryMaxMappings + 1).Find(&rows).Error; err != nil {
		return config, nil, err
	}
	if len(rows) > repositoryMaxMappings {
		return config, nil, apperrors.ErrBadRequest
	}
	mappings := make([]RepositoryPriceMappingInput, 0, len(rows))
	for _, row := range rows {
		mappings = append(mappings, RepositoryPriceMappingInput{row.ProviderModelID, row.SourceModelKey})
	}
	slices.SortFunc(mappings, func(a, b RepositoryPriceMappingInput) int {
		return compareStrings(a.ProviderModelID, b.ProviderModelID)
	})
	return config, mappings, nil
}
func compareStrings(a, b string) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}
func repositoryReview(actorID string, source *prices.Snapshot, setting entity.PricingSetting, config entity.RepositoryPriceSetting, mappings []RepositoryPriceMappingInput) string {
	return repositoryHash(struct {
		Actor, Source, Catalogue, Config string
		Enabled                          bool
		Mappings                         []RepositoryPriceMappingInput
	}{actorID, source.Digest(), setting.ETag, config.ETag, config.Enabled, mappings})
}
func repositoryConfigView(actorID string, source *prices.Snapshot, setting entity.PricingSetting, config entity.RepositoryPriceSetting, mappings []RepositoryPriceMappingInput, write bool) *RepositoryPriceConfigView {
	models := source.Models()
	view := &RepositoryPriceConfigView{ReviewETag: repositoryReview(actorID, source, setting, config, mappings), Enabled: config.Enabled, CanWrite: write, Mappings: mappings, Source: RepositoryPriceSourceView{ID: prices.SourceID, Digest: source.Digest(), ModelCount: len(models), Models: []RepositorySourceModel{}}, LastAttemptAt: config.LastAttemptAt, LastSuccessAt: config.LastSuccessAt, LastResult: config.LastResult}
	for _, m := range models {
		view.Source.Models = append(view.Source.Models, RepositorySourceModel{m.Key, m.ProviderKey, m.Name, m.Protocol, m.ContextThreshold})
	}
	slices.SortFunc(view.Source.Models, func(a, b RepositorySourceModel) int { return compareStrings(a.Key, b.Key) })
	return view
}
func (s *Service) GetRepositoryPriceSource(ctx context.Context, actorID string) (*RepositoryPriceConfigView, error) {
	source, err := s.repositoryPriceSnapshot()
	if err != nil {
		return nil, apperrors.ErrInternal
	}
	var result *RepositoryPriceConfigView
	err = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := exactCatalogPermission(tx, actorID, "prices.read"); err != nil {
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
		actor, err := exactEnabledActor(tx, actorID)
		if err != nil {
			return err
		}
		write, err := exactGovernancePermission(tx, actor, "prices.write")
		if err != nil {
			return err
		}
		result = repositoryConfigView(actorID, source, setting, config, mappings, write)
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	return result, pricingError(err)
}
func validateRepositoryMappings(tx *gorm.DB, source *prices.Snapshot, mappings []RepositoryPriceMappingInput) error {
	if len(mappings) > repositoryMaxMappings {
		return apperrors.ErrBadRequest
	}
	seen := map[string]bool{}
	for _, item := range mappings {
		if !safeTeamSessionID(item.ProviderModelID) || !repositorySourceKey.MatchString(item.SourceModelKey) || seen[item.ProviderModelID] {
			return apperrors.ErrBadRequest
		}
		seen[item.ProviderModelID] = true
		_, connection, err := loadExactPriceSubject(tx, item.ProviderModelID)
		if err != nil {
			return err
		}
		model, ok := source.Lookup(item.SourceModelKey)
		if !ok || model.Protocol != connection.Protocol {
			return apperrors.ErrBadRequest
		}
	}
	return nil
}
func (s *Service) WriteRepositoryPriceSource(ctx context.Context, actorID, etag string, input RepositoryPriceConfigInput) (*RepositoryPriceResult, error) {
	if !personalModelETag(etag) || !repositoryReason(input.Reason) || !credentialReplacementRequestID.MatchString(input.RequestID) {
		return nil, apperrors.ErrBadRequest
	}
	source, err := s.repositoryPriceSnapshot()
	if err != nil {
		return nil, apperrors.ErrInternal
	}
	mappings := slices.Clone(input.Mappings)
	if mappings == nil {
		mappings = []RepositoryPriceMappingInput{}
	}
	slices.SortFunc(mappings, func(a, b RepositoryPriceMappingInput) int {
		return compareStrings(a.ProviderModelID, b.ProviderModelID)
	})
	input.Mappings = mappings
	hash := repositoryHash(struct {
		Actor, Action, Review string
		Input                 RepositoryPriceConfigInput
	}{actorID, "configure", etag, input})
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
		config, before, err := repositoryConfig(tx)
		if err != nil {
			return err
		}
		if repositoryReview(actorID, source, setting, config, before) != etag {
			return pricingStale
		}
		if err := validateRepositoryMappings(tx, source, mappings); err != nil {
			return err
		}
		config.Enabled = input.Enabled
		config.ETag = repositoryHash(struct {
			Before string
			Input  RepositoryPriceConfigInput
		}{config.ETag, input})
		if err := tx.Save(&config).Error; err != nil {
			return err
		}
		if err := tx.Where("1 = 1").Delete(&entity.RepositoryPriceMapping{}).Error; err != nil {
			return err
		}
		for _, m := range mappings {
			if err := tx.Create(&entity.RepositoryPriceMapping{ProviderModelID: m.ProviderModelID, SourceModelKey: m.SourceModelKey}).Error; err != nil {
				return err
			}
		}
		receipt = entity.RepositoryPriceReceipt{RequestID: input.RequestID, ActorID: actorID, RequestHash: hash, SourceDigest: source.Digest(), ReviewETag: etag, Mode: "configure", ConfigDigest: repositoryConfigurationDigest(config, mappings), ConfigETag: config.ETag, CatalogueETag: setting.ETag, CreatedAt: time.Now().UTC().Truncate(time.Microsecond)}
		if err := tx.Create(&receipt).Error; err != nil {
			return err
		}
		if err := appendRepositoryAudit(tx, actorID, "prices.repository.config", "pricing_repository", source.Digest(), input.Reason, RepositoryPriceAudit{RequestID: input.RequestID, Mode: "configure", Enabled: &input.Enabled, Mappings: mappings}); err != nil {
			return err
		}
		created = true
		return personalExact(tx, "request_id", input.RequestID).Take(&receipt).Error
	})
	if err != nil {
		return nil, pricingError(err)
	}
	return s.repositoryResult(ctx, actorID, receipt, created), nil
}
func repositoryKnownReceipt(tx *gorm.DB, actorID, requestID, hash string, receipt *entity.RepositoryPriceReceipt) (bool, error) {
	err := personalExact(tx, "request_id", requestID).Take(receipt).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if receipt.RequestID != requestID || receipt.ActorID != actorID || receipt.RequestHash != hash {
		return false, catalogConflict
	}
	return true, nil
}
func (s *Service) ListRepositoryPriceCandidates(ctx context.Context, actorID string, filter RepositoryPriceCandidateFilter) (*RepositoryPriceCandidatePage, error) {
	if filter.Limit == 0 {
		filter.Limit = 20
	}
	pattern, err := candidatePattern(filter.Query)
	if err != nil || filter.Limit < 1 || filter.Limit > 50 || (filter.Cursor != "" && !safeTeamSessionID(filter.Cursor)) {
		return nil, apperrors.ErrBadRequest
	}
	result := &RepositoryPriceCandidatePage{Items: []RepositoryPriceCandidate{}}
	err = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := exactCatalogPermission(tx, actorID, "prices.read"); err != nil {
			return err
		}
		query := tx.Table("provider_models AS m").Select("m.id AS provider_model_id,m.upstream_name,c.protocol").
			Joins("JOIN provider_connections AS c ON ?", database.ExactTextColumns(tx, clause.Column{Table: "m", Name: "connection_id"}, clause.Column{Table: "c", Name: "id"})).
			Joins("JOIN providers AS p ON ?", database.ExactTextColumns(tx, clause.Column{Table: "c", Name: "provider_id"}, clause.Column{Table: "p", Name: "id"})).
			Where("LOWER(m.upstream_name) LIKE ? ESCAPE '!'", pattern).Order("m.id").Limit(filter.Limit + 1)
		if filter.Cursor != "" {
			query = query.Where("m.id > ?", filter.Cursor)
		}
		if err := query.Scan(&result.Items).Error; err != nil {
			return err
		}
		if len(result.Items) > filter.Limit {
			result.Items = result.Items[:filter.Limit]
			result.NextCursor = result.Items[len(result.Items)-1].ProviderModelID
		}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return result, pricingError(err)
}
