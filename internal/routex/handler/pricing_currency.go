package handler

import (
	"github.com/fox-gonic/fox"

	"github.com/miclle/routex/internal/routex/service"
)

func (ctrl *Ctrl) GetPricingCurrency(c *fox.Context) (*service.PricingCurrencyPage, error) {
	return ctrl.service.GetPricingCurrency(c.Request.Context(), currentAuthentication(c).User.ID)
}
