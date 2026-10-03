package handler

import (
	"strconv"

	"github.com/fox-gonic/fox"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
)

func (ctrl *Ctrl) GetTeamRoles(c *fox.Context) (*service.TeamRolesRecord, error) {
	if err := noTeamQuotaQuery(c); err != nil {
		return nil, err
	}
	result, err := ctrl.service.GetTeamRoles(c.Request.Context(), currentAuthentication(c).User.ID, c.Param("team_id"))
	if err == nil {
		c.Header("ETag", `"`+result.ETag+`"`)
	}
	return result, err
}
func (ctrl *Ctrl) SetTeamRoles(c *fox.Context) (*service.TeamRolesRecord, error) {
	if err := noTeamQuotaQuery(c); err != nil {
		return nil, err
	}
	var input service.TeamRoleInput
	if err := decodeStrictRequest(c, &input); err != nil {
		return nil, err
	}
	etag, err := teamQuotaReviewHeader(c)
	if err != nil {
		return nil, err
	}
	result, err := ctrl.service.SetTeamRoles(c.Request.Context(), currentAuthentication(c).User.ID, c.Param("team_id"), etag, input)
	if err == nil {
		c.Header("ETag", `"`+result.ETag+`"`)
	}
	return result, err
}
func (ctrl *Ctrl) TeamRoleCandidates(c *fox.Context) (*service.TeamRoleCandidatePage, error) {
	query, err := parseTeamQuotaQuery(c, map[string]bool{"query": true, "cursor": true, "limit": true})
	if err != nil {
		return nil, err
	}
	limit := 0
	if query["limit"] != "" {
		limit, err = strconv.Atoi(query["limit"])
		if err != nil || strconv.Itoa(limit) != query["limit"] {
			return nil, apperrors.ErrBadRequest
		}
	}
	result, err := ctrl.service.TeamRoleCandidates(c.Request.Context(), currentAuthentication(c).User.ID, c.Param("team_id"), service.TeamRoleCandidateFilter{Query: query["query"], Cursor: query["cursor"], Limit: limit})
	if err == nil {
		c.Header("ETag", `"`+result.ETag+`"`)
	}
	return result, err
}
func (ctrl *Ctrl) ScopedTeamMemberCandidates(c *fox.Context) (*ResourceCandidatesResponse, error) {
	query, err := parseTeamQuotaQuery(c, map[string]bool{"query": true})
	if err != nil {
		return nil, err
	}
	return resourceCandidates(ctrl.service.ScopedTeamMemberCandidates(c.Request.Context(), currentAuthentication(c).User.ID, c.Param("team_id"), query["query"]))
}
func (ctrl *Ctrl) ScopedTeamModelCandidates(c *fox.Context) (*ResourceCandidatesResponse, error) {
	query, err := parseTeamQuotaQuery(c, map[string]bool{"query": true})
	if err != nil {
		return nil, err
	}
	return resourceCandidates(ctrl.service.ScopedTeamModelCandidates(c.Request.Context(), currentAuthentication(c).User.ID, c.Param("team_id"), query["query"]))
}
