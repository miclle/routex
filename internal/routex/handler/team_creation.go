package handler

import (
	"github.com/fox-gonic/fox"

	"github.com/miclle/routex/internal/routex/service"
)

type TeamCreationResponse struct {
	Team              *TeamResponse                     `json:"team"`
	Receipt           service.TeamCreationReceiptRecord `json:"receipt"`
	Committed         bool                              `json:"committed"`
	RuntimeApplied    bool                              `json:"runtime_applied"`
	ApplicationStatus string                            `json:"application_status"`
}

func teamCreationResponse(result *service.TeamCreationResult) *TeamCreationResponse {
	response := &TeamCreationResponse{Receipt: result.Receipt, Committed: result.Committed, RuntimeApplied: result.RuntimeApplied, ApplicationStatus: result.ApplicationStatus}
	if result.Team != nil {
		response.Team = teamResponse(result.Team)
	}
	return response
}

func (ctrl *Ctrl) GetTeamCreationContext(c *fox.Context) (*service.TeamCreationContext, error) {
	c.Header("Cache-Control", "private, no-store")
	if err := noTeamQuotaQuery(c); err != nil {
		return nil, err
	}
	result, err := ctrl.service.GetTeamCreationContext(c.Request.Context(), currentAuthentication(c).User.ID)
	if err == nil {
		c.Header("ETag", `"`+result.ReviewETag+`"`)
	}
	return result, err
}
