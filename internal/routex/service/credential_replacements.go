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

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/id"
)

var credentialReplacementRequestID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

type CredentialReplacementInput struct {
	RequestID         string `json:"request_id"`
	StoragePolicyETag string `json:"storage_policy_etag,omitempty"`
	Name              string `json:"name"`
	Secret            string `json:"secret"`
	Reason            string `json:"reason"`
}

func (v *CredentialReplacementInput) UnmarshalJSON(raw []byte) error {
	fields := []string{"request_id", "name", "secret", "reason"}
	f, err := vaultStrict(raw, fields)
	if err != nil {
		f, err = vaultStrict(raw, append(fields, "storage_policy_etag"))
	}
	if err != nil {
		return err
	}
	var n CredentialReplacementInput
	for key, target := range map[string]*string{"request_id": &n.RequestID, "name": &n.Name, "secret": &n.Secret, "reason": &n.Reason} {
		if string(f[key]) == "null" || json.Unmarshal(f[key], target) != nil {
			return apperrors.ErrBadRequest
		}
	}
	if review, present := f["storage_policy_etag"]; present {
		if json.Unmarshal(review, &n.StoragePolicyETag) != nil || !personalModelETag(n.StoragePolicyETag) {
			return apperrors.ErrBadRequest
		}
	}
	*v = n
	return nil
}

type CredentialReplacementRecord struct {
	StorageSource        string `json:"storage_source"`
	ID                   string `json:"id"`
	ConnectionID         string `json:"connection_id"`
	ReplacesCredentialID string `json:"replaces_credential_id"`
}

func credentialReplacementHash(actorID, sourceID, etag string, input CredentialReplacementInput) string {
	// Persist only public intent. A secret digest, including an unkeyed hash of
	// a low-entropy secret, must never be stored in the receipt or audit event.
	encoded, _ := json.Marshal(struct {
		ActorID    string `json:"actor_id"`
		SourceID   string `json:"source_id"`
		ETag       string `json:"etag"`
		Name       string `json:"name"`
		Reason     string `json:"reason"`
		PolicyETag string `json:"storage_policy_etag,omitempty"`
	}{actorID, sourceID, etag, input.Name, input.Reason, input.StoragePolicyETag}) // String fields cannot fail encoding.
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
	// Historical inline receipts precede the future-write policy. They retain
	// their original source and never require a newly selected Vault context.
	if result, handled, err := s.replayInlineReplacement(ctx, actorID, sourceID, etag, input); handled || err != nil {
		return result, false, err
	}
	vaultMode, e := s.useVaultCreation(ctx, actorID, input.RequestID, input.StoragePolicyETag)
	if e != nil {
		return nil, false, e
	}
	intent := credentialCreationIntent{Kind: "replacement", Target: sourceID, CredentialName: input.Name, SourceETag: etag, Reason: input.Reason, PolicyETag: input.StoragePolicyETag}
	var op *credentialCreationResult
	if vaultMode {
		op, e = s.createVaultCredential(ctx, actorID, input.RequestID, input.Secret, intent)
	} else {
		op, e = s.createInlineCredential(ctx, actorID, input.RequestID, input.Secret, intent)
	}
	if e != nil {
		return nil, false, e
	}
	return &CredentialReplacementRecord{StorageSource: op.StorageSource, ID: op.CredentialID, ConnectionID: op.ConnectionID, ReplacesCredentialID: sourceID}, op.created, nil
}

func (s *Service) replayInlineReplacement(ctx context.Context, actorID, sourceID, etag string, input CredentialReplacementInput) (*CredentialReplacementRecord, bool, error) {
	var result *CredentialReplacementRecord
	handled := false
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := exactEnabledActor(tx, actorID); err != nil {
			return err
		}
		if err := exactCatalogPermission(tx, actorID, "providers.write"); err != nil {
			return err
		}
		var receipt entity.CredentialReplacementReceipt
		err := personalExact(vaultDB(tx), "request_id", input.RequestID).Take(&receipt).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		var replacement entity.ProviderCredential
		if err = personalExact(vaultDB(tx), "id", receipt.ResultCredentialID).Take(&replacement).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			// A retained receipt whose exact result was deleted cannot create a new result.
			return catalogConflict
		} else if err != nil {
			return err
		}
		if replacement.StorageSource == "vault" {
			return nil
		}
		var operation entity.CredentialStorageOperation
		if err = personalExact(vaultDB(tx), "request_id", input.RequestID).Take(&operation).Error; err == nil {
			return nil
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		handled = true
		if receipt.ActorID != actorID || receipt.SourceCredentialID != sourceID || receipt.RequestID != input.RequestID || receipt.RequestHash != credentialReplacementHash(actorID, sourceID, etag, input) || replacement.ID != receipt.ResultCredentialID || replacement.ConnectionID != receipt.ConnectionID || replacement.ReplacesCredentialID == nil || *replacement.ReplacesCredentialID != sourceID {
			return catalogConflict
		}
		plaintext, err := s.openSecret(replacement.ID, replacement.Ciphertext)
		if err != nil {
			return secretStoreUnavailable
		}
		if !sameCredentialReplacementSecret(plaintext, input.Secret) {
			return catalogConflict
		}
		result = &CredentialReplacementRecord{StorageSource: "inline", ID: replacement.ID, ConnectionID: replacement.ConnectionID, ReplacesCredentialID: sourceID}
		return nil
	})
	return result, handled, catalogError(err)
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
