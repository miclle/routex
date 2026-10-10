package handler

import (
	"github.com/fox-gonic/fox"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
	"net/url"
)

func (ctrl *Ctrl) RuntimeInstallations(c *fox.Context) (*service.RuntimeInstallationPage, error) {
	c.Header("Cache-Control", "private, no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	if c.Request.ContentLength != 0 || len(c.Request.TransferEncoding) != 0 {
		return nil, apperrors.ErrBadRequest
	}
	f, err := parseRuntimeInstallationQuery(c.Request.URL.RawQuery)
	if err != nil {
		return nil, err
	}
	return ctrl.service.ListRuntimeInstallations(c.Request.Context(), currentAuthentication(c).User.ID, f)
}
func parseRuntimeInstallationQuery(raw string) (service.RuntimeInstallationFilter, error) {
	f := service.RuntimeInstallationFilter{}
	if len(raw) > 512 {
		return f, apperrors.ErrBadRequest
	}
	q, err := url.ParseQuery(raw)
	if err != nil {
		return f, apperrors.ErrBadRequest
	}
	for key, values := range q {
		if len(values) != 1 || values[0] == "" {
			return f, apperrors.ErrBadRequest
		}
		switch key {
		case "instance_id":
			f.InstanceID = values[0]
		case "cursor":
			f.Cursor = values[0]
		case "limit":
			f.Limit, err = service.ParseRuntimeInstallationLimit(values[0])
			if err != nil {
				return f, err
			}
		default:
			return f, apperrors.ErrBadRequest
		}
	}
	return f, nil
}
