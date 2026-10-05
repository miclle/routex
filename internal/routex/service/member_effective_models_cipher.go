package service

import (
	"slices"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"gorm.io/gorm"
)

// Compare the current stored source with already-authenticated route material.
// This is continuity evidence only; hashes and ciphertext never enter the DTO.
func readMemberEffectiveCipherHashes(tx *gorm.DB, credentials []entity.ProviderCredential) (map[string]string, error) {
	hashes := map[string]string{}
	ids := []string{}
	for _, c := range credentials {
		ids = append(ids, c.ID)
	}
	slices.Sort(ids)
	ids = slices.Compact(ids)
	if len(ids) == 0 {
		return hashes, nil
	}
	var rows []entity.ProviderCredential
	if err := modelCreationDB(tx).Select("id", "ciphertext").Where(memberModelsExactIDs(tx, "id", ids)).Limit(5001).Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) != len(ids) {
		return nil, apperrors.ErrInternal
	}
	for _, row := range rows {
		if !slices.Contains(ids, row.ID) || hashes[row.ID] != "" {
			return nil, apperrors.ErrInternal
		}
		hashes[row.ID] = personalHash(row.Ciphertext)
	}
	return hashes, nil
}
func memberEffectiveModelCipherProof(modelID string, hashes map[string]string, routes *runtimeRoutes) bool {
	if routes == nil {
		return false
	}
	for _, route := range routes.Models[modelID] {
		if route.Route.Weight <= 0 {
			continue
		}
		for _, credential := range route.Credentials {
			if hashes[credential.ID] == "" || credential.CipherHash != hashes[credential.ID] {
				return false
			}
		}
	}
	return true
}
