package service

import (
	"context"
	"errors"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func teamAttachmentOwner(identity *TeamSessionIdentity) attachmentOwner {
	if identity == nil {
		return attachmentOwner{Kind: entity.StorageOwnerTeam}
	}
	return attachmentOwner{Kind: entity.StorageOwnerTeam, ID: identity.TeamID, CreatorUserID: identity.UserID, CreatorMembershipID: identity.TeamMembershipID}
}

func attachmentScopeQuery(db *gorm.DB, owner attachmentOwner, objectID string) *gorm.DB {
	if owner.Kind != entity.StorageOwnerTeam {
		return db.Where("id = ? AND owner_kind = ? AND owner_id = ? AND purpose = ?", objectID, owner.Kind, owner.ID, "attachment")
	}
	query := personalExact(personalExact(personalExact(db, "id", objectID), "owner_id", owner.ID), "owner_kind", entity.StorageOwnerTeam)
	query = personalExact(personalExact(query, "creator_user_id", owner.CreatorUserID), "creator_membership_id", owner.CreatorMembershipID)
	return query.Where("purpose = ?", "attachment")
}

func teamAttachmentMatches(row entity.StorageObject, owner attachmentOwner) bool {
	return owner.Kind == entity.StorageOwnerTeam && row.OwnerKind == owner.Kind && row.OwnerID == owner.ID &&
		owner.CreatorUserID != "" && owner.CreatorMembershipID != "" && row.CreatorUserID != nil && row.CreatorMembershipID != nil &&
		*row.CreatorUserID == owner.CreatorUserID && *row.CreatorMembershipID == owner.CreatorMembershipID && row.Purpose == "attachment"
}

// Exact current membership is independent of role. Neither ownership of the
// Team nor platform administrative access can borrow the creator's object.
func (s *Service) authorizeTeamAttachment(ctx context.Context, db *gorm.DB, identity *TeamSessionIdentity, modelID string) error {
	if err := s.ReauthorizeTeamSession(ctx, identity, modelID); err != nil {
		return err
	}
	// A caller may pass an initialized locking transaction handle. Session
	// preserves its context/clauses while giving each subject an isolated query.
	db = db.Session(&gorm.Session{})
	var user entity.User
	if err := personalExact(db, "id", identity.UserID).Take(&user).Error; err != nil {
		return err
	}
	if user.ID != identity.UserID || user.Disabled || user.OffboardedAt != nil {
		return apperrors.ErrNotFound
	}
	var team entity.Team
	if err := personalExact(db, "id", identity.TeamID).Take(&team).Error; err != nil {
		return err
	}
	if team.ID != identity.TeamID || team.Status != entity.ResourceActive {
		return apperrors.ErrNotFound
	}
	var member entity.TeamMembership
	query := personalExact(personalExact(personalExact(db, "id", identity.TeamMembershipID), "team_id", identity.TeamID), "user_id", identity.UserID)
	if err := query.Take(&member).Error; err != nil {
		return err
	}
	if member.ID != identity.TeamMembershipID || member.TeamID != identity.TeamID || member.UserID != identity.UserID || member.Status != entity.ResourceActive || (member.Role != entity.TeamMember && member.Role != entity.TeamOwner) {
		return apperrors.ErrNotFound
	}
	return s.ReauthorizeTeamSession(ctx, identity, modelID)
}

func (s *Service) teamAttachmentAuthorization(ctx context.Context, identity *TeamSessionIdentity, modelID string) attachmentAuthorization {
	return func(db *gorm.DB) error { return s.authorizeTeamAttachment(ctx, db, identity, modelID) }
}

func (s *Service) UploadTeamAttachment(ctx context.Context, identity *TeamSessionIdentity, name string, data []byte) (*AttachmentView, error) {
	if err := s.ReauthorizeTeamSession(ctx, identity, ""); err != nil {
		return nil, err
	}
	return s.uploadAttachment(ctx, identity.UserID, teamAttachmentOwner(identity), name, data, s.teamAttachmentAuthorization(ctx, identity, ""))
}

func (s *Service) TeamAttachment(ctx context.Context, identity *TeamSessionIdentity, objectID string) (*AttachmentView, error) {
	row, err := s.scopedAttachment(ctx, teamAttachmentOwner(identity), objectID, s.teamAttachmentAuthorization(ctx, identity, ""))
	if err != nil {
		return nil, err
	}
	if !attachmentReadableAt(row, time.Now().UTC()) {
		return nil, apperrors.ErrNotFound
	}
	view := attachmentView(row)
	return &view, nil
}

func (s *Service) TeamAttachmentContent(ctx context.Context, identity *TeamSessionIdentity, objectID string) (*AttachmentView, []byte, error) {
	row, data, err := s.readScopedAttachment(ctx, teamAttachmentOwner(identity), objectID, s.teamAttachmentAuthorization(ctx, identity, ""))
	if err != nil {
		return nil, nil, err
	}
	view := attachmentView(row)
	return &view, data, nil
}

func (s *Service) DeleteTeamAttachment(ctx context.Context, identity *TeamSessionIdentity, objectID string) (*AttachmentView, error) {
	if err := s.ReauthorizeTeamSession(ctx, identity, ""); err != nil {
		return nil, err
	}
	return s.deleteAttachment(ctx, identity.UserID, teamAttachmentOwner(identity), objectID, func(tx *gorm.DB) error {
		// The governance lock already serializes member/lifecycle changes. Keep
		// row locking here consistent with the existing storage mutation seam.
		return s.authorizeTeamAttachment(ctx, tx.Clauses(clause.Locking{Strength: "UPDATE"}), identity, "")
	})
}

func attachmentError(owner attachmentOwner, err error) error {
	if owner.Kind == entity.StorageOwnerTeam {
		var gateway *GatewayError
		if errors.As(err, &gateway) {
			return gateway
		}
	}
	return catalogError(err)
}
