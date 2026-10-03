package handler

import (
	"github.com/fox-gonic/fox"

	"github.com/miclle/routex/internal/routex/service"
)

func teamModelRequestCandidateFilter(c *fox.Context) (service.TeamModelRequestCandidateFilter, error) {
	query, err := parseTeamQuotaQuery(c, map[string]bool{"q": true, "cursor": true, "limit": true})
	if err != nil {
		return service.TeamModelRequestCandidateFilter{}, err
	}
	limit, err := teamModelRequestLimit(query)
	if err != nil {
		return service.TeamModelRequestCandidateFilter{}, err
	}
	return service.TeamModelRequestCandidateFilter{Query: query["q"], Cursor: query["cursor"], Limit: limit}, nil
}
func (ctrl *Ctrl) ListTeamModelRequestTeams(c *fox.Context) (*service.TeamModelRequestTeamPage, error) {
	filter, err := teamModelRequestCandidateFilter(c)
	if err != nil {
		return nil, err
	}
	return ctrl.service.ListTeamModelRequestTeams(c.Request.Context(), currentAuthentication(c).User.ID, filter)
}
func (ctrl *Ctrl) ListTeamModelRequestCandidates(c *fox.Context) (*service.TeamModelRequestCandidatePage, error) {
	filter, err := teamModelRequestCandidateFilter(c)
	if err != nil {
		return nil, err
	}
	return ctrl.service.ListTeamModelRequestCandidates(c.Request.Context(), currentAuthentication(c).User.ID, c.Param("team_id"), filter)
}
func (ctrl *Ctrl) GetTeamModelRequestCandidate(c *fox.Context) (*service.TeamModelRequestCandidate, error) {
	if err := noTeamQuotaQuery(c); err != nil {
		return nil, err
	}
	result, err := ctrl.service.GetTeamModelRequestCandidate(c.Request.Context(), currentAuthentication(c).User.ID, c.Param("team_id"), c.Param("model_id"))
	if err == nil {
		c.Header("ETag", `"`+result.ReviewETag+`"`)
	}
	return result, err
}
