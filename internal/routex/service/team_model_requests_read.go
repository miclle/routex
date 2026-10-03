package service

import (
	"context"
	"database/sql"
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
)

func teamModelCurrentRead(tx *gorm.DB, actorID, teamID string) (bool, error) {
	_, err := teamRoleTargetAccess(tx, actorID, teamID, false)
	if errors.Is(err, apperrors.ErrNotFound) || errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	return err == nil, err
}
func (s *Service) teamModelRequestDetail(tx *gorm.DB, actorID string, row entity.TeamModelRequest, reviewer bool) (*TeamModelRequestDetail, error) {
	result := &TeamModelRequestDetail{TeamModelRequestRecord: teamModelRequestRecord(row), AllowedActions: []string{}, ApplicationStatus: "unavailable"}
	canRead, err := teamModelCurrentRead(tx, actorID, row.TeamID)
	if err != nil {
		return nil, err
	}
	var subject *teamModelSubject
	if canRead {
		subject, err = loadTeamModelSubject(tx, row.ApplicantUserID, row.TeamID, row.ModelID, false)
		if err != nil {
			return nil, err
		}
		result.CurrentTeam = &TeamModelCurrentTeam{ID: subject.Team.ID, Name: subject.Team.Name, Status: subject.Team.Status}
		if subject.Model != nil && subject.Name != nil {
			result.CurrentModel = &PersonalModelCurrentModel{ID: subject.Model.ID, Name: subject.Name.Name, Status: subject.Model.Status}
		}
		membership := teamModelMembershipMatches(subject, row)
		granted := subject.Grant != nil
		applied := false
		result.CurrentMembershipMatches, result.CurrentGranted, result.RuntimeApplied = &membership, &granted, &applied
		result.ApplicationStatus = "pending"
		if row.Status == entity.TeamModelRequestApproved {
			result.ApplicationStatus = "superseded"
			if teamModelAvailable(subject) && subject.Team.Status == entity.ResourceActive && subject.Grant != nil && subject.Grant.SourceRequestID != nil && *subject.Grant.SourceRequestID == row.ID {
				result.ApplicationStatus = "pending"
				applied = s.RuntimeTeamModelGrantApplied(row.TeamID, row.ModelID, row.ID)
				if applied {
					result.ApplicationStatus = "applied"
				}
			}
		}
	}
	result.AllowedActions = teamModelAllowed(actorID, row, subject, reviewer)
	result.ReviewETag = teamModelReviewETag(actorID, row, subject)
	return result, nil
}
func (s *Service) GetTeamModelRequest(ctx context.Context, actorID, teamID, requestID string, reviewer bool) (*TeamModelRequestDetail, error) {
	if !personalModelID(requestID, "tmr") || reviewer && !safeTeamSessionID(teamID) || !reviewer && teamID != "" {
		return nil, apperrors.ErrBadRequest
	}
	var result *TeamModelRequestDetail
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := exactEnabledActor(tx, actorID); err != nil {
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
		var row entity.TeamModelRequest
		if err := query.Take(&row).Error; err != nil {
			return err
		}
		if row.ID != requestID || reviewer && row.TeamID != teamID || !reviewer && row.ApplicantUserID != actorID {
			return apperrors.ErrNotFound
		}
		var err error
		result, err = s.teamModelRequestDetail(tx, actorID, row, reviewer)
		return err
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return result, catalogError(err)
}
func teamModelRequestQuery(tx *gorm.DB, actorID, teamID string, filter TeamModelRequestFilter, reviewer bool) *gorm.DB {
	q := tx.Model(&entity.TeamModelRequest{})
	if reviewer {
		q = personalExact(q, "team_id", teamID)
	} else {
		q = personalExact(q, "applicant_user_id", actorID)
		if filter.TeamID != "" {
			q = personalExact(q, "team_id", filter.TeamID)
		}
	}
	if filter.ModelID != "" {
		q = personalExact(q, "model_id", filter.ModelID)
	}
	if filter.Status != "" {
		q = personalExact(q, "status", filter.Status)
	}
	return q
}
func (s *Service) ListTeamModelRequests(ctx context.Context, actorID, teamID string, filter TeamModelRequestFilter, reviewer bool) (*TeamModelRequestPage, error) {
	if reviewer && (!safeTeamSessionID(teamID) || filter.TeamID != "") || !reviewer && teamID != "" || filter.TeamID != "" && !safeTeamSessionID(filter.TeamID) || filter.ModelID != "" && !safeTeamSessionID(filter.ModelID) || filter.Status != "" && !personalModelStatus(filter.Status) {
		return nil, apperrors.ErrBadRequest
	}
	if filter.Limit == 0 {
		filter.Limit = 25
	}
	if filter.Limit < 1 || filter.Limit > 50 {
		return nil, apperrors.ErrBadRequest
	}
	scope := teamModelCursorScope(actorID, teamID, filter, reviewer)
	cursor, err := decodeTeamModelRequestCursor(filter.Cursor, scope)
	if err != nil {
		return nil, err
	}
	result := &TeamModelRequestPage{Items: []TeamModelRequestRecord{}}
	err = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if reviewer {
			if _, err := teamModelReviewerAccess(tx, actorID, teamID); err != nil {
				return err
			}
		} else if _, err := exactEnabledActor(tx, actorID); err != nil {
			return err
		}
		query := teamModelRequestQuery(tx, actorID, teamID, filter, reviewer)
		if err := query.Session(&gorm.Session{}).Count(&result.Total).Error; err != nil {
			return err
		}
		if cursor != nil {
			query = query.Where(clause.Or(clause.Lt{Column: clause.Column{Name: "created_at"}, Value: cursor.CreatedAt}, clause.And(clause.Eq{Column: clause.Column{Name: "created_at"}, Value: cursor.CreatedAt}, clause.Lt{Column: clause.Column{Name: "id"}, Value: cursor.ID})))
		}
		var rows []entity.TeamModelRequest
		if err := query.Order("created_at DESC,id DESC").Limit(filter.Limit + 1).Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) > filter.Limit {
			row := rows[filter.Limit-1]
			value := encodeTeamModelRequestCursor(teamModelRequestCursor{Scope: scope, CreatedAt: row.CreatedAt.UTC(), ID: row.ID})
			result.NextCursor = &value
			rows = rows[:filter.Limit]
		}
		for _, row := range rows {
			if !personalModelID(row.ID, "tmr") || reviewer && row.TeamID != teamID || !reviewer && row.ApplicantUserID != actorID || filter.TeamID != "" && row.TeamID != filter.TeamID || filter.ModelID != "" && row.ModelID != filter.ModelID {
				return apperrors.ErrInternal
			}
			result.Items = append(result.Items, teamModelRequestRecord(row))
		}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return result, catalogError(err)
}
func (s *Service) TeamModelRequestWorkspace(ctx context.Context, actorID, teamID string) (*TeamModelRequestWorkspace, error) {
	if !safeTeamSessionID(teamID) {
		return nil, apperrors.ErrBadRequest
	}
	result := &TeamModelRequestWorkspace{TeamID: teamID, Models: []PersonalModelCurrentModel{}, CanReviewRequests: true}
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := teamModelReviewerAccess(tx, actorID, teamID); err != nil {
			return err
		}
		var team entity.Team
		if err := personalExact(tx, "id", teamID).Take(&team).Error; err != nil {
			return err
		}
		if team.ID != teamID {
			return apperrors.ErrNotFound
		}
		result.Name, result.Status = team.Name, team.Status
		query := tx.Table("team_model_grants AS g").Select("m.id,n.name,m.status").Joins("JOIN models AS m ON ?", database.ExactTextColumns(tx, clause.Column{Table: "g", Name: "model_id"}, clause.Column{Table: "m", Name: "id"})).Joins("JOIN model_names AS n ON ?", database.ExactTextColumns(tx, clause.Column{Table: "n", Name: "current_model_id"}, clause.Column{Table: "m", Name: "id"})).Where(database.ExactTextColumns(tx, clause.Column{Table: "n", Name: "model_id"}, clause.Column{Table: "m", Name: "id"})).Where(database.ExactText(tx, clause.Column{Table: "g", Name: "team_id"}, teamID))
		if err := query.Order("m.id").Limit(1001).Scan(&result.Models).Error; err != nil {
			return err
		}
		if len(result.Models) > 1000 {
			return errTeamModelRequestOverflow
		}
		for _, model := range result.Models {
			if !safeTeamSessionID(model.ID) {
				return apperrors.ErrInternal
			}
		}
		result.ModelCount = len(result.Models)
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return result, catalogError(err)
}
