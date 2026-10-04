package handler

import (
	"strconv"

	"github.com/fox-gonic/fox"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
)

func memberTeamsListFilter(c *fox.Context) (service.MemberTeamsFilter, error) {
	query, err := parseTeamQuotaQuery(c, map[string]bool{"cursor": true, "limit": true})
	if err != nil {
		return service.MemberTeamsFilter{}, err
	}
	limit := 20
	if raw := query["limit"]; raw != "" {
		limit, err = strconv.Atoi(raw)
		if err != nil || strconv.Itoa(limit) != raw || limit < 1 || limit > 50 {
			return service.MemberTeamsFilter{}, apperrors.ErrBadRequest
		}
	}
	return service.MemberTeamsFilter{Cursor: query["cursor"], Limit: limit}, nil
}
func (ctrl *Ctrl) ListMemberTeams(c *fox.Context) (*service.MemberTeamsPage, error) {
	c.Header("Cache-Control", "no-store")
	filter, err := memberTeamsListFilter(c)
	if err != nil {
		return nil, err
	}
	return ctrl.service.ListMemberTeams(c.Request.Context(), currentAuthentication(c).User.ID, c.Param("user_id"), filter)
}
