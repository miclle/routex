package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/id"
)

func appendPersonalModelAudit(tx *gorm.DB, actorID, action string, row entity.PersonalModelRequest, reason string) error {
	encoded, err := json.Marshal(struct {
		ApplicantUserID string  `json:"applicant_user_id"`
		ModelID         string  `json:"model_id"`
		ModelName       string  `json:"model_name"`
		Status          string  `json:"request_status"`
		DecisionID      *string `json:"decision_id"`
		Action          string  `json:"action"`
		Reason          string  `json:"reason"`
	}{row.ApplicantUserID, row.ModelID, row.ModelName, row.Status, row.DecisionID, action, reason})
	if err != nil {
		return err
	}
	eventID, err := id.NewPrefixed("aud")
	if err != nil {
		return err
	}
	details := string(encoded)
	return tx.Create(&entity.AuditEvent{ID: eventID, ActorID: actorID, Action: "personal.model_request." + action, ResourceType: "personal_model_request", ResourceID: row.ID, DetailsJSON: &details}).Error
}
func (s *Service) CreatePersonalModelRequest(ctx context.Context, actorID, etag string, input PersonalModelRequestInput) (*PersonalModelRequestDetail, bool, error) {
	if !personalModelReason(input.Reason, false) {
		return nil, false, apperrors.ErrBadRequest
	}
	input.Reason = strings.TrimSpace(input.Reason)
	if !credentialReplacementRequestID.MatchString(input.RequestID) || !personalModelID(input.ModelID, "mdl") || !personalModelETag(etag) || !personalModelReason(input.Reason, true) {
		return nil, false, apperrors.ErrBadRequest
	}
	hash := personalCreationHash(actorID, etag, input)
	var row entity.PersonalModelRequest
	created := false
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		actor, err := exactEnabledActor(tx, actorID)
		if err != nil {
			return err
		}
		err = personalExact(tx, "request_id", input.RequestID).Take(&row).Error
		if err == nil {
			if row.RequestID != input.RequestID || row.ApplicantUserID != actorID || row.RequestHash != hash {
				return errPersonalModelConflict
			}
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		subject, err := loadPersonalModelSubject(tx, actorID, input.ModelID, true)
		if err != nil {
			return err
		}
		if !personalModelSubjectAvailable(subject) || subject.Grant != nil || subject.Pending != nil || personalModelCandidateETag(subject) != etag {
			return errPersonalModelConflict
		}
		requestID, err := id.NewPrefixed("mar")
		if err != nil {
			return err
		}
		now := time.Now().UTC().Truncate(time.Microsecond)
		row = entity.PersonalModelRequest{ID: requestID, RequestID: input.RequestID, RequestHash: hash, ApplicantUserID: actorID, ApplicantName: actor.Name, ModelID: input.ModelID, ModelName: subject.Name.Name, ReviewETag: etag, Reason: input.Reason, Status: entity.PersonalModelRequestPending, CreatedAt: now, UpdatedAt: now}
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		if err := tx.Create(&entity.PersonalModelRequestPendingSlot{ApplicantUserID: actorID, ModelID: input.ModelID, RequestID: row.ID, CreatedAt: now}).Error; err != nil {
			return err
		}
		if err := appendPersonalModelAudit(tx, actorID, "create", row, input.Reason); err != nil {
			return err
		}
		created = true
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		err = errPersonalModelConflict
	}
	if err != nil {
		return nil, false, catalogError(err)
	}
	result, err := s.GetPersonalModelRequest(ctx, actorID, actorID, row.ID, false)
	return result, created, err
}
func (s *Service) DecidePersonalModelRequest(ctx context.Context, actorID, userID, requestID, etag string, input PersonalModelDecisionInput, reviewer bool) (*PersonalModelDecisionRecord, error) {
	if !personalModelReason(input.Reason, false) {
		return nil, apperrors.ErrBadRequest
	}
	input.Reason = strings.TrimSpace(input.Reason)
	if !personalModelID(userID, "usr") || !personalModelID(requestID, "mar") || !personalModelETag(etag) || !credentialReplacementRequestID.MatchString(input.DecisionID) || !personalModelReason(input.Reason, input.Action == "reject") || reviewer && (input.Action != "approve" && input.Action != "reject") || !reviewer && (input.Action != "withdraw" || input.Reason != "") {
		return nil, apperrors.ErrBadRequest
	}
	release := s.pinPersonalKeyMutation()
	defer release()
	hash := personalDecisionHash(actorID, requestID, etag, input)
	var saved entity.PersonalModelRequest
	approved := false
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		actor, err := personalModelRequestAccess(tx, actorID, userID, reviewer)
		if err != nil {
			return err
		}
		if reviewer && actorID == userID {
			return apperrors.ErrForbidden
		}
		var original entity.PersonalModelRequest
		if err := personalExact(personalExact(tx, "applicant_user_id", userID), "id", requestID).Take(&original).Error; err != nil {
			return err
		}
		if original.ID != requestID || original.ApplicantUserID != userID {
			return apperrors.ErrNotFound
		}
		// Immutable receipt reconciliation precedes obsolete current model, grant,
		// review and lifecycle checks. It never inserts or restores a grant.
		if original.DecisionID != nil {
			if *original.DecisionID != input.DecisionID || original.DecisionActorID == nil || *original.DecisionActorID != actorID || original.DecisionRequestHash != hash {
				return errPersonalModelConflict
			}
			saved = original
			return nil
		}
		if original.Status != entity.PersonalModelRequestPending {
			return errPersonalModelConflict
		}
		subject, err := loadPersonalModelSubject(tx, userID, original.ModelID, true)
		if err != nil {
			return err
		}
		if subject.User.Disabled || subject.User.OffboardedAt != nil || input.Action == "approve" && !personalModelSubjectAvailable(subject) {
			return errPersonalModelConflict
		}
		if err := personalExact(personalExact(tx, "id", requestID), "applicant_user_id", userID).Clauses(clause.Locking{Strength: "UPDATE"}).Take(&saved).Error; err != nil {
			return err
		}
		if saved.Status != entity.PersonalModelRequestPending || saved.DecisionID != nil || personalModelReviewETag(actorID, saved, subject) != etag {
			return errPersonalModelConflict
		}
		if subject.Pending == nil || subject.Pending.RequestID != saved.ID {
			return errPersonalModelConflict
		}
		if input.Action == "approve" && subject.Grant == nil {
			source := saved.ID
			grant := entity.UserModelGrant{UserID: userID, ModelID: saved.ModelID, SourceRequestID: &source, CreatedAt: time.Now().UTC().Truncate(time.Microsecond)}
			if err := tx.Create(&grant).Error; err != nil {
				return err
			}
			if err := advancePersonalGrantRevision(modelCreationDB(tx), userID); err != nil {
				return err
			}
			approved = true
		}
		now := time.Now().UTC().Truncate(time.Microsecond)
		saved.DecisionID = &input.DecisionID
		saved.DecisionActorID = &actor.ID
		saved.DecisionActorName = &actor.Name
		saved.DecisionAction = input.Action
		saved.DecisionReason = input.Reason
		saved.DecisionReviewETag = etag
		saved.DecisionRequestHash = hash
		saved.DecidedAt = &now
		saved.ResolvedAt = &now
		saved.UpdatedAt = now
		switch input.Action {
		case "approve":
			saved.Status = entity.PersonalModelRequestApproved
		case "reject":
			saved.Status = entity.PersonalModelRequestRejected
		case "withdraw":
			saved.Status = entity.PersonalModelRequestWithdrawn
		}
		if err := tx.Save(&saved).Error; err != nil {
			return err
		}
		if err := personalExact(tx, "request_id", saved.ID).Delete(&entity.PersonalModelRequestPendingSlot{}).Error; err != nil {
			return err
		}
		if err := appendPersonalModelAudit(tx, actorID, input.Action, saved, input.Reason); err != nil {
			return err
		}
		// Read the persisted precision after GORM's UpdatedAt handling; receipts and
		// future reviewed validators must hash the actual saved representation.
		return personalExact(tx, "id", saved.ID).Take(&saved).Error
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	release()
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		err = errPersonalModelConflict
	}
	if err != nil {
		return nil, catalogError(err)
	}
	if approved || saved.Status == entity.PersonalModelRequestApproved {
		// A known durable approval is returned separately from current publication.
		// Publication failure keeps the exact receipt available for safe retries.
		_ = s.RefreshRuntime(ctx)
	}
	detail, err := s.GetPersonalModelRequest(ctx, actorID, userID, requestID, reviewer)
	if err != nil {
		return nil, err
	}
	return &PersonalModelDecisionRecord{DecisionID: *saved.DecisionID, Committed: true, SavedRequest: personalModelRecord(saved), CurrentGranted: detail.CurrentGranted, RuntimeApplied: detail.RuntimeApplied, ApplicationStatus: detail.ApplicationStatus}, nil
}
