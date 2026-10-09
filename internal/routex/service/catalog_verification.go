package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
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

// VerifyCredential performs real protocol-specific model discovery without relaying any
// upstream response bodies or error text to clients or logs.
func (s *Service) VerifyCredential(ctx context.Context, actorID, credentialID string) (*CredentialVerification, error) {
	if s.secrets == nil {
		return nil, secretStoreUnavailable
	}
	db := s.authDB(ctx)
	capturedActor, e := exactEnabledActor(modelCreationDB(db), actorID)
	if e != nil {
		return nil, e
	}
	var credential entity.ProviderCredential
	if err := exactCatalogPermission(db, actorID, "providers.write"); err != nil {
		return nil, err
	}
	if err := personalExact(modelCreationDB(db), "id", credentialID).Take(&credential).Error; err != nil {
		return nil, catalogError(err)
	}
	var connection entity.ProviderConnection
	if err := personalExact(modelCreationDB(db), "id", credential.ConnectionID).Take(&connection).Error; err != nil {
		return nil, catalogError(err)
	}
	if !validTransportGeneration(connection.TransportGeneration) {
		return nil, connectionMetadataUnavailable
	}
	_, _, transportRevision, err := s.resolveConnectionEgress(db, connection)
	if err != nil {
		return nil, err
	}
	rows := []entity.ProviderCredential{credential}
	if err := attachCredentialSources(db, rows); err != nil {
		return nil, catalogError(err)
	}
	credential = rows[0]
	capturedCredential := credential
	capturedConnection := connection
	sourceProof := credentialSourceProof(credential)
	holder, holderErr := s.acquireCredentialSource(credential)
	if holderErr != nil {
		return nil, apperrors.ErrInternal
	}
	defer holder.release()
	plaintext, err := s.resolveCredential(ctx, credential)
	if err != nil {
		return nil, apperrors.ErrInternal
	}
	if err := s.cacheCredentialValue(ctx, credential, plaintext); err != nil {
		return nil, err
	}
	names, verified := s.discoverModels(ctx, connection, plaintext)
	result := &CredentialVerification{Verified: verified, Message: "Credential verification failed"}
	if verified {
		result.Message = "Credential verified"
		result.DiscoveredModels = len(names)
		if entity.ConnectionAdapter(connection) == entity.AdapterAzureOpenAIClassic {
			result.DiscoveredModels = 0
			result.Message = "Credential authenticated; deployment coverage requires administrator attestation"
			names = nil
		}
	}
	auditID, err := id.NewPrefixed("aud")
	if err != nil {
		return nil, apperrors.ErrInternal
	}
	if !holder.admitUse() {
		return nil, catalogConflict
	}
	err = db.Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		if err := exactCatalogPermission(tx, actorID, "providers.write"); err != nil {
			return err
		}
		currentActor, e := exactEnabledActor(modelCreationDB(tx), actorID)
		if e != nil {
			return e
		}
		if currentActor.ID != capturedActor.ID || !currentActor.CreatedAt.Equal(capturedActor.CreatedAt) {
			return catalogConflict
		}
		if err := personalExact(modelCreationDB(tx), "id", capturedConnection.ID).Clauses(clause.Locking{Strength: "UPDATE"}).Take(&connection).Error; err != nil {
			return err
		}
		_, _, currentTransport, err := s.resolveConnectionEgress(tx, connection)
		if err != nil {
			return err
		}
		if currentTransport != transportRevision || connection.ID != capturedConnection.ID || !connection.CreatedAt.Equal(capturedConnection.CreatedAt) || connection.TransportGeneration != capturedConnection.TransportGeneration || !validTransportGeneration(connection.TransportGeneration) || !sameConnectionTransport(connectionTransportTuple(connection), connectionTransportTuple(capturedConnection)) {
			return catalogConflict
		}
		if err := personalExact(modelCreationDB(tx), "id", capturedCredential.ID).Clauses(clause.Locking{Strength: "UPDATE"}).Take(&credential).Error; err != nil {
			return err
		}
		if credential.ID != capturedCredential.ID || credential.ConnectionID != capturedCredential.ConnectionID || !credential.CreatedAt.Equal(capturedCredential.CreatedAt) {
			return catalogConflict
		}
		rows := []entity.ProviderCredential{credential}
		if e := attachCredentialSources(tx, rows); e != nil {
			return e
		}
		if credentialSourceProof(rows[0]) != sourceProof {
			return catalogConflict
		}
		// Failed native pagination preserves last-success discovery evidence,
		// while current verification and runtime authorization are revoked.
		if verified || (connection.Protocol != entity.ProtocolAnthropicMessages && connection.Protocol != entity.ProtocolGeminiGenerateContent) {
			if err := tx.Where("credential_id = ?", credential.ID).Delete(&entity.CredentialModelAccess{}).Error; err != nil {
				return err
			}
		}
		updates := map[string]any{"verification_status": "failed", "verified_at": nil, "enabled": false}
		if verified {
			updates["verification_status"], updates["verified_at"] = "verified", time.Now().UTC()
			updates["verified_transport_generation"] = connection.TransportGeneration
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
					model = entity.ProviderModel{CapabilityTransportGeneration: connection.TransportGeneration, ID: modelID, ConnectionID: connection.ID, UpstreamName: name}
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
			if entity.ConnectionAdapter(connection) == entity.AdapterAzureOpenAIClassic {
				absent, e := credentialMissingActiveDeployment(tx, connection, credential.ID)
				if e != nil {
					return e
				}
				missing = 0
				if absent {
					missing = 1
				}
			}
			if missing != 0 {
				updates["enabled"] = false
			}
		}
		if err := tx.Model(&credential).Updates(updates).Error; err != nil {
			return err
		}
		action := "credential.verify"
		if !verified {
			action = "credential.verify.failed"
		}
		return tx.Create(&entity.AuditEvent{ID: auditID, ActorID: actorID, Action: action, ResourceType: "credential", ResourceID: credential.ID}).Error
	})
	if err == nil {
		s.InvalidateRuntimeCredential(credentialID)
		if !verified {
			// Delivery observability cannot change the authoritative credential
			// result. The durable failed audit event is reconciled automatically if
			// this best-effort enqueue is interrupted.
			_ = db.Transaction(func(tx *gorm.DB) error {
				return s.recordOperationalAlertOccurrence(tx, notificationSourceCredentialVerification, auditID, "credential_verification:"+credential.ID, notificationKindCredentialFailure, "high", "verification_failed", time.Now().UTC())
			})
		}
	}
	return result, s.refreshAfterMutation(ctx, catalogError(err))
}

func (s *Service) discoverModels(ctx context.Context, connection entity.ProviderConnection, plaintext string) ([]string, bool) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	client, _, err := s.clientForConnection(ctx, connection)
	if err != nil {
		return nil, false
	}
	if client != s.upstream {
		defer client.CloseIdleConnections()
	}
	return s.discoverModelsWithClient(ctx, connection, plaintext, client)
}

// The caller owns the deadline and the captured transport client. Verification
// and transient diagnostics share native parsing without sharing mutations.
func (s *Service) discoverModelsWithClient(ctx context.Context, connection entity.ProviderConnection, plaintext string, client *http.Client) ([]string, bool) {
	if connection.Protocol == entity.ProtocolGeminiGenerateContent {
		return s.discoverGeminiModels(ctx, connection, plaintext, client)
	}
	if connection.Protocol == entity.ProtocolAnthropicMessages {
		return s.discoverMessagesModels(ctx, connection, plaintext, client)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, connection.BaseURL+"/models", nil)
	if err != nil {
		return nil, false
	}
	req.Header.Set("Authorization", "Bearer "+plaintext)
	if entity.ConnectionAdapter(connection) == entity.AdapterAzureOpenAIClassic {
		if validateConnectionAdapter(connection, s.allowPrivateUpstream) != nil {
			return nil, false
		}
		req.URL.Path = "/openai/models"
		req.URL.RawQuery = url.Values{"api-version": []string{*connection.APIVersion}}.Encode()
		req.Header.Del("Authorization")
		req.Header.Set("api-key", plaintext)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
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
