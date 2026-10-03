package handler

import (
	"github.com/fox-gonic/fox"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
	"net/http"
	"net/url"
	"strconv"
)

func teamQuotaReviewHeader(c *fox.Context) (string, error) {
	value, err := projectRequestReviewHeader(c, true)
	if err != nil || len(value) != 64 {
		return "", apperrors.ErrBadRequest
	}
	return value, nil
}
func noTeamQuotaQuery(c *fox.Context) error {
	if c.Request.URL.RawQuery != "" {
		return apperrors.ErrBadRequest
	}
	return nil
}
func (ctrl *Ctrl) TeamQuotaRequestContext(c *fox.Context) (*service.TeamQuotaRequestContext, error) {
	query, err := parseTeamQuotaQuery(c, map[string]bool{"dimension": true})
	if err != nil {
		return nil, err
	}
	result, err := ctrl.service.GetTeamQuotaRequestContext(c.Request.Context(), currentAuthentication(c).User.ID, c.Param("team_id"), query["dimension"])
	if err == nil {
		c.Header("ETag", `"`+result.ETag+`"`)
	}
	return result, err
}
func (ctrl *Ctrl) CreateTeamQuotaRequest(c *fox.Context) error {
	if err := noTeamQuotaQuery(c); err != nil {
		return err
	}
	var input service.TeamQuotaRequestInput
	if err := decodeStrictRequest(c, &input); err != nil {
		return err
	}
	etag, err := teamQuotaReviewHeader(c)
	if err != nil {
		return err
	}
	result, created, err := ctrl.service.CreateTeamQuotaRequest(c.Request.Context(), currentAuthentication(c).User.ID, c.Param("team_id"), etag, input)
	if err != nil {
		return err
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	c.Header("ETag", `"`+result.ETag+`"`)
	c.JSON(status, result)
	return nil
}
func parseTeamQuotaQuery(c *fox.Context, allowed map[string]bool) (map[string]string, error) {
	result := map[string]string{}
	query, err := url.ParseQuery(c.Request.URL.RawQuery)
	if err != nil {
		return nil, apperrors.ErrBadRequest
	}
	for field, values := range query {
		if !allowed[field] || len(values) != 1 || values[0] == "" {
			return nil, apperrors.ErrBadRequest
		}
		result[field] = values[0]
	}
	return result, nil
}
func teamQuotaListFilter(c *fox.Context, admin bool) (service.TeamQuotaRequestFilter, error) {
	allowed := map[string]bool{"status": true, "dimension": true, "team_id": true, "cursor": true, "limit": true}
	if !admin {
		allowed["view"] = true
	}
	query, err := parseTeamQuotaQuery(c, allowed)
	if err != nil {
		return service.TeamQuotaRequestFilter{}, err
	}
	limit := 0
	if query["limit"] != "" {
		limit, err = strconv.Atoi(query["limit"])
		if err != nil || strconv.Itoa(limit) != query["limit"] {
			return service.TeamQuotaRequestFilter{}, apperrors.ErrBadRequest
		}
	}
	view := query["view"]
	if !admin && view == "" {
		view = "my"
	}
	return service.TeamQuotaRequestFilter{View: view, Status: query["status"], Dimension: query["dimension"], TeamID: query["team_id"], Cursor: query["cursor"], Limit: limit}, nil
}
func (ctrl *Ctrl) ListTeamQuotaRequests(c *fox.Context) (*service.TeamQuotaRequestPage, error) {
	filter, err := teamQuotaListFilter(c, false)
	if err != nil {
		return nil, err
	}
	return ctrl.service.ListTeamQuotaRequests(c.Request.Context(), currentAuthentication(c).User.ID, filter, false)
}
func (ctrl *Ctrl) ListAdminTeamQuotaRequests(c *fox.Context) (*service.TeamQuotaRequestPage, error) {
	filter, err := teamQuotaListFilter(c, true)
	if err != nil {
		return nil, err
	}
	return ctrl.service.ListTeamQuotaRequests(c.Request.Context(), currentAuthentication(c).User.ID, filter, true)
}
func (ctrl *Ctrl) GetTeamQuotaRequest(c *fox.Context) (*service.TeamQuotaRequestDetail, error) {
	return ctrl.getTeamQuotaRequest(c, false)
}
func (ctrl *Ctrl) GetAdminTeamQuotaRequest(c *fox.Context) (*service.TeamQuotaRequestDetail, error) {
	return ctrl.getTeamQuotaRequest(c, true)
}
func (ctrl *Ctrl) getTeamQuotaRequest(c *fox.Context, admin bool) (*service.TeamQuotaRequestDetail, error) {
	if err := noTeamQuotaQuery(c); err != nil {
		return nil, err
	}
	result, err := ctrl.service.GetTeamQuotaRequest(c.Request.Context(), currentAuthentication(c).User.ID, c.Param("request_id"), admin)
	if err == nil {
		c.Header("ETag", `"`+result.ETag+`"`)
	}
	return result, err
}
func (ctrl *Ctrl) DecideTeamQuotaRequest(c *fox.Context) (*service.TeamQuotaDecisionRecord, error) {
	if err := noTeamQuotaQuery(c); err != nil {
		return nil, err
	}
	var input service.TeamQuotaDecisionInput
	if err := decodeStrictRequest(c, &input); err != nil {
		return nil, err
	}
	etag, err := teamQuotaReviewHeader(c)
	if err != nil {
		return nil, err
	}
	return ctrl.service.DecideTeamQuotaRequest(c.Request.Context(), currentAuthentication(c).User.ID, c.Param("request_id"), etag, input)
}
