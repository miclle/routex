package handler

import "github.com/fox-gonic/fox"

func (ctrl *Ctrl) GetAdminModel(c *fox.Context) (*ModelResponse, error) {
	if err := noTeamQuotaQuery(c); err != nil {
		return nil, err
	}
	result, err := ctrl.service.GetAdminModel(c.Request.Context(), currentAuthentication(c).User.ID, c.Param("model_id"))
	if err != nil {
		return nil, err
	}
	return modelResponse(*result), nil
}
