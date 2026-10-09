package service

import (
	"context"
	"encoding/json"
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
	// Secret resolution, transport and final continuity checks share one budget.
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	db := s.authDB(ctx)
	if err := authorizeGovernance(db, actor, "egress.test"); err != nil {
		return nil, err
	}
	var row entity.Egress
	if err := memberRolesExact(memberRolesDB(db).Model(&entity.Egress{}), "id", egressID).First(&row).Error; err != nil {
		return nil, catalogError(err)
	}
	if row.ID != egressID || !connectionMetadataBirth(row.CreatedAt) {
		return nil, apperrors.ErrNotFound
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
	var holder *credentialSourceHolder
	if input.ConnectionID == "" {
		diagnostic, err = s.diagnoseEgress(ctx, input.TargetBaseURL, config)
	} else {
		if input.TargetBaseURL != "" {
			return nil, apperrors.ErrBadRequest
		}
		captured, captureErr := s.egressAPICapture(ctx, actor, input.ConnectionID, "")
		if captureErr != nil {
			return nil, captureErr
		}
		proof = &captured
		holder, err = s.acquireCredentialSource(captured.Credential)
		if err != nil {
			return nil, connectionDiagnosticError(err)
		}
		// A retained source remains admitted until the response body is closed and
		// the final transactional source/adapter proof has been checked.
		defer holder.release()
		plaintext, resolveErr := s.resolveCredential(ctx, captured.Credential)
		if resolveErr != nil {
			return nil, connectionDiagnosticError(resolveErr)
		}
		request, requestErr := s.egressDiagnosticRequest(ctx, captured.Connection, plaintext)
		if requestErr != nil {
			return nil, requestErr
		}
		current, captureErr := s.egressAPICapture(ctx, actor, input.ConnectionID, captured.Credential.ID)
		if captureErr != nil {
			return nil, captureErr
		}
		var currentEgress entity.Egress
		if err := memberRolesExact(memberRolesDB(db).Model(&entity.Egress{}), "id", egressID).First(&currentEgress).Error; err != nil {
			return nil, catalogError(err)
		}
		if !captured.same(current) || !egressDiagnosticSame(row, currentEgress) || !holder.admitUse() {
			return nil, catalogConflict
		}
		if ctx.Err() != nil {
			return nil, connectionDiagnosticUnavailable
		}
		// The explicitly selected candidate overrides the Connection's transport.
		// It is a diagnostic, so the candidate may itself be stored disabled.
		diagnostic, err = upstream.Diagnose(ctx, request, s.allowPrivateUpstream, s.allowPrivateEgress, config)
	}
	if ctx.Err() != nil {
		return nil, connectionDiagnosticUnavailable
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
		if err := memberRolesExact(memberRolesDB(tx).Model(&entity.Egress{}), "id", egressID).Clauses(clause.Locking{Strength: "UPDATE"}).First(&current).Error; err != nil {
			return err
		}
		if proof != nil {
			currentProof, err := s.egressAPISnapshot(tx, actor, input.ConnectionID, proof.Credential.ID)
			if err != nil {
				return err
			}
			if !proof.same(currentProof) || !holder.admitUse() {
				result.Stale = true
				return nil
			}
		}
		if !egressDiagnosticSame(row, current) {
			result.Stale = true
			return nil
		}
		if ctx.Err() != nil {
			return connectionDiagnosticUnavailable
		}
		encoded, _ := json.Marshal(diagnostic)
		return tx.Model(&current).Updates(map[string]any{"last_diagnostic": string(encoded), "last_checked_at": time.Now().UTC()}).Error
	})
	return result, catalogError(err)
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
