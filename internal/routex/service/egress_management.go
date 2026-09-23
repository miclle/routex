package service

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/id"
	"github.com/miclle/routex/pkg/upstream"
)

type EgressDiagnosticInput struct {
	ETag          string `json:"etag"`
	TargetBaseURL string `json:"target_base_url,omitempty"`
	ConnectionID  string `json:"connection_id,omitempty"`
}
type EgressDiagnosticView struct {
	upstream.Diagnostic
	Stale bool `json:"stale"`
}
type ConnectionEgressInput struct {
	ETag     string  `json:"etag"`
	Mode     string  `json:"mode"`
	EgressID *string `json:"egress_id"`
}
type ConnectionEgressView struct {
	ConnectionID string  `json:"connection_id"`
	Mode         string  `json:"mode"`
	EgressID     *string `json:"egress_id"`
	ETag         string  `json:"etag"`
}

func (s *Service) TestEgressDraft(ctx context.Context, actor, egressID string, input EgressInput) (*EgressDiagnosticView, error) {
	db := s.authDB(ctx)
	if err := authorizeGovernance(db, actor, "egress.test"); err != nil {
		return nil, err
	}
	var row entity.Egress
	if egressID != "" {
		if err := db.First(&row, "id = ?", egressID).Error; err != nil {
			return nil, catalogError(err)
		}
		if input.ETag == "" || input.ETag != row.ETag {
			return nil, catalogConflict
		}
	} else {
		var err error
		row.ID, err = id.NewPrefixed("egr")
		if err != nil {
			return nil, apperrors.ErrInternal
		}
	}
	row, config, _, err := s.prepareEgress(row, input)
	if err != nil {
		return nil, err
	}
	if config == nil {
		config, err = s.egressConfig(row)
		if err != nil {
			return nil, err
		}
	}
	diagnostic, err := s.diagnoseEgress(ctx, input.TestTargetBaseURL, config)
	if err != nil {
		return nil, err
	}
	if err := authorizeGovernance(db, actor, "egress.test"); err != nil {
		return nil, err
	}
	result := &EgressDiagnosticView{Diagnostic: diagnostic}
	if egressID != "" {
		var current entity.Egress
		if err := db.First(&current, "id = ?", egressID).Error; err != nil {
			return nil, catalogError(err)
		}
		result.Stale = current.ETag != input.ETag
	}
	return result, nil
}
func (s *Service) TestEgress(ctx context.Context, actor, egressID string, input EgressDiagnosticInput) (*EgressDiagnosticView, error) {
	db := s.authDB(ctx)
	if err := authorizeGovernance(db, actor, "egress.test"); err != nil {
		return nil, err
	}
	var row entity.Egress
	if err := db.First(&row, "id = ?", egressID).Error; err != nil {
		return nil, catalogError(err)
	}
	if input.ETag == "" || input.ETag != row.ETag {
		return nil, catalogConflict
	}
	config, err := s.egressConfig(row)
	if err != nil {
		return nil, err
	}
	var diagnostic upstream.Diagnostic
	var proof *egressAPIProof
	if input.ConnectionID == "" {
		diagnostic, err = s.diagnoseEgress(ctx, input.TargetBaseURL, config)
	} else {
		if input.TargetBaseURL != "" {
			return nil, apperrors.ErrBadRequest
		}
		request, captured, requestErr := s.egressAPIRequest(ctx, db, actor, input.ConnectionID)
		proof = captured
		if requestErr != nil {
			return nil, requestErr
		}
		diagnostic, err = upstream.Diagnose(ctx, request, s.allowPrivateUpstream, s.allowPrivateEgress, config)
	}
	if err != nil {
		return nil, apperrors.ErrBadRequest
	}
	result := &EgressDiagnosticView{Diagnostic: diagnostic}
	err = db.Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		if err := authorizeGovernance(tx, actor, "egress.test"); err != nil {
			return err
		}
		var current entity.Egress
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, "id = ?", egressID).Error; err != nil {
			return err
		}
		if proof != nil {
			if err := authorizeGovernance(tx, actor, "providers.write"); err != nil {
				return err
			}
			var connection entity.ProviderConnection
			var credential entity.ProviderCredential
			if err := tx.First(&connection, "id = ?", proof.Connection.ID).Error; err != nil {
				return err
			}
			if err := tx.First(&credential, "id = ?", proof.Credential.ID).Error; err != nil {
				return err
			}
			if !proof.matches(connection, credential) {
				result.Stale = true
				return nil
			}
		}
		if current.ETag != input.ETag {
			result.Stale = true
			return nil
		}
		encoded, _ := json.Marshal(diagnostic)
		return tx.Model(&current).Updates(map[string]any{"last_diagnostic": string(encoded), "last_checked_at": time.Now().UTC()}).Error
	})
	return result, catalogError(err)
}

type egressAPIProof struct {
	Connection entity.ProviderConnection
	Credential entity.ProviderCredential
}

func (p *egressAPIProof) matches(c entity.ProviderConnection, k entity.ProviderCredential) bool {
	return c.ID == p.Connection.ID && c.BaseURL == p.Connection.BaseURL && c.Protocol == p.Connection.Protocol && c.ETag == p.Connection.ETag && k.Enabled && k.VerificationStatus == "verified" && k.ID == p.Credential.ID && k.ConnectionID == c.ID && k.Ciphertext == p.Credential.Ciphertext
}
func (s *Service) egressAPIRequest(ctx context.Context, db *gorm.DB, actor, connectionID string) (*http.Request, *egressAPIProof, error) {
	if err := authorizeGovernance(db, actor, "providers.write"); err != nil {
		return nil, nil, err
	}
	var connection entity.ProviderConnection
	if err := db.First(&connection, "id = ?", connectionID).Error; err != nil {
		return nil, nil, catalogError(err)
	}
	var credential entity.ProviderCredential
	if err := db.Where("connection_id = ? AND enabled = ? AND verification_status = ?", connection.ID, true, "verified").Order("priority, created_at, id").First(&credential).Error; err != nil {
		return nil, nil, catalogError(err)
	}
	if s.secrets == nil {
		return nil, nil, secretStoreUnavailable
	}
	plaintext, err := s.secrets.Open(credential.ID, credential.Ciphertext)
	if err != nil {
		return nil, nil, secretStoreUnavailable
	}
	base, err := upstream.ValidateBaseURL(connection.BaseURL, s.allowPrivateUpstream)
	if err != nil {
		return nil, nil, apperrors.ErrBadRequest
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base.String(), "/")+"/models", nil)
	if err != nil {
		return nil, nil, apperrors.ErrBadRequest
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
		return nil, nil, apperrors.ErrBadRequest
	}
	request.Header.Set("Accept", "application/json")
	return request, &egressAPIProof{Connection: connection, Credential: credential}, nil
}

func (s *Service) SetEgressDefault(ctx context.Context, actor string, input EgressDefaultView) (*EgressDefaultView, error) {
	if input.ETag == "" || (input.EgressID != nil && *input.EgressID == "") {
		return nil, apperrors.ErrBadRequest
	}
	var result entity.EgressSetting
	s.egressMu.Lock()
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		if err := authorizeGovernance(tx, actor, "egress.write"); err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&result, 1).Error; err != nil {
			return err
		}
		if result.ETag != input.ETag {
			return catalogConflict
		}
		if input.EgressID != nil {
			var egress entity.Egress
			if err := tx.First(&egress, "id = ?", *input.EgressID).Error; err != nil {
				return err
			}
			if !egress.Enabled {
				return catalogConflict
			}
		}
		result.DefaultEgressID = input.EgressID
		var err error
		result.ETag, err = id.NewPrefixed("rev")
		if err != nil {
			return err
		}
		if err := tx.Save(&result).Error; err != nil {
			return err
		}
		return appendAudit(tx, actor, "egress.default", "egress_setting", "1")
	})
	if err == nil {
		s.invalidateEgressRuntime()
	}
	s.egressMu.Unlock()
	if err = s.refreshAfterMutation(ctx, catalogError(err)); err != nil {
		return nil, err
	}
	return &EgressDefaultView{EgressID: result.DefaultEgressID, ETag: result.ETag}, nil
}
func (s *Service) SetConnectionEgress(ctx context.Context, actor, connectionID string, input ConnectionEgressInput) (*ConnectionEgressView, error) {
	if input.ETag == "" || (input.Mode != "default" && input.Mode != "direct" && input.Mode != "proxy") || (input.Mode == "proxy") != (input.EgressID != nil) || (input.EgressID != nil && *input.EgressID == "") {
		return nil, apperrors.ErrBadRequest
	}
	db := s.authDB(ctx)
	if err := authorizeGovernance(db, actor, "providers.write"); err != nil {
		return nil, err
	}
	var original entity.ProviderConnection
	if err := db.First(&original, "id = ?", connectionID).Error; err != nil {
		return nil, catalogError(err)
	}
	if original.ETag != input.ETag {
		return nil, catalogConflict
	}
	next := original
	next.EgressMode, next.EgressID = input.Mode, input.EgressID
	_, config, revision, err := s.resolveConnectionEgress(db, next)
	if err != nil {
		return nil, err
	}
	result, err := s.diagnoseEgress(ctx, original.BaseURL, config)
	if err != nil {
		return nil, err
	}
	if !result.TransportOK {
		return nil, egressValidationFailed
	}
	s.egressMu.Lock()
	err = db.Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		if err := authorizeGovernance(tx, actor, "providers.write"); err != nil {
			return err
		}
		var current entity.ProviderConnection
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, "id = ?", connectionID).Error; err != nil {
			return err
		}
		if current.ETag != input.ETag {
			return catalogConflict
		}
		_, _, latest, err := s.resolveConnectionEgress(tx, next)
		if err != nil {
			return err
		}
		if latest != revision {
			return catalogConflict
		}
		next.ETag, err = id.NewPrefixed("rev")
		if err != nil {
			return err
		}
		if err := tx.Model(&current).Select("EgressMode", "EgressID", "ETag").Updates(&next).Error; err != nil {
			return err
		}
		return appendAudit(tx, actor, "connection.egress", "connection", connectionID)
	})
	if err == nil {
		s.invalidateEgressRuntime()
	}
	s.egressMu.Unlock()
	if err = s.refreshAfterMutation(ctx, catalogError(err)); err != nil {
		return nil, err
	}
	return &ConnectionEgressView{ConnectionID: connectionID, Mode: next.EgressMode, EgressID: next.EgressID, ETag: next.ETag}, nil
}
