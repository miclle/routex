package service

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/id"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// StartSecretRotationWorker joins all owned work on stop. A page has a finite
// deadline and never holds governance locks while awaiting remote I/O.
func (s *Service) StartSecretRotationWorker(ctx context.Context) (func(), error) {
	run, cancel := context.WithCancel(context.WithoutCancel(ctx))
	done := make(chan struct{})
	var once sync.Once
	go func() {
		defer close(done)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-run.Done():
				return
			case <-ticker.C:
				work, stop := context.WithTimeout(run, 10*time.Second)
				_ = s.RunSecretRotationOnce(work)
				stop()
			}
		}
	}()
	return func() { once.Do(cancel); <-done }, nil
}

// RunSecretRotationOnce performs at most one 50-row page, or one bounded
// publication/observation step. It is also the controlled acceptance entry point.
func (s *Service) RunSecretRotationOnce(ctx context.Context) error {
	s.rootMutation.Lock()
	defer s.rootMutation.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var p entity.SecretWritePolicy
	if err := s.authDB(ctx).Take(&p, 1).Error; err != nil {
		return catalogError(err)
	}
	if !p.Initialized {
		return nil
	}
	current := s.rootPolicy.Load()
	if current == nil || current.epoch != p.Epoch {
		if err := s.loadSecretPolicy(ctx); err != nil {
			return catalogError(err)
		}
	}
	if p.ActiveJobID == nil {
		if err := s.RefreshRuntime(ctx); err != nil {
			return secretStoreUnavailable
		}
		return catalogError(s.authDB(ctx).Transaction(func(tx *gorm.DB) error { _, err := s.rootPersistProof(tx, p); return err }))
	}
	var job entity.SecretRotationJob
	if err := personalExact(s.authDB(ctx), "id", *p.ActiveJobID).Take(&job).Error; err != nil {
		return catalogError(err)
	}
	if _, err := rootCountsChecked(job); err != nil {
		return err
	}
	if job.Status != "completed" && job.Status != "rolled_back" && job.InventoryVersion != rootInventoryVersion {
		return s.rootBlock(ctx, p, &job, "inventory_scope_changed")
	}
	if job.Status == "blocked" || job.Status == "completed" || job.Status == "rolled_back" {
		return nil
	}
	if err := s.RefreshRuntime(ctx); err != nil {
		return s.rootBlock(ctx, p, &job, "publication_pending")
	}
	if _, err := s.rootCurrentProof(s.authDB(ctx), p); err != nil {
		return s.rootBlock(ctx, p, &job, "process_unverified")
	}
	if err := s.rootClaim(ctx, &job, p); err != nil {
		return catalogError(err)
	}
	if job.Phase == "migration" || job.Phase == "verification" {
		if job.Domain < len(rootDomains) {
			if err := s.rootProcessPage(ctx, p, job); err != nil {
				return err
			}
			// A committed page can change the routing source digest. Publish
			// after all page transactions and egress locks are released so a
			// following action can prove the actual current inventory.
			if err := s.RefreshRuntime(ctx); err != nil {
				// The page owns a detached job value. Preserve its committed
				// cursor/counts rather than blocking with the pre-page copy.
				if err := personalExact(s.authDB(ctx), "id", job.ID).Take(&job).Error; err != nil {
					return catalogError(err)
				}
				return s.rootBlock(ctx, p, &job, "publication_pending")
			}
			return nil
		}
		if job.Phase == "migration" {
			job.Phase = "verification"
			job.Domain = 0
			job.Cursor = ""
			job.ScanGeneration++
			return catalogError(s.rootSaveCheckpoint(ctx, p, &job))
		}
	}
	if err := s.RefreshRuntime(ctx); err != nil {
		return s.rootBlock(ctx, p, &job, "publication_pending")
	}
	return catalogError(s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		locked, err := lockedSecretPolicy(tx)
		if err != nil {
			return err
		}
		if !rootSameJobPolicy(locked, p, job) {
			return secretPolicyChanged
		}
		proof, err := s.rootPersistProof(tx, locked)
		if err != nil {
			job.Status = "blocked"
			job.BlockerCode = "process_unverified"
			job.ObservationStartedAt = nil
			job.ObservationLastConfirmedAt = nil
			return rootSaveLeasedJob(tx, &job, s.secretNow())
		}
		now := s.secretNow()
		same := job.ObservationStartedAt != nil && job.ObservationLastConfirmedAt != nil && job.VerifiedProcessID == proof.ProcessID && job.VerifiedSnapshotID == proof.RuntimeSnapshotID && now.Sub(*job.ObservationLastConfirmedAt) <= 15*time.Second && now.Sub(*job.ObservationLastConfirmedAt) >= 0
		if !same {
			job.ObservationStartedAt = &now
		}
		job.ObservationLastConfirmedAt = &now
		job.VerifiedProcessID = proof.ProcessID
		job.VerifiedSnapshotID = proof.RuntimeSnapshotID
		job.Status = "observing"
		job.Phase = "observation"
		job.BlockerCode = "observation_pending"
		if now.Sub(*job.ObservationStartedAt) >= secretObservationDuration {
			job.Status = "ready"
			job.BlockerCode = ""
		}
		return rootSaveLeasedJob(tx, &job, now)
	}))
}
func rootSameJobPolicy(current, expected entity.SecretWritePolicy, job entity.SecretRotationJob) bool {
	return current.Initialized && current.Epoch == expected.Epoch && current.Epoch == job.CutoverEpoch && current.WriteKeyID != nil && *current.WriteKeyID == job.TargetKeyID && current.ActiveJobID != nil && *current.ActiveJobID == job.ID
}
func (s *Service) rootClaim(ctx context.Context, job *entity.SecretRotationJob, p entity.SecretWritePolicy) error {
	s.instanceMu.RLock()
	lease := s.instance
	s.instanceMu.RUnlock()
	if lease == nil {
		return secretStoreUnavailable
	}
	token, err := id.NewPrefixed("lck")
	if err != nil {
		return err
	}
	return s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		var current entity.SecretRotationJob
		if err := personalExact(tx, "id", job.ID).Clauses(clause.Locking{Strength: "UPDATE"}).Take(&current).Error; err != nil {
			return err
		}
		now := s.secretNow()
		if current.LeaseUntil != nil && current.LeaseUntil.After(now) && current.LeaseExecutorID != lease.id {
			return secretStoreUnavailable
		}
		if current.CutoverEpoch != p.Epoch || current.ETag != job.ETag {
			return secretPolicyChanged
		}
		until := now.Add(30 * time.Second)
		current.LeaseToken = token
		current.LeaseExecutorID = lease.id
		current.LeaseUntil = &until
		if err := tx.Save(&current).Error; err != nil {
			return err
		}
		*job = current
		return nil
	})
}
func rootSaveLeasedJob(tx *gorm.DB, job *entity.SecretRotationJob, now time.Time) error {
	priorToken := job.LeaseToken
	job.UpdatedAt = now
	job.ETag = rootJobETag(*job)
	result := personalExact(personalExact(tx.Model(&entity.SecretRotationJob{}), "id", job.ID), "lease_token", priorToken).Select("*").Updates(job)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return secretPolicyChanged
	}
	return nil
}
func (s *Service) rootSaveCheckpoint(ctx context.Context, p entity.SecretWritePolicy, job *entity.SecretRotationJob) error {
	return s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		current, err := lockedSecretPolicy(tx)
		if err != nil {
			return err
		}
		if !rootSameJobPolicy(current, p, *job) {
			return secretPolicyChanged
		}
		return rootSaveLeasedJob(tx, job, s.secretNow())
	})
}
func (s *Service) rootBlock(ctx context.Context, p entity.SecretWritePolicy, job *entity.SecretRotationJob, code string) error {
	job.Status = "blocked"
	job.BlockerCode = code
	job.ObservationStartedAt = nil
	job.ObservationLastConfirmedAt = nil
	err := s.rootSaveCheckpoint(ctx, p, job)
	if err != nil {
		return catalogError(err)
	}
	return secretStoreUnavailable
}
func (s *Service) rootProcessPage(ctx context.Context, p entity.SecretWritePolicy, job entity.SecretRotationJob) error {
	domain := rootDomains[job.Domain]
	rows, err := s.rootInventoryPage(ctx, domain, job.Cursor)
	if err != nil {
		return s.rootBlock(ctx, p, &job, "ciphertext_invalid")
	}
	if len(rows) == 0 {
		counts := rootCounts(job)
		if _, seen := counts[domain]; !seen {
			counts[domain] = rootDomainCounts{}
		}
		encoded, err := json.Marshal(counts)
		if err != nil {
			return err
		}
		job.CountsJSON = string(encoded)
		job.Domain++
		job.Cursor = ""
		return catalogError(s.rootSaveCheckpoint(ctx, p, &job))
	}
	systemJob := s.beginSystemJob(SystemJobSecretRootRotation, len(rows))
	completed := 0
	defer func() {
		status, detail := systemJobCompleted, "processed"
		if completed != len(rows) {
			status, detail = systemJobFailed, "rotation_blocked"
		}
		s.finishSystemJob(systemJob, SystemJobSecretRootRotation, status, detail, completed, len(rows))
	}()
	for _, row := range rows {
		targetID, authErr := s.secrets.KeyID(row.reference, row.ciphertext)
		resultCipher := row.ciphertext
		outcome := "already_target"
		if authErr != nil {
			outcome = "blocked"
		} else if targetID != job.TargetKeyID {
			if job.Phase == "verification" {
				outcome = "blocked"
			} else {
				resultCipher, err = s.secrets.Rewrap(row.reference, row.ciphertext, job.TargetKeyID)
				if err != nil {
					outcome = "blocked"
				} else {
					outcome = "rewrapped"
				}
			}
		}
		// Egress write ordering is mutex -> governance -> policy. MFA is User ->
		// policy, never governance. Every other domain uses governance -> policy.
		if domain == "egresses" {
			s.egressMu.Lock()
		}
		err = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
			if domain == "user_mfa" {
				if err := rootMFALock(tx, row); err != nil {
					return err
				}
			} else if err := lockGovernance(tx); err != nil {
				return err
			}
			current, err := lockedSecretPolicy(tx)
			if err != nil {
				return err
			}
			if !rootSameJobPolicy(current, p, job) {
				return secretPolicyChanged
			}
			var leased entity.SecretRotationJob
			if err := personalExact(tx, "id", job.ID).Take(&leased).Error; err != nil {
				return err
			}
			if leased.LeaseToken != job.LeaseToken || leased.LeaseUntil == nil || !leased.LeaseUntil.After(s.secretNow()) {
				return secretPolicyChanged
			}
			if outcome == "rewrapped" {
				outcome, err = s.rootCAS(tx, domain, row, resultCipher)
				if err != nil {
					return err
				}
			}
			item := entity.SecretRotationItem{JobID: job.ID, Domain: domain, SubjectID: row.id, SubjectGeneration: row.generation, Reference: row.reference, OriginalDigest: rootHash(row.ciphertext), ResultDigest: rootHash(resultCipher), Outcome: outcome, Attempts: 1, UpdatedAt: s.secretNow()}
			if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "job_id"}, {Name: "domain"}, {Name: "subject_id"}}, DoUpdates: clause.Assignments(map[string]any{"subject_generation": item.SubjectGeneration, "reference": item.Reference, "original_digest": item.OriginalDigest, "result_digest": item.ResultDigest, "outcome": item.Outcome, "attempts": gorm.Expr("secret_rotation_items.attempts + 1"), "updated_at": item.UpdatedAt})}).Create(&item).Error; err != nil {
				return err
			}
			counts := rootCounts(job)
			n := counts[domain]
			n.Scanned++
			switch outcome {
			case "rewrapped":
				n.Rewrapped++
			case "already_target":
				n.AlreadyTarget++
			case "deleted":
				n.Deleted++
			case "changed":
				n.Changed++
			case "blocked":
				n.Blocked++
			}
			counts[domain] = n
			encoded, _ := json.Marshal(counts)
			job.CountsJSON = string(encoded)
			if outcome == "blocked" {
				job.Status = "blocked"
				job.BlockerCode = "ciphertext_invalid"
			} else if outcome != "changed" {
				job.Cursor = row.id
			}
			return rootSaveLeasedJob(tx, &job, s.secretNow())
		})
		if domain == "egresses" {
			s.egressMu.Unlock()
		}
		if err != nil {
			return catalogError(err)
		}
		if outcome == "blocked" {
			return secretStoreUnavailable
		}
		completed++
		if outcome == "changed" {
			return nil
		}
	}
	return nil
}
