package service

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/upstream"
)

// Every independent query starts from a fresh statement while retaining the transaction.
func modelCreationDB(tx *gorm.DB) *gorm.DB { return tx.Session(&gorm.Session{NewDB: true}) }
func modelCreationRead(tx *gorm.DB, actor string) error {
	for _, permission := range []string{"models.read_all", "providers.read"} {
		if err := exactCatalogPermission(modelCreationDB(tx), actor, permission); err != nil {
			return err
		}
	}
	return nil
}
func modelCreationConnection(tx *gorm.DB, connectionID string) (entity.ProviderConnection, entity.Provider, error) {
	var connection entity.ProviderConnection
	var provider entity.Provider
	if err := personalExact(modelCreationDB(tx), "id", connectionID).Take(&connection).Error; err != nil {
		return connection, provider, err
	}
	if !validTransportGeneration(connection.TransportGeneration) {
		return connection, provider, connectionMetadataUnavailable
	}
	if connection.ID != connectionID {
		return connection, provider, apperrors.ErrNotFound
	}
	if err := personalExact(modelCreationDB(tx), "id", connection.ProviderID).Take(&provider).Error; err != nil {
		return connection, provider, err
	}
	return connection, provider, nil
}
func modelCreationConnectionView(c entity.ProviderConnection, p entity.Provider) ModelCreationConnection {
	return ModelCreationConnection{Adapter: entity.ConnectionAdapter(c), APIVersion: c.APIVersion, ID: c.ID, ProviderID: p.ID, ProviderName: p.Name, Name: c.Name, Protocol: c.Protocol, BaseURL: c.BaseURL}
}
func (s *Service) GetModelCreationContext(ctx context.Context, actor, connectionID string) (*ModelCreationContext, error) {
	if !modelCreationID(connectionID, "con") {
		return nil, apperrors.ErrBadRequest
	}
	var result *ModelCreationContext
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := modelCreationRead(tx, actor); err != nil {
			return err
		}
		c, p, err := modelCreationConnection(tx, connectionID)
		if err != nil {
			return err
		}
		user, err := exactEnabledActor(modelCreationDB(tx), actor)
		if err != nil {
			return err
		}
		allowed, err := exactGovernancePermission(modelCreationDB(tx), user, "models.write")
		if err != nil {
			return err
		}
		result = &ModelCreationContext{Connection: modelCreationConnectionView(c, p), CanCreate: allowed, ObservedAt: time.Now().UTC()}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return result, catalogError(err)
}

type modelCreationCredentialProof struct {
	TransportCurrent         bool   `json:"-"`
	TransportGeneration      string `json:",omitempty"`
	ID, Revision, CipherHash string
	CreatedAt                time.Time
	Enabled                  bool
	VerificationStatus       string
	Access                   []string
}
type modelCreationTopology struct {
	Binding        entity.ModelProviderBinding
	ProviderModel  entity.ProviderModel
	Connection     entity.ProviderConnection
	EgressRevision string
}
type modelCreationModelProof struct {
	Model    entity.Model
	Name     entity.ModelName
	Topology []modelCreationTopology
}
type modelCreationState struct {
	TransportProof map[string]string `json:",omitempty"`
	Connection     entity.ProviderConnection
	Provider       entity.Provider
	EgressRevision string
	Credentials    []modelCreationCredentialProof
	ProviderModels []entity.ProviderModel
	Models         []modelCreationModelProof
	ReservedNames  []*entity.ModelName
	Associated     []bool
	TransportReady bool
}

func modelCreationCredentials(tx *gorm.DB, connectionID string, selected []string) ([]modelCreationCredentialProof, error) {
	var credentials []entity.ProviderCredential
	if err := personalExact(modelCreationDB(tx), "connection_id", connectionID).Order("id").Limit(201).Find(&credentials).Error; err != nil {
		return nil, err
	}
	if len(credentials) > 200 {
		return nil, apperrors.ErrBadRequest
	}
	credentialIDs := []string{}
	for _, credential := range credentials {
		credentialIDs = append(credentialIDs, credential.ID)
	}
	var accesses []entity.CredentialModelAccess
	if len(credentialIDs) > 0 && len(selected) > 0 {
		if err := modelCreationDB(tx).Where("credential_id IN ? AND provider_model_id IN ?", credentialIDs, selected).Limit(10201).Find(&accesses).Error; err != nil {
			return nil, err
		}
		if len(accesses) > 10200 {
			return nil, apperrors.ErrBadRequest
		}
	}
	if err := attachCredentialSources(tx, credentials); err != nil {
		return nil, err
	}
	var connection entity.ProviderConnection
	if err := personalExact(modelCreationDB(tx), "id", connectionID).Take(&connection).Error; err != nil {
		return nil, err
	}
	var models []entity.ProviderModel
	if len(selected) > 0 {
		if err := personalExact(modelCreationDB(tx), "connection_id", connectionID).Where(memberModelsExactIDs(tx, "id", selected)).Limit(201).Find(&models).Error; err != nil {
			return nil, err
		}
	}
	projected, err := loadDeploymentCoverage(tx, credentials, []entity.ProviderConnection{connection}, models, accesses, 10200)
	if err != nil {
		return nil, err
	}
	accesses = projected

	result := make([]modelCreationCredentialProof, 0, len(credentials))
	for _, credential := range credentials {
		var generation string
		if credential.VerifiedTransportGeneration != "0" {
			generation = credential.VerifiedTransportGeneration
		}
		proof := modelCreationCredentialProof{TransportCurrent: verifiedTransportCurrent(credential, connection), TransportGeneration: generation, ID: credential.ID, Revision: credentialRuntimeRevision(credential), CipherHash: credentialSourceProof(credential), CreatedAt: credential.CreatedAt.UTC(), Enabled: credential.Enabled, VerificationStatus: credential.VerificationStatus, Access: []string{}}
		for _, access := range accesses {
			if access.CredentialID == credential.ID && slices.Contains(selected, access.ProviderModelID) {
				proof.Access = append(proof.Access, access.ProviderModelID)
			}
		}
		slices.Sort(proof.Access)
		result = append(result, proof)
	}
	return result, nil
}
func modelCreationReady(credentials []modelCreationCredentialProof, pm string) bool {
	count := 0
	for _, c := range credentials {
		if c.Enabled && c.TransportCurrent && c.VerificationStatus == "verified" {
			count++
			if !slices.Contains(c.Access, pm) {
				return false
			}
		}
	}
	return count > 0
}
func (s *Service) modelCreationModel(tx *gorm.DB, id, protocol string) (modelCreationModelProof, error) {
	result := modelCreationModelProof{Topology: []modelCreationTopology{}}
	if err := personalExact(modelCreationDB(tx), "id", id).Take(&result.Model).Error; err != nil {
		return result, err
	}
	if err := personalExact(personalExact(modelCreationDB(tx), "model_id", id), "current_model_id", id).Take(&result.Name).Error; err != nil {
		return result, err
	}
	var bindings []entity.ModelProviderBinding
	if err := personalExact(modelCreationDB(tx), "model_id", id).Order("id").Limit(201).Find(&bindings).Error; err != nil {
		return result, err
	}
	if len(bindings) > 200 {
		return result, apperrors.ErrBadRequest
	}
	for _, binding := range bindings {
		pm, connection, err := loadExactPriceSubject(modelCreationDB(tx), binding.ProviderModelID)
		if err != nil {
			return result, err
		}
		if connection.Protocol != protocol {
			continue
		}
		_, _, revision, err := s.resolveConnectionEgress(modelCreationDB(tx), connection)
		if err != nil {
			return result, err
		}
		result.Topology = append(result.Topology, modelCreationTopology{Binding: binding, ProviderModel: pm, Connection: connection, EgressRevision: revision})
	}
	return result, nil
}
func modelCreationWeight(model modelCreationModelProof, providerID string) (int, []string) {
	blockers := []string{}
	weight := 100
	if model.Model.Status != entity.ResourceActive {
		blockers = append(blockers, "model_inactive")
	}
	if len(model.Topology) > 0 {
		weight = 0
		total := 0
		valid := true
		duplicate := false
		for _, row := range model.Topology {
			total += row.Binding.Weight
			valid = valid && row.Binding.Weight >= 0 && row.Binding.Weight <= 100
			duplicate = duplicate || row.Connection.ProviderID == providerID
		}
		if !valid || total != 100 {
			blockers = append(blockers, "protocol_weights_invalid")
		}
		if duplicate {
			blockers = append(blockers, "provider_protocol_duplicate")
		}
	}
	return weight, blockers
}
func (s *Service) captureModelCreation(tx *gorm.DB, connectionID string, items []ModelCreationItem) (*modelCreationState, *ModelCreationPreview, error) {
	c, p, err := modelCreationConnection(tx, connectionID)
	if err != nil {
		return nil, nil, err
	}
	state := &modelCreationState{Connection: c, Provider: p, ProviderModels: []entity.ProviderModel{}, Models: []modelCreationModelProof{}, ReservedNames: []*entity.ModelName{}, Associated: []bool{}, TransportReady: true}
	if !p.Enabled || !entity.SupportedNativeProtocol(c.Protocol) {
		state.TransportReady = false
	}
	if _, err := upstream.ValidateBaseURL(c.BaseURL, s.allowPrivateUpstream); err != nil {
		state.TransportReady = false
	}
	_, _, state.EgressRevision, err = s.resolveConnectionEgress(modelCreationDB(tx), c)
	if err != nil {
		state.TransportReady = false
	}
	selected := make([]string, 0, len(items))
	for _, item := range items {
		if item.ProviderModelID != "" {
			selected = append(selected, item.ProviderModelID)
		}
	}
	state.Credentials, err = modelCreationCredentials(tx, c.ID, selected)
	if err != nil {
		return nil, nil, err
	}
	result := &ModelCreationPreview{Connection: modelCreationConnectionView(c, p), Items: []ModelCreationReviewedItem{}, ObservedAt: time.Now().UTC(), CanCommit: true}
	topologyRows := 0
	for _, item := range items {
		var pm entity.ProviderModel
		if item.UpstreamName != "" {
			pm, err = modelCreationManualProviderModel(tx, c, item.UpstreamName)
			if err != nil {
				return nil, nil, err
			}
		} else if err := personalExact(personalExact(modelCreationDB(tx), "id", item.ProviderModelID), "connection_id", c.ID).Take(&pm).Error; err != nil {
			return nil, nil, err
		}
		state.ProviderModels = append(state.ProviderModels, pm)
		var associated int64
		if pm.ID != "" {
			if err := personalExact(modelCreationDB(tx).Model(&entity.ModelProviderBinding{}), "provider_model_id", pm.ID).Count(&associated).Error; err != nil {
				return nil, nil, err
			}
		}
		state.Associated = append(state.Associated, associated > 0)
		row := ModelCreationReviewedItem{ProviderModelID: pm.ID, UpstreamName: pm.UpstreamName, Target: item.Target, Name: item.Name, Protocol: c.Protocol, InitialWeight: 100, BlockerCodes: []string{}}
		if pm.Disabled || pm.ID != "" && !capabilityTransportCurrent(pm, c) {
			row.BlockerCodes = append(row.BlockerCodes, "provider_model_disabled")
		}
		if item.UpstreamName != "" {
			row.WarningCodes = []string{"credential_coverage_unproven"}
		} else if !modelCreationReady(state.Credentials, pm.ID) {
			row.BlockerCodes = append(row.BlockerCodes, "credential_coverage_missing")
		}
		if associated > 0 {
			row.BlockerCodes = append(row.BlockerCodes, "provider_model_associated")
		}
		if !state.TransportReady {
			row.BlockerCodes = append(row.BlockerCodes, "connection_unavailable")
		}
		if item.Target == "new" {
			var name entity.ModelName
			err := personalExact(modelCreationDB(tx), "name", item.Name).Take(&name).Error
			if err == nil {
				row.BlockerCodes = append(row.BlockerCodes, "name_reserved")
				state.ReservedNames = append(state.ReservedNames, &name)
			} else if errors.Is(err, gorm.ErrRecordNotFound) {
				state.ReservedNames = append(state.ReservedNames, nil)
			} else {
				return nil, nil, err
			}
		} else {
			model, err := s.modelCreationModel(tx, item.ModelID, c.Protocol)
			if err != nil {
				return nil, nil, err
			}
			state.Models = append(state.Models, model)
			topologyRows += len(model.Topology)
			if topologyRows > 500 {
				return nil, nil, apperrors.ErrBadRequest
			}
			row.ModelID = &model.Model.ID
			row.Name = model.Name.Name
			var blockers []string
			row.InitialWeight, blockers = modelCreationWeight(model, c.ProviderID)
			row.BlockerCodes = append(row.BlockerCodes, blockers...)
		}
		result.CanCommit = result.CanCommit && len(row.BlockerCodes) == 0
		result.Items = append(result.Items, row)
	}
	state.TransportProof = modelCreationTransportProof(state)
	normalizeModelCreationState(state)
	result.ReviewETag = personalHash(struct {
		Items []ModelCreationItem
		State *modelCreationState
	}{items, state})
	return state, result, nil
}
func (s *Service) PreviewModelCreationBatch(ctx context.Context, actor, connectionID string, input ModelCreationPreviewInput) (*ModelCreationPreview, error) {
	if !modelCreationID(connectionID, "con") {
		return nil, apperrors.ErrBadRequest
	}
	items, err := normalizeModelCreationItems(input.Items)
	if err != nil {
		return nil, err
	}
	var result *ModelCreationPreview
	err = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := modelCreationRead(tx, actor); err != nil {
			return err
		}
		_, result, err = s.captureModelCreation(tx, connectionID, items)
		if err == nil {
			result.ReviewETag = modelCreationReviewHash(actor, result.ReviewETag)
		}
		return err
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return result, catalogError(err)
}

func normalizeModelCreationFilter(f ModelCreationFilter, prefix string) (ModelCreationFilter, string, error) {
	if len(f.Query) > 200 {
		return f, "", apperrors.ErrBadRequest
	}
	pattern, err := candidatePattern(f.Query)
	if err != nil {
		return f, "", err
	}
	if f.Limit == 0 {
		f.Limit = 20
	}
	if f.Limit < 1 || f.Limit > 50 || f.Cursor != "" && !modelCreationID(f.Cursor, prefix) {
		return f, "", apperrors.ErrBadRequest
	}
	return f, pattern, nil
}
func modelCreationPage[T any](rows []T, limit int, getID func(T) string) ([]T, *string) {
	if len(rows) <= limit {
		return rows, nil
	}
	cursor := getID(rows[limit-1])
	return rows[:limit], &cursor
}
func (s *Service) ListModelCreationConnections(ctx context.Context, actor string, filter ModelCreationFilter) (*ModelCreationConnectionPage, error) {
	f, pattern, err := normalizeModelCreationFilter(filter, "con")
	if err != nil {
		return nil, err
	}
	result := &ModelCreationConnectionPage{Items: []ModelCreationConnection{}}
	err = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := modelCreationRead(tx, actor); err != nil {
			return err
		}
		query := modelCreationDB(tx).Table("provider_connections AS c").Select("c.id,c.provider_id,p.name AS provider_name,c.name,c.protocol,c.base_url,c.adapter,c.api_version").Joins("JOIN providers AS p ON ?", database.ExactTextColumns(tx, clause.Column{Table: "p", Name: "id"}, clause.Column{Table: "c", Name: "provider_id"})).Where("LOWER(c.name) LIKE ? ESCAPE '!'", pattern)
		if f.Cursor != "" {
			var cursor entity.ProviderConnection
			if err := personalExact(modelCreationDB(tx), "id", f.Cursor).Take(&cursor).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return apperrors.ErrBadRequest
				}
				return err
			}
			query = query.Where("c.id > ?", f.Cursor)
		}
		if err := query.Order("c.id").Limit(f.Limit + 1).Scan(&result.Items).Error; err != nil {
			return err
		}
		result.Items, result.NextCursor = modelCreationPage(result.Items, f.Limit, func(r ModelCreationConnection) string { return r.ID })
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return result, catalogError(err)
}
func (s *Service) ListModelCreationProviderModels(ctx context.Context, actor, connectionID string, filter ModelCreationFilter) (*ModelCreationProviderModelPage, error) {
	if !modelCreationID(connectionID, "con") {
		return nil, apperrors.ErrBadRequest
	}
	f, pattern, err := normalizeModelCreationFilter(filter, "pmd")
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	result := &ModelCreationProviderModelPage{Items: []ModelCreationProviderModel{}}
	err = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := modelCreationRead(tx, actor); err != nil {
			return err
		}
		connection, _, err := modelCreationConnection(tx, connectionID)
		if err != nil {
			return err
		}
		if f.Cursor != "" {
			var cursor entity.ProviderModel
			if err := personalExact(personalExact(modelCreationDB(tx), "id", f.Cursor), "connection_id", connectionID).Take(&cursor).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return apperrors.ErrBadRequest
				}
				return err
			}
		}
		var models []entity.ProviderModel
		query := personalExact(modelCreationDB(tx), "connection_id", connectionID).Where("LOWER(upstream_name) LIKE ? ESCAPE '!'", pattern)
		if f.Cursor != "" {
			query = query.Where("id > ?", f.Cursor)
		}
		if err := query.Order("id").Limit(f.Limit + 1).Find(&models).Error; err != nil {
			return err
		}
		ids := []string{}
		for _, pm := range models {
			ids = append(ids, pm.ID)
		}
		credentials, err := modelCreationCredentials(tx, connectionID, ids)
		if err != nil {
			return err
		}
		var bindings []entity.ModelProviderBinding
		if len(ids) > 0 {
			if err := modelCreationDB(tx).Where("provider_model_id IN ?", ids).Limit(501).Find(&bindings).Error; err != nil {
				return err
			}
			if len(bindings) > 500 {
				return apperrors.ErrBadRequest
			}
		}
		for _, pm := range models {
			row := ModelCreationProviderModel{ID: pm.ID, UpstreamName: pm.UpstreamName, Disabled: pm.Disabled, InputCapabilities: []string{}, CredentialReady: capabilityTransportCurrent(pm, connection) && modelCreationReady(credentials, pm.ID), BlockerCodes: []string{}}
			if pm.SupportsImageInput {
				row.InputCapabilities = append(row.InputCapabilities, "image")
			}
			if pm.SupportsPDFInput {
				row.InputCapabilities = append(row.InputCapabilities, "pdf")
			}
			if pm.Disabled {
				row.BlockerCodes = append(row.BlockerCodes, "provider_model_disabled")
			}
			if !row.CredentialReady {
				row.BlockerCodes = append(row.BlockerCodes, "credential_coverage_missing")
			}
			for _, binding := range bindings {
				if binding.ProviderModelID == pm.ID {
					row.BlockerCodes = append(row.BlockerCodes, "provider_model_associated")
					break
				}
			}
			row.Selectable = len(row.BlockerCodes) == 0
			result.Items = append(result.Items, row)
		}
		result.Items, result.NextCursor = modelCreationPage(result.Items, f.Limit, func(r ModelCreationProviderModel) string { return r.ID })
		if err := applyModelCreationDefaults(tx, connection, result.Items); err != nil {
			return err
		}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return result, catalogError(err)
}
func (s *Service) ListModelCreationTargets(ctx context.Context, actor, connectionID string, filter ModelCreationFilter) (*ModelCreationTargetPage, error) {
	if !modelCreationID(connectionID, "con") {
		return nil, apperrors.ErrBadRequest
	}
	f, pattern, err := normalizeModelCreationFilter(filter, "mdl")
	if err != nil {
		return nil, err
	}
	result := &ModelCreationTargetPage{Items: []ModelCreationTarget{}}
	err = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := modelCreationRead(tx, actor); err != nil {
			return err
		}
		c, _, err := modelCreationConnection(tx, connectionID)
		if err != nil {
			return err
		}
		if f.Cursor != "" {
			var cursor entity.Model
			if err := personalExact(modelCreationDB(tx), "id", f.Cursor).Take(&cursor).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return apperrors.ErrBadRequest
				}
				return err
			}
			if cursor.Status != entity.ResourceActive {
				return apperrors.ErrBadRequest
			}
		}
		var names []entity.ModelName
		query := modelCreationDB(tx).Model(&entity.ModelName{}).Joins("JOIN models AS m ON ?", database.ExactTextColumns(tx, clause.Column{Table: "m", Name: "id"}, clause.Column{Table: "model_names", Name: "current_model_id"})).Where(database.ExactTextColumns(tx, clause.Column{Table: "m", Name: "id"}, clause.Column{Table: "model_names", Name: "model_id"})).Where(database.ExactText(tx, clause.Column{Table: "m", Name: "status"}, entity.ResourceActive)).Select("model_names.*").Where("LOWER(model_names.name) LIKE ? ESCAPE '!'", pattern)
		if f.Cursor != "" {
			query = query.Where("model_id > ?", f.Cursor)
		}
		if err := query.Order("model_id").Limit(f.Limit + 1).Find(&names).Error; err != nil {
			return err
		}
		for _, name := range names {
			if name.CurrentModelID == nil || *name.CurrentModelID != name.ModelID {
				return apperrors.ErrNotFound
			}
			model, err := s.modelCreationModel(tx, name.ModelID, c.Protocol)
			if err != nil {
				return err
			}
			if model.Model.Status != entity.ResourceActive {
				continue
			}
			weight, blockers := modelCreationWeight(model, c.ProviderID)
			result.Items = append(result.Items, ModelCreationTarget{ID: model.Model.ID, Name: name.Name, InitialWeight: weight, Selectable: len(blockers) == 0, BlockerCodes: blockers})
		}
		result.Items, result.NextCursor = modelCreationPage(result.Items, f.Limit, func(r ModelCreationTarget) string { return r.ID })
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return result, catalogError(err)
}

// Sorted locks match Model weight replacement: Models first, Connections second.
func lockModelCreationSubjects(tx *gorm.DB, state *modelCreationState) error {
	modelIDs := []string{}
	connectionIDs := []string{state.Connection.ID}
	for _, model := range state.Models {
		modelIDs = append(modelIDs, model.Model.ID)
		for _, row := range model.Topology {
			connectionIDs = append(connectionIDs, row.Connection.ID)
		}
	}
	slices.Sort(modelIDs)
	slices.Sort(connectionIDs)
	connectionIDs = slices.Compact(connectionIDs)
	for _, id := range modelIDs {
		var model entity.Model
		if err := personalExact(modelCreationDB(tx), "id", id).Clauses(clause.Locking{Strength: "UPDATE"}).Take(&model).Error; err != nil {
			return err
		}
	}
	for _, id := range connectionIDs {
		var connection entity.ProviderConnection
		if err := personalExact(modelCreationDB(tx), "id", id).Clauses(clause.Locking{Strength: "UPDATE"}).Take(&connection).Error; err != nil {
			return err
		}
	}
	for _, pm := range state.ProviderModels {
		if pm.ID == "" {
			continue
		}
		var model entity.ProviderModel
		if err := personalExact(modelCreationDB(tx), "id", pm.ID).Clauses(clause.Locking{Strength: "UPDATE"}).Take(&model).Error; err != nil {
			return err
		}
	}
	return nil
}

func modelCreationReviewHash(actor, contextHash string) string {
	return personalHash(struct{ Actor, Context string }{actor, contextHash})
}

func normalizeModelCreationState(state *modelCreationState) {
	state.Connection.CreatedAt = state.Connection.CreatedAt.UTC()
	state.Provider.CreatedAt = state.Provider.CreatedAt.UTC()
	for i := range state.ProviderModels {
		state.ProviderModels[i].CreatedAt = state.ProviderModels[i].CreatedAt.UTC()
	}
	for i := range state.Models {
		model := &state.Models[i]
		model.Model.CreatedAt = model.Model.CreatedAt.UTC()
		model.Name.CreatedAt = model.Name.CreatedAt.UTC()
		if model.Name.ExpiresAt != nil {
			at := model.Name.ExpiresAt.UTC()
			model.Name.ExpiresAt = &at
		}
		for j := range model.Topology {
			row := &model.Topology[j]
			row.Binding.CreatedAt = row.Binding.CreatedAt.UTC()
			row.ProviderModel.CreatedAt = row.ProviderModel.CreatedAt.UTC()
			row.Connection.CreatedAt = row.Connection.CreatedAt.UTC()
		}
	}
	for _, name := range state.ReservedNames {
		if name != nil {
			name.CreatedAt = name.CreatedAt.UTC()
			if name.ExpiresAt != nil {
				at := name.ExpiresAt.UTC()
				name.ExpiresAt = &at
			}
		}
	}
}

// Manual names declare configuration, never credential coverage. Path-based native
// protocols must accept the exact segment before any catalogue side effect.
func validManualModelName(c entity.ProviderConnection, name string) bool {
	if !validUpstreamName(name) {
		return false
	}
	if entity.ConnectionAdapter(c) == entity.AdapterAzureOpenAIClassic {
		return upstream.ValidAzureDeployment(name)
	}
	if c.Protocol == entity.ProtocolGeminiGenerateContent {
		return geminiModelSegment.MatchString(name)
	}
	return entity.SupportedNativeProtocol(c.Protocol)
}

func modelCreationManualProviderModel(tx *gorm.DB, c entity.ProviderConnection, name string) (entity.ProviderModel, error) {
	if !validManualModelName(c, name) {
		return entity.ProviderModel{}, apperrors.ErrBadRequest
	}
	var collisions []entity.ProviderModel
	if err := personalExact(personalExact(modelCreationDB(tx), "connection_id", c.ID), "upstream_name", name).Limit(2).Find(&collisions).Error; err != nil {
		return entity.ProviderModel{}, err
	}
	if len(collisions) > 0 {
		return entity.ProviderModel{}, catalogConflict
	}
	return entity.ProviderModel{ConnectionID: c.ID, UpstreamName: name}, nil
}
