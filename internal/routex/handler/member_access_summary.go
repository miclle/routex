package handler

import (
	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/service"
)

func (ctrl *Ctrl) MemberAccessSummary(c *fox.Context) (*service.MemberAccessSummary, error) {
	userID, err := memberOverviewSubject(c)
	if err != nil {
		return nil, err
	}
	return ctrl.service.GetMemberAccessSummary(c.Request.Context(), currentAuthentication(c).User.ID, userID)
}
