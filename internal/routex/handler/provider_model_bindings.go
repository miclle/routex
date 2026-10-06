package handler

import (
	"strings"

	"github.com/fox-gonic/fox"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
)

func providerModelBindingsRequest(c *fox.Context) error {
	c.Header("Cache-Control", "no-store")
	if err := noTeamQuotaQuery(c); err != nil {
		return err
	}
	target := c.Param("provider_id")
	if !memberOverviewUserID.MatchString(target) || !strings.HasPrefix(target, "prv_") {
		return apperrors.ErrBadRequest
	}
	return nil
}
func (ctrl *Ctrl) GetProviderModelBindings(c *fox.Context) (*service.ProviderModelBindings, error) {
	if err := providerModelBindingsRequest(c); err != nil {
		return nil, err
	}
	return ctrl.service.GetProviderModelBindings(c.Request.Context(), currentAuthentication(c).User.ID, c.Param("provider_id"))
}
