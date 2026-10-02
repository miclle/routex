package service

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/id"
)

var credentialReplacementRequestID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

type CredentialReplacementInput struct {
	RequestID string `json:"request_id"`
	Name      string `json:"name"`
	Secret    string `json:"secret"`
	Reason    string `json:"reason"`
}

type CredentialReplacementRecord struct {
	ID                   string `json:"id"`
	ConnectionID         string `json:"connection_id"`
	ReplacesCredentialID string `json:"replaces_credential_id"`
}

func credentialReplacementHash(actorID, sourceID, etag string, input CredentialReplacementInput) string {
	// Persist only public intent. A secret digest, including an unkeyed hash of
	// a low-entropy secret, must never be stored in the receipt or audit event.
	encoded, _ := json.Marshal(struct {
		ActorID  string `json:"actor_id"`
		SourceID string `json:"source_id"`
		ETag     string `json:"etag"`
		Name     string `json:"name"`
		Reason   string `json:"reason"`
	}{actorID, sourceID, etag, input.Name, input.Reason}) // String fields cannot fail encoding.
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

func sameCredentialReplacementSecret(original, submitted string) bool {
	originalDigest, submittedDigest := sha256.Sum256([]byte(original)), sha256.Sum256([]byte(submitted))
	return subtle.ConstantTimeCompare(originalDigest[:], submittedDigest[:]) == 1
}

func (s *Service) CreateCredentialReplacement(ctx context.Context, actorID, sourceID, etag string, input CredentialReplacementInput) (*CredentialReplacementRecord, bool, error) {
	input.Name, input.Reason = strings.TrimSpace(input.Name), strings.TrimSpace(input.Reason)
	if !validCredentialDeleteTarget(sourceID, etag) || !credentialReplacementRequestID.MatchString(input.RequestID) || !validCatalogLabel(input.Name) || !validCredentialMetadataReason(input.Reason) || input.Secret == "" || len(input.Secret) > 2048 || strings.ContainsAny(input.Secret, "\r\n") {
		return nil, false, apperrors.ErrBadRequest
	}
	requestHash := credentialReplacementHash(actorID, sourceID, etag, input)
	var result *CredentialReplacementRecord
	created := false
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		if err := authorizeGovernance(tx, actorID, "providers.write"); err != nil {
			return err
		}
		var receipt entity.CredentialReplacementReceipt
		err := tx.First(&receipt, "request_id = ?", input.RequestID).Error
		if err == nil {
			if receipt.ActorID != actorID || receipt.SourceCredentialID != sourceID || receipt.RequestHash != requestHash {
				return catalogConflict
			}
			if s.secrets == nil {
				return secretStoreUnavailable
			}
			var replacement entity.ProviderCredential
			if err := tx.Select("id", "connection_id", "replaces_credential_id", "ciphertext").First(&replacement, "id = ?", receipt.ResultCredentialID).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return catalogConflict
				}
				return err
			}
			if replacement.ConnectionID != receipt.ConnectionID || replacement.ReplacesCredentialID == nil || *replacement.ReplacesCredentialID != sourceID {
				return catalogConflict
			}
			plaintext, err := s.secrets.Open(replacement.ID, replacement.Ciphertext)
			if err != nil {
				return secretStoreUnavailable
			}
			if !sameCredentialReplacementSecret(plaintext, input.Secret) {
				return catalogConflict
			}
			result = &CredentialReplacementRecord{ID: replacement.ID, ConnectionID: replacement.ConnectionID, ReplacesCredentialID: sourceID}
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var source entity.ProviderCredential
		if err := tx.Omit("ciphertext").First(&source, "id = ?", sourceID).Error; err != nil {
			return err
		}
		var connection entity.ProviderConnection
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&connection, "id = ?", source.ConnectionID).Error; err != nil {
			return err
		}
		if err := tx.Omit("ciphertext").Clauses(clause.Locking{Strength: "UPDATE"}).First(&source, "id = ?", sourceID).Error; err != nil {
			return err
		}
		if credentialMetadataRecord(source).ETag != etag {
			return catalogConflict
		}
		if err := checkCredentialName(tx, connection.ID, "", input.Name); err != nil {
			return err
		}
		replacement, err := s.prepareCredential(connection.ID, input.Name, input.Secret, source.Priority)
		if err != nil {
			return err
		}
		replacement.ReplacesCredentialID = &sourceID
		if err := tx.Create(&replacement).Error; err != nil {
			return err
		}
		receipt = entity.CredentialReplacementReceipt{RequestID: input.RequestID, ActorID: actorID, SourceCredentialID: sourceID, ConnectionID: connection.ID, ResultCredentialID: replacement.ID, RequestHash: requestHash}
		if err := tx.Create(&receipt).Error; err != nil {
			return err
		}
		if err := appendCredentialReplacementAudit(tx, actorID, sourceID, replacement, input.Reason); err != nil {
			return err
		}
		result = &CredentialReplacementRecord{ID: replacement.ID, ConnectionID: connection.ID, ReplacesCredentialID: sourceID}
		created = true
		return nil
	})
	// Pending, disabled credentials with no discovery coverage cannot participate
	// in authorization or routing. Creation acknowledges persistence only; it
	// never publishes runtime state or advances the independent verify/enable steps.
	return result, created, catalogError(err)
}

func appendCredentialReplacementAudit(tx *gorm.DB, actorID, sourceID string, replacement entity.ProviderCredential, reason string) error {
	encoded, err := json.Marshal(struct {
		SourceID     string `json:"source_id"`
		ConnectionID string `json:"connection_id"`
		Name         string `json:"name"`
		Priority     int    `json:"priority"`
		Reason       string `json:"reason"`
	}{sourceID, replacement.ConnectionID, replacement.Name, replacement.Priority, reason})
	if err != nil {
		return apperrors.ErrInternal
	}
	eventID, err := id.NewPrefixed("aud")
	if err != nil {
		return err
	}
	raw := string(encoded)
	return tx.Create(&entity.AuditEvent{ID: eventID, ActorID: actorID, Action: "credential.replacement.create", ResourceType: "credential", ResourceID: replacement.ID, DetailsJSON: &raw}).Error
}
