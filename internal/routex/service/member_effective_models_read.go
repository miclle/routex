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

type memberEffectiveTeam struct {
	ID, Name, Status                                                                   string
	CreatedAt                                                                          time.Time
	MembershipID, MembershipUserID, MembershipTeamID, MembershipRole, MembershipStatus string
}
type memberEffectiveModelsData struct {
	CipherHashes map[string]string
	Metadata     *memberModelsData
	Teams        []memberEffectiveTeam
	TeamGrants   []entity.TeamModelGrant
	TeamsRead    bool
}

func memberEffectiveModelsPermissions(tx *gorm.DB, actorID string) (bool, bool, bool, error) {
	actor, err := exactEnabledActor(modelCreationDB(tx), actorID)
	if err != nil {
		return false, false, false, err
	}
	if actor.Role != entity.RoleAdmin && actor.Role != entity.RoleMember {
		return false, false, false, apperrors.ErrUnauthorized
	}
	read, err := exactGovernancePermissionForAdmittedActor(modelCreationDB(tx), actor, "members.read")
	if err != nil {
		return false, false, false, err
	}
	if !read {
		return false, false, false, apperrors.ErrForbidden
	}
	flags := make([]bool, 3)
	for i, p := range []string{"teams.read_all", "providers.read", "prices.read"} {
		flags[i], err = exactGovernancePermissionForAdmittedActor(modelCreationDB(tx), actor, p)
		if err != nil {
			return false, false, false, err
		}
	}
	return flags[0], flags[1], flags[2], nil
}
func memberEffectiveTeamsQuery(tx *gorm.DB, userID string) *gorm.DB {
	return modelCreationDB(tx).Table("team_memberships AS member").Select("team.id,team.name,team.status,team.created_at,member.id AS membership_id,member.user_id AS membership_user_id,member.team_id AS membership_team_id,member.role AS membership_role,member.status AS membership_status").
		Joins("JOIN teams AS team ON ?", database.ExactTextColumns(tx, clause.Column{Table: "team", Name: "id"}, clause.Column{Table: "member", Name: "team_id"})).
		Where(database.ExactText(tx, clause.Column{Table: "member", Name: "user_id"}, userID)).
		Where(database.ExactText(tx, clause.Column{Table: "member", Name: "status"}, entity.ResourceActive)).
		Where(database.ExactText(tx, clause.Column{Table: "team", Name: "status"}, entity.ResourceActive)).Order("team.id").Limit(101)
}
func readMemberEffectiveModels(tx *gorm.DB, subject entity.User, teams, providers, prices bool) (*memberEffectiveModelsData, error) {
	if subject.CreatedAt.IsZero() || !personalModelETag(subject.PersonalGrantRevision) {
		return nil, apperrors.ErrInternal
	}
	data := &memberEffectiveModelsData{Metadata: &memberModelsData{Subject: subject, ProvidersRead: providers, PricesRead: prices}, TeamsRead: teams}
	meta := data.Metadata
	applications, err := loadRegistrationApplications(modelCreationDB(tx), []entity.User{subject})
	if err != nil {
		return nil, err
	}
	meta.Applications = applications
	if err := personalExact(modelCreationDB(tx), "user_id", subject.ID).Order("model_id").Limit(1001).Find(&meta.Grants).Error; err != nil {
		return nil, err
	}
	if len(meta.Grants) > 1000 {
		return nil, ErrModelCatalogOverflow
	}
	ids := []string{}
	for _, g := range meta.Grants {
		if g.UserID != subject.ID || !validAdminModelTarget(g.ModelID) {
			return nil, apperrors.ErrInternal
		}
		ids = append(ids, g.ModelID)
	}
	// No Team SQL, IDs, labels or readiness enrichment without this independent grant.
	if teams {
		if err := memberEffectiveTeamsQuery(tx, subject.ID).Scan(&data.Teams).Error; err != nil {
			return nil, err
		}
		if len(data.Teams) > 100 {
			return nil, ErrModelCatalogOverflow
		}
		teamIDs := []string{}
		seen := map[string]bool{}
		for _, team := range data.Teams {
			if !safeTeamSessionID(team.ID) || !safeTeamSessionID(team.MembershipID) || team.MembershipUserID != subject.ID || team.MembershipTeamID != team.ID || team.CreatedAt.IsZero() || team.Status != entity.ResourceActive || team.MembershipStatus != entity.ResourceActive || team.MembershipRole != entity.TeamOwner && team.MembershipRole != entity.TeamMember || !validCatalogLabel(team.Name) || seen[team.ID] {
				return nil, apperrors.ErrInternal
			}
			seen[team.ID] = true
			teamIDs = append(teamIDs, team.ID)
		}
		if len(teamIDs) > 0 {
			if err := modelCreationDB(tx).Where(memberModelsExactIDs(tx, "team_id", teamIDs)).Order("team_id,model_id").Limit(5001).Find(&data.TeamGrants).Error; err != nil {
				return nil, err
			}
			if len(data.TeamGrants) > 5000 {
				return nil, ErrModelCatalogOverflow
			}
			for _, g := range data.TeamGrants {
				if !seen[g.TeamID] || !validAdminModelTarget(g.ModelID) || g.SourceRequestID != nil && !safeTeamSessionID(*g.SourceRequestID) {
					return nil, apperrors.ErrInternal
				}
				ids = append(ids, g.ModelID)
			}
		}
	}
	slices.Sort(ids)
	ids = slices.Compact(ids)
	if len(ids) > 1000 {
		return nil, ErrModelCatalogOverflow
	}
	if len(ids) == 0 {
		return data, nil
	}
	if err := modelCreationDB(tx).Where(memberModelsExactIDs(tx, "id", ids)).Order("id").Limit(1001).Find(&meta.Models).Error; err != nil {
		return nil, err
	}
	if len(meta.Models) != len(ids) {
		return nil, apperrors.ErrInternal
	}
	for _, m := range meta.Models {
		if !slices.Contains(ids, m.ID) || m.Status != entity.ResourceActive && m.Status != entity.ResourceDisabled && m.Status != entity.ResourceArchived {
			return nil, apperrors.ErrInternal
		}
	}
	if err := readMemberModelMetadata(tx, meta, ids); err != nil {
		return nil, err
	}
	hashes, err := readMemberEffectiveCipherHashes(tx, meta.Credentials)
	if err != nil {
		return nil, err
	}
	data.CipherHashes = hashes
	return data, nil
}
func (s *Service) MemberEffectiveModels(ctx context.Context, actorID, userID string) (*MemberEffectiveModelsPage, error) {
	if !safeTeamSessionID(actorID) {
		return nil, apperrors.ErrUnauthorized
	}
	if !safeTeamSessionID(userID) {
		return nil, apperrors.ErrBadRequest
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var result *MemberEffectiveModelsPage
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		teams, providers, prices, err := memberEffectiveModelsPermissions(tx, actorID)
		if err != nil {
			return err
		}
		subject, err := memberModelsSubject(tx, userID, false)
		if err != nil {
			return err
		}
		data, err := readMemberEffectiveModels(tx, subject, teams, providers, prices)
		if err != nil {
			return err
		}
		result = s.projectMemberEffectiveModels(data)
		return ctx.Err()
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, catalogError(err)
	}
	return result, nil
}
