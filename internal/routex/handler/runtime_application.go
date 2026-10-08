package handler

import (
	"github.com/fox-gonic/fox"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
	"net/url"
)

func (ctrl *Ctrl) RuntimeApplications(c *fox.Context) (*service.RuntimeApplicationPage, error) {
	c.Header("Cache-Control", "private, no-store")
	f, err := parseRuntimeApplicationQuery(c.Request.URL.RawQuery)
	if err != nil {
		return nil, err
	}
	return ctrl.service.ListRuntimeApplications(c.Request.Context(), currentAuthentication(c).User.ID, f)
}
func parseRuntimeApplicationQuery(raw string) (service.RuntimeApplicationFilter, error) {
	f := service.RuntimeApplicationFilter{}
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
			f.Limit, err = service.ParseRuntimeApplicationLimit(values[0])
			if err != nil {
				return f, err
			}
		default:
			return f, apperrors.ErrBadRequest
		}
	}
	return f, nil
}
