package service

import (
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
)

type teamModelSubject struct {
	Team    entity.Team
	User    *entity.User
	Member  *entity.TeamMembership
	Model   *entity.Model
	Name    *entity.ModelName
	Grant   *entity.TeamModelGrant
	Pending *entity.TeamModelRequestPendingSlot
}

func teamModelOptional(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	return err
}
func loadTeamModelSubject(tx *gorm.DB, userID, teamID, modelID string, lock bool) (*teamModelSubject, error) {
	result := &teamModelSubject{}
	query := personalExact(tx, "id", userID)
	if lock {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var user entity.User
	err := query.Take(&user).Error
	if err != nil {
		if err := teamModelOptional(err); err != nil {
			return nil, err
		}
	} else {
		if user.ID != userID {
			return nil, apperrors.ErrNotFound
		}
		result.User = &user
	}
	query = personalExact(tx, "id", teamID)
	if lock {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	if err := query.Take(&result.Team).Error; err != nil {
		return nil, err
	}
	if result.Team.ID != teamID {
		return nil, apperrors.ErrNotFound
	}
	var member entity.TeamMembership
	err = personalExact(personalExact(tx, "team_id", teamID), "user_id", userID).Take(&member).Error
	if err != nil {
		if err := teamModelOptional(err); err != nil {
			return nil, err
		}
	} else {
		if member.TeamID != teamID || member.UserID != userID {
			return nil, apperrors.ErrNotFound
		}
		result.Member = &member
	}
	if modelID == "" {
		return result, nil
	}
	var model entity.Model
	query = personalExact(tx, "id", modelID)
	if lock {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	err = query.Take(&model).Error
	if err != nil {
		if err := teamModelOptional(err); err != nil {
			return nil, err
		}
	} else {
		if model.ID != modelID {
			return nil, apperrors.ErrNotFound
		}
		result.Model = &model
		var name entity.ModelName
		err = personalExact(personalExact(tx, "current_model_id", modelID), "model_id", modelID).Take(&name).Error
		if err != nil {
			if err := teamModelOptional(err); err != nil {
				return nil, err
			}
		} else {
			if name.ModelID != modelID || name.CurrentModelID == nil || *name.CurrentModelID != modelID {
				return nil, apperrors.ErrNotFound
			}
			result.Name = &name
		}
	}
	var grant entity.TeamModelGrant
	err = personalExact(personalExact(tx, "team_id", teamID), "model_id", modelID).Take(&grant).Error
	if err != nil {
		if err := teamModelOptional(err); err != nil {
			return nil, err
		}
	} else {
		if grant.TeamID != teamID || grant.ModelID != modelID {
			return nil, apperrors.ErrNotFound
		}
		result.Grant = &grant
	}
	var pending entity.TeamModelRequestPendingSlot
	err = personalExact(personalExact(tx, "team_id", teamID), "model_id", modelID).Take(&pending).Error
	if err != nil {
		if err := teamModelOptional(err); err != nil {
			return nil, err
		}
	} else {
		if pending.TeamID != teamID || pending.ModelID != modelID {
			return nil, apperrors.ErrNotFound
		}
		result.Pending = &pending
	}
	return result, nil
}
func teamModelActiveMember(subject *teamModelSubject) bool {
	return subject != nil && subject.User != nil && !subject.User.Disabled && subject.User.OffboardedAt == nil && subject.Team.Status == entity.ResourceActive && subject.Member != nil && subject.Member.Status == entity.ResourceActive && (subject.Member.Role == entity.TeamMember || subject.Member.Role == entity.TeamOwner) && subject.Member.UserID == subject.User.ID && subject.Member.TeamID == subject.Team.ID && safeTeamSessionID(subject.Member.ID)
}
func teamModelMembershipMatches(subject *teamModelSubject, row entity.TeamModelRequest) bool {
	return teamModelActiveMember(subject) && subject.Team.ID == row.TeamID && subject.User.ID == row.ApplicantUserID && subject.Member.ID == row.ApplicantMembershipID
}
func teamModelAvailable(subject *teamModelSubject) bool {
	return subject != nil && subject.Model != nil && subject.Name != nil && subject.Model.Status == entity.ResourceActive && subject.Name.Name != ""
}

// Reviews bind only relevant identity, lifecycle and grant facts. User secrets
// and unrelated private account fields never enter these public validators.
func teamModelSubjectReview(subject *teamModelSubject) any {
	if subject == nil {
		return nil
	}
	var user any
	if subject.User != nil {
		user = struct {
			ID           string
			Disabled     bool
			OffboardedAt *time.Time
		}{subject.User.ID, subject.User.Disabled, subject.User.OffboardedAt}
	}
	return struct {
		Team    entity.Team
		User    any
		Member  *entity.TeamMembership
		Model   *entity.Model
		Name    *entity.ModelName
		Grant   *entity.TeamModelGrant
		Pending *entity.TeamModelRequestPendingSlot
	}{subject.Team, user, subject.Member, subject.Model, subject.Name, subject.Grant, subject.Pending}
}
func teamModelCandidateETag(actorID string, subject *teamModelSubject) string {
	return personalHash(struct {
		ActorID string
		Subject any
	}{actorID, teamModelSubjectReview(subject)})
}
func teamModelReviewETag(actorID string, row entity.TeamModelRequest, subject *teamModelSubject) string {
	return personalHash(struct {
		ActorID string
		Row     entity.TeamModelRequest
		Subject any
	}{actorID, row, teamModelSubjectReview(subject)})
}
func teamModelReviewerAccess(tx *gorm.DB, actorID, teamID string) (*entity.User, error) {
	actor, err := exactEnabledActor(tx, actorID)
	if err != nil {
		return nil, err
	}
	allowed, err := teamTargetActionAllowed(tx, actorID, teamID, "teams.models.write")
	if err != nil {
		return nil, err
	}
	if !allowed {
		return nil, apperrors.ErrForbidden
	}
	return &actor, nil
}
func teamModelAllowed(actorID string, row entity.TeamModelRequest, subject *teamModelSubject, reviewer bool) []string {
	if row.Status != entity.TeamModelRequestPending {
		return []string{}
	}
	if !reviewer {
		if actorID == row.ApplicantUserID {
			return []string{"withdraw"}
		}
		return []string{}
	}
	if actorID == row.ApplicantUserID || subject == nil || subject.Team.ID != row.TeamID || subject.Pending == nil || subject.Pending.RequestID != row.ID {
		return []string{}
	}
	if teamModelMembershipMatches(subject, row) && teamModelAvailable(subject) && subject.Model.ID == row.ModelID {
		return []string{"approve", "reject"}
	}
	return []string{"reject"}
}
