package service

import (
	"context"
	"database/sql"
	"slices"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func replaceMemberRoleRows(tx *gorm.DB, userID string, before, after []string) error {
	for _, id := range before {
		if !slices.Contains(after, id) {
			if err := memberRolesExact(memberRolesExact(memberRolesDB(tx), "user_id", userID), "role_id", id).Delete(&entity.UserRole{}).Error; err != nil {
				return err
			}
		}
	}
	for _, id := range after {
		if !slices.Contains(before, id) {
			if err := memberRolesDB(tx).Create(&entity.UserRole{UserID: userID, RoleID: id}).Error; err != nil {
				return err
			}
		}
	}
	return advanceMemberRoleRevision(tx, userID)
}
func lockMemberRoleDefinitions(tx *gorm.DB, snapshot *memberRolesSnapshot) error {
	ids := []string{}
	for id := range snapshot.Definitions {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for start := 0; start < len(ids); start += memberRolesBatchSize {
		end := min(start+memberRolesBatchSize, len(ids))
		var rows []entity.Role
		if err := memberRoleIDsQuery(memberRolesDB(tx).Model(&entity.Role{}), "id", ids[start:end]).Order("id").Clauses(clause.Locking{Strength: "UPDATE"}).Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) != end-start {
			return catalogConflict
		}
	}
	return nil
}
func (s *Service) SetReviewedMemberRoles(ctx context.Context, actorID, userID, etag string, input MemberRolesInput) (*MemberRolesResult, error) {
	if err := memberMetadataIDs(actorID, userID); err != nil {
		return nil, err
	}
	if !validMemberRoleDigest(etag) {
		return nil, apperrors.ErrBadRequest
	}
	if err := validateMemberRolesInput(input); err != nil {
		return nil, err
	}
	return s.setMemberRolesReviewed(ctx, actorID, userID, etag, input, false)
}
func (s *Service) setMemberRolesReviewed(ctx context.Context, actorID, userID, etag string, input MemberRolesInput, legacy bool) (*MemberRolesResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		actor, subject, err := memberRolesPeople(tx, actorID, userID, false, true)
		if err != nil {
			return err
		}
		before, err := loadMemberRoles(tx, actor, subject)
		if err != nil {
			return err
		}
		if legacy {
			input.BuiltinDefinitionETag = before.Page.BuiltinRole.DefinitionETag
			input.RoleDefinitions = []MemberRoleDefinitionProof{}
			for _, id := range input.RoleIDs {
				d, ok := before.Definitions[id]
				if !ok || d.Summary.AssignmentKind != RoleAssignmentExplicit {
					return apperrors.ErrBadRequest
				}
				input.RoleDefinitions = append(input.RoleDefinitions, MemberRoleDefinitionProof{id, d.Summary.DefinitionETag})
			}
			etag = before.Page.ETag
		}
		if err := memberRolesReview(before, etag, input); err != nil {
			return err
		}
		if slices.Equal(before.Assigned, input.RoleIDs) {
			return nil
		}
		if err := lockMemberRoleDefinitions(tx, before); err != nil {
			return err
		}
		// Size the complete safe audit before any delete/create or revision write.
		afterValues := memberRolesAuditValues{subject.Role, slices.Clone(input.RoleIDs), memberRolesUnion(before.Definitions, append(slices.Clone(input.RoleIDs), before.Page.BuiltinRole.ID))}
		raw, err := encodeMemberRolesAudit(userID, input.Reason, memberRolesAuditValuesFor(before), afterValues)
		if err != nil {
			return err
		}
		if err := replaceMemberRoleRows(tx, userID, before.Assigned, input.RoleIDs); err != nil {
			return err
		}
		if err := memberRolesUserQuery(tx, userID, false).First(&subject).Error; err != nil {
			return err
		}
		after, err := loadMemberRoles(tx, actor, subject)
		if err != nil {
			return err
		}
		if !slices.Equal(after.Assigned, input.RoleIDs) {
			return memberRolesUnavailable
		}
		if err := appendMemberRolesAudit(tx, actorID, userID, raw); err != nil {
			return err
		}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, catalogError(err)
	}
	// The committed transaction is not current-effect confirmation. Re-read under
	// fresh independent admin authority; errors retain uncertainty without replay.
	return s.confirmMemberRoles(ctx, actorID, userID, input)
}
func (s *Service) confirmMemberRoles(ctx context.Context, actorID, userID string, input MemberRolesInput) (*MemberRolesResult, error) {
	var result *MemberRolesResult
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		actor, subject, err := memberRolesPeople(tx, actorID, userID, false, false)
		if err != nil {
			return err
		}
		current, err := loadMemberRoles(tx, actor, subject)
		if err != nil {
			return err
		}
		if !slices.Equal(current.Assigned, input.RoleIDs) {
			return memberRolesUnavailable
		}
		if err := memberRolesReview(current, current.Page.ETag, input); err != nil {
			return err
		}
		result = memberRolesResult(current)
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		switch err {
		case apperrors.ErrUnauthorized, apperrors.ErrForbidden, apperrors.ErrNotFound, catalogConflict, memberRolesOverflow:
			return nil, err
		}
		return nil, memberRolesUnavailable
	}
	return result, nil
}
