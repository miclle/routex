package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/id"
)

type CredentialMetadataInput struct {
	Name     string `json:"name"`
	Priority *int   `json:"priority"`
	Reason   string `json:"reason"`
}

type CredentialMetadataRecord struct {
	ID                 string     `json:"id"`
	ConnectionID       string     `json:"connection_id"`
	Name               string     `json:"name"`
	Priority           int        `json:"priority"`
	Enabled            bool       `json:"enabled"`
	VerificationStatus string     `json:"verification_status"`
	VerifiedAt         *time.Time `json:"verified_at"`
	ETag               string     `json:"etag"`
}

func credentialMetadataRecord(row entity.ProviderCredential) *CredentialMetadataRecord {
	result := &CredentialMetadataRecord{ID: row.ID, ConnectionID: row.ConnectionID, Name: row.Name, Priority: row.Priority, Enabled: row.Enabled, VerificationStatus: row.VerificationStatus}
	if row.VerifiedAt != nil {
		verified := row.VerifiedAt.UTC()
		result.VerifiedAt = &verified
	}
	var verifiedAt *string
	if result.VerifiedAt != nil {
		formatted := result.VerifiedAt.Format(time.RFC3339Nano)
		verifiedAt = &formatted
	}
	// This frozen representation contains no secrets. Timestamp normalization
	// makes the validator independent of the driver's time zone representation.
	// Its string/integer/bool fields cannot fail JSON encoding.
	encoded, _ := json.Marshal(struct {
		ID                 string  `json:"id"`
		ConnectionID       string  `json:"connection_id"`
		Name               string  `json:"name"`
		Priority           int     `json:"priority"`
		Enabled            bool    `json:"enabled"`
		VerificationStatus string  `json:"verification_status"`
		VerifiedAt         *string `json:"verified_at"`
	}{result.ID, result.ConnectionID, result.Name, result.Priority, result.Enabled, result.VerificationStatus, verifiedAt})
	digest := sha256.Sum256(encoded)
	result.ETag = hex.EncodeToString(digest[:])
	return result
}

func validCredentialMetadataReason(reason string) bool {
	return reason != "" && len(reason) <= 1024 && utf8.ValidString(reason) && !strings.ContainsFunc(reason, unicode.IsControl)
}

// The connection lock serializes duplicate checks across metadata and creation.
// Go case folding avoids relying on driver-specific collation semantics.
func checkCredentialName(tx *gorm.DB, connectionID, credentialID, name string) error {
	var rows []entity.ProviderCredential
	if err := tx.Select("id", "name").Where("connection_id = ?", connectionID).Find(&rows).Error; err != nil {
		return err
	}
	for _, row := range rows {
		if row.ID != credentialID && strings.EqualFold(strings.TrimSpace(row.Name), name) {
			return catalogConflict
		}
	}
	return nil
}

func (s *Service) GetCredentialMetadata(ctx context.Context, actorID, credentialID string) (*CredentialMetadataRecord, error) {
	db := s.authDB(ctx)
	if err := authorizeGovernance(db, actorID, "providers.read"); err != nil {
		return nil, catalogError(err)
	}
	var row entity.ProviderCredential
	if err := db.Omit("ciphertext").First(&row, "id = ?", credentialID).Error; err != nil {
		return nil, catalogError(err)
	}
	return credentialMetadataRecord(row), nil
}

func (s *Service) WriteCredentialMetadata(ctx context.Context, actorID, credentialID, etag string, input CredentialMetadataInput) (*CredentialMetadataRecord, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.Reason = strings.TrimSpace(input.Reason)
	if etag == "" || !validCatalogLabel(input.Name) || input.Priority == nil || *input.Priority < 0 || *input.Priority > 10000 || !validCredentialMetadataReason(input.Reason) {
		return nil, apperrors.ErrBadRequest
	}
	var result *CredentialMetadataRecord
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		if err := authorizeGovernance(tx, actorID, "providers.write"); err != nil {
			return err
		}
		var row entity.ProviderCredential
		if err := tx.Omit("ciphertext").First(&row, "id = ?", credentialID).Error; err != nil {
			return err
		}
		var connection entity.ProviderConnection
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&connection, "id = ?", row.ConnectionID).Error; err != nil {
			return err
		}
		if err := tx.Omit("ciphertext").Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, "id = ?", credentialID).Error; err != nil {
			return err
		}
		result = credentialMetadataRecord(row)
		// A retry can establish only that the desired metadata is current. It
		// cannot prove who applied it, and must not duplicate audit history.
		if row.Name == input.Name && row.Priority == *input.Priority {
			return nil
		}
		if result.ETag != etag {
			return catalogConflict
		}
		// Historical duplicates remain editable for priority and true no-ops.
		// A new name may never create another ambiguous connection-local label.
		if row.Name != input.Name {
			if err := checkCredentialName(tx, row.ConnectionID, row.ID, input.Name); err != nil {
				return err
			}
		}
		before := credentialMetadataValues{Name: row.Name, Priority: row.Priority}
		if err := tx.Model(&row).Updates(map[string]any{"name": input.Name, "priority": *input.Priority}).Error; err != nil {
			return err
		}
		row.Name, row.Priority = input.Name, *input.Priority
		result = credentialMetadataRecord(row)
		return appendCredentialMetadataAudit(tx, actorID, row.ID, before, credentialMetadataValues{Name: row.Name, Priority: row.Priority}, input.Reason)
	})
	if err := s.refreshAfterMutation(ctx, catalogError(err)); err != nil {
		return nil, err
	}
	return result, nil
}

type credentialMetadataValues struct {
	Name     string `json:"name"`
	Priority int    `json:"priority"`
}

func appendCredentialMetadataAudit(tx *gorm.DB, actorID, credentialID string, before, after credentialMetadataValues, reason string) error {
	encoded, err := json.Marshal(struct {
		Before credentialMetadataValues `json:"before"`
		After  credentialMetadataValues `json:"after"`
		Reason string                   `json:"reason"`
	}{before, after, reason})
	if err != nil {
		return apperrors.ErrInternal
	}
	eventID, err := id.NewPrefixed("aud")
	if err != nil {
		return err
	}
	raw := string(encoded)
	return tx.Create(&entity.AuditEvent{ID: eventID, ActorID: actorID, Action: "credential.metadata.update", ResourceType: "credential", ResourceID: credentialID, DetailsJSON: &raw}).Error
}
