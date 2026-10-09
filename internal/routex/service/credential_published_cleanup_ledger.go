package service

import (
	"errors"
	"strings"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
)

func publishedLedger(row entity.CredentialPublishedCleanup) entity.ProviderCredentialCleanup {
	return entity.ProviderCredentialCleanup{CreationRequestID: row.CreationRequestID, RequestID: row.RequestID, ActorID: row.ActorID, ActorBirth: row.ActorBirth, IntegrationID: row.IntegrationID, IntegrationBirth: row.IntegrationBirth, RevisionID: row.RevisionID, OperationProof: row.OperationProof, ReviewedETag: row.ReviewedETag, Reason: row.Reason, State: row.State, OwnershipJSON: row.OwnershipJSON, CleanupJSON: row.CleanupJSON, RootEpoch: row.RootEpoch, StartedAt: row.StartedAt, Deadline: row.Deadline, FinishedAt: row.FinishedAt}
}
func publishedCommand(row entity.ProviderCredentialCleanup, object string) entity.CredentialPublishedCleanup {
	return entity.CredentialPublishedCleanup{PhysicalObject: object, CreationRequestID: row.CreationRequestID, RequestID: row.RequestID, ActorID: row.ActorID, ActorBirth: row.ActorBirth, IntegrationID: row.IntegrationID, IntegrationBirth: row.IntegrationBirth, RevisionID: row.RevisionID, OperationProof: row.OperationProof, ReviewedETag: row.ReviewedETag, Reason: row.Reason, State: row.State, OwnershipJSON: row.OwnershipJSON, CleanupJSON: row.CleanupJSON, RootEpoch: row.RootEpoch, StartedAt: row.StartedAt, Deadline: row.Deadline, FinishedAt: row.FinishedAt}
}

// The governance lock serializes cross-ledger UUID claims. Never prefer one
// receipt when both namespaces contain a command: ambiguity is unavailable.
func cleanupCommandTx(tx *gorm.DB, command string) (entity.ProviderCredentialCleanup, bool, error) {
	var orphan entity.ProviderCredentialCleanup
	var published entity.CredentialPublishedCleanup
	a := personalExact(vaultDB(tx), "request_id", command).Take(&orphan).Error
	b := personalExact(vaultDB(tx), "request_id", command).Take(&published).Error
	if a != nil && !errors.Is(a, gorm.ErrRecordNotFound) {
		return orphan, false, a
	}
	if b != nil && !errors.Is(b, gorm.ErrRecordNotFound) {
		return orphan, false, b
	}
	if a == nil && b == nil {
		return orphan, false, vaultUnavailable
	}
	if b == nil {
		return publishedLedger(published), true, nil
	}
	if a == nil {
		return orphan, false, nil
	}
	return orphan, false, gorm.ErrRecordNotFound
}

func publishedKnownNoEffect(row entity.CredentialPublishedCleanup) bool {
	if !row.KnownNoEffect || row.State != "failed" || row.FinishedAt == nil || !personalModelETag(row.PhysicalObject) {
		return false
	}
	var ownership VaultObservationView
	var cleanup VaultCleanupView
	if !vaultDecodeObservation(row.OwnershipJSON, &ownership) || !vaultDecodeCleanup(row.CleanupJSON, &cleanup) {
		return false
	}
	return !ownership.Attempted && !ownership.Succeeded && ownership.Failure == nil && cleanup.State == "not_attempted" && !cleanup.Observation.Attempted && !cleanup.Observation.Succeeded && cleanup.Observation.Failure == nil
}

func publishedAttemptsTx(tx *gorm.DB, op entity.CredentialStorageOperation, object string) ([]entity.CredentialPublishedCleanup, string, error) {
	var attempts []entity.CredentialPublishedCleanup
	if err := personalExact(vaultDB(tx), "physical_object", object).Order("request_id").Limit(101).Find(&attempts).Error; err != nil {
		return nil, "", err
	}
	if len(attempts) > 100 {
		return nil, "", vaultUnavailable
	}
	parts := []string{}
	for _, row := range attempts {
		if !publishedCommandValid(row, op, object) {
			return nil, "", vaultUnavailable
		}
		parts = append(parts, row.RequestID+":"+row.ActorID+":"+row.ActorBirth.UTC().Format(time.RFC3339Nano)+":"+row.IntegrationID+":"+row.IntegrationBirth.UTC().Format(time.RFC3339Nano)+":"+row.RevisionID+":"+row.OperationProof+":"+row.State+":"+row.ReviewedETag+":"+row.Reason+":"+row.StartedAt.UTC().Format(time.RFC3339Nano)+":"+row.Deadline.UTC().Format(time.RFC3339Nano)+":"+row.OwnershipJSON+":"+row.CleanupJSON)
		if row.FinishedAt != nil {
			parts = append(parts, row.RequestID+":"+row.FinishedAt.UTC().Format(time.RFC3339Nano))
		}
		if row.KnownNoEffect {
			parts = append(parts, row.RequestID+":known-no-effect")
		}
	}
	return attempts, rootHash(strings.Join(parts, "\n")), nil
}

// RemoteRequestID is permanent and unique to the physical object. It is claimed
// before constructing an SDK owner, even when that owner later fails to send.
func (s *Service) claimPublishedRemote(tx *gorm.DB, op entity.CredentialStorageOperation, ledger entity.ProviderCredentialCleanup, object string) error {
	var denial entity.CredentialSourceDenial
	if err := personalExact(vaultDB(tx), "physical_object", object).Take(&denial).Error; err != nil {
		return err
	}
	if !sourceDenialValid(denial) || denial.CreationRequestID != op.RequestID || denial.RemoteRequestID != nil {
		return catalogConflict
	}
	var attempt entity.CredentialPublishedCleanup
	if err := personalExact(vaultDB(tx), "request_id", ledger.RequestID).Take(&attempt).Error; err != nil {
		return err
	}
	if attempt.PhysicalObject != object || publishedLedger(attempt).OperationProof != ledger.OperationProof || attempt.State != "pending" || attempt.KnownNoEffect {
		return catalogConflict
	}
	updated := personalExact(vaultDB(tx).Model(&entity.CredentialSourceDenial{}), "physical_object", object).Where("remote_request_id IS NULL").Update("remote_request_id", ledger.RequestID)
	if updated.Error != nil {
		return updated.Error
	}
	if updated.RowsAffected != 1 {
		return catalogConflict
	}
	return nil
}

// KnownNoEffect records only the local pre-SDK control-flow branch. A separate
// fresh complete join is still mandatory before any subsequent manual intent;
// this flag alone never attests that an outstanding body has finished.
func (s *Service) finishPublishedCleanup(tx *gorm.DB, ledger entity.ProviderCredentialCleanup, object string, beforeRemoteBoundary bool) error {
	var current entity.CredentialPublishedCleanup
	if err := personalExact(vaultDB(tx), "request_id", ledger.RequestID).Take(&current).Error; err != nil {
		return err
	}
	original := publishedLedger(current)
	if current.PhysicalObject != object || current.State != "pending" || current.KnownNoEffect || original.CreationRequestID != ledger.CreationRequestID || original.ActorID != ledger.ActorID || !original.ActorBirth.Equal(ledger.ActorBirth) || original.IntegrationID != ledger.IntegrationID || !original.IntegrationBirth.Equal(ledger.IntegrationBirth) || original.RevisionID != ledger.RevisionID || original.OperationProof != ledger.OperationProof || original.ReviewedETag != ledger.ReviewedETag || original.Reason != ledger.Reason || original.RootEpoch != ledger.RootEpoch || !original.StartedAt.Equal(ledger.StartedAt) || !original.Deadline.Equal(ledger.Deadline) {
		return catalogConflict
	}
	finished := publishedCommand(ledger, object)
	finished.KnownNoEffect = beforeRemoteBoundary && ledger.State == "failed"
	if finished.KnownNoEffect {
		var denial entity.CredentialSourceDenial
		if err := personalExact(vaultDB(tx), "physical_object", object).Take(&denial).Error; err != nil {
			return err
		}
		if !sourceDenialValid(denial) || denial.CreationRequestID != ledger.CreationRequestID || denial.RemoteRequestID != nil || !publishedKnownNoEffect(finished) {
			return vaultUnavailable
		}
	}
	updated := personalExact(vaultDB(tx).Model(&entity.CredentialPublishedCleanup{}), "request_id", ledger.RequestID).Where("state = ? AND known_no_effect = ?", "pending", false).Updates(map[string]any{"state": finished.State, "ownership_json": finished.OwnershipJSON, "cleanup_json": finished.CleanupJSON, "finished_at": finished.FinishedAt, "known_no_effect": finished.KnownNoEffect})
	if updated.Error != nil {
		return updated.Error
	}
	if updated.RowsAffected != 1 {
		return catalogConflict
	}
	return nil
}

func publishedCommandValid(row entity.CredentialPublishedCleanup, op entity.CredentialStorageOperation, object string) bool {
	if row.PhysicalObject != object || !personalModelETag(object) || row.CreationRequestID != op.RequestID || row.RequestID == op.RequestID || row.OperationProof != cleanupOperationProof(op) || row.IntegrationID != op.IntegrationID || op.IntegrationBirth == nil || !row.IntegrationBirth.Equal(*op.IntegrationBirth) || row.RevisionID != op.RevisionID || !connectionMetadataBirth(row.ActorBirth) || !rootReason(row.Reason) || !vaultStrong(row.ReviewedETag) {
		return false
	}
	if _, err := cleanupReceipt(publishedLedger(row), row.StartedAt); err != nil {
		return false
	}
	return !row.KnownNoEffect || publishedKnownNoEffect(row)
}
