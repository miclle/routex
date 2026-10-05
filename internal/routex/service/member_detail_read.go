package service

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type MemberDetailRecord struct {
	MemberRecord
	LastLoginAt          *time.Time
	RegistrationApproval RegistrationApprovalSummary
	LastLoginStatus      string
}

// GET-only projection: mutation/create records retain their existing shape.
func (s *Service) GetMemberDetail(ctx context.Context, actorID, userID string) (*MemberDetailRecord, error) {
	if !safeTeamSessionID(actorID) {
		return nil, apperrors.ErrUnauthorized
	}
	if !safeTeamSessionID(userID) {
		return nil, apperrors.ErrBadRequest
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var result *MemberDetailRecord
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		var actor entity.User
		err := modelCreationDB(tx).Select("ID", "Role", "Disabled", "OffboardedAt", "CreatedAt", "ApprovalApplicationID").Where(database.ExactText(tx, clause.Column{Name: "id"}, actorID)).Where("disabled = ? AND offboarded_at IS NULL", false).Take(&actor).Error
		if errors.Is(err, gorm.ErrRecordNotFound) || err == nil && (actor.ID != actorID || actor.Role != entity.RoleAdmin && actor.Role != entity.RoleMember) {
			return apperrors.ErrUnauthorized
		}
		if err != nil {
			return err
		}
		if err := requireRegistrationAdmission(modelCreationDB(tx), actor); err != nil {
			return err
		}
		read, err := exactGovernancePermissionForAdmittedActor(modelCreationDB(tx), actor, "members.read")
		if err != nil {
			return err
		}
		if !read {
			return apperrors.ErrForbidden
		}
		var target entity.User
		if err := modelCreationDB(tx).Select("ID", "Email", "Name", "Role", "Disabled", "OffboardedAt", "CreatedAt", "ApprovalApplicationID", "LastLoginAt").Where(database.ExactText(tx, clause.Column{Name: "id"}, userID)).Take(&target).Error; err != nil {
			return err
		}
		if target.ID != userID || target.Role != entity.RoleAdmin && target.Role != entity.RoleMember {
			return apperrors.ErrNotFound
		}
		login, err := memberRecentLoginProjection(target.LastLoginAt)
		if err != nil {
			return err
		}
		apps, err := loadRegistrationApplications(modelCreationDB(tx), []entity.User{target})
		if err != nil {
			return err
		}
		approval, _ := registrationAdmission(target, apps)
		var relationships []entity.UserRole
		if err := modelCreationDB(tx).Where(database.ExactText(tx, clause.Column{Name: "user_id"}, userID)).Limit(memberListRoleBudget + 1).Find(&relationships).Error; err != nil {
			return err
		}
		roles, err := memberListRoles([]string{userID}, relationships)
		if err != nil {
			return err
		}
		status := "historical_unavailable"
		if login != nil {
			status = "recorded"
		}
		result = &MemberDetailRecord{MemberRecord: MemberRecord{User: target, RoleIDs: roles[userID]}, LastLoginAt: login, LastLoginStatus: status, RegistrationApproval: approval}
		return ctx.Err()
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, catalogError(err)
	}
	return result, nil
}
