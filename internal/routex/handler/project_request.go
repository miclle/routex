package handler

import (
	"github.com/fox-gonic/fox"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
	"net/http"
)

type CreateProjectRequestInput struct {
	ProjectID string   `uri:"project_id" json:"-"`
	RequestID string   `json:"request_id"`
	Kind      string   `json:"kind"`
	ModelIDs  []string `json:"model_ids"`
	Reason    string   `json:"reason"`
}
type ListProjectRequestsInput struct {
	ProjectID string `uri:"project_id" json:"-"`
	Status    string `query:"status"`
	Cursor    string `query:"cursor"`
	Limit     int    `query:"limit"`
}
type DecideProjectRequestInput struct {
	ProjectID string `uri:"project_id" json:"-"`
	RequestID string `uri:"request_id" json:"-"`
	Action    string `json:"action"`
	Reason    string `json:"reason"`
}

func (ctrl *Ctrl) CreateProjectRequest(c *fox.Context) error {
	var request CreateProjectRequestInput
	if err := decodeStrictRequest(c, &request); err != nil {
		return err
	}
	request.ProjectID = c.Param("project_id")
	if request.Kind != "" && request.Kind != "MODEL_ACCESS" {
		return apperrors.ErrBadRequest
	}
	result, err := ctrl.service.CreateProjectRequest(c.Request.Context(), currentAuthentication(c).User.ID, request.ProjectID, service.ProjectRequestInput{RequestID: request.RequestID, ModelIDs: request.ModelIDs, Reason: request.Reason})
	if err != nil {
		return err
	}
	c.JSON(http.StatusCreated, result)
	return nil
}
func (ctrl *Ctrl) ListProjectRequests(c *fox.Context, request ListProjectRequestsInput) (*service.ProjectRequestPage, error) {
	return ctrl.service.ListProjectRequests(c.Request.Context(), currentAuthentication(c).User.ID, request.ProjectID, service.ProjectRequestFilter{Status: request.Status, Cursor: request.Cursor, Limit: request.Limit})
}
func (ctrl *Ctrl) ProjectRequestCandidates(c *fox.Context, request ResourceCandidatesRequest) (*ResourceCandidatesResponse, error) {
	return resourceCandidates(ctrl.service.ProjectRequestCandidates(c.Request.Context(), currentAuthentication(c).User.ID, request.ProjectID, request.Query))
}
func (ctrl *Ctrl) DecideProjectRequest(c *fox.Context) (*service.ProjectRequestRecord, error) {
	var request DecideProjectRequestInput
	if err := decodeStrictRequest(c, &request); err != nil {
		return nil, err
	}
	request.ProjectID = c.Param("project_id")
	request.RequestID = c.Param("request_id")
	return ctrl.service.DecideProjectRequest(c.Request.Context(), currentAuthentication(c).User.ID, request.ProjectID, request.RequestID, service.ProjectRequestDecision{Action: request.Action, Reason: request.Reason})
}
