package service

import (
	"context"
	"database/sql"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
)

type TeamModelRequestTeam struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	MembershipID string `json:"membership_id"`
}
type TeamModelRequestTeamPage struct {
	Items      []TeamModelRequestTeam `json:"items"`
	NextCursor *string                `json:"next_cursor"`
}
type TeamModelRequestCandidateFilter struct {
	Query, Cursor string
	Limit         int
}
type TeamModelRequestCandidate struct {
	ID                  string              `json:"id"`
	Name                string              `json:"name"`
	TeamID              string              `json:"team_id"`
	Status              string              `json:"status"`
	CreatedAt           time.Time           `json:"created_at"`
	Protocols           []string            `json:"protocols"`
	InputCapabilities   map[string][]string `json:"input_capabilities"`
	TeamGranted         bool                `json:"team_granted"`
	PendingRequest      bool                `json:"pending_request"`
	OwnPendingRequestID *string             `json:"own_pending_request_id"`
	ReviewETag          string              `json:"review_etag"`
}
type TeamModelRequestCandidatePage struct {
	Items      []TeamModelRequestCandidate `json:"items"`
	NextCursor *string                     `json:"next_cursor"`
}

func normalizeTeamModelCandidateFilter(filter TeamModelRequestCandidateFilter) (TeamModelRequestCandidateFilter, string, error) {
	pattern, err := candidatePattern(filter.Query)
	if err != nil {
		return filter, "", err
	}
	if filter.Limit == 0 {
		filter.Limit = 50
	}
	if filter.Limit < 1 || filter.Limit > 50 || filter.Cursor != "" && !safeTeamSessionID(filter.Cursor) {
		return filter, "", apperrors.ErrBadRequest
	}
	return filter, pattern, nil
}
func (s *Service) ListTeamModelRequestTeams(ctx context.Context, actorID string, filter TeamModelRequestCandidateFilter) (*TeamModelRequestTeamPage, error) {
	filter, pattern, err := normalizeTeamModelCandidateFilter(filter)
	if err != nil {
		return nil, err
	}
	result := &TeamModelRequestTeamPage{Items: []TeamModelRequestTeam{}}
	err = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := exactEnabledActor(tx, actorID); err != nil {
			return err
		}
		q := tx.Table("teams AS t").Select("t.id,t.name,tm.id AS membership_id").Joins("JOIN team_memberships AS tm ON ?", database.ExactTextColumns(tx, clause.Column{Table: "t", Name: "id"}, clause.Column{Table: "tm", Name: "team_id"})).Where(database.ExactText(tx, clause.Column{Table: "tm", Name: "user_id"}, actorID)).Where(database.ExactText(tx, clause.Column{Table: "tm", Name: "status"}, entity.ResourceActive)).Where(database.ExactText(tx, clause.Column{Table: "t", Name: "status"}, entity.ResourceActive)).Where(clause.Or(database.ExactText(tx, clause.Column{Table: "tm", Name: "role"}, entity.TeamMember), database.ExactText(tx, clause.Column{Table: "tm", Name: "role"}, entity.TeamOwner))).Where("LOWER(t.name) LIKE ? ESCAPE '!'", pattern)
		if filter.Cursor != "" {
			q = q.Where("t.id > ?", filter.Cursor)
		}
		if err := q.Order("t.id").Limit(filter.Limit + 1).Scan(&result.Items).Error; err != nil {
			return err
		}
		if len(result.Items) > filter.Limit {
			cursor := result.Items[filter.Limit-1].ID
			result.NextCursor = &cursor
			result.Items = result.Items[:filter.Limit]
		}
		for _, item := range result.Items {
			if !safeTeamSessionID(item.ID) || !safeTeamSessionID(item.MembershipID) {
				return apperrors.ErrInternal
			}
		}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return result, catalogError(err)
}
func (s *Service) ListTeamModelRequestCandidates(ctx context.Context, actorID, teamID string, filter TeamModelRequestCandidateFilter) (*TeamModelRequestCandidatePage, error) {
	return s.teamModelRequestCandidates(ctx, actorID, teamID, "", filter)
}
func (s *Service) GetTeamModelRequestCandidate(ctx context.Context, actorID, teamID, modelID string) (*TeamModelRequestCandidate, error) {
	if !safeTeamSessionID(modelID) {
		return nil, apperrors.ErrBadRequest
	}
	page, err := s.teamModelRequestCandidates(ctx, actorID, teamID, modelID, TeamModelRequestCandidateFilter{})
	if err != nil {
		return nil, err
	}
	if len(page.Items) != 1 {
		return nil, apperrors.ErrNotFound
	}
	return &page.Items[0], nil
}
func (s *Service) teamModelRequestCandidates(ctx context.Context, actorID, teamID, modelID string, filter TeamModelRequestCandidateFilter) (*TeamModelRequestCandidatePage, error) {
	if !safeTeamSessionID(teamID) {
		return nil, apperrors.ErrBadRequest
	}
	filter, pattern, err := normalizeTeamModelCandidateFilter(filter)
	if err != nil {
		return nil, err
	}
	result := &TeamModelRequestCandidatePage{Items: []TeamModelRequestCandidate{}}
	err = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := exactEnabledActor(tx, actorID); err != nil {
			return err
		}
		context, err := loadTeamModelSubject(tx, actorID, teamID, "", false)
		if err != nil {
			return err
		}
		if !teamModelActiveMember(context) {
			return apperrors.ErrNotFound
		}
		var models []struct {
			ID string
		}
		q := tx.Table("models AS m").Select("m.id").Joins("JOIN model_names AS n ON ?", database.ExactTextColumns(tx, clause.Column{Table: "n", Name: "current_model_id"}, clause.Column{Table: "m", Name: "id"})).Where(database.ExactTextColumns(tx, clause.Column{Table: "n", Name: "model_id"}, clause.Column{Table: "m", Name: "id"})).Where(database.ExactText(tx, clause.Column{Table: "m", Name: "status"}, entity.ResourceActive)).Where("LOWER(n.name) LIKE ? ESCAPE '!'", pattern)
		if modelID != "" {
			q = q.Where(database.ExactText(tx, clause.Column{Table: "m", Name: "id"}, modelID))
		}
		if filter.Cursor != "" {
			q = q.Where("m.id > ?", filter.Cursor)
		}
		if err := q.Order("m.id").Limit(filter.Limit + 1).Scan(&models).Error; err != nil {
			return err
		}
		if len(models) > filter.Limit {
			cursor := models[filter.Limit-1].ID
			result.NextCursor = &cursor
			models = models[:filter.Limit]
		}
		for _, model := range models {
			if !safeTeamSessionID(model.ID) {
				return apperrors.ErrInternal
			}
			subject, err := loadTeamModelSubject(tx, actorID, teamID, model.ID, false)
			if err != nil {
				return err
			}
			if !teamModelActiveMember(subject) || !teamModelAvailable(subject) {
				return apperrors.ErrNotFound
			}
			item := TeamModelRequestCandidate{ID: model.ID, Name: subject.Name.Name, TeamID: teamID, Status: subject.Model.Status, CreatedAt: subject.Model.CreatedAt.UTC(), Protocols: []string{}, InputCapabilities: map[string][]string{}, TeamGranted: subject.Grant != nil, PendingRequest: subject.Pending != nil, ReviewETag: teamModelCandidateETag(actorID, subject)}
			if subject.Pending != nil {
				var pending entity.TeamModelRequest
				if err := personalExact(tx, "id", subject.Pending.RequestID).Take(&pending).Error; err != nil {
					return err
				}
				if pending.TeamID != teamID || pending.ModelID != model.ID || pending.Status != entity.TeamModelRequestPending {
					return apperrors.ErrInternal
				}
				if pending.ApplicantUserID == actorID {
					requestID := pending.ID
					item.OwnPendingRequestID = &requestID
				}
			}
			result.Items = append(result.Items, item)
		}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, catalogError(err)
	}
	// Discovery uses the current leased runtime, never unpublished route tables.
	if s.runtime == nil {
		return nil, runtimeUnavailable
	}
	ids := make([]string, 0, len(result.Items))
	metadata := make(map[string]gatewayModelMetadata, len(result.Items))
	for _, item := range result.Items {
		ids = append(ids, item.ID)
		metadata[item.ID] = gatewayModelMetadata{Protocols: []string{}, InputCapabilities: map[string][]string{}}
	}
	metadata, err = s.runtimeGatewayModelMetadata(ids, metadata)
	if err != nil {
		return nil, err
	}
	for i := range result.Items {
		item := metadata[result.Items[i].ID]
		result.Items[i].Protocols = item.Protocols
		result.Items[i].InputCapabilities = item.InputCapabilities
	}
	return result, nil
}
