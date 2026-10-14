package entity

import "time"

// RuntimeConfigurationArchive stores private source facts, never runtime authority.
type RuntimeConfigurationArchive struct {
	ID                  string    `gorm:"primaryKey;size:30;check:ck_runtime_archive_identity,OCTET_LENGTH(id) = 30 AND OCTET_LENGTH(content_digest) = 64 AND OCTET_LENGTH(source_digest) = 64 AND OCTET_LENGTH(authorization_digest) = 64 AND OCTET_LENGTH(network_digest) = 64 AND OCTET_LENGTH(pricing_digest) = 64" json:"-"`
	CodecVersion        int       `gorm:"not null;check:ck_runtime_archive_bounds,codec_version = 1 AND plaintext_bytes > 0 AND plaintext_bytes <= 16777216 AND chunk_count > 0 AND chunk_count <= 512 AND plaintext_bytes > (chunk_count - 1) * 32768 AND plaintext_bytes <= chunk_count * 32768" json:"-"`
	ContentDigest       string    `gorm:"size:64;not null" json:"-"`
	SourceDigest        string    `gorm:"size:64;not null" json:"-"`
	AuthorizationDigest string    `gorm:"size:64;not null" json:"-"`
	NetworkDigest       string    `gorm:"size:64;not null" json:"-"`
	PricingDigest       string    `gorm:"size:64;not null" json:"-"`
	PlaintextBytes      int       `gorm:"not null" json:"-"`
	ChunkCount          int       `gorm:"not null" json:"-"`
	CreatedAt           time.Time `gorm:"precision:6;not null;autoCreateTime:false;check:ck_runtime_archive_birth,created_at > '0001-01-01 00:00:00'" json:"-"`
}

func (RuntimeConfigurationArchive) TableName() string { return "runtime_configuration_archives" }

// Only AuthCiphertext may change during root-envelope rewrap.
type RuntimeConfigurationArchiveChunk struct {
	ID               string                       `gorm:"primaryKey;size:34;check:ck_runtime_archive_chunk_identity,OCTET_LENGTH(id) = 34 AND OCTET_LENGTH(archive_id) = 30 AND OCTET_LENGTH(secret_generation) = 64 AND OCTET_LENGTH(plaintext_digest) = 64" json:"-"`
	ArchiveID        string                       `gorm:"size:30;not null;uniqueIndex:idx_runtime_archive_chunk_order,priority:1" json:"-"`
	Ordinal          int                          `gorm:"not null;uniqueIndex:idx_runtime_archive_chunk_order,priority:2;check:ck_runtime_archive_chunk_bounds,ordinal >= 0 AND ordinal < 512 AND plaintext_bytes > 0 AND plaintext_bytes <= 32768 AND OCTET_LENGTH(auth_ciphertext) BETWEEN 1 AND 131072" json:"-"`
	PlaintextBytes   int                          `gorm:"not null" json:"-"`
	PlaintextDigest  string                       `gorm:"size:64;not null" json:"-"`
	SecretGeneration string                       `gorm:"size:64;not null" json:"-"`
	AuthCiphertext   string                       `gorm:"type:text;not null" json:"-"`
	Archive          *RuntimeConfigurationArchive `gorm:"foreignKey:ArchiveID;references:ID;constraint:OnUpdate:RESTRICT,OnDelete:RESTRICT" json:"-"`
}

func (RuntimeConfigurationArchiveChunk) TableName() string {
	return "runtime_configuration_archive_chunks"
}
