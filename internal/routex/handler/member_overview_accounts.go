package handler

import (
	"strconv"

	"github.com/fox-gonic/fox"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
)

func memberOverviewAccountsFilter(c *fox.Context) (service.MemberOverviewAccountsFilter, error) {
	query, err := parseTeamQuotaQuery(c, map[string]bool{"cursor": true, "limit": true})
	if err != nil {
		return service.MemberOverviewAccountsFilter{}, err
	}
	limit := 10
	if raw := query["limit"]; raw != "" {
		limit, err = strconv.Atoi(raw)
		if err != nil || strconv.Itoa(limit) != raw || limit < 1 || limit > 50 {
			return service.MemberOverviewAccountsFilter{}, apperrors.ErrBadRequest
		}
	}
	return service.MemberOverviewAccountsFilter{Cursor: query["cursor"], Limit: limit}, nil
}

func (ctrl *Ctrl) MemberOverviewAccounts(c *fox.Context) (*service.MemberOverviewAccountsPage, error) {
	c.Header("Cache-Control", "no-store")
	filter, err := memberOverviewAccountsFilter(c)
	if err != nil {
		return nil, err
	}
	return ctrl.service.MemberOverviewAccounts(c.Request.Context(), currentAuthentication(c).User.ID, filter)
}
