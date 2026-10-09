package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"slices"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/id"
	"github.com/miclle/routex/pkg/pricing"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

type ModelRoutingSupply struct {
	ProviderName        string `json:"provider_name"`
	ConnectionName      string `json:"connection_name"`
	VerificationCovered bool   `json:"verification_covered"`
	ConfiguredAvailable bool   `json:"configured_available"`
}
type ModelRoutingProvider struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type ModelRoutingProviderPage struct {
	Items      []ModelRoutingProvider `json:"items"`
	NextCursor *string                `json:"next_cursor"`
}
type ModelRoutingPrice struct {
	Amount   string `json:"amount"`
	Currency string `json:"currency"`
	Enabled  bool   `json:"enabled"`
}
type ModelRoutingPrices struct {
	Input  []ModelRoutingPrice `json:"input"`
	Output []ModelRoutingPrice `json:"output"`
}
type ModelRoutingCandidate struct {
	Prices       *ModelRoutingPrices `json:"prices"`
	ID           string              `json:"id"`
	ProviderID   string              `json:"provider_id"`
	ConnectionID string              `json:"connection_id"`
	UpstreamName string              `json:"upstream_name"`
	Protocol     string              `json:"protocol"`
	ModelRoutingSupply
	Selectable bool   `json:"selectable"`
	ReviewETag string `json:"review_etag"`
}
type ModelRoutingCandidatePage struct {
	Items      []ModelRoutingCandidate `json:"items"`
	NextCursor *string                 `json:"next_cursor"`
}

// Both projections are resource-bound reads, never an alternate global directory.
func routingRead(tx *gorm.DB, actor, modelID, protocol string) (entity.Model, []entity.ModelProviderBinding, error) {
	var model entity.Model
	var bindings []entity.ModelProviderBinding
	if err := modelCreationRead(tx, actor); err != nil {
		return model, nil, err
	}
	if err := personalExact(modelCreationDB(tx), "id", modelID).Take(&model).Error; err != nil {
		return model, nil, err
	}
	if model.ID != modelID {
		return model, nil, apperrors.ErrNotFound
	}
	if err := personalExact(modelCreationDB(tx), "model_id", modelID).Order("id").Limit(201).Find(&bindings).Error; err != nil {
		return model, nil, err
	}
	if len(bindings) > 200 {
		return model, nil, connectionMetadataUnavailable
	}
	for _, b := range bindings {
		if b.ModelID != modelID {
			return model, nil, apperrors.ErrNotFound
		}
	}
	return model, bindings, nil
}
func validRoutingProtocol(protocol string) bool {
	return slices.Contains([]string{"openai_chat", "openai_responses", "anthropic_messages", "gemini_generate_content"}, protocol)
}
func routingFilter(modelID, protocol string, filter ModelCreationFilter, prefix string) (ModelCreationFilter, string, error) {
	if !modelCreationID(modelID, "mdl") || !validRoutingProtocol(protocol) {
		return filter, "", apperrors.ErrBadRequest
	}
	return normalizeModelCreationFilter(filter, prefix)
}
func routingCandidateQuery(tx *gorm.DB, modelID, protocol string) *gorm.DB {
	// A Provider may occur once per protocol. Retained zero-weight bindings count too.
	duplicate := modelCreationDB(tx).Table("model_provider_bindings AS b").Select("bound_connection.provider_id").
		Joins("JOIN provider_models AS bound_model ON ?", database.ExactTextColumns(tx, clause.Column{Table: "bound_model", Name: "id"}, clause.Column{Table: "b", Name: "provider_model_id"})).
		Joins("JOIN provider_connections AS bound_connection ON ?", database.ExactTextColumns(tx, clause.Column{Table: "bound_connection", Name: "id"}, clause.Column{Table: "bound_model", Name: "connection_id"})).
		Where(database.ExactText(tx, clause.Column{Table: "b", Name: "model_id"}, modelID)).Where(database.ExactText(tx, clause.Column{Table: "bound_connection", Name: "protocol"}, protocol))
	return modelCreationDB(tx).Table("provider_models AS pm").
		Joins("JOIN provider_connections AS c ON ?", database.ExactTextColumns(tx, clause.Column{Table: "c", Name: "id"}, clause.Column{Table: "pm", Name: "connection_id"})).
		Joins("JOIN providers AS p ON ?", database.ExactTextColumns(tx, clause.Column{Table: "p", Name: "id"}, clause.Column{Table: "c", Name: "provider_id"})).
		Where(database.ExactText(tx, clause.Column{Table: "c", Name: "protocol"}, protocol)).Where("NOT EXISTS (?)", duplicate.Where(database.ExactTextColumns(tx, clause.Column{Table: "bound_connection", Name: "provider_id"}, clause.Column{Table: "c", Name: "provider_id"})))
}
func (s *Service) ListModelRoutingProviders(ctx context.Context, actor, modelID, protocol string, filter ModelCreationFilter) (*ModelRoutingProviderPage, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	f, pattern, err := routingFilter(modelID, protocol, filter, "prv")
	if err != nil {
		return nil, err
	}
	result := &ModelRoutingProviderPage{Items: []ModelRoutingProvider{}}
	err = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if _, _, err := routingRead(tx, actor, modelID, protocol); err != nil {
			return err
		}
		q := routingCandidateQuery(tx, modelID, protocol).Select("DISTINCT p.id,p.name").Where("LOWER(p.name) LIKE ? ESCAPE '!'", pattern)
		if f.Cursor != "" {
			q = q.Where("p.id > ?", f.Cursor)
		}
		if err := q.Order("p.id").Limit(f.Limit + 1).Scan(&result.Items).Error; err != nil {
			return err
		}
		for _, row := range result.Items {
			if !modelCreationID(row.ID, "prv") {
				return connectionMetadataUnavailable
			}
		}
		result.Items, result.NextCursor = modelCreationPage(result.Items, f.Limit, func(row ModelRoutingProvider) string { return row.ID })
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, catalogError(err)
	}
	return result, nil
}

type routingSupplyState struct {
	Credentials     []modelCreationCredentialProof
	Enabled         bool
	Covered         bool
	SourceAvailable bool
}

// One bounded batch for all visible Connections; no per-row verification or remote reads.
func routingSupplyStates(tx *gorm.DB, connections []entity.ProviderConnection, models []entity.ProviderModel) (map[string]routingSupplyState, error) {
	result := map[string]routingSupplyState{}
	connectionRows := map[string]entity.ProviderConnection{}
	for _, connection := range connections {
		connectionRows[connection.ID] = connection
	}
	ids := []string{}
	pmIDs := []string{}
	for _, c := range connections {
		if !slices.Contains(ids, c.ID) {
			ids = append(ids, c.ID)
		}
	}
	for _, pm := range models {
		pmIDs = append(pmIDs, pm.ID)
	}
	if len(ids) == 0 {
		return result, nil
	}
	var credentials []entity.ProviderCredential
	if err := modelCreationDB(tx).Where(memberModelsExactIDs(tx, "connection_id", ids)).Order("id").Limit(1001).Find(&credentials).Error; err != nil {
		return nil, err
	}
	if len(credentials) > 1000 {
		return nil, connectionMetadataUnavailable
	}
	credentialIDs := []string{}
	for _, c := range credentials {
		credentialIDs = append(credentialIDs, c.ID)
	}
	accesses := []entity.CredentialModelAccess{}
	if len(credentialIDs) > 0 && len(pmIDs) > 0 {
		if err := modelCreationDB(tx).Where(memberModelsExactIDs(tx, "credential_id", credentialIDs)).Where(memberModelsExactIDs(tx, "provider_model_id", pmIDs)).Limit(10001).Find(&accesses).Error; err != nil {
			return nil, err
		}
		if len(accesses) > 10000 {
			return nil, connectionMetadataUnavailable
		}
	}
	if err := attachCredentialSources(tx, credentials); err != nil {
		return nil, err
	}
	accesses, err := loadDeploymentCoverage(tx, credentials, connections, models, accesses, 10000)
	if err != nil {
		return nil, err
	}
	for _, pm := range models {
		state := routingSupplyState{SourceAvailable: true}
		for _, c := range credentials {
			if c.ConnectionID != pm.ConnectionID {
				continue
			}
			var generation string
			if c.VerifiedTransportGeneration != "0" {
				generation = c.VerifiedTransportGeneration
			}
			proof := modelCreationCredentialProof{TransportCurrent: verifiedTransportCurrent(c, connectionRows[c.ConnectionID]), TransportGeneration: generation, ID: c.ID, Revision: credentialRuntimeRevision(c), CipherHash: credentialSourceProof(c), CreatedAt: c.CreatedAt.UTC(), Enabled: c.Enabled, VerificationStatus: c.VerificationStatus, Access: []string{}}
			for _, a := range accesses {
				if a.CredentialID == c.ID && a.ProviderModelID == pm.ID {
					proof.Access = append(proof.Access, a.ProviderModelID)
				}
			}
			if c.Enabled && c.VerificationStatus == "verified" && proof.CipherHash == "" {
				state.SourceAvailable = false
			}
			state.Enabled = state.Enabled || c.Enabled
			state.Credentials = append(state.Credentials, proof)
		}
		state.Covered = capabilityTransportCurrent(pm, connectionRows[pm.ConnectionID]) && modelCreationReady(state.Credentials, pm.ID)
		result[pm.ID] = state
	}
	return result, nil
}
func routingCandidate(model entity.Model, bindings []entity.ModelProviderBinding, pm entity.ProviderModel, c entity.ProviderConnection, p entity.Provider, state routingSupplyState) ModelRoutingCandidate {
	row := ModelRoutingCandidate{ID: pm.ID, ProviderID: p.ID, ConnectionID: c.ID, UpstreamName: pm.UpstreamName, Protocol: c.Protocol,
		ModelRoutingSupply: ModelRoutingSupply{ProviderName: p.Name, ConnectionName: c.Name, VerificationCovered: state.Covered, ConfiguredAvailable: p.Enabled && c.Enabled && !pm.Disabled && capabilityTransportCurrent(pm, c) && state.Enabled}}
	row.Selectable = model.Status == entity.ResourceActive && row.VerificationCovered && row.ConfiguredAvailable && state.SourceAvailable
	// The opaque review binds exact recorded material, births, target topology and configuration.
	raw, _ := json.Marshal(struct {
		Model       entity.Model
		Bindings    []entity.ModelProviderBinding
		PM          entity.ProviderModel
		Connection  entity.ProviderConnection
		Provider    entity.Provider
		Credentials []modelCreationCredentialProof
	}{model, bindings, pm, c, p, state.Credentials})
	proof := runtimeTransportDigestProof(&runtimeData{Connections: []entity.ProviderConnection{c}, ProviderModels: []entity.ProviderModel{pm}})
	if len(proof) > 0 {
		raw, _ = json.Marshal(struct {
			Version string
			Legacy  json.RawMessage
			Proof   map[string]string
		}{"routing.candidate.transport.v1", raw, proof})
	}
	digest := sha256.Sum256(raw)
	row.ReviewETag = hex.EncodeToString(digest[:])
	return row
}
func (s *Service) ListModelRoutingCandidates(ctx context.Context, actor, modelID, protocol, providerID string, filter ModelCreationFilter) (*ModelRoutingCandidatePage, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	f, pattern, err := routingFilter(modelID, protocol, filter, "pmd")
	if err != nil || !modelCreationID(providerID, "prv") {
		return nil, apperrors.ErrBadRequest
	}
	result := &ModelRoutingCandidatePage{Items: []ModelRoutingCandidate{}}
	err = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		model, bindings, err := routingRead(tx, actor, modelID, protocol)
		if err != nil {
			return err
		}
		var provider entity.Provider
		if err := personalExact(modelCreationDB(tx), "id", providerID).Take(&provider).Error; err != nil {
			return err
		}
		if provider.ID != providerID {
			return apperrors.ErrNotFound
		}
		q := routingCandidateQuery(tx, modelID, protocol).Select("pm.*").Where(database.ExactText(tx, clause.Column{Table: "p", Name: "id"}, providerID)).Where("LOWER(pm.upstream_name) LIKE ? ESCAPE '!'", pattern)
		if f.Cursor != "" {
			q = q.Where("pm.id > ?", f.Cursor)
		}
		var pms []entity.ProviderModel
		if err := q.Order("pm.id").Limit(f.Limit + 1).Scan(&pms).Error; err != nil {
			return err
		}
		connectionIDs := []string{}
		for _, pm := range pms {
			if !slices.Contains(connectionIDs, pm.ConnectionID) {
				connectionIDs = append(connectionIDs, pm.ConnectionID)
			}
		}
		connections := []entity.ProviderConnection{}
		if len(connectionIDs) > 0 {
			if err := modelCreationDB(tx).Where(memberModelsExactIDs(tx, "id", connectionIDs)).Order("id").Limit(51).Find(&connections).Error; err != nil {
				return err
			}
		}
		states, err := routingSupplyStates(tx, connections, pms)
		if err != nil {
			return err
		}
		for _, pm := range pms {
			found := false
			for _, c := range connections {
				if c.ID == pm.ConnectionID && c.ProviderID == providerID && c.Protocol == protocol {
					found = true
					result.Items = append(result.Items, routingCandidate(model, bindings, pm, c, provider, states[pm.ID]))
					break
				}
			}
			if !found || !modelCreationID(pm.ID, "pmd") {
				return connectionMetadataUnavailable
			}
		}
		prices, err := routingPrices(tx, actor, pms)
		if err != nil {
			return err
		}
		for i := range result.Items {
			result.Items[i].Prices = prices[result.Items[i].ID]
		}
		result.Items, result.NextCursor = modelCreationPage(result.Items, f.Limit, func(row ModelRoutingCandidate) string { return row.ID })
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, catalogError(err)
	}
	return result, nil
}
func (s *Service) AddReviewedModelBinding(ctx context.Context, actor, modelID, providerModelID, protocol, etag string) (*ModelCatalog, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if !modelCreationID(modelID, "mdl") || !modelCreationID(providerModelID, "pmd") || !validRoutingProtocol(protocol) || !personalModelETag(etag) {
		return nil, apperrors.ErrBadRequest
	}
	bindingID, err := id.NewPrefixed("bnd")
	if err != nil {
		return nil, apperrors.ErrInternal
	}
	err = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		if err := exactCatalogPermission(modelCreationDB(tx), actor, "models.write"); err != nil {
			return err
		}
		if err := personalExact(modelCreationDB(tx), "id", modelID).Clauses(clause.Locking{Strength: "UPDATE"}).Take(&entity.Model{}).Error; err != nil {
			return err
		}
		model, bindings, err := routingRead(tx, actor, modelID, protocol)
		if err != nil {
			return err
		}
		pm, c, err := loadExactPriceSubject(modelCreationDB(tx), providerModelID)
		if err != nil {
			return err
		}
		if err := personalExact(modelCreationDB(tx), "id", c.ID).Clauses(clause.Locking{Strength: "UPDATE"}).Take(&c).Error; err != nil {
			return err
		}
		pm, _, err = loadExactPriceSubject(modelCreationDB(tx), providerModelID)
		if err != nil {
			return err
		}
		if pm.ID != providerModelID || pm.ConnectionID != c.ID || c.Protocol != protocol {
			return catalogConflict
		}
		var provider entity.Provider
		if err := personalExact(modelCreationDB(tx), "id", c.ProviderID).Take(&provider).Error; err != nil {
			return err
		}
		if provider.ID != c.ProviderID {
			return apperrors.ErrNotFound
		}
		var eligible entity.ProviderModel
		if err := routingCandidateQuery(tx, modelID, protocol).Select("pm.*").Where(database.ExactText(tx, clause.Column{Table: "pm", Name: "id"}, pm.ID)).Take(&eligible).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				return catalogConflict
			}
			return err
		}
		if eligible.ID != pm.ID || eligible.ConnectionID != c.ID {
			return catalogConflict
		}
		states, err := routingSupplyStates(tx, []entity.ProviderConnection{c}, []entity.ProviderModel{pm})
		if err != nil {
			return err
		}
		current := routingCandidate(model, bindings, pm, c, provider, states[pm.ID])
		if !current.Selectable || current.ReviewETag != etag {
			return catalogConflict
		}
		if err := modelCreationDB(tx).Create(&entity.ModelProviderBinding{ID: bindingID, ModelID: modelID, ProviderModelID: pm.ID, Weight: 0}).Error; err != nil {
			return err
		}
		if err := stampModelConfiguration(tx, modelID, time.Now()); err != nil {
			return err
		}
		return appendAudit(modelCreationDB(tx), actor, "model.binding.create", "model", modelID)
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err := s.refreshAfterMutation(ctx, catalogError(err)); err != nil {
		return nil, err
	}
	return s.GetAdminModel(ctx, actor, modelID)
}

func enrichRoutingSupplies(tx *gorm.DB, model *ModelCatalog) error {
	ids := []string{}
	for _, b := range model.Bindings {
		ids = append(ids, b.Binding.ProviderModelID)
	}
	if len(ids) > 200 {
		return connectionMetadataUnavailable
	}
	if len(ids) == 0 {
		items := []ModelCatalog{*model}
		projectModelConfiguredReadiness(items, nil, nil, nil, nil)
		model.ConfiguredReady = items[0].ConfiguredReady
		return nil
	}
	var pms []entity.ProviderModel
	if err := modelCreationDB(tx).Where(memberModelsExactIDs(tx, "id", ids)).Limit(201).Find(&pms).Error; err != nil {
		return err
	}
	connectionIDs := []string{}
	for _, pm := range pms {
		if !slices.Contains(connectionIDs, pm.ConnectionID) {
			connectionIDs = append(connectionIDs, pm.ConnectionID)
		}
	}
	var connections []entity.ProviderConnection
	if err := modelCreationDB(tx).Where(memberModelsExactIDs(tx, "id", connectionIDs)).Limit(201).Find(&connections).Error; err != nil {
		return err
	}
	providerIDs := []string{}
	for _, c := range connections {
		if !slices.Contains(providerIDs, c.ProviderID) {
			providerIDs = append(providerIDs, c.ProviderID)
		}
	}
	var providers []entity.Provider
	if err := modelCreationDB(tx).Where(memberModelsExactIDs(tx, "id", providerIDs)).Limit(201).Find(&providers).Error; err != nil {
		return err
	}
	states, err := routingSupplyStates(tx, connections, pms)
	if err != nil {
		return err
	}
	for i, b := range model.Bindings {
		for _, pm := range pms {
			if pm.ID != b.Binding.ProviderModelID {
				continue
			}
			for _, c := range connections {
				if c.ID != pm.ConnectionID {
					continue
				}
				for _, p := range providers {
					if p.ID == c.ProviderID {
						row := routingCandidate(model.Model, nil, pm, c, p, states[pm.ID])
						model.Bindings[i].Supply = &row.ModelRoutingSupply
					}
				}
			}
		}
	}
	for _, b := range model.Bindings {
		if b.Supply == nil {
			return connectionMetadataUnavailable
		}
	}
	items := []ModelCatalog{*model}
	projectModelConfiguredReadiness(items, pms, connections, providers, states)
	model.ConfiguredReady = items[0].ConfiguredReady
	return nil
}

// Prices are optional authorized recorded decimal facts, read in two page-wide batches.
func routingPrices(tx *gorm.DB, actor string, pms []entity.ProviderModel) (map[string]*ModelRoutingPrices, error) {
	result := map[string]*ModelRoutingPrices{}
	user, err := exactEnabledActor(modelCreationDB(tx), actor)
	if err != nil {
		return nil, err
	}
	allowed, err := exactGovernancePermission(modelCreationDB(tx), user, "prices.read")
	if err != nil {
		return nil, err
	}
	if !allowed {
		return result, nil
	}
	ids := []string{}
	for _, pm := range pms {
		ids = append(ids, pm.ID)
		result[pm.ID] = &ModelRoutingPrices{Input: []ModelRoutingPrice{}, Output: []ModelRoutingPrice{}}
	}
	if len(ids) == 0 {
		return result, nil
	}
	prices := []entity.ModelPrice{}
	if err := modelCreationDB(tx).Where(memberModelsExactIDs(tx, "provider_model_id", ids)).Limit(52).Find(&prices).Error; err != nil {
		return nil, err
	}
	if len(prices) > 51 {
		return nil, connectionMetadataUnavailable
	}
	priceIDs := []string{}
	for _, p := range prices {
		priceIDs = append(priceIDs, p.ID)
	}
	if len(priceIDs) == 0 {
		return result, nil
	}
	rates := []entity.PriceRate{}
	if err := modelCreationDB(tx).Where(memberModelsExactIDs(tx, "model_price_id", priceIDs)).Where("tier = ? AND unit = ? AND metric IN ?", "base", "1M_TOKEN", []string{"INPUT_TOKEN", "OUTPUT_TOKEN"}).Order("currency,id").Limit(5001).Find(&rates).Error; err != nil {
		return nil, err
	}
	if len(rates) > 5000 {
		return nil, connectionMetadataUnavailable
	}
	for _, p := range prices {
		if result[p.ProviderModelID] == nil {
			return nil, connectionMetadataUnavailable
		}
		for _, r := range rates {
			if r.ModelPriceID != p.ID {
				continue
			}
			amount, e := pricing.Decimal(r.Amount)
			if e != nil || amount != r.Amount || !pricing.Currency(r.Currency) {
				return nil, connectionMetadataUnavailable
			}
			v := ModelRoutingPrice{Amount: r.Amount, Currency: r.Currency, Enabled: r.Enabled}
			slot := &result[p.ProviderModelID].Input
			if r.Metric == "OUTPUT_TOKEN" {
				slot = &result[p.ProviderModelID].Output
			}
			for _, existing := range *slot {
				if existing.Currency == r.Currency {
					return nil, connectionMetadataUnavailable
				}
			}
			*slot = append(*slot, v)
		}
	}
	return result, nil
}
