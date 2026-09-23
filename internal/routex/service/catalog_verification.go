package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/id"
)

type CredentialVerification struct {
	Verified         bool
	DiscoveredModels int
	Message          string
}

// VerifyCredential performs real OpenAI model discovery without relaying any
// upstream response bodies or error text to clients or logs.
func (s *Service) VerifyCredential(ctx context.Context, actorID, credentialID string) (*CredentialVerification, error) {
	if s.secrets == nil {
		return nil, secretStoreUnavailable
	}
	db := s.authDB(ctx)
	var credential entity.ProviderCredential
	if err := db.First(&credential, "id = ?", credentialID).Error; err != nil {
		return nil, catalogError(err)
	}
	var connection entity.ProviderConnection
	if err := db.First(&connection, "id = ?", credential.ConnectionID).Error; err != nil {
		return nil, catalogError(err)
	}
	plaintext, err := s.secrets.Open(credential.ID, credential.Ciphertext)
	if err != nil {
		return nil, apperrors.ErrInternal
	}
	names, verified := s.discoverModels(ctx, connection, plaintext)
	result := &CredentialVerification{Verified: verified, Message: "Credential verification failed"}
	if verified {
		result.Message = "Credential verified"
		result.DiscoveredModels = len(names)
	}
	err = db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&connection, "id = ?", connection.ID).Error; err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&credential, "id = ?", credential.ID).Error; err != nil {
			return err
		}
		if err := tx.Where("credential_id = ?", credential.ID).Delete(&entity.CredentialModelAccess{}).Error; err != nil {
			return err
		}
		updates := map[string]any{"verification_status": "failed", "verified_at": nil, "enabled": false}
		if verified {
			updates["verification_status"], updates["verified_at"] = "verified", time.Now().UTC()
			// Reverification never silently re-enables a credential. If coverage
			// shrinks below an active route, disable it until an explicit enable.
			updates["enabled"] = credential.Enabled
			for _, name := range names {
				var model entity.ProviderModel
				err := tx.Where("connection_id = ? AND upstream_name = ?", connection.ID, name).First(&model).Error
				if errors.Is(err, gorm.ErrRecordNotFound) {
					modelID, err := id.NewPrefixed("pmd")
					if err != nil {
						return err
					}
					model = entity.ProviderModel{ID: modelID, ConnectionID: connection.ID, UpstreamName: name}
					if err := tx.Create(&model).Error; err != nil {
						return err
					}
				} else if err != nil {
					return err
				}
				if err := tx.Create(&entity.CredentialModelAccess{CredentialID: credential.ID, ProviderModelID: model.ID}).Error; err != nil {
					return err
				}
			}
			var missing int64
			if err := tx.Table("model_provider_bindings AS b").Joins("JOIN provider_models p ON p.id = b.provider_model_id").Where("p.connection_id = ? AND b.weight > 0 AND NOT EXISTS (SELECT 1 FROM credential_model_accesses a WHERE a.provider_model_id = p.id AND a.credential_id = ?)", connection.ID, credential.ID).Count(&missing).Error; err != nil {
				return err
			}
			if missing != 0 {
				updates["enabled"] = false
			}
		}
		if err := tx.Model(&credential).Updates(updates).Error; err != nil {
			return err
		}
		return appendAudit(tx, actorID, "credential.verify", "credential", credential.ID)
	})
	return result, catalogError(err)
}

func (s *Service) discoverModels(ctx context.Context, connection entity.ProviderConnection, plaintext string) ([]string, bool) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, connection.BaseURL+"/models", nil)
	if err != nil {
		return nil, false
	}
	req.Header.Set("Authorization", "Bearer "+plaintext)
	req.Header.Set("Accept", "application/json")
	resp, err := s.upstream.Do(req)
	if err != nil {
		return nil, false
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, false
	}
	const maxDiscoveryBytes = 2 << 20
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxDiscoveryBytes+1))
	if err != nil || len(body) > maxDiscoveryBytes {
		return nil, false
	}
	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &payload) != nil || payload.Data == nil || len(payload.Data) > 2000 {
		return nil, false
	}
	names := make([]string, 0, len(payload.Data))
	seen := make(map[string]bool, len(payload.Data))
	for _, item := range payload.Data {
		if !validUpstreamName(item.ID) {
			return nil, false
		}
		if !seen[item.ID] {
			names = append(names, item.ID)
			seen[item.ID] = true
		}
	}
	return names, true
}
