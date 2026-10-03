package handler

import (
	"github.com/fox-gonic/fox"

	"github.com/miclle/routex/internal/routex/service"
)

type ProjectCreationResponse struct {
	Project           *ProjectResponse                     `json:"project"`
	Receipt           service.ProjectCreationReceiptRecord `json:"receipt"`
	Committed         bool                                 `json:"committed"`
	RuntimeApplied    bool                                 `json:"runtime_applied"`
	ApplicationStatus string                               `json:"application_status"`
}

func projectCreationResponse(result *service.ProjectCreationResult) *ProjectCreationResponse {
	response := &ProjectCreationResponse{Receipt: result.Receipt, Committed: result.Committed, RuntimeApplied: result.RuntimeApplied, ApplicationStatus: result.ApplicationStatus}
	if result.Project != nil {
		response.Project = projectResponse(result.Project)
	}
	return response
}

func (ctrl *Ctrl) GetProjectCreationContext(c *fox.Context) (*service.ProjectCreationContext, error) {
	if err := noTeamQuotaQuery(c); err != nil {
		return nil, err
	}
	result, err := ctrl.service.GetProjectCreationContext(c.Request.Context(), currentAuthentication(c).User.ID)
	if err == nil {
		c.Header("ETag", `"`+result.ReviewETag+`"`)
	}
	return result, err
}

func (ctrl *Ctrl) ListProjectCreationModels(c *fox.Context) (*service.ProjectCreationModelPage, error) {
	query, err := parseTeamQuotaQuery(c, map[string]bool{"q": true, "cursor": true, "limit": true})
	if err != nil {
		return nil, err
	}
	limit, err := teamModelRequestLimit(query)
	if err != nil {
		return nil, err
	}
	return ctrl.service.ListProjectCreationModels(c.Request.Context(), currentAuthentication(c).User.ID, service.ProjectCreationModelFilter{Query: query["q"], Cursor: query["cursor"], Limit: limit})
}
