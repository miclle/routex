package handler

import (
	"github.com/fox-gonic/fox"

	"github.com/miclle/routex/internal/routex/service"
)

type ResourceCandidatesRequest struct {
	ProjectID string `uri:"project_id" json:"-"`
	Query     string `query:"q"`
	Kind      string `query:"kind"`
}
type ResourceCandidateResponse struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email,omitempty"`
}
type ResourceCandidatesResponse struct {
	Items []ResourceCandidateResponse `json:"items"`
}

func resourceCandidates(items []service.ResourceCandidate, err error) (*ResourceCandidatesResponse, error) {
	if err != nil {
		return nil, err
	}
	response := &ResourceCandidatesResponse{Items: []ResourceCandidateResponse{}}
	for _, item := range items {
		response.Items = append(response.Items, ResourceCandidateResponse{ID: item.ID, Name: item.Name, Email: item.Email})
	}
	return response, nil
}
func (ctrl *Ctrl) ProjectManagerCandidates(c *fox.Context, request ResourceCandidatesRequest) (*ResourceCandidatesResponse, error) {
	return resourceCandidates(ctrl.service.ResourceMemberCandidates(c.Request.Context(), currentAuthentication(c).User.ID, service.ProjectResource, request.ProjectID, request.Query))
}
func (ctrl *Ctrl) TeamMemberCandidates(c *fox.Context, request ResourceCandidatesRequest) (*ResourceCandidatesResponse, error) {
	return resourceCandidates(ctrl.service.ResourceMemberCandidates(c.Request.Context(), currentAuthentication(c).User.ID, service.TeamResource, "", request.Query))
}
func (ctrl *Ctrl) ResourceModelCandidates(c *fox.Context, request ResourceCandidatesRequest) (*ResourceCandidatesResponse, error) {
	return resourceCandidates(ctrl.service.ResourceModelCandidates(c.Request.Context(), currentAuthentication(c).User.ID, service.ResourceKind(request.Kind), request.Query))
}
