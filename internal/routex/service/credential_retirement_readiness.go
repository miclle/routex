package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"slices"
	"time"

	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
)

type CredentialRetirementEvidence struct {
	AttemptID   string    `json:"attempt_id"`
	CompletedAt time.Time `json:"completed_at"`
}

type CredentialRetirementReadiness struct {
	Source             *CredentialMetadataRecord     `json:"source"`
	Replacement        *CredentialMetadataRecord     `json:"replacement"`
	SnapshotID         *string                       `json:"snapshot_id"`
	Evidence           *CredentialRetirementEvidence `json:"evidence"`
	EligibleRouteCount int                           `json:"eligible_route_count"`
	Eligible           bool                          `json:"eligible"`
	Blockers           []string                      `json:"blockers"`
	ETag               string                        `json:"etag"`
}

var credentialRetirementBlockers = []string{
	"source_disabled", "replacement_disabled", "replacement_unverified",
	"runtime_unavailable", "runtime_stale", "route_unavailable", "coverage_missing",
	"no_eligible_routes", "scope_overflow", "evidence_missing",
}

func validCredentialRetirementPairIDs(sourceID, replacementID string) bool {
	return sourceID != replacementID && credentialDeleteID.MatchString(sourceID) && credentialDeleteID.MatchString(replacementID)
}

func credentialRetirementPair(db *gorm.DB, sourceID, replacementID string) (entity.ProviderCredential, entity.ProviderCredential, entity.ProviderConnection, error) {
	var source, replacement entity.ProviderCredential
	var connection entity.ProviderConnection
	for _, target := range []struct {
		id  string
		row *entity.ProviderCredential
	}{{sourceID, &source}, {replacementID, &replacement}} {
		if err := db.Omit("ciphertext").First(target.row, "id = ?", target.id).Error; err != nil {
			return source, replacement, connection, err
		}
		if target.row.ID != target.id {
			return source, replacement, connection, gorm.ErrRecordNotFound
		}
	}
	if source.ConnectionID != replacement.ConnectionID || replacement.ReplacesCredentialID == nil || *replacement.ReplacesCredentialID != sourceID {
		return source, replacement, connection, catalogConflict
	}
	if err := db.First(&connection, "id = ?", source.ConnectionID).Error; err != nil {
		return source, replacement, connection, err
	}
	if connection.ID != source.ConnectionID {
		return source, replacement, connection, catalogConflict
	}
	return source, replacement, connection, nil
}

// GetCredentialRetirementReadiness is advisory and writes nothing. Runtime
// capture happens without a borrowed DB connection; the consistent read closes
// before runtime validation so a single-connection pool cannot invert locks.
func (s *Service) GetCredentialRetirementReadiness(ctx context.Context, actorID, sourceID, replacementID string) (*CredentialRetirementReadiness, error) {
	if !validCredentialRetirementPairIDs(sourceID, replacementID) {
		return nil, apperrors.ErrBadRequest
	}
	db := s.authDB(ctx)
	if err := authorizeGovernance(db, actorID, "providers.read"); err != nil {
		return nil, catalogError(err)
	}
	_, _, preflightConnection, err := credentialRetirementPair(db, sourceID, replacementID)
	if err != nil {
		return nil, catalogError(err)
	}
	capture, err := s.captureCredentialRetirementRuntime(preflightConnection.ID, sourceID, replacementID)
	if err != nil {
		return nil, catalogError(err)
	}
	var source, replacement entity.ProviderCredential
	var projection *credentialRetirementRuntimeProjection
	err = db.Transaction(func(tx *gorm.DB) error {
		if err := authorizeGovernance(tx, actorID, "providers.read"); err != nil {
			return err
		}
		var connection entity.ProviderConnection
		var err error
		source, replacement, connection, err = credentialRetirementPair(tx, sourceID, replacementID)
		if err != nil {
			return err
		}
		projection, err = s.credentialRetirementRuntimeReadiness(tx, connection, source, replacement, capture)
		return err
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, catalogError(err)
	}
	return credentialRetirementReadinessRecord(source, replacement, projection, s.validateCredentialRetirementRuntimeCapture(capture))
}

func credentialRetirementReadinessRecord(source, replacement entity.ProviderCredential, projection *credentialRetirementRuntimeProjection, finalBlockers []string) (*CredentialRetirementReadiness, error) {
	if projection == nil || projection.EligibleRouteCount < 0 || projection.EligibleRouteCount > 256 {
		return nil, apperrors.ErrInternal
	}
	wanted := append([]string(nil), projection.Blockers...)
	wanted = append(wanted, finalBlockers...)
	if !source.Enabled {
		wanted = append(wanted, "source_disabled")
	}
	if !replacement.Enabled {
		wanted = append(wanted, "replacement_disabled")
	}
	if replacement.VerificationStatus != "verified" {
		wanted = append(wanted, "replacement_unverified")
	}
	if projection.EligibleRouteCount == 0 {
		wanted = append(wanted, "no_eligible_routes")
	}
	if projection.SnapshotID == "" && !slices.Contains(wanted, "runtime_stale") && !slices.Contains(wanted, "runtime_unavailable") && !slices.Contains(wanted, "scope_overflow") {
		wanted = append(wanted, "runtime_unavailable")
	}
	if projection.Evidence == nil {
		wanted = append(wanted, "evidence_missing")
	}
	for _, blocker := range wanted {
		if !slices.Contains(credentialRetirementBlockers, blocker) {
			return nil, apperrors.ErrInternal
		}
	}
	result := &CredentialRetirementReadiness{Source: credentialMetadataRecord(source), Replacement: credentialMetadataRecord(replacement), EligibleRouteCount: projection.EligibleRouteCount, Blockers: []string{}}
	for _, blocker := range credentialRetirementBlockers {
		if slices.Contains(wanted, blocker) {
			result.Blockers = append(result.Blockers, blocker)
		}
	}
	if projection.SnapshotID != "" && len(finalBlockers) == 0 && !slices.Contains(result.Blockers, "runtime_stale") && !slices.Contains(result.Blockers, "runtime_unavailable") && !slices.Contains(result.Blockers, "scope_overflow") {
		snapshot := projection.SnapshotID
		result.SnapshotID = &snapshot
	}
	var selected *CredentialRetirementEvidence
	if projection.Evidence != nil {
		selected = &CredentialRetirementEvidence{AttemptID: projection.Evidence.AttemptID, CompletedAt: projection.Evidence.CompletedAt.UTC()}
	}
	result.Eligible = len(result.Blockers) == 0
	if result.Eligible {
		result.Evidence = selected
	}
	encoded, err := json.Marshal(struct {
		SourceETag         string                        `json:"source_etag"`
		ReplacementETag    string                        `json:"replacement_etag"`
		ConnectionID       string                        `json:"connection_id"`
		LineageID          *string                       `json:"lineage_id"`
		SnapshotID         string                        `json:"snapshot_id"`
		SourceDigest       string                        `json:"source_digest"`
		ScopeDigest        string                        `json:"scope_digest"`
		EligibleRouteCount int                           `json:"eligible_route_count"`
		SelectedEvidence   *CredentialRetirementEvidence `json:"selected_evidence"`
		Eligible           bool                          `json:"eligible"`
		Blockers           []string                      `json:"blockers"`
	}{result.Source.ETag, result.Replacement.ETag, source.ConnectionID, replacement.ReplacesCredentialID, projection.SnapshotID, projection.SourceDigest, projection.ScopeDigest, result.EligibleRouteCount, selected, result.Eligible, result.Blockers})
	if err != nil {
		return nil, apperrors.ErrInternal
	}
	digest := sha256.Sum256(encoded)
	result.ETag = hex.EncodeToString(digest[:])
	return result, nil
}
