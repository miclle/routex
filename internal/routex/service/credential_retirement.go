package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/id"
)

type CredentialRetirementInput struct {
	RequestID               string `json:"request_id"`
	ReplacementCredentialID string `json:"replacement_credential_id"`
	EvidenceAttemptID       string `json:"evidence_attempt_id"`
	SnapshotID              string `json:"snapshot_id"`
	Reason                  string `json:"reason"`
}

type CredentialRetirementRecord struct {
	RequestID               string    `json:"request_id"`
	SourceCredentialID      string    `json:"source_credential_id"`
	ReplacementCredentialID string    `json:"replacement_credential_id"`
	Committed               bool      `json:"committed"`
	CommittedAt             time.Time `json:"committed_at"`
	RuntimeApplied          bool      `json:"runtime_applied"`
	CurrentSnapshotID       *string   `json:"current_snapshot_id"`
	Blockers                []string  `json:"blockers"`
}

var credentialRetirementSnapshotID = regexp.MustCompile(`^cfg_[0-7][0-9a-hjkmnp-tv-z]{25}$`)
var credentialRetirementApplicationBlockers = []string{"source_reenabled", "source_missing", "replacement_missing", "relationship_changed", "replacement_disabled", "replacement_unverified", "runtime_unavailable", "runtime_stale", "route_unavailable", "coverage_missing", "no_eligible_routes", "scope_overflow"}

func validCredentialRetirementIntent(sourceID, etag string, input CredentialRetirementInput) bool {
	if !validCredentialRetirementPairIDs(sourceID, input.ReplacementCredentialID) || !credentialReplacementRequestID.MatchString(input.RequestID) || !credentialRetirementSnapshotID.MatchString(input.SnapshotID) || !safeCallID.MatchString(input.EvidenceAttemptID) || !validCredentialMetadataReason(input.Reason) || len(etag) != 64 || etag != strings.ToLower(etag) {
		return false
	}
	_, err := hex.DecodeString(etag)
	return err == nil
}

func credentialRetirementHash(actorID, sourceID, etag string, input CredentialRetirementInput) string {
	encoded, _ := json.Marshal(struct {
		ActorID  string                    `json:"actor_id"`
		SourceID string                    `json:"source_id"`
		ETag     string                    `json:"etag"`
		Input    CredentialRetirementInput `json:"input"`
	}{actorID, sourceID, etag, input})
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

func credentialRetirementReceiptMatches(row entity.CredentialRetirementReceipt, actorID, sourceID, etag, requestHash string, input CredentialRetirementInput) bool {
	return row.RequestID == input.RequestID && row.ActorID == actorID && row.SourceCredentialID == sourceID && row.ReplacementCredentialID == input.ReplacementCredentialID && row.RequestHash == requestHash && row.ReadinessETag == etag && row.PreDisableSnapshotID == input.SnapshotID && row.EvidenceAttemptID == input.EvidenceAttemptID && !row.CommittedAt.IsZero()
}

func retirementReceipt(tx *gorm.DB, actorID, sourceID, etag, hash string, input CredentialRetirementInput) (*entity.CredentialRetirementReceipt, error) {
	var row entity.CredentialRetirementReceipt
	if err := tx.First(&row, "request_id = ?", input.RequestID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	if !credentialRetirementReceiptMatches(row, actorID, sourceID, etag, hash, input) {
		return nil, catalogConflict
	}
	return &row, nil
}

func credentialRetirementModelIDs(tx *gorm.DB, connectionID string) ([]string, error) {
	var modelIDs []string
	if err := tx.Table("model_provider_bindings AS b").Distinct("b.model_id").Joins("JOIN provider_models p ON p.id = b.provider_model_id").Where("p.connection_id = ?", connectionID).Order("b.model_id").Limit(maxCredentialReadinessRoutes+1).Pluck("b.model_id", &modelIDs).Error; err != nil {
		return nil, err
	}
	if len(modelIDs) > maxCredentialReadinessRoutes {
		return nil, catalogConflict
	}
	sort.Strings(modelIDs)
	return modelIDs, nil
}

// RetireCredential commits one reviewed reduction. Historical receipt replay is
// authorized before resource lookup and never repeats the disable or tombstone.
func (s *Service) RetireCredential(ctx context.Context, actorID, sourceID, etag string, input CredentialRetirementInput) (*CredentialRetirementRecord, error) {
	input.Reason = strings.TrimSpace(input.Reason)
	if !validCredentialRetirementIntent(sourceID, etag, input) {
		return nil, apperrors.ErrBadRequest
	}
	hash := credentialRetirementHash(actorID, sourceID, etag, input)
	s.egressMu.RLock()
	releasePin, pinErr := s.pinCredentialRetirementRuntime()
	released := false
	release := func() {
		if !released {
			released = true
			if releasePin != nil {
				releasePin()
			}
			s.egressMu.RUnlock()
		}
	}
	defer release()
	workCtx := ctx
	if pinErr == nil {
		// A mutation pin cannot delay publication beyond a bounded part of the
		// authorization lease, even while a pool or row lock is contended.
		auth := s.runtime.auth.Load()
		clock := s.gatewayAttemptClock()
		budget := 2 * time.Second
		if auth == nil || !clock.Before(auth.ValidUntil.Add(-100*time.Millisecond)) {
			release()
			pinErr = runtimeUnavailable
		} else {
			budget = min(budget, auth.ValidUntil.Sub(clock)-100*time.Millisecond)
			var cancel context.CancelFunc
			workCtx, cancel = context.WithTimeout(ctx, budget)
			defer cancel()
		}
	}
	db := s.authDB(workCtx)
	var receipt *entity.CredentialRetirementReceipt
	// A missing publication must not conceal a historical committed receipt.
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		if err := authorizeGovernance(tx, actorID, "providers.write"); err != nil {
			return err
		}
		var err error
		receipt, err = retirementReceipt(tx, actorID, sourceID, etag, hash, input)
		return err
	})
	if err != nil {
		if workCtx.Err() != nil {
			return nil, runtimeUnavailable
		}
		return nil, catalogError(err)
	}
	if receipt != nil {
		release()
		return s.reconcileCredentialRetirement(ctx, *receipt), nil
	}
	if pinErr != nil {
		return nil, pinErr
	}
	source, replacement, connection, preflightErr := credentialRetirementPair(db, sourceID, input.ReplacementCredentialID)
	var modelIDs []string
	var capture *credentialRetirementRuntimeCapture
	if preflightErr == nil {
		modelIDs, preflightErr = credentialRetirementModelIDs(db, connection.ID)
	}
	if preflightErr == nil {
		capture, preflightErr = s.captureCredentialRetirementRuntime(connection.ID, sourceID, input.ReplacementCredentialID)
	}
	fresh := false
	err = db.Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		if err := authorizeGovernance(tx, actorID, "providers.write"); err != nil {
			return err
		}
		var err error
		receipt, err = retirementReceipt(tx, actorID, sourceID, etag, hash, input)
		if err != nil || receipt != nil {
			return err
		}
		if preflightErr != nil {
			return preflightErr
		}
		if len(modelIDs) > 0 {
			var locked []entity.Model
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id IN ?", modelIDs).Order("id").Find(&locked).Error; err != nil {
				return err
			}
			actual := make([]string, 0, len(locked))
			for _, row := range locked {
				actual = append(actual, row.ID)
			}
			sort.Strings(actual)
			if !slices.Equal(actual, modelIDs) {
				return catalogConflict
			}
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&connection, "id = ?", connection.ID).Error; err != nil {
			return err
		}
		currentModels, err := credentialRetirementModelIDs(tx, connection.ID)
		if err != nil {
			return err
		}
		if !slices.Equal(modelIDs, currentModels) {
			return catalogConflict
		}
		var credentials []entity.ProviderCredential
		if err := tx.Omit("ciphertext").Clauses(clause.Locking{Strength: "UPDATE"}).Where("id IN ?", []string{sourceID, input.ReplacementCredentialID}).Order("id").Find(&credentials).Error; err != nil {
			return err
		}
		if len(credentials) != 2 {
			return gorm.ErrRecordNotFound
		}
		for _, row := range credentials {
			switch row.ID {
			case sourceID:
				source = row
			case input.ReplacementCredentialID:
				replacement = row
			default:
				return catalogConflict
			}
		}
		if source.ID != sourceID || replacement.ID != input.ReplacementCredentialID || source.ConnectionID != connection.ID || replacement.ConnectionID != connection.ID || replacement.ReplacesCredentialID == nil || *replacement.ReplacesCredentialID != sourceID || !source.Enabled {
			return catalogConflict
		}
		projection, err := s.credentialRetirementRuntimeReadinessForAttempt(tx, connection, source, replacement, capture, input.EvidenceAttemptID)
		if err != nil {
			return err
		}
		reviewed, err := credentialRetirementReadinessRecord(source, replacement, projection, s.validateCredentialRetirementRuntimeCapture(capture))
		if err != nil {
			return err
		}
		if !reviewed.Eligible || reviewed.ETag != etag || reviewed.SnapshotID == nil || *reviewed.SnapshotID != input.SnapshotID || reviewed.Evidence == nil || reviewed.Evidence.AttemptID != input.EvidenceAttemptID {
			return catalogConflict
		}
		// The final lease/epoch/health check occurs immediately before the reduction,
		// while immutable publication is pinned and every scoped writer is locked.
		if len(s.validateCredentialRetirementRuntimeCapture(capture)) > 0 {
			return catalogConflict
		}
		committed := time.Now().UTC().Truncate(time.Microsecond)
		row := entity.CredentialRetirementReceipt{RequestID: input.RequestID, ActorID: actorID, SourceCredentialID: sourceID, ReplacementCredentialID: replacement.ID, ConnectionID: connection.ID, RequestHash: hash, ReadinessETag: etag, SourceETag: credentialMetadataRecord(source).ETag, ReplacementETag: credentialMetadataRecord(replacement).ETag, PreDisableSnapshotID: input.SnapshotID, EvidenceAttemptID: input.EvidenceAttemptID, CommittedAt: committed}
		if err := tx.Model(&entity.ProviderCredential{}).Where("id = ?", sourceID).Update("enabled", false).Error; err != nil {
			return err
		}
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		if err := appendCredentialRetirementAudit(tx, actorID, row, input.Reason); err != nil {
			return err
		}
		receipt = &row
		fresh = true
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		if workCtx.Err() != nil {
			return nil, runtimeUnavailable
		}
		return nil, catalogError(err)
	}
	if fresh {
		s.InvalidateRuntimeCredential(sourceID)
	}
	release()
	return s.reconcileCredentialRetirement(ctx, *receipt), nil
}

func (s *Service) reconcileCredentialRetirement(ctx context.Context, receipt entity.CredentialRetirementReceipt) *CredentialRetirementRecord {
	result := &CredentialRetirementRecord{RequestID: receipt.RequestID, SourceCredentialID: receipt.SourceCredentialID, ReplacementCredentialID: receipt.ReplacementCredentialID, Committed: true, CommittedAt: receipt.CommittedAt.UTC(), Blockers: []string{}}
	add := func(blocker string) {
		if !slices.Contains(result.Blockers, blocker) {
			result.Blockers = append(result.Blockers, blocker)
		}
	}
	if s.runtime == nil || s.RefreshRuntime(ctx) != nil {
		add("runtime_unavailable")
	}
	db := s.authDB(ctx)
	var source, replacement entity.ProviderCredential
	for _, target := range []struct {
		id      string
		row     *entity.ProviderCredential
		missing string
	}{{receipt.SourceCredentialID, &source, "source_missing"}, {receipt.ReplacementCredentialID, &replacement, "replacement_missing"}} {
		err := db.Omit("ciphertext").First(target.row, "id = ?", target.id).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			add(target.missing)
		} else if err != nil {
			add("runtime_unavailable")
		} else if target.row.ID != target.id {
			add("relationship_changed")
		}
	}
	if source.ID != receipt.SourceCredentialID || replacement.ID != receipt.ReplacementCredentialID {
		return result
	}
	if source.Enabled {
		add("source_reenabled")
	}
	if !replacement.Enabled {
		add("replacement_disabled")
	}
	if replacement.VerificationStatus != "verified" {
		add("replacement_unverified")
	}
	if source.ConnectionID != receipt.ConnectionID || replacement.ConnectionID != receipt.ConnectionID || replacement.ReplacesCredentialID == nil || *replacement.ReplacesCredentialID != receipt.SourceCredentialID {
		add("relationship_changed")
	}
	if len(result.Blockers) > 0 {
		return result
	}
	capture, err := s.captureCredentialRetirementRuntime(receipt.ConnectionID, source.ID, replacement.ID)
	if err != nil {
		add("runtime_unavailable")
		return result
	}
	var projection *credentialRetirementRuntimeProjection
	err = db.Transaction(func(tx *gorm.DB) error {
		var connection entity.ProviderConnection
		var err error
		source, replacement, connection, err = credentialRetirementPair(tx, receipt.SourceCredentialID, receipt.ReplacementCredentialID)
		if err != nil {
			return err
		}
		if source.Enabled {
			add("source_reenabled")
			return nil
		}
		if !replacement.Enabled {
			add("replacement_disabled")
			return nil
		}
		if replacement.VerificationStatus != "verified" {
			add("replacement_unverified")
			return nil
		}
		projection, err = s.credentialRetirementRuntimeApplication(tx, connection, source, replacement, capture)
		return err
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		add("runtime_unavailable")
		return result
	}
	if len(result.Blockers) > 0 {
		return result
	}
	if projection == nil {
		add("runtime_unavailable")
		return result
	}
	for _, blocker := range append(projection.Blockers, s.validateCredentialRetirementRuntimeCapture(capture)...) {
		if slices.Contains(credentialRetirementApplicationBlockers, blocker) {
			add(blocker)
		} else {
			add("runtime_unavailable")
		}
	}
	if projection.SnapshotID == "" && len(result.Blockers) == 0 {
		add("runtime_unavailable")
	}
	if len(result.Blockers) == 0 {
		snapshot := projection.SnapshotID
		result.CurrentSnapshotID = &snapshot
		result.RuntimeApplied = true
	}
	return result
}

func appendCredentialRetirementAudit(tx *gorm.DB, actorID string, receipt entity.CredentialRetirementReceipt, reason string) error {
	encoded, err := json.Marshal(struct {
		SourceID          string `json:"source_id"`
		ReplacementID     string `json:"replacement_id"`
		ConnectionID      string `json:"connection_id"`
		RequestID         string `json:"request_id"`
		SnapshotID        string `json:"snapshot_id"`
		EvidenceAttemptID string `json:"evidence_attempt_id"`
		Reason            string `json:"reason"`
		Before            struct {
			Enabled bool `json:"enabled"`
		} `json:"before"`
		After struct {
			Enabled bool `json:"enabled"`
		} `json:"after"`
	}{SourceID: receipt.SourceCredentialID, ReplacementID: receipt.ReplacementCredentialID, ConnectionID: receipt.ConnectionID, RequestID: receipt.RequestID, SnapshotID: receipt.PreDisableSnapshotID, EvidenceAttemptID: receipt.EvidenceAttemptID, Reason: reason, Before: struct {
		Enabled bool `json:"enabled"`
	}{true}, After: struct {
		Enabled bool `json:"enabled"`
	}{false}})
	if err != nil {
		return apperrors.ErrInternal
	}
	eventID, err := id.NewPrefixed("aud")
	if err != nil {
		return err
	}
	raw := string(encoded)
	return tx.Create(&entity.AuditEvent{ID: eventID, ActorID: actorID, Action: "credential.retire", ResourceType: "credential", ResourceID: receipt.SourceCredentialID, DetailsJSON: &raw}).Error
}
