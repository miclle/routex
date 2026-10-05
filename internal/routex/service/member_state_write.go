package service

import (
	"context"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"gorm.io/gorm"
)

type memberStateMutation struct {
	changed, invalidate bool
	userID              string
}

func memberStateDesired(target entity.User, input MemberStateInput) (entity.User, bool) {
	next := target
	if input.Role != nil {
		next.Role = *input.Role
	}
	if input.Disabled != nil {
		next.Disabled = *input.Disabled
		if !next.Disabled && target.Disabled && target.OffboardedAt != nil {
			next.OffboardedAt = nil
		}
	}
	return next, next.Role != target.Role || next.Disabled != target.Disabled || (next.OffboardedAt == nil) != (target.OffboardedAt == nil)
}
func memberStateAuthorize(actor, target entity.User, input MemberStateInput) error {
	if input.Role != nil && actor.Role != entity.RoleAdmin {
		return apperrors.ErrForbidden
	}
	if actor.Role != entity.RoleAdmin && (actor.ID == target.ID || target.Role == entity.RoleAdmin) {
		return apperrors.ErrForbidden
	}
	return nil
}
func (s *Service) mutateMemberState(ctx context.Context, actorID, userID, etag string, input MemberStateInput, reviewed bool) (memberStateMutation, error) {
	var mutation memberStateMutation
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(memberRolesDB(tx)); err != nil {
			return err
		}
		actor, target, _, err := memberStatePeople(tx, actorID, userID, true, true)
		if err != nil {
			return err
		}
		if err := memberStateAuthorize(actor, target, input); err != nil {
			return err
		}
		next, changed := memberStateDesired(target, input)
		mutation.userID = target.ID
		if !changed {
			return nil
		}
		if reviewed && memberStateRecord(actor, target, true).ETag != etag {
			return catalogConflict
		}
		// Governance serializes every admin reduction. Count exact current intrinsic
		// identities only; disabled/offboarded or collating role aliases cannot save it.
		apps, err := loadRegistrationApplications(tx, []entity.User{target})
		if err != nil {
			return err
		}
		admission, _ := registrationAdmission(target, apps)
		if admission.AdmissionEligible && target.Role == entity.RoleAdmin && !target.Disabled && target.OffboardedAt == nil && (next.Disabled || next.Role != entity.RoleAdmin) {
			count, err := admittedAdministratorCount(tx)
			if err != nil {
				return err
			}
			if count <= 1 {
				return catalogConflict
			}
		}
		if next.Disabled && !target.Disabled {
			if err := validateResourceContinuity(memberRolesDB(tx), target.ID); err != nil {
				return err
			}
		}
		updates := map[string]any{"Role": next.Role, "Disabled": next.Disabled}
		if target.OffboardedAt != nil && next.OffboardedAt == nil {
			updates["OffboardedAt"] = nil
		}
		if err := memberRolesExact(memberRolesDB(tx).Model(&entity.User{}), "id", target.ID).Updates(updates).Error; err != nil {
			return err
		}
		if err := advanceMemberRoleRevision(tx, target.ID); err != nil {
			return err
		}
		if next.Disabled {
			if err := CancelTeamModelRequestsForUser(memberRolesDB(tx), actor.ID, target.ID, "applicant_unavailable"); err != nil {
				return err
			}
			if err := CancelPersonalModelRequestsForUser(memberRolesDB(tx), actor.ID, target.ID, "applicant_unavailable"); err != nil {
				return err
			}
			if err := cancelTeamQuotaRequestsForUser(memberRolesDB(tx), target.ID, "applicant_unavailable"); err != nil {
				return err
			}
			if err := invalidateMFAChallenges(memberRolesDB(tx), target.ID); err != nil {
				return err
			}
			if err := memberRolesExact(memberRolesDB(tx), "user_id", target.ID).Delete(&entity.Session{}).Error; err != nil {
				return err
			}
			if err := revokeOwnerPersonalKeys(memberRolesDB(tx), target.ID); err != nil {
				return err
			}
		}
		if reviewed {
			err = appendMemberStateAudit(memberRolesDB(tx), actor.ID, target, next, input)
		} else {
			action := "member.update"
			if target.OffboardedAt != nil && next.OffboardedAt == nil {
				action = "member.reactivate"
			}
			err = appendAudit(memberRolesDB(tx), actor.ID, action, "user", target.ID)
		}
		if err != nil {
			return err
		}
		mutation.changed = true
		mutation.invalidate = next.Disabled || target.Role == entity.RoleAdmin && next.Role != entity.RoleAdmin
		return nil
	})
	return mutation, memberStateError(err)
}
func (s *Service) SetReviewedMemberState(ctx context.Context, actorID, userID, etag string, input MemberStateInput) (*MemberStateResult, error) {
	if err := memberMetadataIDs(actorID, userID); err != nil {
		return nil, err
	}
	if !validMemberStateInput(input) || !validMemberRoleDigest(etag) {
		return nil, apperrors.ErrBadRequest
	}
	release := s.pinPersonalKeyMutation()
	defer release()
	mutation, err := s.mutateMemberState(ctx, actorID, userID, etag, input, true)
	if err != nil {
		return nil, err
	}
	if mutation.invalidate {
		s.InvalidateRuntimeUser(mutation.userID)
	}
	release()
	// One publication attempt after commit (or exact-authorized no-op). This never
	// replays the transaction, audit, revocations or reduction tombstone.
	if err := s.RefreshRuntime(ctx); err != nil {
		return nil, memberStateError(err)
	}
	return s.confirmMemberState(ctx, actorID, userID, input)
}
func (s *Service) confirmMemberState(ctx context.Context, actorID, userID string, input MemberStateInput) (*MemberStateResult, error) {
	release := s.pinPersonalKeyMutation()
	defer release()
	var result MemberStateResult
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(memberRolesDB(tx)); err != nil {
			return err
		}
		actor, target, write, err := memberStatePeople(tx, actorID, userID, true, true)
		if err != nil {
			return err
		}
		if err := memberStateAuthorize(actor, target, input); err != nil {
			return err
		}
		if _, changed := memberStateDesired(target, input); changed {
			return catalogConflict
		}
		result.MemberStateRecord = memberStateRecord(actor, target, write)
		result.AccountAccessRuntimeApplied = s.memberStateRuntimeApplied(ctx, target)
		result.Confirmation = "current_member_state"
		result.Effect = "current_base_identity"
		if input.Disabled != nil {
			result.Effect = "current_account_access"
			if !result.AccountAccessRuntimeApplied {
				return memberStateUnavailable
			}
		}
		return nil
	})
	if err != nil {
		return nil, memberStateError(err)
	}
	return &result, nil
}

// The internal adapter shares all mutation/revision/revocation guards. Its old
// record and unspecified audit reason are preserved; it is not an HTTP bypass.
func (s *Service) updateMemberLegacy(ctx context.Context, actorID, userID string, disabled *bool, role *string) (*MemberRecord, error) {
	if err := memberMetadataIDs(actorID, userID); err != nil {
		return nil, err
	}
	if disabled == nil && role == nil || role != nil && *role != entity.RoleAdmin && *role != entity.RoleMember {
		return nil, apperrors.ErrBadRequest
	}
	release := s.pinPersonalKeyMutation()
	defer release()
	mutation, err := s.mutateMemberState(ctx, actorID, userID, "", MemberStateInput{Role: role, Disabled: disabled}, false)
	if err != nil {
		return nil, err
	}
	if mutation.invalidate {
		s.InvalidateRuntimeUser(mutation.userID)
	}
	release()
	if err := s.refreshAfterMutation(ctx, nil); err != nil {
		return nil, err
	}
	result, err := memberRecord(s.authDB(ctx), userID)
	return result, catalogError(err)
}
