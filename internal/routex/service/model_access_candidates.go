package service

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
)

type ModelAccessCandidate struct {
	ID                string              `json:"id"`
	Name              string              `json:"name"`
	Status            string              `json:"status"`
	CreatedAt         time.Time           `json:"created_at"`
	Protocols         []string            `json:"protocols"`
	InputCapabilities map[string][]string `json:"input_capabilities"`
	PersonalGranted   bool                `json:"personal_granted"`
	PendingRequestID  *string             `json:"pending_request_id"`
	ReviewETag        string              `json:"review_etag"`
}
type ModelAccessCandidateFilter struct {
	Query, Cursor string
	Limit         int
}
type ModelAccessCandidatePage struct {
	Items      []ModelAccessCandidate `json:"items"`
	NextCursor *string                `json:"next_cursor"`
}
type personalModelSubject struct {
	User    entity.User
	Model   entity.Model
	Name    entity.ModelName
	Grant   *entity.UserModelGrant
	Pending *entity.PersonalModelRequestPendingSlot
}

func loadPersonalModelSubject(tx *gorm.DB, userID, modelID string, lock bool) (*personalModelSubject, error) {
	result := &personalModelSubject{}
	q := personalExact(tx, "id", userID)
	if lock {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	if err := q.First(&result.User).Error; err != nil {
		return nil, err
	}
	if result.User.ID != userID {
		return nil, apperrors.ErrNotFound
	}
	q = personalExact(tx, "id", modelID)
	if lock {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	if err := q.First(&result.Model).Error; err != nil {
		return nil, err
	}
	if result.Model.ID != modelID {
		return nil, apperrors.ErrNotFound
	}
	if err := personalExact(personalExact(tx, "current_model_id", modelID), "model_id", modelID).First(&result.Name).Error; err != nil {
		return nil, err
	}
	if result.Name.ModelID != modelID || result.Name.CurrentModelID == nil || *result.Name.CurrentModelID != modelID {
		return nil, apperrors.ErrNotFound
	}
	var grant entity.UserModelGrant
	err := personalExact(personalExact(tx, "user_id", userID), "model_id", modelID).Take(&grant).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if err == nil && grant.UserID == userID && grant.ModelID == modelID {
		result.Grant = &grant
	}
	var slot entity.PersonalModelRequestPendingSlot
	err = personalExact(personalExact(tx, "applicant_user_id", userID), "model_id", modelID).Take(&slot).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if err == nil && slot.ApplicantUserID == userID && slot.ModelID == modelID {
		result.Pending = &slot
	}
	return result, nil
}
func personalModelCandidateETag(subject *personalModelSubject) string {
	var source *string
	var stamp *time.Time
	var pending *string
	if subject.Grant != nil {
		source = subject.Grant.SourceRequestID
		v := subject.Grant.CreatedAt.UTC()
		stamp = &v
	}
	if subject.Pending != nil {
		v := subject.Pending.RequestID
		pending = &v
	}
	return personalHash(struct {
		UserID, ModelID, Name, Status                       string
		UserCreated, UserUpdated, ModelCreated, NameCreated time.Time
		Disabled                                            bool
		Offboarded                                          *time.Time
		GrantCreated                                        *time.Time
		Source, Pending                                     *string
	}{subject.User.ID, subject.Model.ID, subject.Name.Name, subject.Model.Status, subject.User.CreatedAt.UTC(), subject.User.UpdatedAt.UTC(), subject.Model.CreatedAt.UTC(), subject.Name.CreatedAt.UTC(), subject.User.Disabled, subject.User.OffboardedAt, stamp, source, pending})
}
func (s *Service) ListModelAccessCandidates(ctx context.Context, actorID string, filter ModelAccessCandidateFilter) (*ModelAccessCandidatePage, error) {
	return s.modelAccessCandidates(ctx, actorID, "", filter)
}
func (s *Service) GetModelAccessCandidate(ctx context.Context, actorID, modelID string) (*ModelAccessCandidate, error) {
	if !personalModelID(modelID, "mdl") {
		return nil, apperrors.ErrBadRequest
	}
	page, err := s.modelAccessCandidates(ctx, actorID, modelID, ModelAccessCandidateFilter{})
	if err != nil {
		return nil, err
	}
	if len(page.Items) != 1 {
		return nil, apperrors.ErrNotFound
	}
	return &page.Items[0], nil
}
func (s *Service) modelAccessCandidates(ctx context.Context, actorID, modelID string, filter ModelAccessCandidateFilter) (*ModelAccessCandidatePage, error) {
	pattern, err := candidatePattern(filter.Query)
	if err != nil {
		return nil, err
	}
	if filter.Limit == 0 {
		filter.Limit = 50
	}
	if filter.Limit < 1 || filter.Limit > 50 || filter.Cursor != "" && !personalModelID(filter.Cursor, "mdl") {
		return nil, apperrors.ErrBadRequest
	}
	result := &ModelAccessCandidatePage{Items: []ModelAccessCandidate{}}
	err = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := exactEnabledActor(tx, actorID); err != nil {
			return err
		}
		var rows []struct {
			ID, Name, Status string
			CreatedAt        time.Time
		}
		q := tx.Table("models AS m").Select("m.id,n.name,m.status,m.created_at").Joins("JOIN model_names AS n ON ?", database.ExactTextColumns(tx, clause.Column{Table: "n", Name: "current_model_id"}, clause.Column{Table: "m", Name: "id"})).Where(database.ExactTextColumns(tx, clause.Column{Table: "n", Name: "model_id"}, clause.Column{Table: "m", Name: "id"})).Where(database.ExactText(tx, clause.Column{Table: "m", Name: "status"}, entity.ResourceActive)).Where("LOWER(n.name) LIKE ? ESCAPE '!'", pattern)
		if modelID != "" {
			q = q.Where(database.ExactText(tx, clause.Column{Table: "m", Name: "id"}, modelID))
		}
		if filter.Cursor != "" {
			q = q.Where("m.id > ?", filter.Cursor)
		}
		if err := q.Order("m.id").Limit(filter.Limit + 1).Scan(&rows).Error; err != nil {
			return err
		}
		if len(rows) > filter.Limit {
			v := rows[filter.Limit-1].ID
			result.NextCursor = &v
			rows = rows[:filter.Limit]
		}
		for _, row := range rows {
			if !personalModelID(row.ID, "mdl") || row.Status != entity.ResourceActive {
				return apperrors.ErrInternal
			}
			subject, err := loadPersonalModelSubject(tx, actorID, row.ID, false)
			if err != nil {
				return err
			}
			item := ModelAccessCandidate{ID: row.ID, Name: row.Name, Status: row.Status, CreatedAt: row.CreatedAt.UTC(), Protocols: []string{}, InputCapabilities: map[string][]string{}, PersonalGranted: subject.Grant != nil, ReviewETag: personalModelCandidateETag(subject)}
			if subject.Pending != nil {
				v := subject.Pending.RequestID
				item.PendingRequestID = &v
			}
			result.Items = append(result.Items, item)
		}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, catalogError(err)
	}
	// A purpose-specific directory never silently substitutes unpublished DB route
	// metadata for a missing current runtime lease.
	if s.runtime == nil {
		return nil, runtimeUnavailable
	}
	auth := s.runtime.auth.Load()
	if auth == nil || !time.Now().Before(auth.ValidUntil) {
		return nil, runtimeUnavailable
	}
	ids := make([]string, 0, len(result.Items))
	for _, v := range result.Items {
		ids = append(ids, v.ID)
	}
	initial := make(map[string]gatewayModelMetadata, len(ids))
	for _, modelID := range ids {
		initial[modelID] = gatewayModelMetadata{Protocols: []string{}, InputCapabilities: map[string][]string{}}
	}
	metadata, err := s.runtimeGatewayModelMetadata(ids, initial)
	if err != nil {
		return nil, err
	}
	for i := range result.Items {
		v, ok := metadata[result.Items[i].ID]
		if ok {
			result.Items[i].Protocols = v.Protocols
			result.Items[i].InputCapabilities = v.InputCapabilities
		}
		if result.Items[i].Protocols == nil {
			result.Items[i].Protocols = []string{}
		}
		if result.Items[i].InputCapabilities == nil {
			result.Items[i].InputCapabilities = map[string][]string{}
		}
	}
	return result, nil
}
func personalModelSubjectAvailable(subject *personalModelSubject) bool {
	return subject != nil && !subject.User.Disabled && subject.User.OffboardedAt == nil && subject.Model.Status == entity.ResourceActive && strings.TrimSpace(subject.Name.Name) != ""
}
