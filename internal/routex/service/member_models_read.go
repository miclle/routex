package service

import (
	"context"
	"database/sql"
	"slices"
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func memberModelsExactIDs(tx *gorm.DB, column string, ids []string) clause.Expression {
	expressions := make([]clause.Expression, 0, len(ids))
	for _, id := range ids {
		expressions = append(expressions, database.ExactText(tx, clause.Column{Name: column}, id))
	}
	if len(expressions) == 0 {
		return clause.Eq{Column: clause.Column{Name: column}, Value: nil}
	}
	return clause.Or(expressions...)
}
func memberModelsSubject(tx *gorm.DB, userID string, lock bool) (entity.User, error) {
	var user entity.User
	q := personalExact(modelCreationDB(tx), "id", userID).Select("id", "role", "disabled", "offboarded_at", "created_at", "approval_application_id", "personal_grant_revision")
	if lock {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	if err := q.Take(&user).Error; err != nil {
		return user, err
	}
	if user.ID != userID || user.Role != entity.RoleAdmin && user.Role != entity.RoleMember {
		return user, apperrors.ErrNotFound
	}
	return user, nil
}
func memberModelsPermissions(tx *gorm.DB, actorID string, write bool) (entity.User, bool, bool, bool, error) {
	actor, err := exactEnabledActor(modelCreationDB(tx), actorID)
	if err != nil {
		return actor, false, false, false, err
	}
	if actor.Role != entity.RoleMember && actor.Role != entity.RoleAdmin {
		return actor, false, false, false, apperrors.ErrUnauthorized
	}
	canEdit, err := exactGovernancePermissionForAdmittedActor(modelCreationDB(tx), actor, "members.models.write")
	if err != nil {
		return actor, false, false, false, err
	}
	read := canEdit
	if !write && !read {
		read, err = exactGovernancePermissionForAdmittedActor(modelCreationDB(tx), actor, "members.read")
		if err != nil {
			return actor, false, false, false, err
		}
	}
	if write && !canEdit || !write && !read {
		return actor, false, false, false, apperrors.ErrForbidden
	}
	providers, err := exactGovernancePermissionForAdmittedActor(modelCreationDB(tx), actor, "providers.read")
	if err != nil {
		return actor, false, false, false, err
	}
	prices, err := exactGovernancePermissionForAdmittedActor(modelCreationDB(tx), actor, "prices.read")
	return actor, canEdit, providers, prices, err
}
func readMemberModels(tx *gorm.DB, subject entity.User, canEdit, providers, prices, lock bool) (*memberModelsData, error) {
	if subject.CreatedAt.IsZero() || !personalModelETag(subject.PersonalGrantRevision) {
		return nil, apperrors.ErrInternal
	}
	data := &memberModelsData{Subject: subject, CanEdit: canEdit && !subject.Disabled && subject.OffboardedAt == nil, ProvidersRead: providers, PricesRead: prices, TeamModels: map[string]bool{}, TeamBasis: []memberModelsTeamBasis{}}
	applications, err := loadRegistrationApplications(modelCreationDB(tx), []entity.User{subject})
	if err != nil {
		return nil, err
	}
	data.Applications = applications
	if err := personalExact(modelCreationDB(tx), "user_id", subject.ID).Order("model_id").Limit(1001).Find(&data.Grants).Error; err != nil {
		return nil, err
	}
	if len(data.Grants) > 1000 {
		return nil, ErrModelCatalogOverflow
	}
	personalIDs := []string{}
	for _, g := range data.Grants {
		if g.UserID != subject.ID || !validAdminModelTarget(g.ModelID) {
			return nil, apperrors.ErrInternal
		}
		personalIDs = append(personalIDs, g.ModelID)
	}
	if data.CanEdit {
		var teams []struct{ ID, TeamID, UserID, Status, TeamStatus string }
		q := modelCreationDB(tx).Table("team_memberships AS membership").Select("membership.id,membership.team_id,membership.user_id,membership.status,team.status AS team_status").Joins("JOIN teams AS team ON ?", database.ExactTextColumns(tx, clause.Column{Table: "membership", Name: "team_id"}, clause.Column{Table: "team", Name: "id"})).Where(database.ExactText(tx, clause.Column{Table: "membership", Name: "user_id"}, subject.ID)).Where(database.ExactText(tx, clause.Column{Table: "membership", Name: "status"}, "active")).Where(database.ExactText(tx, clause.Column{Table: "team", Name: "status"}, "active"))
		if err := q.Order("membership.id").Limit(101).Scan(&teams).Error; err != nil {
			return nil, err
		}
		if len(teams) > 100 {
			return nil, ErrModelCatalogOverflow
		}
		teamIDs := []string{}
		members := map[string]string{}
		for _, t := range teams {
			if t.UserID != subject.ID || !safeTeamSessionID(t.ID) || !safeTeamSessionID(t.TeamID) || t.Status != "active" || t.TeamStatus != "active" {
				return nil, apperrors.ErrInternal
			}
			teamIDs = append(teamIDs, t.TeamID)
			members[t.TeamID] = t.ID
			data.TeamBasis = append(data.TeamBasis, memberModelsTeamBasis{TeamID: t.TeamID, MembershipID: t.ID})
		}
		if len(teamIDs) > 0 {
			var grants []entity.TeamModelGrant
			if err := modelCreationDB(tx).Where(memberModelsExactIDs(tx, "team_id", teamIDs)).Order("team_id,model_id").Limit(5001).Find(&grants).Error; err != nil {
				return nil, err
			}
			if len(grants) > 5000 {
				return nil, ErrModelCatalogOverflow
			}
			for _, g := range grants {
				if members[g.TeamID] == "" || !validAdminModelTarget(g.ModelID) {
					return nil, apperrors.ErrInternal
				}
				data.TeamModels[g.ModelID] = true
				data.TeamBasis = append(data.TeamBasis, memberModelsTeamBasis{g.TeamID, members[g.TeamID], g.ModelID})
			}
		}
	}

	modelQuery := modelCreationDB(tx)
	if !data.CanEdit {
		modelQuery = modelQuery.Where(memberModelsExactIDs(tx, "id", personalIDs))
	} else {
		excluded := []string{}
		for id := range data.TeamModels {
			if !slices.Contains(personalIDs, id) {
				excluded = append(excluded, id)
			}
		}
		slices.Sort(excluded)
		active := modelCreationDB(tx).Where(database.ExactText(tx, clause.Column{Name: "status"}, "active"))
		if len(excluded) > 0 {
			active = active.Where(clause.Not(memberModelsExactIDs(tx, "id", excluded)))
		}
		modelQuery = modelQuery.Where(modelCreationDB(tx).Where(memberModelsExactIDs(tx, "id", personalIDs)).Or(active))
	}
	if lock {
		modelQuery = modelQuery.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	if err := modelQuery.Order("id").Limit(1001).Find(&data.Models).Error; err != nil {
		return nil, err
	}
	if len(data.Models) > 1000 {
		return nil, ErrModelCatalogOverflow
	}
	modelIDs := []string{}
	for _, m := range data.Models {
		if !validAdminModelTarget(m.ID) || m.Status != "active" && m.Status != "disabled" && m.Status != "archived" {
			return nil, apperrors.ErrInternal
		}
		modelIDs = append(modelIDs, m.ID)
	}
	for _, id := range personalIDs {
		if !slices.Contains(modelIDs, id) {
			return nil, apperrors.ErrInternal
		}
	}
	if err := readMemberModelMetadata(tx, data, modelIDs); err != nil {
		return nil, err
	}

	return data, nil
}
func (s *Service) MemberModelsWorkspace(ctx context.Context, actorID, userID string) (*MemberModelsWorkspace, error) {
	if !safeTeamSessionID(actorID) {
		return nil, apperrors.ErrUnauthorized
	}
	if !safeTeamSessionID(userID) {
		return nil, apperrors.ErrBadRequest
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var result *MemberModelsWorkspace
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		_, edit, providers, prices, err := memberModelsPermissions(tx, actorID, false)
		if err != nil {
			return err
		}
		subject, err := memberModelsSubject(tx, userID, false)
		if err != nil {
			return err
		}
		data, err := readMemberModels(tx, subject, edit, providers, prices, false)
		if err != nil {
			return err
		}
		result = s.projectMemberModels(actorID, data)
		return ctx.Err()
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return result, catalogError(err)
}
