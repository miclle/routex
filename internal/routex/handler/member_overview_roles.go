package handler

import (
	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/service"
)

func memberOverviewRolesFilter(c *fox.Context) (service.MemberOverviewRolesFilter, error) {
	filter, err := memberOverviewAccountsFilter(c)
	return service.MemberOverviewRolesFilter(filter), err
}
func (ctrl *Ctrl) MemberOverviewRoles(c *fox.Context) (*service.MemberOverviewRolesPage, error) {
	c.Header("Cache-Control", "no-store")
	filter, err := memberOverviewRolesFilter(c)
	if err != nil {
		return nil, err
	}
	return ctrl.service.MemberOverviewRoles(c.Request.Context(), currentAuthentication(c).User.ID, filter)
}
