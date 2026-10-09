package service

import (
	"context"
	"database/sql"
	"net/http"
	"net/url"
	"strings"

	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/upstream"
)

// Proofs contain retained identities, not plaintext or a discovery result.
// Connection metadata binds the actor, Provider and Connection births plus the
// complete adapter/transport generation. Source binds the retained Vault revision.
type egressAPIProof struct {
	Connection entity.ProviderConnection
	Credential entity.ProviderCredential
	Review     ConnectionMetadataRecord
	Source     string
}

func (p egressAPIProof) same(current egressAPIProof) bool {
	return p.Review.ETag == current.Review.ETag && p.Connection.ID == current.Connection.ID &&
		p.Credential.ID == current.Credential.ID && p.Credential.ConnectionID == current.Credential.ConnectionID &&
		p.Credential.CreatedAt.Equal(current.Credential.CreatedAt) && p.Source != "" && p.Source == current.Source &&
		current.Credential.Enabled && current.Credential.VerificationStatus == "verified"
}

func egressDiagnosticSame(captured, current entity.Egress) bool {
	return captured.ID == current.ID && captured.CreatedAt.Equal(current.CreatedAt) && captured.ETag == current.ETag &&
		captured.Kind == current.Kind && captured.Host == current.Host && captured.Port == current.Port &&
		captured.Enabled == current.Enabled && captured.AuthCiphertext == current.AuthCiphertext && captured.SecretGeneration == current.SecretGeneration
}

func (s *Service) egressAPICapture(ctx context.Context, actor, connectionID, credentialID string) (egressAPIProof, error) {
	var captured egressAPIProof
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		captured, err = s.egressAPISnapshot(tx, actor, connectionID, credentialID)
		return err
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return captured, connectionDiagnosticError(err)
}

func (s *Service) egressAPISnapshot(tx *gorm.DB, actor, connectionID, credentialID string) (egressAPIProof, error) {
	if err := connectionMetadataIDs(actor, connectionID); err != nil {
		return egressAPIProof{}, err
	}
	if err := authorizeGovernance(tx, actor, "egress.test"); err != nil {
		return egressAPIProof{}, err
	}
	// Existing Connection diagnostics require providers.write, not providers.read.
	_, _, connection, review, err := connectionMetadataSnapshot(tx, actor, connectionID, true, false)
	if err != nil {
		return egressAPIProof{}, err
	}
	if !entity.SupportedNativeProtocol(connection.Protocol) || validateConnectionAdapter(connection, s.allowPrivateUpstream) != nil {
		return egressAPIProof{}, connectionDiagnosticUnavailable
	}
	var credential entity.ProviderCredential
	q := memberRolesExact(memberRolesDB(tx).Model(&entity.ProviderCredential{}), "connection_id", connection.ID).
		Where("enabled = ? AND verification_status = ?", true, "verified")
	if credentialID == "" {
		// Select once using the existing priority order; later reads never fall back.
		q = q.Order("priority, created_at, id")
	} else {
		q = memberRolesExact(q, "id", credentialID)
	}
	if err := q.First(&credential).Error; err != nil {
		return egressAPIProof{}, err
	}
	if !modelCreationID(credential.ID, "crd") || credentialID != "" && credential.ID != credentialID ||
		credential.ConnectionID != connection.ID || !connectionMetadataBirth(credential.CreatedAt) ||
		!credential.Enabled || credential.VerificationStatus != "verified" {
		return egressAPIProof{}, apperrors.ErrNotFound
	}
	rows := []entity.ProviderCredential{credential}
	if err := attachCredentialSources(tx, rows); err != nil {
		return egressAPIProof{}, err
	}
	credential = rows[0]
	source := credentialSourceProof(credential)
	if source == "" {
		return egressAPIProof{}, connectionDiagnosticUnavailable
	}
	return egressAPIProof{Connection: connection, Credential: credential, Review: review, Source: source}, nil
}

// Build only the native metadata request. Diagnose owns the measured stages and
// the explicitly selected candidate transport; no verification or discovery write.
func (s *Service) egressDiagnosticRequest(ctx context.Context, connection entity.ProviderConnection, plaintext string) (*http.Request, error) {
	if validateConnectionAdapter(connection, s.allowPrivateUpstream) != nil {
		return nil, apperrors.ErrBadRequest
	}
	base, err := upstream.ValidateBaseURL(connection.BaseURL, s.allowPrivateUpstream)
	if err != nil {
		return nil, apperrors.ErrBadRequest
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base.String(), "/")+"/models", nil)
	if err != nil {
		return nil, apperrors.ErrBadRequest
	}
	switch connection.Protocol {
	case entity.ProtocolAnthropicMessages:
		request.Header.Set("x-api-key", plaintext)
		request.Header.Set("anthropic-version", "2023-06-01")
	case entity.ProtocolGeminiGenerateContent:
		request.Header.Set("x-goog-api-key", plaintext)
	case entity.ProtocolOpenAIChat, entity.ProtocolOpenAIResponses:
		request.Header.Set("Authorization", "Bearer "+plaintext)
	default:
		return nil, apperrors.ErrBadRequest
	}
	if entity.ConnectionAdapter(connection) == entity.AdapterAzureOpenAIClassic {
		request.URL.Path = "/openai/models"
		request.URL.RawQuery = url.Values{"api-version": []string{*connection.APIVersion}}.Encode()
		request.Header.Del("Authorization")
		request.Header.Set("api-key", plaintext)
	}
	request.Header.Set("Accept", "application/json")
	return request, nil
}
