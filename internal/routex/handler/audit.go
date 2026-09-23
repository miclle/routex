package handler

import (
	"net/url"

	"github.com/fox-gonic/fox"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
)

func (ctrl *Ctrl) AuditEvents(c *fox.Context) (*service.AuditPage, error) {
	query, err := url.ParseQuery(c.Request.URL.RawQuery)
	if err != nil {
		return nil, apperrors.ErrBadRequest
	}
	for key, values := range query {
		if len(values) != 1 {
			return nil, apperrors.ErrBadRequest
		}
		switch key {
		case "q", "category", "range", "cursor":
		default:
			return nil, apperrors.ErrBadRequest
		}
	}
	return ctrl.service.ListAudit(c.Request.Context(), currentAuthentication(c).User.ID, service.AuditFilter{Query: query.Get("q"), Category: query.Get("category"), Range: query.Get("range"), Cursor: query.Get("cursor")})
}
