package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/id"
)

// Complete current-state hash binds every captured identity/revision. Compact topology
// avoids repeating full rows in the portable MySQL TEXT receipt (maximum64KiB).
type modelCreationInitialTopology struct {
	ModelID         string
	BindingID       string
	ProviderModelID string
	ConnectionID    string
	Protocol        string
	Weight          int
}
type modelCreationSnapshot struct {
	Items        []ModelCreationReceiptItem
	ConnectionID string
	StateHash    string
	Topology     []modelCreationInitialTopology
}
type modelCreationBatchAudit struct {
	RequestID    string                     `json:"request_id"`
	ConnectionID string                     `json:"connection_id"`
	Reason       string                     `json:"reason"`
	Items        []ModelCreationReceiptItem `json:"items"`
}

func modelCreationIntentHash(actor, connection, etag string, input ModelCreationBatchInput) string {
	return personalHash(struct {
		Actor, Connection, ETag string
		Input                   ModelCreationBatchInput
	}{actor, connection, etag, input})
}
func readModelCreationSnapshot(receipt entity.ModelCreationBatchReceipt) (*modelCreationSnapshot, error) {
	var snapshot modelCreationSnapshot
	if len(receipt.SnapshotJSON) > 60*1024 || json.Unmarshal([]byte(receipt.SnapshotJSON), &snapshot) != nil || len(snapshot.Items) < 1 || len(snapshot.Items) > 50 || snapshot.ConnectionID != receipt.ConnectionID || !personalModelETag(snapshot.StateHash) {
		return nil, apperrors.ErrInternal
	}
	for _, item := range snapshot.Items {
		if !modelCreationID(item.ProviderModelID, "pmd") || !validAdminModelTarget(item.ModelID) || !modelCreationID(item.BindingID, "bnd") || !publicModelName.MatchString(item.Name) || !entity.SupportedNativeProtocol(item.Protocol) || item.Weight != 0 && item.Weight != 100 {
			return nil, apperrors.ErrInternal
		}
	}
	if len(snapshot.Topology) < 1 || len(snapshot.Topology) > 500 {
		return nil, apperrors.ErrInternal
	}
	bindingIDs := map[string]bool{}
	totals := map[string]int{}
	models := map[string]string{}
	for _, item := range snapshot.Items {
		models[item.ModelID] = item.Protocol
	}
	for _, row := range snapshot.Topology {
		if models[row.ModelID] != row.Protocol || !modelCreationID(row.BindingID, "bnd") || !modelCreationID(row.ProviderModelID, "pmd") || !modelCreationID(row.ConnectionID, "con") || bindingIDs[row.BindingID] || row.Weight < 0 || row.Weight > 100 {
			return nil, apperrors.ErrInternal
		}
		bindingIDs[row.BindingID] = true
		totals[row.ModelID] += row.Weight
	}
	for _, item := range snapshot.Items {
		if !bindingIDs[item.BindingID] || totals[item.ModelID] != 100 {
			return nil, apperrors.ErrInternal
		}
	}
	return &snapshot, nil
}
func (s *Service) currentModelCreationState(tx *gorm.DB, snapshot *modelCreationSnapshot) (*modelCreationState, []ModelCreationReceiptItem, error) {
	c, p, err := modelCreationConnection(tx, snapshot.ConnectionID)
	if err != nil {
		return nil, nil, err
	}
	state := &modelCreationState{Connection: c, Provider: p, ProviderModels: []entity.ProviderModel{}, Models: []modelCreationModelProof{}, ReservedNames: []*entity.ModelName{}, Associated: []bool{}, TransportReady: true}
	_, _, state.EgressRevision, err = s.resolveConnectionEgress(modelCreationDB(tx), c)
	if err != nil {
		return nil, nil, err
	}
	ids := []string{}
	for _, item := range snapshot.Items {
		ids = append(ids, item.ProviderModelID)
	}
	state.Credentials, err = modelCreationCredentials(tx, c.ID, ids)
	if err != nil {
		return nil, nil, err
	}
	current := make([]ModelCreationReceiptItem, 0, len(snapshot.Items))
	for _, item := range snapshot.Items {
		var pm entity.ProviderModel
		if err := personalExact(personalExact(modelCreationDB(tx), "id", item.ProviderModelID), "connection_id", c.ID).Take(&pm).Error; err != nil {
			return nil, nil, err
		}
		state.ProviderModels = append(state.ProviderModels, pm)
		model, err := s.modelCreationModel(tx, item.ModelID, item.Protocol)
		if err != nil {
			return nil, nil, err
		}
		state.Models = append(state.Models, model)
		var binding entity.ModelProviderBinding
		if err := personalExact(personalExact(personalExact(modelCreationDB(tx), "id", item.BindingID), "model_id", item.ModelID), "provider_model_id", item.ProviderModelID).Take(&binding).Error; err != nil {
			return nil, nil, err
		}
		if binding.Weight < 0 || binding.Weight > 100 {
			return nil, nil, apperrors.ErrInternal
		}
		row := item
		row.Name = model.Name.Name
		row.Weight = binding.Weight
		current = append(current, row)
	}
	normalizeModelCreationState(state)
	return state, current, nil
}
func (s *Service) modelCreationResult(tx *gorm.DB, actor string, receipt entity.ModelCreationBatchReceipt, created bool) (*ModelCreationBatchResult, error) {
	snapshot, err := readModelCreationSnapshot(receipt)
	if err != nil {
		return nil, err
	}
	result := &ModelCreationBatchResult{Receipt: ModelCreationReceipt{RequestID: receipt.RequestID, ConnectionID: receipt.ConnectionID, CreatedAt: receipt.CreatedAt.UTC(), Items: snapshot.Items}, Committed: true, Changed: created, Created: created, ApplicationStatus: "unavailable"}
	if err := modelCreationRead(tx, actor); err != nil {
		if errors.Is(err, apperrors.ErrForbidden) {
			return result, nil
		}
		return nil, err
	}
	current, items, err := s.currentModelCreationState(tx, snapshot)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) || errors.Is(err, runtimeUnavailable) || errors.Is(err, apperrors.ErrBadRequest) || errors.Is(err, apperrors.ErrInternal) {
			return result, nil
		}
		return nil, err
	}
	result.CurrentItems = items
	if personalHash(current) != snapshot.StateHash {
		result.ApplicationStatus = "superseded"
		return result, nil
	}
	result.RuntimeApplied = s.modelCreationRuntimeApplied(current)
	result.ApplicationStatus = "pending"
	if result.RuntimeApplied {
		result.ApplicationStatus = "applied"
	}
	return result, nil
}
func (s *Service) GetModelCreationReceipt(ctx context.Context, actor, requestID string) (*ModelCreationBatchResult, error) {
	if !credentialReplacementRequestID.MatchString(requestID) {
		return nil, apperrors.ErrBadRequest
	}
	var result *ModelCreationBatchResult
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := modelCreationRead(tx, actor); err != nil {
			return err
		}
		var receipt entity.ModelCreationBatchReceipt
		if err := personalExact(personalExact(modelCreationDB(tx), "request_id", requestID), "actor_id", actor).Take(&receipt).Error; err != nil {
			return err
		}
		var err error
		result, err = s.modelCreationResult(tx, actor, receipt, false)
		return err
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return result, catalogError(err)
}
func (s *Service) CreateModelBatch(ctx context.Context, actor, connectionID, etag string, input ModelCreationBatchInput) (*ModelCreationBatchResult, error) {
	if !modelCreationID(connectionID, "con") || !personalModelETag(etag) {
		return nil, apperrors.ErrBadRequest
	}
	input, err := normalizeModelCreationBatch(input)
	if err != nil {
		return nil, err
	}
	hash := modelCreationIntentHash(actor, connectionID, etag, input)
	var receipt entity.ModelCreationBatchReceipt
	created := false
	err = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(modelCreationDB(tx)); err != nil {
			return err
		}
		if err := exactCatalogPermission(modelCreationDB(tx), actor, "models.write"); err != nil {
			return err
		}
		err := personalExact(modelCreationDB(tx), "request_id", input.RequestID).Take(&receipt).Error
		if err == nil {
			if receipt.ActorID != actor || receipt.ConnectionID != connectionID || receipt.RequestHash != hash || receipt.ReviewETag != etag {
				return catalogConflict
			}
			_, err = readModelCreationSnapshot(receipt)
			return err
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		initial, _, err := s.captureModelCreation(tx, connectionID, input.Items)
		if err != nil {
			return err
		}
		if err := lockModelCreationSubjects(tx, initial); err != nil {
			return err
		}
		_, preview, err := s.captureModelCreation(tx, connectionID, input.Items)
		if err != nil {
			return err
		}
		if modelCreationReviewHash(actor, preview.ReviewETag) != etag || !preview.CanCommit {
			return catalogConflict
		}
		items := make([]ModelCreationReceiptItem, 0, len(input.Items))
		for index, item := range input.Items {
			modelID := item.ModelID
			if item.Target == "new" {
				modelID, err = id.NewPrefixed("mdl")
				if err != nil {
					return err
				}
				model := entity.Model{ID: modelID, Status: entity.ResourceActive}
				if err := modelCreationDB(tx).Create(&model).Error; err != nil {
					return err
				}
				if err := modelCreationDB(tx).Create(&entity.ModelName{Name: item.Name, ModelID: modelID, CurrentModelID: &modelID}).Error; err != nil {
					return err
				}
			}
			bindingID, err := id.NewPrefixed("bnd")
			if err != nil {
				return err
			}
			weight := preview.Items[index].InitialWeight
			if err := modelCreationDB(tx).Create(&entity.ModelProviderBinding{ID: bindingID, ModelID: modelID, ProviderModelID: item.ProviderModelID, Weight: weight}).Error; err != nil {
				return err
			}
			items = append(items, ModelCreationReceiptItem{ProviderModelID: item.ProviderModelID, ModelID: modelID, BindingID: bindingID, CreatedModel: item.Target == "new", Name: preview.Items[index].Name, Protocol: preview.Items[index].Protocol, Weight: weight})
		}
		snapshot := &modelCreationSnapshot{Items: items, ConnectionID: connectionID}
		current, _, err := s.currentModelCreationState(tx, snapshot)
		if err != nil {
			return err
		}
		snapshotJSON, err := encodeModelCreationSnapshot(connectionID, items, current)
		if err != nil {
			return err
		}
		receipt = entity.ModelCreationBatchReceipt{RequestID: input.RequestID, ActorID: actor, ConnectionID: connectionID, RequestHash: hash, ReviewETag: etag, SnapshotJSON: snapshotJSON, CreatedAt: time.Now().UTC()}
		if err := modelCreationDB(tx).Create(&receipt).Error; err != nil {
			return err
		}
		if err := personalExact(modelCreationDB(tx), "request_id", input.RequestID).Take(&receipt).Error; err != nil {
			return err
		}
		details, err := json.Marshal(modelCreationBatchAudit{RequestID: input.RequestID, ConnectionID: connectionID, Reason: input.Reason, Items: items})
		if err != nil {
			return err
		}
		auditID, err := id.NewPrefixed("aud")
		if err != nil {
			return err
		}
		detailJSON := string(details)
		if err := modelCreationDB(tx).Create(&entity.AuditEvent{ID: auditID, ActorID: actor, Action: "model.batch_create", ResourceType: "connection", ResourceID: connectionID, DetailsJSON: &detailJSON}).Error; err != nil {
			return err
		}
		created = true
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, catalogError(err)
	}
	// Publication retries reconcile the current catalogue; they never replay the receipt.
	if err := s.refreshAfterMutation(ctx, nil); err != nil {
		return nil, err
	}
	var result *ModelCreationBatchResult
	err = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := exactCatalogPermission(modelCreationDB(tx), actor, "models.write"); err != nil {
			return err
		}
		var err error
		result, err = s.modelCreationResult(tx, actor, receipt, created)
		return err
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return result, catalogError(err)
}
func modelCreationBatchAuditProjection(row entity.AuditEvent) (any, bool) {
	var detail modelCreationBatchAudit
	if row.DetailsJSON == nil || json.Unmarshal([]byte(*row.DetailsJSON), &detail) != nil || row.ResourceType != "connection" || row.ResourceID != detail.ConnectionID || !modelCreationID(detail.ConnectionID, "con") || !credentialReplacementRequestID.MatchString(detail.RequestID) || !validCredentialMetadataReason(detail.Reason) || len(detail.Items) < 1 || len(detail.Items) > 50 {
		return nil, false
	}
	for _, item := range detail.Items {
		if !modelCreationID(item.ProviderModelID, "pmd") || !validAdminModelTarget(item.ModelID) || !modelCreationID(item.BindingID, "bnd") || !publicModelName.MatchString(item.Name) || !entity.SupportedNativeProtocol(item.Protocol) || item.Weight != 0 && item.Weight != 100 {
			return nil, false
		}
	}
	return detail, true
}

func encodeModelCreationSnapshot(connectionID string, items []ModelCreationReceiptItem, current *modelCreationState) (string, error) {
	snapshot := modelCreationSnapshot{Items: items, ConnectionID: connectionID, StateHash: personalHash(current), Topology: []modelCreationInitialTopology{}}
	for _, model := range current.Models {
		for _, row := range model.Topology {
			snapshot.Topology = append(snapshot.Topology, modelCreationInitialTopology{ModelID: model.Model.ID, BindingID: row.Binding.ID, ProviderModelID: row.ProviderModel.ID, ConnectionID: row.Connection.ID, Protocol: row.Connection.Protocol, Weight: row.Binding.Weight})
		}
	}
	if len(snapshot.Topology) > 500 {
		return "", apperrors.ErrBadRequest
	}
	raw, err := json.Marshal(snapshot)
	if err != nil || len(raw) > 60*1024 {
		return "", apperrors.ErrBadRequest
	}
	return string(raw), nil
}
