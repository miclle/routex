package service

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
)

func personalModelRequestAccess(tx *gorm.DB, actorID, userID string, reviewer bool) (*entity.User, error) {
	actor, err := exactEnabledActor(tx, actorID)
	if err != nil {
		return nil, err
	}
	if reviewer {
		allowed, err := exactGovernancePermission(tx, actor, personalModelReviewPermission)
		if err != nil {
			return nil, err
		}
		if !allowed {
			return nil, apperrors.ErrForbidden
		}
	} else if actor.ID != userID {
		return nil, apperrors.ErrNotFound
	}
	return &actor, nil
}
func personalModelReviewETag(actorID string, row entity.PersonalModelRequest, subject *personalModelSubject) string {
	var current string
	if subject != nil {
		current = personalModelCandidateETag(subject)
	}
	return personalHash(struct {
		Actor, ID, RequestHash, Status, Current string
		UpdatedAt                               time.Time
		DecisionID                              *string
	}{actorID, row.ID, row.RequestHash, row.Status, current, row.UpdatedAt.UTC(), row.DecisionID})
}
func personalModelAllowed(actorID string, row entity.PersonalModelRequest, subject *personalModelSubject, reviewer bool) []string {
	result := []string{}
	if row.Status != entity.PersonalModelRequestPending || subject == nil ||
		subject.User.ID != row.ApplicantUserID || subject.Model.ID != row.ModelID ||
		subject.User.Disabled || subject.User.OffboardedAt != nil ||
		subject.Pending == nil || subject.Pending.RequestID != row.ID {
		return result
	}
	if reviewer {
		if actorID != row.ApplicantUserID {
			if personalModelSubjectAvailable(subject) {
				return []string{"approve", "reject"}
			}
			return []string{"reject"}
		}
	} else if actorID == row.ApplicantUserID {
		return []string{"withdraw"}
	}
	return result
}
func (s *Service) personalModelDetail(tx *gorm.DB, actorID string, row entity.PersonalModelRequest, reviewer bool) (*PersonalModelRequestDetail, error) {
	result := &PersonalModelRequestDetail{PersonalModelRequestRecord: personalModelRecord(row), AllowedActions: []string{}, ApplicationStatus: "pending"}
	subject, err := loadPersonalModelSubject(tx, row.ApplicantUserID, row.ModelID, false)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if err == nil {
		result.CurrentModel = &PersonalModelCurrentModel{ID: subject.Model.ID, Name: subject.Name.Name, Status: subject.Model.Status}
		result.CurrentGranted = subject.Grant != nil
		result.AllowedActions = personalModelAllowed(actorID, row, subject, reviewer)
	}
	result.ReviewETag = personalModelReviewETag(actorID, row, subject)
	if row.Status == entity.PersonalModelRequestApproved {
		result.ApplicationStatus = "superseded"
		if subject != nil && personalModelSubjectAvailable(subject) && subject.Grant != nil && subject.Grant.SourceRequestID != nil && *subject.Grant.SourceRequestID == row.ID {
			result.ApplicationStatus = "pending"
			result.RuntimeApplied = s.RuntimePersonalModelGrantApplied(row.ApplicantUserID, row.ModelID, row.ID)
			if result.RuntimeApplied {
				result.ApplicationStatus = "applied"
			}
		}
	}
	return result, nil
}
func (s *Service) GetPersonalModelRequest(ctx context.Context, actorID, userID, requestID string, reviewer bool) (*PersonalModelRequestDetail, error) {
	if !personalModelID(userID, "usr") || !personalModelID(requestID, "mar") {
		return nil, apperrors.ErrBadRequest
	}
	var result *PersonalModelRequestDetail
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := personalModelRequestAccess(tx, actorID, userID, reviewer); err != nil {
			return err
		}
		var row entity.PersonalModelRequest
		if err := personalExact(personalExact(tx, "applicant_user_id", userID), "id", requestID).Take(&row).Error; err != nil {
			return err
		}
		if row.ID != requestID || row.ApplicantUserID != userID {
			return apperrors.ErrNotFound
		}
		var err error
		result, err = s.personalModelDetail(tx, actorID, row, reviewer)
		return err
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return result, catalogError(err)
}
func (s *Service) ListPersonalModelRequests(ctx context.Context, actorID, userID string, filter PersonalModelRequestFilter, reviewer bool) (*PersonalModelRequestPage, error) {
	if !personalModelID(userID, "usr") {
		return nil, apperrors.ErrBadRequest
	}
	if filter.Limit == 0 {
		filter.Limit = 25
	}
	if filter.Limit < 1 || filter.Limit > 50 || filter.Status != "" && !personalModelStatus(filter.Status) {
		return nil, apperrors.ErrBadRequest
	}
	cursor, err := decodePersonalModelCursor(filter.Cursor, actorID+":"+userID+":"+filter.Status)
	if err != nil {
		return nil, err
	}
	result := &PersonalModelRequestPage{Items: []PersonalModelRequestRecord{}}
	err = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := personalModelRequestAccess(tx, actorID, userID, reviewer); err != nil {
			return err
		}
		query := personalExact(tx.Model(&entity.PersonalModelRequest{}), "applicant_user_id", userID)
		if filter.Status != "" {
			query = personalExact(query, "status", filter.Status)
		}
		if err := query.Session(&gorm.Session{}).Count(&result.Total).Error; err != nil {
			return err
		}
		if cursor != nil {
			query = query.Where(clause.Or(clause.Lt{Column: clause.Column{Name: "created_at"}, Value: cursor.CreatedAt}, clause.And(clause.Eq{Column: clause.Column{Name: "created_at"}, Value: cursor.CreatedAt}, clause.Lt{Column: clause.Column{Name: "id"}, Value: cursor.ID})))
		}
		var rows []entity.PersonalModelRequest
		if err := query.Order("created_at DESC,id DESC").Limit(filter.Limit + 1).Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) > filter.Limit {
			row := rows[filter.Limit-1]
			v := encodePersonalModelCursor(personalModelCursor{Scope: actorID + ":" + userID + ":" + filter.Status, CreatedAt: row.CreatedAt.UTC(), ID: row.ID})
			result.NextCursor = &v
			rows = rows[:filter.Limit]
		}
		for _, row := range rows {
			if row.ApplicantUserID != userID || !personalModelID(row.ID, "mar") {
				return apperrors.ErrInternal
			}
			result.Items = append(result.Items, personalModelRecord(row))
		}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return result, catalogError(err)
}
func personalModelStatus(value string) bool {
	switch value {
	case "pending", "approved", "rejected", "withdrawn", "cancelled":
		return true
	}
	return false
}
func (s *Service) MemberModelAccessWorkspace(ctx context.Context, actorID, userID string) (*MemberModelAccessWorkspace, error) {
	if !personalModelID(userID, "usr") {
		return nil, apperrors.ErrBadRequest
	}
	result := &MemberModelAccessWorkspace{UserID: userID, Models: []PersonalModelCurrentModel{}, CanReviewRequests: actorID != userID}
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := personalModelRequestAccess(tx, actorID, userID, true); err != nil {
			return err
		}
		var user entity.User
		if err := personalExact(tx, "id", userID).Select("id").Take(&user).Error; err != nil {
			return err
		}
		if user.ID != userID {
			return apperrors.ErrNotFound
		}
		query := tx.Table("user_model_grants AS g").Select("m.id,n.name,m.status").Joins("JOIN models AS m ON ?", database.ExactTextColumns(tx, clause.Column{Table: "g", Name: "model_id"}, clause.Column{Table: "m", Name: "id"})).Joins("JOIN model_names AS n ON ?", database.ExactTextColumns(tx, clause.Column{Table: "n", Name: "current_model_id"}, clause.Column{Table: "m", Name: "id"})).Where(database.ExactTextColumns(tx, clause.Column{Table: "n", Name: "model_id"}, clause.Column{Table: "m", Name: "id"})).Where(database.ExactText(tx, clause.Column{Table: "g", Name: "user_id"}, userID))
		if err := query.Order("m.id").Limit(1001).Scan(&result.Models).Error; err != nil {
			return err
		}
		if len(result.Models) > 1000 {
			return errPersonalModelOverflow
		}
		for _, model := range result.Models {
			if !personalModelID(model.ID, "mdl") {
				return apperrors.ErrInternal
			}
		}
		result.ModelCount = len(result.Models)
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return result, catalogError(err)
}
