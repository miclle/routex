package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"gorm.io/gorm"
)

// ModelWeightEditReview pairs the real complete Model with its current write
// proof. A subsequent matching GET never proves an earlier write succeeded.
type ModelWeightEditReview struct {
	Model          *ModelCatalog
	ReviewETag     string
	RuntimeApplied bool
}

type ModelWeightEditInput struct{ Weights []ModelWeight }

func (input *ModelWeightEditInput) UnmarshalJSON(raw []byte) error {
	fields, err := modelCreationObject(raw, "weights")
	if err != nil || len(fields) != 1 {
		return apperrors.ErrBadRequest
	}
	var rows []json.RawMessage
	if json.Unmarshal(fields["weights"], &rows) != nil || len(rows) == 0 || len(rows) > 200 {
		return apperrors.ErrBadRequest
	}
	weights := make([]ModelWeight, 0, len(rows))
	for _, rawRow := range rows {
		row, err := modelCreationObject(rawRow, "binding_id", "weight")
		if err != nil || len(row) != 2 {
			return apperrors.ErrBadRequest
		}
		var weight ModelWeight
		if json.Unmarshal(row["binding_id"], &weight.BindingID) != nil || json.Unmarshal(row["weight"], &weight.Weight) != nil || !modelWeightRetainedID(weight.BindingID, "bnd") || weight.Weight < 0 || weight.Weight > 100 {
			return apperrors.ErrBadRequest
		}
		weights = append(weights, weight)
	}
	input.Weights = weights
	return nil
}

func modelWeightEditETag(actor entity.User, write bool, st *modelWeightState) string {
	return memberModelsDigest(struct {
		ActorID       string
		ActorBirth    time.Time
		Write         bool
		Model         entity.Model
		Rows          []ModelWeightRow
		Eligibility   string
		SourceDenials string
	}{actor.ID, actor.CreatedAt.UTC(), write, st.Model, st.Rows, st.currentEligibilityHash(), st.SourceDenialProof})
}

func (s *Service) ReviewModelWeightEdit(ctx context.Context, actorID, modelID string) (*ModelWeightEditReview, error) {
	if !validAdminModelTarget(modelID) {
		return nil, apperrors.ErrBadRequest
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var result *ModelWeightEditReview
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		actor, write, err := modelWeightAuthority(tx, actorID, true, false)
		if err != nil {
			return err
		}
		if err := exactCatalogPermission(tx, actorID, "providers.read"); err != nil {
			return err
		}
		st, err := loadModelWeightState(tx, modelID, false, true)
		if err != nil {
			return err
		}
		if len(st.Rows) > 200 {
			return modelWeightUnavailable
		}
		model, err := loadExactAdminModelCatalog(tx, modelID)
		if err != nil {
			return err
		}
		if err := enrichRoutingSupplies(tx, model); err != nil {
			return err
		}
		result = &ModelWeightEditReview{Model: model, ReviewETag: modelWeightEditETag(actor, write, st), RuntimeApplied: s.modelWeightRuntimeApplied(st)}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return result, catalogError(err)
}

func (s *Service) SetReviewedModelWeights(ctx context.Context, actorID, modelID, reviewETag string, weights []ModelWeight) (*ModelWeightEditReview, error) {
	if !validAdminModelTarget(modelID) || !validMemberRoleDigest(reviewETag) || len(weights) > 200 {
		return nil, apperrors.ErrBadRequest
	}
	if _, err := s.setModelWeights(ctx, actorID, modelID, weights, reviewETag); err != nil {
		return nil, err
	}
	return s.ReviewModelWeightEdit(ctx, actorID, modelID)
}
