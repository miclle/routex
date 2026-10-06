package service

import (
	"slices"
	"strings"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"gorm.io/gorm"
)

func teamCreationModelsDigest(rows []entity.TeamCreationReceiptModel) string {
	type proof struct {
		ID   string
		Born time.Time
	}
	proofs := make([]proof, len(rows))
	for i, row := range rows {
		proofs[i] = proof{row.ModelID, row.ModelCreatedAt.UTC()}
	}
	return personalHash(struct {
		Domain string
		Models []proof
	}{"routex.team-creation.models.snapshot.v1", proofs})
}
func readTeamCreationReceiptModels(tx *gorm.DB, receipt entity.TeamCreationReceipt) ([]entity.TeamCreationReceiptModel, error) {
	if receipt.ModelSnapshotVersion == 0 {
		if receipt.ModelCount != 0 || receipt.ModelDigest != nil {
			return nil, apperrors.ErrInternal
		}
		var unexpected []entity.TeamCreationReceiptModel
		if err := personalExact(tx, "creation_id", receipt.CreationID).Limit(1).Find(&unexpected).Error; err != nil {
			return nil, err
		}
		if len(unexpected) != 0 {
			return nil, apperrors.ErrInternal
		}
		return []entity.TeamCreationReceiptModel{}, nil
	}
	if receipt.ModelSnapshotVersion != 1 || receipt.ModelCount < 1 || receipt.ModelCount > 1000 || receipt.ModelDigest == nil || !teamSessionDigest.MatchString(*receipt.ModelDigest) {
		return nil, apperrors.ErrInternal
	}
	rows := []entity.TeamCreationReceiptModel{}
	if err := personalExact(tx, "creation_id", receipt.CreationID).Order("model_id").Limit(1001).Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) != receipt.ModelCount {
		return nil, apperrors.ErrInternal
	}
	slices.SortFunc(rows, func(a, b entity.TeamCreationReceiptModel) int { return strings.Compare(a.ModelID, b.ModelID) })
	for i, row := range rows {
		if row.CreationID != receipt.CreationID || !validAdminModelTarget(row.ModelID) || row.ModelCreatedAt.IsZero() || i > 0 && rows[i-1].ModelID == row.ModelID {
			return nil, apperrors.ErrInternal
		}
	}
	if teamCreationModelsDigest(rows) != *receipt.ModelDigest {
		return nil, apperrors.ErrInternal
	}
	return rows, nil
}
func teamCreationCurrentModelsMatch(tx *gorm.DB, receipt entity.TeamCreationReceipt, ids []string) (bool, error) {
	original, err := readTeamCreationReceiptModels(tx, receipt)
	if err != nil {
		return false, err
	}
	if len(ids) != len(original) {
		return false, nil
	}
	for i, row := range original {
		if ids[i] != row.ModelID {
			return false, nil
		}
	}
	if len(original) == 0 {
		return true, nil
	}
	var grants []entity.TeamModelGrant
	if err := personalExact(tx, "team_id", receipt.TeamID).Order("model_id").Limit(1001).Find(&grants).Error; err != nil {
		return false, err
	}
	if len(grants) != len(original) {
		return false, nil
	}
	byID := map[string]entity.TeamModelGrant{}
	for _, grant := range grants {
		if grant.TeamID != receipt.TeamID || byID[grant.ModelID].ModelID != "" || grant.SourceRequestID != nil || grant.SourceCreationReceiptID == nil || *grant.SourceCreationReceiptID != receipt.CreationID {
			return false, nil
		}
		byID[grant.ModelID] = grant
	}
	// Current configuration includes disabled Models. Their route availability is
	// independent of the configured original grant, but their birth must match.
	for offset := 0; offset < len(ids); offset += 500 {
		batch := ids[offset:min(offset+500, len(ids))]
		var models []entity.Model
		if err := modelCreationDB(tx).Where("id IN ?", batch).Where(memberModelsExactIDs(tx, "id", batch)).Limit(len(batch) + 1).Find(&models).Error; err != nil {
			return false, err
		}
		if len(models) != len(batch) {
			return false, nil
		}
		births := map[string]time.Time{}
		for _, m := range models {
			if !slices.Contains(batch, m.ID) || !births[m.ID].IsZero() {
				return false, nil
			}
			births[m.ID] = m.CreatedAt
		}
		for i, id := range batch {
			if byID[id].ModelID != id || !births[id].Equal(original[offset+i].ModelCreatedAt) {
				return false, nil
			}
		}
	}
	return true, nil
}
func (s *Service) validateTeamCreationSelectedModels(tx *gorm.DB, review *teamCreationReview, input TeamCreationInput) ([]entity.Model, error) {
	if len(input.ModelIDs) == 0 {
		return nil, nil
	}
	if !review.Public.CanSetModels {
		return nil, apperrors.ErrForbidden
	}
	data, err := readTeamCreationModels(tx, input.ModelIDs, true, false)
	if err != nil {
		return nil, err
	}
	if err = readTeamCreationCiphertexts(tx, data); err != nil {
		return nil, err
	}
	_, proofs, err := s.teamCreationModelProof(data)
	if err != nil {
		return nil, err
	}
	if slices.Contains(proofs, "") || teamCreationModelReviewHash(review, data.Models, proofs) != input.ModelReviewToken {
		return nil, catalogConflict
	}
	return data.Models, nil
}
func persistTeamCreationModels(tx *gorm.DB, receipt *entity.TeamCreationReceipt, models []entity.Model) error {
	if len(models) == 0 {
		return nil
	}
	children := make([]entity.TeamCreationReceiptModel, 0, len(models))
	grants := make([]entity.TeamModelGrant, 0, len(models))
	for _, m := range models {
		source := receipt.CreationID
		children = append(children, entity.TeamCreationReceiptModel{CreationID: receipt.CreationID, ModelID: m.ID, ModelCreatedAt: m.CreatedAt})
		grants = append(grants, entity.TeamModelGrant{TeamID: receipt.TeamID, ModelID: m.ID, SourceCreationReceiptID: &source})
	}
	digest := teamCreationModelsDigest(children)
	receipt.ModelSnapshotVersion = 1
	receipt.ModelCount = len(children)
	receipt.ModelDigest = &digest
	if err := tx.CreateInBatches(&grants, 500).Error; err != nil {
		return err
	}
	return tx.CreateInBatches(&children, 500).Error
}

// Historical empty commits retain their original metadata shape. V66 selected
// commits explicitly identify the normalized snapshot schema; no public audit
// projection exposes these private metadata or arbitrary receipt JSON.
type teamCreationCommitAudit struct {
	CreationID           string   `json:"creation_id"`
	OwnerCount           int      `json:"owner_count"`
	Fields               []string `json:"submitted_fields"`
	Reason               string   `json:"reason"`
	ModelSnapshotVersion int      `json:"model_snapshot_version,omitempty"`
	ModelCount           int      `json:"model_count,omitempty"`
	ModelDigest          *string  `json:"model_digest,omitempty"`
}
