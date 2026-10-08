package handler

import (
	"net/url"

	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/service"
)

func (ctrl *Ctrl) AdminModelMonthlyRequests(c *fox.Context) (*service.ModelMonthlyRequests, error) {
	if len(c.Request.URL.RawQuery) > 32*1024 {
		return nil, errCatalogBadRequest
	}
	query, err := url.ParseQuery(c.Request.URL.RawQuery)
	if err != nil || len(query) != 1 || len(query["model_id"]) == 0 {
		return nil, errCatalogBadRequest
	}
	return ctrl.service.AdminModelMonthlyRequests(c.Request.Context(), currentAuthentication(c).User.ID, query["model_id"])
}
