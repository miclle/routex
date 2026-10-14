package handler

import (
	"github.com/fox-gonic/fox"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
	"net/url"
)

func (ctrl *Ctrl) RuntimeArchiveMetadata(c *fox.Context) (*service.RuntimeArchiveMetadataPage, error) {
	c.Header("Cache-Control", "private, no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	f, err := parseRuntimeArchiveMetadataQuery(c.Request.URL.RawQuery)
	if err != nil {
		return nil, err
	}
	return ctrl.service.ListRuntimeArchiveMetadata(c.Request.Context(), currentAuthentication(c).User.ID, f)
}
func parseRuntimeArchiveMetadataQuery(raw string) (service.RuntimeArchiveMetadataFilter, error) {
	f := service.RuntimeArchiveMetadataFilter{}
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
