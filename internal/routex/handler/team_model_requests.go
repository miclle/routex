package handler

import (
	"net/http"
	"strconv"

	"github.com/fox-gonic/fox"

	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
)

func teamModelRequestFilter(c *fox.Context, reviewer bool) (service.TeamModelRequestFilter, error) {
	allowed := map[string]bool{"status": true, "model_id": true, "cursor": true, "limit": true}
	if !reviewer {
		allowed["team_id"] = true
	}
	query, err := parseTeamQuotaQuery(c, allowed)
	if err != nil {
		return service.TeamModelRequestFilter{}, err
	}
	limit, err := teamModelRequestLimit(query)
	if err != nil {
		return service.TeamModelRequestFilter{}, err
	}
	return service.TeamModelRequestFilter{Status: query["status"], TeamID: query["team_id"], ModelID: query["model_id"], Cursor: query["cursor"], Limit: limit}, nil
}
func teamModelRequestLimit(query map[string]string) (int, error) {
	if query["limit"] == "" {
		return 0, nil
	}
	limit, err := strconv.Atoi(query["limit"])
	if err != nil || limit < 1 || limit > 50 || strconv.Itoa(limit) != query["limit"] {
		return 0, apperrors.ErrBadRequest
	}
	return limit, nil
}
func (ctrl *Ctrl) CreateTeamModelRequest(c *fox.Context) error {
	if err := noTeamQuotaQuery(c); err != nil {
		return err
	}
	var input service.TeamModelRequestInput
	if err := decodeStrictRequest(c, &input); err != nil {
		return err
	}
	etag, err := personalModelHeader(c)
	if err != nil {
		return err
	}
	result, created, err := ctrl.service.CreateTeamModelRequest(c.Request.Context(), currentAuthentication(c).User.ID, etag, input)
	if err != nil {
		return err
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	c.Header("ETag", `"`+result.ReviewETag+`"`)
	c.JSON(status, result)
	return nil
}
func (ctrl *Ctrl) ListOwnTeamModelRequests(c *fox.Context) (*service.TeamModelRequestPage, error) {
	return ctrl.listTeamModelRequests(c, false)
}
func (ctrl *Ctrl) ListTeamModelRequests(c *fox.Context) (*service.TeamModelRequestPage, error) {
	return ctrl.listTeamModelRequests(c, true)
}
func (ctrl *Ctrl) listTeamModelRequests(c *fox.Context, reviewer bool) (*service.TeamModelRequestPage, error) {
	filter, err := teamModelRequestFilter(c, reviewer)
	if err != nil {
		return nil, err
	}
	teamID := ""
	if reviewer {
		teamID = c.Param("team_id")
	}
	return ctrl.service.ListTeamModelRequests(c.Request.Context(), currentAuthentication(c).User.ID, teamID, filter, reviewer)
}
func (ctrl *Ctrl) GetOwnTeamModelRequest(c *fox.Context) (*service.TeamModelRequestDetail, error) {
	return ctrl.getTeamModelRequest(c, false)
}
func (ctrl *Ctrl) GetTeamModelRequest(c *fox.Context) (*service.TeamModelRequestDetail, error) {
	return ctrl.getTeamModelRequest(c, true)
}
func (ctrl *Ctrl) getTeamModelRequest(c *fox.Context, reviewer bool) (*service.TeamModelRequestDetail, error) {
	if err := noTeamQuotaQuery(c); err != nil {
		return nil, err
	}
	teamID := ""
	if reviewer {
		teamID = c.Param("team_id")
	}
	result, err := ctrl.service.GetTeamModelRequest(c.Request.Context(), currentAuthentication(c).User.ID, teamID, c.Param("request_id"), reviewer)
	if err == nil {
		c.Header("ETag", `"`+result.ReviewETag+`"`)
	}
	return result, err
}
func (ctrl *Ctrl) DecideOwnTeamModelRequest(c *fox.Context) (*service.TeamModelDecisionRecord, error) {
	return ctrl.decideTeamModelRequest(c, false)
}
func (ctrl *Ctrl) DecideTeamModelRequest(c *fox.Context) (*service.TeamModelDecisionRecord, error) {
	return ctrl.decideTeamModelRequest(c, true)
}
func (ctrl *Ctrl) decideTeamModelRequest(c *fox.Context, reviewer bool) (*service.TeamModelDecisionRecord, error) {
	if err := noTeamQuotaQuery(c); err != nil {
		return nil, err
	}
	var input service.TeamModelDecisionInput
	if err := decodeStrictRequest(c, &input); err != nil {
		return nil, err
	}
	etag, err := personalModelHeader(c)
	if err != nil {
		return nil, err
	}
	teamID := ""
	if reviewer {
		teamID = c.Param("team_id")
	}
	return ctrl.service.DecideTeamModelRequest(c.Request.Context(), currentAuthentication(c).User.ID, teamID, c.Param("request_id"), etag, input, reviewer)
}
func (ctrl *Ctrl) TeamModelRequestWorkspace(c *fox.Context) (*service.TeamModelRequestWorkspace, error) {
	if err := noTeamQuotaQuery(c); err != nil {
		return nil, err
	}
	return ctrl.service.TeamModelRequestWorkspace(c.Request.Context(), currentAuthentication(c).User.ID, c.Param("team_id"))
}
