package handler

import (
	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/service"
)

func (ctrl *Ctrl) MemberEffectiveModels(c *fox.Context) (*service.MemberEffectiveModelsPage, error) {
	userID, err := memberOverviewSubject(c)
	if err != nil {
		return nil, err
	}
	return ctrl.service.MemberEffectiveModels(c.Request.Context(), currentAuthentication(c).User.ID, userID)
}
