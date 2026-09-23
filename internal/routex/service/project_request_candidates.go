package service

import (
	"context"

	"github.com/miclle/routex/internal/routex/entity"
)

// ProjectRequestCandidates exposes names for requesting access, never invocation
// rights, provider configuration, or a member's unrelated personal grants.
func (s *Service) ProjectRequestCandidates(ctx context.Context, actorID, projectID, query string) ([]ResourceCandidate, error) {
	pattern, err := candidatePattern(query)
	if err != nil {
		return nil, err
	}
	db := s.authDB(ctx)
	if err := projectRequestManager(db, actorID, projectID); err != nil {
		return nil, catalogError(err)
	}
	var project entity.Project
	if err := db.First(&project, "id = ?", projectID).Error; err != nil {
		return nil, catalogError(err)
	}
	if project.Status != entity.ResourceActive {
		return nil, catalogConflict
	}
	granted := db.Model(&entity.ProjectModelGrant{}).Select("model_id").Where("project_id = ?", projectID)
	items := []ResourceCandidate{}
	err = db.Table("models m").Select("m.id,n.name").Joins("JOIN model_names n ON n.current_model_id = m.id").
		Where("m.status = ? AND LOWER(n.name) LIKE ? ESCAPE '!'", entity.ResourceActive, pattern).
		Where("m.id NOT IN (?)", granted).Order("m.id").Limit(50).Scan(&items).Error
	return items, catalogError(err)
}
