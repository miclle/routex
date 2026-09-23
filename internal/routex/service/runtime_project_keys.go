package service

import (
	"sort"

	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/entity"
)

type projectRuntimeData struct {
	Projects []entity.Project
	Managers []entity.ProjectManager
	Keys     []entity.ProjectKey
	Scopes   []entity.ProjectKeyModel
	Grants   []entity.ProjectModelGrant
}

func loadProjectRuntimeData(tx *gorm.DB) (*projectRuntimeData, error) {
	data := &projectRuntimeData{}
	for _, target := range []any{&data.Projects, &data.Managers, &data.Keys, &data.Scopes, &data.Grants} {
		if err := tx.Find(target).Error; err != nil {
			return nil, err
		}
	}
	return data, nil
}
func addProjectRuntimeAuthorization(auth *runtimeAuthorization, data *projectRuntimeData, users map[string]bool) {
	if data == nil {
		return
	}
	managed := map[string]bool{}
	for _, manager := range data.Managers {
		if users[manager.UserID] {
			managed[manager.ProjectID] = true
		}
	}
	projects := map[string]bool{}
	for _, project := range data.Projects {
		projects[project.ID] = project.Status == entity.ResourceActive && managed[project.ID]
	}
	grants := map[string]map[string]bool{}
	for _, grant := range data.Grants {
		if grants[grant.ProjectID] == nil {
			grants[grant.ProjectID] = map[string]bool{}
		}
		grants[grant.ProjectID][grant.ModelID] = true
	}
	scopes := map[string][]string{}
	for _, scope := range data.Scopes {
		scopes[scope.KeyID] = append(scopes[scope.KeyID], scope.ModelID)
	}
	for _, key := range data.Keys {
		if key.Status != entity.KeyActive || !projects[key.ProjectID] {
			continue
		}
		allowed := []string{}
		for _, modelID := range scopes[key.ID] {
			if auth.Models[modelID] && grants[key.ProjectID][modelID] {
				allowed = append(allowed, modelID)
			}
		}
		sort.Strings(allowed)
		auth.Keys[key.TokenHash] = runtimeKey{Key: commonProjectKeyMetadata(key), ProjectID: key.ProjectID, Models: allowed}
	}
}
func (s *Service) InvalidateRuntimeProject(projectID string) {
	if s.runtime != nil {
		s.runtime.deniedProjects.Store(projectID, s.runtime.epoch.Add(1))
	}
}
