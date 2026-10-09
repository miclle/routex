package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/upstream"
)

// ConnectionDiagnosticInput selects retained material, never a pool fallback.
type ConnectionDiagnosticInput struct {
	CredentialID string `json:"credential_id"`
}

func (input *ConnectionDiagnosticInput) UnmarshalJSON(raw []byte) error {
	if len(raw) > 4*1024 || !utf8.Valid(raw) || !modelCreationUnicode(raw) {
		return apperrors.ErrBadRequest
	}
	fields, err := decodeDefaultLimitObject(raw, []string{"credential_id"})
	if err != nil {
		return err
	}
	var next ConnectionDiagnosticInput
	if json.Unmarshal(fields["credential_id"], &next.CredentialID) != nil || !modelCreationID(next.CredentialID, "crd") {
		return apperrors.ErrBadRequest
	}
	*input = next
	return nil
}

// ConnectionDiagnosticResult describes this check, not verification or routing.
type ConnectionDiagnosticResult struct {
	ConnectionID         string    `json:"connection_id"`
	CredentialID         string    `json:"credential_id"`
	Outcome              string    `json:"outcome"`
	Scope                string    `json:"scope"`
	DiscoveredModelCount *int      `json:"discovered_model_count"`
	CheckedAt            time.Time `json:"checked_at"`
}

var connectionDiagnosticUnavailable = &apperrors.Error{Code: http.StatusServiceUnavailable, Message: "connection test unavailable"}

type connectionDiagnosticSnapshot struct {
	review     ConnectionMetadataRecord
	credential entity.ProviderCredential
	source     string
	transport  string
	egress     *upstream.EgressConfig
}

func (s *Service) connectionDiagnosticSnapshot(ctx context.Context, actorID, connectionID, credentialID, etag string) (connectionDiagnosticSnapshot, error) {
	var captured connectionDiagnosticSnapshot
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		_, _, connection, review, err := connectionMetadataSnapshot(tx, actorID, connectionID, false, false)
		if err != nil {
			return err
		}
		if !review.CanEdit {
			return apperrors.ErrForbidden
		}
		// Both digest halves must match: a diagnostic never reconciles stale intent.
		if review.ETag != etag {
			return catalogConflict
		}
		if !entity.SupportedNativeProtocol(connection.Protocol) || validateConnectionAdapter(connection, s.allowPrivateUpstream) != nil {
			return connectionDiagnosticUnavailable
		}
		var credential entity.ProviderCredential
		if err := memberRolesExact(memberRolesDB(tx).Model(&entity.ProviderCredential{}), "id", credentialID).First(&credential).Error; err != nil {
			return err
		}
		if credential.ID != credentialID || credential.ConnectionID != connection.ID || !connectionMetadataBirth(credential.CreatedAt) {
			return apperrors.ErrNotFound
		}
		rows := []entity.ProviderCredential{credential}
		if err := attachCredentialSources(tx, rows); err != nil {
			return err
		}
		credential = rows[0]
		proof := credentialSourceProof(credential)
		if proof == "" {
			return connectionDiagnosticUnavailable
		}
		config, revision, err := s.connectionDiagnosticTransport(tx, connection)
		if err != nil {
			return err
		}
		captured = connectionDiagnosticSnapshot{review: review, credential: credential, source: proof, transport: revision, egress: config}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return captured, connectionDiagnosticError(err)
}

// Resolve the stored effective egress, then bind its exact identity and birth.
// A collation alias or recreated proxy cannot authenticate a captured transport.
func (s *Service) connectionDiagnosticTransport(tx *gorm.DB, connection entity.ProviderConnection) (*upstream.EgressConfig, string, error) {
	resolvedID, config, revision, err := s.resolveConnectionEgress(tx, connection)
	if err != nil {
		return nil, "", err
	}
	expectedID := connection.EgressID
	if connection.EgressMode == "" || connection.EgressMode == "default" {
		var setting entity.EgressSetting
		if err := tx.First(&setting, 1).Error; err != nil {
			return nil, "", err
		}
		if setting.ID != 1 {
			return nil, "", connectionDiagnosticUnavailable
		}
		expectedID = setting.DefaultEgressID
	}
	if expectedID == nil {
		if resolvedID != "" || config != nil {
			return nil, "", connectionDiagnosticUnavailable
		}
		return nil, revision, nil
	}
	if !modelCreationID(*expectedID, "egr") || resolvedID != *expectedID || config == nil {
		return nil, "", connectionDiagnosticUnavailable
	}
	var row entity.Egress
	if err := memberRolesExact(memberRolesDB(tx).Model(&entity.Egress{}), "id", *expectedID).First(&row).Error; err != nil {
		return nil, "", err
	}
	if row.ID != *expectedID || !connectionMetadataBirth(row.CreatedAt) {
		return nil, "", connectionDiagnosticUnavailable
	}
	proof := connectionMetadataHash(struct {
		Revision, EgressID string
		Birth              time.Time
	}{revision, row.ID, row.CreatedAt.UTC()})
	return config, proof, nil
}

func connectionDiagnosticError(err error) error {
	if err == nil {
		return nil
	}
	var app *apperrors.Error
	if errors.As(err, &app) {
		return app
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return apperrors.ErrNotFound
	}
	return connectionDiagnosticUnavailable
}

func (snapshot connectionDiagnosticSnapshot) same(current connectionDiagnosticSnapshot) bool {
	return snapshot.review.ETag == current.review.ETag && snapshot.credential.ID == current.credential.ID &&
		snapshot.credential.ConnectionID == current.credential.ConnectionID && snapshot.credential.CreatedAt.Equal(current.credential.CreatedAt) &&
		snapshot.source == current.source && snapshot.transport == current.transport
}

// TestConnection checks the captured native model-list endpoint without changing
// catalogue configuration, verification, discovery, enablement, routing or
// persistent diagnostics. Existing Vault exposure/drain safety bookkeeping remains.
func (s *Service) TestConnection(ctx context.Context, actorID, connectionID, etag string, input ConnectionDiagnosticInput) (*ConnectionDiagnosticResult, error) {
	if err := connectionMetadataIDs(actorID, connectionID); err != nil {
		return nil, err
	}
	if !modelCreationID(input.CredentialID, "crd") || !validConnectionMetadataETag(etag) {
		return nil, apperrors.ErrBadRequest
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	captured, err := s.connectionDiagnosticSnapshot(ctx, actorID, connectionID, input.CredentialID, etag)
	if err != nil {
		return nil, err
	}
	holder, err := s.acquireCredentialSource(captured.credential)
	if err != nil {
		return nil, connectionDiagnosticError(err)
	}
	defer holder.release()
	plaintext, err := s.resolveCredential(ctx, captured.credential)
	if err != nil {
		return nil, connectionDiagnosticError(err)
	}
	client := s.upstream
	if captured.egress != nil {
		client, err = upstream.NewEgressClient(s.allowPrivateUpstream, s.allowPrivateEgress, *captured.egress)
		if err != nil {
			return nil, connectionDiagnosticUnavailable
		}
		defer client.CloseIdleConnections()
	}
	if client == nil {
		return nil, connectionDiagnosticUnavailable
	}
	current, err := s.connectionDiagnosticSnapshot(ctx, actorID, connectionID, input.CredentialID, etag)
	if err != nil {
		return nil, err
	}
	if !captured.same(current) || !holder.admitUse() {
		return nil, catalogConflict
	}
	connection := entity.ProviderConnection{ID: captured.review.ID, BaseURL: captured.review.BaseURL, Protocol: captured.review.Protocol, Adapter: captured.review.Adapter, APIVersion: captured.review.APIVersion}
	names, passed := s.discoverModelsWithClient(ctx, connection, plaintext, client)
	if ctx.Err() != nil {
		return nil, connectionDiagnosticUnavailable
	}
	current, err = s.connectionDiagnosticSnapshot(ctx, actorID, connectionID, input.CredentialID, etag)
	if err != nil {
		return nil, err
	}
	if !captured.same(current) || !holder.admitUse() {
		return nil, catalogConflict
	}
	if ctx.Err() != nil {
		return nil, connectionDiagnosticUnavailable
	}
	result := &ConnectionDiagnosticResult{ConnectionID: connectionID, CredentialID: input.CredentialID, Outcome: "failed", Scope: "model_discovery", CheckedAt: time.Now().UTC()}
	if entity.ConnectionAdapter(connection) == entity.AdapterAzureOpenAIClassic {
		result.Scope = "authentication_only"
	} else if passed {
		count := len(names)
		result.DiscoveredModelCount = &count
	}
	if passed {
		result.Outcome = "passed"
	}
	return result, nil
}
