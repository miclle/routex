package service

import (
	"context"
	"strings"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/id"
)

func (s *Service) RegistrationEnabled(ctx context.Context) (bool, error) {
	var settings entity.GovernanceSetting
	if err := s.authDB(ctx).First(&settings, 1).Error; err != nil {
		return false, catalogError(err)
	}
	initialized, err := s.Initialized(ctx)
	return settings.RegistrationEnabled && initialized, err
}

func (s *Service) SetRegistrationEnabled(ctx context.Context, actorID string, enabled bool) error {
	return catalogError(s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		if err := activePlatformAdmin(tx, actorID); err != nil {
			return err
		}
		if err := tx.Model(&entity.GovernanceSetting{}).Where("id = ?", 1).Update("registration_enabled", enabled).Error; err != nil {
			return err
		}
		return appendAudit(tx, actorID, "registration.update", "installation", "1")
	}))
}

func prepareMember(email, password, name, role string) (entity.User, error) {
	email, valid := normalizeEmail(email)
	name = strings.TrimSpace(name)
	if !valid || !validPassword(password) || !validCatalogLabel(name) || (role != entity.RoleAdmin && role != entity.RoleMember) {
		return entity.User{}, apperrors.ErrBadRequest
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return entity.User{}, apperrors.ErrInternal
	}
	userID, err := id.NewPrefixed("usr")
	if err != nil {
		return entity.User{}, apperrors.ErrInternal
	}
	revision, err := newMemberRoleRevision()
	if err != nil {
		return entity.User{}, err
	}
	return entity.User{ID: userID, Email: email, Name: name, Role: role, PasswordHash: string(hash), MemberRoleRevision: revision}, nil
}

func (s *Service) Register(ctx context.Context, email, password, name string) (*Authentication, error) {
	enabled, err := s.RegistrationEnabled(ctx)
	if err != nil {
		return nil, err
	}
	if !enabled {
		return nil, apperrors.ErrForbidden
	}
	user, err := prepareMember(email, password, name, entity.RoleMember)
	if err != nil {
		return nil, err
	}
	var auth *Authentication
	err = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		var settings entity.GovernanceSetting
		if err := tx.First(&settings, 1).Error; err != nil {
			return err
		}
		if !settings.RegistrationEnabled {
			return apperrors.ErrForbidden
		}
		var installation entity.Installation
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&installation, 1).Error; err != nil {
			return err
		}
		if !installation.Initialized {
			return apperrors.ErrForbidden
		}
		if err := tx.Create(&user).Error; err != nil {
			return err
		}
		if err := applyCreationDefaultLimit(tx, "user", user.ID, user.ID); err != nil {
			return err
		}
		var err error
		auth, err = createSession(tx, user)
		if err != nil {
			return err
		}
		return appendAudit(tx, user.ID, "member.register", "user", user.ID)
	})
	return auth, s.refreshAfterMutation(ctx, catalogError(err))
}

func (s *Service) CreateMember(ctx context.Context, actorID, email, password, name, role string) (*MemberRecord, error) {
	if role == "" {
		role = entity.RoleMember
	}
	user, err := prepareMember(email, password, name, role)
	if err != nil {
		return nil, err
	}
	db := s.authDB(ctx)
	err = db.Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		if err := authorizeGovernance(tx, actorID, "members.write"); err != nil {
			return err
		}
		if role == entity.RoleAdmin {
			if err := activePlatformAdmin(tx, actorID); err != nil {
				return err
			}
		}
		if err := tx.Create(&user).Error; err != nil {
			return err
		}
		if err := applyCreationDefaultLimit(tx, "user", user.ID, actorID); err != nil {
			return err
		}
		return appendAudit(tx, actorID, "member.create", "user", user.ID)
	})
	if err = s.refreshAfterMutation(ctx, catalogError(err)); err != nil {
		return nil, err
	}
	result, err := memberRecord(db, user.ID)
	return result, catalogError(err)
}

func (s *Service) UpdateMember(ctx context.Context, actorID, userID string, disabled *bool, role *string) (*MemberRecord, error) {
	releasePublication := s.pinPersonalKeyMutation()
	defer releasePublication()
	if disabled == nil && role == nil {
		return nil, apperrors.ErrBadRequest
	}
	if role != nil && *role != entity.RoleAdmin && *role != entity.RoleMember {
		return nil, apperrors.ErrBadRequest
	}
	db := s.authDB(ctx)
	invalidate := false
	var canonicalUserID string
	err := db.Transaction(func(tx *gorm.DB) error {
		// Every administrator lifecycle change takes this singleton lock before
		// counting active admins, so concurrent removals cannot both pass.
		if err := lockGovernance(tx); err != nil {
			return err
		}
		var actor entity.User
		if err := tx.First(&actor, "id = ?", actorID).Error; err != nil {
			return err
		}
		if actor.ID != actorID {
			return apperrors.ErrForbidden
		}
		if err := authorizeGovernance(tx, actorID, "members.write"); err != nil {
			return err
		}

		var target entity.User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&target, "id = ?", userID).Error; err != nil {
			return err
		}
		if target.ID != userID {
			return apperrors.ErrNotFound
		}
		canonicalUserID = target.ID
		if actor.Role != entity.RoleAdmin && (actorID == userID || target.Role == entity.RoleAdmin || role != nil) {
			return apperrors.ErrForbidden
		}
		nextDisabled, nextRole := target.Disabled, target.Role
		if disabled != nil {
			nextDisabled = *disabled
		}
		if role != nil {
			nextRole = *role
		}
		if target.Role == entity.RoleAdmin && !target.Disabled && (nextDisabled || nextRole != entity.RoleAdmin) {
			var activeAdmins int64
			if err := tx.Model(&entity.User{}).Where("role = ? AND disabled = ?", entity.RoleAdmin, false).Count(&activeAdmins).Error; err != nil {
				return err
			}
			if activeAdmins <= 1 {
				return catalogConflict
			}
		}
		if nextDisabled == target.Disabled && nextRole == target.Role {
			return nil
		}
		if nextDisabled && !target.Disabled {
			if err := validateResourceContinuity(tx, userID); err != nil {
				return err
			}
		}
		invalidate = nextDisabled || (target.Role == entity.RoleAdmin && nextRole != entity.RoleAdmin)
		updates := map[string]any{"disabled": nextDisabled, "role": nextRole}
		reactivating := target.Disabled && !nextDisabled && target.OffboardedAt != nil
		if reactivating {
			updates["offboarded_at"] = nil
		}
		if err := tx.Model(&target).Updates(updates).Error; err != nil {
			return err
		}
		if err := advanceMemberRoleRevision(tx, canonicalUserID); err != nil {
			return err
		}
		if nextDisabled {
			if err := CancelTeamModelRequestsForUser(tx, actorID, canonicalUserID, "applicant_unavailable"); err != nil {
				return err
			}
			if err := CancelPersonalModelRequestsForUser(tx, actorID, canonicalUserID, "applicant_unavailable"); err != nil {
				return err
			}
			if err := cancelTeamQuotaRequestsForUser(tx, canonicalUserID, "applicant_unavailable"); err != nil {
				return err
			}
			if err := invalidateMFAChallenges(tx, userID); err != nil {
				return err
			}
			if err := tx.Where("user_id = ?", userID).Delete(&entity.Session{}).Error; err != nil {
				return err
			}
			if err := revokeOwnerPersonalKeys(tx, canonicalUserID); err != nil {
				return err
			}
		}
		if reactivating {
			return appendAudit(tx, actorID, "member.reactivate", "user", userID)
		}
		return appendAudit(tx, actorID, "member.update", "user", userID)
	})
	if err != nil {
		return nil, catalogError(err)
	}
	if invalidate {
		s.InvalidateRuntimeUser(canonicalUserID)
	}
	releasePublication()
	if err := s.refreshAfterMutation(ctx, nil); err != nil {
		return nil, err
	}
	result, err := memberRecord(db, userID)
	return result, catalogError(err)
}

// ListMembers preserves the existing service record shape for internal callers.
func (s *Service) ListMembers(ctx context.Context, actorID string, filter MemberFilter) (*MemberPage, error) {
	page, err := s.ListMemberSummaries(ctx, actorID, filter)
	if err != nil {
		return nil, err
	}
	result := &MemberPage{Members: []MemberRecord{}, NextCursor: page.NextCursor}
	for _, item := range page.Members {
		result.Members = append(result.Members, item.MemberRecord)
	}
	return result, nil
}

func (s *Service) GetMember(ctx context.Context, actorID, userID string) (*MemberRecord, error) {
	db := s.authDB(ctx)
	if err := authorizeGovernance(db, actorID, "members.read"); err != nil {
		return nil, catalogError(err)
	}
	result, err := memberRecord(db, userID)
	return result, catalogError(err)
}
