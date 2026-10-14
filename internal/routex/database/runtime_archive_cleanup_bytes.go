package database

import "gorm.io/gorm"

type RuntimeArchiveChunkSummary struct {
	ArchiveID      string
	ChunkCount     int64
	EncryptedBytes int64
}

// GORM cannot express the byte-length aggregate of a text envelope through its
// typed query API. OCTET_LENGTH is supported by both PostgreSQL and MySQL; values
// remain parameterized and no encrypted content is returned to the service.
func RuntimeArchiveChunkSummaries(db *gorm.DB, archiveIDs []string) ([]RuntimeArchiveChunkSummary, error) {
	rows := []RuntimeArchiveChunkSummary{}
	err := db.Table("runtime_configuration_archive_chunks").
		Select("archive_id, COUNT(*) AS chunk_count, COALESCE(SUM(OCTET_LENGTH(auth_ciphertext)), 0) AS encrypted_bytes").
		Where("archive_id IN ?", archiveIDs).Group("archive_id").Scan(&rows).Error
	return rows, err
}
