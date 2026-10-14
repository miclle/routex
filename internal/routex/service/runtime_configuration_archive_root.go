package service

import (
	"context"
	"errors"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (s *Service) rootRuntimeArchiveInventoryPage(ctx context.Context, cursor string) ([]rootInventoryRow, error) {
	result := []rootInventoryRow{}
	var chunks []entity.RuntimeConfigurationArchiveChunk
	query := s.authDB(ctx).Session(&gorm.Session{}).Where("OCTET_LENGTH(auth_ciphertext) BETWEEN ? AND ?", 1, runtimeArchiveEnvelopeBytes)
	column := clause.Column{Name: "id"}
	if cursor != "" {
		query = query.Where(database.ByteAfter(query, column, cursor))
	}
	if err := query.Clauses(clause.OrderBy{Expression: database.ByteOrder(query, column)}).Limit(50).Find(&chunks).Error; err != nil {
		return nil, err
	}
	for _, chunk := range chunks {
		var header entity.RuntimeConfigurationArchive
		if err := personalExact(s.authDB(ctx), "id", chunk.ArchiveID).Take(&header).Error; err != nil {
			return nil, secretStoreUnavailable
		}
		reference, err := runtimeArchiveChunkReference(header, chunk)
		if err != nil {
			return nil, secretStoreUnavailable
		}
		result = append(result, rootInventoryRow{chunk.ID, chunk.SecretGeneration, reference, chunk.AuthCiphertext})
	}
	// A weakened retained schema must not let missing/oversized ciphertext escape
	// inventory. Probe at most one identity without selecting secret-bearing data.
	var invalid []struct{ ID string }
	invalidQuery := s.authDB(ctx).Model(&entity.RuntimeConfigurationArchiveChunk{}).Select("id").Where("auth_ciphertext IS NULL OR OCTET_LENGTH(auth_ciphertext) < ? OR OCTET_LENGTH(auth_ciphertext) > ?", 1, runtimeArchiveEnvelopeBytes)
	if err := invalidQuery.Limit(1).Find(&invalid).Error; err != nil {
		return nil, err
	}
	if len(invalid) != 0 {
		return nil, secretStoreUnavailable
	}
	return result, nil
}

func rootRuntimeArchiveCAS(tx *gorm.DB, row rootInventoryRow, result string) (string, error) {
	ledger, err := lockRuntimeArchiveRetention(tx)
	if err != nil {
		return "", err
	}
	var chunk entity.RuntimeConfigurationArchiveChunk
	err = personalExact(tx, "id", row.id).Take(&chunk).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "deleted", nil
	}
	if err != nil {
		return "", err
	}
	var header entity.RuntimeConfigurationArchive
	if err := personalExact(tx, "id", chunk.ArchiveID).Take(&header).Error; err != nil {
		return "", secretStoreUnavailable
	}
	reference, err := runtimeArchiveChunkReference(header, chunk)
	if err != nil {
		return "", secretStoreUnavailable
	}
	if reference != row.reference || chunk.SecretGeneration != row.generation || chunk.AuthCiphertext != row.ciphertext {
		return "changed", nil
	}
	if len(result) == 0 || len(result) > runtimeArchiveEnvelopeBytes {
		return "", secretStoreUnavailable
	}
	query := personalExact(tx.Model(&entity.RuntimeConfigurationArchiveChunk{}), "id", chunk.ID)
	query = personalExact(query, "secret_generation", chunk.SecretGeneration)
	query = personalExact(query, "auth_ciphertext", chunk.AuthCiphertext)
	update := query.UpdateColumn("auth_ciphertext", result)
	if update.Error != nil {
		return "", update.Error
	}
	if update.RowsAffected != 1 {
		return "changed", nil
	}
	// Root rotation may increase the retained total beyond a cap. It still
	// proceeds; the next new archive remains paused rather than deleting history.
	total, err := runtimeArchiveRetentionRewrapBytes(ledger.EncryptedBytes, int64(len(chunk.AuthCiphertext)), int64(len(result)))
	if err != nil {
		return "", err
	}
	if total != ledger.EncryptedBytes {
		if err = updateRuntimeArchiveRetention(tx, ledger.RetainedCount, total); err != nil {
			return "", err
		}
	}
	return "rewrapped", nil
}
