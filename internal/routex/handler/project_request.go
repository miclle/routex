package handler

import (
	"encoding/json"
	"github.com/fox-gonic/fox"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
	"net/http"
	"strings"
)

type CreateProjectRequestInput struct {
	ProjectID string          `uri:"project_id" json:"-"`
	RequestID string          `json:"request_id"`
	Kind      json.RawMessage `json:"kind"`
	ModelIDs  json.RawMessage `json:"model_ids"`
	Reason    string          `json:"reason"`
	Quota     json.RawMessage `json:"quota"`
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
	var kind string
	if request.Kind == nil {
		kind = "MODEL_ACCESS"
	} else if json.Unmarshal(request.Kind, &kind) != nil || kind != "MODEL_ACCESS" && kind != "QUOTA" {
		return apperrors.ErrBadRequest
	}
	var models []string
	var quota *service.ProjectQuotaPatch
	var reviewed string
	if kind == "QUOTA" {
		if request.ModelIDs != nil || request.Quota == nil || json.Unmarshal(request.Quota, &quota) != nil || quota == nil {
			return apperrors.ErrBadRequest
		}
		var err error
		reviewed, err = projectRequestReviewHeader(c, true)
		if err != nil {
			return err
		}
	} else if request.Quota != nil || request.ModelIDs == nil || json.Unmarshal(request.ModelIDs, &models) != nil {
		return apperrors.ErrBadRequest
	}
	result, err := ctrl.service.CreateProjectRequest(c.Request.Context(), currentAuthentication(c).User.ID, request.ProjectID, service.ProjectRequestInput{RequestID: request.RequestID, ModelIDs: models, Reason: request.Reason, Kind: kind, Quota: quota, ReviewETag: reviewed})
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
	reviewed, err := projectRequestReviewHeader(c, false)
	if err != nil {
		return nil, err
	}
	return ctrl.service.DecideProjectRequest(c.Request.Context(), currentAuthentication(c).User.ID, request.ProjectID, request.RequestID, service.ProjectRequestDecision{Action: request.Action, Reason: request.Reason, ReviewETag: reviewed})
}

func projectRequestReviewHeader(c *fox.Context, required bool) (string, error) {
	values := c.Request.Header.Values("If-Match")
	if len(values) == 0 && !required {
		return "", nil
	}
	if len(values) != 1 {
		return "", apperrors.ErrBadRequest
	}
	raw := strings.TrimSpace(values[0])
	if len(raw) < 3 || raw[0] != '"' || raw[len(raw)-1] != '"' || strings.ContainsAny(raw[1:len(raw)-1], "\"\r\n,") {
		return "", apperrors.ErrBadRequest
	}
	return raw[1 : len(raw)-1], nil
}

func (ctrl *Ctrl) ProjectQuotaRequestContext(c *fox.Context) (*service.ProjectQuotaRequestContext, error) {
	if c.Request.URL.RawQuery != "" {
		return nil, apperrors.ErrBadRequest
	}
	result, err := ctrl.service.GetProjectQuotaRequestContext(c.Request.Context(), currentAuthentication(c).User.ID, c.Param("project_id"))
	if err == nil {
		c.Header("ETag", `"`+result.ReviewETag+`"`)
	}
	return result, err
}

func (ctrl *Ctrl) GetProjectRequest(c *fox.Context) (*service.ProjectRequestRecord, error) {
	if c.Request.URL.RawQuery != "" {
		return nil, apperrors.ErrBadRequest
	}
	result, err := ctrl.service.GetProjectRequest(c.Request.Context(), currentAuthentication(c).User.ID, c.Param("project_id"), c.Param("request_id"))
	if err == nil && result.ApprovalReviewETag != "" {
		c.Header("ETag", `"`+result.ApprovalReviewETag+`"`)
	}
	return result, err
}
