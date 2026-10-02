package service

import (
	"context"
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

var credentialDeleteID = regexp.MustCompile(`^crd_[0-7][0-9a-hjkmnp-tv-z]{25}$`)

type CredentialDeleteInput struct {
	Reason string `json:"reason"`
}

type CredentialDeleteRecord struct {
	ID             string `json:"id"`
	Absent         bool   `json:"absent"`
	RuntimeApplied bool   `json:"runtime_applied"`
}

func validCredentialDeleteTarget(credentialID, etag string) bool {
	if !credentialDeleteID.MatchString(credentialID) || len(etag) != 64 {
		return false
	}
	_, err := hex.DecodeString(etag)
	return err == nil
}

func (s *Service) DeleteCredential(ctx context.Context, actorID, credentialID, etag string, input CredentialDeleteInput) (*CredentialDeleteRecord, error) {
	input.Reason = strings.TrimSpace(input.Reason)
	if !validCredentialDeleteTarget(credentialID, etag) || !validCredentialMetadataReason(input.Reason) {
		return nil, apperrors.ErrBadRequest
	}
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		if err := authorizeGovernance(tx, actorID, "providers.write"); err != nil {
			return err
		}
		var row entity.ProviderCredential
		if err := tx.Omit("ciphertext").First(&row, "id = ?", credentialID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				// An authorized retry can establish current absence only. It does
				// not infer a previous deletion or create a historical audit event.
				return nil
			}
			return err
		}
		var connection entity.ProviderConnection
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&connection, "id = ?", row.ConnectionID).Error; err != nil {
			return err
		}
		if err := tx.Omit("ciphertext").Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, "id = ?", credentialID).Error; err != nil {
			return err
		}
		if credentialMetadataRecord(row).ETag != etag {
			return catalogConflict
		}
		// Discovery references restrict deletion in both databases. Removing
		// them first preserves the rest of the model and routing catalogue.
		if err := tx.Where("credential_id = ?", credentialID).Delete(&entity.CredentialModelAccess{}).Error; err != nil {
			return err
		}
		if err := tx.Delete(&entity.ProviderCredential{}, "id = ?", credentialID).Error; err != nil {
			return err
		}
		return appendCredentialDeleteAudit(tx, actorID, row, input.Reason)
	})
	if err != nil {
		return nil, catalogError(err)
	}
	// A failed publication must not leave an old route eligible for new work.
	// Requests already dispatched retain their own immutable attempt state.
	s.InvalidateRuntimeCredential(credentialID)
	if s.runtime == nil {
		return nil, runtimeUnavailable
	}
	if err := s.refreshAfterMutation(ctx, nil); err != nil {
		return nil, err
	}
	return &CredentialDeleteRecord{ID: credentialID, Absent: true, RuntimeApplied: true}, nil
}

type credentialDeleteAuditValues struct {
	ID           string `json:"id"`
	ConnectionID string `json:"connection_id"`
	Name         string `json:"name"`
	Priority     int    `json:"priority"`
}

func appendCredentialDeleteAudit(tx *gorm.DB, actorID string, row entity.ProviderCredential, reason string) error {
	encoded, err := json.Marshal(struct {
		Before credentialDeleteAuditValues `json:"before"`
		After  struct {
			Absent bool `json:"absent"`
		} `json:"after"`
		Reason string `json:"reason"`
	}{
		Before: credentialDeleteAuditValues{row.ID, row.ConnectionID, row.Name, row.Priority},
		After: struct {
			Absent bool `json:"absent"`
		}{true},
		Reason: reason,
	})
	if err != nil {
		return apperrors.ErrInternal
	}
	eventID, err := id.NewPrefixed("aud")
	if err != nil {
		return err
	}
	raw := string(encoded)
	return tx.Create(&entity.AuditEvent{ID: eventID, ActorID: actorID, Action: "credential.delete", ResourceType: "credential", ResourceID: row.ID, DetailsJSON: &raw}).Error
}
