package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/id"
	"github.com/miclle/routex/pkg/secret"
	"gorm.io/gorm"
)

var rootRotationID = regexp.MustCompile(`^srt_[0-7][0-9a-hjkmnp-tv-z]{25}$`)
var rootKeyID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

func rootHash(value string) string { return secret.SHA256Hex(value) }
func rootReason(value string) bool {
	return value != "" && strings.TrimSpace(value) == value && utf8.ValidString(value) && utf8.RuneCountInString(value) <= 1000 && !strings.ContainsFunc(value, unicode.IsControl)
}
func rootInput(request, etag, reason string) bool {
	return credentialReplacementRequestID.MatchString(request) && personalModelETag(etag) && rootReason(reason)
}
func (s *Service) StartSecretRotation(ctx context.Context, actor, etag string, input SecretRotationStartInput) (*SecretRotationResult, error) {
	if !rootKeyID.MatchString(input.TargetKeyID) {
		return nil, apperrors.ErrBadRequest
	}
	return s.rootAction(ctx, actor, "", "start", etag, input.RequestID, input.TargetKeyID, input.Reason)
}
func (s *Service) SecretRotationAction(ctx context.Context, actor, jobID, action, etag string, input SecretRotationActionInput) (*SecretRotationResult, error) {
	if !rootRotationID.MatchString(jobID) || (action != "resume" && action != "retire" && action != "rollback") {
		return nil, apperrors.ErrBadRequest
	}
	return s.rootAction(ctx, actor, jobID, action, etag, input.RequestID, "", input.Reason)
}
func (s *Service) rootAction(ctx context.Context, actor, jobID, action, etag, request, target, reason string) (*SecretRotationResult, error) {
	if !rootInput(request, etag, reason) {
		return nil, apperrors.ErrBadRequest
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	s.rootMutation.Lock()
	defer s.rootMutation.Unlock()
	hash := rootHash(fmt.Sprintf("%q", []string{actor, jobID, action, etag, request, target, reason}))
	var receipt entity.SecretRotationReceipt
	created := false
	// Receipt reconciliation happens under current authority before all fresh-action
	// readiness/state gates. It can never replay a prior policy transition.
	replay := false
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := rootAuthorize(tx, actor, "secrets.rotate"); err != nil {
			return err
		}
		err := personalExact(tx, "request_id", request).Take(&receipt).Error
		if err == nil {
			if receipt.ActorID != actor || receipt.RequestHash != hash || receipt.Action != action {
				return catalogConflict
			}
			replay = true
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, catalogError(err)
	}
	if replay {
		var job entity.SecretRotationJob
		if err := personalExact(s.authDB(ctx), "id", receipt.JobID).Take(&job).Error; err != nil {
			return nil, catalogError(err)
		}
		if _, err := rootCountsChecked(job); err != nil {
			return nil, err
		}
		return s.rootMutationResult(ctx, receipt, false), nil
	}
	committed := false
	var drained *secretPolicyView
	gateHeld := false
	defer func() {
		if gateHeld {
			s.runtime.publication.Unlock()
		}
	}()
	if action == "retire" {
		if s.runtime == nil {
			return nil, secretStoreUnavailable
		}
		s.runtime.publication.Lock()
		gateHeld = true
		var job entity.SecretRotationJob
		if err := personalExact(s.authDB(ctx), "id", jobID).Take(&job).Error; err != nil {
			return nil, catalogError(err)
		}
		if _, err := rootCountsChecked(job); err != nil {
			return nil, err
		}
		if err := s.rootTargetSweep(ctx, job.TargetKeyID); err != nil {
			return nil, secretStoreUnavailable
		}
		drained = s.rootPolicy.Load()
		if err := s.drainSecretView(ctx, drained); err != nil {
			return nil, catalogError(err)
		}
		defer func() {
			if !committed && s.rootPolicy.Load() == drained {
				drained.closed.Store(false)
				s.rootReadersClosed.Store(false)
			}
		}()
	}
	err = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		if _, err := rootAuthorize(tx, actor, "secrets.rotate"); err != nil {
			return err
		}
		p, err := lockedSecretPolicy(tx)
		if err != nil {
			return err
		}
		if !p.Initialized || p.WriteKeyID == nil {
			return secretStoreUnavailable
		}
		// Competing global intent IDs cannot slip between preflight and commit.
		var existing entity.SecretRotationReceipt
		if e := personalExact(tx, "request_id", request).Take(&existing).Error; e == nil {
			return catalogConflict
		} else if !errors.Is(e, gorm.ErrRecordNotFound) {
			return e
		}
		var job entity.SecretRotationJob
		var reviewed *entity.SecretRotationJob
		if jobID != "" {
			if err := personalExact(tx, "id", jobID).Take(&job).Error; err != nil {
				return err
			}
			reviewed = &job
		} else {
			if err := tx.Order("created_at DESC,id DESC").Limit(1).Find(&job).Error; err != nil {
				return err
			}
			if job.ID != "" {
				reviewed = &job
			}
		}
		if reviewed != nil {
			if _, err := rootCountsChecked(*reviewed); err != nil {
				return err
			}
		}
		if rootReviewETag(actor, p, reviewed) != etag {
			return catalogConflict
		}
		proof, err := s.rootCurrentProofAdmission(tx, p, action == "retire")
		if err != nil {
			return secretStoreUnavailable
		}
		now := s.secretNow()
		if action != "resume" && p.Epoch == math.MaxUint64 {
			return catalogConflict
		}
		switch action {
		case "start":
			if p.ActiveJobID != nil || target == *p.WriteKeyID {
				return catalogConflict
			}
			var key entity.SecretRootKey
			if err := personalExact(tx, "key_id", target).Take(&key).Error; err != nil {
				return err
			}
			if key.State == "retired" {
				return catalogConflict
			}
			source := *p.WriteKeyID
			job, err = newRootJob(source, target, p.Epoch+1, now)
			if err != nil {
				return err
			}
			if err := tx.Create(&job).Error; err != nil {
				return err
			}
			if err := rootCutover(tx, &p, job, now); err != nil {
				return err
			}
			created = true
		case "resume":
			if p.ActiveJobID == nil || *p.ActiveJobID != job.ID || job.Status != "blocked" || p.Epoch != job.CutoverEpoch {
				return catalogConflict
			}
			job.InventoryVersion = 2
			job.Status = "migrating"
			job.Phase = "migration"
			job.Domain = 0
			job.Cursor = ""
			job.ScanGeneration++
			job.BlockerCode = ""
			job.ObservationStartedAt = nil
			job.ObservationLastConfirmedAt = nil
			job.LeaseUntil = nil
			job.LeaseToken = ""
			job.VerifiedProcessID = ""
			job.VerifiedSnapshotID = ""
		case "rollback":
			if p.ActiveJobID == nil || *p.ActiveJobID != job.ID || job.Status == "completed" || job.Status == "rolled_back" {
				return catalogConflict
			}
			var old entity.SecretRootKey
			if err := personalExact(tx, "key_id", job.SourceKeyID).Take(&old).Error; err != nil {
				return err
			}
			if old.State == "retired" {
				return catalogConflict
			}
			original := job
			original.Status = "rolled_back"
			original.Phase = "completed"
			original.CompletedAt = &now
			original.UpdatedAt = now
			original.ETag = rootJobETag(original)
			if err := tx.Save(&original).Error; err != nil {
				return err
			}
			job, err = newRootJob(*p.WriteKeyID, job.SourceKeyID, p.Epoch+1, now)
			if err != nil {
				return err
			}
			if err := tx.Create(&job).Error; err != nil {
				return err
			}
			if err := rootCutover(tx, &p, job, now); err != nil {
				return err
			}
		case "retire":
			if p.ActiveJobID == nil || *p.ActiveJobID != job.ID || job.Status != "ready" || p.Epoch != job.CutoverEpoch || !s.rootObservationEligible(job, proof) {
				return catalogConflict
			}
			if err := personalExact(tx.Model(&entity.SecretRootKey{}), "key_id", job.SourceKeyID).Updates(map[string]any{"state": "retired", "proof_ciphertext": "", "retired_at": now}).Error; err != nil {
				return err
			}
			p.Epoch++
			p.ActiveJobID = nil
			p.UpdatedAt = now
			p.ETag = rootHash(fmt.Sprintf("policy:%d:%s", p.Epoch, *p.WriteKeyID))
			if err := tx.Save(&p).Error; err != nil {
				return err
			}
			job.Status = "completed"
			job.Phase = "completed"
			job.CompletedAt = &now
		}
		job.UpdatedAt = now
		job.ETag = rootJobETag(job)
		if err := tx.Save(&job).Error; err != nil {
			return err
		}
		receipt = entity.SecretRotationReceipt{RequestID: request, ActorID: actor, JobID: job.ID, Action: action, RequestHash: hash, ReviewETag: etag, ResultEpoch: p.Epoch, ResultKeyID: *p.WriteKeyID, CreatedAt: now}
		if err := tx.Create(&receipt).Error; err != nil {
			return err
		}
		if err := appendRootRotationAudit(tx, actor, receipt, reason, job.Status); err != nil {
			return err
		}
		return personalExact(tx, "request_id", request).Take(&receipt).Error
	})
	if err != nil {
		return nil, catalogError(err)
	}
	committed = true
	if gateHeld {
		s.runtime.publication.Unlock()
		gateHeld = false
	}
	if err = s.loadSecretPolicy(ctx); err != nil {
		return s.rootMutationResult(ctx, receipt, created), nil
	}
	_ = s.RefreshRuntime(ctx)
	return s.rootMutationResult(ctx, receipt, created), nil
}
func newRootJob(source, target string, epoch uint64, now time.Time) (entity.SecretRotationJob, error) {
	jobID, err := id.NewPrefixed("srt")
	if err != nil {
		return entity.SecretRotationJob{}, err
	}
	job := entity.SecretRotationJob{ID: jobID, SourceKeyID: source, TargetKeyID: target, CutoverEpoch: epoch, Status: "migrating", Phase: "migration", InventoryVersion: 2, ScanGeneration: 1, CountsJSON: "{}", CreatedAt: now, UpdatedAt: now}
	job.ETag = rootJobETag(job)
	return job, nil
}
func rootCutover(tx *gorm.DB, p *entity.SecretWritePolicy, job entity.SecretRotationJob, now time.Time) error {
	p.Epoch = job.CutoverEpoch
	p.WriteKeyID = &job.TargetKeyID
	p.ActiveJobID = &job.ID
	p.UpdatedAt = now
	p.ETag = rootHash(fmt.Sprintf("policy:%d:%s", p.Epoch, job.TargetKeyID))
	if err := tx.Model(&entity.SecretRootKey{}).Where("state = ?", "write").Update("state", "decrypt_only").Error; err != nil {
		return err
	}
	if err := personalExact(tx.Model(&entity.SecretRootKey{}), "key_id", job.TargetKeyID).Update("state", "write").Error; err != nil {
		return err
	}
	return tx.Save(p).Error
}
func (s *Service) rootMutationResult(ctx context.Context, receipt entity.SecretRotationReceipt, created bool) *SecretRotationResult {
	result := &SecretRotationResult{Receipt: SecretRotationReceiptView{receipt.RequestID, receipt.JobID, receipt.Action, receipt.CreatedAt}, Committed: true, Created: created, ApplicationStatus: "unavailable"}
	var p entity.SecretWritePolicy
	if s.authDB(ctx).Take(&p, 1).Error != nil {
		return result
	}
	view := s.rootPolicy.Load()
	result.WritePolicyApplied = view != nil && !view.closed.Load() && p.Epoch == view.epoch && p.WriteKeyID != nil && *p.WriteKeyID == view.writeID
	if p.Epoch != receipt.ResultEpoch || p.WriteKeyID == nil || *p.WriteKeyID != receipt.ResultKeyID {
		result.ApplicationStatus = "superseded"
	} else {
		result.ApplicationStatus = "pending"
	}
	var job entity.SecretRotationJob
	if personalExact(s.authDB(ctx), "id", receipt.JobID).Take(&job).Error == nil {
		result.Rotation = rootRotationView(job, true, false)
	}
	if _, err := s.rootCurrentProof(s.authDB(ctx), p); err == nil {
		result.PublicationApplied = true
		if result.ApplicationStatus == "pending" && result.WritePolicyApplied {
			result.ApplicationStatus = "applied"
		}
	}
	return result
}
func appendRootRotationAudit(tx *gorm.DB, actor string, receipt entity.SecretRotationReceipt, reason, status string) error {
	raw, err := json.Marshal(rootRotationAudit{receipt.RequestID, receipt.JobID, receipt.Action, receipt.ResultKeyID, fmt.Sprint(receipt.ResultEpoch), reason, status})
	if err != nil {
		return err
	}
	auditID, err := id.NewPrefixed("aud")
	if err != nil {
		return err
	}
	details := string(raw)
	return tx.Create(&entity.AuditEvent{ID: auditID, ActorID: actor, Action: "secret_rotation." + receipt.Action, ResourceType: "secret_rotation", ResourceID: receipt.JobID, DetailsJSON: &details}).Error
}

type rootRotationAudit struct {
	RequestID  string `json:"request_id"`
	RotationID string `json:"rotation_id"`
	Action     string `json:"action"`
	KeyID      string `json:"key_id"`
	Epoch      string `json:"epoch"`
	Reason     string `json:"reason"`
	Status     string `json:"status"`
}
