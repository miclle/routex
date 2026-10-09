package service

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
)

// Original reference identifiers are server-generated fixed suffixes of the
// validated SDK object URL. Equal physical objects therefore require equal
// reference suffixes; bind every matching retained logical/auth revision.
// No secret decryption or Vault request occurs in this transaction.
func (s *Service) publishedCleanupDependencies(tx *gorm.DB, op entity.CredentialStorageOperation) (string, string, error) {
	rev, _, _, err := s.credentialReferenceClientTx(tx, operationReference(op))
	if err != nil {
		return "", "", err
	}
	object, err := credentialPhysicalObject(rev, op.ReferenceID)
	if err != nil {
		return "", "", err
	}
	var refs []entity.CredentialVaultReference
	if err = personalExact(vaultDB(tx), "reference_id", op.ReferenceID).Limit(credentialDrainObjectLimit + 1).Find(&refs).Error; err != nil {
		return "", "", err
	}
	if len(refs) > credentialDrainObjectLimit {
		return "", "", vaultUnavailable
	}
	ids := []string{}
	for _, ref := range refs {
		if ref.ReferenceID != op.ReferenceID {
			return "", "", vaultUnavailable
		}
		ids = append(ids, ref.CredentialID)
	}
	var live []entity.ProviderCredential
	if len(ids) != 0 {
		if err = vaultDB(tx).Where(memberModelsExactIDs(tx, "id", ids)).Limit(len(ids) + 1).Find(&live).Error; err != nil {
			return "", "", err
		}
	}
	hydrated := make([]entity.ProviderCredential, 0, len(refs))
	for _, ref := range refs {
		hydrated = append(hydrated, entity.ProviderCredential{ID: ref.CredentialID, CreatedAt: ref.CredentialBirth, StorageSource: "vault"})
	}
	if err := attachCredentialSources(vaultDB(tx), hydrated); err != nil {
		return "", "", err
	}
	sources := map[string]entity.CredentialVaultReference{}
	for _, credential := range hydrated {
		if credential.VaultReference == nil || credentialSourceProof(credential) == "" {
			return "", "", vaultUnavailable
		}
		sources[credential.ID] = *credential.VaultReference
	}
	// No surviving Credential, even disabled, may share a retained object. Missing
	// revision proof is uncertainty, never evidence of absence.
	parts := []string{object}
	original := false
	for _, ref := range refs {
		source, ok := sources[ref.CredentialID]
		if !ok || source.ReferenceID != ref.ReferenceID || source.RevisionID != ref.RevisionID || !personalModelETag(source.PhysicalObject) {
			return "", "", vaultUnavailable
		}
		physical := source.PhysicalObject
		if physical != object {
			continue
		}
		for _, credential := range live {
			if credential.ID == ref.CredentialID {
				return "", "", catalogConflict
			}
		}
		if ref.CredentialID == op.CredentialID {
			if ref.CredentialBirth.Equal(op.CredentialBirth) && ref.RevisionID == op.RevisionID && ref.IntegrationID == op.IntegrationID && op.IntegrationBirth != nil && ref.IntegrationBirth.Equal(*op.IntegrationBirth) && ref.ReaderGeneration == op.ReaderGeneration && ref.ExpectedMarkerSHA256 == op.ExpectedMarkerSHA256 && ref.DescriptorSHA256 == op.DescriptorSHA256 {
				original = true
			} else {
				return "", "", vaultUnavailable
			}
		}
		parts = append(parts, source.SourceContext+":"+ref.CredentialID+":"+ref.CredentialBirth.UTC().Format(time.RFC3339Nano)+":"+ref.IntegrationID+":"+ref.IntegrationBirth.UTC().Format(time.RFC3339Nano)+":"+ref.RevisionID+":"+ref.ReaderGeneration+":"+ref.ExpectedMarkerSHA256+":"+ref.DescriptorSHA256)
	}
	if !original {
		return "", "", vaultUnavailable
	}
	var operations []entity.CredentialStorageOperation
	if err = personalExact(vaultDB(tx), "reference_id", op.ReferenceID).Limit(credentialDrainObjectLimit + 1).Find(&operations).Error; err != nil {
		return "", "", err
	}
	if len(operations) > credentialDrainObjectLimit {
		return "", "", vaultUnavailable
	}
	for _, candidate := range operations {
		if candidate.ReferenceID != op.ReferenceID {
			return "", "", vaultUnavailable
		}
		if candidate.RequestID != op.RequestID {
			return "", "", catalogConflict
		}
		if candidate.State != "committed" || candidate.ClaimedUntil.After(s.secretNow()) || s.credentialCreationHeld(candidate.RequestID) {
			return "", "", catalogConflict
		}
		parts = append(parts, cleanupOperationProof(candidate))
	}
	sort.Strings(parts)
	return object, rootHash(strings.Join(parts, "\n")), nil
}

func (s *Service) publishedCleanupPreview(tx *gorm.DB, op entity.CredentialStorageOperation, claiming bool) (string, string, string, error) {
	object, dependencies, err := s.publishedCleanupDependencies(tx, op)
	if err != nil {
		if errors.Is(err, catalogConflict) {
			return "", "", "retained_reference", nil
		}
		return "", "", "published_source_unavailable", nil
	}

	attempts, attemptProof, err := publishedAttemptsTx(tx, op, object)
	if err != nil {
		return object, "", "published_drain_unproven", nil
	}
	if !claiming && len(attempts) >= 100 {
		return object, attemptProof, "published_drain_unproven", nil
	}
	pending := 0
	for _, attempt := range attempts {
		if claiming && attempt.State == "pending" && !attempt.KnownNoEffect {
			pending++
			continue
		}
		if !publishedKnownNoEffect(attempt) {
			return object, attemptProof, "published_drain_unproven", nil
		}
	}
	if claiming && pending != 1 {
		return object, attemptProof, "published_drain_unproven", nil
	}
	snap, err := s.credentialDrainSnapshot(tx, op, object)
	if err != nil {
		return object, "", "published_process_unproven", nil
	}
	proof := rootHash(dependencies + ":" + credentialDrainSnapshotProof(snap) + ":" + attemptProof)
	if !credentialDrainSnapshotValid(snap, op, object, claiming || len(attempts) > 0) {
		return object, proof, "published_process_unproven", nil
	}
	var denial entity.CredentialSourceDenial
	err = personalExact(vaultDB(tx), "physical_object", object).Take(&denial).Error
	if err == nil {
		if !sourceDenialValid(denial) || denial.CreationRequestID != op.RequestID || denial.RemoteRequestID != nil || (!claiming && len(attempts) == 0) {
			return object, proof, "published_drain_unproven", nil
		}
		proof = rootHash(proof + ":" + denial.RequestID + ":" + denial.CreatedAt.UTC().Format(time.RFC3339Nano))
		for _, use := range snap.Uses {
			if use.JoinedAt != nil && use.JoinedAt.Before(denial.CreatedAt) {
				return object, proof, "published_drain_unproven", nil
			}
		}
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return "", "", "", err
	} else if claiming || len(attempts) > 0 {
		return object, proof, "published_drain_unproven", nil
	}

	return object, proof, "", nil
}

func (s *Service) claimPublishedCleanup(tx *gorm.DB, op entity.CredentialStorageOperation, ledger entity.ProviderCredentialCleanup) (entity.CredentialSourceDenial, error) {
	object, _, code, err := s.publishedCleanupPreview(tx, op, false)
	if err != nil {
		return entity.CredentialSourceDenial{}, err
	}
	if code != "" {
		return entity.CredentialSourceDenial{}, catalogConflict
	}
	var old entity.CredentialSourceDenial
	err = personalExact(vaultDB(tx), "physical_object", object).Take(&old).Error
	if err == nil {
		if !sourceDenialValid(old) || old.CreationRequestID != op.RequestID || old.RemoteRequestID != nil {
			return old, catalogConflict
		}
		return old, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return old, err
	}
	var count int64
	if err := vaultDB(tx).Model(&entity.CredentialSourceDenial{}).Count(&count).Error; err != nil {
		return old, err
	}
	if count >= credentialDrainObjectLimit {
		return old, vaultUnavailable
	}
	row := entity.CredentialSourceDenial{PhysicalObject: object, CreationRequestID: op.RequestID, RequestID: ledger.RequestID, CreatedAt: ledger.StartedAt}
	if !sourceDenialValid(row) {
		return row, vaultUnavailable
	}
	return row, vaultDB(tx).Create(&row).Error
}

// A finite join budget is part of the existing 25-second command. Incomplete
// joins permanently block this command; its UUID retry only returns a receipt.
func (s *Service) joinPublishedCleanup(ctx context.Context, denial entity.CredentialSourceDenial, op entity.CredentialStorageOperation) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := s.reconcileCredentialDenial(ctx, denial); err != nil {
		return err
	}
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		var ready bool
		err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
			if err := lockGovernance(tx); err != nil {
				return err
			}
			snap, err := s.credentialDrainSnapshot(tx, op, denial.PhysicalObject)
			if err != nil {
				return err
			}
			ready = credentialDrainSnapshotValid(snap, op, denial.PhysicalObject, true)
			return nil
		})
		if err != nil {
			return err
		}
		if ready {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
