package handler

import (
	"strconv"

	"github.com/fox-gonic/fox"

	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
)

func (ctrl *Ctrl) ListModelAccessCandidates(c *fox.Context) (*service.ModelAccessCandidatePage, error) {
	query, err := parseTeamQuotaQuery(c, map[string]bool{"q": true, "cursor": true, "limit": true})
	if err != nil {
		return nil, err
	}
	limit := 0
	if query["limit"] != "" {
		limit, err = strconv.Atoi(query["limit"])
		if err != nil || limit < 1 || limit > 50 || strconv.Itoa(limit) != query["limit"] {
			return nil, apperrors.ErrBadRequest
		}
	}
	return ctrl.service.ListModelAccessCandidates(c.Request.Context(), currentAuthentication(c).User.ID, service.ModelAccessCandidateFilter{Query: query["q"], Cursor: query["cursor"], Limit: limit})
}
func (ctrl *Ctrl) GetModelAccessCandidate(c *fox.Context) (*service.ModelAccessCandidate, error) {
	if err := noTeamQuotaQuery(c); err != nil {
		return nil, err
	}
	result, err := ctrl.service.GetModelAccessCandidate(c.Request.Context(), currentAuthentication(c).User.ID, c.Param("model_id"))
	if err == nil {
		c.Header("ETag", `"`+result.ReviewETag+`"`)
	}
	return result, err
}
