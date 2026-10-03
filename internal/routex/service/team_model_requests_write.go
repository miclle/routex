package service

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/id"
)

func (s *Service) CreateTeamModelRequest(ctx context.Context, actorID, etag string, input TeamModelRequestInput) (*TeamModelRequestDetail, bool, error) {
	if !personalModelReason(input.Reason, false) {
		return nil, false, apperrors.ErrBadRequest
	}
	input.Reason = strings.TrimSpace(input.Reason)
	if !credentialReplacementRequestID.MatchString(input.RequestID) || !safeTeamSessionID(input.TeamID) || !safeTeamSessionID(input.ModelID) || !personalModelETag(etag) || !personalModelReason(input.Reason, true) {
		return nil, false, apperrors.ErrBadRequest
	}
	hash := teamModelCreationHash(actorID, etag, input)
	var saved entity.TeamModelRequest
	created := false
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		actor, err := exactEnabledActor(tx, actorID)
		if err != nil {
			return err
		}
		err = personalExact(tx, "request_id", input.RequestID).Take(&saved).Error
		if err == nil {
			if !teamModelKnownCreation(saved, actorID, hash, input) {
				return errTeamModelRequestConflict
			}
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		subject, err := loadTeamModelSubject(tx, actorID, input.TeamID, input.ModelID, true)
		if err != nil {
			return err
		}
		if !teamModelActiveMember(subject) {
			return apperrors.ErrForbidden
		}
		if !teamModelAvailable(subject) || subject.Grant != nil || subject.Pending != nil || teamModelCandidateETag(actorID, subject) != etag {
			return errTeamModelRequestConflict
		}
		requestID, err := id.NewPrefixed("tmr")
		if err != nil {
			return err
		}
		now := time.Now().UTC().Truncate(time.Microsecond)
		saved = entity.TeamModelRequest{ID: requestID, RequestID: input.RequestID, RequestHash: hash, TeamID: input.TeamID, TeamName: subject.Team.Name, ApplicantUserID: actor.ID, ApplicantName: actor.Name, ApplicantMembershipID: subject.Member.ID, ModelID: input.ModelID, ModelName: subject.Name.Name, ReviewETag: etag, Reason: input.Reason, Status: entity.TeamModelRequestPending, CreatedAt: now, UpdatedAt: now}
		if err := tx.Create(&saved).Error; err != nil {
			return err
		}
		if err := tx.Create(&entity.TeamModelRequestPendingSlot{TeamID: input.TeamID, ModelID: input.ModelID, RequestID: saved.ID, CreatedAt: now}).Error; err != nil {
			return err
		}
		if err := appendTeamModelRequestAudit(tx, actorID, "create", saved, input.Reason); err != nil {
			return err
		}
		created = true
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		err = errTeamModelRequestConflict
	}
	if err != nil {
		return nil, false, catalogError(err)
	}
	result, err := s.GetTeamModelRequest(ctx, actorID, "", saved.ID, false)
	return result, created, err
}

func teamModelKnownCreation(row entity.TeamModelRequest, actorID, hash string, input TeamModelRequestInput) bool {
	return row.RequestID == input.RequestID && row.TeamID == input.TeamID && row.ModelID == input.ModelID && row.ApplicantUserID == actorID && row.RequestHash == hash
}

func teamModelKnownDecision(row entity.TeamModelRequest, actorID, hash string, input TeamModelDecisionInput) bool {
	status := map[string]string{"approve": entity.TeamModelRequestApproved, "reject": entity.TeamModelRequestRejected, "withdraw": entity.TeamModelRequestWithdrawn}[input.Action]
	return status != "" && row.Status == status && row.DecisionAction == input.Action && row.DecisionReason == input.Reason && row.DecisionID != nil && *row.DecisionID == input.DecisionID && row.DecisionActorID != nil && *row.DecisionActorID == actorID && row.DecisionRequestHash == hash
}
func (s *Service) DecideTeamModelRequest(ctx context.Context, actorID, teamID, requestID, etag string, input TeamModelDecisionInput, reviewer bool) (*TeamModelDecisionRecord, error) {
	if !personalModelReason(input.Reason, false) {
		return nil, apperrors.ErrBadRequest
	}
	input.Reason = strings.TrimSpace(input.Reason)
	if !personalModelID(requestID, "tmr") || reviewer && !safeTeamSessionID(teamID) || !reviewer && teamID != "" || !personalModelETag(etag) || !credentialReplacementRequestID.MatchString(input.DecisionID) || !personalModelReason(input.Reason, input.Action == "reject") || reviewer && (input.Action != "approve" && input.Action != "reject") || !reviewer && (input.Action != "withdraw" || input.Reason != "") {
		return nil, apperrors.ErrBadRequest
	}
	hash := teamModelDecisionHash(actorID, requestID, etag, input)
	var saved entity.TeamModelRequest
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		actor, err := exactEnabledActor(tx, actorID)
		if err != nil {
			return err
		}
		query := personalExact(tx, "id", requestID)
		if reviewer {
			if _, err := teamModelReviewerAccess(tx, actorID, teamID); err != nil {
				return err
			}
			query = personalExact(query, "team_id", teamID)
		} else {
			query = personalExact(query, "applicant_user_id", actorID)
		}
		var original entity.TeamModelRequest
		if err := query.Take(&original).Error; err != nil {
			return err
		}
		if original.ID != requestID || reviewer && original.TeamID != teamID || !reviewer && original.ApplicantUserID != actorID {
			return apperrors.ErrNotFound
		}
		if reviewer && original.ApplicantUserID == actorID {
			return apperrors.ErrForbidden
		}
		// A reauthorized original receipt reconciles history before obsolete
		// membership or grant checks, and never restores a removed shared grant.
		if original.DecisionID != nil {
			if !teamModelKnownDecision(original, actorID, hash, input) {
				return errTeamModelRequestConflict
			}
			saved = original
			return nil
		}
		if original.Status != entity.TeamModelRequestPending {
			return errTeamModelRequestConflict
		}
		canRead, err := teamModelCurrentRead(tx, actorID, original.TeamID)
		if err != nil {
			return err
		}
		var subject *teamModelSubject
		if canRead {
			subject, err = loadTeamModelSubject(tx, original.ApplicantUserID, original.TeamID, original.ModelID, true)
			if err != nil {
				return err
			}
		}
		if err := personalExact(tx, "id", requestID).Clauses(clause.Locking{Strength: "UPDATE"}).Take(&saved).Error; err != nil {
			return err
		}
		if saved.Status != entity.TeamModelRequestPending || saved.DecisionID != nil || teamModelReviewETag(actorID, saved, subject) != etag || !slices.Contains(teamModelAllowed(actorID, saved, subject, reviewer), input.Action) {
			return errTeamModelRequestConflict
		}
		if input.Action == "approve" && subject.Grant == nil {
			source := saved.ID
			if err := tx.Create(&entity.TeamModelGrant{TeamID: saved.TeamID, ModelID: saved.ModelID, SourceRequestID: &source}).Error; err != nil {
				return err
			}
		}
		now := time.Now().UTC().Truncate(time.Microsecond)
		saved.DecisionID, saved.DecisionActorID, saved.DecisionActorName = &input.DecisionID, &actor.ID, &actor.Name
		saved.DecisionAction, saved.DecisionReason, saved.DecisionReviewETag, saved.DecisionRequestHash = input.Action, input.Reason, etag, hash
		saved.DecidedAt, saved.ResolvedAt, saved.UpdatedAt = &now, &now, now
		switch input.Action {
		case "approve":
			saved.Status = entity.TeamModelRequestApproved
		case "reject":
			saved.Status = entity.TeamModelRequestRejected
		case "withdraw":
			saved.Status = entity.TeamModelRequestWithdrawn
		}
		if err := tx.Save(&saved).Error; err != nil {
			return err
		}
		if err := personalExact(tx, "request_id", saved.ID).Delete(&entity.TeamModelRequestPendingSlot{}).Error; err != nil {
			return err
		}
		if err := appendTeamModelRequestAudit(tx, actorID, input.Action, saved, input.Reason); err != nil {
			return err
		}
		return personalExact(tx, "id", saved.ID).Take(&saved).Error
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		err = errTeamModelRequestConflict
	}
	if err != nil {
		return nil, catalogError(err)
	}
	if saved.Status == entity.TeamModelRequestApproved {
		_ = s.RefreshRuntime(ctx)
	}
	detail, err := s.GetTeamModelRequest(ctx, actorID, teamID, requestID, reviewer)
	if err != nil {
		return nil, err
	}
	return &TeamModelDecisionRecord{DecisionID: *saved.DecisionID, Committed: true, SavedRequest: teamModelRequestRecord(saved), CurrentGranted: detail.CurrentGranted, CurrentMembershipMatches: detail.CurrentMembershipMatches, RuntimeApplied: detail.RuntimeApplied, ApplicationStatus: detail.ApplicationStatus}, nil
}
