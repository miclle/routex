package service

import (
	"errors"
	"math"

	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const runtimeArchiveRetainedVersions int64 = 100
const runtimeArchiveRetainedEncryptedBytes int64 = 256 << 20

var errRuntimeArchiveVersionCap = errors.New("archive version retention cap reached")
var errRuntimeArchiveByteCap = errors.New("archive encrypted byte retention cap reached")

func lockRuntimeArchiveRetention(tx *gorm.DB) (entity.RuntimeArchiveRetention, error) {
	var row entity.RuntimeArchiveRetention
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", 1).Take(&row).Error
	if err != nil {
		return row, err
	}
	if row.ID != 1 || !row.Initialized || row.RetainedCount < 0 || row.EncryptedBytes < 0 {
		return row, errRuntimeArchive
	}
	return row, nil
}
func runtimeArchiveRetentionAdmission(row entity.RuntimeArchiveRetention, additional int64) error {
	if row.ID != 1 || !row.Initialized || row.RetainedCount < 0 || row.EncryptedBytes < 0 || additional < 1 {
		return errRuntimeArchive
	}
	if row.RetainedCount >= runtimeArchiveRetainedVersions {
		return errRuntimeArchiveVersionCap
	}
	if row.EncryptedBytes >= runtimeArchiveRetainedEncryptedBytes || additional > runtimeArchiveRetainedEncryptedBytes-row.EncryptedBytes {
		return errRuntimeArchiveByteCap
	}
	return nil
}
func updateRuntimeArchiveRetention(tx *gorm.DB, count, encryptedBytes int64) error {
	if count < 0 || encryptedBytes < 0 {
		return errRuntimeArchive
	}
	r := tx.Model(&entity.RuntimeArchiveRetention{}).Where("id = ? AND initialized = ?", 1, true).Updates(map[string]any{"retained_count": count, "encrypted_bytes": encryptedBytes})
	if r.Error != nil {
		return r.Error
	}
	if r.RowsAffected != 1 {
		return errRuntimeArchive
	}
	return nil
}
func runtimeArchiveRetentionRewrapBytes(previous, oldBytes, newBytes int64) (int64, error) {
	if previous < 0 || oldBytes < 1 || newBytes < 1 || previous < oldBytes || newBytes > math.MaxInt64-(previous-oldBytes) {
		return 0, errRuntimeArchive
	}
	return previous - oldBytes + newBytes, nil
}
