package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/id"
	"github.com/miclle/routex/pkg/limits"
)

func normalizeTeamQuotaRequest(input TeamQuotaRequestInput) (TeamQuotaRequestInput, error) {
	input.Reason = strings.TrimSpace(input.Reason)
	if !credentialReplacementRequestID.MatchString(input.RequestID) || !validTeamQuotaDimension(input.Dimension) || !teamQuotaReason(input.Reason, true) {
		return input, apperrors.ErrBadRequest
	}
	var err error
	input.TargetValue, err = canonicalTeamQuotaTarget(input.Dimension, input.TargetValue)
	return input, err
}
func teamQuotaCreationHash(actorID, teamID, etag string, input TeamQuotaRequestInput) (string, error) {
	return teamQuotaHash(struct {
		ActorID, TeamID, ETag string
		Input                 TeamQuotaRequestInput
	}{actorID, teamID, etag, input})
}
func (s *Service) CreateTeamQuotaRequest(ctx context.Context, actorID, teamID, etag string, input TeamQuotaRequestInput) (*TeamQuotaRequestDetail, bool, error) {
	if !safeTeamSessionID(teamID) || !teamSessionDigest.MatchString(etag) {
		return nil, false, apperrors.ErrBadRequest
	}
	input, err := normalizeTeamQuotaRequest(input)
	if err != nil {
		return nil, false, err
	}
	digest, err := teamQuotaCreationHash(actorID, teamID, etag, input)
	if err != nil {
		return nil, false, err
	}
	var savedID string
	created := false
	err = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		if _, err := exactEnabledActor(tx, actorID); err != nil {
			return err
		}
		current, err := loadTeamQuotaSubject(tx, teamID, actorID, true)
		if errors.Is(err, errTeamQuotaSubject) {
			return apperrors.ErrForbidden
		}
		if err != nil {
			return err
		}
		var existing entity.TeamQuotaRequest
		err = quotaExact(tx, "request_id", input.RequestID).First(&existing).Error
		if err == nil {
			if existing.RequestID != input.RequestID || existing.TeamID != teamID || existing.ApplicantUserID != actorID || existing.RequestHash != digest {
				return catalogConflict
			}
			savedID = existing.ID
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		context, err := s.teamQuotaContext(tx, current, input.Dimension)
		if err != nil {
			return err
		}
		if context.ETag != etag {
			return catalogConflict
		}
		if !teamQuotaGreater(input.TargetValue, context.MemberEffective) {
			return apperrors.ErrBadRequest
		}
		if !context.Eligible {
			if slices.Contains(context.Blockers, "currency_changed") {
				return catalogConflict
			}
			return runtimeUnavailable
		}
		var slot entity.TeamQuotaPendingSlot
		slotQuery := quotaExact(quotaExact(quotaExact(tx, "team_id", teamID), "applicant_user_id", actorID), "dimension", input.Dimension)
		err = slotQuery.First(&slot).Error
		if err == nil {
			var old entity.TeamQuotaRequest
			if err := quotaExact(tx, "id", slot.RequestID).First(&old).Error; err != nil {
				return err
			}
			if old.TeamID != teamID || old.ApplicantUserID != actorID || old.Dimension != input.Dimension {
				return apperrors.ErrInternal
			}
			if teamQuotaPending(old.Status) && old.ApplicantMembershipID != current.Member.ID {
				if err := cancelTeamQuotaRequest(tx, &old, "membership_replaced"); err != nil {
					return err
				}
			} else {
				return catalogConflict
			}
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		requestID, err := id.NewPrefixed("qrq")
		if err != nil {
			return err
		}
		stepID, err := id.NewPrefixed("qst")
		if err != nil {
			return err
		}
		snapshot, err := json.Marshal(context)
		if err != nil {
			return err
		}
		now := time.Now().UTC().Truncate(time.Microsecond)
		row := entity.TeamQuotaRequest{ID: requestID, RequestID: input.RequestID, RequestHash: digest, CreationReviewETag: etag, TeamID: teamID, ApplicantUserID: actorID, ApplicantMembershipID: current.Member.ID, TeamName: current.Team.Name, ApplicantName: current.User.Name, Dimension: input.Dimension, TargetValue: input.TargetValue, Reason: input.Reason, SubmittedSnapshotJSON: string(snapshot), Status: entity.TeamQuotaRequestPendingOwner, CurrentStepID: &stepID, CreatedAt: now, UpdatedAt: now}
		step := entity.TeamQuotaRequestStep{ID: stepID, RequestID: requestID, Ordinal: 1, Stage: entity.TeamQuotaStageOwner, Status: entity.TeamQuotaStepPending, EnteredAt: now}
		if input.Dimension == "money" {
			row.Currency = current.Pricing.PlatformCurrency
		}
		if len(current.Owners) == 0 {
			row.Status = entity.TeamQuotaRequestPendingAdmin
			row.EscalationReason = "no_eligible_owner"
			step.Stage = entity.TeamQuotaStageAdmin
		}
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		if err := tx.Create(&step).Error; err != nil {
			return err
		}
		slot = entity.TeamQuotaPendingSlot{TeamID: teamID, ApplicantUserID: actorID, Dimension: input.Dimension, RequestID: requestID, CreatedAt: now}
		if err := tx.Create(&slot).Error; err != nil {
			return err
		}
		if err := appendTeamQuotaRequestAudit(tx, actorID, "team.quota_request.create", row, step, input.Reason); err != nil {
			return err
		}
		savedID = requestID
		created = true
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, false, catalogError(err)
	}
	result, err := s.GetTeamQuotaRequest(ctx, actorID, savedID, false)
	return result, created, err
}
func teamQuotaStepAuthority(access *teamQuotaAccess, row entity.TeamQuotaRequest, step entity.TeamQuotaRequestStep, action string) bool {
	if action == "withdraw" {
		return row.ApplicantUserID == access.Actor.ID
	}
	if row.ApplicantUserID == access.Actor.ID {
		return false
	}
	if step.Stage == entity.TeamQuotaStageOwner {
		return slices.Contains(access.OwnedTeams, row.TeamID)
	}
	return step.Stage == entity.TeamQuotaStageAdmin && (row.Dimension == "tokens" && access.Tokens || row.Dimension == "money" && access.Money)
}
func teamQuotaDecisionHash(actorID, requestID, etag string, input TeamQuotaDecisionInput) (string, error) {
	return teamQuotaHash(struct {
		ActorID, RequestID, ETag string
		Input                    TeamQuotaDecisionInput
	}{actorID, requestID, etag, input})
}
func teamQuotaPatch(policy limits.Policy, dimension, target, currency string) (limits.Policy, error) {
	if dimension == "money" {
		value := target
		policy.MoneyMonth = &value
		policy.Currency = currency
	} else {
		value, err := canonicalTeamQuotaTarget(dimension, target)
		if err != nil {
			return policy, err
		}
		number, err := strconvTeamQuotaInteger(value)
		if err != nil {
			return policy, err
		}
		policy.TokensMonth = &number
	}
	return limits.Normalize(policy)
}
func strconvTeamQuotaInteger(value string) (int64, error) {
	number, err := strconv.ParseInt(value, 10, 64)
	return number, err
}

func (s *Service) DecideTeamQuotaRequest(ctx context.Context, actorID, requestID, etag string, input TeamQuotaDecisionInput) (*TeamQuotaDecisionRecord, error) {
	input.Reason = strings.TrimSpace(input.Reason)
	if !safeTeamSessionID(requestID) || !safeTeamSessionID(input.StepID) || !teamSessionDigest.MatchString(etag) || !credentialReplacementRequestID.MatchString(input.DecisionID) || !slices.Contains([]string{"approve", "reject", "withdraw"}, input.Action) || !teamQuotaReason(input.Reason, input.Action == "reject") {
		return nil, apperrors.ErrBadRequest
	}
	digest, err := teamQuotaDecisionHash(actorID, requestID, etag, input)
	if err != nil {
		return nil, err
	}
	s.limitMu.Lock()
	defer s.limitMu.Unlock()
	var saved entity.TeamQuotaRequestStep
	var teamID, userID string
	freshPolicy, parentChanged, cancelled := false, false, false
	err = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		access, err := teamQuotaActorAccess(tx, actorID)
		if err != nil {
			return err
		}
		// The path is scoped before a row can disclose its Team, applicant or stage.
		var row entity.TeamQuotaRequest
		if err := quotaExact(teamQuotaVisible(tx.Model(&entity.TeamQuotaRequest{}), access, "detail", false), "id", requestID).First(&row).Error; err != nil {
			return err
		}
		if row.ID != requestID {
			return apperrors.ErrNotFound
		}
		teamID, userID = row.TeamID, row.ApplicantUserID
		// Lock only the stable Team identity before the request. Current lifecycle,
		// policy and currency validators are irrelevant to an immutable receipt.
		var teams []entity.Team
		if err := quotaExact(tx, "id", row.TeamID).Select("id").Clauses(clause.Locking{Strength: "UPDATE"}).Find(&teams).Error; err != nil {
			return err
		}
		if err := quotaExact(tx, "id", requestID).Clauses(clause.Locking{Strength: "UPDATE"}).First(&row).Error; err != nil {
			return err
		}
		var step entity.TeamQuotaRequestStep
		if err := quotaExact(quotaExact(tx, "request_id", requestID), "id", input.StepID).Clauses(clause.Locking{Strength: "UPDATE"}).First(&step).Error; err != nil {
			return err
		}
		if step.ID != input.StepID || step.RequestID != requestID {
			return catalogConflict
		}
		if !teamQuotaStepAuthority(access, row, step, input.Action) {
			return apperrors.ErrForbidden
		}
		// Reauthorize first; immutable receipt reconciliation precedes obsolete
		// relationship, current review or later workflow-stage validation.
		var receipt entity.TeamQuotaRequestStep
		receiptErr := quotaExact(tx, "decision_id", input.DecisionID).First(&receipt).Error
		if receiptErr == nil {
			if receipt.DecisionID == nil || *receipt.DecisionID != input.DecisionID || receipt.ID != input.StepID || receipt.RequestID != requestID || receipt.DecisionHash != digest || receipt.ActorID != actorID || receipt.Action != input.Action || receipt.ReviewETag != etag {
				return catalogConflict
			}
			saved = receipt
			return nil
		}
		if !errors.Is(receiptErr, gorm.ErrRecordNotFound) {
			return receiptErr
		}
		if !teamQuotaCurrentStep(row, step) {
			return catalogConflict
		}
		current, subjectErr := loadTeamQuotaSubject(tx, row.TeamID, row.ApplicantUserID, false)
		if subjectErr != nil && !errors.Is(subjectErr, errTeamQuotaSubject) {
			return subjectErr
		}
		if current == nil || current.Member.ID != row.ApplicantMembershipID {
			if err := cancelTeamQuotaRequest(tx, &row, "membership_unavailable"); err != nil {
				return err
			}
			cancelled = true
			return nil
		}
		detail, err := s.teamQuotaDetail(tx, access, row, false)
		if err != nil {
			return err
		}
		if detail.ETag != etag {
			return catalogConflict
		}
		if input.Action == "approve" {
			if detail.CurrentContext == nil || !detail.CurrentContext.Eligible {
				return runtimeUnavailable
			}
			if row.Dimension == "money" && row.Currency != current.Pricing.PlatformCurrency {
				return catalogConflict
			}
			if !teamQuotaGreater(row.TargetValue, detail.CurrentContext.MemberEffective) {
				return catalogConflict
			}
			overflow := teamQuotaGreater(row.TargetValue, detail.CurrentContext.TeamEffective)
			if step.Stage == entity.TeamQuotaStageOwner && overflow {
				nextID, err := id.NewPrefixed("qst")
				if err != nil {
					return err
				}
				now := time.Now().UTC().Truncate(time.Microsecond)
				next := entity.TeamQuotaRequestStep{ID: nextID, RequestID: row.ID, Ordinal: 2, Stage: entity.TeamQuotaStageAdmin, Status: entity.TeamQuotaStepPending, EnteredAt: now}
				if err := tx.Create(&next).Error; err != nil {
					return err
				}
				row.Status = entity.TeamQuotaRequestPendingAdmin
				row.CurrentStepID = &nextID
				row.EscalationReason = "target_exceeds_team"
			} else {
				beforeParent := current.TeamPolicy
				beforeChild := current.MemberPolicy
				parent := beforeParent
				child, err := teamQuotaPatch(beforeChild, row.Dimension, row.TargetValue, row.Currency)
				if err != nil {
					return err
				}
				if overflow {
					if step.Stage != entity.TeamQuotaStageAdmin {
						return apperrors.ErrForbidden
					}
					parent, err = teamQuotaPatch(beforeParent, row.Dimension, row.TargetValue, row.Currency)
					if err != nil {
						return err
					}
					current.TeamRow, err = persistResourceLimitPolicy(tx, actorID, resolvedLimitTarget{kind: "team", id: row.TeamID, teamID: row.TeamID}, current.TeamRow, beforeParent, parent, input.Reason)
					if err != nil {
						return err
					}
					parentChanged = true
				}
				if !teamQuotaDimensionWithinParent(row.Dimension, parent, child) {
					return catalogConflict
				}
				for _, policy := range []limits.Policy{parent, child} {
					if policy.MoneyMonth != nil && policy.Currency != current.Pricing.PlatformCurrency {
						return catalogConflict
					}
				}
				current.MemberRow, err = persistResourceLimitPolicy(tx, actorID, resolvedLimitTarget{kind: "team_member", id: teamMemberLimitScopeID(row.TeamID, row.ApplicantUserID), teamID: row.TeamID, userID: row.ApplicantUserID}, current.MemberRow, beforeChild, child, input.Reason)
				if err != nil {
					return err
				}
				parentJSON, err := json.Marshal(parent)
				if err != nil {
					return err
				}
				childJSON, err := json.Marshal(child)
				if err != nil {
					return err
				}
				row.ApprovedTeamPolicyETag = current.TeamRow.ETag
				row.ApprovedMemberPolicyETag = current.MemberRow.ETag
				row.ApprovedTeamPolicyJSON = string(parentJSON)
				row.ApprovedMemberPolicyJSON = string(childJSON)
				row.Status = entity.TeamQuotaRequestApproved
				row.CurrentStepID = nil
				freshPolicy = true
			}
		} else {
			row.CurrentStepID = nil
			if input.Action == "reject" {
				row.Status = entity.TeamQuotaRequestRejected
			} else {
				row.Status = entity.TeamQuotaRequestWithdrawn
			}
		}
		now := time.Now().UTC().Truncate(time.Microsecond)
		step.DecisionID = &input.DecisionID
		step.DecisionHash = digest
		step.ReviewETag = etag
		step.ActorID = actorID
		step.ActorName = access.Actor.Name
		step.Action = input.Action
		step.Reason = input.Reason
		step.DecidedAt = &now
		switch input.Action {
		case "approve":
			step.Status = entity.TeamQuotaStepApproved
		case "reject":
			step.Status = entity.TeamQuotaStepRejected
		case "withdraw":
			step.Status = entity.TeamQuotaStepWithdrawn
		}
		if err := tx.Save(&step).Error; err != nil {
			return err
		}
		row.UpdatedAt = now
		if !teamQuotaPending(row.Status) {
			row.ResolvedAt = &now
			if err := quotaExact(tx, "request_id", row.ID).Delete(&entity.TeamQuotaPendingSlot{}).Error; err != nil {
				return err
			}
		}
		if err := tx.Save(&row).Error; err != nil {
			return err
		}
		if err := appendTeamQuotaRequestAudit(tx, actorID, "team.quota_request."+input.Action, row, step, input.Reason); err != nil {
			return err
		}
		saved = step
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, catalogError(err)
	}
	if cancelled {
		return nil, catalogConflict
	}
	if freshPolicy {
		s.denyLimitScope("team_member", teamMemberLimitScopeID(teamID, userID))
		if parentChanged {
			s.denyLimitScope("team", teamID)
		}
	}
	// A known saved receipt is independent of local publication success. Refresh
	// only the current database; exact historical retries never restore policies.
	if input.Action == "approve" {
		_ = s.RefreshRuntime(ctx)
	}
	detail, err := s.GetTeamQuotaRequest(ctx, actorID, requestID, false)
	if err != nil {
		return nil, err
	}
	return &TeamQuotaDecisionRecord{DecisionID: input.DecisionID, StepID: input.StepID, Action: input.Action, Committed: true, SavedStep: teamQuotaStepRecord(saved), Request: detail}, nil
}
